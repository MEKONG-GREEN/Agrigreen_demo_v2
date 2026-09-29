package domain

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Kind là loại cơ sở.
type Kind string

// Các loại cơ sở của MKG.
const (
	KindHub       Kind = "hub"       // hub sơ chế: sấy, nén, lưu
	KindWarehouse Kind = "warehouse" // kho lưu
	KindYard      Kind = "yard"      // bãi tập kết
)

// Status là trạng thái dữ liệu chủ. Không có "đã xóa" (RB-1): ngừng dùng là phiên bản mới.
type Status string

// Trạng thái cơ sở.
const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
)

// Decision là quyết định với một phiên bản đề xuất.
type Decision string

// Quyết định phê duyệt.
const (
	DecisionApproved Decision = "approved"
	DecisionRejected Decision = "rejected"
)

// Point là tọa độ WGS84, decimal (không float).
type Point struct {
	Lat decimal.Decimal
	Lng decimal.Decimal
}

var (
	lat90  = decimal.NewFromInt(90)
	lng180 = decimal.NewFromInt(180)
)

// ParsePoint đọc tọa độ từ số JSON (json.Number) hoặc chuỗi, tối đa 7 chữ số thập phân.
func ParsePoint(lat, lng string) (Point, error) {
	la, err := decimal.NewFromString(strings.TrimSpace(lat))
	if err != nil || la.Abs().GreaterThan(lat90) || !la.Equal(la.Truncate(7)) {
		return Point{}, fieldErr("location.lat", "vĩ độ WGS84 trong [-90, 90], tối đa 7 chữ số thập phân")
	}
	ln, err := decimal.NewFromString(strings.TrimSpace(lng))
	if err != nil || ln.Abs().GreaterThan(lng180) || !ln.Equal(ln.Truncate(7)) {
		return Point{}, fieldErr("location.lng", "kinh độ WGS84 trong [-180, 180], tối đa 7 chữ số thập phân")
	}
	return Point{Lat: la, Lng: ln}, nil
}

// FacilitySpec là nội dung một phiên bản dữ liệu chủ cơ sở.
type FacilitySpec struct {
	Code             string
	Name             string
	Kind             Kind
	ParentFacilityID *uuid.UUID
	Location         Point
	// Boundary là GeoJSON (Polygon hoặc MultiPolygon, WGS84) — DB chuẩn hóa thành MultiPolygon.
	Boundary      json.RawMessage
	ProvinceCode  string
	Status        Status
	Capacity      mass.Kg
	EffectiveFrom time.Time
}

// Validate kiểm các quy tắc của phiên bản.
func (s FacilitySpec) Validate() error {
	switch {
	case !codeRe.MatchString(s.Code):
		return fieldErr("code", "mã cơ sở phải khớp ^[A-Z0-9][A-Z0-9_-]{1,31}$")
	case strings.TrimSpace(s.Name) == "" || len([]rune(s.Name)) > 200:
		return fieldErr("name", "bắt buộc, tối đa 200 ký tự")
	case s.Kind != KindHub && s.Kind != KindWarehouse && s.Kind != KindYard:
		return fieldErr("kind", "phải là hub, warehouse hoặc yard")
	case s.Kind == KindHub && s.ParentFacilityID != nil:
		return fieldErr("parent_facility_id", "hub không thuộc cơ sở khác")
	case !provinceRe.MatchString(s.ProvinceCode):
		return fieldErr("province_code", "mã tỉnh 2 chữ số")
	case s.Status != StatusActive && s.Status != StatusInactive:
		return fieldErr("status", "phải là active hoặc inactive")
	case s.Capacity.NonNegative() != nil:
		return fieldErr("capacity_kg", "không âm")
	case s.EffectiveFrom.IsZero():
		return fieldErr("effective_from", "bắt buộc")
	}
	if len(s.Boundary) > 0 && string(s.Boundary) != "null" {
		var g struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(s.Boundary, &g) != nil || (g.Type != "Polygon" && g.Type != "MultiPolygon") {
			return fieldErr("boundary", "GeoJSON Polygon hoặc MultiPolygon (WGS84)")
		}
	}
	return nil
}

// FacilityVersion là một dòng sổ phiên bản.
type FacilityVersion struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	FacilityID uuid.UUID
	Seq        int64 // = số phiên bản
	Spec       FacilitySpec
	// Supersedes/ChangeReason: có từ phiên bản 2 (mẫu bản ghi bù mục 8.1).
	Supersedes   *uuid.UUID
	ChangeReason *string
	ProposedBy   uuid.UUID
	EventTime    time.Time
	RecordedAt   time.Time
	IdemKey      string
	// Decision/DecidedBy: nil khi còn chờ duyệt.
	Decision  *Decision
	DecidedBy *uuid.UUID
	DecidedAt *time.Time
}

// Pending cho biết phiên bản còn chờ quyết định.
func (v FacilityVersion) Pending() bool { return v.Decision == nil }

// CheckChangeReason: từ phiên bản 2 bắt buộc lý do thay đổi ≥ 10 ký tự.
func CheckChangeReason(reason string) error {
	if len([]rune(strings.TrimSpace(reason))) < 10 {
		return fieldErr("reason", "lý do thay đổi bắt buộc, tối thiểu 10 ký tự")
	}
	return nil
}

// CheckDecisionReason: từ chối bắt buộc lý do 10..500 ký tự; phê duyệt thì tùy chọn.
func CheckDecisionReason(d Decision, reason string) error {
	n := len([]rune(strings.TrimSpace(reason)))
	if d == DecisionRejected && n < 10 {
		return fieldErr("reason", "từ chối bắt buộc lý do, tối thiểu 10 ký tự")
	}
	if n > 0 && (n < 10 || n > 500) {
		return fieldErr("reason", "lý do 10..500 ký tự")
	}
	return nil
}

// Facility là dữ liệu chủ hiện hành (phiên bản đã duyệt mới nhất).
type Facility struct {
	ID         uuid.UUID
	Spec       FacilitySpec
	VersionID  uuid.UUID
	VersionSeq int64
	ProposedBy uuid.UUID
	ApprovedBy uuid.UUID
	ApprovedAt time.Time
}
