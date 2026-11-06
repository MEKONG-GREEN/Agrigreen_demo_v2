package store

import (
	"context"
	"errors"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PNPeriod215 struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Year   int       `json:"year"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Cutoff time.Time `json:"cutoff"`
}

func PNPeriods215(ctx context.Context, tx pgx.Tx, department string, year int, after uuid.UUID) ([]PNPeriod215, error) {
	rows, err := tx.Query(ctx, `SELECT p.id,p.name,p.plan_year,p.period_from,p.period_to,p.cutoff_date
 FROM pf_extension.khtc_period p WHERE p.report_type='program' AND p.plan_year=$1
 AND $2=ANY(p.departments) AND ($3::uuid='00000000-0000-0000-0000-000000000000'::uuid OR p.id<$3)
 AND NOT EXISTS(SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.period_id=p.id AND e.action='cancelled')
 ORDER BY p.id DESC LIMIT 51`, year, department, after)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PNPeriod215, error) {
		var p PNPeriod215
		err := r.Scan(&p.ID, &p.Name, &p.Year, &p.From, &p.To, &p.Cutoff)
		return p, err
	})
}

type PNStoredSnapshot215 struct {
	ID        uuid.UUID
	PeriodID  uuid.UUID
	Version   int64
	InputHash string
	Payload   []byte
}

// Read a lightweight frozen projection, never scanDraft: that would deserialize
// unrelated legacy decimal objects before this reader's numeric guards.
func PNSnapshot215(ctx context.Context, tx pgx.Tx, period, snapshot uuid.UUID) (*PNStoredSnapshot215, error) {
	var s PNStoredSnapshot215
	err := tx.QueryRow(ctx, `SELECT id,period_id,seq,input_hash,
 CASE WHEN octet_length(payload::text)<=$3 THEN payload ELSE NULL END
 FROM pf_extension.khtc_report_draft WHERE period_id=$1
 AND ($2::uuid='00000000-0000-0000-0000-000000000000'::uuid OR id=$2)
 ORDER BY seq DESC LIMIT 1`, period, snapshot, domain.PNProgressPayloadBytes215).Scan(&s.ID, &s.PeriodID, &s.Version, &s.InputHash, &s.Payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}
