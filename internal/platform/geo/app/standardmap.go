package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Bản đồ chuẩn hóa cho ứng dụng tiêu thụ (TMS — mkg/logistics), GIS US-INT-02 / FN-17: MỘT lần
// gọi trả các lớp nền đã chuẩn hóa trong khung nhìn dạng GeoJSON — ranh giới hành chính, ranh
// giới/marker HTX, kho/Hub kèm widget sức chứa, đường bộ, đường thủy đi được — cùng danh sách
// bản đồ nền. Chỉ đọc; không có thửa hay dữ liệu cá nhân (R-04). TMS thuộc vùng mkg gọi API
// công khai này bằng tài khoản dịch vụ vai map_consumer (R-03: không nối DB pf).

// Tên lớp của bản đồ chuẩn hóa.
const (
	LayerAdminBoundaries = "admin_boundaries"
	LayerWaterways       = "waterways"
)

// StandardLayers là các lớp bản đồ chuẩn hóa theo thứ tự trả về.
var StandardLayers = []string{LayerAdminBoundaries, LayerCooperatives, LayerFacilities, LayerRoads, LayerWaterways}

// standardMapRoles: vai đọc bản đồ chuẩn hóa — tài khoản dịch vụ TMS và người dùng bản đồ GIS.
var standardMapRoles = append([]string{port.RoleMapConsumer}, layerReadRoles...)

// Khóa tham số chuỗi (pf/masterdata) của bản đồ nền theo loại — URL mẫu tile trong lãnh thổ
// (RB-7). Chưa cấu hình → danh sách rỗng (nhà cung cấp tile chưa chọn).
const ParamBasemapPrefix = "gis.basemap_url"

// BasemapKinds là các loại bản đồ nền của AC-1 (Vệ tinh/Đường phố/Địa hình).
var BasemapKinds = []string{"street", "satellite", "terrain"}

// StandardMapInput là truy vấn bản đồ chuẩn hóa.
type StandardMapInput struct {
	BBoxInput
	Layers      map[string]bool // rỗng = mọi lớp
	MinLoadTons *decimal.Decimal
	MinLoadBad  bool
	AdminLevel  string // province | commune; rỗng → theo zoom (< 10 tỉnh, ≥ 10 xã)
}

// Basemap là một bản đồ nền đã cấu hình.
type Basemap struct {
	Kind, URL string
}

// StandardMap là kết quả.
type StandardMap struct {
	GeneratedAt     time.Time
	AdminLevel      string
	AdminBoundaries []store.AdminBoundary
	AdminTruncated  bool
	Cooperatives    map[string]any // FeatureCollection đã dựng của pf/cooperative (port.MapLayers)
	Facilities      *FacilityPage
	Roads           *RoadPage
	Waterways       *port.LayerPage
	Basemaps        []Basemap
}

// StandardMap trả bản đồ chuẩn hóa của khung nhìn (GIS US-INT-02 AC-1). Tài khoản dịch vụ vai
// map_consumer đọc toàn tenant (lớp nền dùng chung, không định danh); người dùng khác theo phạm
// vi của mình. Mỗi lớp tối đa limit đối tượng (≤ 1.000, BR-25).
func (s *Service) StandardMap(ctx context.Context, in StandardMapInput) (StandardMap, error) {
	a, err := s.access(ctx, standardMapRoles...)
	if err != nil {
		return StandardMap{}, err
	}
	if a.p.Has(port.RoleMapConsumer) {
		a.sc, a.coopOnly = authz.Scope{All: true}, false
	}
	want := in.Layers
	if len(want) == 0 {
		want = map[string]bool{}
		for _, l := range StandardLayers {
			want[l] = true
		}
	}
	for k := range want {
		known := false
		for _, l := range StandardLayers {
			known = known || k == l
		}
		if !known {
			return StandardMap{}, problem.Validation.WithField("layers", "chỉ nhận "+strings.Join(StandardLayers, ", "))
		}
	}
	bb := store.BBox{MinLon: in.MinLon, MinLat: in.MinLat, MaxLon: in.MaxLon, MaxLat: in.MaxLat}
	if err := validBBox(bb); err != nil {
		return StandardMap{}, err
	}
	limit, err := viewportLimit(in.Limit)
	if err != nil {
		return StandardMap{}, err
	}
	in.Limit = limit
	level := in.AdminLevel
	switch level {
	case "":
		level = string(partyport.LevelCommune)
		if in.Zoom < 10 {
			level = string(partyport.LevelProvince)
		}
	case string(partyport.LevelProvince), string(partyport.LevelCommune):
	default:
		return StandardMap{}, problem.Validation.WithField("admin_level", "phải là province hoặc commune")
	}
	if in.MinLoadBad || (in.MinLoadTons != nil && !in.MinLoadTons.IsPositive()) {
		return StandardMap{}, problem.Validation.WithField("min_load_tons", "tải trọng phương tiện phải là số lớn hơn 0")
	}
	out := StandardMap{GeneratedAt: s.now(), AdminLevel: level, Basemaps: []Basemap{}}
	if want[LayerAdminBoundaries] && !a.sc.Empty() {
		err := s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
			bs, e := store.AdminBoundariesInBBox(ctx, tx, bb, level, SimplifyTolerance(in.Zoom), limit+1)
			if len(bs) > limit {
				bs, out.AdminTruncated = bs[:limit], true
			}
			out.AdminBoundaries = bs
			return e
		})
		if err != nil {
			return StandardMap{}, err
		}
	}
	if want[LayerCooperatives] {
		ml, err := s.mapLayers()
		if err != nil {
			return StandardMap{}, err
		}
		if out.Cooperatives, err = ml.CooperativeLayer(ctx, in.mapQuery(a.p.Has(port.RoleMapConsumer))); err != nil {
			return StandardMap{}, err
		}
	}
	if want[LayerFacilities] {
		pg, err := s.facilityLayer(ctx, a, FacilityListInput{BBoxInput: in.BBoxInput})
		if err != nil {
			return StandardMap{}, err
		}
		out.Facilities = &pg
	}
	if want[LayerRoads] {
		pg, err := s.roadLayer(ctx, a, RoadQueryInput{BBoxInput: in.BBoxInput, MinLoadTons: in.MinLoadTons})
		if err != nil {
			return StandardMap{}, err
		}
		out.Roads = &pg
	}
	if want[LayerWaterways] {
		if s.Waterways == nil {
			return StandardMap{}, ErrDependency.WithDetail("chưa nối lớp đường thủy (pf/waterway)")
		}
		ml := decimal.Zero
		if in.MinLoadTons != nil {
			ml = *in.MinLoadTons
		}
		pg, err := s.Waterways.SegmentsInBBox(ctx, in.MinLon, in.MinLat, in.MaxLon, in.MaxLat, ml, limit)
		if err != nil {
			return StandardMap{}, err
		}
		out.Waterways = &pg
	}
	if s.Params != nil {
		for _, k := range BasemapKinds {
			v, err := s.Params.At(ctx, ParamBasemapPrefix+":"+k, out.GeneratedAt)
			switch {
			case errors.Is(err, params.ErrParamNotFound):
			case err != nil:
				return StandardMap{}, ErrDependency.WithDetail("không tra được tham số bản đồ nền").WithCause(err)
			case strings.TrimSpace(v.Text) != "":
				out.Basemaps = append(out.Basemaps, Basemap{Kind: k, URL: strings.TrimSpace(v.Text)})
			}
		}
	}
	return out, nil
}
