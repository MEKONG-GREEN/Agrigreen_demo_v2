package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// OrgRuleVersion ghim vào mọi bản ghi sổ biến động tổ chức.
const OrgRuleVersion = "pf.party.org/v1"

// Độ dài tối thiểu của lý do.
const (
	MinDeactivateReason = 20 // HTX US-HTXSTATUS-01 AC-3
	MinMoveReason       = 10 // sáp nhập/di chuyển nhánh phải có căn cứ (KN US-ADM-07/08)
)

// OrgAction là loại biến động trong sổ pf_party.org_event.
type OrgAction string

// Các biến động.
const (
	ActionCreated     OrgAction = "created"
	ActionMoved       OrgAction = "moved"
	ActionRenamed     OrgAction = "renamed"
	ActionDeactivated OrgAction = "deactivated"
	ActionReactivated OrgAction = "reactivated"
)

// Lỗi kiểm dữ liệu.
var (
	ErrKind          = errors.New("kind không hợp lệ")
	ErrCode          = errors.New("code bắt buộc, 1–40 ký tự [A-Za-z0-9._-]")
	ErrName          = errors.New("name bắt buộc, tối đa 200 ký tự")
	ErrUnitRequired  = errors.New("admin_unit_code bắt buộc với loại tổ chức này")
	ErrUnitForbidden = errors.New("tổ chức cấp quốc gia không gắn đơn vị hành chính")
	ErrParent        = errors.New("tổ chức cấp trên không hợp lệ cho loại tổ chức này")
	ErrTaxCode       = errors.New("mã số thuế gồm 10 chữ số (hoặc 13 chữ số với đơn vị phụ thuộc)")
	ErrArea          = errors.New("diện tích (ha) phải là số không âm, tối đa 4 chữ số thập phân")
	ErrLocation      = errors.New("toạ độ phải có cả lat và lon trong phạm vi hợp lệ")
	ErrDeactivate    = errors.New("lý do vô hiệu hóa phải có ít nhất 20 ký tự")
	ErrMoveReason    = errors.New("lý do di chuyển bắt buộc, ít nhất 10 ký tự")
	ErrDate          = errors.New("ngày không hợp lệ hoặc ở tương lai")
)

var codeRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)

// KnownKind cho biết k là loại tổ chức đã khai báo.
func KnownKind(k port.OrgKind) bool {
	switch k {
	case port.OrgTTKNNational, port.OrgTTKNProvince, port.OrgExtensionTeam, port.OrgDCRD,
		port.OrgCooperative, port.OrgMKG, port.OrgPartner:
		return true
	}
	return false
}

// UnitLevel là cấp đơn vị hành chính mà loại tổ chức phải gắn: "" (không gắn),
// "province" hoặc "commune". Khớp masterdata/port.Level.
func UnitLevel(k port.OrgKind) string {
	switch k {
	case port.OrgTTKNProvince:
		return "province"
	case port.OrgExtensionTeam, port.OrgCooperative:
		return "commune"
	case port.OrgPartner:
		return "any" // bên thứ ba: tùy chọn, tỉnh hoặc xã
	}
	return ""
}

// AllowedParent cho biết tổ chức loại child được đặt dưới cha loại parent (nil = gốc).
// Cây khuyến nông 3 cấp: TW → Tỉnh → Xã/Cộng đồng (KN US-ADM-07); HTX đặt dưới
// tổ KN xã, TTKN tỉnh, hoặc đứng độc lập (danh bạ CGH).
func AllowedParent(child port.OrgKind, parent *port.OrgKind) bool {
	switch child {
	case port.OrgTTKNNational, port.OrgDCRD, port.OrgMKG:
		return parent == nil
	case port.OrgTTKNProvince:
		return parent == nil || *parent == port.OrgTTKNNational
	case port.OrgExtensionTeam:
		return parent != nil && *parent == port.OrgTTKNProvince
	case port.OrgCooperative:
		return parent == nil || *parent == port.OrgExtensionTeam || *parent == port.OrgTTKNProvince
	case port.OrgPartner:
		return parent == nil
	}
	return false
}

// ValidateOrgBasics kiểm các trường không phụ thuộc DB.
func ValidateOrgBasics(kind port.OrgKind, code, name, unit string, codeGenerated bool) error {
	if !KnownKind(kind) {
		return FieldError{"kind", ErrKind}
	}
	if !codeGenerated && !codeRe.MatchString(code) {
		return FieldError{"code", ErrCode}
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(name)); n == 0 || n > 200 {
		return FieldError{"name", ErrName}
	}
	switch UnitLevel(kind) {
	case "":
		if unit != "" {
			return FieldError{"admin_unit_code", ErrUnitForbidden}
		}
	case "province", "commune":
		if unit == "" {
			return FieldError{"admin_unit_code", ErrUnitRequired}
		}
	}
	return nil
}

// NormalizeTaxCode chuẩn hóa mã số thuế Việt Nam: 10 chữ số, hoặc 13 chữ số
// viết thành NNNNNNNNNN-NNN (đơn vị phụ thuộc).
func NormalizeTaxCode(raw string) (string, error) {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if r != '-' && r != ' ' && r != '.' {
			return "", ErrTaxCode
		}
	}
	d := b.String()
	switch len(d) {
	case 10:
		return d, nil
	case 13:
		return d[:10] + "-" + d[10:], nil
	}
	return "", ErrTaxCode
}

// ValidArea kiểm diện tích ha: không âm, ≤ 4 chữ số thập phân, < 10^10.
func ValidArea(a decimal.Decimal) bool {
	return !a.IsNegative() && a.Exponent() >= -4 && a.LessThan(decimal.New(1, 10))
}

// Point là toạ độ WGS84 (EPSG:4326).
type Point struct {
	Lat decimal.Decimal
	Lon decimal.Decimal
}

// Valid kiểm phạm vi toạ độ.
func (p Point) Valid() bool {
	return p.Lat.GreaterThanOrEqual(decimal.NewFromInt(-90)) && p.Lat.LessThanOrEqual(decimal.NewFromInt(90)) &&
		p.Lon.GreaterThanOrEqual(decimal.NewFromInt(-180)) && p.Lon.LessThanOrEqual(decimal.NewFromInt(180))
}

// CooperativeProfile là hồ sơ HTX/THT (CGH FN-03). Không có dữ liệu cá nhân —
// người đại diện là RepresentativeRef ở Org.
type CooperativeProfile struct {
	TaxCode       string // rỗng = chưa có
	AreaHa        *decimal.Decimal
	EstablishedOn *time.Time // ngày, 00:00 UTC
	Location      *Point     // nil → hiển thị tâm xã, nhãn "vị trí tạm" (CGH AC-03)
}

// Validate kiểm hồ sơ so với thời điểm now.
func (c CooperativeProfile) Validate(now time.Time) error {
	if c.AreaHa != nil && !ValidArea(*c.AreaHa) {
		return FieldError{"cooperative.area_ha", ErrArea}
	}
	if c.EstablishedOn != nil && c.EstablishedOn.After(now) {
		return FieldError{"cooperative.established_on", ErrDate}
	}
	if c.Location != nil && !c.Location.Valid() {
		return FieldError{"cooperative.location", ErrLocation}
	}
	return nil
}

// ValidateDeactivateReason kiểm lý do vô hiệu hóa (đếm ký tự, không đếm byte).
func ValidateDeactivateReason(reason string) error {
	if utf8.RuneCountInString(strings.TrimSpace(reason)) < MinDeactivateReason {
		return FieldError{"reason", ErrDeactivate}
	}
	return nil
}

// ValidateMoveReason kiểm lý do di chuyển nhánh.
func ValidateMoveReason(reason string) error {
	if utf8.RuneCountInString(strings.TrimSpace(reason)) < MinMoveReason {
		return FieldError{"reason", ErrMoveReason}
	}
	return nil
}

// OrgStreamKey là khóa chuỗi băm của một tổ chức.
func OrgStreamKey(ref uuid.UUID) string { return "org:" + ref.String() }

// OrgEvent là một dòng sổ biến động tổ chức (ảnh chụp trạng thái SAU sự việc).
type OrgEvent struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Org           port.Org
	Action        OrgAction
	PrevParentRef uuid.UUID
	PrevAdminUnit string
	Reason        string
	Details       map[string]any // hồ sơ HTX, không PII
	EventTime     time.Time
	ActorRef      uuid.UUID
	IdemKey       string
	Seq           int64
	RecordedAt    time.Time
}
