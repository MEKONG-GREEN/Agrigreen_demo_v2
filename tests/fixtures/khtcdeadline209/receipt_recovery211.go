package khtcdeadline209

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CloseEffectCounts211 witnesses the real native event, snapshot and permanent
// operation anchor. It exposes neither a Forms service nor private mutable state.
func (f *Native) CloseEffectCounts211(t testing.TB, tid uuid.UUID) [3]int {
	t.Helper()
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	var out [3]int
	if err := pool.QueryRow(context.Background(), `SELECT
 (SELECT count(*) FROM pf_forms.period_event WHERE tenant_id=$1),
 (SELECT count(*) FROM pf_forms.close_snapshot WHERE tenant_id=$1),
 (SELECT count(*) FROM pf_forms.period_operation_key WHERE tenant_id=$1)`, tid).Scan(&out[0], &out[1], &out[2]); err != nil {
		t.Fatal(err)
	}
	return out
}

// MissingReceiptOperations211 models only a not-found receipt read. The actual
// permanent key, native ledger, append-only triggers and supplier CAS all remain
// intact. This is not a retention/GC execution or a port stub returning unknown.
func (f *Native) MissingReceiptOperations211(t testing.TB, policy port.PeriodOperationPolicy) port.PeriodOperations {
	t.Helper()
	module, err := forms.New(missingReceiptBeginner211{f.Pool}, f.Clock, forms.Deps{Profiles: extension.FormProfiles()})
	if err != nil {
		t.Fatal(err)
	}
	if err = module.UsePeriodOperationPolicy("kn_khtc", policy); err != nil {
		t.Fatal(err)
	}
	return module.PeriodOperations()
}

type missingReceiptBeginner211 struct{ tenant.Beginner }
type missingReceiptTx211 struct{ pgx.Tx }
type missingReceiptRow211 struct{}

func (missingReceiptRow211) Scan(...any) error { return pgx.ErrNoRows }
func (b missingReceiptBeginner211) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.Beginner.BeginTx(ctx, o)
	if err != nil {
		return nil, err
	}
	return missingReceiptTx211{tx}, nil
}
func (tx missingReceiptTx211) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if q == "SELECT content,recorded_at FROM pf_forms.period_operation_receipt WHERE tenant_id=$1 AND id=$2 ORDER BY seq LIMIT 1" {
		return missingReceiptRow211{}
	}
	return tx.Tx.QueryRow(ctx, q, args...)
}
