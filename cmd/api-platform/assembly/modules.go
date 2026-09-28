package assembly

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	syncmod "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
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
}

// List trả module theo thứ tự gắn route.
func (m Modules) List() []httpx.Module {
	return []httpx.Module{m.MasterData, m.Party, m.Geo, m.Content, m.Extension, m.Mechanization, m.MRV, m.Sync, m.Notify, m.Accounts}
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
	g := geo.New(geoPool, clk, geo.Deps{Scope: pty.ScopeResolver(), Params: md.Params(), AdminUnits: md.AdminUnits(), Orgs: pty.Orgs()})

	contentPool, err := pool(content.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	cnt := content.New(contentPool, clk, content.Deps{Scope: pty.ScopeResolver(), Catalogs: md.Catalogs(), AdminUnits: md.AdminUnits(), Store: store})

	extPool, err := pool(extension.DBRole)
	if err != nil {
		return Modules{}, pools, err
	}
	kn := extension.New(extPool, clk, extension.Deps{Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Units: md.AdminUnits(), Params: md.Params(), Catalogs: md.Catalogs()})

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
		Sources: []syncport.Source{g.SyncSource(), cnt.SyncSource(), kn.SyncSource(), nt.SyncSource()},
		Sinks:   []syncport.Sink{g.SyncSink(), kn.SyncSink(), mrvMod.SyncSink(), pty.SyncSink()},
	})

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
	acc := accounts.New(accPool, clk, accounts.Deps{Directory: dir, Scope: pty.ScopeResolver(), Orgs: pty.Orgs()})

	return Modules{MasterData: md, Party: pty, Geo: g, Content: cnt, Extension: kn, Mechanization: mech, MRV: mrvMod, Sync: snc, Notify: nt, Accounts: acc}, pools, nil
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
