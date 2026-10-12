// Package store là SQL của pf/hrm (schema pf_hrm, pf 000147). Chạy trong giao dịch của module (vai r_hrm,
// db.ForModule); RLS p_tenant lọc tenant. Sổ chỉ ghi thêm: không có UPDATE/DELETE.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/domain"
)

// ItemRow là một phiên bản mục danh mục.
type ItemRow struct {
	Kind        string
	Code        string
	Name        string
	SortOrder   int
	Status      string
	Attrs       json.RawMessage
	Placeholder bool
	Version     int64
	RecordedAt  time.Time
	ActorRef    uuid.UUID
}

const itemCols = `kind, code, name, sort_order, status, attrs, placeholder, seq, recorded_at, actor_ref`

func scanItem(r pgx.CollectableRow) (ItemRow, error) {
	var x ItemRow
	err := r.Scan(&x.Kind, &x.Code, &x.Name, &x.SortOrder, &x.Status, &x.Attrs, &x.Placeholder, &x.Version, &x.RecordedAt, &x.ActorRef)
	return x, err
}

// Current trả phiên bản mới nhất của mỗi mã thuộc loại kind (kind rỗng = mọi loại), sắp theo kind, sort_order, mã.
func Current(ctx context.Context, tx pgx.Tx, kind string) ([]ItemRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+itemCols+` FROM (
	    SELECT DISTINCT ON (kind, code) `+itemCols+` FROM pf_hrm.catalog_item
	     WHERE ($1 = '' OR kind = $1) ORDER BY kind, code, seq DESC) v
	  ORDER BY kind, sort_order, code`, kind)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanItem)
}

// History trả mọi phiên bản của (kind, code), cũ trước.
func History(ctx context.Context, tx pgx.Tx, kind, code string) ([]ItemRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+itemCols+` FROM pf_hrm.catalog_item WHERE kind = $1 AND code = $2 ORDER BY seq`, kind, code)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanItem)
}

// Latest trả phiên bản mới nhất của (kind, code); ok = false nếu mã chưa có.
func Latest(ctx context.Context, tx pgx.Tx, kind, code string) (ItemRow, bool, error) {
	rows, err := tx.Query(ctx, `SELECT `+itemCols+` FROM pf_hrm.catalog_item WHERE kind = $1 AND code = $2 ORDER BY seq DESC LIMIT 1`,
		kind, code)
	if err != nil {
		return ItemRow{}, false, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanItem)
	if errors.Is(err, pgx.ErrNoRows) {
		return ItemRow{}, false, nil
	}
	return x, err == nil, err
}

// InsertItem ghi một phiên bản mục danh mục; seq, chuỗi băm do trigger fn_chain đặt.
func InsertItem(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, it domain.Item, attrs []byte, placeholder bool, at time.Time,
	actor uuid.UUID, idem string) (int64, error) {
	status := "retired"
	if it.Active {
		status = "active"
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_hrm.catalog_item (id, tenant_id, stream_key, kind, code, name, sort_order, status, attrs,
	    placeholder, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3::text || ':' || $4::text, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12, $13) RETURNING seq`,
		id, tid, string(it.Kind), it.Code, it.Name, it.SortOrder, status, string(attrs), placeholder, at.UTC(), actor, idem,
		domain.RuleVersion).Scan(&seq)
	return seq, err
}

// ScaleRow là một phiên bản bảng hệ số.
type ScaleRow struct {
	GradeCode    string
	ValidFrom    time.Time
	Coefficients []decimal.Decimal
	HoldMonths   int
	Note         string
	Placeholder  bool
	Version      int64
	RecordedAt   time.Time
}

const scaleCols = `grade_code, valid_from, coefficients::text[], hold_months, coalesce(note, ''), placeholder, seq, recorded_at`

func scanScale(r pgx.CollectableRow) (ScaleRow, error) {
	var x ScaleRow
	var cs []string
	var hold int16
	if err := r.Scan(&x.GradeCode, &x.ValidFrom, &cs, &hold, &x.Note, &x.Placeholder, &x.Version, &x.RecordedAt); err != nil {
		return x, err
	}
	x.HoldMonths = int(hold)
	for _, c := range cs {
		d, err := decimal.NewFromString(c)
		if err != nil {
			return x, err
		}
		x.Coefficients = append(x.Coefficients, d)
	}
	return x, nil
}

// Scales trả mọi phiên bản bảng hệ số của ngạch grade (grade rỗng = mọi ngạch), theo ngạch, valid_from, seq.
func Scales(ctx context.Context, tx pgx.Tx, grade string) ([]ScaleRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+scaleCols+` FROM pf_hrm.grade_scale WHERE ($1 = '' OR grade_code = $1)
	  ORDER BY grade_code, valid_from, seq`, grade)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanScale)
}

// ScalesAt trả bảng hệ số có hiệu lực tại ngày at của mỗi ngạch (grade rỗng = mọi ngạch): valid_from lớn nhất ≤ at,
// cùng ngày lấy seq lớn nhất (bản sửa sau thắng).
func ScalesAt(ctx context.Context, tx pgx.Tx, grade string, at time.Time) ([]ScaleRow, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (grade_code) `+scaleCols+` FROM pf_hrm.grade_scale
	  WHERE ($1 = '' OR grade_code = $1) AND valid_from <= $2::date ORDER BY grade_code, valid_from DESC, seq DESC`,
		grade, at.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanScale)
}

// InsertScale ghi một phiên bản bảng hệ số.
func InsertScale(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, s domain.GradeScale, placeholder bool, at time.Time,
	actor uuid.UUID, idem string) (int64, error) {
	cs := make([]string, len(s.Coefficients))
	for i, c := range s.Coefficients {
		cs[i] = c.StringFixed(2)
	}
	var note *string
	if s.Note != "" {
		note = &s.Note
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_hrm.grade_scale (id, tenant_id, stream_key, grade_code, valid_from, steps, coefficients,
	    hold_months, note, placeholder, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'scale:' || $3::text, $3, $4::date, $5, $6::numeric[], $7, $8, $9, $10, $11, $12, $13) RETURNING seq`,
		id, tid, s.GradeCode, s.ValidFrom.Format(time.DateOnly), len(cs), cs, s.HoldMonths, note, placeholder, at.UTC(), actor, idem,
		domain.RuleVersion).Scan(&seq)
	return seq, err
}

// RuleRow là một phiên bản tham số.
type RuleRow struct {
	Key         string
	ValidFrom   time.Time
	Payload     json.RawMessage
	Note        string
	Placeholder bool
	Version     int64
	RecordedAt  time.Time
}

const ruleCols = `key, valid_from, payload, coalesce(note, ''), placeholder, seq, recorded_at`

func scanRule(r pgx.CollectableRow) (RuleRow, error) {
	var x RuleRow
	err := r.Scan(&x.Key, &x.ValidFrom, &x.Payload, &x.Note, &x.Placeholder, &x.Version, &x.RecordedAt)
	return x, err
}

// Rules trả mọi phiên bản của tham số key, theo valid_from, seq.
func Rules(ctx context.Context, tx pgx.Tx, key string) ([]RuleRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+ruleCols+` FROM pf_hrm.rule_param WHERE key = $1 ORDER BY valid_from, seq`, key)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanRule)
}

// RuleAt trả phiên bản có hiệu lực tại ngày at; ok = false nếu chưa có.
func RuleAt(ctx context.Context, tx pgx.Tx, key string, at time.Time) (RuleRow, bool, error) {
	rows, err := tx.Query(ctx, `SELECT `+ruleCols+` FROM pf_hrm.rule_param WHERE key = $1 AND valid_from <= $2::date
	  ORDER BY valid_from DESC, seq DESC LIMIT 1`, key, at.Format(time.DateOnly))
	if err != nil {
		return RuleRow{}, false, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanRule)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuleRow{}, false, nil
	}
	return x, err == nil, err
}

// InsertRule ghi một phiên bản tham số (payload đã chuẩn hóa).
func InsertRule(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, in domain.RuleVersionInput, placeholder bool, at time.Time,
	actor uuid.UUID, idem string) (int64, error) {
	var note *string
	if in.Note != "" {
		note = &in.Note
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_hrm.rule_param (id, tenant_id, stream_key, key, valid_from, payload, note, placeholder,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'rule:' || $3::text, $3, $4::date, $5::jsonb, $6, $7, $8, $9, $10, $11) RETURNING seq`,
		id, tid, string(in.Key), in.ValidFrom.Format(time.DateOnly), string(in.Payload), note, placeholder, at.UTC(), actor, idem,
		domain.RuleVersion).Scan(&seq)
	return seq, err
}
