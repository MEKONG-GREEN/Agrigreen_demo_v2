// Package party là module bên tham gia (mục 10.5): đồng ý theo mục đích, cây tổ
// chức TW → Tỉnh → Xã, Nút HTX/THT (ADR-0009). Hồ sơ HTX, nông hộ, xã viên và sổ hồ sơ KN
// thuộc pf/cooperative từ W3.3 — nối qua port (UseCoopProfiles, OrgDirectory, ConsentStates).
//
// Module khác chỉ được import party/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform.
package party

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/shareread"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "party_app"

// Deps là port của module khác mà party cần.
type Deps struct {
	NavigationPolicy port.NavigationPolicy
	// AdminUnits: đơn vị hành chính có hiệu lực theo thời gian (pf/masterdata) —
	// kiểm mã đơn vị khi tạo/di chuyển tổ chức và tính phạm vi TTKN tỉnh.
	AdminUnits port.AdminUnits
	// Vault: kho định danh — tạo chủ hộ/người đại diện, nhãn hiển thị.
	Vault client.Client
}

// Module là pf/party đã nối.
type Module struct {
	db    tenant.Beginner // nguồn k_ref (kref.go)
	svc   *app.Service
	reg   *app.Registry
	share *app.Sharing
	kn    *app.KN
	mods  *app.Modules
}

// New tạo module với pool của vai party_app. deps tùy chọn để giữ tương thích
// với gốc lắp ráp cũ; thiếu deps thì các thao tác cần masterdata/vault trả
// 503 party.dependency_unavailable.
func New(db tenant.Beginner, clk clock.Clock, deps ...Deps) *Module {
	db = forModule(db)
	var d Deps
	if len(deps) > 0 {
		d = deps[0]
	}
	reg := &app.Registry{DB: db, Clock: clk, Units: d.AdminUnits, Vault: d.Vault}
	kn := &app.KN{DB: db, Clock: clk, Reg: reg, NavigationPolicy: d.NavigationPolicy}
	mods := &app.Modules{DB: db, Clock: clk, Names: func() port.ActorNames { return kn.Names }}
	kn.Modules = mods
	return &Module{
		db:    db,
		svc:   &app.Service{DB: db, Clock: clk},
		reg:   reg,
		share: &app.Sharing{DB: db, Clock: clk},
		kn:    kn,
		mods:  mods,
	}
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) {
	handler.Handler{Svc: m.svc}.Register(rg)
	handler.Registry{Reg: m.reg, KN: m.kn}.RegisterApp(rg)
	handler.Me{Reg: m.reg}.Register(rg)
	handler.Sharing{Svc: m.share}.RegisterApp(rg)
	handler.KN{Svc: m.kn}.RegisterApp(rg)
	handler.KN{Svc: m.kn}.RegisterKNCollect(rg)  // KN v7.7 FN-XA-03.03: /v1/kn/cooperatives* (nông hộ: pf/cooperative)
	handler.Modules{Svc: m.mods}.RegisterApp(rg) // W1.4: GET /v1/me/modules
}

// RegisterAdmin gắn route Admin API.
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) {
	handler.Registry{Reg: m.reg, KN: m.kn}.RegisterAdmin(rg)
	handler.Sharing{Svc: m.share}.RegisterAdmin(rg)
	handler.KN{Svc: m.kn}.RegisterAdmin(rg)
	handler.Modules{Svc: m.mods}.RegisterAdmin(rg) // W1.4: /v1/modules*
}

// ModuleEnablement (W1.4, R-MOD-08) cho gốc lắp ráp và pf/accounts: trạng thái bật module theo đơn vị.
func (m *Module) ModuleEnablement() port.ModuleEnablement { return m.mods }

// ModuleGate trả middleware chặn route của module (tên repo, vd "pf/geo") khi đã tắt cho tenant người
// gọi (403 module_not_enabled); nil với module luôn bật. Gốc lắp ráp gắn vào nhóm route của module.
func (m *Module) ModuleGate(module string) gin.HandlerFunc { return handler.ModuleGate(m.mods, module) }

// PartnerAccess cho module chủ dữ liệu kiểm cấp quyền chia sẻ và ghi sổ đọc của bề mặt đối tác
// /v1/partner/* (ADR-0020, qua port).
func (m *Module) PartnerAccess() port.PartnerAccess { return m.share }

// ValveGrants cho consumer van của nền tảng tìm tenant bên cấp của phong bì van (ADR-0020, qua port).
func (m *Module) ValveGrants() port.ValveGrants { return m.share }

// GrantLookup (G5, ADR-0022 mục 3) cho cmd/api-public kiểm cấp quyền dp_* của bên thứ ba (không vai OIDC).
func (m *Module) GrantLookup() port.GrantLookup { return m.share }

// ContextEpochs trả bộ đọc epoch ngữ cảnh theo đơn vị xem (W4.1; W7.2 k/access hợp nhất).
func (m *Module) ContextEpochs() port.ContextEpochs { return m.share }

// Middleware (httpx.RequestMiddleware) gắn nhật ký đọc qua cấp phép chia sẻ cho MỌI route (W4.2, ADR-0030 mục 4):
// mục đích X-Purpose → app.purpose; một sự kiện gộp pf.party.share_read.recorded mỗi yêu cầu có đọc qua cấp phép,
// đóng khi lỗi (internal/shared/shareread).
func (m *Module) Middleware() gin.HandlerFunc { return shareread.Middleware(m.share.RecordShareRead) }

// UseEgress (G5, ADR-0024) nối điểm kiểm soát xuất (pf/metering) cho kênh partner_share của
// RecordRead (bề mặt /v1/partner/*). cp nil → hành vi cũ.
func (m *Module) UseEgress(cp meter.Checkpoint, mode meter.Mode, log *slog.Logger) {
	m.share.Egress, m.share.EgressMode, m.share.Log = cp, mode, log
}

// UseFourEyes (W6.1) nối cap/workflow (pf/workflow) cho duyệt đề xuất cấp quyền chia sẻ. Nil → luồng cũ.
func (m *Module) UseFourEyes(f port.FourEyes) { m.share.Approvals = f }

// ConsentChecker cho module khác (qua port).
func (m *Module) ConsentChecker() port.ConsentChecker { return m.svc }

// Orgs cho module khác tra cứu tổ chức (qua port).
func (m *Module) Orgs() port.Orgs { return m.reg }

// OrgDirectory cho pf/cooperative đọc Nút theo lô (danh sách, thống kê, marker HTX — W3.3).
func (m *Module) OrgDirectory() port.OrgDirectory { return m.reg }

// ConsentStates cho pf/cooperative đọc trạng thái đồng ý của nông hộ trong danh sách (G-04, W3.3).
func (m *Module) ConsentStates() port.ConsentStates { return m.reg }

// UseCoopProfiles (W3.3) nối hồ sơ HTX và số nông hộ thành viên của pf/cooperative cho các màn HTX của sổ tổ
// chức (G1, HTX US-HTX-*, KN FN-XA-03.03, danh bạ đối tác) và bước 2 của tạo HTX. Thiếu → thao tác cần hồ sơ
// HTX trả 503 party.dependency_unavailable.
func (m *Module) UseCoopProfiles(p port.CoopProfiles) { m.reg.Profiles, m.share.Profiles = p, p }

// ScopeResolver cho module khác tính phạm vi theo cây phân cấp (qua port, ADR-0009).
func (m *Module) ScopeResolver() port.ScopeResolver { return m.reg }

// UsePlotStats nối thống kê thửa của pf/geo cho danh sách HTX (G-03). Gốc lắp ráp
// gọi sau khi dựng pf/geo (pf/geo cần phạm vi của pf/party nên dựng sau).
func (m *Module) UsePlotStats(ps port.PlotStats) { m.reg.Plots = ps }

// UseSeasonActivity nối số vụ chưa xong / nhật ký chờ duyệt của pf/cooperative (qua gốc
// lắp ráp) cho cảnh báo trước khi vô hiệu hóa HTX (HTX US-HTXSTATUS-01 AC-1).
func (m *Module) UseSeasonActivity(a port.SeasonActivity) { m.reg.Activity = a }

// SyncSink nhận bản ghi đồng ý thu ngoại tuyến qua pf/sync (entity consent).
func (m *Module) SyncSink() syncport.Sink { return app.ConsentSink{Svc: m.svc} }

// ---------------------------------------------------------------------------
// KN v7.7 gói P2 (phòng ban, khu vực, cây 3 cấp, menu theo cấu hình)
// ---------------------------------------------------------------------------

// Departments cho module khác đọc phòng ban (P1 ngữ cảnh phiên, P3 mẫu báo cáo — qua port).
func (m *Module) Departments() port.Departments { return m.kn }

// Regions cho P4 nhóm xã theo Khu vực và dòng tổng Khu vực/Toàn tỉnh (qua port).
func (m *Module) Regions() port.Regions { return m.kn }

// UseDepartmentUsage nối nguồn đếm tài khoản/bản ghi theo phòng ban (P1, P3/P4/P5/P7) — cột số tài
// khoản, cảnh báo khi ngưng, chặn "xóa" (FN-AD-04.03). Gọi nhiều lần để cộng nhiều nguồn.
func (m *Module) UseDepartmentUsage(u port.DepartmentUsage) { m.kn.Usage = append(m.kn.Usage, u) }

// UseOrgImpact nối nguồn đếm của bảng "Ảnh hưởng" (P1 tài khoản, P4 kỳ chưa chốt).
func (m *Module) UseOrgImpact(s port.OrgImpactSource) { m.kn.Impact = append(m.kn.Impact, s) }

// UseFormAvailability nối P3 cho menu "Báo cáo đột xuất" (FN-AD-04.06).
func (m *Module) UseFormAvailability(f port.FormAvailability) { m.kn.Forms = f }

// ContextNavigation returns the validated server-context landing resolver.
func (m *Module) ContextNavigation() port.Navigation { return m.kn.ContextNavigation }

// UseSessionContext nối ngữ cảnh phiên của P1 (X-AG-Context) cho menu.
func (m *Module) UseSessionContext(s port.SessionContext) { m.kn.Session = s }

// UseMachineCounts nối tổng số máy khai báo của HTX (pf/mechanization, KN v7.7 FN-XA-03.04) cho cảnh báo
// vô hiệu hóa HTX ở POST /v1/kn/cooperatives/{ref}/deactivate (FN-XA-03.03).
func (m *Module) UseMachineCounts(c port.CoopMachineCounts) { m.reg.Machines = c }

// UseActorNames nối danh bạ tên người thao tác cho khung Log.
func (m *Module) UseActorNames(n port.ActorNames) { m.kn.Names = n }

// CooperativeRegistrar tạo Nút HTX qua cổng nội bộ có kiểm vai cho luồng Neo D2.
func (m *Module) CooperativeRegistrar() port.CooperativeRegistrar { return m.reg }
