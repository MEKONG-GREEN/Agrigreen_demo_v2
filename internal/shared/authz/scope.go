package authz

import "slices"

// Scope là phạm vi dữ liệu của người gọi theo đơn vị hành chính (ADR-0009).
//
// RLS chỉ cách ly tenant (R-33). Phạm vi theo cây TW → Tỉnh → Xã là phân quyền
// tầng ứng dụng: module lọc truy vấn bằng Units và kiểm ghi bằng Allows.
// All = true: toàn quốc (TTKN TW, Cục KTHT, quản trị nền tảng).
type Scope struct {
	All bool
	// Units là mã đơn vị hành chính (tỉnh và xã, mã hiện hành) người gọi được thấy.
	Units []string
}

// Allows cho biết người gọi có được thao tác dữ liệu thuộc đơn vị unitCode.
func (s Scope) Allows(unitCode string) bool {
	return s.All || slices.Contains(s.Units, unitCode)
}

// Empty cho biết phạm vi rỗng (không thấy gì) — mặc định chặn.
func (s Scope) Empty() bool { return !s.All && len(s.Units) == 0 }
