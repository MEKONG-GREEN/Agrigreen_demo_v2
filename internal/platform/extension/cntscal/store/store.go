// Package store: SQL lịch trực KSGM (pf_extension.cnts_duty_roster) và lịch tiêm phòng (cnts_vaccination_batch) —
// W11.46 — trên pool vai extension_app, luôn trong giao dịch có ngữ cảnh tenant (RLS). Hai sổ chỉ ghi thêm
// (shared.apply_ledger: DB tính seq, prev_hash, row_hash, chống trùng idem_key).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/cntscal/domain"
)

// Meta là cột chung của một dòng sổ.
type Meta struct {
	ID            uuid.UUID
	Seq           int64
	EventTime     time.Time
	RecordedAt    time.Time
	ActorRef      uuid.UUID
	OnBehalfOf    *uuid.UUID
	SupportReason *string
	IdemKey       string
}

// Lock tuần tự hóa ghi theo khóa nghiệp vụ (tháng lịch trực, lứa).
func Lock(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.cntscal:' || $1::text, 0))", key)
	return err
}

func optText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---------------------------------------------------------------------------
// Lịch trực KSGM
// ---------------------------------------------------------------------------

// Roster là một phiên bản lịch trực của tháng.
type Roster struct {
	Meta
	Month  time.Time
	Source string
	Cells  []domain.Cell
	Note   string
}

const rosterCols = `id, seq, month, source, cells, coalesce(note, ''), event_time, recorded_at, actor_ref, on_behalf_of,
  support_reason, idem_key`

func scanRoster(row pgx.Row) (*Roster, error) {
	var r Roster
	var raw []byte
	err := row.Scan(&r.ID, &r.Seq, &r.Month, &r.Source, &raw, &r.Note, &r.EventTime, &r.RecordedAt, &r.ActorRef, &r.OnBehalfOf,
		&r.SupportReason, &r.IdemKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Month = time.Date(r.Month.Year(), r.Month.Month(), 1, 0, 0, 0, 0, time.UTC)
	if err := json.Unmarshal(raw, &r.Cells); err != nil {
		return nil, err
	}
	return &r, nil
}

// LatestRoster trả phiên bản mới nhất của tháng (nil = chưa lập).
func LatestRoster(ctx context.Context, tx pgx.Tx, month time.Time) (*Roster, error) {
	return scanRoster(tx.QueryRow(ctx, "SELECT "+rosterCols+` FROM pf_extension.cnts_duty_roster
	  WHERE month = $1::date ORDER BY seq DESC LIMIT 1`, month.Format(time.DateOnly)))
}

// RosterByIdem trả phiên bản đã ghi với khóa idem (gọi lặp).
func RosterByIdem(ctx context.Context, tx pgx.Tx, key string) (*Roster, error) {
	return scanRoster(tx.QueryRow(ctx, "SELECT "+rosterCols+" FROM pf_extension.cnts_duty_roster WHERE idem_key = $1 LIMIT 1", key))
}

// RosterVersion là một dòng lịch sử lưu.
type RosterVersion struct {
	Version   int64
	Source    string
	CellCount int
	Note      string
	At        time.Time
	ActorRef  uuid.UUID
}

// RosterVersions trả lịch sử lưu của tháng (mới nhất trước).
func RosterVersions(ctx context.Context, tx pgx.Tx, month time.Time, limit int) ([]RosterVersion, error) {
	rows, err := tx.Query(ctx, `SELECT seq, source, cell_count, coalesce(note, ''), event_time, actor_ref
	  FROM pf_extension.cnts_duty_roster WHERE month = $1::date ORDER BY seq DESC LIMIT $2`, month.Format(time.DateOnly), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (RosterVersion, error) {
		var v RosterVersion
		err := r.Scan(&v.Version, &v.Source, &v.CellCount, &v.Note, &v.At, &v.ActorRef)
		return v, err
	})
}

// InsertRoster ghi một phiên bản lịch trực; r.Seq do DB tính.
func InsertRoster(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *Roster) error {
	cells := r.Cells
	if cells == nil {
		cells = []domain.Cell{}
	}
	raw, err := json.Marshal(cells)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_duty_roster
	    (id, tenant_id, stream_key, month, source, cells, cell_count, note, event_time, actor_ref, on_behalf_of, support_reason,
	     idem_key, rule_version)
	  VALUES ($1, $2, $3, $4::date, $5, $6::jsonb, $7, $8, $9, $10, $11, $12, $13, $14)
	  RETURNING seq, recorded_at`,
		r.ID, tid, "duty:"+domain.MonthKey(r.Month), r.Month.Format(time.DateOnly), r.Source, string(raw), len(cells), optText(r.Note),
		r.EventTime.UTC(), r.ActorRef, r.OnBehalfOf, r.SupportReason, r.IdemKey, domain.RuleVersion).Scan(&r.Seq, &r.RecordedAt)
}

// ---------------------------------------------------------------------------
// Lịch tiêm phòng
// ---------------------------------------------------------------------------

// Batch là một phiên bản lịch tiêm của lứa.
type Batch struct {
	Meta
	Ref               uuid.UUID
	Event             string
	CommuneCode       string
	ModelType         string
	Breed             string
	Schedule          string
	ScheduleValidFrom time.Time
	HouseholdRef      *uuid.UUID
	Locality          string
	Received          time.Time
	AgeAtReceipt      int
	HeadCount         *int
	Doses             []domain.Dose
	Note              string
	StatusReason      string
	CapturedAt        *time.Time
}

const batchCols = `id, seq, batch_ref, event, commune_code, model_type_code, coalesce(breed_code, ''), schedule_code,
  schedule_valid_from, household_ref, coalesce(locality, ''), received_date, age_at_receipt, head_count, doses,
  coalesce(note, ''), coalesce(status_reason, ''), captured_at, event_time, recorded_at, actor_ref, on_behalf_of, support_reason,
  idem_key`

type doseRow struct {
	Vaccine  string  `json:"vaccine"`
	Shot     int     `json:"shot"`
	AgeDays  int     `json:"age_days"`
	Expected string  `json:"expected"`
	Actual   *string `json:"actual"`
	Note     string  `json:"note"`
}

func scanBatch(row pgx.Row) (Batch, error) {
	var b Batch
	var raw []byte
	var head *int32
	err := row.Scan(&b.ID, &b.Seq, &b.Ref, &b.Event, &b.CommuneCode, &b.ModelType, &b.Breed, &b.Schedule, &b.ScheduleValidFrom,
		&b.HouseholdRef, &b.Locality, &b.Received, &b.AgeAtReceipt, &head, &raw, &b.Note, &b.StatusReason, &b.CapturedAt,
		&b.EventTime, &b.RecordedAt, &b.ActorRef, &b.OnBehalfOf, &b.SupportReason, &b.IdemKey)
	if err != nil {
		return b, err
	}
	if head != nil {
		h := int(*head)
		b.HeadCount = &h
	}
	var ds []doseRow
	if err := json.Unmarshal(raw, &ds); err != nil {
		return b, err
	}
	for _, d := range ds {
		x, err := domain.DecodeDose(d.Vaccine, d.Shot, d.AgeDays, d.Expected, d.Actual, d.Note)
		if err != nil {
			return b, err
		}
		b.Doses = append(b.Doses, x)
	}
	return b, nil
}

// InsertBatch ghi một phiên bản lứa; b.Seq do DB tính.
func InsertBatch(ctx context.Context, tx pgx.Tx, tid uuid.UUID, b *Batch) error {
	raw, err := json.Marshal(domain.EncodeDoses(b.Doses))
	if err != nil {
		return err
	}
	var breed *string
	if b.Breed != "" {
		breed = &b.Breed
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_vaccination_batch
	    (id, tenant_id, stream_key, batch_ref, event, commune_code, model_type_code, breed_code, schedule_code, schedule_valid_from,
	     household_ref, locality, received_date, age_at_receipt, head_count, doses, note, status_reason, captured_at, event_time,
	     actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::date, $11, $12, $13::date, $14, $15, $16::jsonb, $17, $18, $19, $20, $21, $22,
	    $23, $24, $25)
	  RETURNING seq, recorded_at`,
		b.ID, tid, "vbatch:"+b.Ref.String(), b.Ref, b.Event, b.CommuneCode, b.ModelType, breed, b.Schedule,
		b.ScheduleValidFrom.Format(time.DateOnly), b.HouseholdRef, optText(b.Locality), b.Received.Format(time.DateOnly), b.AgeAtReceipt,
		b.HeadCount, string(raw), optText(b.Note), optText(b.StatusReason), b.CapturedAt, b.EventTime.UTC(), b.ActorRef, b.OnBehalfOf,
		b.SupportReason, b.IdemKey, domain.RuleVersion).Scan(&b.Seq, &b.RecordedAt)
}

// LatestBatch trả phiên bản hiện hành của lứa (nil = không có).
func LatestBatch(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Batch, error) {
	b, err := scanBatch(tx.QueryRow(ctx, "SELECT "+batchCols+` FROM pf_extension.cnts_vaccination_batch
	  WHERE batch_ref = $1 ORDER BY seq DESC LIMIT 1`, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// BatchByIdem trả phiên bản đã ghi với khóa idem.
func BatchByIdem(ctx context.Context, tx pgx.Tx, key string) (*Batch, error) {
	b, err := scanBatch(tx.QueryRow(ctx, "SELECT "+batchCols+" FROM pf_extension.cnts_vaccination_batch WHERE idem_key = $1 LIMIT 1", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// BatchVersions trả mọi phiên bản của lứa (cũ trước).
func BatchVersions(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Batch, error) {
	rows, err := tx.Query(ctx, "SELECT "+batchCols+" FROM pf_extension.cnts_vaccination_batch WHERE batch_ref = $1 ORDER BY seq", ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Batch
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// BatchFilter lọc danh sách lứa (bản hiện hành). Communes rỗng + All = mọi xã.
type BatchFilter struct {
	All       bool
	Communes  []string
	Commune   string
	Household *uuid.UUID
	From, To  *time.Time // ngày nhận giống
	Limit     int
}

// ListBatches trả bản hiện hành của các lứa theo bộ lọc (ngày nhận giống mới trước).
func ListBatches(ctx context.Context, tx pgx.Tx, f BatchFilter) ([]Batch, error) {
	var from, to *string
	if f.From != nil {
		s := f.From.Format(time.DateOnly)
		from = &s
	}
	if f.To != nil {
		s := f.To.Format(time.DateOnly)
		to = &s
	}
	communes := f.Communes
	if communes == nil {
		communes = []string{}
	}
	rows, err := tx.Query(ctx, `SELECT `+batchCols+` FROM (
	    SELECT DISTINCT ON (batch_ref) * FROM pf_extension.cnts_vaccination_batch
	    WHERE ($1 OR commune_code = ANY ($2::text[])) AND ($3 = '' OR commune_code = $3)
	      AND ($4::uuid IS NULL OR household_ref = $4) AND ($5::date IS NULL OR received_date >= $5::date)
	      AND ($6::date IS NULL OR received_date <= $6::date)
	    ORDER BY batch_ref, seq DESC) b
	  ORDER BY received_date DESC, batch_ref LIMIT $7`, f.All, communes, f.Commune, f.Household, from, to, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Batch
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
