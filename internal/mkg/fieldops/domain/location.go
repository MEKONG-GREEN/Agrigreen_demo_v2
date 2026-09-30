package domain

import (
	"bytes"
	"encoding/json"

	"github.com/shopspring/decimal"
)

// RuleLocation: dấu GPS của bản ghi đếm cuộn (G3-2) — tùy chọn, không bao giờ chặn lưu.
const RuleLocation = "fieldops.location.v1"

// Trạng thái GPS máy báo (app hiện trường: ok | weak | denied | unavailable; thêm timeout |
// no_device theo hợp đồng app). Khác → unknown; tọa độ sai → invalid (bỏ tọa độ).
var gpsStatuses = map[string]bool{"ok": true, "weak": true, "denied": true, "unavailable": true, "timeout": true, "no_device": true}

// Location là dấu GPS đã chuẩn hóa của một bản ghi đếm.
type Location struct {
	Status    string
	Lat, Lng  *decimal.Decimal // 7 chữ số thập phân; cùng có hoặc cùng không
	AccuracyM *decimal.Decimal // 2 chữ số
}

// ParseLocation chuẩn hóa gps ({lat, lng|lon, accuracy_m}) + gps_status của máy. Không
// bao giờ lỗi: thiếu cả hai → nil; trạng thái lạ → unknown (rỗng mà có tọa độ → ok); tọa
// độ hỏng/ngoài khoảng → invalid và bỏ tọa độ. Số đọc dạng json.Number → decimal (R-10).
func ParseLocation(status string, raw json.RawMessage) *Location {
	raw = bytes.TrimSpace(raw)
	hasFix := len(raw) > 0 && !bytes.Equal(raw, []byte("null"))
	if status == "" && !hasFix {
		return nil
	}
	l := &Location{Status: status}
	if !gpsStatuses[status] {
		l.Status = "unknown"
		if status == "" {
			l.Status = "ok"
		}
	}
	if !hasFix {
		return l
	}
	var g struct {
		Lat       json.Number `json:"lat"`
		Lng       json.Number `json:"lng"`
		Lon       json.Number `json:"lon"`
		AccuracyM json.Number `json:"accuracy_m"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&g); err != nil {
		l.Status = "invalid"
		return l
	}
	lngS := g.Lng
	if lngS == "" {
		lngS = g.Lon
	}
	lat, e1 := decimal.NewFromString(g.Lat.String())
	lng, e2 := decimal.NewFromString(lngS.String())
	if e1 != nil || e2 != nil || lat.Abs().GreaterThan(decimal.NewFromInt(90)) || lng.Abs().GreaterThan(decimal.NewFromInt(180)) {
		l.Status = "invalid"
		return l
	}
	lat, lng = lat.Round(7), lng.Round(7)
	l.Lat, l.Lng = &lat, &lng
	if g.AccuracyM != "" {
		if a, err := decimal.NewFromString(g.AccuracyM.String()); err == nil && !a.IsNegative() && a.LessThan(decimal.NewFromInt(10_000_000)) {
			a = a.Round(2)
			l.AccuracyM = &a
		}
	}
	return l
}
