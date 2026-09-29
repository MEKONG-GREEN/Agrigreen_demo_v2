package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Đường bộ (GIS US-ROAD-01/02/03, BRD FN-08, BR-29/30). Tra cứu hạ tầng tĩnh
// theo tải trọng — không lập lịch/tối ưu vận chuyển (thuộc TMS).

// RoadInput là tuyến gửi lên. Con trỏ nil (khi sửa) = giữ giá trị hiện hành.
type RoadInput struct {
	Ref          uuid.UUID // tạo: Nil → server sinh
	Name         *string
	RoadClass    *string
	MaxLoadTons  *decimal.Decimal
	MaxLoadBad   bool
	Geometry     json.RawMessage // GeoJSON LineString; sửa: rỗng = giữ hình
	ProvinceCode *string
	EventTime    time.Time
	DeviceID     *string
	SupersedesID uuid.UUID
	Reason       string
	IdemKey      string
}

// RoadWriteResult là kết quả ghi tuyến.
type RoadWriteResult struct {
	Road     store.Road
	Segment  store.RoadSegment
	Replayed bool
}

type roadState struct {
	name, class, province string
	load                  decimal.Decimal
	line                  domain.LineString
}

func (s *Service) resolveRoad(ctx context.Context, a access, in RoadInput, cur *store.Road) (roadState, error) {
	var st roadState
	if cur != nil {
		st = roadState{name: cur.Name, class: cur.RoadClass, province: cur.ProvinceCode, load: cur.MaxLoadTons}
	}
	st.name = strOr(in.Name, st.name)
	st.class = strOr(in.RoadClass, st.class)
	st.province = strOr(in.ProvinceCode, st.province)
	// GIS US-ROAD-02/03 AC-2: thiếu tên/phân loại, tải trọng ≤ 0 hoặc sai định dạng.
	if st.name == "" || st.class == "" {
		f := problem.Validation.WithDetail(domain.MsgRoadRequired)
		if st.name == "" {
			f = f.WithField("name", "bắt buộc")
		}
		if st.class == "" {
			f = f.WithField("road_class", "bắt buộc")
		}
		return st, f
	}
	switch {
	case in.MaxLoadBad || (in.MaxLoadTons == nil && cur == nil) || (in.MaxLoadTons != nil && !in.MaxLoadTons.IsPositive()):
		return st, problem.Validation.WithDetail(domain.MsgRoadLoadPositive).WithField("max_load_tons", domain.MsgRoadLoadPositive)
	case in.MaxLoadTons != nil:
		st.load = *in.MaxLoadTons
	}
	if len([]rune(st.name)) > 200 {
		return st, problem.Validation.WithField("name", "tối đa 200 ký tự")
	}
	if !domain.KnownRoadClass(st.class) {
		return st, problem.Validation.WithField("road_class", "phải là quoc_lo, tinh_lo, huyen_lo, truc_xa hoặc bo_ruong")
	}
	if len(in.Geometry) > 0 && string(in.Geometry) != "null" {
		ls, err := domain.ParseLineString(in.Geometry)
		if err != nil {
			return st, violationProblem(err)
		}
		st.line = ls
	} else if cur == nil {
		return st, problem.Validation.WithField("geometry", "bắt buộc: GeoJSON LineString")
	}
	if err := s.unitAt(ctx, "province_code", st.province, mdport.LevelProvince); err != nil {
		return st, err
	}
	if !a.sc.Allows(st.province) {
		return st, ErrOutOfScope
	}
	return st, nil
}

func (s *Service) roadReplay(ctx context.Context, key string) func(pgx.Tx) (*RoadWriteResult, error) {
	return func(tx pgx.Tx) (*RoadWriteResult, error) {
		seg, err := store.RoadSegmentByIdemKey(ctx, tx, key)
		if err != nil || seg == nil {
			return nil, err
		}
		r, err := store.RoadByRef(ctx, tx, seg.RoadRef, false)
		if err != nil || r == nil {
			return nil, err
		}
		return &RoadWriteResult{Road: *r, Segment: *seg, Replayed: true}, nil
	}
}

func roadWrite(ctx context.Context, tx pgx.Tx, seg store.RoadSegment, geojson, status, statusReason string) (RoadWriteResult, error) {
	if err := store.InsertRoadSegment(ctx, tx, &seg, geojson); err != nil {
		return RoadWriteResult{}, err
	}
	if err := store.PutRoad(ctx, tx, seg.ID, status, statusReason); err != nil {
		return RoadWriteResult{}, err
	}
	r, err := store.RoadByRef(ctx, tx, seg.RoadRef, false)
	if err != nil {
		return RoadWriteResult{}, err
	}
	return RoadWriteResult{Road: *r, Segment: seg}, nil
}

// CreateRoad vẽ mới tuyến đường bộ (GIS US-ROAD-02 AC-1); chiều dài tính trắc địa trong DB.
func (s *Service) CreateRoad(ctx context.Context, in RoadInput) (RoadWriteResult, error) {
	a, err := s.access(ctx, adminRoles...)
	if err != nil {
		return RoadWriteResult{}, err
	}
	if in.IdemKey == "" {
		return RoadWriteResult{}, problem.IdempotencyRequired
	}
	if in.Ref == uuid.Nil {
		in.Ref = ids.New()
	} else if in.Ref.Version() != 7 {
		return RoadWriteResult{}, problem.Validation.WithField("ref", "phải là UUIDv7")
	}
	st, err := s.resolveRoad(ctx, a, in, nil)
	if err != nil {
		return RoadWriteResult{}, err
	}
	at, err := s.eventTime(in.EventTime)
	if err != nil {
		return RoadWriteResult{}, err
	}
	res, replayed, err := writeOnce(ctx, s, a.p.TenantID, s.roadReplay(ctx, in.IdemKey), func(tx pgx.Tx) (RoadWriteResult, error) {
		if ex, err := store.RoadByRef(ctx, tx, in.Ref, false); err != nil {
			return RoadWriteResult{}, err
		} else if ex != nil {
			return RoadWriteResult{}, ErrFeatureState.WithDetail("Mã tham chiếu tuyến đã được dùng")
		}
		return roadWrite(ctx, tx, store.RoadSegment{
			ID: ids.New(), TenantID: a.p.TenantID, RoadRef: in.Ref, Action: domain.ActionRecorded, Name: st.name, RoadClass: st.class,
			MaxLoadTons: st.load, PointCount: len(st.line), ProvinceCode: st.province, EventTime: at, ActorRef: a.p.ActorRef,
			DeviceID: in.DeviceID, IdemKey: in.IdemKey,
		}, st.line.GeoJSON(), domain.StatusActive, "")
	})
	res.Replayed = replayed
	return res, err
}

// CorrectRoad ghi phiên bản tuyến mới (GIS US-ROAD-03 AC-1: kéo/thêm/xóa điểm,
// sửa tên/phân loại/tải trọng) — bản ghi bù trỏ phiên bản hiện hành kèm lý do.
func (s *Service) CorrectRoad(ctx context.Context, ref uuid.UUID, in RoadInput) (RoadWriteResult, error) {
	a, err := s.access(ctx, adminRoles...)
	if err != nil {
		return RoadWriteResult{}, err
	}
	if in.IdemKey == "" {
		return RoadWriteResult{}, problem.IdempotencyRequired
	}
	if in.SupersedesID == uuid.Nil {
		return RoadWriteResult{}, problem.Validation.WithField("supersedes_id", "bắt buộc: phiên bản tuyến đang được thay")
	}
	why, err := reason("correction_reason", in.Reason)
	if err != nil {
		return RoadWriteResult{}, err
	}
	at, err := s.eventTime(in.EventTime)
	if err != nil {
		return RoadWriteResult{}, err
	}
	res, replayed, err := writeOnce(ctx, s, a.p.TenantID, s.roadReplay(ctx, in.IdemKey), func(tx pgx.Tx) (RoadWriteResult, error) {
		cur, err := store.RoadByRef(ctx, tx, ref, true)
		if err != nil {
			return RoadWriteResult{}, err
		}
		if cur == nil {
			return RoadWriteResult{}, ErrRoadNotFound
		}
		if cur.Status == domain.StatusRetired {
			return RoadWriteResult{}, ErrFeatureState.WithDetail("tuyến đã ngừng dùng — khôi phục trước khi sửa")
		}
		if in.SupersedesID != cur.CurrentSegmentID {
			return RoadWriteResult{}, ErrVersionConflict.With("current_segment_id", cur.CurrentSegmentID.String()).
				With("current_version", cur.CurrentVersion)
		}
		if !a.sc.Allows(cur.ProvinceCode) {
			return RoadWriteResult{}, ErrOutOfScope
		}
		st, err := s.resolveRoad(ctx, a, in, cur)
		if err != nil {
			return RoadWriteResult{}, err
		}
		gj, points := "", 0
		if st.line != nil {
			gj, points = st.line.GeoJSON(), len(st.line)
		} else if points, err = segmentPoints(ctx, tx, cur); err != nil {
			return RoadWriteResult{}, err
		}
		sup := cur.CurrentSegmentID
		return roadWrite(ctx, tx, store.RoadSegment{
			ID: ids.New(), TenantID: a.p.TenantID, RoadRef: ref, Action: domain.ActionRecorded, Name: st.name, RoadClass: st.class,
			MaxLoadTons: st.load, PointCount: points, ProvinceCode: st.province, EventTime: at, SupersedesID: &sup,
			CorrectionReason: &why, ActorRef: a.p.ActorRef, DeviceID: in.DeviceID, IdemKey: in.IdemKey,
		}, gj, domain.StatusActive, "")
	})
	res.Replayed = replayed
	return res, err
}

// segmentPoints đọc số đỉnh của phiên bản hiện hành.
func segmentPoints(ctx context.Context, tx pgx.Tx, cur *store.Road) (int, error) {
	segs, err := store.RoadSegments(ctx, tx, cur.Ref)
	if err != nil {
		return 0, err
	}
	for _, sg := range segs {
		if sg.ID == cur.CurrentSegmentID {
			return sg.PointCount, nil
		}
	}
	return 2, nil
}

// RoadStatusInput ngừng dùng / khôi phục tuyến.
type RoadStatusInput struct {
	Ref     uuid.UUID
	Reason  string
	IdemKey string
}

// RetireRoad ngừng dùng tuyến (xóa mềm): ẩn khỏi bản đồ, giữ lịch sử.
func (s *Service) RetireRoad(ctx context.Context, in RoadStatusInput) (RoadWriteResult, error) {
	return s.roadStatus(ctx, in, domain.ActionRetired)
}

// RestoreRoad khôi phục tuyến đã ngừng dùng.
func (s *Service) RestoreRoad(ctx context.Context, in RoadStatusInput) (RoadWriteResult, error) {
	return s.roadStatus(ctx, in, domain.ActionRestored)
}

func (s *Service) roadStatus(ctx context.Context, in RoadStatusInput, action string) (RoadWriteResult, error) {
	a, err := s.access(ctx, adminRoles...)
	if err != nil {
		return RoadWriteResult{}, err
	}
	if in.IdemKey == "" {
		return RoadWriteResult{}, problem.IdempotencyRequired
	}
	why, err := reason("reason", in.Reason)
	if err != nil {
		return RoadWriteResult{}, err
	}
	now := s.now()
	res, replayed, err := writeOnce(ctx, s, a.p.TenantID, s.roadReplay(ctx, in.IdemKey), func(tx pgx.Tx) (RoadWriteResult, error) {
		cur, err := store.RoadByRef(ctx, tx, in.Ref, true)
		if err != nil {
			return RoadWriteResult{}, err
		}
		if cur == nil {
			return RoadWriteResult{}, ErrRoadNotFound
		}
		if !a.sc.Allows(cur.ProvinceCode) {
			return RoadWriteResult{}, ErrOutOfScope
		}
		status, statusReason := domain.StatusRetired, why
		switch {
		case action == domain.ActionRetired && cur.Status == domain.StatusRetired:
			return RoadWriteResult{}, ErrFeatureState.WithDetail("tuyến đã ngừng dùng")
		case action == domain.ActionRestored && cur.Status != domain.StatusRetired:
			return RoadWriteResult{}, ErrFeatureState.WithDetail("tuyến chưa ngừng dùng")
		case action == domain.ActionRestored:
			status, statusReason = domain.StatusActive, ""
		}
		points, err := segmentPoints(ctx, tx, cur)
		if err != nil {
			return RoadWriteResult{}, err
		}
		sup := cur.CurrentSegmentID
		return roadWrite(ctx, tx, store.RoadSegment{
			ID: ids.New(), TenantID: a.p.TenantID, RoadRef: cur.Ref, Action: action, Name: cur.Name, RoadClass: cur.RoadClass,
			MaxLoadTons: cur.MaxLoadTons, PointCount: points, ProvinceCode: cur.ProvinceCode, EventTime: now, SupersedesID: &sup,
			CorrectionReason: &why, ActorRef: a.p.ActorRef, IdemKey: in.IdemKey,
		}, "", status, statusReason)
	})
	res.Replayed = replayed
	return res, err
}

// RoadDetail là tuyến kèm mọi phiên bản.
type RoadDetail struct {
	Road     store.Road
	Versions []store.RoadSegment
}

// GetRoad đọc tuyến kèm lịch sử phiên bản.
func (s *Service) GetRoad(ctx context.Context, ref uuid.UUID) (RoadDetail, error) {
	a, err := s.access(ctx, layerReadRoles...)
	if err != nil {
		return RoadDetail{}, err
	}
	var d RoadDetail
	found := false
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		r, err := store.RoadByRef(ctx, tx, ref, false)
		if err != nil || r == nil {
			return err
		}
		found, d.Road = true, *r
		d.Versions, err = store.RoadSegments(ctx, tx, ref)
		return err
	})
	if err != nil {
		return RoadDetail{}, err
	}
	if !found {
		return RoadDetail{}, ErrRoadNotFound
	}
	return d, nil
}

// RoadQueryInput là truy vấn lớp đường bộ.
type RoadQueryInput struct {
	BBoxInput
	MinLoadTons *decimal.Decimal
	MinLoadBad  bool
	RoadClass   string
}

// RoadPage là kết quả lớp đường bộ. NoneMatched: đã lọc theo tải trọng mà không
// có tuyến nào (GIS US-ROAD-01 AC-2) — Notice mang thông báo cho người dùng.
type RoadPage struct {
	Items       []store.Road
	Truncated   bool
	NoneMatched bool
	Notice      string
}

// RoadsInBBox trả tuyến trong khung nhìn có tải trọng cho phép ≥ tải trọng nhập
// (BR-30); chiều dài mỗi tuyến là trắc địa (US-ROAD-01 AC-1). Lớp hạ tầng dùng
// chung — không lọc theo phạm vi, chỉ theo vai.
func (s *Service) RoadsInBBox(ctx context.Context, in RoadQueryInput) (RoadPage, error) {
	a, err := s.access(ctx, layerReadRoles...)
	if err != nil {
		return RoadPage{}, err
	}
	return s.roadLayer(ctx, a, in)
}

func (s *Service) roadLayer(ctx context.Context, a access, in RoadQueryInput) (RoadPage, error) {
	limit, err := viewportLimit(in.Limit)
	if err != nil {
		return RoadPage{}, err
	}
	bb := store.BBox{MinLon: in.MinLon, MinLat: in.MinLat, MaxLon: in.MaxLon, MaxLat: in.MaxLat}
	if err := validBBox(bb); err != nil {
		return RoadPage{}, err
	}
	if in.MinLoadBad || (in.MinLoadTons != nil && !in.MinLoadTons.IsPositive()) {
		return RoadPage{}, problem.Validation.WithField("min_load_tons", "tải trọng phương tiện phải là số lớn hơn 0")
	}
	in.RoadClass = strings.TrimSpace(in.RoadClass)
	if in.RoadClass != "" && !domain.KnownRoadClass(in.RoadClass) {
		return RoadPage{}, problem.Validation.WithField("road_class", "phải là quoc_lo, tinh_lo, huyen_lo, truc_xa hoặc bo_ruong")
	}
	var items []store.Road
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListRoadsBBox(ctx, tx, store.RoadQuery{
			BBox: bb, MinLoadTons: in.MinLoadTons, RoadClass: in.RoadClass, SimplifyDeg: SimplifyTolerance(in.Zoom), Limit: limit + 1,
		})
		return e
	})
	if err != nil {
		return RoadPage{}, err
	}
	pg := RoadPage{Items: items}
	if len(items) > limit {
		pg.Items, pg.Truncated = items[:limit], true
	}
	if pg.Items == nil {
		pg.Items = []store.Road{}
	}
	if in.MinLoadTons != nil && len(pg.Items) == 0 {
		pg.NoneMatched, pg.Notice = true, domain.MsgRoadNoneMatched
	}
	return pg, nil
}
