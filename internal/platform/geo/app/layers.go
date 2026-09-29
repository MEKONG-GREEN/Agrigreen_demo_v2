package app

import (
	"context"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Lớp bản đồ web GIS gộp một lần gọi (BRD FN-01: bật/tắt từng lớp): chỉ khung
// nhìn, mỗi lớp tối đa 1.000 đối tượng (BR-25); polygon thửa chỉ khi zoom ≥
// geo.plot_layer_min_zoom (BR-24, mặc định 13).

// Tên lớp.
const (
	LayerCooperatives = "cooperatives"
	LayerPlots        = "plots"
	LayerFacilities   = "facilities"
	LayerRoads        = "roads"
)

// LayersInput là truy vấn lớp.
type LayersInput struct {
	BBoxInput
	Layers      map[string]bool
	MinLoadTons *decimal.Decimal
	MinLoadBad  bool
}

// LayersOut là các lớp đã yêu cầu (nil = không yêu cầu).
type LayersOut struct {
	Cooperatives *CoopLayer
	Plots        *BBoxPage
	PlotsNotice  string
	PlotMinZoom  int
	Facilities   *FacilityPage
	Roads        *RoadPage
}

// Layers trả các lớp của khung nhìn.
func (s *Service) Layers(ctx context.Context, in LayersInput) (LayersOut, error) {
	a, err := s.access(ctx, layerReadRoles...)
	if err != nil {
		return LayersOut{}, err
	}
	for k := range in.Layers {
		switch k {
		case LayerCooperatives, LayerPlots, LayerFacilities, LayerRoads:
		default:
			return LayersOut{}, problem.Validation.WithField("layers", "chỉ nhận cooperatives, plots, facilities, roads")
		}
	}
	if err := validBBox(store.BBox{MinLon: in.MinLon, MinLat: in.MinLat, MaxLon: in.MaxLon, MaxLat: in.MaxLat}); err != nil {
		return LayersOut{}, err
	}
	var out LayersOut
	if out.PlotMinZoom, err = s.plotMinZoom(ctx); err != nil {
		return LayersOut{}, err
	}
	if in.Layers[LayerCooperatives] {
		l, err := s.coopLayer(ctx, a, in.BBoxInput)
		if err != nil {
			return LayersOut{}, err
		}
		out.Cooperatives = &l
	}
	if in.Layers[LayerPlots] {
		if in.Zoom >= out.PlotMinZoom {
			pg, err := s.PlotsInBBox(ctx, in.BBoxInput)
			if err != nil {
				return LayersOut{}, err
			}
			out.Plots = &pg
		} else {
			out.PlotsNotice = "Polygon lô/thửa chỉ hiển thị từ mức zoom cận cảnh (≥ " + strconv.Itoa(out.PlotMinZoom) + ")."
		}
	}
	if in.Layers[LayerFacilities] {
		pg, err := s.facilityLayer(ctx, a, FacilityListInput{BBoxInput: in.BBoxInput})
		if err != nil {
			return LayersOut{}, err
		}
		out.Facilities = &pg
	}
	if in.Layers[LayerRoads] {
		pg, err := s.roadLayer(ctx, a, RoadQueryInput{BBoxInput: in.BBoxInput, MinLoadTons: in.MinLoadTons, MinLoadBad: in.MinLoadBad})
		if err != nil {
			return LayersOut{}, err
		}
		out.Roads = &pg
	}
	return out, nil
}
