package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// Diện tích khai báo: kiểm cú pháp, độ dài TRƯỚC khi parse (§4: 1e100000, 1e-100000, chuỗi rất dài).
func TestParseDeclared(t *testing.T) {
	for _, ok := range []string{`"1.0"`, `1.25`, `"1234567.1234"`, `null`, ``} {
		if _, why := ParseDeclared(json.RawMessage(ok)); why != "" {
			t.Errorf("%s phải hợp lệ: %s", ok, why)
		}
	}
	for _, bad := range []string{`"1e100000"`, `1e-100000`, `"-1"`, `"0"`, `"12345678"`, `"1.12345"`, `"abc"`, strings.Repeat("9", 400)} {
		if _, why := ParseDeclared(json.RawMessage(bad)); why == "" {
			t.Errorf("%.20s phải bị chặn", bad)
		}
	}
}

// FN-XA-03.05 (A2): ≥ 3 điểm không thẳng hàng; vòng chưa khép tự khép; chỉ Polygon/MultiPolygon WGS84.
func TestParseGeometry(t *testing.T) {
	tri := `{"type":"Polygon","coordinates":[[[105.6,10.05],[105.601,10.05],[105.6,10.051]]]}`
	s, err := ParseGeometry(json.RawMessage(tri))
	if err != nil || s.Points != 3 || !strings.HasPrefix(s.GeoJSON, `{"type":"MultiPolygon"`) {
		t.Fatalf("tam giác: %+v %v", s, err)
	}
	for raw, code := range map[string]string{
		`{"type":"Polygon","coordinates":[[[105.6,10.05],[105.601,10.05],[105.602,10.05]]]}`:      CodeTooFewPoints,
		`{"type":"LineString","coordinates":[[105.6,10.05],[105.601,10.05]]}`:                     CodeInvalidGeometry,
		`{"type":"Polygon","coordinates":[[[580000,1110000],[580100,1110000],[580100,1110100]]]}`: CodeCRSUnsupported,
		`null`: CodeInvalidGeometry,
	} {
		_, err := ParseGeometry(json.RawMessage(raw))
		if v, ok := err.(*Violation); !ok || v.Code != code {
			t.Errorf("%.40s → muốn %s, nhận %v", raw, code, err)
		}
	}
}
