package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Yêu cầu hỗ trợ của nông dân App HTX (HTX US-SUPPORT-01) — PA1-HTX-SUPPORT-GPSLOG (#313): chuyển từ pf/extension về
// module chủ HTX, cùng quy tắc nhận (loại, mức khẩn, mô tả, ảnh, toạ độ). Không còn tự sinh nhiệm vụ khuyến nông.

// supportCategories là loại sự cố (nguyên mẫu SUPPORT_CATEGORIES của App HTX).
var supportCategories = map[string]bool{"dich_hai": true, "benh_lua": true, "thoi_tiet": true, "ky_thuat": true, "may_moc": true, "khac": true}

// supportUrgencies là mức khẩn (nguyên mẫu SUPPORT_PRIORITIES).
var supportUrgencies = map[string]bool{"binh_thuong": true, "khan": true}

// Giới hạn và nhãn theo AC của backlog HTX US-SUPPORT-01.
const (
	MaxSupportPhotos = 5
	UrgencyNormal    = "binh_thuong"
	// LabelGPSMissing là nhãn của yêu cầu không có vị trí GPS (AC-1).
	LabelGPSMissing = "Thiếu vị trí GPS"
	// MsgSupportRequired, MsgSupportPhotos: thông điệp nguyên văn AC-3.
	MsgSupportRequired = "Vui lòng nhập mô tả và chọn loại sự cố trước khi gửi."
	MsgSupportPhotos   = "Vượt quá số lượng ảnh cho phép (tối đa 5) hoặc dung lượng ảnh quá lớn."
	msgSupportGPS      = "toạ độ không hợp lệ: vĩ độ −90…90, kinh độ −180…180, độ chính xác ≥ 0 (m)"
	msgSupportPII      = "không ghi SĐT hoặc số CCCD vào mô tả — chọn nông hộ bằng mã tham chiếu"
)

// Point là toạ độ WGS84 (decimal — không float trong domain).
type Point struct {
	Lat, Lon decimal.Decimal
}

// SupportRequest là yêu cầu hỗ trợ (sự việc chỉ ghi thêm).
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
	SubmittedBy    uuid.UUID
	DeviceID       *string
	IdemKey        string
	SyncSeq        int64
	// LegacyTaskStatus: trạng thái nhiệm vụ khuyến nông lúc chép (chỉ yêu cầu chép từ pf/extension, #313); "" với yêu cầu mới.
	LegacyTaskStatus string
}

// GPSMissing: không có vị trí → nhãn "Thiếu vị trí GPS" nhưng vẫn nhận (AC-1).
func (r SupportRequest) GPSMissing() bool { return r.Location == nil }

// Validate kiểm yêu cầu (mô tả + loại bắt buộc, ≤ 5 ảnh, toạ độ hợp lệ, không PII trong mô tả).
func (r *SupportRequest) Validate() error {
	r.Description = strings.TrimSpace(r.Description)
	if r.Urgency == "" {
		r.Urgency = UrgencyNormal
	}
	switch {
	case r.Description == "":
		return FieldError{"description", MsgSupportRequired}
	case !supportCategories[r.Category]:
		return FieldError{"category", MsgSupportRequired}
	case !supportUrgencies[r.Urgency]:
		return FieldError{"urgency", "phải là binh_thuong hoặc khan"}
	}
	if n := utf8.RuneCountInString(r.Description); n < 10 || n > 2000 {
		return FieldError{"description", "10–2000 ký tự"}
	}
	if ContainsPII(r.Description) {
		return FieldError{"description", msgSupportPII}
	}
	if len(r.PhotoRefs) > MaxSupportPhotos {
		return FieldError{"photo_refs", MsgSupportPhotos}
	}
	seen := map[uuid.UUID]bool{}
	for _, f := range r.PhotoRefs {
		if f == uuid.Nil || seen[f] {
			return FieldError{"photo_refs", "mã tệp trống hoặc bị lặp"}
		}
		seen[f] = true
	}
	if r.Location != nil && (r.Location.Lat.Abs().GreaterThan(maxLat) || r.Location.Lon.Abs().GreaterThan(maxLon)) {
		return FieldError{"location", msgSupportGPS}
	}
	if r.GPSAccuracyM != nil && (r.Location == nil || r.GPSAccuracyM.IsNegative()) {
		return FieldError{"gps_accuracy_m", msgSupportGPS}
	}
	return nil
}

// ValidSupportCategory: loại sự cố có trong danh mục (bộ lọc danh sách quản lý HTX).
func ValidSupportCategory(c string) bool { return supportCategories[c] }

// ValidSupportUrgency: mức khẩn hợp lệ.
func ValidSupportUrgency(u string) bool { return supportUrgencies[u] }
