package assembly

import (
	"context"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port/farmerid"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics"
	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation"
	syncmod "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// PoolOpener mở pool cho một vai DB của cụm pf.
type PoolOpener func(ctx context.Context, role string) (*pgxpool.Pool, error)

// External là phụ thuộc ngoài cụm pf.
type External struct {
	// Vault: kho định danh (R-04, qua client).
	Vault client.Client
	// Store: kho đối tượng (tệp minh chứng, tệp đính kèm, gói MRV). Nil → đọc từ
	// môi trường (AG_S3_* hoặc AG_OBJSTORE_DIR); không cấu hình → các đường tải tệp trả 503.
	Store objstore.Store
	// CursorKey: khóa HMAC con trỏ đồng bộ (AG_SYNC_CURSOR_KEY, ≥ 32 byte). Nil → đọc biến môi trường.
	CursorKey []byte
	// Accounts: Keycloak Admin REST cho pf/accounts. Nil → đọc AG_KC_*; chưa cấu
	// hình → các route /v1/accounts/* trả 503 accounts.dependency_unavailable.
	Accounts accport.Directory
	// Egress (G5, ADR-0024): điểm kiểm soát xuất DUY NHẤT (pf/metering Checkpoint) cho xuất báo cáo,
	// gói MRV, ba consumer van và /v1/partner/*. Nil → hành vi cũ (chưa nối pf/metering). Chế độ đọc
	// từ AG_EGRESS_MODE (observe mặc định | enforce).
	Egress meter.Checkpoint
}

// Modules là các module đã nối — worker và kiểm thử dùng lại port của chúng.
type Modules struct {
	MasterData    *masterdata.Module
	Party         *party.Module
	Geo           *geo.Module
	Content       *content.Module
	Extension     *extension.Module
	Mechanization *mechanization.Module
	MRV           *mrv.Module
	Sync          *syncmod.Module
	Notify        *notify.Module
	Accounts      *accounts.Module
	Reports       *reports.Module
	Waterway      *waterway.Module
	Simulation    *simulation.Module
	CropCycle     *cropcycle.Module
	Metrics       *metrics.Module
	ExtData       *extdata.Module   // proxy khử định danh nguồn ngoài (ADR-0007), không vai DB
	Metering      *metering.Module  // G5 (ADR-0023/0024): sổ sử dụng Public API, điểm kiểm soát xuất, bảng kê
	APIClient     *apiclient.Module // G5 (ADR-0022): khách hàng Public API, khóa, đăng ký, gói hạn mức — Admin API
}

// Named trả module kèm tên repo (module.yaml) theo thứ tự gắn route.
func (m Modules) Named() []NamedModule {
	return []NamedModule{
		{"pf/masterdata", m.MasterData}, {"pf/party", m.Party}, {"pf/geo", m.Geo}, {"pf/content", m.Content},
		{"pf/extension", m.Extension}, {"pf/mechanization", m.Mechanization}, {"pf/mrv", m.MRV}, {"pf/sync", m.Sync},
		{"pf/notify", m.Notify}, {"pf/accounts", m.Accounts}, {"pf/reports", m.Reports}, {"pf/waterway", m.Waterway},
		{"pf/simulation", m.Simulation}, {"pf/cropcycle", m.CropCycle}, {"pf/metrics", m.Metrics}, {"pf/extdata", m.ExtData},
		{"pf/metering", m.Metering}, {"pf/apiclient", m.APIClient},
	}
}

// List trả module theo thứ tự gắn route. W1.4 (R-MOD-08): route App/Admin của module per_tenant đi qua
// cổng bật module của pf/party (403 module_not_enabled khi đã tắt cho tenant người gọi).
func (m Modules) List() []httpx.Module {
	named := m.Named()
	out := make([]httpx.Module, 0, len(named))
	for _, n := range named {
		var gate gin.HandlerFunc
		if m.Party != nil {
			gate = m.Party.ModuleGate(n.Name)
		}
		out = append(out, Gated(n.Module, gate))
	}
	return out
}

// Build là gốc lắp ráp DUY NHẤT: mở pool theo vai của từng module (R-05) và nối
// port giữa các module (R-02: module chỉ thấy port của nhau). Thêm module ở đây.
func Build(ctx context.Context, open PoolOpener, clk clock.Clock, ext External) (Modules, []*pgxpool.Pool, error) {
	var pools []*pgxpool.Pool
	pool := func(role string) (*pgxpool.Pool, error) {
		p, err := open(ctx, role)
		if err == nil {
			pools = append(pools, p)
		}
		return p, err
	}
	store := ext.Store
	if store == nil {
		s, err := FileStoreFromEnv(ctx)
		if err != nil {
			return Modules{}, pools, err
		}
		store = s
	}

	mdPool, err := pool(masterdata.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	md := masterdata.New(mdPool, clk)

	partyPool, err := pool(party.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	pty := party.New(partyPool, clk, party.Deps{AdminUnits: md.AdminUnits(), Vault: ext.Vault})

	geoPool, err := pool(geo.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	g := geo.New(geoPool, clk, geo.Deps{Scope: pty.ScopeResolver(), Params: md.Params(), AdminUnits: md.AdminUnits(), Orgs: pty.Orgs(), Households: pty.Households(),
		CoopMarkers: pty.CoopMarkers()}) // GIS US-BND-02: điểm vị trí HTX của pf/party
	pty.UsePlotStats(g.PlotStats()) // G-03: số thửa đã số hoá trong danh sách HTX

	contentPool, err := pool(content.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	cnt := content.New(contentPool, clk, content.Deps{Scope: pty.ScopeResolver(), Catalogs: md.Catalogs(), AdminUnits: md.AdminUnits(), Store: store})

	extPool, err := pool(extension.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	kn := extension.New(extPool, clk, extension.Deps{Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Units: md.AdminUnits(), Params: md.Params(), Catalogs: md.Catalogs(),
		Plots: g.PlotStats(), Coops: pty.CooperativeStats()})
	md.UseStaffDirectory(kn.StaffNames()) // G-07: tên người đặt/duyệt tham số
	pty.UseActorNames(kn.StaffNames())    // KN v7.7 P2: tên người thao tác trong Log phòng ban/khu vực

	mechPool, err := pool(mechanization.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	mech := mechanization.New(mechPool, clk, mechanization.Deps{
		Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Params: md.Params(), Catalogs: md.Catalogs(), AdminUnits: md.AdminUnits(), Vault: ext.Vault,
	})

	mrvPool, err := pool(mrv.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	mrvMod := mrv.New(mrvPool, clk, mrv.Deps{Scope: pty.ScopeResolver(), Consent: pty.ConsentChecker(), Store: store, Params: md.Params()})

	// pf/cropcycle (App HTX): SOP, vụ, nhật ký, sản lượng. Liên kết tài khoản nông dân ↔ nông hộ
	// (port.FarmerHouseholds) nối sau khi dựng pf/accounts (bên dưới).
	ccPool, err := pool(cropcycle.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	cc := cropcycle.New(ccPool, clk, cropcycle.Deps{Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Plots: g.Plots(), Params: md.Params(), Catalogs: md.Catalogs(),
		GpsLog: g.GpsLog()}) // HTX US-GPS-03: log GPS qua consumer outbox pf.cropcycle.gps_log
	pty.UseSeasonActivity(SeasonActivity{Stats: cc.Stats()}) // HTX US-HTXSTATUS-01 AC-1: vụ chưa xong / nhật ký chờ duyệt
	// ADR-0020 (RB-2, Q-MKG-12): MKG là tenant riêng — bề mặt đối tác /v1/partner/* kiểm cấp quyền
	// chia sẻ của pf/party; consumer van MKG ghi vào tenant bên cấp đã cấp valve_* cho tenant phong bì.
	g.UseSharing(pty.PartnerAccess(), pty.ValveGrants())
	cc.UseSharing(pty.PartnerAccess(), pty.ValveGrants())
	mrvMod.UseValveGrants(pty.ValveGrants())
	kn.UseGpsLog(g.GpsLog()) // HTX US-GPS-03: log GPS yêu cầu hỗ trợ (pf.extension.gps_log)

	syncPool, err := pool(syncmod.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	// pf/notify cần sổ thiết bị của pf/sync (gắn token đẩy) và là Source của pf/sync:
	// dùng cổng thiết bị độc lập (syncmod.NewDevices) để không vòng phụ thuộc. API chỉ
	// xếp lần gửi đẩy; gửi thật ở consumer pf.notify.push của worker.
	notifyPool, err := pool(notify.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	nt := notify.New(notifyPool, clk, notify.Deps{
		Scope: pty.ScopeResolver(), Devices: syncmod.NewDevices(syncPool), Staff: kn.Staff(), Units: md.AdminUnits(),
	})

	// ADR-0006: con trỏ pull ký HMAC — production bắt buộc AG_SYNC_CURSOR_KEY ≥ 32 byte.
	if err := syncmod.CheckCursorKey(cursorKey(ext), residency.Production(os.Getenv)); err != nil {
		return Modules{}, pools, err
	}
	snc := syncmod.New(syncPool, clk, syncmod.Deps{
		Scope: pty.ScopeResolver(), Consent: pty.ConsentChecker(), Params: md.Params(), Vault: ext.Vault,
		Store: store, CursorKey: cursorKey(ext),
		Sources: []syncport.Source{md.SyncSource(), g.SyncSource(), cnt.SyncSource(), kn.SyncSource(), nt.SyncSource(), mrvMod.SyncSource(), cc.SyncSource(),
			kn.ReportSyncSource()}, // KN v7.7 P7: report_task, form_layout, report_submission (Cấp Xã ngoại tuyến)
		Sinks: []syncport.Sink{g.SyncSink(), kn.SyncSink(), mrvMod.SyncSink(), pty.SyncSink(), cc.SyncSink(), kn.ReportSyncSink()},
	})
	nt.UseSyncEntities(snc.PullEntities())               // KN US-SYNC-02: kiểm entities[] của nudge
	snc.UseIntegrations(queueNames(), dlqDepthFromEnv()) // GIS US-SYNC-01: màn giám sát đồng bộ

	accPool, err := pool(accounts.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	dir := ext.Accounts
	if dir == nil {
		if dir, err = accounts.DirectoryFromEnv(); err != nil {
			return Modules{}, pools, err
		}
	}
	acc := accounts.New(accPool, clk, accounts.Deps{Directory: dir, Scope: pty.ScopeResolver(), Orgs: pty.Orgs(),
		Households: pty.Households(), Vault: ext.Vault, Params: md.Params(),
		Departments: pty.Departments(), Units: md.AdminUnits(), Names: kn.StaffNames(), // KN v7.7 P1
		Modules: pty.ModuleEnablement()}) // W1.4: vai của module đã tắt không cấp được
	// KN v7.7 P1 ↔ P2: menu theo ngữ cảnh phiên, số tài khoản theo phòng ban (mở khóa "xóa" phòng ban),
	// dòng tài khoản của bảng Ảnh hưởng, tên người thao tác trong Log (Keycloak, dự phòng danh bạ cán bộ).
	pty.UseSessionContext(acc.SessionContext())
	pty.UseDepartmentUsage(acc.DepartmentUsage())
	pty.UseOrgImpact(acc.OrgImpact())
	pty.UseActorNames(acc.ActorNames())
	pty.UseMachineCounts(mech.MachineCounts())         // KN v7.7 FN-XA-03.03/03.04: cảnh báo số máy khi vô hiệu hóa HTX
	g.UseKN(farmerid.New(ext.Vault), acc.ActorNames()) // KN v7.7 FN-XA-03.05: tên nông dân (kho định danh) + tên người thao tác của Log thửa
	if c := acc.StaffContacts(); c != nil {
		kn.UseContacts(c) // US-DIR-02: số điện thoại cán bộ đọc từ Keycloak lúc bấm liên hệ
	}
	kn.UseFarmers(acc.FarmerHouseholds())                                                     // HTX US-SUPPORT-01: nông hộ của tài khoản nông dân
	snc.UseFarmers(acc.FarmerHouseholds())                                                    // App HTX: pull của nông dân theo nông hộ liên kết
	cc.UseFarmerHouseholds(farmerHouseholds{acc.FarmerHouseholds()})                          // HTX US-SEASON-01: thửa của nông hộ mình
	g.UseFarmers(farmerHouseholds{acc.FarmerHouseholds()})                                    // HTX US-GPS-03: nông dân đọc log GPS của mình
	nt.UseAreaFarmers(AreaFarmers{Plots: g.PlotHouseholds(), Accounts: acc.FarmerAccounts()}) // HTX US-NEWS-03

	// pf/reports: API tạo việc xuất và cấp URL tải; worker dựng tệp (cmd/worker). Địa chỉ email tra qua pf/accounts (MailDirectory).
	repPool, err := pool(reports.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	rep := reports.New(repPool, clk, reports.Deps{
		Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Units: md.AdminUnits(), Params: md.Params(), Catalogs: md.Catalogs(), Store: store,
		Extension: kn.ReportData(), Mechanization: mech.ReportData(), CropCycle: cc.ReportData(), Directory: acc.MailDirectory(),
		Recipients:     acc.ReportRecipients(),                 // HTX US-DASH-03: người nhận = tài khoản của HTX
		Consolidations: kn.Consolidations(), Getenv: os.Getenv, // KN v7.7 P4: báo cáo Chi cục; cờ Google Sheet (tắt mặc định)
		LinkDownloadBase: os.Getenv("AG_REPORT_LINK_BASE"), // A4: liên kết tải Chi cục (route công khai /v1/public/report-links)
	})
	// KN v7.7 P4: vòng đời báo cáo — engine mẫu (P3), khu vực (P2), cán bộ phụ trách (P1), lịch sử gửi Chi cục; nối ngược
	// cho P2: bảng Ảnh hưởng (kỳ chưa chốt), dữ liệu theo phòng ban, menu "Báo cáo đột xuất", cột "biểu đang dùng".
	kn.UseReportCycle(extension.ReportCycleDeps{Forms: rep.Forms(), Regions: pty.Regions(), Assignments: acc.Assignments(), Names: acc.ActorNames(),
		Log: slog.Default()})
	kn.UseDispatchHistory(rep.DispatchHistory())
	pty.UseOrgImpact(kn.OpenPeriodsImpact())
	pty.UseDepartmentUsage(kn.ReportUsage())
	pty.UseFormAvailability(rep.FormAvailability())
	md.UseFormUsage(rep.FormUsage())
	kn.UseTablePDF(rep.TablePDF()) // KN v7.7 P5: "Xuất PDF" lịch công tác
	// KN v7.7 P1 → P5: vai, phòng ban, tỉnh, xã, cấp quản lý từ ngữ cảnh phiên + sổ gán vai trò (thay suy từ vai realm).
	kn.UseKNContext(acc.ExtensionKNContext(), acc.ExtensionKNDirectory())

	// pf/waterway (G2): API ghi sổ đoạn/công trình, tạo job dựng và phát hành; worker dựng đồ thị (cmd/worker).
	wwPool, err := pool(waterway.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	ww := waterway.New(wwPool, clk, waterway.Deps{Params: md.Params(), Catalogs: md.Catalogs()})
	g.UseWaterways(ww.Layer()) // GIS US-INT-02: lớp đường thủy của bản đồ chuẩn hóa
	// G4-0 (ADR-0020): bề mặt đối tác /v1/partner/waterway/route cho mkg/logistics (loại waterway_routes).
	ww.UseSharing(pty.PartnerAccess())

	// pf/simulation: API ghi kịch bản/yêu cầu; worker dựng ảnh chụp qua SupplySource (pf/cropcycle
	// + pf/party + pf/geo + pf/masterdata) và WaterwayRouter (pf/waterway); optimizer tính và nộp qua RegisterInternal.
	simPool, err := pool(simulation.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	sim := simulation.New(simPool, clk, simulation.Deps{Params: simulation.MasterdataParams(md.Params()),
		Supply: SupplySource{Seasons: cc.SupplyStats(), Coops: pty.CooperativeSites(), Centroids: g.CoopCentroids(), Catalogs: md.Catalogs()},
		Router: WaterwayRouter{WW: ww.Router()}})

	// pf/metrics (G3): chỉ số có lịch sử qua port.Source của module chủ dữ liệu (mỗi nguồn đọc
	// bằng pool vai của module đó, R-05); nhật ký lớp GIS + phát lại, hình học as-of qua pf/geo.
	metPool, err := pool(metrics.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	met := metrics.New(metPool, clk, metrics.Deps{Scope: pty.ScopeResolver(), Geometry: g.LayerGeometry(),
		Sources: map[string]metricsport.Source{
			"pf/extension": kn.MetricsSource(), "pf/mechanization": mech.MetricsSource(), "pf/cropcycle": cc.MetricsSource(), "pf/geo": g.MetricsSource(),
		}})

	// pf/extdata (ADR-0007): thời tiết/độ mặn/tile qua proxy khử định danh; danh mục nguồn
	// AG_EXTDATA_* (mặc định rỗng). Cấu hình sai (vd nguồn ngoài lãnh thổ thiếu ADR ở production) → lỗi khởi động.
	ed, err := extdata.FromEnv(os.Getenv, clk, slog.Default())
	if err != nil {
		return Modules{}, pools, err
	}

	// pf/metering (G5, ADR-0023/0024): sổ sử dụng Public API, điểm kiểm soát xuất (AG_EGRESS_MODE: observe | enforce),
	// bảng kê (Admin API; worker dựng ảnh chụp). Tham số kênh/giá đọc pf/masterdata; gói hạn mức nối UsePlans (pf/apiclient).
	mode, err := EgressModeFromEnv(os.Getenv)
	if err != nil {
		return Modules{}, pools, err
	}
	meterPool, err := pool(metering.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	meter := metering.New(meterPool, clk, metering.Deps{Params: md.Params(), Mode: mode})

	// pf/apiclient (G5, ADR-0022): Admin API quản trị khách hàng Public API; pepper khóa từ trình quản lý
	// bí mật (không đặt → phát hành khóa trả 503). Worker xuất đọc collection_volume qua pf/cropcycle
	// (cùng luật k-ẩn danh với đọc trực tuyến); pf/metering tra gói hạn mức qua pf/apiclient.
	apicPool, err := pool(apiclient.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	peppers, err := apiclient.PeppersFromEnv()
	if err != nil {
		return Modules{}, pools, err
	}
	opTenant, err := apiclient.OperatorTenantFromEnv()
	if err != nil {
		return Modules{}, pools, err
	}
	apic := apiclient.New(apicPool, clk, apiclient.Deps{Peppers: peppers, OperatorTenant: opTenant, Store: store})
	apic.UseCollectionSource(cc.CollectionVolumeSource())
	meter.UsePlans(apic.Plans())

	// G5 (ADR-0022): nguồn sản phẩm collection_volume cần dữ kiện thửa (pf/geo) + đơn vị hành chính.
	cc.UseCollectionVolume(g.PlotFacts(), md.AdminUnits())
	// G5 (ADR-0024): mọi đường xuất phía nền tảng qua MỘT điểm kiểm soát — pf/metering (ext.Egress ghi đè cho kiểm thử).
	egress := ext.Egress
	if egress == nil {
		egress = meter.Checkpoint()
	}
	rep.UseEgress(egress, mode)
	mrvMod.UseEgress(egress, mode, slog.Default())
	g.UseEgress(egress, mode, slog.Default())
	cc.UseEgress(egress, mode, slog.Default())
	pty.UseEgress(egress, mode, slog.Default())
	kn.UseEgress(egress, mode, slog.Default()) // A5: xuất lịch công tác — nội bộ, observe

	return Modules{MasterData: md, Party: pty, Geo: g, Content: cnt, Extension: kn, Mechanization: mech, MRV: mrvMod, Sync: snc, Notify: nt, Accounts: acc,
		Reports: rep, Waterway: ww, Simulation: sim, CropCycle: cc, Metrics: met, ExtData: ed, Metering: meter, APIClient: apic}, pools, nil
}

// EgressModeFromEnv đọc AG_EGRESS_MODE (ADR-0024 mục 2): rỗng → observe; giá trị lạ → lỗi khởi động.
func EgressModeFromEnv(getenv func(string) string) (meter.Mode, error) {
	return meter.ParseMode(getenv(meter.EnvEgressMode))
}

// farmerHouseholds chuyển port pf/accounts (liên kết tài khoản nông dân ↔ nông hộ)
// sang port của pf/cropcycle.
type farmerHouseholds struct{ src accport.FarmerHouseholds }

func (f farmerHouseholds) HouseholdsOf(ctx context.Context, actorRef uuid.UUID) ([]uuid.UUID, error) {
	links, err := f.src.HouseholdsForSubject(ctx, actorRef)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(links))
	for _, l := range links {
		out = append(out, l.HouseholdRef)
	}
	return out, nil
}

func cursorKey(ext External) []byte {
	if ext.CursorKey != nil {
		return ext.CursorKey
	}
	return []byte(os.Getenv("AG_SYNC_CURSOR_KEY"))
}

// FileStoreFromEnv mở kho đối tượng nếu đã cấu hình (AG_S3_ENDPOINT hoặc
// AG_OBJSTORE_DIR); chưa cấu hình → nil.
func FileStoreFromEnv(ctx context.Context) (objstore.Store, error) {
	if os.Getenv("AG_S3_ENDPOINT") == "" && os.Getenv("AG_OBJSTORE_DIR") == "" {
		return nil, nil
	}
	return objstore.FromEnv(ctx)
}

// queueNames là tên mọi consumer đã khai (Queues) cho màn giám sát đồng bộ (GIS US-SYNC-01).
func queueNames() []string {
	qs := Queues()
	out := make([]string, 0, len(qs))
	for _, q := range qs {
		out = append(out, q.Name)
	}
	return out
}

// dlqDepthFromEnv đọc độ sâu DLQ qua RabbitMQ management API (AG_RABBITMQ_MGMT_URL); chưa cấu
// hình → nil (màn giám sát hiện "không đo được").
func dlqDepthFromEnv() func(context.Context) (map[string]int64, error) {
	u := os.Getenv("AG_RABBITMQ_MGMT_URL")
	if u == "" {
		return nil
	}
	return func(ctx context.Context) (map[string]int64, error) { return inbox.DLQDepth(ctx, nil, u) }
}
