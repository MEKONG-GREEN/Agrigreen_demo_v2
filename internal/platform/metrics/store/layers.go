package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// LayerRow là một dòng nhật ký lớp kèm cột nguồn.
type LayerRow struct {
	port.LayerChange
	SourceModule string
	Seq          int64
}

const layerCols = `id, layer, feature_ref, op, before_attrs, after_attrs, source_event_id, source_type, source_module,
  event_time, recorded_at, seq`

func scanLayer(r pgx.Row) (LayerRow, error) {
	var x LayerRow
	var op string
	var before, after []byte
	err := r.Scan(&x.ID, &x.Layer, &x.FeatureRef, &op, &before, &after, &x.SourceEventID, &x.SourceType, &x.SourceModule,
		&x.EventTime, &x.RecordedAt, &x.Seq)
	x.Op = port.LayerOp(op)
	if before != nil {
		x.Before = json.RawMessage(before)
	}
	if after != nil {
		x.After = json.RawMessage(after)
	}
	x.EventTime, x.RecordedAt = x.EventTime.UTC(), x.RecordedAt.UTC()
	return x, err
}

// IdemKey là khóa idempotency của một dòng nhật ký (một sự kiện nguồn có thể đổi
// nhiều đối tượng, mỗi đối tượng một dòng).
func IdemKey(c port.LayerChange) string {
	return "layer:" + c.SourceEventID.String() + ":" + c.Layer + ":" + c.FeatureRef.String()
}

// LayerByIdem tìm dòng đã ghi cho cùng sự kiện nguồn; không có → nil.
func LayerByIdem(ctx context.Context, tx pgx.Tx, c port.LayerChange) (*LayerRow, error) {
	x, err := scanLayer(tx.QueryRow(ctx, "SELECT "+layerCols+" FROM pf_metrics.layer_event WHERE idem_key = $1 LIMIT 1", IdemKey(c)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &x, err
}

// LatestFeature đọc dòng mới nhất của một đối tượng; không có → nil.
func LatestFeature(ctx context.Context, tx pgx.Tx, layer string, ref uuid.UUID) (*LayerRow, error) {
	x, err := scanLayer(tx.QueryRow(ctx, "SELECT "+layerCols+` FROM pf_metrics.layer_event
	  WHERE layer = $1 AND feature_ref = $2 ORDER BY recorded_at DESC, id DESC LIMIT 1`, layer, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &x, err
}

func jsonArg(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// InsertLayer ghi một dòng nhật ký (RecordedAt, Seq đọc lại từ DB).
func InsertLayer(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x *LayerRow) error {
	err := tx.QueryRow(ctx, `INSERT INTO pf_metrics.layer_event
	    (id, tenant_id, stream_key, layer, feature_ref, op, before_attrs, after_attrs, source_event_id, source_type, source_module,
	     event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $10, $11, $12, $13, $14, $15)
	  RETURNING recorded_at, seq`,
		x.ID, tid, "layer:"+x.Layer+":"+x.FeatureRef.String(), x.Layer, x.FeatureRef, string(x.Op), jsonArg(x.Before), jsonArg(x.After),
		x.SourceEventID, x.SourceType, x.SourceModule, x.EventTime.UTC(), domain.SystemActor, IdemKey(x.LayerChange), domain.RuleLayer).
		Scan(&x.RecordedAt, &x.Seq)
	x.RecordedAt = x.RecordedAt.UTC()
	return ledger.Classify(err)
}

// EventQuery lọc nhật ký lớp theo recorded_at [From, To) và con trỏ (recorded_at, id).
type EventQuery struct {
	Layer      string // rỗng = mọi lớp
	FeatureRef uuid.UUID
	From, To   time.Time
	HasAfter   bool // có con trỏ (AfterAt, AfterID)
	AfterAt    time.Time
	AfterID    uuid.UUID
	Limit      int
}

// ListLayer liệt kê nhật ký theo (recorded_at, id) tăng dần.
func ListLayer(ctx context.Context, tx pgx.Tx, q EventQuery) ([]LayerRow, error) {
	rows, err := tx.Query(ctx, "SELECT "+layerCols+` FROM pf_metrics.layer_event
	  WHERE ($1 = '' OR layer = $1) AND ($2 = '00000000-0000-0000-0000-000000000000'::uuid OR feature_ref = $2)
	    AND recorded_at >= $3 AND recorded_at < $4
	    AND (NOT $5::boolean OR (recorded_at, id) > ($6, $7))
	  ORDER BY recorded_at, id LIMIT $8`,
		q.Layer, q.FeatureRef, q.From.UTC(), q.To.UTC(), q.HasAfter, q.AfterAt.UTC(), q.AfterID, q.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (LayerRow, error) { return scanLayer(r) })
}

// ChangesBetween đọc mọi thay đổi của một lớp sau watermark (nếu có) và trước cutoff,
// theo (recorded_at, id) — đầu vào của phát lại và dựng ảnh chụp.
func ChangesBetween(ctx context.Context, tx pgx.Tx, layer string, wm *port.Watermark, cutoff time.Time) ([]port.LayerChange, error) {
	has := wm != nil
	var at time.Time
	var id uuid.UUID
	if has {
		at, id = wm.RecordedAt.UTC(), wm.LastEventID
	}
	rows, err := tx.Query(ctx, "SELECT "+layerCols+` FROM pf_metrics.layer_event
	  WHERE layer = $1 AND recorded_at < $2 AND (NOT $3::boolean OR (recorded_at, id) > ($4, $5))
	  ORDER BY recorded_at, id`, layer, cutoff.UTC(), has, at, id)
	if err != nil {
		return nil, err
	}
	xs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (LayerRow, error) { return scanLayer(r) })
	out := make([]port.LayerChange, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.LayerChange)
	}
	return out, err
}

// Snapshot là một dòng layer_snapshot.
type Snapshot struct {
	port.LayerSnapshot
	Cutoff         time.Time
	FeaturesSHA256 []byte
	BaseDate       *clock.Date
	RecordedAt     time.Time
}

const snapCols = `layer, business_date, cutoff, watermark_event_id, watermark_recorded_at, features, features_sha256, base_date, recorded_at`

func scanSnap(r pgx.Row) (Snapshot, error) {
	var s Snapshot
	var day time.Time
	var base *time.Time
	var wmID *uuid.UUID
	var wmAt *time.Time
	var feats []byte
	if err := r.Scan(&s.Layer, &day, &s.Cutoff, &wmID, &wmAt, &feats, &s.FeaturesSHA256, &base, &s.RecordedAt); err != nil {
		return s, err
	}
	s.BusinessDate = domain.DateOf(day)
	if base != nil {
		b := domain.DateOf(*base)
		s.BaseDate = &b
	}
	if wmID != nil && wmAt != nil {
		s.Watermark = port.Watermark{LastEventID: *wmID, RecordedAt: wmAt.UTC()}
	}
	fs, err := domain.DecodeFeatures(feats)
	s.Features = fs
	return s, err
}

// LatestSnapshot đọc ảnh chụp mới nhất có ngày ≤ d; không có → nil.
func LatestSnapshot(ctx context.Context, tx pgx.Tx, layer string, d clock.Date) (*Snapshot, error) {
	s, err := scanSnap(tx.QueryRow(ctx, "SELECT "+snapCols+` FROM pf_metrics.layer_snapshot
	  WHERE layer = $1 AND business_date <= $2 ORDER BY business_date DESC LIMIT 1`, layer, domain.DateTime(d)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// SnapshotsOn đọc ảnh chụp của đúng ngày d (mọi lớp).
func SnapshotsOn(ctx context.Context, tx pgx.Tx, d clock.Date) ([]Snapshot, error) {
	rows, err := tx.Query(ctx, "SELECT "+snapCols+" FROM pf_metrics.layer_snapshot WHERE business_date = $1 ORDER BY layer", domain.DateTime(d))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Snapshot, error) { return scanSnap(r) })
}

// InsertSnapshot ghi ảnh chụp; đã có (lớp, ngày) → false (không ghi đè, RB-1).
func InsertSnapshot(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s Snapshot) (bool, error) {
	feats, err := domain.EncodeFeatures(s.Features)
	if err != nil {
		return false, err
	}
	var wmID *uuid.UUID
	var wmAt *time.Time
	if s.Watermark.LastEventID != uuid.Nil {
		id, at := s.Watermark.LastEventID, s.Watermark.RecordedAt.UTC()
		wmID, wmAt = &id, &at
	}
	var base *time.Time
	if s.BaseDate != nil {
		b := domain.DateTime(*s.BaseDate)
		base = &b
	}
	tag, err := tx.Exec(ctx, `INSERT INTO pf_metrics.layer_snapshot
	    (tenant_id, layer, business_date, cutoff, watermark_event_id, watermark_recorded_at, feature_count, features, features_sha256, base_date, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11) ON CONFLICT DO NOTHING`,
		tid, s.Layer, domain.DateTime(s.BusinessDate), s.Cutoff.UTC(), wmID, wmAt, len(s.Features), string(feats), s.FeaturesSHA256, base, domain.RuleLayer)
	return tag.RowsAffected() == 1, err
}
