package domain

import (
	"encoding/json"
	"testing"
)

func f(v float64) *float64 { return &v }

func TestZoneNormalize(t *testing.T) {
	square := json.RawMessage(`{"type":"Polygon","coordinates":[[[105.1,10.4],[105.2,10.4],[105.2,10.5],[105.1,10.4]]]}`)
	cases := []struct {
		name  string
		z     Zone
		field string // "" = hợp lệ
	}{
		{"đa giác", Zone{Kind: KindLivestock, Name: "Vùng heo", Communes: []string{"30001"}, Mode: ModePolygon, Geometry: square}, ""},
		{"bán kính", Zone{Kind: KindOutbreak, Name: "Ổ dịch", Communes: []string{"30001"}, Mode: ModeRadius, Center: &Point{Lat: 10.4, Lng: 105.1}, RadiusM: f(3000)}, ""},
		{"hướng", Zone{Kind: KindDirection, Name: "Hướng", Communes: []string{"30001"},
			Geometry: json.RawMessage(`{"type":"LineString","coordinates":[[105.1,10.4],[105.2,10.5]]}`)}, ""},
		{"loại sai", Zone{Kind: "khac", Name: "x", Communes: []string{"30001"}, Mode: ModePolygon, Geometry: square}, "kind"},
		{"không xã", Zone{Kind: KindLivestock, Name: "x", Mode: ModePolygon, Geometry: square}, "communes"},
		{"vòng hở", Zone{Kind: KindLivestock, Name: "x", Communes: []string{"30001"}, Mode: ModePolygon,
			Geometry: json.RawMessage(`{"type":"Polygon","coordinates":[[[105.1,10.4],[105.2,10.4],[105.2,10.5],[105.1,10.5]]]}`)}, "geometry"},
		{"ngoài VN", Zone{Kind: KindLivestock, Name: "x", Communes: []string{"30001"}, Mode: ModePolygon,
			Geometry: json.RawMessage(`{"type":"Polygon","coordinates":[[[2.1,48.4],[2.2,48.4],[2.2,48.5],[2.1,48.4]]]}`)}, "geometry"},
		{"bán kính quá lớn", Zone{Kind: KindBuffer, Name: "x", Communes: []string{"30001"}, Mode: ModeRadius, Center: &Point{Lat: 10.4, Lng: 105.1}, RadiusM: f(90000)}, "radius_m"},
		{"hướng bằng đa giác", Zone{Kind: KindDirection, Name: "x", Communes: []string{"30001"}, Mode: ModePolygon, Geometry: square}, "draw_mode"},
		{"đường cho vùng", Zone{Kind: KindLivestock, Name: "x", Communes: []string{"30001"}, Mode: ModeLine,
			Geometry: json.RawMessage(`{"type":"LineString","coordinates":[[105.1,10.4],[105.2,10.5]]}`)}, "draw_mode"},
		{"điểm thay đa giác", Zone{Kind: KindLivestock, Name: "x", Communes: []string{"30001"}, Mode: ModePolygon,
			Geometry: json.RawMessage(`{"type":"Point","coordinates":[105.1,10.4]}`)}, "geometry"},
	}
	for _, c := range cases {
		es := c.z.Normalize()
		if c.field == "" && len(es) > 0 {
			t.Errorf("%s: muốn hợp lệ, nhận %v", c.name, es)
		}
		if c.field != "" {
			found := false
			for _, e := range es {
				found = found || e.Field == c.field
			}
			if !found {
				t.Errorf("%s: muốn lỗi %s, nhận %v", c.name, c.field, es)
			}
		}
	}
}

func TestEpidemicKind(t *testing.T) {
	if EpidemicKind(KindLivestock) || !EpidemicKind(KindBuffer) || !ValidReason("đổi hình") || ValidReason("ab") {
		t.Fatal("quy tắc loại/lý do sai")
	}
}
