package domain

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
)

// GIS US-HUB-01 (BR-50): % lấp đầy và lớp màu theo ngưỡng (mặc định 60/85 như nguyên mẫu).
func TestCapacityClass_GIS_US_HUB_01(t *testing.T) {
	d := decimal.RequireFromString
	for _, tc := range []struct {
		used, capacity, pct, class string
	}{
		{"0", "100", "0", CapacityAvailable},
		{"59.94", "100", "59.9", CapacityAvailable},
		{"60", "100", "60", CapacityNearFull},
		{"84.9", "100", "84.9", CapacityNearFull},
		{"85", "100", "85", CapacityFull},
		{"130", "100", "130", CapacityFull}, // quá tải vẫn là đầy
		{"1", "3", "33.3", CapacityAvailable},
	} {
		pct := FillPct(d(tc.used), d(tc.capacity))
		if !pct.Equal(d(tc.pct)) {
			t.Fatalf("FillPct(%s/%s) = %s, muốn %s", tc.used, tc.capacity, pct, tc.pct)
		}
		if c := CapacityClass(pct, DefaultHubWarnPct, DefaultHubFullPct); c != tc.class {
			t.Fatalf("CapacityClass(%s) = %s, muốn %s", pct, c, tc.class)
		}
	}
	if !FillPct(d("5"), decimal.Zero).IsZero() {
		t.Fatal("sức chứa 0 → 0 %")
	}
	if !AdminEntered(CapacityManual) || !AdminEntered(CapacityImport) || AdminEntered(CapacityValve) {
		t.Fatal("BR-52: manual/import là số Admin, valve thì không")
	}
}

func TestParsePointAndLineString(t *testing.T) {
	p, err := ParsePoint(json.RawMessage(`{"type":"Point","coordinates":[105.6,10.05]}`))
	if err != nil || p[0] != 105.6 || p[1] != 10.05 {
		t.Fatalf("Point: %v %v", p, err)
	}
	for _, bad := range []string{`{"type":"Point","coordinates":[580000,1110000]}`, `{"type":"Polygon","coordinates":[]}`, `null`,
		`{"type":"Point","coordinates":[105.6,10.05],"crs":{"type":"name","properties":{"name":"EPSG:3405"}}}`} {
		if _, err := ParsePoint(json.RawMessage(bad)); err == nil {
			t.Fatalf("phải từ chối %s", bad)
		}
	}
	if _, err := ParsePoint(json.RawMessage(`{"type":"Point","coordinates":[580000,1110000]}`)); code(err) != CodeCRSUnsupported {
		t.Fatalf("toạ độ phẳng phải geo.crs_unsupported: %v", err)
	}
	ls, err := ParseLineString(json.RawMessage(`{"type":"LineString","coordinates":[[105.6,10.05],[105.6,10.05],[105.61,10.05]]}`))
	if err != nil || len(ls) != 2 {
		t.Fatalf("LineString bỏ đỉnh trùng liên tiếp: %v %v", ls, err)
	}
	if ls.GeoJSON() != `{"type":"LineString","coordinates":[[105.6,10.05],[105.61,10.05]]}` {
		t.Fatalf("GeoJSON = %s", ls.GeoJSON())
	}
	if _, err := ParseLineString(json.RawMessage(`{"type":"LineString","coordinates":[[105.6,10.05],[105.6,10.05]]}`)); code(err) != CodeInvalidGeometry {
		t.Fatalf("một điểm phân biệt phải bị từ chối: %v", err)
	}
	if !KnownRoadClass(RoadBoRuong) || KnownRoadClass("cao_toc") {
		t.Fatal("phân loại đường bộ (BRD FN-08 B2)")
	}
}

// GIS US-BND-08: đọc điểm (marker) từ GeoJSON/KML chỉ khi nhập kho/Hub; nhập thửa vẫn từ chối điểm.
func TestParseFeaturesPoints_GIS_US_BND_08(t *testing.T) {
	gj := []byte(`{"type":"FeatureCollection","features":[
	  {"type":"Feature","properties":{"ten":"Hub A"},"geometry":{"type":"Point","coordinates":[105.6,10.05]}},
	  {"type":"Feature","properties":{"ten":"Kho B"},"geometry":{"type":"Polygon","coordinates":[[[105.6,10.05],[105.601,10.05],[105.601,10.051],[105.6,10.051],[105.6,10.05]]]}}]}`)
	feats, err := ParseFeaturesPoints(FormatGeoJSON, gj)
	if err != nil || len(feats) != 2 || feats[0].Point == nil || feats[0].Err != nil || feats[1].Geom == nil || feats[1].Err != nil {
		t.Fatalf("GeoJSON điểm + vùng: %+v %v", feats, err)
	}
	plots, err := ParseFeatures(FormatGeoJSON, gj)
	if err != nil || plots[0].Err == nil || plots[0].Point != nil {
		t.Fatalf("nhập thửa phải từ chối điểm: %+v %v", plots, err)
	}
	kml := []byte(`<kml><Document><Placemark><name>Kho K</name><Point><coordinates>105.61,10.06,0</coordinates></Point></Placemark>
	  <Placemark><name>Sai</name><Point><coordinates>105.61</coordinates></Point></Placemark></Document></kml>`)
	feats, err = ParseFeaturesPoints(FormatKML, kml)
	if err != nil || len(feats) != 2 || feats[0].Point == nil || (*feats[0].Point)[0] != 105.61 || feats[0].Name != "Kho K" || feats[1].Err == nil {
		t.Fatalf("KML Point: %+v %v", feats, err)
	}
	if plots, _ := ParseFeatures(FormatKML, kml); plots[0].Err == nil {
		t.Fatal("KML Point khi nhập thửa phải bị loại")
	}
}
