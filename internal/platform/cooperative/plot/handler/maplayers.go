package handler

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/app"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
)

// MapLayers thực hiện geoport.MapLayers: lớp ranh giới HTX và lớp thửa (GeoJSON đã dựng, JSON như trước W3.3) cho
// GET /v1/geo/layers và GET /v1/geo/standard-map của pf/geo. Gốc lắp ráp nối bằng geo.Module.UseMapLayers.
type MapLayers struct{ Svc *app.Service }

var _ geoport.MapLayers = MapLayers{}

func bboxOf(q geoport.MapLayerQuery) app.BBoxInput {
	return app.BBoxInput{MinLon: q.MinLon, MinLat: q.MinLat, MaxLon: q.MaxLon, MaxLat: q.MaxLat, Zoom: q.Zoom, Limit: q.Limit}
}

// CooperativeLayer thực hiện geoport.MapLayers.
func (m MapLayers) CooperativeLayer(ctx context.Context, q geoport.MapLayerQuery) (map[string]any, error) {
	l, err := m.Svc.CoopLayerForMap(ctx, bboxOf(q), q.MapConsumer)
	if err != nil {
		return nil, err
	}
	return plainCollection(coopLayerDTO(l, q.Limit)), nil
}

// PlotLayer thực hiện geoport.MapLayers.
func (m MapLayers) PlotLayer(ctx context.Context, q geoport.MapLayerQuery) (map[string]any, error) {
	pg, err := m.Svc.PlotsInBBox(ctx, bboxOf(q))
	if err != nil {
		return nil, err
	}
	return plainCollection(gin.H{"type": "FeatureCollection", "features": featureCollection(pg.Items), "truncated": pg.Truncated}), nil
}

// plainCollection đổi FeatureCollection dựng bằng gin.H sang map[string]any (features []map[string]any, properties
// map[string]any) theo hợp đồng geoport.MapLayers. JSON không đổi.
func plainCollection(fc gin.H) map[string]any {
	out := map[string]any(fc)
	if fs, ok := fc["features"].([]gin.H); ok {
		feats := make([]map[string]any, 0, len(fs))
		for _, f := range fs {
			if p, ok := f["properties"].(gin.H); ok {
				f["properties"] = map[string]any(p)
			}
			feats = append(feats, map[string]any(f))
		}
		out["features"] = feats
	}
	return out
}
