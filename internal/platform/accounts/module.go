// Package accounts là module quản trị tài khoản cán bộ (pf/accounts): cầu nối
// Admin API ↔ Keycloak Admin REST, sổ thao tác chỉ ghi thêm và consumer trạng
// thái tổ chức (KN US-ADM-01..03, HTX US-ACC-01/02, US-HTXSTATUS-01..03, CGH FN-01).
//
// Module khác chỉ được import accounts/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform (gốc lắp ráp) và cmd/worker.
package accounts

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/keycloak"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	reportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "accounts_app"

// Deps là phụ thuộc của module.
type Deps struct {
	// Directory: Keycloak Admin REST. Nil → mọi route trả 503 accounts.dependency_unavailable.
	Directory port.Directory
	// Scope, Orgs: pf/party — phạm vi theo cây phân cấp và tra tổ chức (ADR-0009).
	Scope partyport.ScopeResolver
	Orgs  partyport.Orgs
	// Households, Vault, Params: tài khoản nông dân App HTX (G2) — thành viên HTX của
	// nông hộ, tra nông hộ theo SĐT qua chỉ mục mù, tham số activation.grant_lifespan_hours.
	Households partyport.Households
	Vault      client.Client
	Params     mdport.Params
}

// Module là pf/accounts đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai accounts_app.
func New(db tenant.Beginner, clk clock.Clock, d Deps) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk, Dir: d.Directory, Scope: d.Scope, Orgs: d.Orgs,
		Households: d.Households, Vault: d.Vault, Params: d.Params}}
}

// RegisterApp gắn route Application API của App HTX: tài khoản nông dân (quản lý
// HTX, cán bộ KN) và danh bạ cán bộ HTX của nông dân.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// FarmerHouseholds cho module khác: nông hộ liên kết với tài khoản nông dân
// (cropcycle: phạm vi thửa; extension: yêu cầu hỗ trợ). Tenant lấy từ context.
func (m *Module) FarmerHouseholds() port.FarmerHouseholds { return m.svc }

// FarmerAccounts cho gốc lắp ráp: tài khoản nông dân của các nông hộ (cảnh báo khẩn theo địa bàn).
func (m *Module) FarmerAccounts() port.FarmerAccounts { return m.svc }

// RegisterAdmin gắn route Admin API (MFA bắt buộc ở router).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

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
func (m *Module) MailDirectory() reportsport.MailDirectory {
	if m.svc.Dir == nil {
		return nil
	}
	return app.MailDirectory{Dir: m.svc.Dir}
}

// ReportRecipients cho pf/reports: tài khoản đã đăng ký của một HTX (email đã xác minh)
// để quản lý HTX chọn người nhận lịch báo cáo (HTX US-DASH-03). Chưa cấu hình Keycloak → nil.
func (m *Module) ReportRecipients() reportsport.RecipientDirectory {
	if m.svc.Dir == nil {
		return nil
	}
	return app.MailDirectory{Dir: m.svc.Dir}
}

// StaffContacts cho pf/extension: số điện thoại của cán bộ lúc bấm "gọi/nhắn" trong
// danh bạ trực (KN US-DIR-02). Chưa cấu hình Keycloak → nil (route liên hệ trả 503).
func (m *Module) StaffContacts() extport.StaffContacts {
	if m.svc.Dir == nil {
		return nil
	}
	return app.StaffContacts{Dir: m.svc.Dir}
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
