// Package store: truy cập schema pf_metrics (vai metrics_app). Mọi hàm chạy trong
// giao dịch đã đặt ngữ cảnh tenant (RLS).
package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// ErrDefinitionEdited: tệp định nghĩa của một phiên bản đã dùng bị sửa tại chỗ.
var ErrDefinitionEdited = errors.New("metrics: tệp định nghĩa của phiên bản đã dùng bị sửa — thêm phiên bản mới thay vì sửa")

// PinDefinition ghim băm tệp định nghĩa lần đầu tenant dùng (key, version); lần sau
// so băm.
func PinDefinition(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d domain.Def) error {
	if _, err := tx.Exec(ctx, `INSERT INTO pf_metrics.definition_pin
	    (tenant_id, metric_key, def_version, file_sha256, unit, grain, effective_from, proposed_by, approved_by)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT DO NOTHING`,
		tid, string(d.Key), d.Version, d.SHA256[:], d.Unit, string(d.Grain), domain.DateTime(d.EffectiveFrom), d.ProposedBy, d.ApprovedBy); err != nil {
		return err
	}
	var h []byte
	if err := tx.QueryRow(ctx, `SELECT file_sha256 FROM pf_metrics.definition_pin WHERE metric_key = $1 AND def_version = $2`,
		string(d.Key), d.Version).Scan(&h); err != nil {
		return err
	}
	if !bytes.Equal(h, d.SHA256[:]) {
		return fmt.Errorf("%w: %s v%d", ErrDefinitionEdited, d.Key, d.Version)
	}
	return nil
}

// ValueRow là một dòng sổ daily_value.
type ValueRow struct {
	ID               uuid.UUID
	Key              port.Key
	DefVersion       int
	ScopeUnit        string
	Dimension        string
	BusinessDate     clock.Date
	Value            decimal.Decimal
	Unit             string
	Cutoff           time.Time
	Watermark        port.Watermark
	InputsHash       []byte
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	Seq              int64
	Current          bool // dòng mới nhất của ô (không bị thay)
}

// StreamKey là chuỗi của một ô (key, đơn vị, chiều, ngày).
func StreamKey(key port.Key, unit, dim string, d clock.Date) string {
	return fmt.Sprintf("value:%s:%s:%s:%s", key, unit, dim, d)
}

const valueCols = `id, metric_key, def_version, scope_unit, dimension, business_date, value::text, unit, cutoff,
  watermark_event_id, watermark_recorded_at, inputs_hash, recorded_at, supersedes_id, correction_reason, seq`

func scanValue(row pgx.Row) (ValueRow, error) {
	var v ValueRow
	var key, val string
	var day time.Time
	var wmID *uuid.UUID
	var wmAt *time.Time
	if err := row.Scan(&v.ID, &key, &v.DefVersion, &v.ScopeUnit, &v.Dimension, &day, &val, &v.Unit, &v.Cutoff, &wmID, &wmAt,
		&v.InputsHash, &v.RecordedAt, &v.SupersedesID, &v.CorrectionReason, &v.Seq); err != nil {
		return v, err
	}
	v.Key, v.BusinessDate = port.Key(key), domain.DateOf(day)
	if wmID != nil {
		v.Watermark.LastEventID = *wmID
	}
	if wmAt != nil {
		v.Watermark.RecordedAt = wmAt.UTC()
	}
	d, err := decimal.NewFromString(val)
	v.Value = d
	return v, err
}

// LatestValue đọc dòng mới nhất của một ô; không có → nil.
func LatestValue(ctx context.Context, tx pgx.Tx, key port.Key, unit, dim string, d clock.Date) (*ValueRow, error) {
	v, err := scanValue(tx.QueryRow(ctx, "SELECT "+valueCols+` FROM pf_metrics.daily_value
	  WHERE stream_key = $1 ORDER BY seq DESC LIMIT 1`, StreamKey(key, unit, dim, d)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &v, err
}

// InsertValue ghi một dòng (seq, băm chuỗi do DB tính).
func InsertValue(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v *ValueRow) error {
	var wmID *uuid.UUID
	var wmAt *time.Time
	if v.Watermark.LastEventID != uuid.Nil {
		id := v.Watermark.LastEventID
		wmID = &id
	}
	if !v.Watermark.RecordedAt.IsZero() {
		t := v.Watermark.RecordedAt.UTC()
		wmAt = &t
	}
	sk := StreamKey(v.Key, v.ScopeUnit, v.Dimension, v.BusinessDate)
	err := tx.QueryRow(ctx, `INSERT INTO pf_metrics.daily_value
	    (id, tenant_id, stream_key, metric_key, def_version, scope_unit, dimension, business_date, value, unit, cutoff,
	     watermark_event_id, watermark_recorded_at, inputs_hash, event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10, $11, $12, $13, $14, $11, $15, $16, $17, $18, $19)
	  RETURNING seq, recorded_at`,
		v.ID, tid, sk, string(v.Key), v.DefVersion, v.ScopeUnit, v.Dimension, domain.DateTime(v.BusinessDate), v.Value.String(), v.Unit,
		v.Cutoff.UTC(), wmID, wmAt, v.InputsHash, v.SupersedesID, v.CorrectionReason, domain.SystemActor,
		sk+":"+hex.EncodeToString(v.InputsHash), domain.RuleValue).Scan(&v.Seq, &v.RecordedAt)
	return ledger.Classify(err)
}

// ValueQuery lọc chuỗi giá trị.
type ValueQuery struct {
	Key       port.Key
	Unit      string
	Dimension string
	From, To  clock.Date
	History   bool // true: cả dòng đã bị thay (lịch sử tính lại)
}

// ListValues trả giá trị theo ngày tăng dần (mỗi ngày dòng hiện hành; History → mọi dòng).
func ListValues(ctx context.Context, tx pgx.Tx, q ValueQuery) ([]ValueRow, error) {
	sql := "SELECT " + valueCols + `, seq = max(seq) OVER (PARTITION BY business_date) AS current FROM pf_metrics.daily_value
	  WHERE metric_key = $1 AND scope_unit = $2 AND dimension = $3 AND business_date BETWEEN $4 AND $5`
	if q.History {
		sql += " ORDER BY business_date, seq"
	} else {
		sql = "SELECT DISTINCT ON (business_date) " + valueCols + `, true FROM pf_metrics.daily_value
	  WHERE metric_key = $1 AND scope_unit = $2 AND dimension = $3 AND business_date BETWEEN $4 AND $5
	  ORDER BY business_date, seq DESC`
	}
	rows, err := tx.Query(ctx, sql, string(q.Key), q.Unit, q.Dimension, domain.DateTime(q.From), domain.DateTime(q.To))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ValueRow, error) {
		var v ValueRow
		var key, val string
		var day time.Time
		var wmID *uuid.UUID
		var wmAt *time.Time
		if err := r.Scan(&v.ID, &key, &v.DefVersion, &v.ScopeUnit, &v.Dimension, &day, &val, &v.Unit, &v.Cutoff, &wmID, &wmAt,
			&v.InputsHash, &v.RecordedAt, &v.SupersedesID, &v.CorrectionReason, &v.Seq, &v.Current); err != nil {
			return v, err
		}
		v.Key, v.BusinessDate = port.Key(key), domain.DateOf(day)
		if wmID != nil {
			v.Watermark.LastEventID = *wmID
		}
		if wmAt != nil {
			v.Watermark.RecordedAt = wmAt.UTC()
		}
		d, err := decimal.NewFromString(val)
		v.Value = d
		return v, err
	})
}

// Dimensions trả các chiều đã ghi của key trong phạm vi (grain season: mã vụ).
func Dimensions(ctx context.Context, tx pgx.Tx, key port.Key, unit string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT dimension FROM pf_metrics.daily_value
	  WHERE metric_key = $1 AND scope_unit = $2 ORDER BY 1`, string(key), unit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
