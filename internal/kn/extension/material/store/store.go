// Package store: SQL sổ vật tư pf_extension.material_record (migration 000077) trên pool vai extension_app, luôn trong
// giao dịch có ngữ cảnh tenant (RLS). Sổ chỉ ghi thêm (shared.apply_ledger: DB tính seq, prev_hash, row_hash, chống
// trùng idem_key); "sửa" = dòng mới có supersedes_id — bản hiện hành là bản không bị dòng nào thay.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/material/domain"
)

// Record là một dòng sổ vật tư.
type Record struct {
	ID               uuid.UUID
	CommuneCode      string
	ProvinceCode     string
	DepartmentCode   string
	RecordedOn       time.Time
	MaterialCode     string
	MaterialName     string
	Quantity         decimal.Decimal
	Unit             string
	Note             *string
	Source           string
	OnBehalfOf       *uuid.UUID
	SupportReason    *string
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	IdemKey          string
	Seq              int64
	Superseded       bool // có dòng khác thay (chỉ khi đọc)
}

const cols = `m.id, m.commune_code, m.province_code, m.department_code, m.recorded_on, m.material_code, m.material_name,
  m.quantity::text, m.unit, m.note, m.source, m.on_behalf_of, m.support_reason, m.event_time, m.recorded_at, m.supersedes_id,
  m.correction_reason, m.actor_ref, m.idem_key, m.seq,
  EXISTS (SELECT 1 FROM pf_extension.material_record s WHERE s.supersedes_id = m.id)`

func scan(row pgx.Row) (Record, error) {
	var r Record
	var q string
	err := row.Scan(&r.ID, &r.CommuneCode, &r.ProvinceCode, &r.DepartmentCode, &r.RecordedOn, &r.MaterialCode, &r.MaterialName, &q,
		&r.Unit, &r.Note, &r.Source, &r.OnBehalfOf, &r.SupportReason, &r.EventTime, &r.RecordedAt, &r.SupersedesID, &r.CorrectionReason,
		&r.ActorRef, &r.IdemKey, &r.Seq, &r.Superseded)
	if err != nil {
		return r, err
	}
	r.Quantity, err = decimal.NewFromString(q)
	return r, err
}

func collect(rows pgx.Rows) ([]Record, error) {
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Insert ghi một dòng sổ (stream 'material:<xã>').
func Insert(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *Record) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.material_record (id, tenant_id, stream_key, commune_code, province_code,
	    department_code, recorded_on, material_code, material_name, quantity, unit, note, source, on_behalf_of, support_reason,
	    event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::text, $5::text, $6::text, $7::date, $8::text, $9::text, $10::numeric, $11::text,
	    $12::text, $13::text, $14::uuid, $15::text, $16::timestamptz, $17::uuid, $18::text, $19::uuid, $20::text, $21::text)
	  RETURNING seq, recorded_at`,
		r.ID, tid, "material:"+r.CommuneCode, r.CommuneCode, r.ProvinceCode, r.DepartmentCode, r.RecordedOn.Format(time.DateOnly),
		r.MaterialCode, r.MaterialName, r.Quantity.String(), r.Unit, r.Note, r.Source, r.OnBehalfOf, r.SupportReason, r.EventTime.UTC(),
		r.SupersedesID, r.CorrectionReason, r.ActorRef, r.IdemKey, domain.RuleVersion).Scan(&r.Seq, &r.RecordedAt)
}

// ByID đọc một dòng; không có → nil.
func ByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Record, error) {
	r, err := scan(tx.QueryRow(ctx, "SELECT "+cols+" FROM pf_extension.material_record m WHERE m.id = $1::uuid", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ByIdemKeys đọc các dòng theo idem_key, theo thứ tự keys (bỏ khóa chưa có). Mỗi khóa
// một dòng — dòng mới nhất nếu khóa được phát lại sau hạn 30 ngày (ADR-0012).
func ByIdemKeys(ctx context.Context, tx pgx.Tx, keys []string) ([]Record, error) {
	rows, err := tx.Query(ctx, "SELECT "+cols+` FROM pf_extension.material_record m
	  WHERE m.idem_key = ANY($1::text[])
	    AND m.id IN (SELECT DISTINCT ON (idem_key) id FROM pf_extension.material_record
	                 WHERE idem_key = ANY($1::text[]) ORDER BY idem_key, recorded_at DESC, id DESC)
	  ORDER BY array_position($1::text[], m.idem_key)`, keys)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// Lock tuần tự hóa việc sửa một dòng (hai bản sửa đồng thời của cùng dòng → một bị 409).
func Lock(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.material:' || $1::text, 0))", id.String())
	return err
}

// Filter là điều kiện đọc bản hiện hành.
type Filter struct {
	All          bool
	Units        []string // xã/tỉnh trong phạm vi
	Commune      string
	Dept         string // rỗng = mọi phòng ban (Admin)
	From, To     *time.Time
	MaterialCode string
	// Con trỏ (recorded_on, id) của dòng cuối trang trước; nil = trang đầu.
	AfterOn *time.Time
	AfterID uuid.UUID
	Limit   int
}

func dateArg(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
}

func unitsArg(u []string) []string {
	if u == nil {
		return []string{}
	}
	return u
}

const currentWhere = `($1::boolean OR m.commune_code = ANY($2::text[]))
    AND ($3::text = '' OR m.commune_code = $3::text)
    AND ($4::text = '' OR m.department_code = $4::text)
    AND ($5::date IS NULL OR m.recorded_on >= $5::date)
    AND ($6::date IS NULL OR m.recorded_on <= $6::date)
    AND NOT EXISTS (SELECT 1 FROM pf_extension.material_record s WHERE s.supersedes_id = m.id)`

// ListCurrent trả bản hiện hành, mới nhất trước (recorded_on, id giảm dần).
func ListCurrent(ctx context.Context, tx pgx.Tx, f Filter) ([]Record, error) {
	rows, err := tx.Query(ctx, "SELECT "+cols+` FROM pf_extension.material_record m
	  WHERE `+currentWhere+`
	    AND ($7::text = '' OR m.material_code = $7::text)
	    AND ($8::date IS NULL OR (m.recorded_on, m.id) < ($8::date, $9::uuid))
	  ORDER BY m.recorded_on DESC, m.id DESC LIMIT $10::int`,
		f.All, unitsArg(f.Units), f.Commune, f.Dept, dateArg(f.From), dateArg(f.To), f.MaterialCode, dateArg(f.AfterOn), f.AfterID, f.Limit)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// SummaryRow là tổng bản hiện hành theo xã × vật tư × ĐVT.
type SummaryRow struct {
	CommuneCode  string
	MaterialCode string
	MaterialName string
	Unit         string
	Quantity     decimal.Decimal
	Records      int
}

// Summary tổng hợp bản hiện hành theo xã × vật tư (AC: Cấp Tỉnh xem tổng hợp vật tư theo xã).
func Summary(ctx context.Context, tx pgx.Tx, f Filter) ([]SummaryRow, error) {
	rows, err := tx.Query(ctx, `SELECT m.commune_code, m.material_code, max(m.material_name), m.unit, sum(m.quantity)::text, count(*)
	  FROM pf_extension.material_record m
	  WHERE `+currentWhere+`
	  GROUP BY m.commune_code, m.material_code, m.unit
	  ORDER BY m.commune_code, m.material_code, m.unit`,
		f.All, unitsArg(f.Units), f.Commune, f.Dept, dateArg(f.From), dateArg(f.To))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SummaryRow{}
	for rows.Next() {
		var s SummaryRow
		var q string
		if err := rows.Scan(&s.CommuneCode, &s.MaterialCode, &s.MaterialName, &s.Unit, &q, &s.Records); err != nil {
			return nil, err
		}
		if s.Quantity, err = decimal.NewFromString(q); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Chain trả mọi dòng của chuỗi sửa chứa id (gốc → các bản bù), theo thứ tự ghi.
func Chain(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]Record, error) {
	rows, err := tx.Query(ctx, `WITH RECURSIVE up AS (
	    SELECT r.id, r.supersedes_id FROM pf_extension.material_record r WHERE r.id = $1::uuid
	    UNION SELECT r.id, r.supersedes_id FROM pf_extension.material_record r JOIN up ON r.id = up.supersedes_id
	  ), root AS (SELECT id FROM up WHERE supersedes_id IS NULL),
	  down AS (
	    SELECT r.id FROM pf_extension.material_record r JOIN root ON r.id = root.id
	    UNION SELECT r.id FROM pf_extension.material_record r JOIN down ON r.supersedes_id = down.id
	  )
	  SELECT `+cols+` FROM pf_extension.material_record m WHERE m.id IN (SELECT id FROM down)
	  ORDER BY m.recorded_at, m.seq`, id)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}
