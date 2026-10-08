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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Cơ sở kho / Hub / nhà máy của tenant (kho bên thứ ba...) — GIS US-BND-05/06/08,
// BRD FN-04 (danh sách Hub riêng với HTX) và FN-06. Hub của MKG KHÔNG ghi ở đây:
// chúng đến qua van công bố vào hub_view (port.HubSink).

// FacilityInput là thuộc tính cơ sở gửi lên. Con trỏ nil (khi sửa) = giữ giá trị hiện hành.
type FacilityInput struct {
	Ref          uuid.UUID // tạo: Nil → server sinh
	Kind         *string
	Code         *string // tạo: rỗng → sinh từ loại + ref
	Name         *string
	Point        json.RawMessage // GeoJSON Point (marker)
	Polygon      json.RawMessage // GeoJSON Polygon/MultiPolygon (khuôn viên), tùy chọn
	RemovePoly   bool            // sửa: bỏ khuôn viên đang có
	ProvinceCode *string
	CommuneCode  *string
	// Capacity: sức chứa danh định; CapacityBad: giá trị gửi lên không phải số (thông báo AC-2).
	Capacity    *decimal.Decimal
	CapacityBad bool
	Unit        *string
	EventTime   time.Time
	DeviceID    *string
	// Sửa: phiên bản đang thay + lý do.
	SupersedesID uuid.UUID
	Reason       string
	IdemKey      string

	parsedPoint *domain.Position
	parsedPoly  domain.MultiPolygon
}

// FacilityWriteResult là kết quả ghi cơ sở.
type FacilityWriteResult struct {
	Facility store.Facility
	Event    store.FacilityEvent
	Replayed bool
}

func strOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return strings.TrimSpace(*p)
}

// facilityState là thuộc tính sau khi áp input lên hiện trạng (hoặc rỗng khi tạo).
type facilityState struct {
	kind, code, name, province, commune, unit string
	capacity                                  decimal.Decimal
}

// resolveFacility kiểm và gộp input với hiện trạng cur (nil khi tạo).
func (s *Service) resolveFacility(ctx context.Context, a access, in *FacilityInput, cur *store.Facility) (facilityState, error) {
	var st facilityState
	if cur != nil {
		st = facilityState{kind: cur.Kind, code: cur.Code, name: cur.Name, province: cur.ProvinceCode,
			commune: cur.CommuneCode, unit: cur.Unit, capacity: cur.Capacity}
	}
	st.kind = strOr(in.Kind, st.kind)
	st.code = strOr(in.Code, st.code)
	st.name = strOr(in.Name, st.name)
	st.province = strOr(in.ProvinceCode, st.province)
	st.commune = strOr(in.CommuneCode, st.commune)
	st.unit = strOr(in.Unit, st.unit)
	if st.unit == "" {
		st.unit = domain.UnitTon
	}
	f := problem.Validation
	bad := false
	add := func(field, why string) { f, bad = f.WithField(field, why), true }
	// GIS US-BND-05/06 AC-2: thiếu tên/địa chỉ → "Vui lòng nhập đầy đủ Tên Hub và Địa chỉ."
	// Địa chỉ = tỉnh + xã (mã đơn vị hành chính); không lưu địa chỉ chữ ngoài vault (R-04).
	if st.name == "" || st.province == "" || st.commune == "" {
		f = f.WithDetail(domain.MsgFacilityRequired)
		if st.name == "" {
			add("name", "bắt buộc")
		}
		if st.province == "" {
			add("province_code", "bắt buộc (địa chỉ)")
		}
		if st.commune == "" {
			add("commune_code", "bắt buộc (địa chỉ)")
		}
	}
	switch {
	case in.CapacityBad || (in.Capacity == nil && cur == nil) || (in.Capacity != nil && !in.Capacity.IsPositive()):
		if !bad {
			f = f.WithDetail(domain.MsgCapacityPositive)
		}
		add("capacity", domain.MsgCapacityPositive)
	case in.Capacity != nil:
		st.capacity = *in.Capacity
	}
	if len([]rune(st.name)) > 200 {
		add("name", "tối đa 200 ký tự")
	}
	if !domain.KnownFacilityKind(st.kind) {
		add("kind", "phải là kho, hub hoặc nha_may")
	}
	if !domain.KnownUnit(st.unit) {
		add("unit", "phải là tan, m3 hoặc ke")
	}
	if len(in.Point) > 0 && string(in.Point) != "null" {
		p, err := domain.ParsePoint(in.Point)
		if err != nil {
			return st, violationProblem(err)
		}
		in.parsedPoint = p
	}
	if len(in.Polygon) > 0 && string(in.Polygon) != "null" {
		mp, _, err := domain.ParseGeometry(in.Polygon)
		if err != nil {
			return st, violationProblem(err)
		}
		in.parsedPoly = mp
	}
	if cur == nil && in.parsedPoint == nil && in.parsedPoly == nil {
		add("point", "bắt buộc: gắn Marker vị trí hoặc vẽ ranh giới khuôn viên")
	}
	if bad {
		return st, f
	}
	if st.code == "" {
		st.code = strings.ToUpper(strings.ReplaceAll(st.kind, "_", "")) + "-" + strings.ToUpper(in.Ref.String()[len(in.Ref.String())-8:])
	}
	if !facilityCodeRe(st.code) {
		return st, problem.Validation.WithField("code", "1–40 ký tự chữ, số, '.', '_', '-'")
	}
	if err := s.unitAt(ctx, "province_code", st.province, partyport.LevelProvince); err != nil {
		return st, err
	}
	if err := s.unitAt(ctx, "commune_code", st.commune, partyport.LevelCommune); err != nil {
		return st, err
	}
	if !a.sc.Allows(st.commune) {
		return st, ErrOutOfScope
	}
	return st, nil
}

func facilityCodeRe(c string) bool {
	if c == "" || len(c) > 40 {
		return false
	}
	for i, r := range c {
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && (r == '.' || r == '_' || r == '-'))
		if !ok {
			return false
		}
	}
	return true
}

func (s *Service) facilityReplay(ctx context.Context, key string) func(pgx.Tx) (*FacilityWriteResult, error) {
	return func(tx pgx.Tx) (*FacilityWriteResult, error) {
		e, err := store.FacilityEventByIdemKey(ctx, tx, key)
		if err != nil || e == nil {
			return nil, err
		}
		f, err := store.FacilityByRef(ctx, tx, e.FacilityRef, false)
		if err != nil || f == nil {
			return nil, err
		}
		return &FacilityWriteResult{Facility: *f, Event: *e, Replayed: true}, nil
	}
}

// facilityWrite ghi dòng sổ, bảng chiếu, sự kiện trong giao dịch đang mở.
func (s *Service) facilityWrite(ctx context.Context, tx pgx.Tx, e store.FacilityEvent, in *FacilityInput, statusReason string) (FacilityWriteResult, error) {
	pointGJ, polyGJ := "", ""
	if in != nil && in.parsedPoint != nil {
		pointGJ = domain.PointGeoJSON(*in.parsedPoint)
	}
	keepPoly := in == nil || !in.RemovePoly
	if in != nil && in.parsedPoly != nil {
		gj, err := checkFacilityPoly(ctx, tx, in.parsedPoly)
		if err != nil {
			return FacilityWriteResult{}, err
		}
		polyGJ = gj
	}
	if err := store.InsertFacilityEvent(ctx, tx, &e, pointGJ, polyGJ, keepPoly); err != nil {
		return FacilityWriteResult{}, err
	}
	if err := store.PutFacility(ctx, tx, e.ID, statusReason); err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return FacilityWriteResult{}, ErrFacilityCodeExists
		}
		return FacilityWriteResult{}, err
	}
	var commune *string
	if e.CommuneCode != "" {
		c := e.CommuneCode
		commune = &c
	}
	if err := enqueue(ctx, tx, e.TenantID, port.EventFacilityRecorded, e.EventTime, port.FacilityRecordedV1{
		FacilityRef: e.FacilityRef, EventID: e.ID, Version: e.Version, Action: e.Action, Kind: e.Kind, Code: e.Code, Name: e.Name,
		Status: e.Status, ProvinceCode: e.ProvinceCode, CommuneCode: commune,
		Lon: strconv.FormatFloat(e.Lon, 'f', 7, 64), Lat: strconv.FormatFloat(e.Lat, 'f', 7, 64), HasPolygon: e.HasPolygon,
		Capacity: e.Capacity.StringFixed(3), Unit: e.Unit, SupersedesID: e.SupersedesID, CorrectionReason: e.CorrectionReason,
		ActorRef: e.ActorRef, EventTime: e.EventTime.Format(time.RFC3339),
	}); err != nil {
		return FacilityWriteResult{}, err
	}
	f, err := store.FacilityByRef(ctx, tx, e.FacilityRef, false)
	if err != nil {
		return FacilityWriteResult{}, err
	}
	return FacilityWriteResult{Facility: *f, Event: e}, nil
}

func checkFacilityPoly(ctx context.Context, tx pgx.Tx, mp domain.MultiPolygon) (string, error) {
	gj := mp.GeoJSON()
	an, err := store.Analyze(ctx, tx, gj)
	if err != nil {
		return "", err
	}
	if !an.Valid {
		return "", ErrInvalidGeometry.WithDetail("Ranh giới khuôn viên tự cắt hoặc sai cấu trúc (" + an.Reason + ")")
	}
	if !an.AreaHa.IsPositive() || an.AreaHa.GreaterThan(domain.MaxAreaHa) {
		return "", ErrAreaOutOfRange.WithDetail("Diện tích khuôn viên " + an.AreaHa.String() + " ha ngoài giới hạn")
	}
	return gj, nil
}

// CreateFacility thêm cơ sở kho/Hub/nhà máy (GIS US-BND-05 AC-1; Admin API).
func (s *Service) CreateFacility(ctx context.Context, in FacilityInput) (FacilityWriteResult, error) {
	a, err := s.access(ctx, adminRoles...)
	if err != nil {
		return FacilityWriteResult{}, err
	}
	return s.createFacility(ctx, a, in)
}

func (s *Service) createFacility(ctx context.Context, a access, in FacilityInput) (FacilityWriteResult, error) {
	if in.IdemKey == "" {
		return FacilityWriteResult{}, problem.IdempotencyRequired
	}
	if in.Ref == uuid.Nil {
		in.Ref = ids.New()
	} else if in.Ref.Version() != 7 {
		return FacilityWriteResult{}, problem.Validation.WithField("ref", "phải là UUIDv7")
	}
	st, err := s.resolveFacility(ctx, a, &in, nil)
	if err != nil {
		return FacilityWriteResult{}, err
	}
	at, err := s.eventTime(in.EventTime)
	if err != nil {
		return FacilityWriteResult{}, err
	}
	res, replayed, err := writeOnce(ctx, s, a.p.TenantID, s.facilityReplay(ctx, in.IdemKey), func(tx pgx.Tx) (FacilityWriteResult, error) {
		if ex, err := store.FacilityByCode(ctx, tx, st.code); err != nil {
			return FacilityWriteResult{}, err
		} else if ex != nil {
			return FacilityWriteResult{}, ErrFacilityCodeExists.With("facility_ref", ex.Ref.String())
		}
		if ex, err := store.FacilityByRef(ctx, tx, in.Ref, false); err != nil {
			return FacilityWriteResult{}, err
		} else if ex != nil {
			return FacilityWriteResult{}, ErrFacilityCodeExists.WithDetail("Mã tham chiếu kho/Hub đã được dùng")
		}
		return s.facilityWrite(ctx, tx, store.FacilityEvent{
			ID: ids.New(), TenantID: a.p.TenantID, FacilityRef: in.Ref, Action: domain.ActionFacilityCreated, Kind: st.kind,
			Code: st.code, Name: st.name, ProvinceCode: st.province, CommuneCode: st.commune,
			Capacity: st.capacity, Unit: st.unit, Status: domain.StatusActive, EventTime: at, ActorRef: a.p.ActorRef,
			DeviceID: in.DeviceID, IdemKey: in.IdemKey,
		}, &in, "")
	})
	res.Replayed = replayed
	return res, err
}

// UpdateFacility ghi phiên bản thuộc tính mới (GIS US-BND-06 AC-1) — bản ghi bù
// trỏ dòng sổ hiện hành kèm lý do; sai phiên bản → geo.version_conflict.
func (s *Service) UpdateFacility(ctx context.Context, ref uuid.UUID, in FacilityInput) (FacilityWriteResult, error) {
	a, err := s.access(ctx, adminRoles...)
	if err != nil {
		return FacilityWriteResult{}, err
	}
	if in.IdemKey == "" {
		return FacilityWriteResult{}, problem.IdempotencyRequired
	}
	if in.SupersedesID == uuid.Nil {
		return FacilityWriteResult{}, problem.Validation.WithField("supersedes_id", "bắt buộc: phiên bản đang được thay (current_event_id)")
	}
	why, err := reason("correction_reason", in.Reason)
	if err != nil {
		return FacilityWriteResult{}, err
	}
	at, err := s.eventTime(in.EventTime)
	if err != nil {
		return FacilityWriteResult{}, err
	}
	in.Ref = ref
	res, replayed, err := writeOnce(ctx, s, a.p.TenantID, s.facilityReplay(ctx, in.IdemKey), func(tx pgx.Tx) (FacilityWriteResult, error) {
		cur, err := store.FacilityByRef(ctx, tx, ref, true)
		if err != nil {
			return FacilityWriteResult{}, err
		}
		if cur == nil {
			return FacilityWriteResult{}, ErrFacilityNotFound
		}
		if cur.Status == domain.StatusRetired {
			return FacilityWriteResult{}, ErrFeatureState.WithDetail("kho/Hub đã ngừng dùng — khôi phục trước khi sửa")
		}
		if in.SupersedesID != cur.CurrentEventID {
			return FacilityWriteResult{}, ErrVersionConflict.With("current_event_id", cur.CurrentEventID.String()).
				With("current_version", cur.CurrentVersion)
		}
		if err := s.facilityScope(a, cur); err != nil {
			return FacilityWriteResult{}, err
		}
		st, err := s.resolveFacility(ctx, a, &in, cur)
		if err != nil {
			return FacilityWriteResult{}, err
		}
		if st.code != cur.Code {
			if ex, err := store.FacilityByCode(ctx, tx, st.code); err != nil {
				return FacilityWriteResult{}, err
			} else if ex != nil {
				return FacilityWriteResult{}, ErrFacilityCodeExists.With("facility_ref", ex.Ref.String())
			}
		}
		sup := cur.CurrentEventID
		return s.facilityWrite(ctx, tx, store.FacilityEvent{
			ID: ids.New(), TenantID: a.p.TenantID, FacilityRef: ref, Action: domain.ActionFacilityUpdated, Kind: st.kind,
			Code: st.code, Name: st.name, ProvinceCode: st.province, CommuneCode: st.commune,
			Capacity: st.capacity, Unit: st.unit, Status: domain.StatusActive, EventTime: at, SupersedesID: &sup,
			CorrectionReason: &why, ActorRef: a.p.ActorRef, DeviceID: in.DeviceID, IdemKey: in.IdemKey,
		}, &in, "")
	})
	res.Replayed = replayed
	return res, err
}

func (s *Service) facilityScope(a access, f *store.Facility) error {
	unit := f.ProvinceCode
	if f.CommuneCode != "" {
		unit = f.CommuneCode
	}
	if !a.sc.Allows(unit) {
		return ErrOutOfScope
	}
	return nil
}

// FacilityStatusInput ngừng dùng / khôi phục cơ sở.
type FacilityStatusInput struct {
	Ref     uuid.UUID
	Reason  string
	IdemKey string
}

// RetireFacility ngừng dùng cơ sở (xóa mềm, GIS US-BND-06 AC-2, BR-14/19).
func (s *Service) RetireFacility(ctx context.Context, in FacilityStatusInput) (FacilityWriteResult, error) {
	return s.facilityStatus(ctx, in, domain.ActionRetired)
}

// RestoreFacility khôi phục cơ sở đã ngừng dùng.
func (s *Service) RestoreFacility(ctx context.Context, in FacilityStatusInput) (FacilityWriteResult, error) {
	return s.facilityStatus(ctx, in, domain.ActionRestored)
}

func (s *Service) facilityStatus(ctx context.Context, in FacilityStatusInput, action string) (FacilityWriteResult, error) {
	a, err := s.access(ctx, adminRoles...)
	if err != nil {
		return FacilityWriteResult{}, err
	}
	if in.IdemKey == "" {
		return FacilityWriteResult{}, problem.IdempotencyRequired
	}
	why, err := reason("reason", in.Reason)
	if err != nil {
		return FacilityWriteResult{}, err
	}
	now := s.now()
	res, replayed, err := writeOnce(ctx, s, a.p.TenantID, s.facilityReplay(ctx, in.IdemKey), func(tx pgx.Tx) (FacilityWriteResult, error) {
		cur, err := store.FacilityByRef(ctx, tx, in.Ref, true)
		if err != nil {
			return FacilityWriteResult{}, err
		}
		if cur == nil {
			return FacilityWriteResult{}, ErrFacilityNotFound
		}
		if err := s.facilityScope(a, cur); err != nil {
			return FacilityWriteResult{}, err
		}
		status, statusReason := domain.StatusRetired, why
		switch {
		case action == domain.ActionRetired && cur.Status == domain.StatusRetired:
			return FacilityWriteResult{}, ErrFeatureState.WithDetail("kho/Hub đã ngừng dùng")
		case action == domain.ActionRestored && cur.Status != domain.StatusRetired:
			return FacilityWriteResult{}, ErrFeatureState.WithDetail("kho/Hub chưa ngừng dùng")
		case action == domain.ActionRestored:
			status, statusReason = domain.StatusActive, ""
		}
		sup := cur.CurrentEventID
		return s.facilityWrite(ctx, tx, store.FacilityEvent{
			ID: ids.New(), TenantID: a.p.TenantID, FacilityRef: cur.Ref, Action: action, Kind: cur.Kind, Code: cur.Code,
			Name: cur.Name, ProvinceCode: cur.ProvinceCode, CommuneCode: cur.CommuneCode,
			Capacity: cur.Capacity, Unit: cur.Unit, Status: status, EventTime: now, SupersedesID: &sup, CorrectionReason: &why,
			ActorRef: a.p.ActorRef, IdemKey: in.IdemKey,
		}, nil, statusReason)
	})
	res.Replayed = replayed
	return res, err
}

// ---------------------------------------------------------------------------
// Đọc: danh sách/lớp cơ sở + hub MKG, chi tiết (Event Log)
// ---------------------------------------------------------------------------

// FacilityItem là một cơ sở trên bản đồ: của tenant (Origin tenant) hoặc hub MKG (Origin mkg).
type FacilityItem struct {
	Origin       string // tenant | mkg
	Ref          uuid.UUID
	Kind         string
	Code         string
	Name         string
	Lon, Lat     float64
	ProvinceCode string
	CommuneCode  string
	Nominal      *decimal.Decimal // sức chứa danh định
	Unit         string
	Status       string
	Version      int64
	GeoJSON      string
	Capacity     CapacityView
}

// Nguồn gốc cơ sở trên bản đồ.
const (
	OriginTenant = "tenant"
	OriginMKG    = "mkg"
)

// FacilityListInput là truy vấn lớp cơ sở.
type FacilityListInput struct {
	BBoxInput
	Kind           string
	IncludeRetired bool
}

// FacilityPage là kết quả lớp cơ sở.
type FacilityPage struct {
	Items     []FacilityItem
	Truncated bool
	WarnPct   decimal.Decimal
	FullPct   decimal.Decimal
}

// FacilitiesInBBox trả cơ sở của tenant và hub MKG trong khung nhìn, kèm sức chứa
// hiệu lực và lớp màu (GIS FN-04 B2, US-WH-01 AC-1, US-HUB-01). Lớp hạ tầng là dữ
// liệu dùng chung — không lọc theo phạm vi, chỉ theo vai.
func (s *Service) FacilitiesInBBox(ctx context.Context, in FacilityListInput) (FacilityPage, error) {
	a, err := s.access(ctx, layerReadRoles...)
	if err != nil {
		return FacilityPage{}, err
	}
	return s.facilityLayer(ctx, a, in)
}

func (s *Service) facilityLayer(ctx context.Context, a access, in FacilityListInput) (FacilityPage, error) {
	limit, err := viewportLimit(in.Limit)
	if err != nil {
		return FacilityPage{}, err
	}
	bb := store.BBox{MinLon: in.MinLon, MinLat: in.MinLat, MaxLon: in.MaxLon, MaxLat: in.MaxLat}
	if err := validBBox(bb); err != nil {
		return FacilityPage{}, err
	}
	if in.Kind != "" && !domain.KnownFacilityKind(in.Kind) {
		return FacilityPage{}, problem.Validation.WithField("kind", "phải là kho, hub hoặc nha_may")
	}
	warn, full, err := s.hubThresholds(ctx, s.now())
	if err != nil {
		return FacilityPage{}, err
	}
	pg := FacilityPage{Items: []FacilityItem{}, WarnPct: warn, FullPct: full}
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		q := store.FacilityQuery{BBox: bb, Kind: in.Kind, IncludeRetired: in.IncludeRetired, Limit: limit + 1}
		fs, err := store.ListFacilitiesBBox(ctx, tx, q)
		if err != nil {
			return err
		}
		hs, err := store.ListHubsBBox(ctx, tx, q)
		if err != nil {
			return err
		}
		fRefs := make([]uuid.UUID, 0, len(fs))
		for _, f := range fs {
			fRefs = append(fRefs, f.Ref)
		}
		hRefs := make([]uuid.UUID, 0, len(hs))
		for _, h := range hs {
			hRefs = append(hRefs, h.HubRef)
		}
		fl, err := store.LatestFor(ctx, tx, domain.SubjectFacility, fRefs)
		if err != nil {
			return err
		}
		hl, err := store.LatestFor(ctx, tx, domain.SubjectHub, hRefs)
		if err != nil {
			return err
		}
		for _, f := range fs {
			nominal := f.Capacity
			pg.Items = append(pg.Items, FacilityItem{
				Origin: OriginTenant, Ref: f.Ref, Kind: f.Kind, Code: f.Code, Name: f.Name, Lon: f.Lon, Lat: f.Lat,
				ProvinceCode: f.ProvinceCode, CommuneCode: f.CommuneCode, Nominal: &nominal, Unit: f.Unit, Status: f.Status,
				Version: f.CurrentVersion, GeoJSON: f.GeoJSON,
				Capacity: effectiveCapacity(fl[store.SubjectKey{Kind: domain.SubjectFacility, Ref: f.Ref}], warn, full),
			})
		}
		for _, h := range hs {
			pg.Items = append(pg.Items, FacilityItem{
				Origin: OriginMKG, Ref: h.HubRef, Kind: h.Kind, Code: h.Code, Name: h.Name, Lon: h.Lon, Lat: h.Lat,
				ProvinceCode: h.ProvinceCode, CommuneCode: h.CommuneCode, Nominal: h.Capacity, Unit: h.Unit, Status: h.Status,
				Version: h.Version, GeoJSON: h.GeoJSON,
				Capacity: effectiveCapacity(hl[store.SubjectKey{Kind: domain.SubjectHub, Ref: h.HubRef}], warn, full),
			})
		}
		return nil
	})
	if err != nil {
		return FacilityPage{}, err
	}
	if len(pg.Items) > limit {
		pg.Items, pg.Truncated = pg.Items[:limit], true
	}
	return pg, nil
}

// FacilityDetail là cơ sở kèm Event Log và sức chứa.
type FacilityDetail struct {
	Facility store.Facility
	Events   []store.FacilityEvent
	Capacity CapacityView
	History  []store.CapacitySnapshot
}

// GetFacility đọc cơ sở của tenant kèm lịch sử (GIS BR-14/19).
func (s *Service) GetFacility(ctx context.Context, ref uuid.UUID) (FacilityDetail, error) {
	a, err := s.access(ctx, layerReadRoles...)
	if err != nil {
		return FacilityDetail{}, err
	}
	warn, full, err := s.hubThresholds(ctx, s.now())
	if err != nil {
		return FacilityDetail{}, err
	}
	var d FacilityDetail
	found := false
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		f, err := store.FacilityByRef(ctx, tx, ref, false)
		if err != nil || f == nil {
			return err
		}
		found, d.Facility = true, *f
		if d.Events, err = store.FacilityEvents(ctx, tx, ref); err != nil {
			return err
		}
		l, err := store.LatestFor(ctx, tx, domain.SubjectFacility, []uuid.UUID{ref})
		if err != nil {
			return err
		}
		d.Capacity = effectiveCapacity(l[store.SubjectKey{Kind: domain.SubjectFacility, Ref: ref}], warn, full)
		d.History, err = store.CapacityHistory(ctx, tx, domain.SubjectFacility, ref, 100)
		return err
	})
	if err != nil {
		return FacilityDetail{}, err
	}
	if !found {
		return FacilityDetail{}, ErrFacilityNotFound
	}
	return d, nil
}

// ---------------------------------------------------------------------------
// Nhập hàng loạt vị trí kho/Hub (GIS US-BND-08)
// ---------------------------------------------------------------------------

// FacilityImportInput là tệp GeoJSON/KML/Shapefile vị trí kho/Hub (điểm hoặc vùng).
type FacilityImportInput struct {
	Format       string
	FileName     string
	Data         []byte
	Kind         string // mặc định khi đối tượng không có thuộc tính kind/loai
	ProvinceCode string // mặc định khi thiếu province_code/ma_tinh
	CommuneCode  string // mặc định khi thiếu commune_code/ma_xa
	Unit         string // mặc định khi thiếu unit/don_vi
	IdemKey      string
}

// FacilityImportFeature là kết quả một đối tượng.
type FacilityImportFeature struct {
	Index        int
	Name         string
	Status       string
	Code         string // mã lỗi khi rejected
	Reason       string
	FacilityRef  uuid.UUID
	FacilityCode string
}

// FacilityImportResult là kết quả cả tệp.
type FacilityImportResult struct {
	Format                        string
	Accepted, Duplicate, Rejected int
	Features                      []FacilityImportFeature
}

// ImportFacilities nạp kho/Hub từ tệp: mỗi đối tượng điểm (marker) hoặc vùng
// (khuôn viên, marker = điểm trong vùng) thành một cơ sở, cùng quy tắc như thêm tay.
func (s *Service) ImportFacilities(ctx context.Context, in FacilityImportInput) (FacilityImportResult, error) {
	a, err := s.access(ctx, adminRoles...)
	if err != nil {
		return FacilityImportResult{}, err
	}
	if in.IdemKey == "" {
		return FacilityImportResult{}, problem.IdempotencyRequired
	}
	format := domain.DetectFormat(in.Format, in.FileName, in.Data)
	feats, err := domain.ParseFeaturesPoints(format, in.Data)
	if err != nil {
		return FacilityImportResult{}, violationProblem(err)
	}
	res := FacilityImportResult{Format: format}
	for _, f := range feats {
		r := FacilityImportFeature{Index: f.Index, Name: f.Name}
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
		fin := FacilityInput{IdemKey: in.IdemKey + ":" + strconv.Itoa(f.Index)}
		set := func(dst **string, def string, keys ...string) {
			v := firstOf(f.Props, keys...)
			if v == "" {
				v = def
			}
			*dst = &v
		}
		set(&fin.Name, f.Name, "name", "ten", "ten_hub", "ten_kho")
		set(&fin.Code, "", "code", "ma", "ma_kho", "ma_hub")
		set(&fin.Kind, in.Kind, "kind", "loai")
		set(&fin.ProvinceCode, in.ProvinceCode, "province_code", "ma_tinh")
		set(&fin.CommuneCode, in.CommuneCode, "commune_code", "ma_xa")
		set(&fin.Unit, in.Unit, "unit", "don_vi", "dvt")
		if *fin.Kind == "" {
			k := domain.FacilityHub
			fin.Kind = &k
		}
		if c := firstOf(f.Props, "capacity", "suc_chua", "capacity_tons"); c != "" {
			d, err := decimal.NewFromString(strings.ReplaceAll(c, ",", "."))
			if err != nil {
				fin.CapacityBad = true
			} else {
				fin.Capacity = &d
			}
		}
		if f.Point != nil {
			fin.Point = json.RawMessage(domain.PointGeoJSON(*f.Point))
		} else {
			fin.Polygon = json.RawMessage(f.Geom.GeoJSON())
		}
		out, err := s.createFacility(ctx, a, fin)
		if err != nil {
			var pr *problem.Problem
			if !errors.As(err, &pr) || pr.Status >= 500 {
				return FacilityImportResult{}, err
			}
			reject(err)
			continue
		}
		r.FacilityRef, r.FacilityCode = out.Facility.Ref, out.Facility.Code
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
