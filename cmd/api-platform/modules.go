package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// PoolOpener mở pool cho một vai DB của cụm pf.
type PoolOpener func(ctx context.Context, role string) (*pgxpool.Pool, error)

// External là phụ thuộc ngoài cụm pf: kho định danh (R-04, qua client), kho đối tượng.
type External struct {
	Vault client.Client
	// Store: kho đối tượng. nil → thử objstore.FromEnv; không cấu hình thì tải
	// tệp đính kèm của pf/content trả 503.
	Store objstore.Store
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

	contentPool, err := pool(content.DBRole)
	if err != nil {
		return nil, pools, err
	}
	store := ext.Store
	if store == nil {
		if s, err := objstore.FromEnv(ctx); err == nil {
			store = s
		}
	}
	cnt := content.New(contentPool, clk, content.Deps{
		Scope: pty.ScopeResolver(), Catalogs: md.Catalogs(), AdminUnits: md.AdminUnits(), Store: store,
	})
	// cnt.SyncSource() nối vào pf/sync khi module đồng bộ có mặt.

	return []httpx.Module{md, pty, cnt}, pools, nil
}
