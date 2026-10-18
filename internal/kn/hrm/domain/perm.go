package domain

// W11.22 — PHÂN QUYỀN HỒ SƠ (BRD HCTH v3.0 FN-20, UR-ADM-09, UR-TECH-03): ma trận nhóm quyền × phạm vi (toàn Trung
// tâm / đơn vị mình / bản thân / không) theo NHÓM VAI (suy từ ngữ cảnh phiên KN) và theo NGƯỜI DÙNG; quyền theo người
// dùng ưu tiên hơn quyền theo nhóm. Mặc định theo FN-07 BR-04 và BR-03.

import (
	"encoding/json"
	"regexp"
	"sort"
)

// Perm là một nhóm quyền trên hồ sơ.
type Perm string

// Nhóm quyền.
const (
	PermView      Perm = "profile.view"   // xem danh sách + chi tiết hồ sơ (trừ thẻ Thống kê BC Sở)
	PermEdit      Perm = "profile.edit"   // thêm, sửa hồ sơ, quá trình, thông tin làm việc
	PermSensitive Perm = "sensitive.view" // xem/sửa thẻ Thống kê BC Sở (TK-01..06)
	PermExport    Perm = "export"         // xuất danh sách
	PermAudit     Perm = "audit.view"     // xem nhật ký xem/xuất của hồ sơ
)

// Perms theo thứ tự hiển thị. W11.24: hai nhóm quyền tài liệu (docs.go — FN-20 "quản lý tài liệu").
var Perms = []Perm{PermView, PermEdit, PermSensitive, PermExport, PermAudit, PermDocView, PermDocManage}

// Scope là phạm vi của một nhóm quyền.
type Scope string

// Phạm vi, từ hẹp tới rộng.
const (
	ScopeNone Scope = "none"
	ScopeSelf Scope = "self" // hồ sơ gắn với tài khoản của chính người gọi
	ScopeUnit Scope = "unit" // hồ sơ thuộc đơn vị nội bộ của người gọi + cán bộ xã có đơn vị quản lý đó (HS-37)
	ScopeAll  Scope = "all"  // toàn Trung tâm
)

// Scopes theo thứ tự.
var Scopes = []Scope{ScopeNone, ScopeSelf, ScopeUnit, ScopeAll}

// Nhóm vai của ma trận (suy ở app từ ngữ cảnh phiên KN — không lấy từ client).
const (
	GroupAdmin   = "admin"   // Admin (không chỉnh được — chặn gỡ quyền Admin cuối)
	GroupHCTH    = "hcth"    // Cấp Tỉnh ở phòng Hành chính – Tổng hợp
	GroupManager = "manager" // lãnh đạo các phòng (cấp quản lý ≥ phó trưởng phòng — KNContext.Manager)
	GroupTinh    = "tinh"    // Cấp Tỉnh khác
	GroupXa      = "xa"      // Cấp Xã
	GroupTW      = "tw"      // Cấp TW
)

// Groups theo thứ tự hiển thị.
var Groups = []string{GroupAdmin, GroupHCTH, GroupManager, GroupTinh, GroupXa, GroupTW}

// Matrix là quyền của một nhóm hoặc một người: nhóm quyền → phạm vi. Thiếu khóa = không đặt (người dùng: lấy theo nhóm).
type Matrix map[Perm]Scope

// DefaultMatrix là ma trận mặc định (FN-07 BR-04: HCTH, Admin xem đầy đủ; lãnh đạo phòng xem đơn vị mình; Cấp TW, Cấp
// Xã không truy cập hồ sơ — chỉ Contact. BR-03: thẻ Thống kê BC Sở chỉ HCTH, Admin).
func DefaultMatrix(group string) Matrix {
	m := Matrix{}
	for _, p := range Perms {
		m[p] = ScopeNone
	}
	switch group {
	case GroupAdmin, GroupHCTH:
		for _, p := range Perms {
			m[p] = ScopeAll
		}
	case GroupManager:
		m[PermView], m[PermExport] = ScopeUnit, ScopeUnit
		m[PermDocView] = ScopeUnit // W11.24 FN-14: lãnh đạo phòng xem tài liệu phòng mình
	}
	return m
}

// Grantee là đối tượng của một dòng ma trận.
const (
	GranteeRole = "role"
	GranteeUser = "user"
)

var reUserRef = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ParseMatrix kiểm một dòng ma trận: nhóm vai có trong danh mục (không phải Admin), người dùng là UUID; nhóm quyền,
// phạm vi hợp lệ; thẻ Thống kê BC Sở chỉ cấp được cho nhóm HCTH hoặc từng người (BR-03), phạm vi none/all.
func ParseMatrix(kind, grantee string, raw []byte) (Matrix, []byte, error) {
	switch kind {
	case GranteeRole:
		if !contains(Groups, grantee) {
			return nil, nil, invalid("nhóm vai %q", grantee)
		}
		if grantee == GroupAdmin {
			return nil, nil, invalid("quyền của Admin cố định (chặn gỡ quyền Admin cuối)")
		}
	case GranteeUser:
		if !reUserRef.MatchString(grantee) {
			return nil, nil, invalid("người dùng phải là UUID chữ thường")
		}
	default:
		return nil, nil, invalid("grantee_kind %q", kind)
	}
	var m map[string]string
	if err := decodeStrict(raw, &m); err != nil {
		return nil, nil, invalid("ma trận: %v", err)
	}
	out := Matrix{}
	for k, v := range m {
		p, s := Perm(k), Scope(v)
		if !containsPerm(Perms, p) {
			return nil, nil, invalid("nhóm quyền %q", k)
		}
		if !containsScope(Scopes, s) {
			return nil, nil, invalid("phạm vi %q", v)
		}
		if p == PermSensitive {
			if s != ScopeNone && s != ScopeAll {
				return nil, nil, invalid("thẻ Thống kê BC Sở: phạm vi none hoặc all")
			}
			if s == ScopeAll && kind == GranteeRole && grantee != GroupHCTH {
				return nil, nil, invalid("thẻ Thống kê BC Sở chỉ cấp cho nhóm HCTH hoặc từng người (BR-03)")
			}
		}
		out[p] = s
	}
	canon, err := json.Marshal(out)
	return out, canon, err
}

// EffectiveMatrix gộp: mặc định của nhóm ← dòng của nhóm ← dòng của người dùng (người dùng ưu tiên). Admin luôn mặc định.
func EffectiveMatrix(group string, role, user Matrix) Matrix {
	m := DefaultMatrix(group)
	if group == GroupAdmin {
		return m
	}
	for _, src := range []Matrix{role, user} {
		for p, s := range src {
			m[p] = s
		}
	}
	return m
}

// Allows cho biết phạm vi s cho phép hồ sơ có quan hệ (self, unit) với người gọi.
func (s Scope) Allows(self, unit bool) bool {
	switch s {
	case ScopeAll:
		return true
	case ScopeUnit:
		return unit || self
	case ScopeSelf:
		return self
	}
	return false
}

// SortedPerms trả khóa của m theo thứ tự Perms.
func (m Matrix) SortedPerms() []Perm {
	out := make([]Perm, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return permIndex(out[i]) < permIndex(out[j]) })
	return out
}

func permIndex(p Perm) int {
	for i, x := range Perms {
		if x == p {
			return i
		}
	}
	return len(Perms)
}

func containsPerm(xs []Perm, p Perm) bool { return permIndex(p) < len(xs) && xs[permIndex(p)] == p }

func containsScope(xs []Scope, s Scope) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
