package domain

import (
	"encoding/json"
	"testing"
)

// G3-2: dấu GPS tùy chọn không bao giờ chặn lưu; số đọc dạng thập phân (R-10).
func TestParseLocation(t *testing.T) {
	str := func(l *Location) string {
		if l == nil {
			return "nil"
		}
		s := l.Status
		if l.Lat != nil {
			s += "|" + l.Lat.String() + "|" + l.Lng.String()
		}
		if l.AccuracyM != nil {
			s += "|" + l.AccuracyM.String()
		}
		return s
	}
	for _, c := range []struct {
		status, raw, want string
	}{
		{"", "", "nil"},
		{"", "null", "nil"},
		{"denied", "", "denied"},
		{"ok", `{"lat": 10.45200015, "lng": 105.341, "accuracy_m": 6.456}`, "ok|10.4520002|105.341|6.46"},
		{"weak", `{"lat": 9.78, "lon": 105.47}`, "weak|9.78|105.47"},
		{"", `{"lat": 1, "lng": 2}`, "ok|1|2"},
		{"sao-hoa", "", "unknown"},
		{"ok", `{"lat": 91, "lng": 0}`, "invalid"},
		{"ok", `{"lat": "x"}`, "invalid"},
		{"ok", `[1,2]`, "invalid"},
		{"ok", `{"lat": 1, "lng": 2, "accuracy_m": -3}`, "ok|1|2"},
	} {
		if got := str(ParseLocation(c.status, json.RawMessage(c.raw))); got != c.want {
			t.Errorf("%q %s: %s, muốn %s", c.status, c.raw, got, c.want)
		}
	}
}
