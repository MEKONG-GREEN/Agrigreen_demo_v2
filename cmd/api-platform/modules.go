package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
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

	extPool, err := pool(extension.DBRole)
	if err != nil {
		return nil, pools, err
	}
	kn := extension.New(extPool, clk, extension.Deps{
		Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Units: md.AdminUnits(), Params: md.Params(), Catalogs: md.Catalogs(),
	})

	return []httpx.Module{md, pty, kn}, pools, nil
}
