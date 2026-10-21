// Package accounts là module quản trị tài khoản cán bộ (pf/accounts): cầu nối
// Admin API ↔ Keycloak Admin REST, sổ thao tác chỉ ghi thêm và consumer trạng
// thái tổ chức (KN US-ADM-01..03, HTX US-ACC-01/02, US-HTXSTATUS-01..03, CGH FN-01).
//
// Module khác chỉ được import accounts/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform (gốc lắp ráp) và cmd/worker.
package accounts

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/keycloak"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "accounts_app"

// Deps là phụ thuộc của module.
type Deps struct {
	Navigation      partyport.Navigation // registered application context landing
	SessionPolicies port.SessionPolicies // trusted application-owned request projections
	// Sessions contains trusted application-owned projections for selected requests.
	Sessions *authz.SessionRegistry
	// Directory: Keycloak Admin REST. Nil → mọi route trả 503 accounts.dependency_unavailable.
	Directory port.Directory
	// Scope, Orgs: pf/party — phạm vi theo cây phân cấp và tra tổ chức (ADR-0009).
	Scope partyport.ScopeResolver
	Orgs  partyport.Orgs
	// Households, Vault, Params: tài khoản nông dân App HTX (G2) — thành viên HTX của
	// nông hộ, tra nông hộ theo SĐT qua chỉ mục mù, tham số activation.grant_lifespan_hours.
	Households partyport.Households
	Vault      client.Client
	Params     params.Params
	// KN v7.7 gói P1: phòng ban (pf/party P2), tên xã (pf/masterdata), danh bạ tên dự phòng cho Log.
	Departments partyport.Departments
	Units       partyport.AdminUnits
	Names       partyport.ActorNames
	// W1.4 (R-MOD-08): trạng thái bật module theo đơn vị — vai của module đã tắt không cấp được.
	Modules partyport.ModuleEnablement
	// DormantDryRun (D24): job accounts_dormant chỉ đếm, không báo trước/khóa (AG_ACCOUNTS_DORMANT_DRY_RUN).
	DormantDryRun bool
}

// Module là pf/accounts đã nối.
type Module struct {
	svc      *app.Service
	sessions *authz.SessionRegistry
}

// New tạo module với pool của vai accounts_app.
func New(db tenant.Beginner, clk clock.Clock, d Deps) *Module {
	db = forModule(db)
	return &Module{svc: &app.Service{DB: db, Clock: clk, Dir: d.Directory, Scope: d.Scope, Orgs: d.Orgs, Navigation: d.Navigation,
		Households: d.Households, Vault: d.Vault, Params: d.Params, SessionPolicies: d.SessionPolicies, Departments: d.Departments, Units: d.Units, Names: d.Names,
		Modules: d.Modules, DormantDryRun: d.DormantDryRun}, sessions: d.Sessions}
}

// RegisterApp gắn route Application API của App HTX: tài khoản nông dân (quản lý
// HTX, cán bộ KN) và danh bạ cán bộ HTX của nông dân.
func (m *Module) RegisterApp(rg *gin.RouterGroup) {
	h := handler.Handler{Svc: m.svc}
	h.RegisterApp(rg)
	h.RegisterKNApp(rg) // KN v7.7: /v1/me/contexts, /v1/me/context
}

// FarmerHouseholds cho module khác: nông hộ liên kết với tài khoản nông dân
// (cropcycle: phạm vi thửa; extension: yêu cầu hỗ trợ). Tenant lấy từ context.
func (m *Module) FarmerHouseholds() port.FarmerHouseholds { return m.svc }

// FarmerAccounts cho gốc lắp ráp: tài khoản nông dân của các nông hộ (cảnh báo khẩn theo địa bàn).
func (m *Module) FarmerAccounts() port.FarmerAccounts { return m.svc }

// RegisterAdmin gắn route Admin API (MFA bắt buộc ở router).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) {
	h := handler.Handler{Svc: m.svc}
	h.RegisterAdmin(rg)
	h.RegisterKNAdmin(rg) // KN v7.7: gán vai trò, Log, chế độ hỗ trợ
}

// Middleware thực hiện httpx.RequestMiddleware: ngữ cảnh phiên KN v7.7 (header X-AG-Context) cho MỌI
// route của hai bề mặt — vai, phòng ban, phạm vi giải ở server (gap §4.5).
func (m *Module) Middleware() gin.HandlerFunc {
	return handler.Handler{Svc: m.svc, Sessions: m.sessions}.Middleware()
}

// UseDepartments nối phòng ban của pf/party sau khi dựng (gốc lắp ráp).
func (m *Module) UseDepartments(d partyport.Departments) { m.svc.Departments = d }

// UseFacilityCatalog (W11.41) nối danh mục cơ sở giết mổ CNTS (pf/masterdata): facility_refs của phạm vi Cấp Xã
// phải là cơ sở đang dùng. Không nối → chỉ kiểm dạng UUID.
func (m *Module) UseFacilityCatalog(f app.FacilityCatalog) { m.svc.Facilities = f }

// SessionContext cho pf/party: menu theo ngữ cảnh phiên (P2 UseSessionContext).
func (m *Module) SessionContext() partyport.SessionContext { return app.SessionContext{} }

// DepartmentUsage cho pf/party: số tài khoản theo phòng ban (P2 UseDepartmentUsage — mở khóa `xóa` phòng ban).
func (m *Module) DepartmentUsage() partyport.DepartmentUsage { return m.svc }

// OrgImpact cho pf/party: dòng `accounts` của bảng Ảnh hưởng (FN-AD-04.02).
func (m *Module) OrgImpact() partyport.OrgImpactSource { return app.OrgImpact{S: m.svc} }

// ActorNames cho module khác: tên hiển thị người thao tác trong khung Log (Keycloak, dự phòng danh bạ).
func (m *Module) ActorNames() partyport.ActorNames { return m.svc }

// Assignments cho P4/P5: lần gán vai trò còn hiệu lực.
func (m *Module) Assignments() port.Assignments { return m.svc }

// RoleHolders (gói NOTIF): tài khoản theo vai / cờ nhiệm vụ trên lần gán (vd Cấp Tỉnh, QTNV báo giá) — người nhận thông
// báo của consumer worker (tenant tường minh).
func (m *Module) RoleHolders() port.RoleHolders { return m.svc }

// ScopedDutyHolders provides a read-only recipient query restricted to one issuer node.
func (m *Module) ScopedDutyHolders() port.ScopedDutyHolders { return app.ScopedDutyReader{Svc: m.svc} }

// XaScopes (W11.46) cho lịch trực KSGM BM-03 của pf/extension: cán bộ Cấp Xã của phòng ban kèm xã phụ trách, giới hạn
// cơ sở/hộ (tenant tường minh).
func (m *Module) XaScopes() port.XaScopes { return m.svc }

// KNPeople (W11.07) cho pf/hrm, Contact, đánh giá: phạm vi nhân sự của ngữ cảnh phiên (BR-04 HCTH, HS-37).
func (m *Module) KNPeople() port.KNPeople { return m.svc }

// InternalUnits (W11.07) cho module khác: danh mục đơn vị nội bộ (phòng/trạm/trại/BGĐ — Q-068).
func (m *Module) InternalUnits() port.InternalUnits { return m.svc }

// OrgStatusHandler xử lý pf.party.org.deactivated/reactivated (consumer
// port.OrgStatusConsumer, chạy trong inbox.Process trên pool accounts_app).
func (m *Module) OrgStatusHandler() inbox.Handler {
	return func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
		_, err := m.svc.HandleOrgStatus(ctx, tx, env)
		return err
	}
}

// MailDirectory cho pf/reports: email đã xác minh của tài khoản lúc gửi lịch báo
// cáo. Chưa cấu hình Keycloak → nil (pf/reports bỏ gửi kèm cảnh báo).
func (m *Module) MailDirectory() port.MailDirectory {
	if m.svc.Dir == nil {
		return nil
	}
	return app.MailDirectory{Dir: m.svc.Dir}
}

// ReportRecipients cho pf/reports: tài khoản đã đăng ký của một HTX (email đã xác minh)
// để quản lý HTX chọn người nhận lịch báo cáo (HTX US-DASH-03). Chưa cấu hình Keycloak → nil.
func (m *Module) ReportRecipients() port.RecipientDirectory {
	if m.svc.Dir == nil {
		return nil
	}
	return app.MailDirectory{Dir: m.svc.Dir}
}

// StaffContacts cho pf/extension: số điện thoại của cán bộ lúc bấm "gọi/nhắn" trong
// danh bạ trực (KN US-DIR-02). Chưa cấu hình Keycloak → nil (route liên hệ trả 503).
func (m *Module) StaffContacts() port.StaffContacts {
	if m.svc.Dir == nil {
		return nil
	}
	return app.StaffContacts{Dir: m.svc.Dir}
}

// ExtensionKNContext cho pf/extension (KN v7.7 P5): ngữ cảnh KN của request (X-AG-Context) kèm tỉnh và cấp
// quản lý của lần gán — extension.UseKNContext(acc.ExtensionKNContext(), acc.ExtensionKNDirectory()).
func (m *Module) ExtensionKNContext() port.KNContextResolver {
	return app.ExtensionKN{Assignments: m.svc, Orgs: m.svc.Orgs}
}

// ExtensionKNDirectory cho pf/extension (KN v7.7 P5): cán bộ KN của phòng ban × tỉnh từ sổ gán vai trò.
func (m *Module) ExtensionKNDirectory() port.KNDirectory {
	return app.ExtensionKN{Assignments: m.svc, Orgs: m.svc.Orgs}
}

// DormantTenants tìm tenant có tài khoản đang hoạt động bằng pool ag_ops_jobs (worker, nối thẳng) cho job
// port.JobDormant.
func DormantTenants(pool *pgxpool.Pool) app.TenantLister {
	return func(ctx context.Context) ([]uuid.UUID, error) { return store.DormantTenants(ctx, pool) }
}

// Jobs trả job định kỳ của module (chỉ chạy ở leader): D24 — mỗi ngày 02:00 báo trước (ngày 38) và khóa (ngày 45)
// tài khoản không hoạt động, ghi sổ account_event (job.accounts_dormant). Cần Directory (Keycloak) có SessionReader.
func (m *Module) Jobs(tenants app.TenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: port.JobDormant, Spec: schedule.Daily{Hour: 2, Minute: 0}, RunOnStart: true, Timeout: time.Hour,
		Run: func(ctx context.Context) error {
			r, err := m.svc.RunDormant(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("vô hiệu tài khoản không hoạt động", "event", "job."+port.JobDormant, "checked", r.Checked,
					"warned", r.Warned, "disabled", r.Disabled, "dry_run", r.DryRun, "outcome", outcome)
			}
			return err
		},
	}}
}

// RunDormant chạy một lượt job accounts_dormant (kiểm thử, công cụ vận hành).
func (m *Module) RunDormant(ctx context.Context, tenants app.TenantLister) (app.DormantResult, error) {
	return m.svc.RunDormant(ctx, tenants)
}

// OrgStatusEvents là các sự kiện consumer đăng ký.
var OrgStatusEvents = []string{partyport.EventOrgDeactivated, partyport.EventOrgReactivated}

// DirectoryFromEnv dựng client Keycloak Admin REST từ AG_KC_*; chưa đặt
// AG_KC_ADMIN_URL → (nil, nil): module chạy nhưng trả 503.
func DirectoryFromEnv() (port.Directory, error) {
	cfg, ok, err := keycloak.ConfigFromEnv()
	if err != nil || !ok {
		return nil, err
	}
	c, err := keycloak.New(cfg)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// MemoryDirectory là Directory trong bộ nhớ cho kiểm thử và chạy thử.
type MemoryDirectory = keycloak.Memory

// NewMemoryDirectory tạo Directory trong bộ nhớ (realm có mọi vai do module quản lý).
func NewMemoryDirectory() *MemoryDirectory { return keycloak.NewMemory() }
