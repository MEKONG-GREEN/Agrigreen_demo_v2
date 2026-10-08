package domain

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func code(err error) string {
	if v, ok := AsViolation(err); ok {
		return v.Code
	}
	if err != nil {
		return "khác: " + err.Error()
	}
	return ""
}

// FN-XA-03.05 (BRD v7.7, quyết định A2 03/10/2026): tối thiểu 3 điểm phân biệt không thẳng hàng — tam giác hợp lệ;
// điểm trùng/thẳng hàng vẫn bị chặn.
func TestParseGeometryMinPoints(t *testing.T) {
	cases := []struct {
		name, geom, want string
		points           int
	}{
		{"tam giác 3 điểm (A2)", `{"type":"Polygon","coordinates":[[[105.7,10.0],[105.71,10.0],[105.71,10.01],[105.7,10.0]]]}`, "", 3},
		{"tam giác chưa khép", `{"type":"Polygon","coordinates":[[[105.7,10.0],[105.71,10.0],[105.71,10.01]]]}`, "", 3},
		{"3 điểm nhưng 1 trùng", `{"type":"Polygon","coordinates":[[[105.7,10.0],[105.71,10.0],[105.71,10.0]]]}`, CodeTooFewPoints, 0},
		{"3 điểm thẳng hàng", `{"type":"Polygon","coordinates":[[[105.7,10.0],[105.71,10.01],[105.72,10.02]]]}`, CodeTooFewPoints, 0},
		{"4 điểm nhưng 2 trùng", `{"type":"Polygon","coordinates":[[[105.7,10.0],[105.71,10.0],[105.71,10.0],[105.7,10.0]]]}`, CodeTooFewPoints, 0},
		{"4 điểm thẳng hàng", `{"type":"Polygon","coordinates":[[[105.7,10.0],[105.71,10.01],[105.72,10.02],[105.73,10.03]]]}`, CodeTooFewPoints, 0},
		{"4 điểm hợp lệ, chưa khép", `{"type":"Polygon","coordinates":[[[105.7,10.0],[105.71,10.0],[105.71,10.01],[105.7,10.01]]]}`, "", 4},
		{"MultiPolygon hợp lệ", `{"type":"MultiPolygon","coordinates":[[[[105.7,10.0],[105.71,10.0],[105.71,10.01],[105.7,10.01],[105.7,10.0]]]]}`, "", 4},
		{"toạ độ VN-2000", `{"type":"Polygon","coordinates":[[[585000,1108000],[585100,1108000],[585100,1108100],[585000,1108100]]]}`, CodeCRSUnsupported, 0},
		{"crs EPSG:3405", `{"type":"Polygon","crs":{"type":"name","properties":{"name":"EPSG:3405"}},"coordinates":[[[105.7,10.0],[105.71,10.0],[105.71,10.01],[105.7,10.01]]]}`, CodeCRSUnsupported, 0},
		{"crs CRS84", `{"type":"Polygon","crs":{"type":"name","properties":{"name":"urn:ogc:def:crs:OGC:1.3:CRS84"}},"coordinates":[[[105.7,10.0],[105.71,10.0],[105.71,10.01],[105.7,10.01]]]}`, "", 4},
		{"Point", `{"type":"Point","coordinates":[105.7,10.0]}`, CodeInvalidGeometry, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mp, n, err := ParseGeometry(json.RawMessage(c.geom))
			if got := code(err); got != c.want {
				t.Fatalf("mã lỗi = %q, muốn %q", got, c.want)
			}
			if c.want == "" {
				if n != c.points {
					t.Fatalf("số điểm = %d, muốn %d", n, c.points)
				}
				r := mp[0].Outer
				if r[0] != r[len(r)-1] {
					t.Fatal("vòng phải được khép")
				}
			}
		})
	}
}

func TestParseKML(t *testing.T) {
	kml := `<?xml version="1.0" encoding="UTF-8"?>
<kml xmlns="http://www.opengis.net/kml/2.2"><Document>
 <Placemark><name>Thửa A</name>
  <ExtendedData><Data name="commune_code"><value>31117</value></Data></ExtendedData>
  <Polygon><outerBoundaryIs><LinearRing><coordinates>
   105.70,10.00,0 105.71,10.00,0 105.71,10.01,0 105.70,10.01,0 105.70,10.00,0
  </coordinates></LinearRing></outerBoundaryIs></Polygon></Placemark>
 <Placemark><name>Cọc mốc</name><Point><coordinates>105.7,10.0</coordinates></Point></Placemark>
 <Placemark><name>Thửa B</name><MultiGeometry>
  <Polygon><outerBoundaryIs><LinearRing><coordinates>105.8,10.0 105.81,10.0 105.81,10.01 105.8,10.01</coordinates></LinearRing></outerBoundaryIs>
   <innerBoundaryIs><LinearRing><coordinates>105.802,10.002 105.803,10.002 105.803,10.003</coordinates></LinearRing></innerBoundaryIs></Polygon>
 </MultiGeometry><ExtendedData><SchemaData><SimpleData name="ma_xa">31120</SimpleData></SchemaData></ExtendedData></Placemark>
</Document></kml>`
	format := DetectFormat("", "thua.kml", []byte(kml))
	if format != FormatKML {
		t.Fatalf("định dạng = %q", format)
	}
	feats, err := ParseFeatures(format, []byte(kml))
	if err != nil {
		t.Fatal(err)
	}
	if len(feats) != 3 {
		t.Fatalf("số đối tượng = %d, muốn 3", len(feats))
	}
	if feats[0].Err != nil || feats[0].Name != "Thửa A" || feats[0].Props["commune_code"] != "31117" || feats[0].Points != 4 {
		t.Fatalf("đối tượng 1 sai: %+v", feats[0])
	}
	if code(feats[1].Err) != CodeInvalidGeometry {
		t.Fatalf("Point phải bị loại, được %v", feats[1].Err)
	}
	if feats[2].Err != nil || len(feats[2].Geom[0].Holes) != 1 || feats[2].Props["ma_xa"] != "31120" {
		t.Fatalf("đối tượng 3 sai: %+v", feats[2])
	}
}

func TestParseFeaturesFormats(t *testing.T) {
	if _, err := ParseFeatures(FormatShapefile, []byte("PK\x03\x04")); code(err) != CodeFormat {
		t.Fatalf("gói .zip hỏng phải trả geo.format_invalid, được %v", err)
	}
	if _, err := ParseFeatures("docx", []byte("x")); code(err) != CodeFormatUnsupported {
		t.Fatalf("định dạng lạ phải trả geo.format_unsupported, được %v", err)
	}
	if got := DetectFormat("", "ranh_gioi.shp", nil); got != FormatShapefile {
		t.Fatalf("nhận dạng .shp = %q", got)
	}
	if _, err := ParseFeatures(FormatGeoJSON, []byte(`{"type":"FeatureCollection","crs":{"type":"name","properties":{"name":"EPSG:32648"}},"features":[]}`)); code(err) != CodeCRSUnsupported {
		t.Fatalf("crs UTM phải bị từ chối, được %v", err)
	}
	if _, err := ParseFeatures(FormatKML, []byte("<kml><Placemark>")); code(err) != CodeFormat {
		t.Fatalf("KML hỏng phải trả geo.format_invalid, được %v", err)
	}
}

func TestClassifyOverlaps(t *testing.T) {
	coopA, coopB := uuid.New(), uuid.New()
	tol := decimal.NewFromInt(1)
	hits := []Overlap{
		{Code: "a-nhỏ", CooperativeRef: coopA, Pct: decimal.RequireFromString("0.5")},
		{Code: "a-lớn", CooperativeRef: coopA, Pct: decimal.RequireFromString("12")},
		{Code: "b", CooperativeRef: coopB, Pct: decimal.RequireFromString("40")},
		{Code: "nháp", CooperativeRef: uuid.Nil, Pct: decimal.RequireFromString("5")},
	}
	// Cùng HTX hoặc thửa nháp → cần xác nhận; HTX khác → chặn.
	same, cross := ClassifyOverlaps(coopA, hits, tol)
	if len(same) != 2 || same[0].Code != "a-lớn" || same[1].Code != "nháp" {
		t.Fatalf("cần xác nhận = %+v", same)
	}
	if len(cross) != 1 || cross[0].Code != "b" {
		t.Fatalf("bị chặn = %+v", cross)
	}
	// Thửa mới là thửa nháp: mọi chồng lấn chỉ cần xác nhận.
	same, cross = ClassifyOverlaps(uuid.Nil, hits, tol)
	if len(same) != 3 || len(cross) != 0 {
		t.Fatalf("thửa nháp = %+v / %+v", same, cross)
	}
	if got := AreaDeviationPct(decimal.RequireFromString("1.06"), decimal.NewFromInt(1)); !got.Equal(decimal.NewFromInt(6)) {
		t.Fatalf("AreaDeviationPct = %s", got)
	}
	if got := OverlapPct(decimal.NewFromInt(1), decimal.NewFromInt(10), decimal.NewFromInt(4)); !got.Equal(decimal.NewFromInt(25)) {
		t.Fatalf("OverlapPct theo thửa nhỏ hơn = %s", got)
	}
}
