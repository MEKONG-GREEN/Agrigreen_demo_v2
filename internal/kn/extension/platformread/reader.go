// Package platformread reads published geographic and organizational metadata for KN.
package platformread

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/jackc/pgx/v5"
)

type reader struct{ db tenant.Beginner }

func newReader(pool tenant.Beginner) reader {
	return reader{db: db.ForModule(pool, db.Module("pf/extension"))}
}
func (r reader) read(ctx context.Context, fn func(pgx.Tx) error) error {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return err
	}
	return tenant.WithTenantTxOptions(ctx, r.db, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, fn)
}
