// Package domain: quy tắc thuần của Báo cáo dịch bệnh BM-10 và báo nghi dịch (W11.49, D4 CN9, BRD CNTS v3.7 FN-11,
// UR-XA-10): ổ dịch là chuỗi phiên bản chỉ ghi thêm (mỗi lần gửi/cập nhật diễn biến/kết thúc là một phiên bản đủ trường),
// báo nghi dịch được cán bộ xã xác nhận thành ổ dịch hoặc bác bỏ.
package domain

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	extdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
)

// Phiên bản quy tắc của sổ dịch bệnh (ADR-0056 mục 7–8).
const (
	// RuleVersionV1: ổ dịch một hộ/địa bàn (W11.49) — dữ liệu cũ, chỉ đọc (API trả legacy = true). Báo nghi dịch vẫn v1.
	RuleVersionV1 = "kn.cnts.outbreak/v1"
	// RuleVersion: ổ dịch nhiều hộ (CNTS v4.0 FN-11 BR-03, #252) — DB-04 chọn hộ trong danh sách hộ chăn nuôi (bảng con
	// cnts_outbreak_household), DB-14 chụp theo hộ, DB-15 tự đếm, DB-16 diện tích thủy sản. Mọi phiên bản ổ dịch mới.
	RuleVersion = "kn.cnts.outbreak/v2"
)

// Sự việc của chuỗi ổ dịch.
const (
	EventReported = "reported" // Cấp Xã gửi Cấp Tỉnh (phiên bản 1)
	EventUpdated  = "updated"  // cập nhật diễn biến
	EventClosed   = "closed"   // Cấp Tỉnh xác nhận kết thúc ổ dịch
	EventReopened = "reopened" // Cấp Tỉnh mở lại (kết thúc nhầm, dịch tái phát)
)

// Trạng thái hiển thị của ổ dịch (BRD Mục V vòng đời BM-10: Lưu nháp → Đã gửi → Đang theo dõi → Đã kết thúc; "Lưu nháp"
// chỉ trên máy — như W11.42).
const (
	StatusReported   = "reported"   // Đã gửi
	StatusMonitoring = "monitoring" // Đang theo dõi
	StatusClosed     = "closed"     // Đã kết thúc
)

// StatusOf suy trạng thái từ sự việc của bản hiện hành.
func StatusOf(event string) string {
	switch event {
	case EventReported:
		return StatusReported
	case EventClosed:
		return StatusClosed
	default:
		return StatusMonitoring
	}
}

// Statuses là các trạng thái ổ dịch (lọc danh sách).
var Statuses = []string{StatusReported, StatusMonitoring, StatusClosed}

// Sự việc của báo nghi dịch (cũng là trạng thái).
const (
	SuspectReported  = "reported"  // Chờ xác nhận
	SuspectConfirmed = "confirmed" // Đã xác nhận → ổ dịch
	SuspectDismissed = "dismissed" // Bác bỏ
)

// SuspectStatuses là các trạng thái báo nghi dịch.
var SuspectStatuses = []string{SuspectReported, SuspectConfirmed, SuspectDismissed}

// Nguồn tin DB-03.
var Sources = []string{"ubnd_xa", "nong_ho", "can_bo"}

// Nguồn vị trí DB-05: GPS của thiết bị hoặc chọn trên bản đồ.
var LocationSources = []string{"gps", "map"}

// Giới hạn.
const (
	MaxAttachments = 10
	MaxText        = 2000
	MaxLocality    = 300
	MaxLabName     = 300
	MaxReason      = 1000
	// MaxHouseholds: số hộ tối đa của một phiên bản ổ dịch (DB-04, bảng con position 1–200).
	MaxHouseholds = 200
)

// Nhóm vật nuôi của danh mục cnts_species (FN-06): bệnh thuộc nhóm thủy sản khi loài của bệnh có attrs.group = thuy_san.
const (
	AttrSpeciesGroup = "group"
	GroupAquatic     = "thuy_san"
)

// Point là tọa độ WGS84 (độ thập phân, chuỗi để không qua float).
type Point struct {
	Lat decimal.Decimal
	Lon decimal.Decimal
}

// Khung tọa độ chấp nhận: lãnh thổ Việt Nam kể cả biển đảo (giả định W11.49 — chặn tọa độ đảo dấu/0,0).
var (
	latMin, latMax = decimal.NewFromInt(8), decimal.NewFromInt(24)
	lonMin, lonMax = decimal.NewFromInt(102), decimal.NewFromInt(118)
)

// InVietnam cho biết điểm nằm trong khung tọa độ chấp nhận.
func (p Point) InVietnam() bool {
	return !p.Lat.LessThan(latMin) && !p.Lat.GreaterThan(latMax) && !p.Lon.LessThan(lonMin) && !p.Lon.GreaterThan(lonMax)
}

// Outbreak là nội dung một phiên bản ổ dịch (DB-01…DB-13 + diễn biến).
type Outbreak struct {
	CommuneCode    string
	DiseaseCode    string
	SpeciesCode    string
	ReceivedAt     time.Time
	Source         string
	SuspectRef     *uuid.UUID
	HouseholdRef   *uuid.UUID
	Locality       string
	Location       *Point
	LocationSource string
	GPSAccuracyM   *decimal.Decimal
	HerdSize       int64
	HerdAgeDays    *int64
	Dead           int64
	Symptoms       string
	SampleDate     *time.Time
	LabName        string
	LabResult      string
	CulledHead     *int64
	CulledKg       *decimal.Decimal
	ChemicalLiters *decimal.Decimal
	BufferNote     string
	PreventionNote string
	AttachmentRefs []uuid.UUID
	AsOf           time.Time // ngày diễn biến (ngày nghiệp vụ, UTC 00:00)
	ProgressNote   string
	CapturedAt     *time.Time
	// AffectedHouseholds là DB-15 (số hộ ở DB-04) — dịch vụ đếm, không nhận từ API (AC-04). nil ở dữ liệu cũ v1.
	AffectedHouseholds *int64
	// AquaFarmedHa, AquaWildHa là DB-16: diện tích thủy sản bị ảnh hưởng (ha) — nuôi trồng, khai thác.
	AquaFarmedHa *decimal.Decimal
	AquaWildHa   *decimal.Decimal
}

// FieldError là lỗi của một trường.
type FieldError struct{ Field, Reason string }

func (e FieldError) Error() string { return e.Field + ": " + e.Reason }

// Errors là danh sách lỗi trường.
type Errors []FieldError

func (es Errors) Error() string {
	parts := make([]string, 0, len(es))
	for _, e := range es {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "; ")
}

func (es *Errors) add(field, reason string) { *es = append(*es, FieldError{field, reason}) }

var communeRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,20}$`)

func runes(s string) int { return utf8.RuneCountInString(s) }

func one(v string, set []string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// text chuẩn hóa ô văn bản: cắt khoảng trắng, độ dài ≤ max, không chứa số điện thoại/CCCD (dữ liệu cá nhân).
func text(es *Errors, field string, s *string, max int, required bool) {
	*s = strings.TrimSpace(*s)
	switch {
	case *s == "":
		if required {
			es.add(field, "bắt buộc")
		}
	case runes(*s) > max:
		es.add(field, "tối đa "+strconv.Itoa(max)+" ký tự")
	case extdomain.ContainsPII(*s):
		es.add(field, "không ghi số điện thoại, số CCCD (dữ liệu cá nhân)")
	}
}

func refs(es *Errors, field string, rs []uuid.UUID) {
	if len(rs) > MaxAttachments {
		es.add(field, "tối đa 10 tệp")
		return
	}
	seen := map[uuid.UUID]bool{}
	for _, r := range rs {
		if r == uuid.Nil || seen[r] {
			es.add(field, "mã tệp trống hoặc bị lặp")
			return
		}
		seen[r] = true
	}
}

func location(es *Errors, p *Point, acc *decimal.Decimal, required bool) {
	if p == nil {
		if required {
			es.add("location", "chưa có vị trí ổ dịch — lấy GPS hoặc chọn trên bản đồ (DB-05)")
		}
		return
	}
	if !p.InVietnam() {
		es.add("location", "tọa độ ngoài lãnh thổ Việt Nam (vĩ độ 8–24, kinh độ 102–118)")
	}
	if acc != nil && acc.IsNegative() {
		es.add("gps_accuracy_m", "không được âm")
	}
}

func nonNeg(es *Errors, field string, v *int64) {
	if v != nil && *v < 0 {
		es.add(field, "không được âm")
	}
}

// measure kiểm khối lượng/thể tích numeric(14,3): không âm, ≤ 11 chữ số phần nguyên, ≤ 3 số lẻ.
func measure(es *Errors, field string, v *decimal.Decimal) {
	switch {
	case v == nil:
	case v.IsNegative():
		es.add(field, "không được âm")
	case !v.Equal(v.Round(3)) || v.GreaterThanOrEqual(decimal.New(1, 11)):
		es.add(field, "tối đa 11 chữ số phần nguyên, 3 số lẻ")
	}
}

// NormalizeOutbreak chuẩn hóa và kiểm một phiên bản ổ dịch (UR-XA-10 quy tắc chặn; DB-01…DB-13). today là ngày hiện tại
// (giờ Việt Nam): ngày diễn biến, ngày lấy mẫu, thời điểm tiếp nhận không ở tương lai. Mã danh mục, xã kiểm ở ứng dụng.
func NormalizeOutbreak(o *Outbreak, now time.Time, today time.Time) Errors {
	var es Errors
	o.CommuneCode = strings.TrimSpace(o.CommuneCode)
	o.DiseaseCode = strings.TrimSpace(o.DiseaseCode)
	o.SpeciesCode = strings.TrimSpace(o.SpeciesCode)
	if !communeRe.MatchString(o.CommuneCode) {
		es.add("commune_code", "mã xã không hợp lệ")
	}
	if o.DiseaseCode == "" || runes(o.DiseaseCode) > 64 {
		es.add("disease_code", "chọn bệnh trong danh mục (DB-01)")
	}
	if o.SpeciesCode == "" || runes(o.SpeciesCode) > 64 {
		es.add("species_code", "chọn vật nuôi trong danh mục")
	}
	if o.ReceivedAt.IsZero() {
		es.add("received_at", "bắt buộc (DB-02)")
	} else if o.ReceivedAt.After(now.Add(5 * time.Minute)) {
		es.add("received_at", "không được ở tương lai")
	}
	if !one(o.Source, Sources) {
		es.add("source", "UBND xã, nông hộ báo hoặc cán bộ phát hiện (DB-03)")
	}
	// DB-04 v2: hộ chọn trong danh sách hộ chăn nuôi (kiểm ở ứng dụng); địa bàn chỉ là ghi chú tùy chọn. household_ref cũ
	// (v1) không ghi được nữa.
	if o.HouseholdRef != nil {
		es.add("household_ref", "dữ liệu cũ — chọn hộ trong danh sách hộ chăn nuôi (DB-04)")
	}
	text(&es, "locality", &o.Locality, MaxLocality, false)
	location(&es, o.Location, o.GPSAccuracyM, true)
	if o.Location != nil && !one(o.LocationSource, LocationSources) {
		es.add("location_source", "gps hoặc map")
	}
	if o.HerdSize < 1 || o.HerdSize > 100000000 {
		es.add("herd_size", "quy mô đàn phải ≥ 1 con (DB-06)")
	}
	if o.HerdAgeDays != nil && (*o.HerdAgeDays < 0 || *o.HerdAgeDays > 20000) {
		es.add("herd_age_days", "ngày tuổi 0–20000")
	}
	if o.Dead < 0 {
		es.add("dead", "không được âm")
	} else if o.HerdSize >= 1 && o.Dead > o.HerdSize {
		es.add("dead", "số con chết lớn hơn quy mô đàn (DB-07 > DB-06)")
	}
	text(&es, "symptoms", &o.Symptoms, MaxText, true)
	if o.SampleDate != nil && o.SampleDate.After(today) {
		es.add("sample_date", "ngày lấy mẫu ở tương lai")
	}
	text(&es, "lab_name", &o.LabName, MaxLabName, false)
	text(&es, "lab_result", &o.LabResult, MaxText, false)
	nonNeg(&es, "culled_head", o.CulledHead)
	if o.CulledHead != nil && o.HerdSize >= 1 && *o.CulledHead > o.HerdSize {
		es.add("culled_head", "số con tiêu hủy lớn hơn quy mô đàn (DB-10 > DB-06)")
	}
	measure(&es, "culled_kg", o.CulledKg)
	measure(&es, "chemical_liters", o.ChemicalLiters)
	text(&es, "buffer_note", &o.BufferNote, MaxText, false)
	text(&es, "prevention_note", &o.PreventionNote, MaxText, false)
	text(&es, "progress_note", &o.ProgressNote, MaxText, false)
	refs(&es, "attachment_refs", o.AttachmentRefs)
	if o.AsOf.IsZero() {
		es.add("as_of", "bắt buộc")
	} else if o.AsOf.After(today) {
		es.add("as_of", "ngày diễn biến ở tương lai")
	}
	return es
}

var haRe = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,2})?$`)

// ParseHectares đọc diện tích DB-16 (ha): số thập phân thường ≥ 0, ≤ 10 chữ số phần nguyên, ≤ 2 chữ số lẻ — kiểm cú pháp
// và độ dài TRƯỚC khi parse (chặn "1e100000", chuỗi rất dài). Rỗng → nil.
func ParseHectares(s string) (*decimal.Decimal, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	if len(s) > 13 || !haRe.MatchString(s) {
		return nil, false
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return nil, false
	}
	return &d, true
}

// CheckAqua kiểm DB-16 theo nhóm bệnh (UR-XA-10, FN-11 AC-05): bệnh nhóm thủy sản → bắt buộc ghi riêng nuôi trồng và
// khai thác (0 được); bệnh khác → không ghi.
func CheckAqua(o *Outbreak, aquatic bool) Errors {
	var es Errors
	switch {
	case aquatic:
		if o.AquaFarmedHa == nil {
			es.add("aqua_farmed_ha", "bắt buộc khi bệnh thuộc nhóm thủy sản (DB-16, nuôi trồng)")
		}
		if o.AquaWildHa == nil {
			es.add("aqua_wild_ha", "bắt buộc khi bệnh thuộc nhóm thủy sản (DB-16, khai thác)")
		}
	default:
		if o.AquaFarmedHa != nil {
			es.add("aqua_farmed_ha", "chỉ ghi khi bệnh thuộc nhóm thủy sản (DB-16)")
		}
		if o.AquaWildHa != nil {
			es.add("aqua_wild_ha", "chỉ ghi khi bệnh thuộc nhóm thủy sản (DB-16)")
		}
	}
	return es
}

// CheckHouseholds kiểm DB-04/DB-15 (UR-XA-10): ít nhất một hộ (chọn trong danh sách hoặc nhập mới), tối đa
// MaxHouseholds, hộ chọn không lặp.
func CheckHouseholds(selected []uuid.UUID, added int) Errors {
	var es Errors
	switch n := len(selected) + added; {
	case n == 0:
		es.add("households", "chọn ít nhất một hộ chăn nuôi hoặc nhập hộ chưa có (DB-04, DB-15 ≥ 1)")
	case n > MaxHouseholds:
		es.add("households", "tối đa "+strconv.Itoa(MaxHouseholds)+" hộ mỗi ổ dịch")
	}
	seen := map[uuid.UUID]bool{}
	for _, r := range selected {
		if r == uuid.Nil || seen[r] {
			es.add("households", "mã hộ trống hoặc bị lặp")
			break
		}
		seen[r] = true
	}
	return es
}

// Suspect là nội dung một tin báo nghi dịch.
type Suspect struct {
	CommuneCode    string
	ReporterKind   string
	ObservedAt     time.Time
	SpeciesCode    string
	DiseaseCode    string
	HouseholdRef   *uuid.UUID
	Locality       string
	Location       *Point
	GPSAccuracyM   *decimal.Decimal
	Affected       *int64
	Dead           *int64
	Description    string
	AttachmentRefs []uuid.UUID
	CapturedAt     *time.Time
}

// NormalizeSuspect chuẩn hóa và kiểm tin báo nghi dịch. Vị trí không bắt buộc (tin qua điện thoại) — bắt buộc khi xác
// nhận thành ổ dịch.
func NormalizeSuspect(s *Suspect, now time.Time) Errors {
	var es Errors
	s.CommuneCode = strings.TrimSpace(s.CommuneCode)
	s.SpeciesCode = strings.TrimSpace(s.SpeciesCode)
	s.DiseaseCode = strings.TrimSpace(s.DiseaseCode)
	if !communeRe.MatchString(s.CommuneCode) {
		es.add("commune_code", "mã xã không hợp lệ")
	}
	if !one(s.ReporterKind, Sources) {
		es.add("reporter_kind", "nông hộ, UBND xã hoặc cán bộ")
	}
	if s.ObservedAt.IsZero() {
		es.add("observed_at", "bắt buộc")
	} else if s.ObservedAt.After(now.Add(5 * time.Minute)) {
		es.add("observed_at", "không được ở tương lai")
	}
	if runes(s.SpeciesCode) > 64 {
		es.add("species_code", "mã quá dài")
	}
	if runes(s.DiseaseCode) > 64 {
		es.add("disease_code", "mã quá dài")
	}
	text(&es, "locality", &s.Locality, MaxLocality, s.HouseholdRef == nil)
	location(&es, s.Location, s.GPSAccuracyM, false)
	nonNeg(&es, "affected", s.Affected)
	nonNeg(&es, "dead", s.Dead)
	if s.Affected != nil && s.Dead != nil && *s.Dead > *s.Affected && *s.Affected > 0 {
		es.add("dead", "số con chết lớn hơn số con có biểu hiện bệnh")
	}
	text(&es, "description", &s.Description, MaxText, true)
	refs(&es, "attachment_refs", s.AttachmentRefs)
	return es
}

// Reason chuẩn hóa lý do kết thúc/mở lại/bác bỏ (bắt buộc, ≤ 1000 ký tự, không PII).
func Reason(s string) (string, Errors) {
	var es Errors
	text(&es, "reason", &s, MaxReason, true)
	return s, es
}
