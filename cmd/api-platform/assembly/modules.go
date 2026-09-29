package assembly

import (
	"context"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
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
}

// List trả module theo thứ tự gắn route.
func (m Modules) List() []httpx.Module {
	return []httpx.Module{m.MasterData, m.Party, m.Geo, m.Content, m.Extension, m.Mechanization, m.MRV, m.Sync, m.Notify, m.Accounts, m.Reports, m.Waterway, m.Simulation, m.CropCycle}
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
	mrvMod := mrv.New(mrvPool, clk, mrv.Deps{Scope: pty.ScopeResolver(), Consent: pty.ConsentChecker(), Store: store})

	// pf/cropcycle (App HTX): SOP, vụ, nhật ký, sản lượng. Liên kết tài khoản nông dân ↔ nông hộ
	// (port.FarmerHouseholds) nối sau khi dựng pf/accounts (bên dưới).
	ccPool, err := pool(cropcycle.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	cc := cropcycle.New(ccPool, clk, cropcycle.Deps{Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Plots: g.Plots(), Params: md.Params(), Catalogs: md.Catalogs(),
		GpsLog: g.GpsLog()}) // HTX US-GPS-03: log GPS qua consumer outbox pf.cropcycle.gps_log
	pty.UseSeasonActivity(SeasonActivity{Stats: cc.Stats()}) // HTX US-HTXSTATUS-01 AC-1: vụ chưa xong / nhật ký chờ duyệt
	kn.UseGpsLog(g.GpsLog())                                 // HTX US-GPS-03: log GPS yêu cầu hỗ trợ (pf.extension.gps_log)

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

	snc := syncmod.New(syncPool, clk, syncmod.Deps{
		Scope: pty.ScopeResolver(), Consent: pty.ConsentChecker(), Params: md.Params(), Vault: ext.Vault,
		Store: store, CursorKey: cursorKey(ext),
		Sources: []syncport.Source{md.SyncSource(), g.SyncSource(), cnt.SyncSource(), kn.SyncSource(), nt.SyncSource(), mrvMod.SyncSource(), cc.SyncSource()},
		Sinks:   []syncport.Sink{g.SyncSink(), kn.SyncSink(), mrvMod.SyncSink(), pty.SyncSink(), cc.SyncSink()},
	})
	nt.UseSyncEntities(snc.PullEntities()) // KN US-SYNC-02: kiểm entities[] của nudge

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
		Households: pty.Households(), Vault: ext.Vault, Params: md.Params()})
	if c := acc.StaffContacts(); c != nil {
		kn.UseContacts(c) // US-DIR-02: số điện thoại cán bộ đọc từ Keycloak lúc bấm liên hệ
	}
	kn.UseFarmers(acc.FarmerHouseholds())                                                     // HTX US-SUPPORT-01: nông hộ của tài khoản nông dân
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
	})

	// pf/waterway (G2): API ghi sổ đoạn/công trình, tạo job dựng và phát hành; worker dựng đồ thị (cmd/worker).
	wwPool, err := pool(waterway.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	ww := waterway.New(wwPool, clk, waterway.Deps{Params: md.Params(), Catalogs: md.Catalogs()})

	// pf/simulation: API ghi kịch bản/yêu cầu; worker dựng ảnh chụp qua SupplySource (pf/cropcycle
	// + pf/party + pf/geo + pf/masterdata) và WaterwayRouter (pf/waterway); optimizer tính và nộp qua RegisterInternal.
	simPool, err := pool(simulation.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	sim := simulation.New(simPool, clk, simulation.Deps{Params: simulation.MasterdataParams(md.Params()),
		Supply: SupplySource{Seasons: cc.SupplyStats(), Coops: pty.CooperativeSites(), Centroids: g.CoopCentroids(), Catalogs: md.Catalogs()},
		Router: WaterwayRouter{WW: ww.Router()}})

	return Modules{MasterData: md, Party: pty, Geo: g, Content: cnt, Extension: kn, Mechanization: mech, MRV: mrvMod, Sync: snc, Notify: nt, Accounts: acc,
		Reports: rep, Waterway: ww, Simulation: sim, CropCycle: cc}, pools, nil
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
