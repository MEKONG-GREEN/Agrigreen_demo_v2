package domain

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Loại sự cố của yêu cầu hỗ trợ (App HTX, nguyên mẫu SUPPORT_CATEGORIES) và loại
// nhiệm vụ tương ứng của App Khuyến nông.
var supportCategoryTask = map[string]string{
	"dich_hai":  "sau_benh",
	"benh_lua":  "sau_benh",
	"thoi_tiet": "khac",
	"ky_thuat":  "ky_thuat",
	"may_moc":   "co_gioi",
	"khac":      "khac",
}

// Mức khẩn (nguyên mẫu SUPPORT_PRIORITIES) và ưu tiên nhiệm vụ tương ứng.
var supportUrgencyPriority = map[string]string{"binh_thuong": "normal", "khan": "urgent"}

// Giới hạn yêu cầu hỗ trợ (HTX US-SUPPORT-01 AC-3).
const (
	MaxSupportPhotos = 5
	// UrgencyNormal là mức khẩn mặc định.
	UrgencyNormal = "binh_thuong"
)

// acMessage là thông điệp hiển thị nguyên văn theo AC của backlog (viết hoa, có dấu chấm).
type acMessage string

func (m acMessage) Error() string { return string(m) }

// Thông điệp theo AC của backlog HTX US-SUPPORT-01.
var (
	ErrSupportRequired error = acMessage("Vui lòng nhập mô tả và chọn loại sự cố trước khi gửi.")
	ErrSupportPhotos   error = acMessage("Vượt quá số lượng ảnh cho phép (tối đa 5) hoặc dung lượng ảnh quá lớn.")
	ErrSupportGPS            = errors.New("toạ độ không hợp lệ: vĩ độ −90…90, kinh độ −180…180, độ chính xác ≥ 0 (m)")
)

// LabelGPSMissing là nhãn của yêu cầu không có vị trí GPS (HTX US-SUPPORT-01 AC-1).
const LabelGPSMissing = "Thiếu vị trí GPS"

// Point là toạ độ WGS84 (decimal — không float trong domain, dựng geometry ở store).
type Point struct {
	Lat, Lon decimal.Decimal
}

// SupportRequest là yêu cầu hỗ trợ của nông dân (sự việc chỉ ghi thêm).
type SupportRequest struct {
	ID             uuid.UUID
	HouseholdRef   uuid.UUID
	CooperativeRef uuid.UUID
	CommuneCode    string
	ProvinceCode   string
	PlotRef        uuid.UUID
	Category       string
	Urgency        string
	Description    string
	PhotoRefs      []uuid.UUID
	Location       *Point
	GPSAccuracyM   *decimal.Decimal
	EventTime      time.Time
	ReceivedAt     time.Time
	TaskID         uuid.UUID
	TaskCreatedAt  time.Time
	AssigneeRef    uuid.UUID
	AssignmentRule string
	SubmittedBy    uuid.UUID
	DeviceID       *string
	IdemKey        string
	// Trạng thái nhiệm vụ đi kèm khi đọc (không lưu ở support_request).
	TaskStatus Status
	TaskSeq    int64
	LastNote   string
}

// GPSMissing: không có vị trí → gắn nhãn "Thiếu vị trí GPS" nhưng vẫn nhận (AC-1).
func (r SupportRequest) GPSMissing() bool { return r.Location == nil }

// TaskCategory trả loại nhiệm vụ của App Khuyến nông cho loại sự cố.
func (r SupportRequest) TaskCategory() string { return supportCategoryTask[r.Category] }

// TaskPriority trả mức ưu tiên nhiệm vụ cho mức khẩn.
func (r SupportRequest) TaskPriority() string { return supportUrgencyPriority[r.Urgency] }

// Validate kiểm yêu cầu (mô tả + loại bắt buộc, ≤ 5 ảnh, toạ độ hợp lệ, không PII trong mô tả).
func (r *SupportRequest) Validate() error {
	r.Description = strings.TrimSpace(r.Description)
	if r.Urgency == "" {
		r.Urgency = UrgencyNormal
	}
	if _, ok := supportCategoryTask[r.Category]; !ok || r.Description == "" {
		field := "category"
		if r.Description == "" {
			field = "description"
		}
		return FieldError{Field: field, Err: ErrSupportRequired}
	}
	if _, ok := supportUrgencyPriority[r.Urgency]; !ok {
		return fieldErr("urgency", "phải là binh_thuong hoặc khan")
	}
	if n := utf8.RuneCountInString(r.Description); n < 10 || n > 2000 {
		return fieldErr("description", "10–2000 ký tự")
	}
	if ContainsPII(r.Description) {
		return FieldError{Field: "description", Err: ErrPIIInText}
	}
	if len(r.PhotoRefs) > MaxSupportPhotos {
		return FieldError{Field: "photo_refs", Err: ErrSupportPhotos}
	}
	seen := map[uuid.UUID]bool{}
	for _, f := range r.PhotoRefs {
		if f == uuid.Nil || seen[f] {
			return fieldErr("photo_refs", "mã tệp trống hoặc bị lặp")
		}
		seen[f] = true
	}
	if r.Location != nil {
		lat, lon := r.Location.Lat, r.Location.Lon
		if lat.Abs().GreaterThan(decimal.NewFromInt(90)) || lon.Abs().GreaterThan(decimal.NewFromInt(180)) {
			return FieldError{Field: "location", Err: ErrSupportGPS}
		}
	}
	if r.GPSAccuracyM != nil {
		if r.Location == nil || r.GPSAccuracyM.IsNegative() {
			return FieldError{Field: "gps_accuracy_m", Err: ErrSupportGPS}
		}
	}
	return nil
}

// DutyCandidate là một cán bộ trong danh bạ kèm trạng thái trực tại thời điểm nhận.
type DutyCandidate struct {
	Staff  Staff
	Manual *ManualStatus
	Shifts []Shift
}

// PickAssignee chọn người nhận nhiệm vụ tự sinh tại now theo chuỗi dự phòng của cây
// cấu trúc (KN US-TASK-01 AC-2):
//  1. cán bộ phụ trách XÃ đang trực (DutyAt = on_duty);
//  2. cán bộ cấp TỈNH (unit = tỉnh) đang trực;
//  3. cán bộ cấp tỉnh đang hoạt động bất kỳ;
//  4. không có ai → rule admin_queue (để trống, quản trị tỉnh gán lại).
//
// Trong cùng một bậc chọn người có ít nhiệm vụ đang mở nhất (openLoad), rồi theo actor_ref.
func PickAssignee(now time.Time, commune, province string, cands []DutyCandidate, openLoad map[uuid.UUID]int,
	ruleCommune, ruleProvinceOnDuty, ruleProvince, ruleAdmin string) (uuid.UUID, string) {
	onDuty := func(c DutyCandidate) bool {
		st, _, _ := DutyAt(now, c.Manual, c.Shifts)
		return st == DutyOn
	}
	pick := func(match func(DutyCandidate) bool) uuid.UUID {
		var pool []DutyCandidate
		for _, c := range cands {
			if c.Staff.Active && match(c) {
				pool = append(pool, c)
			}
		}
		if len(pool) == 0 {
			return uuid.Nil
		}
		sort.SliceStable(pool, func(i, j int) bool {
			li, lj := openLoad[pool[i].Staff.ActorRef], openLoad[pool[j].Staff.ActorRef]
			if li != lj {
				return li < lj
			}
			return pool[i].Staff.ActorRef.String() < pool[j].Staff.ActorRef.String()
		})
		return pool[0].Staff.ActorRef
	}
	provinceLevel := func(c DutyCandidate) bool { return c.Staff.UnitCode == province && province != "" }
	if a := pick(func(c DutyCandidate) bool { return c.Staff.UnitCode == commune && onDuty(c) }); a != uuid.Nil {
		return a, ruleCommune
	}
	if a := pick(func(c DutyCandidate) bool { return provinceLevel(c) && onDuty(c) }); a != uuid.Nil {
		return a, ruleProvinceOnDuty
	}
	if a := pick(provinceLevel); a != uuid.Nil {
		return a, ruleProvince
	}
	return uuid.Nil, ruleAdmin
}

// ValidSupportCategory: loại sự cố có trong danh mục (bộ lọc danh sách cán bộ).
func ValidSupportCategory(c string) bool { _, ok := supportCategoryTask[c]; return ok }

// ValidSupportUrgency: mức khẩn hợp lệ.
func ValidSupportUrgency(u string) bool { _, ok := supportUrgencyPriority[u]; return ok }

// SupportCategories trả các loại sự cố hợp lệ (cho tài liệu API và app).
func SupportCategories() []string {
	out := make([]string, 0, len(supportCategoryTask))
	for k := range supportCategoryTask {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
