package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// AsOf thực hiện port.Source trên mọi nguồn đã nối: chọn định nghĩa có hiệu lực
// vào d, kiểm ngày đã kết thúc, gọi nguồn của module chủ dữ liệu với định nghĩa
// trong context, rồi ghim phiên bản, đơn vị, cutoff.
func (s *Service) AsOf(ctx context.Context, key port.Key, d clock.Date, sc port.Scope) (port.Value, error) {
	def, ok := s.Catalog.Effective(key, d)
	if !ok {
		return port.Value{}, port.ErrUnknownMetric
	}
	if !domain.Closed(d, s.now()) {
		return port.Value{}, port.ErrNotClosed
	}
	return s.asOfDef(ctx, def, d, sc)
}

func (s *Service) asOfDef(ctx context.Context, def domain.Def, d clock.Date, sc port.Scope) (port.Value, error) {
	src := s.Sources[def.SourceModule]
	if src == nil {
		return port.Value{}, ErrDependency.WithDetail("chưa nối nguồn " + def.SourceModule)
	}
	v, err := src.AsOf(port.WithDefinition(ctx, def.Definition), def.Key, d, sc)
	if err != nil {
		return port.Value{}, err
	}
	v.Key, v.DefinitionVersion, v.BusinessDate, v.Scope, v.Unit, v.Cutoff = def.Key, def.Version, d, sc, def.Unit, port.Cutoff(d)
	return v, nil
}

// RecordResult đếm kết quả một lần ghi sổ.
type RecordResult struct {
	Inserted, Superseded, Unchanged, NoData int
}

func (r *RecordResult) add(o RecordResult) {
	r.Inserted += o.Inserted
	r.Superseded += o.Superseded
	r.Unchanged += o.Unchanged
	r.NoData += o.NoData
}

// RecordDay tính và ghi sổ mọi chỉ số của tenant cho ngày d (đã kết thúc). Giá trị
// trùng băm đầu vào với dòng hiện hành → không ghi (idempotent); khác → dòng mới
// supersedes dòng cũ kèm lý do (tính lại). keys rỗng = mọi chỉ số.
func (s *Service) RecordDay(ctx context.Context, tid uuid.UUID, d clock.Date, keys []port.Key) (RecordResult, error) {
	var res RecordResult
	if !domain.Closed(d, s.now()) {
		return res, ErrNotClosed.WithDetail(d.String())
	}
	if len(keys) == 0 {
		keys = s.Catalog.Keys()
	}
	var errs []error
	for _, key := range keys {
		def, ok := s.Catalog.Effective(key, d)
		if !ok {
			continue // định nghĩa chưa có hiệu lực vào d
		}
		r, err := s.recordKey(ctx, tid, def, d)
		res.add(r)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", key, d, err))
		}
	}
	return res, errors.Join(errs...)
}

func (s *Service) recordKey(ctx context.Context, tid uuid.UUID, def domain.Def, d clock.Date) (RecordResult, error) {
	var res RecordResult
	if err := s.tx(ctx, tid, func(tx pgx.Tx) error { return store.PinDefinition(ctx, tx, tid, def) }); err != nil {
		if errors.Is(err, store.ErrDefinitionEdited) {
			return res, ErrDefEdited.WithDetail(err.Error())
		}
		return res, err
	}
	scopes := []port.Scope{{TenantID: tid}}
	if l, ok := s.Sources[def.SourceModule].(port.ScopeLister); ok {
		list, err := l.Scopes(ctx, def.Key, d, tid)
		if err != nil {
			return res, err
		}
		scopes = list
	}
	for _, sc := range scopes {
		sc.TenantID = tid
		if len(sc.Units) > 1 {
			continue // chỉ ghi sổ phạm vi toàn tenant hoặc một đơn vị
		}
		if def.Grain == port.GrainSeason && sc.Dimension == "" {
			continue
		}
		v, err := s.asOfDef(ctx, def, d, sc)
		if errors.Is(err, port.ErrNoData) {
			res.NoData++
			continue
		}
		if err != nil {
			return res, err
		}
		r, err := s.writeValue(ctx, tid, def, d, sc, v)
		res.add(r)
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *Service) writeValue(ctx context.Context, tid uuid.UUID, def domain.Def, d clock.Date, sc port.Scope, v port.Value) (RecordResult, error) {
	var res RecordResult
	unit := domain.ScopeUnit(sc)
	hash := domain.InputsHash(def, sc, d, v.Value, v.Watermark)
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		prev, err := store.LatestValue(ctx, tx, def.Key, unit, sc.Dimension, d)
		if err != nil {
			return err
		}
		if prev != nil && bytes.Equal(prev.InputsHash, hash) {
			res.Unchanged++
			return nil
		}
		row := &store.ValueRow{ID: ids.New(), Key: def.Key, DefVersion: def.Version, ScopeUnit: unit, Dimension: sc.Dimension,
			BusinessDate: d, Value: v.Value, Unit: def.Unit, Cutoff: port.Cutoff(d), Watermark: v.Watermark, InputsHash: hash}
		if prev != nil {
			reason := fmt.Sprintf("tính lại: v%d → v%d, %s → %s", prev.DefVersion, def.Version, prev.Value.String(), v.Value.String())
			row.SupersedesID, row.CorrectionReason = &prev.ID, &reason
		}
		if err := store.InsertValue(ctx, tx, tid, row); err != nil {
			return err
		}
		if prev != nil {
			res.Superseded++
		} else {
			res.Inserted++
		}
		return nil
	})
	return res, err
}

// Backfill ghi sổ mọi ngày trong [from, to] (đã kết thúc) — lệnh worker
// -metrics-backfill. Dòng hiện hành không đổi băm được giữ nguyên; khác → supersedes.
func (s *Service) Backfill(ctx context.Context, tid uuid.UUID, from, to clock.Date, keys []port.Key) (RecordResult, error) {
	var res RecordResult
	if domain.Before(to, from) {
		return res, problem.Validation.WithField("to", "phải ≥ from")
	}
	for d := from; !domain.Before(to, d); d = domain.AddDays(d, 1) {
		if !domain.Closed(d, s.now()) {
			break
		}
		r, err := s.RecordDay(ctx, tid, d, keys)
		res.add(r)
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// Đọc (Application API)
// ---------------------------------------------------------------------------

// ListDefinitions trả mọi phiên bản định nghĩa người gọi xem được.
func (s *Service) ListDefinitions(ctx context.Context) ([]domain.Def, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	var out []domain.Def
	for _, d := range s.Catalog.All() {
		if canView(p, d) {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil, problem.Forbidden
	}
	return out, nil
}

// ValuesQuery là tham số GET /v1/metrics/{key}/values.
type ValuesQuery struct {
	Key       port.Key
	From, To  *clock.Date
	Unit      string
	Dimension string
	History   bool
}

// ValuesResult là chuỗi giá trị đã lưu.
type ValuesResult struct {
	Def        domain.Def // phiên bản mới nhất (tiêu đề, đơn vị)
	Unit       string     // đơn vị hành chính
	Dimension  string
	From, To   clock.Date
	Items      []store.ValueRow
	Dimensions []string // grain season, không truyền dimension: các vụ đã ghi
}

// Values đọc lịch sử đã lưu (kỳ đã chốt không bị sửa — dòng tính lại supersedes).
func (s *Service) Values(ctx context.Context, q ValuesQuery) (ValuesResult, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return ValuesResult{}, problem.Unauthenticated
	}
	vers := s.Catalog.Versions(q.Key)
	if len(vers) == 0 {
		return ValuesResult{}, ErrUnknownMetric
	}
	def := vers[len(vers)-1]
	if !canView(p, def) {
		return ValuesResult{}, problem.Forbidden
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return ValuesResult{}, err
	}
	switch {
	case q.Unit == "" && !sc.All:
		return ValuesResult{}, ErrOutOfScope.WithDetail("phạm vi toàn tenant chỉ cho vai phạm vi TW — truyền unit")
	case q.Unit != "" && !sc.Allows(q.Unit):
		return ValuesResult{}, ErrOutOfScope
	}
	today := clock.BusinessDate(s.now())
	to := domain.AddDays(today, -1)
	if q.To != nil {
		to = *q.To
	}
	from := domain.AddDays(to, -30)
	if q.From != nil {
		from = *q.From
	}
	if domain.Before(to, from) {
		return ValuesResult{}, problem.Validation.WithField("from", "phải ≤ to")
	}
	if domain.Before(domain.AddDays(from, MaxRangeDays), to) {
		return ValuesResult{}, ErrRangeTooLarge.WithDetail(fmt.Sprintf("tối đa %d ngày", MaxRangeDays))
	}
	out := ValuesResult{Def: def, Unit: q.Unit, Dimension: q.Dimension, From: from, To: to, Items: []store.ValueRow{}}
	err = s.readTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if def.Grain == port.GrainSeason && q.Dimension == "" {
			dims, err := store.Dimensions(ctx, tx, q.Key, q.Unit)
			out.Dimensions = dims
			return err
		}
		rows, err := store.ListValues(ctx, tx, store.ValueQuery{Key: q.Key, Unit: q.Unit, Dimension: q.Dimension, From: from, To: to, History: q.History})
		out.Items = rows
		return err
	})
	if out.Dimensions == nil {
		out.Dimensions = []string{}
	}
	return out, err
}
