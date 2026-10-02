// Package party là module bên tham gia (mục 10.5): đồng ý theo mục đích, cây tổ
// chức TW → Tỉnh → Xã, HTX/THT và nông hộ (ADR-0009).
//
// Module khác chỉ được import party/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform.
package party

import (
	"github.com/gin-gonic/gin"

	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "party_app"

// Deps là port của module khác mà party cần.
type Deps struct {
	// AdminUnits: đơn vị hành chính có hiệu lực theo thời gian (pf/masterdata) —
	// kiểm mã đơn vị khi tạo/di chuyển tổ chức và tính phạm vi TTKN tỉnh.
	AdminUnits mdport.AdminUnits
	// Vault: kho định danh — tạo chủ hộ/người đại diện, nhãn hiển thị.
	Vault client.Client
}

// Module là pf/party đã nối.
type Module struct {
	svc   *app.Service
	reg   *app.Registry
	share *app.Sharing
	kn    *app.KN
}

// New tạo module với pool của vai party_app. deps tùy chọn để giữ tương thích
// với gốc lắp ráp cũ; thiếu deps thì các thao tác cần masterdata/vault trả
// 503 party.dependency_unavailable.
func New(db tenant.Beginner, clk clock.Clock, deps ...Deps) *Module {
	var d Deps
	if len(deps) > 0 {
		d = deps[0]
	}
	reg := &app.Registry{DB: db, Clock: clk, Units: d.AdminUnits, Vault: d.Vault}
	return &Module{
		svc:   &app.Service{DB: db, Clock: clk},
		reg:   reg,
		share: &app.Sharing{DB: db, Clock: clk},
		kn:    &app.KN{DB: db, Clock: clk, Reg: reg},
	}
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) {
	handler.Handler{Svc: m.svc}.Register(rg)
	handler.Registry{Reg: m.reg, KN: m.kn}.RegisterApp(rg)
	handler.Me{Reg: m.reg}.Register(rg)
	handler.Sharing{Svc: m.share}.RegisterApp(rg)
	handler.KN{Svc: m.kn}.RegisterApp(rg)
	handler.KN{Svc: m.kn}.RegisterKNCollect(rg) // KN v7.7 FN-XA-03.03: /v1/kn/cooperatives*, /v1/kn/households*
}

// RegisterAdmin gắn route Admin API.
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) {
	handler.Registry{Reg: m.reg, KN: m.kn}.RegisterAdmin(rg)
	handler.Sharing{Svc: m.share}.RegisterAdmin(rg)
	handler.KN{Svc: m.kn}.RegisterAdmin(rg)
}

// PartnerAccess cho module chủ dữ liệu kiểm cấp quyền chia sẻ và ghi sổ đọc của bề mặt đối tác
// /v1/partner/* (ADR-0020, qua port).
func (m *Module) PartnerAccess() port.PartnerAccess { return m.share }

// ValveGrants cho consumer van của nền tảng tìm tenant bên cấp của phong bì van (ADR-0020, qua port).
func (m *Module) ValveGrants() port.ValveGrants { return m.share }

// ConsentChecker cho module khác (qua port).
func (m *Module) ConsentChecker() port.ConsentChecker { return m.svc }

// Orgs cho module khác tra cứu tổ chức (qua port).
func (m *Module) Orgs() port.Orgs { return m.reg }

// Households cho module khác tra HTX hiện tại của nông hộ (geo: gán thửa).
func (m *Module) Households() port.Households { return m.reg }

// ScopeResolver cho module khác tính phạm vi theo cây phân cấp (qua port, ADR-0009).
func (m *Module) ScopeResolver() port.ScopeResolver { return m.reg }

// UsePlotStats nối thống kê thửa của pf/geo cho danh sách HTX (G-03). Gốc lắp ráp
// gọi sau khi dựng pf/geo (pf/geo cần phạm vi của pf/party nên dựng sau).
func (m *Module) UsePlotStats(ps geoport.PlotStats) { m.reg.Plots = ps }

// UseSeasonActivity nối số vụ chưa xong / nhật ký chờ duyệt của pf/cropcycle (qua gốc
// lắp ráp) cho cảnh báo trước khi vô hiệu hóa HTX (HTX US-HTXSTATUS-01 AC-1).
func (m *Module) UseSeasonActivity(a port.SeasonActivity) { m.reg.Activity = a }

// CoopMarkers cho pf/geo: điểm vị trí HTX làm marker lớp ranh giới HTX (GIS US-BND-02).
func (m *Module) CoopMarkers() geoport.CoopMarkers { return m.reg }

// CooperativeSites cho gốc lắp ráp: HTX đang hoạt động, vị trí, số hộ (nguồn cung mô phỏng).
func (m *Module) CooperativeSites() port.CooperativeSites { return m.reg }

// CooperativeStats cho module khác đếm HTX, hộ thành viên theo đơn vị (qua port, G-02).
func (m *Module) CooperativeStats() port.CooperativeStats { return m.reg }

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

// UseSessionContext nối ngữ cảnh phiên của P1 (X-AG-Context) cho menu.
func (m *Module) UseSessionContext(s port.SessionContext) { m.kn.Session = s }

// UseMachineCounts nối tổng số máy khai báo của HTX (pf/mechanization, KN v7.7 FN-XA-03.04) cho cảnh báo
// vô hiệu hóa HTX ở POST /v1/kn/cooperatives/{ref}/deactivate (FN-XA-03.03).
func (m *Module) UseMachineCounts(c port.CoopMachineCounts) { m.reg.Machines = c }

// UseActorNames nối danh bạ tên người thao tác cho khung Log.
func (m *Module) UseActorNames(n port.ActorNames) { m.kn.Names = n }
