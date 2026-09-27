package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// Bí danh sang gốc lắp ráp dùng chung (cmd/api-platform/assembly).
type (
	PoolOpener = assembly.PoolOpener
	External   = assembly.External
)

// buildModules nối mọi module qua assembly.Build — cùng một đường với tests/e2e.
func buildModules(ctx context.Context, open PoolOpener, clk clock.Clock, ext External) ([]httpx.Module, []*pgxpool.Pool, error) {
	mods, pools, err := assembly.Build(ctx, open, clk, ext)
	if err != nil {
		return nil, pools, err
	}
	return mods.List(), pools, nil
}
