package main

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
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

// External là phụ thuộc ngoài cụm pf: kho định danh (R-04, qua client).
type External struct {
	Vault client.Client
}

// buildModules là gốc lắp ráp: mở pool theo vai của từng module và nối các port
// giữa module (R-02: module chỉ thấy port của nhau). Thêm module mới ở đây.
func buildModules(ctx context.Context, open PoolOpener, clk clock.Clock, ext External) ([]httpx.Module, []*pgxpool.Pool, error) {
	var pools []*pgxpool.Pool
	pool := func(role string) (*pgxpool.Pool, error) {
		p, err := open(ctx, role)
		if err == nil {
			pools = append(pools, p)
		}
		return p, err
	}

	mdPool, err := pool(masterdata.DBRole)
	if err != nil {
		return nil, pools, err
	}
	md := masterdata.New(mdPool, clk)

	partyPool, err := pool(party.DBRole)
	if err != nil {
		return nil, pools, err
	}
	pty := party.New(partyPool, clk, party.Deps{AdminUnits: md.AdminUnits(), Vault: ext.Vault})

	files, err := fileStoreFromEnv(ctx)
	if err != nil {
		return nil, pools, err
	}

	mrvPool, err := pool(mrv.DBRole)
	if err != nil {
		return nil, pools, err
	}
	// Kho đối tượng cho gói xuất MRV: thiếu cấu hình → tải gói trả 503 mrv.dependency_unavailable.
	mrvMod := mrv.New(mrvPool, clk, mrv.Deps{Scope: pty.ScopeResolver(), Consent: pty.ConsentChecker(), Store: files})

	syncPool, err := pool(syncmod.DBRole)
	if err != nil {
		return nil, pools, err
	}
	snc := syncmod.New(syncPool, clk, syncmod.Deps{
		Scope: pty.ScopeResolver(), Consent: pty.ConsentChecker(), Params: md.Params(), Vault: ext.Vault,
		Store: files, CursorKey: []byte(os.Getenv("AG_SYNC_CURSOR_KEY")),
		// Source/Sink của geo (plot, plot_boundary), content (document), extension (task,
		// directive, task_update, field_report), mrv (evidence) nối ở đây khi các module có.
		Sources: nil,
		Sinks:   []syncport.Sink{mrvMod.SyncSink()},
	})

	return []httpx.Module{md, pty, mrvMod, snc}, pools, nil
}

// fileStoreFromEnv mở kho đối tượng cho tệp minh chứng nếu đã cấu hình
// (AG_S3_ENDPOINT hoặc AG_OBJSTORE_DIR); chưa cấu hình → nil, /v1/files trả 503.
func fileStoreFromEnv(ctx context.Context) (objstore.Store, error) {
	if os.Getenv("AG_S3_ENDPOINT") == "" && os.Getenv("AG_OBJSTORE_DIR") == "" {
		return nil, nil
	}
	return objstore.FromEnv(ctx)
}
