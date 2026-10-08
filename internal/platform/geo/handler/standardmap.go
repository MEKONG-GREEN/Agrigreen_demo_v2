package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/app"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// StandardMapSchema là định danh phiên bản định dạng của bản đồ chuẩn hóa (GIS US-INT-02).
const StandardMapSchema = "agrigreen.standard_map/v1"

// tagLayer gắn properties.layer cho từng đối tượng của một FeatureCollection đã dựng (của geo: gin.H; của
// pf/cooperative qua port.MapLayers: map[string]any).
func tagLayer(fc map[string]any, layer string, out *[]gin.H) (count int, truncated bool) {
	var feats []map[string]any
	switch fs := fc["features"].(type) {
	case []gin.H:
		for _, f := range fs {
			feats = append(feats, f)
		}
	case []map[string]any:
		feats = fs
	}
	for _, f := range feats {
		switch p := f["properties"].(type) {
		case gin.H:
			p["layer"] = layer
		case map[string]any:
			p["layer"] = layer
		}
		*out = append(*out, f)
	}
	t, _ := fc["truncated"].(bool)
	return len(feats), t
}

func waterwayFeature(s geoport.LayerSegment) gin.H {
	coords := make([][2]float64, 0, len(s.Line))
	for _, p := range s.Line {
		coords = append(coords, [2]float64{p.Lng, p.Lat})
	}
	geom, _ := json.Marshal(gin.H{"type": "LineString", "coordinates": coords})
	var ml *string
	if s.MaxLoadTons != nil {
		v := s.MaxLoadTons.StringFixed(3)
		ml = &v
	}
	return gin.H{"type": "Feature", "id": s.SegmentRef.String(), "geometry": json.RawMessage(geom),
		"properties": gin.H{"layer": app.LayerWaterways, "segment_ref": s.SegmentRef.String(), "name": s.Name, "waterway_class": s.Class,
			"max_load_tons": ml, "certain": s.Certain, "passable": s.Passable}}
}

// standardMap: GET /v1/geo/standard-map?bbox=&zoom=&layers=&min_load_tons=&admin_level=&limit=
// — bản đồ chuẩn hóa chỉ đọc cho TMS (GIS US-INT-02 / FN-17): MỘT FeatureCollection gồm mọi lớp
// (properties.layer), tóm tắt từng lớp, bản đồ nền đã cấu hình và ngưỡng widget kho/Hub.
func (h Handler) standardMap(c *gin.Context) {
	bb, ok := bboxInput(c, 12)
	if !ok {
		return
	}
	want := map[string]bool{}
	for _, l := range strings.Split(c.Query("layers"), ",") {
		if l = strings.TrimSpace(l); l != "" {
			want[l] = true
		}
	}
	minLoad, bad := decQuery(c, "min_load_tons")
	out, err := h.Svc.StandardMap(c.Request.Context(), app.StandardMapInput{BBoxInput: bb, Layers: want, MinLoadTons: minLoad, MinLoadBad: bad,
		AdminLevel: c.Query("admin_level")})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	feats := []gin.H{}
	layers := gin.H{}
	sum := func(name string, n int, trunc bool) { layers[name] = gin.H{"count": n, "truncated": trunc} }
	if out.AdminBoundaries != nil || want[app.LayerAdminBoundaries] || len(want) == 0 {
		for _, b := range out.AdminBoundaries {
			feats = append(feats, gin.H{"type": "Feature", "id": b.UnitCode, "geometry": json.RawMessage(b.GeoJSON),
				"properties": gin.H{"layer": app.LayerAdminBoundaries, "unit_code": b.UnitCode, "level": b.Level, "name": b.Name,
					"source": b.Source, "area_ha": b.AreaHa.StringFixed(4)}})
		}
		sum(app.LayerAdminBoundaries, len(out.AdminBoundaries), out.AdminTruncated)
	}
	var thresholds any
	if out.Cooperatives != nil {
		n, t := tagLayer(out.Cooperatives, app.LayerCooperatives, &feats)
		sum(app.LayerCooperatives, n, t)
	}
	if out.Facilities != nil {
		fc := facilityPageDTO(*out.Facilities, bb.Limit)
		thresholds = fc["thresholds"]
		n, t := tagLayer(fc, app.LayerFacilities, &feats)
		sum(app.LayerFacilities, n, t)
	}
	if out.Roads != nil {
		n, t := tagLayer(roadPageDTO(*out.Roads, bb.Limit), app.LayerRoads, &feats)
		sum(app.LayerRoads, n, t)
	}
	if out.Waterways != nil {
		for _, s := range out.Waterways.Items {
			feats = append(feats, waterwayFeature(s))
		}
		layers[app.LayerWaterways] = gin.H{"count": len(out.Waterways.Items), "truncated": out.Waterways.Truncated,
			"passable_count": out.Waterways.PassableCount, "version_no": out.Waterways.VersionNo}
	}
	basemaps := make([]gin.H, 0, len(out.Basemaps))
	for _, b := range out.Basemaps {
		basemaps = append(basemaps, gin.H{"kind": b.Kind, "url": b.URL})
	}
	var notice any
	if len(basemaps) == 0 {
		notice = "Chưa cấu hình bản đồ nền (nhà cung cấp tile trong lãnh thổ chưa chọn — RB-7)."
	}
	c.JSON(http.StatusOK, gin.H{
		"schema": StandardMapSchema, "type": "FeatureCollection", "generated_at": out.GeneratedAt.UTC().Format(time.RFC3339),
		"bbox": []float64{bb.MinLon, bb.MinLat, bb.MaxLon, bb.MaxLat}, "zoom": bb.Zoom, "admin_level": out.AdminLevel,
		"features": feats, "layers": layers, "basemaps": basemaps, "basemap_notice": notice, "capacity_thresholds": thresholds,
	})
}
