package domain

// W11.46 (D4 CN6, BRD CNTS v3.7 FN-12, UR-XA-05, CV-06/CV-07) — địa điểm công tác và check-in GPS của lịch công tác.
// Quy tắc thuần: khoảng cách tới địa điểm đã đăng ký, cờ xa/GPS yếu, kiểm thời điểm check-in.

import (
	"errors"
	"math"
	"time"
)

// Nguồn địa điểm công tác.
const (
	PlaceSourceMap = "map"
	PlaceSourceGPS = "gps"
)

// Mặc định của check-in (tham số theo phòng ban ghi đè — port.ParamScheduleCheckin*).
const (
	DefaultCheckinFarM           = 500 // FN-12: check-in cách địa điểm > 500 m → cảnh báo
	DefaultCheckinAccuracyM      = 50  // BRD Mục VIII: cảnh báo khi độ chính xác > 50 m (cnts.gps_accuracy_m)
	DefaultCheckinRetentionDays  = 90  // hạn lưu tọa độ check-in (giả định W11.46, chờ PO)
	MaxCheckinRetentionDays      = 400 // trần hạn lưu
	CheckinMaxLateDays           = 7   // bản ngoại tuyến gửi muộn tối đa 7 ngày sau ngày công tác
	CheckinClockSkew             = 5 * time.Minute
	earthRadiusM                 = 6371008.8
	maxCheckinAccuracyM          = 100000
	vietnamLatMin, vietnamLatMax = 8.0, 24.0
	vietnamLonMin, vietnamLonMax = 102.0, 118.0
)

// Lỗi quy tắc địa điểm / check-in.
var (
	ErrPointRange      = errors.New("tọa độ phải nằm trong Việt Nam (vĩ độ 8–24, kinh độ 102–118)")
	ErrPlaceSource     = errors.New("nguồn địa điểm phải là map hoặc gps")
	ErrCheckinAccuracy = errors.New("độ chính xác (m) phải từ 0 đến 100000")
	ErrCheckinFuture   = errors.New("thời điểm check-in ở tương lai")
	ErrCheckinWrongDay = errors.New("chỉ check-in trong ngày công tác của mục (giờ Việt Nam)")
	ErrCheckinTooLate  = errors.New("bản check-in ngoại tuyến gửi quá 7 ngày sau ngày công tác")
)

// GeoPoint là một điểm WGS84 (độ thập phân).
type GeoPoint struct {
	Lat, Lon float64
	Source   string // PlaceSourceMap | PlaceSourceGPS (chỉ địa điểm công tác)
}

// Equal so hai điểm (nil = không có).
func (p *GeoPoint) Equal(o *GeoPoint) bool {
	if p == nil || o == nil {
		return p == nil && o == nil
	}
	return math.Abs(p.Lat-o.Lat) < 1e-7 && math.Abs(p.Lon-o.Lon) < 1e-7 && p.Source == o.Source
}

// InVietnam kiểm khung tọa độ Việt Nam (như ổ dịch W11.49).
func (p GeoPoint) InVietnam() bool {
	return p.Lat >= vietnamLatMin && p.Lat <= vietnamLatMax && p.Lon >= vietnamLonMin && p.Lon <= vietnamLonMax
}

// Normalize kiểm địa điểm công tác: trong Việt Nam, nguồn hợp lệ (rỗng = map); làm tròn 7 chữ số (~1 cm).
func (p GeoPoint) Normalize() (GeoPoint, error) {
	if math.IsNaN(p.Lat) || math.IsNaN(p.Lon) || !p.InVietnam() {
		return p, ErrPointRange
	}
	if p.Source == "" {
		p.Source = PlaceSourceMap
	}
	if p.Source != PlaceSourceMap && p.Source != PlaceSourceGPS {
		return p, ErrPlaceSource
	}
	p.Lat, p.Lon = round7(p.Lat), round7(p.Lon)
	return p, nil
}

func round7(v float64) float64 { return math.Round(v*1e7) / 1e7 }

// DistanceM là khoảng cách mặt cầu (haversine) giữa hai điểm, mét.
func DistanceM(a, b GeoPoint) float64 {
	rad := math.Pi / 180
	dLat, dLon := (b.Lat-a.Lat)*rad, (b.Lon-a.Lon)*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

// CheckinInput là một lần bấm "Check-in" (CV-07).
type CheckinInput struct {
	Point      GeoPoint
	AccuracyM  *float64
	CapturedAt time.Time
	Mock       bool
}

// CheckinEval là kết quả đánh giá check-in (lưu ở sổ, không có tọa độ).
type CheckinEval struct {
	DistanceM    *float64 // nil = mục không có địa điểm trên bản đồ
	Far          bool
	WeakAccuracy bool
}

// EvaluateCheckin kiểm và đánh giá check-in của mục ngày itemDay: tọa độ trong Việt Nam, sai số hợp lệ, thời điểm
// thiết bị thuộc ngày công tác (giờ zone), không ở tương lai (cho phép lệch đồng hồ 5 phút), gửi muộn ≤ 7 ngày; khoảng
// cách tới địa điểm planned (nil → không tính), xa khi > farM, GPS yếu khi sai số > accM.
func EvaluateCheckin(in CheckinInput, itemDay time.Time, planned *GeoPoint, now time.Time, zone *time.Location, farM, accM float64) (CheckinEval, error) {
	var ev CheckinEval
	if math.IsNaN(in.Point.Lat) || math.IsNaN(in.Point.Lon) || !in.Point.InVietnam() {
		return ev, FieldError{Field: "lat", Err: ErrPointRange}
	}
	if a := in.AccuracyM; a != nil && (math.IsNaN(*a) || *a < 0 || *a > maxCheckinAccuracyM) {
		return ev, FieldError{Field: "accuracy_m", Err: ErrCheckinAccuracy}
	}
	if in.CapturedAt.After(now.Add(CheckinClockSkew)) {
		return ev, FieldError{Field: "captured_at", Err: ErrCheckinFuture}
	}
	if !civil(in.CapturedAt, zone).Equal(itemDay) {
		return ev, FieldError{Field: "captured_at", Err: ErrCheckinWrongDay}
	}
	if civil(now, zone).After(itemDay.AddDate(0, 0, CheckinMaxLateDays)) {
		return ev, FieldError{Field: "captured_at", Err: ErrCheckinTooLate}
	}
	if planned != nil {
		d := math.Round(DistanceM(*planned, in.Point)*10) / 10
		ev.DistanceM, ev.Far = &d, d > farM
	}
	ev.WeakAccuracy = in.AccuracyM != nil && *in.AccuracyM > accM
	return ev, nil
}

// FieldWork cho biết mục là công tác ngoài cơ quan (cần check-in, FN-12 "Ngày công tác ngoài cơ quan chưa check-in →
// Cảnh báo"): có địa điểm trên bản đồ hoặc ghi địa điểm / xã / HTX.
func (it ScheduleItem) FieldWork() bool {
	return it.Place != nil || it.Location != "" || it.CommuneCode != ""
}

// MissedCheckin: mục công tác ngoài cơ quan, ngày đã qua (giờ zone) mà chưa check-in.
func MissedCheckin(it ScheduleItem, checkedIn bool, now time.Time, zone *time.Location) bool {
	return !checkedIn && it.FieldWork() && civil(now, zone).After(it.Day)
}
