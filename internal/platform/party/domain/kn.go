package domain

// KN v7.7 gói P2: quy tắc phòng ban, khu vực (FN-AD-04.02, FN-AD-04.03). Thuần, không phụ thuộc DB.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// Phiên bản quy tắc ghim vào sổ.
const (
	DepartmentRuleVersion = "pf.party.department/v1"
	RegionRuleVersion     = "pf.party.region/v1"
	OrgRegionRuleVersion  = "pf.party.org_region/v1"
)

// Hành động của sổ phòng ban.
const (
	DeptCreated       = "created"
	DeptUpdated       = "updated"
	DeptStatusChanged = "status_changed"
	DeptRetired       = "retired"
)

// Hành động của sổ khu vực và gán khu vực.
const (
	RegionCreated     = "created"
	RegionUpdated     = "updated"
	RegionDeactivated = "deactivated"
	RegionReactivated = "reactivated"

	OrgRegionAssigned = "assigned"
	OrgRegionChanged  = "changed"
	OrgRegionCleared  = "cleared"
)

// MinKNReason là độ dài tối thiểu của lý do ngưng/xóa/đổi gán.
const MinKNReason = 10

var (
	deptCodeRe   = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
	regionCodeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$`)
)

// Lỗi kiểm dữ liệu.
var (
	ErrDeptCode     = errors.New("mã phòng ban 2–40 ký tự [a-z0-9_], bắt đầu bằng chữ thường")
	ErrDeptName     = errors.New("tên bắt buộc, tối đa 200 ký tự")
	ErrDeptDesc     = errors.New("mô tả tối đa 1000 ký tự")
	ErrSortOrder    = errors.New("thứ tự là số nguyên 0–100000")
	ErrDeptStatus   = errors.New("trạng thái phải là active (Đang áp dụng), not_open (Chưa mở) hoặc suspended (Ngưng hoạt động)")
	ErrMenuProfile  = errors.New("bộ menu không có trong mã: trong_trot hoặc generic")
	ErrKNReason     = fmt.Errorf("lý do bắt buộc, %d–500 ký tự", MinKNReason)
	ErrRegionCode   = errors.New("mã khu vực 1–40 ký tự [A-Za-z0-9_.-]")
	ErrRegionName   = errors.New("tên khu vực bắt buộc, tối đa 200 ký tự")
	ErrProvinceCode = errors.New("mã tỉnh bắt buộc")
)

// ValidStatus cho biết s là trạng thái phòng ban.
func ValidStatus(s string) bool {
	return s == port.DeptActive || s == port.DeptNotOpen || s == port.DeptSuspended
}

// StatusLabel là nhãn tiếng Việt của trạng thái phòng ban.
func StatusLabel(s string) string {
	switch s {
	case port.DeptActive:
		return "Đang áp dụng"
	case port.DeptNotOpen:
		return "Chưa mở"
	case port.DeptSuspended:
		return "Ngưng hoạt động"
	}
	return s
}

// ValidateReason kiểm lý do (MinKNReason–500 ký tự) của trường field.
func ValidateReason(field, r string) error {
	n := utf8.RuneCountInString(strings.TrimSpace(r))
	if n < MinKNReason || n > 500 {
		return FieldError{field, ErrKNReason}
	}
	return nil
}

// DeptSnapshot là trạng thái phòng ban ghi vào sổ (before/after).
type DeptSnapshot struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
	Status      string `json:"status"`
	MenuProfile string `json:"menu_profile"`
	Retired     bool   `json:"retired"`
}

// DeptSnapshotOf chụp một phòng ban.
func DeptSnapshotOf(d port.Department) DeptSnapshot {
	return DeptSnapshot{Code: d.Code, Name: d.Name, Description: d.Description, SortOrder: d.SortOrder, Status: d.Status,
		MenuProfile: d.MenuProfile, Retired: d.Retired}
}

// NormalizeDepartment kiểm và chuẩn hóa trường nhập của phòng ban (tạo/sửa).
func NormalizeDepartment(d port.Department) (port.Department, error) {
	d.Code = strings.TrimSpace(d.Code)
	d.Name = strings.TrimSpace(d.Name)
	d.Description = strings.TrimSpace(d.Description)
	d.MenuProfile = strings.TrimSpace(d.MenuProfile)
	if d.MenuProfile == "" {
		d.MenuProfile = port.MenuProfileGeneric
	}
	switch {
	case !deptCodeRe.MatchString(d.Code):
		return d, FieldError{"code", ErrDeptCode}
	case d.Name == "" || utf8.RuneCountInString(d.Name) > 200:
		return d, FieldError{"name", ErrDeptName}
	case utf8.RuneCountInString(d.Description) > 1000:
		return d, FieldError{"description", ErrDeptDesc}
	case d.SortOrder < 0 || d.SortOrder > 100000:
		return d, FieldError{"sort_order", ErrSortOrder}
	case !KnownMenuProfile(d.MenuProfile):
		return d, FieldError{"menu_profile", ErrMenuProfile}
	}
	if d.Status == "" {
		d.Status = port.DeptNotOpen
	}
	if !ValidStatus(d.Status) {
		return d, FieldError{"status", ErrDeptStatus}
	}
	return d, nil
}

// RegionSnapshot là trạng thái khu vực ghi vào sổ.
type RegionSnapshot struct {
	ProvinceCode       string `json:"province_code"`
	Code               string `json:"code"`
	Name               string `json:"name"`
	SortOrder          int    `json:"sort_order"`
	Active             bool   `json:"active"`
	DeactivationReason string `json:"deactivation_reason,omitempty"`
}

// NormalizeRegion kiểm khu vực.
func NormalizeRegion(r port.Region) (port.Region, error) {
	r.ProvinceCode = strings.TrimSpace(r.ProvinceCode)
	r.Code = strings.TrimSpace(r.Code)
	r.Name = strings.TrimSpace(r.Name)
	switch {
	case r.ProvinceCode == "":
		return r, FieldError{"province_code", ErrProvinceCode}
	case !regionCodeRe.MatchString(r.Code):
		return r, FieldError{"code", ErrRegionCode}
	case r.Name == "" || utf8.RuneCountInString(r.Name) > 200:
		return r, FieldError{"name", ErrRegionName}
	case r.SortOrder < 0 || r.SortOrder > 100000:
		return r, FieldError{"sort_order", ErrSortOrder}
	}
	return r, nil
}

// FieldChange là một dòng "giá trị cũ → mới" của Log (kn-v7.7-plan §1.5).
type FieldChange struct {
	Field string  `json:"field"`
	Old   *string `json:"old"`
	New   *string `json:"new"`
}

// Diff so hai ảnh chụp JSON (object phẳng) và trả các trường đổi, sắp theo tên. before nil (tạo
// mới) → mọi trường của after là mới.
func Diff(before, after []byte) []FieldChange {
	var b, a map[string]any
	decodeObj(before, &b)
	decodeObj(after, &a)
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	slices.Sort(names)
	out := []FieldChange{}
	for _, k := range names {
		ov, ok1 := b[k]
		nv, ok2 := a[k]
		o, n := scalar(ov, ok1), scalar(nv, ok2)
		if eq(o, n) {
			continue
		}
		out = append(out, FieldChange{Field: k, Old: o, New: n})
	}
	return out
}

// decodeObj đọc object JSON giữ số dạng chuỗi (không qua float).
func decodeObj(raw []byte, out *map[string]any) {
	if len(raw) == 0 {
		return
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	_ = d.Decode(out)
}

func scalar(v any, ok bool) *string {
	if !ok || v == nil {
		return nil
	}
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case bool:
		s = strconv.FormatBool(x)
	case json.Number:
		s = x.String()
	default:
		raw, _ := json.Marshal(x)
		s = string(raw)
	}
	return &s
}

func eq(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
