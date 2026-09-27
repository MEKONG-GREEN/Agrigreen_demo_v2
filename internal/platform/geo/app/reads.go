package app

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// MaxViewportPlots: GIS BR-25 — tối đa 1.000 polygon mỗi khung nhìn.
const MaxViewportPlots = 1000

// SimplifyTolerance trả dung sai đơn giản hóa (độ) cho mức zoom web-mercator:
// nửa điểm ảnh ở xích đạo. Zoom ≥ 17 (cận cảnh 1:5.000 trở xuống) giữ nguyên hình.
func SimplifyTolerance(zoom int) float64 {
	if zoom >= 17 || zoom < 0 {
		return 0
	}
	return 360.0 / (256.0 * math.Pow(2, float64(zoom))) / 2
}

// BBoxInput là tham số đọc theo khung nhìn.
type BBoxInput struct {
	MinLon, MinLat, MaxLon, MaxLat float64
	Zoom                           int
	Limit                          int
}

// BBoxPage là kết quả khung nhìn. Truncated = còn thửa trong khung nhìn vượt Limit.
type BBoxPage struct {
	Items     []store.Plot
	Truncated bool
}

// PlotsInBBox trả thửa trong khung nhìn thuộc phạm vi người gọi (GIS FN-07 BR-24/25).
func (s *Service) PlotsInBBox(ctx context.Context, in BBoxInput) (BBoxPage, error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return BBoxPage{}, err
	}
	if in.Limit == 0 {
		in.Limit = MaxViewportPlots
	}
	if in.Limit < 1 || in.Limit > MaxViewportPlots {
		return BBoxPage{}, problem.Validation.WithField("limit", "từ 1 đến 1000")
	}
	if !(in.MinLon < in.MaxLon && in.MinLat < in.MaxLat) || in.MinLon < -180 || in.MaxLon > 180 || in.MinLat < -90 || in.MaxLat > 90 {
		return BBoxPage{}, problem.Validation.WithField("bbox", "phải là minLon,minLat,maxLon,maxLat theo WGS84, min < max")
	}
	if a.sc.Empty() {
		return BBoxPage{Items: []store.Plot{}}, nil
	}
	var items []store.Plot
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListBBox(ctx, tx, store.BBoxQuery{
			Filter: a.filter(), MinLon: in.MinLon, MinLat: in.MinLat, MaxLon: in.MaxLon, MaxLat: in.MaxLat,
			SimplifyDeg: SimplifyTolerance(in.Zoom), Limit: in.Limit + 1,
		})
		return e
	})
	if err != nil {
		return BBoxPage{}, err
	}
	pg := BBoxPage{Items: items}
	if len(items) > in.Limit {
		pg.Items, pg.Truncated = items[:in.Limit], true
	}
	if pg.Items == nil {
		pg.Items = []store.Plot{}
	}
	return pg, nil
}

// PlotDetail là thửa kèm lịch sử phiên bản và xung đột.
type PlotDetail struct {
	Plot       store.Plot
	Boundaries []store.Boundary
	Conflicts  []store.Conflict
}

// GetPlot đọc thửa kèm mọi phiên bản ranh giới; ngoài phạm vi → 404 (không lộ sự tồn tại).
func (s *Service) GetPlot(ctx context.Context, ref uuid.UUID) (PlotDetail, error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return PlotDetail{}, err
	}
	var d PlotDetail
	var p *store.Plot
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if p, e = store.PlotByRef(ctx, tx, ref, false); e != nil || p == nil || !a.sees(p.CommuneCode, p.CooperativeRef) {
			return e
		}
		if d.Boundaries, e = store.Boundaries(ctx, tx, ref); e != nil {
			return e
		}
		d.Conflicts, e = store.ListConflicts(ctx, tx, store.ConflictFilter{All: true, PlotRef: ref, Limit: 200})
		return e
	})
	if err != nil {
		return PlotDetail{}, err
	}
	if p == nil || !a.sees(p.CommuneCode, p.CooperativeRef) {
		return PlotDetail{}, ErrPlotNotFound
	}
	d.Plot = *p
	return d, nil
}

// CoopPage là một trang thửa của HTX.
type CoopPage struct {
	Items []store.Plot
	Next  *uuid.UUID
}

// PlotsOfCooperative liệt kê thửa của một HTX trong phạm vi (HTX US-PLOT-01).
func (s *Service) PlotsOfCooperative(ctx context.Context, coop, after uuid.UUID, limit int) (CoopPage, error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return CoopPage{}, err
	}
	if limit <= 0 || limit > MaxViewportPlots {
		limit = 200
	}
	if a.coopOnly && coop != a.p.OrgRef {
		return CoopPage{}, ErrOutOfScope
	}
	if a.sc.Empty() {
		return CoopPage{Items: []store.Plot{}}, nil
	}
	var items []store.Plot
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListByCooperative(ctx, tx, a.filter(), coop, after, limit+1)
		return e
	})
	if err != nil {
		return CoopPage{}, err
	}
	pg := CoopPage{Items: items}
	if len(items) > limit {
		pg.Items = items[:limit]
		last := pg.Items[limit-1].Ref
		pg.Next = &last
	}
	if pg.Items == nil {
		pg.Items = []store.Plot{}
	}
	return pg, nil
}

// OverlapCheckInput kiểm trước một ranh giới (KN US-PLOT-03: cảnh báo ngay khi vẽ).
type OverlapCheckInput struct {
	Geometry       json.RawMessage
	CommuneCode    string
	CooperativeRef uuid.UUID
	ExcludePlotRef uuid.UUID
	DeclaredAreaHa *decimal.Decimal
}

// OverlapCheck là kết quả kiểm trước — không ghi gì.
type OverlapCheck struct {
	AreaHa       decimal.Decimal
	PointCount   int
	Blocking     []domain.Overlap // HTX khác → bị chặn khi lưu (geo.overlap_cross_coop)
	NeedsConfirm []domain.Overlap // cùng HTX / thửa nháp → lưu được khi confirm_overlap = true
	Tolerance    decimal.Decimal
	// DeviationPct: lệch so với diện tích khai báo; DeviationNeedsConfirm khi vượt ngưỡng.
	DeviationPct          *decimal.Decimal
	DeviationNeedsConfirm bool
}

// CheckOverlap chạy đủ các kiểm tra như khi lưu nhưng không ghi (giao dịch luôn rollback).
func (s *Service) CheckOverlap(ctx context.Context, in OverlapCheckInput) (OverlapCheck, error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return OverlapCheck{}, err
	}
	mp, n, err := domain.ParseGeometry(in.Geometry)
	if err != nil {
		return OverlapCheck{}, violationProblem(err)
	}
	in.CommuneCode = strings.TrimSpace(in.CommuneCode)
	if a.coopOnly && in.CooperativeRef == uuid.Nil {
		in.CooperativeRef = a.p.OrgRef
	}
	if err := s.checkCommune(ctx, in.CommuneCode); err != nil {
		return OverlapCheck{}, err
	}
	if !a.sc.Allows(in.CommuneCode) {
		return OverlapCheck{}, ErrOutOfScope
	}
	tol, maxDev, err := s.thresholds(ctx, s.now())
	if err != nil {
		return OverlapCheck{}, err
	}
	out := OverlapCheck{PointCount: n, Tolerance: tol}
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		c, e := checkShape(ctx, tx, mp, in.CommuneCode, in.CooperativeRef, in.ExcludePlotRef, tol)
		if e != nil {
			return e
		}
		c.deviation(in.DeclaredAreaHa)
		out.AreaHa, out.Blocking, out.NeedsConfirm, out.DeviationPct = c.areaHa, c.cross, c.same, c.devPct
		out.DeviationNeedsConfirm = c.devPct != nil && c.devPct.GreaterThan(maxDev)
		return errRollback
	})
	if err != nil && !errors.Is(err, errRollback) {
		return OverlapCheck{}, err
	}
	return out, nil
}

// errRollback kết thúc giao dịch chỉ đọc mà không commit.
var errRollback = errors.New("geo: rollback có chủ đích")
