package app

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Ranh giới HTX (GIS US-BND-02/03/07, BRD FN-04 BR-13/14). Sổ phiên bản như
// ranh giới thửa: sửa là phiên bản mới trỏ phiên bản cũ kèm lý do; xóa là ngừng
// dùng (ẩn khỏi bản đồ, giữ lịch sử và người xóa). Tên, địa chỉ HTX nằm ở sổ
// pf/party — geo chỉ giữ cooperative_ref.

// CoopShapeInput là một hình ranh giới HTX gửi lên.
type CoopShapeInput struct {
	ID        uuid.UUID       // Nil → server sinh
	Geometry  json.RawMessage // GeoJSON Polygon/MultiPolygon WGS84
	Parsed    domain.MultiPolygon
	Points    int
	Source    string // geo_web (mặc định) | htx_app | import_file
	EventTime time.Time
	DeviceID  *string
}

// CoopBoundaryInput ghi ranh giới đầu tiên của HTX.
type CoopBoundaryInput struct {
	CooperativeRef uuid.UUID
	// CommuneCode: đơn vị hành chính của HTX khi chưa nối pf/party (có party thì lấy từ sổ HTX).
	CommuneCode string
	Shape       CoopShapeInput
	IdemKey     string
}

// CoopCorrectInput ghi phiên bản mới thay phiên bản SupersedesID.
type CoopCorrectInput struct {
	CooperativeRef uuid.UUID
	SupersedesID   uuid.UUID
	Reason         string
	Shape          CoopShapeInput
	IdemKey        string
}

// CoopStatusInput ngừng dùng / khôi phục ranh giới HTX.
type CoopStatusInput struct {
	CooperativeRef uuid.UUID
	Reason         string
	IdemKey        string
}

// CoopWriteResult là kết quả ghi ranh giới HTX.
type CoopWriteResult struct {
	Area     store.CoopArea
	Boundary store.CoopBoundary
	Replayed bool
}

// coopUnit trả đơn vị hành chính của HTX (phạm vi): có pf/party thì lấy từ sổ HTX
// (tồn tại, đúng loại, đang hoạt động); chưa nối thì dùng mã gửi lên.
func (s *Service) coopUnit(ctx context.Context, coop uuid.UUID, fallback string) (string, error) {
	if coop == uuid.Nil {
		return "", problem.Validation.WithDetail(domain.MsgCoopRequired).WithField("cooperative_ref", "bắt buộc: HTX trong sổ pf/party")
	}
	if s.Orgs == nil {
		fallback = strings.TrimSpace(fallback)
		if err := s.unitAt(ctx, "commune_code", fallback, ""); err != nil {
			return "", err
		}
		return fallback, nil
	}
	o, err := s.Orgs.Get(ctx, coop)
	if errors.Is(err, partyport.ErrOrgNotFound) {
		return "", ErrCooperativeInvalid
	}
	if err != nil {
		return "", ErrDependency.WithDetail("không tra được HTX").WithCause(err)
	}
	if o.Kind != partyport.OrgCooperative || !o.Active || o.AdminUnitCode == "" {
		return "", ErrCooperativeInvalid
	}
	return o.AdminUnitCode, nil
}

// prepareCoopShape đọc, kiểm hình phía Go, chuẩn hóa nguồn/thời điểm/định danh.
func (s *Service) prepareCoopShape(sh *CoopShapeInput, coopOnly bool) error {
	if sh.Parsed == nil {
		mp, n, err := domain.ParseGeometry(sh.Geometry)
		if err != nil {
			return violationProblem(err)
		}
		sh.Parsed, sh.Points = mp, n
	}
	if sh.Source == "" {
		sh.Source = domain.CoopSourceGeoWeb
		if coopOnly {
			sh.Source = domain.CoopSourceHTXApp
		}
	}
	if !domain.KnownCoopSource(sh.Source) {
		return problem.Validation.WithField("source", "phải là geo_web, htx_app hoặc import_file")
	}
	t, err := s.eventTime(sh.EventTime)
	if err != nil {
		return err
	}
	sh.EventTime = t
	if sh.ID == uuid.Nil {
		sh.ID = ids.New()
	} else if sh.ID.Version() != 7 {
		return problem.Validation.WithField("boundary_id", "phải là UUIDv7")
	}
	return nil
}

// checkCoopShape kiểm hình hợp lệ và diện tích hợp lý trong PostGIS.
func checkCoopShape(ctx context.Context, tx pgx.Tx, mp domain.MultiPolygon) (string, error) {
	gj := mp.GeoJSON()
	an, err := store.Analyze(ctx, tx, gj)
	if err != nil {
		return "", err
	}
	if !an.Valid {
		return "", ErrInvalidGeometry.WithDetail("Ranh giới HTX tự cắt hoặc sai cấu trúc (" + an.Reason + ")")
	}
	if an.AreaHa.LessThan(domain.MinCoopAreaHa) || an.AreaHa.GreaterThan(domain.MaxCoopAreaHa) {
		return "", ErrAreaOutOfRange.WithDetail("Diện tích "+an.AreaHa.String()+" ha nằm ngoài khoảng "+
			domain.MinCoopAreaHa.String()+"–"+domain.MaxCoopAreaHa.String()+" ha").With("area_ha", an.AreaHa.String())
	}
	return gj, nil
}

func (s *Service) coopReplay(ctx context.Context, key string) func(pgx.Tx) (*CoopWriteResult, error) {
	return func(tx pgx.Tx) (*CoopWriteResult, error) {
		b, err := store.CoopBoundaryByIdemKey(ctx, tx, key)
		if err != nil || b == nil {
			return nil, err
		}
		a, err := store.CoopAreaByRef(ctx, tx, b.CooperativeRef, false)
		if err != nil || a == nil {
			return nil, err
		}
		return &CoopWriteResult{Area: *a, Boundary: *b, Replayed: true}, nil
	}
}

// coopWrite ghi một dòng sổ ranh giới HTX, cập nhật bảng chiếu và phát sự kiện.
func (s *Service) coopWrite(ctx context.Context, tx pgx.Tx, b store.CoopBoundary, geojson, status, statusReason string) (CoopWriteResult, error) {
	if err := store.InsertCoopBoundary(ctx, tx, &b, geojson); err != nil {
		return CoopWriteResult{}, err
	}
	if err := store.PutCoopArea(ctx, tx, b, status, statusReason); err != nil {
		return CoopWriteResult{}, err
	}
	if err := enqueue(ctx, tx, b.TenantID, port.EventCooperativeBoundaryRecorded, b.EventTime, port.CooperativeBoundaryRecordedV1{
		CooperativeRef: b.CooperativeRef, BoundaryID: b.ID, Version: b.Version, Action: b.Action, Status: status,
		AreaHa: b.AreaHa.StringFixed(4), PointCount: b.PointCount, Source: b.Source, CommuneCode: b.CommuneCode,
		SupersedesID: b.SupersedesID, CorrectionReason: b.CorrectionReason, ActorRef: b.ActorRef,
		EventTime: b.EventTime.Format(time.RFC3339),
	}); err != nil {
		return CoopWriteResult{}, err
	}
	a, err := store.CoopAreaByRef(ctx, tx, b.CooperativeRef, false)
	if err != nil {
		return CoopWriteResult{}, err
	}
	return CoopWriteResult{Area: *a, Boundary: b}, nil
}

// canWriteCoop kiểm người gọi được ghi ranh giới HTX coop thuộc đơn vị unit.
func (a access) canWriteCoop(unit string, coop uuid.UUID) error {
	if a.coopOnly && coop != a.p.OrgRef {
		return ErrOutOfScope.WithDetail("quản lý HTX chỉ vẽ ranh giới cho chính HTX của mình")
	}
	if !a.sc.Allows(unit) {
		return ErrOutOfScope
	}
	return nil
}

// CreateCoopBoundary ghi ranh giới đầu tiên của HTX (GIS US-BND-02 AC-1).
func (s *Service) CreateCoopBoundary(ctx context.Context, in CoopBoundaryInput) (CoopWriteResult, error) {
	a, err := s.access(ctx, coopWriteRoles...)
	if err != nil {
		return CoopWriteResult{}, err
	}
	return s.createCoop(ctx, a, in)
}

func (s *Service) createCoop(ctx context.Context, a access, in CoopBoundaryInput) (CoopWriteResult, error) {
	if in.IdemKey == "" {
		return CoopWriteResult{}, problem.IdempotencyRequired
	}
	if a.coopOnly && in.CooperativeRef == uuid.Nil {
		in.CooperativeRef = a.p.OrgRef
	}
	unit, err := s.coopUnit(ctx, in.CooperativeRef, in.CommuneCode)
	if err != nil {
		return CoopWriteResult{}, err
	}
	if err := a.canWriteCoop(unit, in.CooperativeRef); err != nil {
		return CoopWriteResult{}, err
	}
	if err := s.prepareCoopShape(&in.Shape, a.coopOnly); err != nil {
		return CoopWriteResult{}, err
	}
	res, replayed, err := writeOnce(ctx, s, a.p.TenantID, s.coopReplay(ctx, in.IdemKey), func(tx pgx.Tx) (CoopWriteResult, error) {
		cur, err := store.CoopAreaByRef(ctx, tx, in.CooperativeRef, true)
		if err != nil {
			return CoopWriteResult{}, err
		}
		if cur != nil {
			return CoopWriteResult{}, ErrCoopBoundaryExists.With("current_boundary_id", cur.CurrentBoundaryID.String()).
				With("current_version", cur.CurrentVersion).With("status", cur.Status)
		}
		gj, err := checkCoopShape(ctx, tx, in.Shape.Parsed)
		if err != nil {
			return CoopWriteResult{}, err
		}
		return s.coopWrite(ctx, tx, store.CoopBoundary{
			ID: in.Shape.ID, TenantID: a.p.TenantID, CooperativeRef: in.CooperativeRef, Action: domain.ActionRecorded,
			PointCount: in.Shape.Points, Source: in.Shape.Source, CommuneCode: unit, EventTime: in.Shape.EventTime,
			ActorRef: a.p.ActorRef, DeviceID: in.Shape.DeviceID, IdemKey: in.IdemKey,
		}, gj, domain.StatusActive, "")
	})
	res.Replayed = replayed
	return res, err
}

// CorrectCoopBoundary ghi phiên bản ranh giới HTX mới (bản ghi bù, GIS US-BND-03 AC-1).
func (s *Service) CorrectCoopBoundary(ctx context.Context, in CoopCorrectInput) (CoopWriteResult, error) {
	a, err := s.access(ctx, coopWriteRoles...)
	if err != nil {
		return CoopWriteResult{}, err
	}
	return s.correctCoop(ctx, a, in)
}

func (s *Service) correctCoop(ctx context.Context, a access, in CoopCorrectInput) (CoopWriteResult, error) {
	if in.IdemKey == "" {
		return CoopWriteResult{}, problem.IdempotencyRequired
	}
	if in.SupersedesID == uuid.Nil {
		return CoopWriteResult{}, problem.Validation.WithField("supersedes_id", "bắt buộc: phiên bản ranh giới đang được thay")
	}
	why, err := reason("correction_reason", in.Reason)
	if err != nil {
		return CoopWriteResult{}, err
	}
	if err := s.prepareCoopShape(&in.Shape, a.coopOnly); err != nil {
		return CoopWriteResult{}, err
	}
	res, replayed, err := writeOnce(ctx, s, a.p.TenantID, s.coopReplay(ctx, in.IdemKey), func(tx pgx.Tx) (CoopWriteResult, error) {
		cur, err := s.lockCoop(ctx, tx, a, in.CooperativeRef)
		if err != nil {
			return CoopWriteResult{}, err
		}
		if cur.Status == domain.StatusRetired {
			return CoopWriteResult{}, ErrFeatureState.WithDetail("ranh giới HTX đã ngừng dùng — khôi phục trước khi sửa")
		}
		if in.SupersedesID != cur.CurrentBoundaryID {
			return CoopWriteResult{}, ErrVersionConflict.With("current_boundary_id", cur.CurrentBoundaryID.String()).
				With("current_version", cur.CurrentVersion)
		}
		gj, err := checkCoopShape(ctx, tx, in.Shape.Parsed)
		if err != nil {
			return CoopWriteResult{}, err
		}
		sup := in.SupersedesID
		return s.coopWrite(ctx, tx, store.CoopBoundary{
			ID: in.Shape.ID, TenantID: a.p.TenantID, CooperativeRef: in.CooperativeRef, Action: domain.ActionRecorded,
			PointCount: in.Shape.Points, Source: in.Shape.Source, CommuneCode: cur.CommuneCode, EventTime: in.Shape.EventTime,
			SupersedesID: &sup, CorrectionReason: &why, ActorRef: a.p.ActorRef, DeviceID: in.Shape.DeviceID, IdemKey: in.IdemKey,
		}, gj, domain.StatusActive, "")
	})
	res.Replayed = replayed
	return res, err
}

// lockCoop khóa ranh giới HTX hiện hành và kiểm quyền ghi (ngoài phạm vi đọc → 404).
func (s *Service) lockCoop(ctx context.Context, tx pgx.Tx, a access, coop uuid.UUID) (*store.CoopArea, error) {
	cur, err := store.CoopAreaByRef(ctx, tx, coop, true)
	if err != nil {
		return nil, err
	}
	if cur == nil || !a.sees(cur.CommuneCode, cur.CooperativeRef) {
		return nil, ErrCoopBoundaryNotFound
	}
	if err := a.canWriteCoop(cur.CommuneCode, coop); err != nil {
		return nil, err
	}
	return cur, nil
}

// RetireCoopBoundary ngừng dùng ranh giới HTX (xóa mềm, GIS US-BND-03 AC-2, BR-14):
// dòng sổ mới mang lại hình đang có, lý do và người xóa; ẩn khỏi bản đồ dùng chung.
func (s *Service) RetireCoopBoundary(ctx context.Context, in CoopStatusInput) (CoopWriteResult, error) {
	return s.coopStatus(ctx, in, domain.ActionRetired)
}

// RestoreCoopBoundary khôi phục ranh giới HTX đã ngừng dùng (BR-14 "khôi phục khi cần").
func (s *Service) RestoreCoopBoundary(ctx context.Context, in CoopStatusInput) (CoopWriteResult, error) {
	return s.coopStatus(ctx, in, domain.ActionRestored)
}

func (s *Service) coopStatus(ctx context.Context, in CoopStatusInput, action string) (CoopWriteResult, error) {
	a, err := s.access(ctx, coopWriteRoles...)
	if err != nil {
		return CoopWriteResult{}, err
	}
	if in.IdemKey == "" {
		return CoopWriteResult{}, problem.IdempotencyRequired
	}
	why, err := reason("reason", in.Reason)
	if err != nil {
		return CoopWriteResult{}, err
	}
	now := s.now()
	res, replayed, err := writeOnce(ctx, s, a.p.TenantID, s.coopReplay(ctx, in.IdemKey), func(tx pgx.Tx) (CoopWriteResult, error) {
		cur, err := s.lockCoop(ctx, tx, a, in.CooperativeRef)
		if err != nil {
			return CoopWriteResult{}, err
		}
		status, statusReason := domain.StatusRetired, why
		if action == domain.ActionRetired && cur.Status == domain.StatusRetired {
			return CoopWriteResult{}, ErrFeatureState.WithDetail("ranh giới HTX đã ngừng dùng")
		}
		if action == domain.ActionRestored {
			if cur.Status != domain.StatusRetired {
				return CoopWriteResult{}, ErrFeatureState.WithDetail("ranh giới HTX chưa ngừng dùng")
			}
			status, statusReason = domain.StatusActive, ""
		}
		sup := cur.CurrentBoundaryID
		prev, err := store.CoopBoundaries(ctx, tx, in.CooperativeRef)
		if err != nil {
			return CoopWriteResult{}, err
		}
		points := 4
		for _, p := range prev {
			if p.ID == sup {
				points = p.PointCount
			}
		}
		return s.coopWrite(ctx, tx, store.CoopBoundary{
			ID: ids.New(), TenantID: a.p.TenantID, CooperativeRef: in.CooperativeRef, Action: action, PointCount: points,
			Source: cur.Source, CommuneCode: cur.CommuneCode, EventTime: now, SupersedesID: &sup, CorrectionReason: &why,
			ActorRef: a.p.ActorRef, IdemKey: in.IdemKey,
		}, "", status, statusReason)
	})
	res.Replayed = replayed
	return res, err
}

// CoopDetail là ranh giới HTX hiện hành kèm toàn bộ phiên bản (lịch sử chỉ đọc).
type CoopDetail struct {
	Area     store.CoopArea
	Versions []store.CoopBoundary
}

// GetCoopBoundary đọc ranh giới HTX; ngoài phạm vi → 404 (không lộ sự tồn tại).
func (s *Service) GetCoopBoundary(ctx context.Context, coop uuid.UUID) (CoopDetail, error) {
	a, err := s.access(ctx, layerReadRoles...)
	if err != nil {
		return CoopDetail{}, err
	}
	var d CoopDetail
	found := false
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.CoopAreaByRef(ctx, tx, coop, false)
		if err != nil || cur == nil || !a.sees(cur.CommuneCode, cur.CooperativeRef) {
			return err
		}
		found, d.Area = true, *cur
		d.Versions, err = store.CoopBoundaries(ctx, tx, coop)
		return err
	})
	if err != nil {
		return CoopDetail{}, err
	}
	if !found {
		return CoopDetail{}, ErrCoopBoundaryNotFound
	}
	return d, nil
}

// CoopLayer là lớp HTX của khung nhìn: vùng ranh giới và marker.
type CoopLayer struct {
	Items []store.CoopArea
	// Markers: điểm HTX từ pf/party — cho HTX có ranh giới (marker ưu tiên) và HTX
	// chưa có ranh giới (chỉ marker, GIS US-BND-02 "gắn Marker").
	Markers   map[uuid.UUID]port.CoopMarker
	Unbounded []port.CoopMarker
	Truncated bool
}

// CoopBoundariesInBBox trả ranh giới HTX đang dùng giao khung nhìn trong phạm vi
// (GIS FN-04 B2; tối đa 1.000 — BR-25). Hình được đơn giản hóa theo zoom.
func (s *Service) CoopBoundariesInBBox(ctx context.Context, in BBoxInput) (CoopLayer, error) {
	a, err := s.access(ctx, layerReadRoles...)
	if err != nil {
		return CoopLayer{}, err
	}
	return s.coopLayer(ctx, a, in)
}

func (s *Service) coopLayer(ctx context.Context, a access, in BBoxInput) (CoopLayer, error) {
	out := CoopLayer{Items: []store.CoopArea{}, Markers: map[uuid.UUID]port.CoopMarker{}, Unbounded: []port.CoopMarker{}}
	limit, err := viewportLimit(in.Limit)
	if err != nil {
		return out, err
	}
	bb := store.BBox{MinLon: in.MinLon, MinLat: in.MinLat, MaxLon: in.MaxLon, MaxLat: in.MaxLat}
	if err := validBBox(bb); err != nil {
		return out, err
	}
	if a.sc.Empty() {
		return out, nil
	}
	var items []store.CoopArea
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListCoopAreasBBox(ctx, tx, store.CoopBBoxQuery{
			Filter: a.filter(), MinLon: in.MinLon, MinLat: in.MinLat, MaxLon: in.MaxLon, MaxLat: in.MaxLat,
			SimplifyDeg: SimplifyTolerance(in.Zoom), Limit: limit + 1,
		})
		return e
	})
	if err != nil {
		return out, err
	}
	if len(items) > limit {
		items, out.Truncated = items[:limit], true
	}
	if items != nil {
		out.Items = items
	}
	if s.Markers == nil {
		return out, nil
	}
	ms, err := s.Markers.MarkersInBBox(ctx, in.MinLon, in.MinLat, in.MaxLon, in.MaxLat, limit)
	if err != nil {
		return out, ErrDependency.WithDetail("không đọc được vị trí HTX (pf/party)").WithCause(err)
	}
	has := map[uuid.UUID]bool{}
	for _, it := range out.Items {
		has[it.CooperativeRef] = true
	}
	for _, m := range ms {
		if !a.sc.Allows(m.CommuneCode) || (a.coopOnly && m.Ref != a.p.OrgRef) {
			continue
		}
		if has[m.Ref] {
			out.Markers[m.Ref] = m
		} else if len(out.Items)+len(out.Unbounded) < limit {
			out.Unbounded = append(out.Unbounded, m)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Nhập hàng loạt ranh giới HTX (GIS US-BND-07)
// ---------------------------------------------------------------------------

// CoopImportInput là tệp GeoJSON/KML/Shapefile ranh giới HTX.
type CoopImportInput struct {
	Format   string
	FileName string
	Data     []byte
	// Reason: lý do khi đối tượng thay ranh giới HTX đang có (mặc định nêu tên tệp).
	Reason  string
	IdemKey string
}

// CoopImportFeature là kết quả một đối tượng.
type CoopImportFeature struct {
	Index          int
	Name           string
	Status         string // accepted | duplicate | rejected
	Replaced       bool   // đã thay phiên bản đang có (phiên bản cũ giữ trong sổ)
	Code           string
	Reason         string
	CooperativeRef uuid.UUID
	BoundaryID     uuid.UUID
	Version        int64
	AreaHa         *decimal.Decimal
}

// CoopImportResult là kết quả cả tệp.
type CoopImportResult struct {
	Format                        string
	Accepted, Duplicate, Rejected int
	Features                      []CoopImportFeature
}

// ImportCoopBoundaries nạp ranh giới HTX từ tệp (Admin API). Mỗi đối tượng vùng
// phải chỉ ra HTX bằng cooperative_ref hoặc Mã HTX (cooperative_code/ma_htx).
// Chỉ nhận WGS84 — hệ khác → geo.crs_unsupported cho cả tệp. HTX đã có ranh giới
// → phiên bản mới thay phiên bản hiện hành kèm lý do.
func (s *Service) ImportCoopBoundaries(ctx context.Context, in CoopImportInput) (CoopImportResult, error) {
	a, err := s.access(ctx, adminRoles...)
	if err != nil {
		return CoopImportResult{}, err
	}
	if in.IdemKey == "" {
		return CoopImportResult{}, problem.IdempotencyRequired
	}
	why := strings.TrimSpace(in.Reason)
	if why == "" {
		why = "Nhập hàng loạt ranh giới HTX từ tệp " + strings.TrimSpace(in.FileName)
	}
	if !domain.ValidReason(why) {
		return CoopImportResult{}, problem.Validation.WithField("reason", "ít nhất 10 ký tự")
	}
	format := domain.DetectFormat(in.Format, in.FileName, in.Data)
	feats, err := domain.ParseFeatures(format, in.Data)
	if err != nil {
		return CoopImportResult{}, violationProblem(err)
	}
	res := CoopImportResult{Format: format}
	codes := map[string]uuid.UUID{}
	for _, f := range feats {
		r := CoopImportFeature{Index: f.Index, Name: f.Name}
		reject := func(err error) {
			r.Status = FeatureRejected
			r.Code, r.Reason = rejectReason(err)
			res.Rejected++
			res.Features = append(res.Features, r)
		}
		if f.Err != nil {
			reject(violationProblem(f.Err))
			continue
		}
		coop, ok, perr := propUUID(f.Props, "cooperative_ref")
		switch {
		case perr != nil:
			reject(problem.Validation.WithField("cooperative_ref", "phải là UUID"))
			continue
		case !ok:
			code := firstOf(f.Props, "cooperative_code", "ma_htx")
			if code == "" {
				reject(problem.Validation.WithDetail(domain.MsgCoopRequired).WithField("properties.cooperative_ref", "bắt buộc: cooperative_ref hoặc Mã HTX"))
				continue
			}
			ref, err := s.coopByCode(ctx, code, codes)
			if err != nil {
				var pr *problem.Problem
				if !errors.As(err, &pr) || pr.Status >= 500 {
					return CoopImportResult{}, err
				}
				reject(err)
				continue
			}
			coop = ref
		}
		r.CooperativeRef = coop
		key := in.IdemKey + ":" + strconv.Itoa(f.Index)
		shape := CoopShapeInput{Parsed: f.Geom, Points: f.Points, Source: domain.CoopSourceImportFile}
		out, err := s.importCoop(ctx, a, coop, firstOf(f.Props, "commune_code", "ma_xa"), shape, why, key)
		if err != nil {
			var pr *problem.Problem
			if !errors.As(err, &pr) || pr.Status >= 500 {
				return CoopImportResult{}, err
			}
			reject(err)
			continue
		}
		r.BoundaryID, r.Version, r.Replaced = out.Boundary.ID, out.Boundary.Version, out.Boundary.SupersedesID != nil
		area := out.Boundary.AreaHa
		r.AreaHa = &area
		r.Status = FeatureAccepted
		if out.Replayed {
			r.Status = FeatureDuplicate
			res.Duplicate++
		} else {
			res.Accepted++
		}
		res.Features = append(res.Features, r)
	}
	return res, nil
}

// importCoop ghi ranh giới đầu tiên, hoặc phiên bản thay phiên bản hiện hành.
func (s *Service) importCoop(ctx context.Context, a access, coop uuid.UUID, commune string, shape CoopShapeInput, why, key string) (CoopWriteResult, error) {
	unit, err := s.coopUnit(ctx, coop, commune)
	if err != nil {
		return CoopWriteResult{}, err
	}
	if err := a.canWriteCoop(unit, coop); err != nil {
		return CoopWriteResult{}, err
	}
	var cur *store.CoopArea
	var prev *CoopWriteResult
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if prev, e = s.coopReplay(ctx, key)(tx); e != nil || prev != nil {
			return e
		}
		cur, e = store.CoopAreaByRef(ctx, tx, coop, false)
		return e
	})
	switch {
	case err != nil:
		return CoopWriteResult{}, err
	case prev != nil:
		return *prev, nil
	case cur == nil:
		return s.createCoop(ctx, a, CoopBoundaryInput{CooperativeRef: coop, CommuneCode: commune, Shape: shape, IdemKey: key})
	}
	if cur.Status == domain.StatusRetired {
		return CoopWriteResult{}, ErrFeatureState.WithDetail("ranh giới HTX đã ngừng dùng — khôi phục trước khi nhập đè")
	}
	return s.correctCoop(ctx, a, CoopCorrectInput{CooperativeRef: coop, SupersedesID: cur.CurrentBoundaryID, Reason: why, Shape: shape, IdemKey: key})
}
