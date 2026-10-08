package domain

import (
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/shptest"
)

func sq(x, y, d float64) []shptest.Point {
	return []shptest.Point{{x, y}, {x + d, y}, {x + d, y + d}, {x, y + d}}
}

// KN US-PLOT-02: đọc gói Shapefile Polygon/MultiPolygon có lỗ, thuộc tính .dbf tiếng Việt.
func TestParseShapefileZip(t *testing.T) {
	feats := []shptest.Feature{
		{Parts: []shptest.Polygon{{Outer: sq(105.60, 10.05, 0.001), Holes: [][]shptest.Point{sq(105.6002, 10.0502, 0.0002)}}},
			Attrs: map[string]string{"TEN_THUA": "Ruộng ông Tư", "MA_XA": "31117", "MA_HTX": "HTX000001"}},
		{Parts: []shptest.Polygon{{Outer: sq(105.61, 10.05, 0.001)}, {Outer: sq(105.62, 10.05, 0.001)}},
			Attrs: map[string]string{"SO_TO": "12", "SO_THUA": "345", "MA_XA": "31117"}},
		{Parts: nil, Attrs: map[string]string{"TEN_THUA": "Rỗng"}},
		{Parts: []shptest.Polygon{{Outer: sq(105.63, 10.05, 0.001)}}, Attrs: map[string]string{"TEN_THUA": "Đã xóa"}},
	}
	data := shptest.Zip([]string{"TEN_THUA", "SO_TO", "SO_THUA", "MA_XA", "MA_HTX"}, feats, shptest.WGS84, shptest.Options{Deleted: []int{3}})
	if got := DetectFormat("", "thua.zip", data); got != FormatShapefile {
		t.Fatalf("nhận dạng .zip = %q", got)
	}
	out, err := ParseFeatures(FormatShapefile, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("bản ghi đã xóa trong .dbf bị bỏ: %d", len(out))
	}
	f1 := out[0]
	if f1.Err != nil || f1.Name != "Ruộng ông Tư" || len(f1.Geom) != 1 || len(f1.Geom[0].Holes) != 1 || f1.Points != 8 {
		t.Fatalf("đối tượng 1: %+v", f1)
	}
	if f1.Props["cooperative_code"] != "HTX000001" || f1.Props["commune_code"] != "31117" {
		t.Fatalf("thuộc tính .dbf ánh xạ Mã HTX/Mã xã: %v", f1.Props)
	}
	if f2 := out[1]; f2.Err != nil || len(f2.Geom) != 2 || f2.Name != "Tờ 12 thửa 345" || f2.Index != 2 {
		t.Fatalf("đối tượng 2 (MultiPolygon): %+v", f2)
	}
	if f3 := out[2]; code(f3.Err) != CodeInvalidGeometry {
		t.Fatalf("Null shape phải bị loại riêng: %+v", f3)
	}
	// Vòng ngoài giữ đúng toạ độ (không đảo x/y).
	if p := f1.Geom[0].Outer[0]; p[0] < 105 || p[1] > 11 {
		t.Fatalf("toạ độ kinh/vĩ: %v", p)
	}
}

func TestParseShapefileRejects(t *testing.T) {
	one := []shptest.Feature{{Parts: []shptest.Polygon{{Outer: sq(105.60, 10.05, 0.001)}}, Attrs: map[string]string{"TEN": "A"}}}
	cases := []struct {
		name, prj, code, want string
		opt                   shptest.Options
	}{
		{"VN-2000", shptest.VN2000UTM48, CodeCRSUnsupported, "VN-2000", shptest.Options{}},
		{"UTM WGS84", shptest.UTM48N, CodeCRSUnsupported, "hệ toạ độ phẳng", shptest.Options{}},
		{"thiếu .prj", "", CodeCRSUnsupported, ".prj", shptest.Options{}},
		{"thiếu .dbf", shptest.WGS84, CodeFormat, ".dbf", shptest.Options{NoDBF: true}},
		{"hệ lạ", `GEOGCS["GCS_Indian_1960",DATUM["D_Indian_1960",SPHEROID["Everest",6377276.345,300.8017]]]`, CodeCRSUnsupported, "Indian_1960", shptest.Options{}},
	}
	for _, c := range cases {
		_, err := ParseFeatures(FormatShapefile, shptest.Zip([]string{"TEN"}, one, c.prj, c.opt))
		v, _ := AsViolation(err)
		if code(err) != c.code || v == nil || !strings.Contains(v.Msg, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// .prj VN-2000 dạng OGC WKT2 cũng bị nhận ra.
	if err := CheckPRJ(`PROJCRS["VN-2000 / UTM zone 48N",BASEGEOGCRS["VN-2000",DATUM["Vietnam 2000"]],ID["EPSG",3405]]`); code(err) != CodeCRSUnsupported || !strings.Contains(err.Error(), "VN-2000") {
		t.Fatalf("WKT2 VN-2000: %v", err)
	}
	if err := CheckPRJ(`GEOGCRS["WGS 84",DATUM["World Geodetic System 1984",ELLIPSOID["WGS 84",6378137,298.257223563]],ID["EPSG",4326]]`); err != nil {
		t.Fatalf("WKT2 WGS 84 phải được nhận: %v", err)
	}
	// Toạ độ phẳng dù .prj ghi WGS84 → chặn ở kiểm miền toạ độ.
	flat := []shptest.Feature{{Parts: []shptest.Polygon{{Outer: sq(580000, 1110000, 50)}}, Attrs: map[string]string{"TEN": "B"}}}
	out, err := ParseFeatures(FormatShapefile, shptest.Zip([]string{"TEN"}, flat, shptest.WGS84, shptest.Options{}))
	if err != nil || code(out[0].Err) != CodeCRSUnsupported {
		t.Fatalf("toạ độ mét phải bị loại: %v %+v", err, out)
	}
	if _, err := ParseFeatures(FormatShapefile, []byte{0x00, 0x00, 0x27, 0x0a, 0x00}); code(err) != CodeFormat {
		t.Fatalf(".shp rời: %v", err)
	}
}
