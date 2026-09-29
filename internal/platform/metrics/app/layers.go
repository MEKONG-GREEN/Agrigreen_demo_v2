package app

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// ---------------------------------------------------------------------------
// port.LayerLog
// ---------------------------------------------------------------------------

// Append ghi một thay đổi lớp, idempotent theo (SourceEventID, lớp, đối tượng)
// (GIS US-EVL-01, R-61).
func (s *Service) Append(ctx context.Context, tid uuid.UUID, c port.LayerChange) error {
	return s.tx(ctx, tid, func(tx pgx.Tx) error {
		_, err := s.appendTx(ctx, tx, tid, c, "")
		return err
	})
}

func validChange(c port.LayerChange) error {
	switch {
	case !slices.Contains(port.Layers, c.Layer):
		return ErrUnknownLayer.WithDetail(c.Layer)
	case c.FeatureRef == uuid.Nil, c.SourceEventID == uuid.Nil, c.SourceType == "", c.EventTime.IsZero():
		return ErrInvalidChange.WithDetail("thiếu feature_ref, source_event_id, source_type hoặc event_time")
	case !domain.ValidOp(c.Op):
		return ErrInvalidChange.WithDetail("op không hợp lệ")
	case c.Op == port.LayerCreate && len(c.Before) > 0:
		return ErrInvalidChange.WithDetail("create không có before")
	}
	return nil
}

// appendTx ghi trong giao dịch sẵn có; trả false nếu đã ghi trước đó.
func (s *Service) appendTx(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c port.LayerChange, module string) (bool, error) {
	if err := validChange(c); err != nil {
		return false, err
	}
	dup, err := store.LayerByIdem(ctx, tx, c)
	if err != nil || dup != nil {
		return false, err
	}
	if c.ID == uuid.Nil {
		c.ID = ids.New()
	}
	if module == "" {
		module = "unknown"
	}
	x := &store.LayerRow{LayerChange: c, SourceModule: module}
	return true, store.InsertLayer(ctx, tx, tid, x)
}

// ReplayLayer = ảnh chụp mới nhất ≤ d + thay đổi sau watermark và trước Cutoff(d)
// (GIS US-REPLAY-01).
func (s *Service) ReplayLayer(ctx context.Context, tid uuid.UUID, layer string, d clock.Date) (port.LayerSnapshot, error) {
	if !slices.Contains(port.Layers, layer) {
		return port.LayerSnapshot{}, ErrUnknownLayer.WithDetail(layer)
	}
	var out port.LayerSnapshot
	err := s.readTx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		out, _, err = replayTx(ctx, tx, layer, d)
		return err
	})
	return out, err
}

// replayTx trả kết quả phát lại và ngày ảnh chụp nền (nil = dựng từ đầu nhật ký).
func replayTx(ctx context.Context, tx pgx.Tx, layer string, d clock.Date) (port.LayerSnapshot, *clock.Date, error) {
	out := port.LayerSnapshot{Layer: layer, BusinessDate: d}
	snap, err := store.LatestSnapshot(ctx, tx, layer, d)
	if err != nil {
		return out, nil, err
	}
	var base []port.LayerFeature
	var wm *port.Watermark
	var baseDate *clock.Date
	if snap != nil {
		base, baseDate = snap.Features, &snap.BusinessDate
		if snap.Watermark.LastEventID != uuid.Nil {
			w := snap.Watermark
			wm, out.Watermark = &w, w
		}
	}
	changes, err := store.ChangesBetween(ctx, tx, layer, wm, port.Cutoff(d))
	if err != nil {
		return out, nil, err
	}
	if n := len(changes); n > 0 {
		out.Watermark = port.Watermark{LastEventID: changes[n-1].ID, RecordedAt: changes[n-1].RecordedAt}
	}
	out.Features = domain.Fold(base, changes)
	return out, baseDate, nil
}

// FullReplay dựng thẳng từ nhật ký (không dùng ảnh chụp) — đối chiếu kiểm toán và
// kiểm thử bắt buộc "replay = dựng thẳng từ nhật ký".
func (s *Service) FullReplay(ctx context.Context, tid uuid.UUID, layer string, d clock.Date) (port.LayerSnapshot, error) {
	out := port.LayerSnapshot{Layer: layer, BusinessDate: d}
	err := s.readTx(ctx, tid, func(tx pgx.Tx) error {
		changes, err := store.ChangesBetween(ctx, tx, layer, nil, port.Cutoff(d))
		if n := len(changes); n > 0 {
			out.Watermark = port.Watermark{LastEventID: changes[n-1].ID, RecordedAt: changes[n-1].RecordedAt}
		}
		out.Features = domain.Fold(nil, changes)
		return err
	})
	return out, err
}

// BuildSnapshots chụp mọi lớp của tenant cho ngày d đã kết thúc (GIS US-SNAP-01).
// Ảnh chụp đã có không bị ghi đè. Trả số ảnh chụp mới.
func (s *Service) BuildSnapshots(ctx context.Context, tid uuid.UUID, d clock.Date) (int, error) {
	if !domain.Closed(d, s.now()) {
		return 0, ErrNotClosed.WithDetail(d.String())
	}
	n := 0
	for _, layer := range port.Layers {
		err := s.tx(ctx, tid, func(tx pgx.Tx) error {
			snap, base, err := replayTx(ctx, tx, layer, d)
			if err != nil {
				return err
			}
			if base != nil && *base == d {
				return nil // đã chụp
			}
			if len(snap.Features) == 0 && snap.Watermark.LastEventID == uuid.Nil {
				return nil // lớp chưa có dữ liệu
			}
			h, err := domain.FeaturesSHA256(snap.Features)
			if err != nil {
				return err
			}
			ok, err := store.InsertSnapshot(ctx, tx, tid, store.Snapshot{LayerSnapshot: snap, Cutoff: port.Cutoff(d), FeaturesSHA256: h, BaseDate: base})
			if ok {
				n++
			}
			return err
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// Consumer nhật ký lớp (worker, vai metrics_app)
// ---------------------------------------------------------------------------

// patch là thay đổi suy từ một sự kiện nguồn, trước khi ghép thuộc tính trước.
type patch struct {
	Layer   string
	Feature uuid.UUID
	Op      port.LayerOp // create được đổi thành update nếu đối tượng đã có
	Attrs   map[string]any
}

// mergeAttrs ghép thuộc tính trước với patch.
func mergeAttrs(before json.RawMessage, p map[string]any) (json.RawMessage, error) {
	m := map[string]any{}
	if len(before) > 0 {
		if err := json.Unmarshal(before, &m); err != nil {
			return nil, err
		}
	}
	for k, v := range p {
		m[k] = v
	}
	return json.Marshal(m)
}

// applyPatches ghi các dòng nhật ký của một sự kiện nguồn trong giao dịch inbox.
func (s *Service) applyPatches(ctx context.Context, tx pgx.Tx, tid uuid.UUID, src sourceEvent, ps []patch) error {
	for _, p := range ps {
		prev, err := store.LatestFeature(ctx, tx, p.Layer, p.Feature)
		if err != nil {
			return err
		}
		c := port.LayerChange{Layer: p.Layer, FeatureRef: p.Feature, Op: p.Op, SourceEventID: src.ID, SourceType: src.Type, EventTime: src.Time}
		var before json.RawMessage
		if prev != nil {
			before = prev.After
			if len(before) == 0 {
				before = prev.Before
			}
			c.Before = before
			if c.Op == port.LayerCreate {
				c.Op = port.LayerUpdate
			}
		} else if c.Op != port.LayerCreate && c.Op != port.LayerRetire {
			c.Op = port.LayerCreate // lần đầu nhật ký thấy đối tượng (sự kiện trước khi bật nhật ký)
		}
		if c.After, err = mergeAttrs(before, p.Attrs); err != nil {
			return err
		}
		if _, err := s.appendTx(ctx, tx, tid, c, src.Module); err != nil {
			return err
		}
	}
	return nil
}
