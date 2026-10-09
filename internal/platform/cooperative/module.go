// Package cooperative là module pf/cooperative (dom/cooperative): hồ sơ HTX, nông hộ và xã viên, thửa, vụ, nhật ký
// canh tác, SOP, khai báo sản lượng (docs/design/cooperative.md, ADR-0027, ADR-0030).
//
// W3.3: dữ liệu ở schema pf_coop (pf 000131–000133), mọi giao dịch chạy dưới vai r_cooperative (DBModule). Mã chuyển
// từ pf/cropcycle (season/), pf/geo (plot/), pf/party (member/) cùng route, consumer, thực thể đồng bộ, nguồn chỉ số;
// mặt tiền app/ hiện thực các cổng của cooperative/port trên các phần đó (thay store.Legacy + cmd/internal/coopwire
// của W3.2). Đường dẫn HTTP, mã lỗi và tên sự kiện v1 giữ nguyên (D-8, D-9).
//
// Module khác chỉ được import cooperative/port (R-02). Tệp này nối các lớp cho các gốc lắp ráp.
package cooperative

import (
	"context"
	"errors"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/app"
	bilateralapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/bilateral/app"
	bilateralhttp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/bilateral/handler"
	memberapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/app"
	memberhttp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/handler"
	plotapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/app"
	plothttp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	seasonapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/app"
	seasonhttp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/store"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/grantlookup"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/cropcyclesrc"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Name là tên repo của module (module.yaml).
const Name = "pf/cooperative"

// Sources là nguồn của mặt tiền (store.Sources); trường nil = chưa nối → port.ErrUnavailable.
type Sources = store.Sources

// Deps là port của module khác mà pf/cooperative cần. Trường nil = tính năng tương ứng trả 503 hoặc bỏ qua kiểm
// (như trước W3.3 ở module chủ cũ).
type Deps struct {
	// Scope: phạm vi TW → Tỉnh → Xã của người gọi (pf/party, ADR-0009).
	Scope partyport.ScopeResolver
	// Orgs: Nút tổ chức (HTX = Nút kind=cooperative); nếu cũng là partyport.OrgCodes thì nhập tệp theo Mã HTX được.
	Orgs partyport.Orgs
	// Dir: đọc Nút theo lô (danh bạ, thống kê, marker HTX — hồ sơ ở pf_coop, Nút ở pf_party).
	Dir partyport.OrgDirectory
	// Consents: trạng thái đồng ý trong danh sách nông hộ (G-04).
	Consents partyport.ConsentStates
	// AdminUnits: kiểm mã xã khi tạo thửa.
	AdminUnits partyport.AdminUnits
	// AdminGeom: phép tính trên ranh giới hành chính của pf/geo (thửa ngoài địa giới xã).
	AdminGeom geoport.AdminGeometry
	// Params: ngưỡng GPS, năng suất, chồng lấn thửa (pf/masterdata).
	Params params.Params
	// Catalogs: rice_variety, season, machine_type (pf/masterdata).
	Catalogs mdport.Catalogs
	// GpsLog: log GPS của pf/geo cho consumer port.GpsLogConsumer (HTX US-GPS-03).
	GpsLog geoport.GpsLog
	// Households, Farmers: kho định danh qua cổng hẹp (chủ hộ; tên nông dân của thửa KN — nhãn đã che). Bộ điều hợp
	// trên vault client: cooperative/port/farmerid (gốc lắp ráp nối) — gốc module không import kho định danh (RB-3).
	Households port.HouseholdIdentities
	Farmers    port.FarmerIdentity
}

// Module là pf/cooperative đã nối.
type Module struct {
	svc    *app.Service
	season *seasonapp.Service
	plot   *plotapp.Service
	member *memberapp.Service
	// W5.6 (ADR-0034): sổ song phương MKG ↔ HTX — phía HTX (route chỉ gắn sau UseBilateral) và cổng hẹp cho cổng
	// tích hợp (BilateralLedger).
	bilateral   *bilateralapp.Service
	bilateralOn bool
	ledger      bilateralapp.Ledger
}

// New tạo module; mọi giao dịch chạy dưới r_cooperative (db.ForModule(DBModule)).
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	db = forModule(db)
	m := &Module{svc: &app.Service{}}
	codes, _ := deps.Orgs.(partyport.OrgCodes)
	m.member = &memberapp.Service{DB: db, Clock: clk, Orgs: deps.Orgs, Codes: codes, Dir: deps.Dir, Scope: deps.Scope,
		Consents: deps.Consents, Vault: deps.Households}
	m.plot = &plotapp.Service{DB: db, Clock: clk, Scope: deps.Scope, Params: deps.Params, Units: deps.AdminUnits, Orgs: deps.Orgs,
		Households: memberships{m.member}, Markers: markers{m.member}, AdminGeom: deps.AdminGeom}
	m.plot.Vault = deps.Farmers
	m.season = &seasonapp.Service{DB: db, Clock: clk, Scope: deps.Scope, Orgs: deps.Orgs, Plots: m.plot, Params: deps.Params,
		Catalogs: deps.Catalogs, GpsLog: deps.GpsLog}
	m.bilateral = &bilateralapp.Service{DB: db, Clock: clk}
	m.ledger = bilateralapp.Ledger{DB: db, Clock: clk}
	m.svc.Store = store.Sources{
		Cooperatives: cooperatives{m.member, m.plot},
		Households:   households{m.member, m.plot},
		Plots:        m.plot,
		Seasons:      seasonapp.SeasonsPort{Svc: m.season},
		Diary:        seasonapp.DiaryPort{Svc: m.season},
		Season:       m.season,
	}
	return m
}

// NewFacade tạo mặt tiền trên nguồn có sẵn (kiểm thử hợp đồng port, không DB).
func NewFacade(src Sources) *Module { return &Module{svc: &app.Service{Store: src}} }

// UseFarmerHouseholds nối liên kết tài khoản nông dân ↔ nông hộ (pf/accounts) sau khi module đó được dựng.
func (m *Module) UseFarmerHouseholds(f port.FarmerHouseholds) { m.season.Farmers = f }

// UseSharing nối cổng cấp quyền chia sẻ của pf/party (ADR-0020): bề mặt đối tác /v1/partner/plots*,
// /v1/partner/season-harvest-dates.
func (m *Module) UseSharing(pa partyport.PartnerAccess) { m.season.Sharing, m.plot.Sharing = pa, pa }

// UseActorNames nối danh bạ tên người thao tác (khung Log của thửa KN và sổ hồ sơ KN — pf/accounts).
func (m *Module) UseActorNames(n partyport.ActorNames) { m.plot.Names, m.member.Names = n, n }

// UseEgress (G5, ADR-0024) nối điểm kiểm soát xuất (pf/metering) cho tệp KN xuất (kn_plot_file). cp nil → hành vi cũ.
func (m *Module) UseEgress(cp meter.Checkpoint, mode meter.Mode, log *slog.Logger) {
	m.plot.Egress, m.plot.EgressMode, m.plot.Log = cp, mode, log
}

// EnvBilateralAllowProposed: "1" bật phía HTX của luồng song phương với hợp đồng proposed (dev/test, W5.6). Production
// từ chối khởi động nếu đặt (ErrBilateralProposedInProduction) — bật production chờ C-12.
const EnvBilateralAllowProposed = "AG_BILATERAL_ALLOW_PROPOSED"

// ErrBilateralProposedInProduction: cờ dev/test của luồng song phương đặt ở production.
var ErrBilateralProposedInProduction = errors.New("pf/cooperative: " + EnvBilateralAllowProposed +
	" chỉ cho dev/test — production từ chối hợp đồng song phương proposed (C-12)")

// UseBilateral (W5.6, ADR-0034) bật phía HTX của luồng song phương (/v1/bilateral/*): hợp đồng proposed chỉ phục vụ
// ngoài production với allowProposed (AG_BILATERAL_ALLOW_PROPOSED=1) — production từ chối tới khi C-12 chấp nhận và
// hợp đồng approved. Gọi trước RegisterApp.
func (m *Module) UseBilateral(production, allowProposed bool) {
	m.bilateral.Production, m.bilateral.AllowProposed, m.bilateralOn = production, allowProposed && !production, true
}

// RegisterApp gắn route Application API (đường dẫn giữ nguyên từ pf/cropcycle, pf/geo, pf/party).
func (m *Module) RegisterApp(rg *gin.RouterGroup) {
	seasonhttp.Handler{Svc: m.season}.RegisterApp(rg)
	plothttp.Handler{Svc: m.plot}.RegisterApp(rg)
	memberhttp.Handler{Svc: m.member}.RegisterApp(rg)
	if m.bilateralOn {
		bilateralhttp.Handler{Svc: m.bilateral}.RegisterApp(rg)
	}
}

// Bilateral trả use case phía HTX của luồng song phương (kiểm thử, gốc lắp ráp).
func (m *Module) Bilateral() *bilateralapp.Service { return m.bilateral }

// BilateralLedger (W5.6) là cổng hẹp của sổ song phương cho cổng tích hợp (nạp theo lô bên a, feed của cặp, tổng kiểm
// ngày) — chạy ở tenant đối tác, chỉ nhánh counterparty của chính sách hai bên.
func (m *Module) BilateralLedger() apiclientport.BilateralLedger { return m.ledger }

// RegisterAdmin gắn route Admin API (nhóm đã bắt buộc MFA).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) {
	seasonhttp.Handler{Svc: m.season}.RegisterAdmin(rg)
	plothttp.Handler{Svc: m.plot}.RegisterAdmin(rg)
	memberhttp.Handler{Svc: m.member}.RegisterAdmin(rg)
}

// Cooperatives trả cổng hồ sơ HTX.
func (m *Module) Cooperatives() port.Cooperatives { return m.svc }

// Households trả cổng nông hộ.
func (m *Module) Households() port.Households { return m.svc }

// Plots trả cổng thửa.
func (m *Module) Plots() port.Plots { return m.svc }

// Seasons trả cổng vụ.
func (m *Module) Seasons() port.Seasons { return m.svc }

// Diary trả cổng nhật ký canh tác.
func (m *Module) Diary() port.Diary { return app.Diary{Svc: m.svc} }

// Resolve trả cổng phân giải chủ thể (R-DB-07, cap/evidence W6.5).
func (m *Module) Resolve() port.Resolve { return m.svc }

// ---- Cổng do module tầng dưới khai (gốc lắp ráp nối, R-MOD-05) ----

// CoopProfiles cho pf/party: hồ sơ HTX (tạo HTX hai bước, màn HTX, danh bạ đối tác).
func (m *Module) CoopProfiles() partyport.CoopProfiles { return m.member }

// CoopLocations (W5.5) cho party/port/grantlookup: vị trí hồ sơ HTX của bên cấp cho feed cooperative_directory.
func (m *Module) CoopLocations() grantlookup.CoopLocations { return m.member }

// PartnerFeedSource (W5.5) là nguồn bản ghi feed plot_directory theo bên cấp cho job dựng feed của pf/apiclient.
func (m *Module) PartnerFeedSource() apiclientport.PartnerSource { return m.plot }

// PartyHouseholds cho pf/accounts (liên kết tài khoản nông dân ↔ nông hộ): xã viên của hộ.
func (m *Module) PartyHouseholds() partyport.Households {
	return memberapp.PartyHouseholds{Svc: m.member}
}

// PlotStats cho pf/party: số thửa đã số hoá theo xã / HTX (G-02/G-03, bảng Ảnh hưởng của cây Nút).
func (m *Module) PlotStats() partyport.PlotStats { return plotStats{m.plot} }

// PlotGeometry cho pf/geo: xã, HTX, khoảng cách tới thửa cho log GPS (HTX US-GPS-01/03).
func (m *Module) PlotGeometry() geoport.PlotGeometry { return m.plot }

// MapLayers cho pf/geo: lớp HTX, lớp thửa của bản đồ gộp và bản đồ chuẩn hóa.
func (m *Module) MapLayers() geoport.MapLayers { return plothttp.MapLayers{Svc: m.plot} }

// ReportData cho pf/reports (qua gốc lắp ráp): báo cáo sản lượng HTX (HTX US-DASH-02, US-DASH-03).
func (m *Module) ReportData() cropcyclesrc.ReportData { return seasonapp.ReportData{Svc: m.season} }

// SyncSources là chiều xuống của module cho pf/sync: sop_version, season, farm_log, plot.
func (m *Module) SyncSources() []syncport.Source {
	return []syncport.Source{seasonapp.SyncSource{Svc: m.season}, plotapp.SyncSource{Svc: m.plot}}
}

// SyncSinks là chiều lên của module cho pf/sync: season_open, farm_log, farm_log_batch, harvest, plot_boundary.
func (m *Module) SyncSinks() []syncport.Sink {
	return []syncport.Sink{seasonapp.SyncSink{Svc: m.season}, plotapp.SyncSink{Svc: m.plot}}
}

// GpsLogHandler là inbox.Handler của consumer port.GpsLogConsumer (tên consumer giữ "pf.cropcycle.gps_log").
func (m *Module) GpsLogHandler() inbox.Handler { return m.season.HandleGpsLog }

// ---- bộ ghép nội bộ ----

// cooperatives ghép hồ sơ HTX (member) và tâm vùng HTX (plot) thành port.Cooperatives.
type cooperatives struct {
	member *memberapp.Service
	plot   *plotapp.Service
}

func (c cooperatives) ByUnit(ctx context.Context, units []string) ([]port.UnitCooperatives, error) {
	return c.member.CooperativesByUnit(ctx, units)
}

func (c cooperatives) Active(ctx context.Context) ([]port.CooperativeSite, error) {
	return c.member.ActiveCooperatives(ctx)
}

func (c cooperatives) Centroids(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]port.CoopCentroid, error) {
	return c.plot.Centroids(ctx, coops)
}

// households ghép xã viên (member) và nông hộ có thửa trong xã (plot) thành port.Households.
type households struct {
	member *memberapp.Service
	plot   *plotapp.Service
}

func (h households) Membership(ctx context.Context, ref uuid.UUID) (port.HouseholdMembership, error) {
	return h.member.HouseholdMembership(ctx, ref)
}

func (h households) InCommunes(ctx context.Context, communes []string) ([]uuid.UUID, error) {
	return h.plot.InCommunes(ctx, communes)
}

// memberships: plot kiểm nông hộ thuộc HTX của thửa khi gán.
type memberships struct{ member *memberapp.Service }

func (s memberships) Membership(ctx context.Context, ref uuid.UUID) (port.HouseholdMembership, error) {
	return s.member.HouseholdMembership(ctx, ref)
}

// markers: điểm vị trí hồ sơ HTX (member) cho lớp HTX của plot.
type markers struct{ member *memberapp.Service }

func (k markers) MarkersInBBox(ctx context.Context, minLon, minLat, maxLon, maxLat float64, limit int) ([]plotapp.CoopMarker, error) {
	ms, err := k.member.MarkersInBBox(ctx, minLon, minLat, maxLon, maxLat, limit)
	if err != nil {
		return nil, err
	}
	out := make([]plotapp.CoopMarker, len(ms))
	for i, x := range ms {
		out[i] = plotapp.CoopMarker(x)
	}
	return out, nil
}

// plotStats đổi DTO thống kê thửa sang partyport.PlotStat.
type plotStats struct{ plot *plotapp.Service }

func (p plotStats) DigitizedByCommune(ctx context.Context, communes []string) ([]partyport.PlotStat, error) {
	rows, err := p.plot.DigitizedByCommune(ctx, communes)
	if err != nil {
		return nil, err
	}
	out := make([]partyport.PlotStat, len(rows))
	for i, r := range rows {
		out[i] = partyport.PlotStat(r)
	}
	return out, nil
}

func (p plotStats) DigitizedByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]partyport.PlotStat, error) {
	m, err := p.plot.DigitizedByCooperative(ctx, coops)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]partyport.PlotStat, len(m))
	for k, v := range m {
		out[k] = partyport.PlotStat(v)
	}
	return out, nil
}
