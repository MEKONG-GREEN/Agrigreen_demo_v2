package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ControlHeads reads committed heads, under the caller's Lock. Both ledgers are
// append-only and RLS-scoped; no synthetic counter, status or timestamp can lose
// an ABA. Every control writer already takes the same native period lock.
func ControlHeads(ctx context.Context, tx pgx.Tx, id uuid.UUID) (eventSeq, dueSeq int64, err error) {
	err = tx.QueryRow(ctx, `SELECT
 coalesce((SELECT max(seq) FROM pf_forms.period_event WHERE period_ref=$1::uuid),0),
 coalesce((SELECT max(seq) FROM pf_forms.period_due WHERE period_id=$1::uuid),0)`, id).Scan(&eventSeq, &dueSeq)
	return
}
