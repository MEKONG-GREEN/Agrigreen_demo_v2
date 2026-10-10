package domain

// W11.07 (Q-068): đơn vị nội bộ của TTKN tỉnh (phòng/trạm/trại/BGĐ/văn phòng/tăng cường). Hàm thuần.

import (
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// InternalUnitRuleVersion ghi vào sổ đơn vị nội bộ (danh mục loại port.InternalUnitKinds).
const InternalUnitRuleVersion = "accounts.internal-unit/1"

// Sự kiện sổ đơn vị nội bộ.
const (
	UnitCreated     = "created"
	UnitUpdated     = "updated"
	UnitDeactivated = "deactivated"
	UnitReactivated = "reactivated"
)

// Thông điệp kiểm dữ liệu đơn vị nội bộ.
const (
	MsgUnitCode     = "mã 2–40 ký tự [a-z0-9_], bắt đầu bằng chữ thường"
	MsgUnitKind     = "loại đơn vị nội bộ không có trong danh mục (ban_giam_doc, van_phong, phong, tram, trai, tang_cuong)"
	MsgUnitName     = "tên bắt buộc, tối đa 200 ký tự"
	MsgUnitSort     = "thứ tự là số nguyên 0–100000"
	MsgUnitDeptKind = "chỉ đơn vị loại phòng mới gắn phòng ban"
)

// InternalUnitSnapshot là trạng thái đơn vị nội bộ ghi vào sổ.
type InternalUnitSnapshot struct {
	Code           string `json:"code"`
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	DepartmentCode string `json:"department_code"`
	SortOrder      int    `json:"sort_order"`
	Active         bool   `json:"active"`
}

// InternalUnitSnapshotOf chụp một đơn vị nội bộ.
func InternalUnitSnapshotOf(u port.InternalUnit) InternalUnitSnapshot {
	return InternalUnitSnapshot{Code: u.Code, Kind: u.Kind, Name: u.Name, DepartmentCode: u.DepartmentCode, SortOrder: u.SortOrder,
		Active: u.Active}
}

// NormalizeInternalUnit kiểm và chuẩn hóa đơn vị nội bộ (tạo/sửa): mã như mã phòng ban, loại trong danh mục,
// chỉ loại "phong" mới gắn phòng ban P2.
func NormalizeInternalUnit(u port.InternalUnit) (port.InternalUnit, error) {
	u.Code, u.Kind, u.Name = strings.TrimSpace(u.Code), strings.TrimSpace(u.Kind), strings.TrimSpace(u.Name)
	u.DepartmentCode = strings.TrimSpace(u.DepartmentCode)
	switch {
	case !unitCodeRe.MatchString(u.Code):
		return u, FieldError{Field: "code", Msg: MsgUnitCode}
	case port.CatalogLabel(port.InternalUnitKinds, u.Kind) == "":
		return u, FieldError{Field: "kind", Msg: MsgUnitKind}
	case u.Name == "" || utf8.RuneCountInString(u.Name) > 200:
		return u, FieldError{Field: "name", Msg: MsgUnitName}
	case u.SortOrder < 0 || u.SortOrder > 100000:
		return u, FieldError{Field: "sort_order", Msg: MsgUnitSort}
	case u.DepartmentCode != "" && u.Kind != port.UnitKindPhong:
		return u, FieldError{Field: "department_code", Msg: MsgUnitDeptKind}
	case u.DepartmentCode != "" && !unitCodeRe.MatchString(u.DepartmentCode):
		return u, FieldError{Field: "department_code", Msg: MsgUnitCode}
	}
	return u, nil
}
