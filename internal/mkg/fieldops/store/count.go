package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// RollCount là một dòng sổ roll_count (G3: có lệnh gom và công đoạn).
type RollCount struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	Seq                int64
	OrderID            uuid.UUID
	PlotRef            uuid.UUID
	HTXRef             uuid.UUID
	Stage              string
	Rolls              int
	LooseKg            *mass.Kg
	DiscrepancyReason  *string
	HTXConfirmationRef *uuid.UUID
	SupersedesID       *uuid.UUID
	CorrectionReason   *string
	EventTime          time.Time
	RecordedAt         time.Time
	ActorRef           uuid.UUID
	DeviceID           *string
	IdemKey            string
	RuleVersion        string
}

// Count chiếu sang domain.Count.
func (r RollCount) Count() domain.Count {
	c := domain.Count{Rolls: r.Rolls, LooseKg: r.LooseKg}
	if r.DiscrepancyReason != nil {
		c.DiscrepancyReason = *r.DiscrepancyReason
	}
	return c
}

func kgArg(k *mass.Kg) *string {
	if k == nil {
		return nil
	}
	s := k.String()
	return &s
}

func kgScan(s *string) (*mass.Kg, error) {
	if s == nil {
		return nil, nil
	}
	k, err := mass.ParseKg(*s)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// InsertRollCount ghi sổ đếm cuộn.
func InsertRollCount(ctx context.Context, tx pgx.Tx, r *RollCount) error {
	r.RuleVersion = domain.RuleRollCount
	return tx.QueryRow(ctx, `INSERT INTO mkg_fieldops.roll_count
	    (id, tenant_id, stream_key, plot_ref, htx_ref, rolls, htx_confirmation_ref, event_time, supersedes_id, correction_reason,
	     actor_ref, device_id, idem_key, rule_version, order_id, stage, loose_kg, discrepancy_reason)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17::numeric, $18)
	  RETURNING seq, recorded_at`,
		r.ID, r.TenantID, LotStream(r.OrderID), r.PlotRef, r.HTXRef, r.Rolls, r.HTXConfirmationRef, r.EventTime.UTC(),
		r.SupersedesID, r.CorrectionReason, r.ActorRef, r.DeviceID, r.IdemKey, r.RuleVersion, r.OrderID, r.Stage,
		kgArg(r.LooseKg), r.DiscrepancyReason).Scan(&r.Seq, &r.RecordedAt)
}

const rollCols = `id, tenant_id, seq, order_id, plot_ref, htx_ref, stage, rolls, loose_kg::text, discrepancy_reason,
  htx_confirmation_ref, supersedes_id, correction_reason, event_time, recorded_at, actor_ref, device_id, idem_key, rule_version`

func scanRoll(row pgx.Row) (RollCount, error) {
	var r RollCount
	var loose *string
	err := row.Scan(&r.ID, &r.TenantID, &r.Seq, &r.OrderID, &r.PlotRef, &r.HTXRef, &r.Stage, &r.Rolls, &loose, &r.DiscrepancyReason,
		&r.HTXConfirmationRef, &r.SupersedesID, &r.CorrectionReason, &r.EventTime, &r.RecordedAt, &r.ActorRef, &r.DeviceID,
		&r.IdemKey, &r.RuleVersion)
	if err != nil {
		return r, err
	}
	r.LooseKg, err = kgScan(loose)
	return r, err
}

func oneRoll(ctx context.Context, tx pgx.Tx, where string, args ...any) (*RollCount, error) {
	r, err := scanRoll(tx.QueryRow(ctx, "SELECT "+rollCols+" FROM mkg_fieldops.roll_count WHERE order_id IS NOT NULL AND "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// RollCountByIdem tìm bản ghi đếm cuộn theo idem_key.
func RollCountByIdem(ctx context.Context, tx pgx.Tx, key string) (*RollCount, error) {
	return oneRoll(ctx, tx, "idem_key = $1", key)
}

// RollCountByID đọc một bản ghi đếm cuộn.
func RollCountByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*RollCount, error) {
	return oneRoll(ctx, tx, "id = $1", id)
}

// IDTaken cho biết id đã có trong sổ đếm cuộn (kể cả bản ghi G0 không có lệnh gom).
func IDTaken(ctx context.Context, tx pgx.Tx, id uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM mkg_fieldops.roll_count WHERE id = $1)", id).Scan(&ok)
	return ok, err
}

// Superseded cho biết bản ghi đã có bản bù.
func Superseded(ctx context.Context, tx pgx.Tx, id uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM mkg_fieldops.roll_count WHERE supersedes_id = $1)", id).Scan(&ok)
	return ok, err
}

// RollCounts liệt kê sổ đếm cuộn của một lệnh gom (cả bản ghi bị bù) theo seq.
func RollCounts(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]RollCount, error) {
	rows, err := tx.Query(ctx, "SELECT "+rollCols+" FROM mkg_fieldops.roll_count WHERE order_id = $1 ORDER BY seq", orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RollCount
	for rows.Next() {
		r, err := scanRoll(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Total là bảng chiếu roll_total_current.
type Total struct {
	OrderID           uuid.UUID
	PlotRef           uuid.UUID
	Stage             string
	HTXRef            uuid.UUID
	Rolls             int
	LooseKg           *mass.Kg
	RollCountID       uuid.UUID
	DiscrepancyReason *string
	EventTime         time.Time
	LastSeq           int64
	UpdatedAt         time.Time
}

// Count chiếu sang domain.Count.
func (t Total) Count() domain.Count {
	c := domain.Count{Rolls: t.Rolls, LooseKg: t.LooseKg}
	if t.DiscrepancyReason != nil {
		c.DiscrepancyReason = *t.DiscrepancyReason
	}
	return c
}

const totalCols = `order_id, plot_ref, stage, htx_ref, rolls, loose_kg::text, roll_count_id, discrepancy_reason, event_time, last_seq, updated_at`

func scanTotal(row pgx.Row) (Total, error) {
	var t Total
	var loose *string
	err := row.Scan(&t.OrderID, &t.PlotRef, &t.Stage, &t.HTXRef, &t.Rolls, &loose, &t.RollCountID, &t.DiscrepancyReason,
		&t.EventTime, &t.LastSeq, &t.UpdatedAt)
	if err != nil {
		return t, err
	}
	t.LooseKg, err = kgScan(loose)
	return t, err
}

// TotalFor đọc số hiệu lực của (lệnh gom, thửa, công đoạn); nil nếu chưa có.
func TotalFor(ctx context.Context, tx pgx.Tx, orderID, plot uuid.UUID, stage string) (*Total, error) {
	if stage == "" {
		return nil, nil
	}
	t, err := scanTotal(tx.QueryRow(ctx, "SELECT "+totalCols+` FROM mkg_fieldops.roll_total_current
	    WHERE order_id = $1 AND plot_ref = $2 AND stage = $3`, orderID, plot, stage))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// UpsertTotal đặt bản ghi hiệu lực của (lệnh gom, thửa, công đoạn) (R-21).
func UpsertTotal(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, r RollCount) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.roll_total_current
	    (tenant_id, order_id, plot_ref, stage, htx_ref, rolls, loose_kg, roll_count_id, discrepancy_reason, event_time, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8, $9, $10, $11)
	  ON CONFLICT (tenant_id, order_id, plot_ref, stage) DO UPDATE SET
	    rolls = EXCLUDED.rolls, loose_kg = EXCLUDED.loose_kg, roll_count_id = EXCLUDED.roll_count_id,
	    discrepancy_reason = EXCLUDED.discrepancy_reason, event_time = EXCLUDED.event_time, last_seq = EXCLUDED.last_seq,
	    updated_at = now()`,
		tenantID, r.OrderID, r.PlotRef, r.Stage, r.HTXRef, r.Rolls, kgArg(r.LooseKg), r.ID, r.DiscrepancyReason, r.EventTime.UTC(), r.Seq)
	return err
}

// Totals liệt kê số hiệu lực của một lệnh gom theo thửa, công đoạn.
func Totals(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]Total, error) {
	rows, err := tx.Query(ctx, "SELECT "+totalCols+` FROM mkg_fieldops.roll_total_current WHERE order_id = $1
	    ORDER BY plot_ref, array_position(ARRAY['bale','gather','boat_load'], stage)`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Total
	for rows.Next() {
		t, err := scanTotal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
