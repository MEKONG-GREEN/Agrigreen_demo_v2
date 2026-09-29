package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// attrsVisible: đối tượng có địa bàn (commune_code/province_code) chỉ hiện với người
// có phạm vi chứa địa bàn đó; lớp nền không có địa bàn (mạng đường thủy, cân đối vụ)
// hiện với mọi người được xem nhật ký lớp.
func attrsVisible(sc authz.Scope, attrs ...json.RawMessage) bool {
	if sc.All {
		return true
	}
	located := false
	for _, a := range attrs {
		if len(a) == 0 {
			continue
		}
		var m map[string]any
		if json.Unmarshal(a, &m) != nil {
			continue
		}
		for _, k := range []string{"commune_code", "province_code"} {
			if v, ok := m[k].(string); ok && v != "" {
				located = true
				if sc.Allows(v) {
					return true
				}
			}
		}
	}
	return !located
}

func (s *Service) gisCaller(ctx context.Context) (authz.Principal, authz.Scope, error) {
	p, err := authz.Require(ctx, gisViewers...)
	if err != nil {
		return p, authz.Scope{}, err
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return p, sc, err
	}
	if sc.Empty() {
		return p, sc, ErrOutOfScope.WithDetail("tài khoản chưa gắn đơn vị công tác")
	}
	return p, sc, nil
}

// EventsQuery là tham số GET /v1/gis/events.
type EventsQuery struct {
	Layer      string
	FeatureRef uuid.UUID
	From, To   *clock.Date
	After      *port.Watermark // con trỏ đã giải mã: dòng cuối trang trước
	Limit      int
}

// EventsPage là một trang nhật ký lớp.
type EventsPage struct {
	Items     []store.LayerRow
	NextAfter *port.Watermark // con trỏ trang sau; nil = hết
	From, To  clock.Date
}

// ListEvents đọc nhật ký lớp theo recorded_at ∈ [đầu ngày from, Cutoff(to)) (GIS US-EVL-01).
func (s *Service) ListEvents(ctx context.Context, q EventsQuery) (EventsPage, error) {
	p, sc, err := s.gisCaller(ctx)
	if err != nil {
		return EventsPage{}, err
	}
	if q.Layer != "" && !slices.Contains(port.Layers, q.Layer) {
		return EventsPage{}, ErrUnknownLayer.WithDetail(q.Layer)
	}
	today := clock.BusinessDate(s.now())
	to := today
	if q.To != nil {
		to = *q.To
	}
	from := domain.AddDays(to, -6)
	if q.From != nil {
		from = *q.From
	}
	if domain.Before(to, from) {
		return EventsPage{}, ErrRangeTooLarge.WithDetail("from phải ≤ to")
	}
	if domain.Before(domain.AddDays(from, MaxRangeDays), to) {
		return EventsPage{}, ErrRangeTooLarge.WithDetail(fmt.Sprintf("tối đa %d ngày", MaxRangeDays))
	}
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	eq := store.EventQuery{Layer: q.Layer, FeatureRef: q.FeatureRef, From: clock.StartOfBusinessDate(from), To: port.Cutoff(to), Limit: limit}
	if q.After != nil {
		eq.HasAfter, eq.AfterAt, eq.AfterID = true, q.After.RecordedAt, q.After.LastEventID
	}
	out := EventsPage{From: from, To: to, Items: []store.LayerRow{}}
	// Lọc phạm vi sau khi đọc: đọc tiếp trang tới khi đủ hoặc hết; con trỏ là dòng cuối đã xét.
	more := true
	for guard := 0; guard < 20 && more && len(out.Items) < limit; guard++ {
		var rows []store.LayerRow
		if err := s.readTx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			rows, e = store.ListLayer(ctx, tx, eq)
			return e
		}); err != nil {
			return EventsPage{}, err
		}
		more = len(rows) == limit
		for i, r := range rows {
			eq.HasAfter, eq.AfterAt, eq.AfterID = true, r.RecordedAt, r.ID
			if attrsVisible(sc, r.After, r.Before) {
				out.Items = append(out.Items, r)
			}
			if len(out.Items) == limit {
				if i < len(rows)-1 {
					more = true
				}
				break
			}
		}
	}
	if more && eq.HasAfter {
		out.NextAfter = &port.Watermark{LastEventID: eq.AfterID, RecordedAt: eq.AfterAt}
	}
	return out, nil
}

// LayerView là một lớp của ảnh chụp / phát lại đã lọc phạm vi.
type LayerView struct {
	Layer        string
	BusinessDate clock.Date
	Watermark    port.Watermark
	Basis        string // "snapshot" | "replay"
	BaseDate     *clock.Date
	Features     []FeatureView
	RecordedAt   *time.Time
}

// FeatureView là một đối tượng kèm hình học as-of (nếu yêu cầu).
type FeatureView struct {
	FeatureRef uuid.UUID
	Attributes json.RawMessage
	Retired    bool
	Geometry   json.RawMessage
}

func layersParam(layers []string) ([]string, error) {
	if len(layers) == 0 {
		return port.Layers, nil
	}
	for _, l := range layers {
		if !slices.Contains(port.Layers, l) {
			return nil, ErrUnknownLayer.WithDetail(l)
		}
	}
	return layers, nil
}

// SnapshotsOn đọc ảnh chụp đã lưu của đúng ngày d (GIS US-SNAP-01).
func (s *Service) SnapshotsOn(ctx context.Context, d clock.Date, layers []string) ([]LayerView, error) {
	p, sc, err := s.gisCaller(ctx)
	if err != nil {
		return nil, err
	}
	if layers, err = layersParam(layers); err != nil {
		return nil, err
	}
	var snaps []store.Snapshot
	if err := s.readTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		snaps, e = store.SnapshotsOn(ctx, tx, d)
		return e
	}); err != nil {
		return nil, err
	}
	out := []LayerView{}
	for _, sn := range snaps {
		if !slices.Contains(layers, sn.Layer) {
			continue
		}
		at := sn.RecordedAt
		out = append(out, LayerView{Layer: sn.Layer, BusinessDate: sn.BusinessDate, Watermark: sn.Watermark, Basis: "snapshot",
			BaseDate: sn.BaseDate, Features: visible(sc, sn.Features), RecordedAt: &at})
	}
	return out, nil
}

func visible(sc authz.Scope, fs []port.LayerFeature) []FeatureView {
	out := []FeatureView{}
	for _, f := range fs {
		if attrsVisible(sc, f.Attributes) {
			out = append(out, FeatureView{FeatureRef: f.FeatureRef, Attributes: f.Attributes, Retired: f.Retired})
		}
	}
	return out
}

// Replay trả mọi lớp (hoặc các lớp chọn) như hệ biết lúc hết ngày d (GIS
// US-REPLAY-01); geometry → tra hình học as-of qua pf/geo theo geometry_ref.
func (s *Service) Replay(ctx context.Context, d clock.Date, layers []string, geometry bool) ([]LayerView, error) {
	p, sc, err := s.gisCaller(ctx)
	if err != nil {
		return nil, err
	}
	if layers, err = layersParam(layers); err != nil {
		return nil, err
	}
	if domain.Before(clock.BusinessDate(s.now()), d) {
		return nil, ErrFutureDate
	}
	out := []LayerView{}
	err = s.readTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		for _, l := range layers {
			snap, base, err := replayTx(ctx, tx, l, d)
			if err != nil {
				return err
			}
			out = append(out, LayerView{Layer: l, BusinessDate: d, Watermark: snap.Watermark, Basis: "replay", BaseDate: base, Features: visible(sc, snap.Features)})
		}
		return nil
	})
	if err != nil || !geometry {
		return out, err
	}
	return out, s.attachGeometry(tenant.WithID(ctx, p.TenantID), out)
}

func (s *Service) attachGeometry(ctx context.Context, views []LayerView) error {
	if s.Geometry == nil {
		return ErrDependency.WithDetail("chưa nối pf/geo để tra hình học")
	}
	type loc struct{ v, f int }
	var refs []port.GeometryRef
	where := map[port.GeometryRef][]loc{}
	for vi, v := range views {
		for fi, f := range v.Features {
			var m struct {
				GeometryRef *struct {
					Kind string    `json:"kind"`
					ID   uuid.UUID `json:"id"`
				} `json:"geometry_ref"`
			}
			if json.Unmarshal(f.Attributes, &m) != nil || m.GeometryRef == nil || m.GeometryRef.ID == uuid.Nil {
				continue
			}
			r := port.GeometryRef{Kind: m.GeometryRef.Kind, ID: m.GeometryRef.ID}
			if _, ok := where[r]; !ok {
				refs = append(refs, r)
			}
			where[r] = append(where[r], loc{vi, fi})
		}
	}
	if len(refs) == 0 {
		return nil
	}
	geoms, err := s.Geometry.Geometries(ctx, refs)
	if err != nil {
		return ErrDependency.WithDetail("không tra được hình học").WithCause(err)
	}
	for r, g := range geoms {
		for _, l := range where[r] {
			views[l.v].Features[l.f].Geometry = g
		}
	}
	return nil
}
