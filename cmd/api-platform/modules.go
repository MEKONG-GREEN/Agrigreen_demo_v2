package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// PoolOpener mở pool cho một vai DB của cụm pf.
type PoolOpener func(ctx context.Context, role string) (*pgxpool.Pool, error)

// buildModules là gốc lắp ráp: mở pool theo vai của từng module và nối các port
// giữa module (R-02: module chỉ thấy port của nhau). Thêm module mới ở đây.
func buildModules(ctx context.Context, open PoolOpener, clk clock.Clock) ([]httpx.Module, []*pgxpool.Pool, error) {
	var pools []*pgxpool.Pool
	pool := func(role string) (*pgxpool.Pool, error) {
		p, err := open(ctx, role)
		if err == nil {
			pools = append(pools, p)
		}
		return p, err
	}

	partyPool, err := pool(party.DBRole)
	if err != nil {
		return nil, pools, err
	}
	partyMod := party.New(partyPool, clk)

	return []httpx.Module{partyMod}, pools, nil
}
