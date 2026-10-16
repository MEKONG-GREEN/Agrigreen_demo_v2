package assembly

import (
	"context"
	reportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port/farmerid"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation"
	syncmod "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// PoolOpener mở pool cho một vai đăng nhập của cụm pf (W2.6: vai đơn vị app_*).
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
	// Login: vai đăng nhập của đơn vị mở pool (W2.6, ADR-0028). Rỗng → app_platform; worker đặt app_worker.
	Login string
	// Replica (W6.3, R-DB-10 §22.1): mở pool vai đăng nhập trên bản sao chỉ đọc cụm pf cho truy vấn phân tích
	// của pf/metrics; nil → primary. ReplicaOpenerFromEnv dựng từ AG_DB_REPLICA_URL.
	Replica PoolOpener
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
	Cooperative   *cooperative.Module // dom/cooperative (W3.3): HTX, hộ, thửa, vụ — schema pf_coop, vai r_cooperative
	Metrics       *metrics.Module
	ExtData       *extdata.Module   // proxy khử định danh nguồn ngoài (ADR-0007), không vai DB
	Metering      *metering.Module  // G5 (ADR-0023/0024): sổ sử dụng Public API, điểm kiểm soát xuất, bảng kê
	APIClient     *apiclient.Module // G5 (ADR-0022): khách hàng Public API, khóa, đăng ký, gói hạn mức — Admin API
	Forms         *forms.Module     // W6.2 cap/forms: biểu theo kỳ, chốt kỳ kèm ảnh chụp đóng băng
	Price         *price.Module     // W11.71 cap/price: khung giá — danh mục nền, /v1/price/* (ADR-0049)
	HRM           *hrm.Module       // W11.21 dom/hrm: danh mục nhân sự HCTH (FN-19), /v1/hrm/* (ADR-0050)
	Contact       *contact.Module   // W11.23 cap/directory: Contact dùng chung (FN-13), /v1/contacts/*
}

// Named trả module kèm tên repo (module.yaml) theo thứ tự gắn route.
func (m Modules) Named() []NamedModule {
	return []NamedModule{
		{"pf/masterdata", m.MasterData}, {"pf/party", m.Party}, {"pf/geo", m.Geo}, {"pf/content", m.Content},
		{"pf/extension", m.Extension}, {"pf/mechanization", m.Mechanization}, {"pf/mrv", m.MRV}, {"pf/sync", m.Sync},
		{"pf/notify", m.Notify}, {"pf/accounts", m.Accounts}, {"pf/reports", m.Reports}, {"pf/waterway", m.Waterway},
		{"pf/simulation", m.Simulation}, {"pf/cropcycle", m.CropCycle}, {"pf/cooperative", m.Cooperative}, {"pf/metrics", m.Metrics},
		{"pf/extdata", m.ExtData}, {"pf/metering", m.Metering}, {"pf/apiclient", m.APIClient}, {"pf/forms", m.Forms},
		{"pf/price", m.Price}, {"pf/hrm", m.HRM}, {"pf/contact", m.Contact},
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

// Build là gốc lắp ráp DUY NHẤT: mở MỘT pool vai đăng nhập đơn vị (ADR-0028; module chạy dưới r_<module>) và nối
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
	// W2.6 (ADR-0028): MỘT pool vai đăng nhập của đơn vị (app_platform; worker: app_worker) cho mọi module —
	// mỗi module tự bọc pool bằng db.ForModule trong New (SET LOCAL ROLE r_<module>).
	login := ext.Login
	if login == "" {
		login = dbadmin.AppPlatform
	}
	unit, err := pool(login)
	if err != nil {
		return Modules{}, pools, err
	}
	store := ext.Store
	if store == nil {
		s, err := FileStoreFromEnv(ctx)
		if err != nil {
			return Modules{}, pools, err
		}
		store = s
	}

	md := masterdata.New(unit, clk)

	pty := party.New(unit, clk, party.Deps{AdminUnits: md.AdminUnits(), Vault: ext.Vault})
	wf := wireWorkflow(unit, clk, md, pty) // W6.1: cap/workflow cho duyệt tham số, cấp quyền (r_workflow, W2.6: cùng pool đơn vị)

	g := geo.New(unit, clk, geo.Deps{Scope: pty.ScopeResolver(), Params: md.Params(), AdminUnits: md.AdminUnits()})
	// pf/cooperative (W3.3, docs/design/cooperative.md): HTX, hộ, thửa, vụ ở schema pf_coop (vai r_cooperative). Module tầng
	// dưới (pf/party, pf/geo) khai interface của mình, gốc lắp ráp nối hiện thực của pf/cooperative (R-MOD-05).
	coop := cooperative.New(unit, clk, cooperative.Deps{Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Dir: pty.OrgDirectory(), CooperativeRegistrar: pty.CooperativeRegistrar(),
		Consents: pty.ConsentStates(), AdminUnits: md.AdminUnits(), AdminGeom: g.AdminGeometry(), Params: md.Params(),
		Catalogs: md.Catalogs(), GpsLog: g.GpsLog(), Households: farmerid.Households(ext.Vault), Farmers: farmerid.New(ext.Vault)}) // HTX US-GPS-03: log GPS qua consumer outbox pf.cropcycle.gps_log
	pty.UseCoopProfiles(coop.CoopProfiles()) // tạo HTX hai bước, màn HTX, danh bạ đối tác
	pty.UsePlotStats(coop.PlotStats())       // G-03: số thửa đã số hoá trong danh sách HTX
	g.UsePlots(coop.PlotGeometry())          // HTX US-GPS-01/03: khoảng cách tới lô của log GPS
	g.UseMapLayers(coop.MapLayers())         // GIS FN-01, US-INT-02: lớp HTX, lớp thửa của bản đồ gộp
	// W5.6 (ADR-0034, C-12): phía HTX của luồng song phương (/v1/bilateral/*) — hợp đồng còn proposed: chỉ bật ngoài
	// production với AG_BILATERAL_ALLOW_PROPOSED=1; production từ chối khởi động nếu đặt.
	if os.Getenv(cooperative.EnvBilateralAllowProposed) == "1" {
		if residency.Production(os.Getenv) {
			return Modules{}, pools, cooperative.ErrBilateralProposedInProduction
		}
		coop.UseBilateral(false, true)
	}

	cnt := content.New(unit, clk, content.Deps{Scope: pty.ScopeResolver(), Catalogs: md.Catalogs(), AdminUnits: md.AdminUnits(), Store: store})

	kn := extension.New(unit, clk, extension.Deps{Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Units: md.AdminUnits(), Params: md.Params(), Catalogs: md.Catalogs(),
		Plots: coop.Plots(), Coops: coop.Cooperatives()})
	md.UseStaffDirectory(kn.StaffNames()) // G-07: tên người đặt/duyệt tham số
	pty.UseActorNames(kn.StaffNames())    // KN v7.7 P2: tên người thao tác trong Log phòng ban/khu vực

	// PFPUB/D2 (ADR-0053 §8): nhãn "Chưa xác nhận" của dòng HTX trong số đã chốt cho KHTC.
	kn.UseHTXConfirmations(coop.HTXConfirmations())

	mech := mechanization.New(unit, clk, mechanization.Deps{
		Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Params: md.Params(), Catalogs: md.Catalogs(), AdminUnits: md.AdminUnits(), Vault: ext.Vault,
	})

	mrvMod := mrv.New(unit, clk, mrv.Deps{Scope: pty.ScopeResolver(), Consent: pty.ConsentChecker(), Store: store, Params: md.Params()})

	// pf/cropcycle (W3.3: chỉ còn van thu gom MKG ↔ HTX).
	cc := cropcycle.New(unit, clk, cropcycle.Deps{Scope: pty.ScopeResolver(), Plots: coop.Plots()})
	pty.UseSeasonActivity(SeasonActivity{Seasons: coop.Seasons()}) // HTX US-HTXSTATUS-01 AC-1: vụ chưa xong / nhật ký chờ duyệt
	// ADR-0020 (RB-2, Q-MKG-12): MKG là tenant riêng — bề mặt đối tác /v1/partner/* kiểm cấp quyền
	// chia sẻ của pf/party; consumer van MKG ghi vào tenant bên cấp đã cấp valve_* cho tenant phong bì.
	g.UseSharing(pty.ValveGrants())
	coop.UseSharing(pty.PartnerAccess())
	cc.UseSharing(pty.PartnerAccess(), pty.ValveGrants())
	mrvMod.UseValveGrants(pty.ValveGrants())
	kn.UseGpsLog(g.GpsLog()) // HTX US-GPS-03: log GPS yêu cầu hỗ trợ (pf.extension.gps_log)

	kn.UseCNTSCatalogs(md.CNTSCatalogs()) // W11.49: bệnh, vật nuôi của Báo cáo dịch bệnh BM-10 (danh mục CNTS theo ngày)

	// pf/notify cần sổ thiết bị của pf/sync (gắn token đẩy) và là Source của pf/sync:
	// dùng cổng thiết bị độc lập (syncmod.NewDevices) để không vòng phụ thuộc. API chỉ
	// xếp lần gửi đẩy; gửi thật ở consumer pf.notify.push của worker.
	nt := notify.New(unit, clk, notify.Deps{
		Scope: pty.ScopeResolver(), Devices: syncmod.NewDevices(unit), Staff: kn.Staff(), Units: md.AdminUnits(),
	})

	// ADR-0006: con trỏ pull ký HMAC — production bắt buộc AG_SYNC_CURSOR_KEY ≥ 32 byte.
	if err := syncmod.CheckCursorKey(cursorKey(ext), residency.Production(os.Getenv)); err != nil {
		return Modules{}, pools, err
	}
	// W6.5: cap/evidence sở hữu pf_files — route /v1/files của pf/sync gọi port.Files; chủ thể pf/cooperative.
	ev := wireEvidence(unit, clk, evidence.Deps{Store: store, Vault: ext.Vault, Scope: pty.ScopeResolver(),
		Consent: pty.ConsentChecker()}, coop.Resolve())
	// W6.2: cap/forms (/v1/forms/*), KN v1 ghi song song chốt/mở lại kỳ; W11.42: dựng trước pf/sync — biểu kỳ CNTS đồng bộ
	// ngoại tuyến (forms_task, forms_submission).
	fm, err := wireForms(unit, clk, wf, kn, md.CNTSCatalogs())
	if err != nil {
		return Modules{}, pools, err
	}
	kn.UseCNTSFacilities(md.CNTSCatalogs())  // W11.44: tên, mộc dấu cơ sở giết mổ của bảng BM-02
	kn.UseKHTCDepartments(pty.Departments()) // W11.62: phòng tham gia mặc định + tên phòng của Kỳ KHTC
	snc := syncmod.New(unit, clk, syncmod.Deps{
		Scope: pty.ScopeResolver(), Params: md.Params(), Vault: ext.Vault,
		Files: ev.Files(), CursorKey: cursorKey(ext),
		Sources: append([]syncport.Source{md.SyncSource(), cnt.SyncSource(), kn.SyncSource(), nt.SyncSource(), mrvMod.SyncSource(), cc.SyncSource(),
			kn.ReportSyncSource(), fm.SyncSource()}, coop.SyncSources()...), // KN v7.7 P7: report_task, form_layout, report_submission (Cấp Xã ngoại tuyến); W11.42 forms_task
		Sinks: append([]syncport.Sink{kn.SyncSink(), mrvMod.SyncSink(), pty.SyncSink(), cc.SyncSink(), kn.ReportSyncSink(), fm.SyncSink()},
			coop.SyncSinks()...),
	})
	nt.UseSyncEntities(snc.PullEntities())               // KN US-SYNC-02: kiểm entities[] của nudge
	snc.UseIntegrations(queueNames(), dlqDepthFromEnv()) // GIS US-SYNC-01: màn giám sát đồng bộ

	dir := ext.Accounts
	if dir == nil {
		if dir, err = accounts.DirectoryFromEnv(); err != nil {
			return Modules{}, pools, err
		}
	}
	acc := accounts.New(unit, clk, accounts.Deps{Directory: dir, Scope: pty.ScopeResolver(), Orgs: pty.Orgs(),
		Households: coop.PartyHouseholds(), Vault: ext.Vault, Params: md.Params(),
		Departments: pty.Departments(), Units: md.AdminUnits(), Names: kn.StaffNames(), // KN v7.7 P1
		Modules: pty.ModuleEnablement()}) // W1.4: vai của module đã tắt không cấp được
	// KN v7.7 P1 ↔ P2: menu theo ngữ cảnh phiên, số tài khoản theo phòng ban (mở khóa "xóa" phòng ban),
	// dòng tài khoản của bảng Ảnh hưởng, tên người thao tác trong Log (Keycloak, dự phòng danh bạ cán bộ).
	acc.UseFacilityCatalog(md.CNTSCatalogs()) // W11.41: phạm vi Cấp Xã theo cơ sở giết mổ của danh mục CNTS
	pty.UseSessionContext(acc.SessionContext())
	pty.UseDepartmentUsage(acc.DepartmentUsage())
	pty.UseOrgImpact(acc.OrgImpact())
	pty.UseActorNames(acc.ActorNames())
	pty.UseMachineCounts(mech.MachineCounts()) // KN v7.7 FN-XA-03.03/03.04: cảnh báo số máy khi vô hiệu hóa HTX
	coop.UseActorNames(acc.ActorNames())       // KN v7.7 FN-XA-03.03/03.05: tên người thao tác của Log thửa, Log hồ sơ
	if c := acc.StaffContacts(); c != nil {
		kn.UseContacts(c) // US-DIR-02: số điện thoại cán bộ đọc từ Keycloak lúc bấm liên hệ
	}
	kn.UseFarmers(acc.FarmerHouseholds())                                                         // HTX US-SUPPORT-01: nông hộ của tài khoản nông dân
	snc.UseFarmers(acc.FarmerHouseholds())                                                        // App HTX: pull của nông dân theo nông hộ liên kết
	coop.UseFarmerHouseholds(farmerHouseholds{acc.FarmerHouseholds()})                            // HTX US-SEASON-01: thửa của nông hộ mình
	g.UseFarmers(farmerHouseholds{acc.FarmerHouseholds()})                                        // HTX US-GPS-03: nông dân đọc log GPS của mình
	nt.UseAreaFarmers(AreaFarmers{Households: coop.Households(), Accounts: acc.FarmerAccounts()}) // HTX US-NEWS-03

	// pf/reports: API tạo việc xuất và cấp URL tải; worker dựng tệp (cmd/worker). Địa chỉ email tra qua pf/accounts (MailDirectory).
	rep := reports.New(unit, clk, reports.Deps{FormSeeds: []reportsport.FormSeedRegistration{extension.ReportFormSeeds()}, Builders: extension.ReportBuilders(extension.ReportBuilderDeps{Data: kn.ReportData(), Orgs: pty.Orgs(), Units: md.AdminUnits(), Catalogs: md.Catalogs(), Clock: clk}), Catalog: []reportsport.CatalogRegistration{extension.ReportCatalog()}, Policies: []reportsport.Policy{extension.ReportPolicy()},
		ResolveSession: extension.ReportSessionResolver(pty.Orgs()),
		Scope:          pty.ScopeResolver(), Orgs: pty.Orgs(), Units: md.AdminUnits(), Params: md.Params(), Catalogs: md.Catalogs(), Store: store,
		Mechanization: mech.ReportData(), CropCycle: coop.ReportData(), Directory: acc.MailDirectory(),
		Recipients:     acc.ReportRecipients(),                 // HTX US-DASH-03: người nhận = tài khoản của HTX
		Consolidations: kn.Consolidations(), Getenv: os.Getenv, // KN v7.7 P4: báo cáo Chi cục; cờ Google Sheet (tắt mặc định)
		LinkDownloadBase: os.Getenv("AG_REPORT_LINK_BASE"), // A4: liên kết tải Chi cục (route công khai /v1/public/report-links)
	})
	// KN v7.7 P4: vòng đời báo cáo — engine mẫu (P3), khu vực (P2), cán bộ phụ trách (P1), lịch sử gửi Chi cục; nối ngược
	// cho P2: bảng Ảnh hưởng (kỳ chưa chốt), dữ liệu theo phòng ban, menu "Báo cáo đột xuất", cột "biểu đang dùng".
	kn.UseReportCycle(extension.ReportCycleDeps{Forms: rep.Forms(), Regions: pty.Regions(), Assignments: acc.Assignments(), Names: acc.ActorNames(),
		Log: slog.Default()})
	kn.UseDispatchHistory(rep.DispatchHistory())
	// W11.71: cap/price (/v1/price/*), khung giá — ADR-0049; W11.72: danh bạ cán bộ phân công xã báo giá.
	pr, err := wirePrice(unit, clk, PriceOfficers{Assignments: acc.Assignments(), Names: acc.ActorNames()})
	if err != nil {
		return Modules{}, pools, err
	}
	// W11.76: khóa tuần (lịch kỳ hệ thống W11.03) lưu lúc duyệt bảng giá.
	pr.UseWeeks(PriceWeeks{Calendar: kn})
	hr, err := hrm.New(unit, clk, KNHRMAccess) // W11.21: dom/hrm (/v1/hrm/*), danh mục nhân sự — ADR-0050
	if err != nil {
		return Modules{}, pools, err
	}
	// W11.22: hồ sơ nhân sự — PII qua kho định danh (ngăn hồ sơ vault 000004), đơn vị nội bộ của pf/accounts (W11.07);
	// mặc định TẮT ở production (AG_HRM_RECORDS, chờ DPIA — Q-005).
	hr.UseRecords(ext.Vault, KNHRMUnits{Units: acc.InternalUnits()}, hrm.RecordsEnabled(os.Getenv, residency.Production(os.Getenv)))
	// W11.23: cap/directory — Contact dùng chung (FN-13). Cán bộ thuộc tỉnh chiếu từ hồ sơ nhân sự (cùng cổng AG_HRM_RECORDS),
	// PII qua thẻ danh bạ của kho định danh; tên cán bộ của danh bạ trực pf/extension đọc từ Contact trước (Q-072).
	ct, err := contact.New(unit, clk, KNContactAccess, ext.Vault)
	if err != nil {
		return Modules{}, pools, err
	}
	ct.UseStaff(KNContactStaff{Dir: hr.Directory()}, KNContactLabels{Units: acc.InternalUnits(), AdminUnits: md.AdminUnits()})
	kn.UseContactNames(ct.Names())
	wireHRMDocs(hr, ev) // W11.24: kho tài liệu nhân sự — tệp qua pf/evidence (hr_records)
	pty.UseOrgImpact(kn.OpenPeriodsImpact())
	pty.UseDepartmentUsage(kn.ReportUsage())
	// W11.02 (FN-AD-04.03): đếm bản ghi theo phòng ban ở mọi module mang department_code — mẫu/gửi Chi cục, máy móc HTX,
	// thửa (thửa cũ không phòng ban → Trồng trọt, X-12).
	pty.UseDepartmentUsage(rep.DepartmentUsage())
	pty.UseDepartmentUsage(mech.DepartmentUsage())
	pty.UseDepartmentUsage(coop.DepartmentUsage())
	pty.UseFormAvailability(rep.FormAvailability())
	md.UseFormUsage(rep.FormUsage())
	kn.UseTablePDF(rep.TablePDF()) // KN v7.7 P5: "Xuất PDF" lịch công tác
	pr.UseTablePDF(rep.TablePDF()) // W11.75: "Tải PDF" phiếu báo giá (FN-XA-03.02)
	// KN v7.7 P1 → P5: vai, phòng ban, tỉnh, xã, cấp quản lý từ ngữ cảnh phiên + sổ gán vai trò (thay suy từ vai realm).
	kn.UseKNContext(acc.ExtensionKNContext(), acc.ExtensionKNDirectory())
	// W11.43: cảnh báo CNTS — bài nộp cap/forms (SubmissionFeed), người nhận theo sổ gán vai trò (cán bộ xã, Lãnh đạo phòng,
	// người phụ trách biểu).
	kn.UseCNTSAlerts(extension.CNTSAlertDeps{Feed: fm.SubmissionFeed(), Directory: acc.ExtensionKNDirectory(), Holders: acc.RoleHolders()})
	kn.UseRoleHolders(acc.RoleHolders()) // W11.46: người theo dõi lịch công tác
	kn.UseXaScopes(acc.XaScopes())       // W11.46: viên chức Cấp Xã CNTS của lịch trực KSGM BM-03

	// pf/waterway (G2): API ghi sổ đoạn/công trình, tạo job dựng và phát hành; worker dựng đồ thị (cmd/worker).
	ww := waterway.New(unit, clk, waterway.Deps{Params: md.Params(), Catalogs: md.Catalogs()})
	g.UseWaterways(ww.Layer()) // GIS US-INT-02: lớp đường thủy của bản đồ chuẩn hóa
	// G4-0 (ADR-0020): bề mặt đối tác /v1/partner/waterway/route cho mkg/logistics (loại waterway_routes).
	ww.UseSharing(pty.PartnerAccess())

	// pf/simulation: API ghi kịch bản/yêu cầu; worker dựng ảnh chụp qua SupplySource (pf/cooperative
	// + pf/masterdata) và WaterwayRouter (pf/waterway); optimizer tính và nộp qua RegisterInternal.
	sim := simulation.New(unit, clk, simulation.Deps{Params: simulation.MasterdataParams(md.Params()),
		Supply: SupplySource{Seasons: coop.Seasons(), Coops: coop.Cooperatives(), Catalogs: md.Catalogs()},
		Router: WaterwayRouter{WW: ww.Router()}})

	// pf/metrics (G3; cap/analytics): chỉ số có lịch sử + nhật ký lớp GIS. W6.3 (R-DB-10): nguồn chỉ số và hình
	// học as-of đọc view công bố pf_<module>_pub bằng vai r_metrics — không còn nối port.Source của module chủ;
	// truy vấn phân tích chạy trên bản sao chỉ đọc khi có (AG_DB_REPLICA_URL → ext.Replica), không thì primary.
	var replica *pgxpool.Pool
	if ext.Replica != nil {
		if replica, err = ext.Replica(ctx, login); err != nil {
			return Modules{}, pools, err
		}
		pools = append(pools, replica)
	}
	mdeps := metrics.Deps{Scope: pty.ScopeResolver()}
	if replica != nil {
		mdeps.Replica = replica
	}
	met := metrics.New(unit, clk, mdeps)

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
	meter := metering.New(unit, clk, metering.Deps{Params: md.Params(), Mode: mode})

	// pf/apiclient (G5, ADR-0022): Admin API quản trị khách hàng Public API; pepper khóa từ trình quản lý
	// bí mật (không đặt → phát hành khóa trả 503). Worker xuất đọc collection_volume qua pf/cropcycle
	// (cùng luật k-ẩn danh với đọc trực tuyến); pf/metering tra gói hạn mức qua pf/apiclient.
	peppers, err := apiclient.PeppersFromEnv()
	if err != nil {
		return Modules{}, pools, err
	}
	opTenant, err := apiclient.OperatorTenantFromEnv()
	if err != nil {
		return Modules{}, pools, err
	}
	// W5.8 (R-INT-08): AG_PARTNER_SANDBOX=1 → phát hành khóa thử agks_ (môi trường thử của đối tác); production từ chối.
	sandbox, err := apiclient.SandboxFromEnv(os.Getenv)
	if err != nil {
		return Modules{}, pools, err
	}
	apic := apiclient.New(unit, clk, apiclient.Deps{Peppers: peppers, OperatorTenant: opTenant, Store: store, Sandbox: sandbox})
	apic.UseCollectionSource(cc.CollectionVolumeSource())
	// W5.7 (ADR-0033 §4): đăng ký webhook mỏng — khóa chủ dẫn xuất bí mật (AG_WEBHOOK_KEY_V<n>, không đặt → 503) và
	// chính sách đích chặn SSRF (AG_WEBHOOK_ALLOW_HOSTS; AG_WEBHOOK_ALLOW_CIDRS bị cấm ở production).
	whKeys, err := apiclient.WebhookKeysFromEnv()
	if err != nil {
		return Modules{}, pools, err
	}
	whGuard, err := apiclient.WebhookGuardFromEnv(os.Getenv, residency.Production(os.Getenv))
	if err != nil {
		return Modules{}, pools, err
	}
	apic.UseWebhooks(whKeys, whGuard)
	meter.UsePlans(apic.Plans())

	// G5 (ADR-0022): nguồn sản phẩm collection_volume cần dữ kiện thửa (pf/cooperative) + đơn vị hành chính.
	cc.UseCollectionVolume(coop.Plots(), coop.Seasons(), md.AdminUnits())
	// G5 (ADR-0024): mọi đường xuất phía nền tảng qua MỘT điểm kiểm soát — pf/metering (ext.Egress ghi đè cho kiểm thử).
	egress := ext.Egress
	if egress == nil {
		egress = meter.Checkpoint()
	}
	rep.UseEgress(egress, mode)
	mrvMod.UseEgress(egress, mode, slog.Default())
	g.UseEgress(egress, mode, slog.Default())
	cc.UseEgress(egress, mode, slog.Default())
	coop.UseEgress(egress, mode, slog.Default())
	pty.UseEgress(egress, mode, slog.Default())
	kn.UseEgress(egress, mode, slog.Default()) // A5: xuất lịch công tác — nội bộ, observe
	pr.UseEgress(egress, mode, slog.Default()) // W11.77: xuất báo cáo giá — nội bộ, observe
	hr.UseEgress(egress, mode, slog.Default()) // W11.28: tải tệp báo cáo Sở NS01–06 — nội bộ, observe

	return Modules{MasterData: md, Party: pty, Geo: g, Content: cnt, Extension: kn, Mechanization: mech, MRV: mrvMod, Sync: snc, Notify: nt, Accounts: acc,
		Reports: rep, Waterway: ww, Simulation: sim, CropCycle: cc, Cooperative: coop, Metrics: met, ExtData: ed, Metering: meter, APIClient: apic,
		Forms: fm, Price: pr, HRM: hr, Contact: ct}, pools, nil
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
