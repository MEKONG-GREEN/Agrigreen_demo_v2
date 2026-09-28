package domain

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"
)

// Định dạng tệp nhập (KN US-PLOT-02, GIS FN-05).
const (
	FormatGeoJSON   = "geojson"
	FormatKML       = "kml"
	FormatShapefile = "shapefile"
)

// CodeFormatUnsupported: định dạng tệp chưa hỗ trợ.
const CodeFormatUnsupported = "geo.format_unsupported"

// MaxFeatures mỗi lần nhập — xử lý đồng bộ dưới 2 giây (R-52).
const MaxFeatures = 200

// Feature là một đối tượng trong tệp nhập. Err khác nil: đối tượng bị loại (vd không phải vùng).
type Feature struct {
	Index  int // thứ tự trong tệp, từ 1
	Name   string
	Props  map[string]string
	Geom   MultiPolygon
	Points int
	Err    error
}

// DetectFormat đoán định dạng từ tham số, tên tệp hoặc nội dung.
func DetectFormat(explicit, fileName string, data []byte) string {
	switch f := strings.ToLower(strings.TrimSpace(explicit)); f {
	case FormatGeoJSON, "json":
		return FormatGeoJSON
	case FormatKML, FormatShapefile:
		return f
	case "shp", "zip":
		return FormatShapefile
	}
	name := strings.ToLower(fileName)
	switch {
	case strings.HasSuffix(name, ".geojson") || strings.HasSuffix(name, ".json"):
		return FormatGeoJSON
	case strings.HasSuffix(name, ".kml"):
		return FormatKML
	case strings.HasSuffix(name, ".shp") || strings.HasSuffix(name, ".zip"):
		return FormatShapefile
	}
	t := bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	switch {
	case len(t) > 0 && t[0] == '{':
		return FormatGeoJSON
	case len(t) > 0 && t[0] == '<':
		return FormatKML
	case bytes.HasPrefix(data, []byte{0x00, 0x00, 0x27, 0x0a}) || bytes.HasPrefix(data, []byte("PK")):
		return FormatShapefile // mã tệp .shp hoặc gói zip
	}
	return ""
}

// ParseFeatures đọc tệp theo định dạng. Lỗi trả về là lỗi cả tệp; lỗi từng đối
// tượng nằm ở Feature.Err.
func ParseFeatures(format string, data []byte) ([]Feature, error) {
	var feats []Feature
	var err error
	switch format {
	case FormatGeoJSON:
		feats, err = parseGeoJSONFeatures(data)
	case FormatKML:
		feats, err = parseKML(data)
	case FormatShapefile:
		feats, err = parseShapefileZip(data)
	default:
		return nil, violation(CodeFormatUnsupported, "Không nhận ra định dạng tệp — hỗ trợ GeoJSON (.geojson/.json), KML (.kml) và Shapefile (gói .zip)")
	}
	if err != nil {
		return nil, err
	}
	if len(feats) == 0 {
		return nil, violation(CodeFormat, "Tệp không có đối tượng nào")
	}
	if len(feats) > MaxFeatures {
		return nil, violation(CodeFormat, "Tối đa "+strconv.Itoa(MaxFeatures)+" đối tượng mỗi lần nhập")
	}
	return feats, nil
}

// ---------------------------------------------------------------------------
// GeoJSON
// ---------------------------------------------------------------------------

type rawFeature struct {
	Type       string                     `json:"type"`
	Geometry   json.RawMessage            `json:"geometry"`
	Properties map[string]json.RawMessage `json:"properties"`
	Features   []json.RawMessage          `json:"features"`
	CRS        json.RawMessage            `json:"crs"`
}

func parseGeoJSONFeatures(data []byte) ([]Feature, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var top rawFeature
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, violation(CodeFormat, "Tệp GeoJSON không hợp lệ")
	}
	if err := CheckCRS(top.CRS); err != nil {
		return nil, err
	}
	switch top.Type {
	case "FeatureCollection":
		out := make([]Feature, 0, len(top.Features))
		for i, raw := range top.Features {
			var f rawFeature
			if err := json.Unmarshal(raw, &f); err != nil || f.Type != "Feature" {
				out = append(out, Feature{Index: i + 1, Err: violation(CodeFormat, "đối tượng không phải GeoJSON Feature")})
				continue
			}
			out = append(out, geoJSONFeature(i+1, f))
		}
		return out, nil
	case "Feature":
		return []Feature{geoJSONFeature(1, top)}, nil
	case "Polygon", "MultiPolygon":
		mp, n, err := ParseGeometry(data)
		return []Feature{{Index: 1, Props: map[string]string{}, Geom: mp, Points: n, Err: err}}, nil
	}
	return nil, violation(CodeFormat, "GeoJSON phải là FeatureCollection, Feature hoặc Polygon/MultiPolygon")
}

func geoJSONFeature(idx int, f rawFeature) Feature {
	out := Feature{Index: idx, Props: map[string]string{}}
	for k, v := range f.Properties {
		var s string
		if json.Unmarshal(v, &s) == nil {
			out.Props[k] = strings.TrimSpace(s)
			continue
		}
		if t := strings.TrimSpace(string(v)); t != "null" {
			out.Props[k] = t // số, bool: giữ dạng chữ
		}
	}
	out.Name = firstProp(out.Props, "name", "ten", "ten_thua", "Name")
	if err := CheckCRS(f.CRS); err != nil {
		out.Err = err
		return out
	}
	out.Geom, out.Points, out.Err = ParseGeometry(f.Geometry)
	return out
}

func firstProp(p map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := p[k]; v != "" {
			return v
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// KML (OGC 07-147r2) — luôn là WGS84 kinh/vĩ độ theo đặc tả
// ---------------------------------------------------------------------------

type kmlPlacemark struct {
	name   strings.Builder
	props  map[string]string
	polys  []Polygon
	other  string
	badPos bool
}

// parseKML đọc mọi Placemark, lấy Polygon (kể cả trong MultiGeometry), tên và
// ExtendedData (Data/value, SimpleData) làm thuộc tính.
func parseKML(data []byte) ([]Feature, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var (
		out      []Feature
		cur      *kmlPlacemark
		stack    []string
		text     strings.Builder
		dataName string
		inPoly   bool
	)
	within := func(name string) bool {
		for _, s := range stack {
			if s == name {
				return true
			}
		}
		return false
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, violation(CodeFormat, "Tệp KML không hợp lệ (lỗi cú pháp XML)")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			stack = append(stack, name)
			text.Reset()
			switch name {
			case "Placemark":
				cur = &kmlPlacemark{props: map[string]string{}}
			case "Polygon":
				if cur != nil {
					cur.polys = append(cur.polys, Polygon{})
					inPoly = true
				}
			case "Point", "LineString", "LinearRing":
				if cur != nil && !inPoly && cur.other == "" {
					cur.other = name
				}
			case "Data", "SimpleData":
				dataName = ""
				for _, a := range t.Attr {
					if a.Name.Local == "name" {
						dataName = strings.TrimSpace(a.Value)
					}
				}
			}
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			name := t.Name.Local
			if cur != nil {
				switch name {
				case "name":
					if len(stack) >= 2 && stack[len(stack)-2] == "Placemark" {
						cur.name.WriteString(strings.TrimSpace(text.String()))
					}
				case "value":
					if within("Data") && dataName != "" {
						cur.props[dataName] = strings.TrimSpace(text.String())
					}
				case "SimpleData":
					if dataName != "" {
						cur.props[dataName] = strings.TrimSpace(text.String())
					}
				case "coordinates":
					if inPoly && len(cur.polys) > 0 {
						r, ok := parseKMLCoords(text.String())
						if !ok {
							cur.badPos = true
						}
						p := &cur.polys[len(cur.polys)-1]
						switch {
						case within("innerBoundaryIs"):
							p.Holes = append(p.Holes, r)
						case within("outerBoundaryIs"):
							p.Outer = r
						}
					}
				case "Polygon":
					inPoly = false
				case "Placemark":
					out = append(out, cur.feature(len(out)+1))
					cur = nil
				}
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			text.Reset()
		}
	}
	return out, nil
}

func (k *kmlPlacemark) feature(idx int) Feature {
	f := Feature{Index: idx, Name: k.name.String(), Props: k.props}
	if f.Name == "" {
		f.Name = firstProp(k.props, "name", "ten", "ten_thua")
	}
	switch {
	case k.badPos:
		f.Err = violation(CodeInvalidGeometry, "toạ độ KML phải có dạng kinh_độ,vĩ_độ[,độ_cao]")
	case len(k.polys) == 0 && k.other != "":
		f.Err = violation(CodeInvalidGeometry, "chỉ nhận vùng khép kín (Polygon), không nhận "+k.other)
	case len(k.polys) == 0:
		f.Err = violation(CodeInvalidGeometry, "Placemark không có Polygon")
	default:
		f.Geom = MultiPolygon(k.polys)
		f.Points, f.Err = f.Geom.Normalize()
	}
	return f
}

// parseKMLCoords đọc chuỗi "lon,lat[,alt] lon,lat[,alt] ...".
func parseKMLCoords(s string) (Ring, bool) {
	var r Ring
	for _, tuple := range strings.Fields(s) {
		parts := strings.Split(tuple, ",")
		if len(parts) < 2 {
			return nil, false
		}
		lon, err1 := strconv.ParseFloat(parts[0], 64)
		lat, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 != nil || err2 != nil {
			return nil, false
		}
		r = append(r, Position{lon, lat})
	}
	return r, true
}
