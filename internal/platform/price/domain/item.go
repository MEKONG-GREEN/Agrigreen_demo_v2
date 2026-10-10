package domain

// Mặt hàng theo thời kỳ (W11.73, pf 000141; BRD Báo giá v1.3 FN-AD-04.03): quy tắc thuần — kiểm dữ liệu, phép chiếu
// "phiên bản có hiệu lực tại ngày", danh sách mặt hàng của một xã theo ngày (Cơ bản ∪ Địa phương đã gán, lọc theo bộ
// theo vụ). Không I/O, không đồng hồ.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Loại mặt hàng (BR-03).
const (
	ItemBasic = "basic" // Cơ bản — áp dụng mọi xã đang hoạt động
	ItemLocal = "local" // Địa phương — gán theo xã
)

// Trạng thái phiên bản sổ có hiệu lực.
const (
	StatusActive  = "active"
	StatusRetired = "retired"
)

// Trạng thái mặt hàng tại một ngày (cột "Trạng thái" của màn List: Hoạt động / Chờ hiệu lực / Ngưng).
const (
	ItemStateActive  = "active"
	ItemStatePending = "pending"
	ItemStateRetired = "retired"
)

// Trạng thái đề xuất.
const (
	ProposalPending  = "pending"
	ProposalApproved = "approved"
	ProposalRejected = "rejected"
)

var (
	reItemCode  = regexp.MustCompile(`^MH[0-9]{5}$`)
	rePointCode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	reSetCode   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
)

// ItemCode trả mã mặt hàng thứ n (BR-01: hệ thống sinh, không tái sử dụng).
func ItemCode(n int) string { return fmt.Sprintf("MH%05d", n) }

// ValidItemCode: mã mặt hàng đúng dạng.
func ValidItemCode(s string) bool { return reItemCode.MatchString(s) }

// ValidPointCode: mã xã báo giá đúng dạng (mặc định = mã ĐVHC xã; liên kết lỏng tới xã báo giá của W11.72).
func ValidPointCode(s string) bool { return rePointCode.MatchString(s) }

// ValidSetCode: mã bộ mặt hàng đúng dạng.
func ValidSetCode(s string) bool { return reSetCode.MatchString(s) }

// ValidGroupCode: mã nhóm hàng đúng dạng (CHECK của pf 000138).
func ValidGroupCode(s string) bool { return reGroupCode.MatchString(s) }

// Item là nội dung một phiên bản mặt hàng.
type Item struct {
	Code         string
	Name         string // tên loại (vd "Xoài")
	Variety      string // giống / phẩm cấp (vd "cát Hòa Lộc loại 1") — bắt buộc (BR-07, AC-06)
	GroupCode    string
	UnitCode     string
	Spec         string // quy cách (rỗng = không có)
	SalePlace    string // nơi bán
	Kind         string // basic | local
	SortOrder    int
	Status       string // active | retired
	ValidFrom    time.Time
	ReplacesCode string
	Note         string
}

// ValidateItem kiểm nội dung mặt hàng (cùng ràng buộc CHECK của pf 000141; mã kiểm riêng vì do hệ thống sinh).
func ValidateItem(it Item) error {
	if err := runeLen("tên mặt hàng", it.Name, 1, 100); err != nil {
		return err
	}
	if strings.TrimSpace(it.Variety) == "" {
		return fmt.Errorf("%w: nhập giống hoặc phẩm cấp (mỗi mặt hàng = loại + giống/phẩm cấp + đơn vị tính)", ErrInvalid)
	}
	if err := runeLen("giống/phẩm cấp", it.Variety, 1, 100); err != nil {
		return err
	}
	if !reGroupCode.MatchString(it.GroupCode) {
		return fmt.Errorf("%w: nhóm hàng %q", ErrInvalid, it.GroupCode)
	}
	if !reUnitCode.MatchString(it.UnitCode) {
		return fmt.Errorf("%w: đơn vị tính %q", ErrInvalid, it.UnitCode)
	}
	if it.Spec != "" {
		if err := runeLen("quy cách", it.Spec, 1, 100); err != nil {
			return err
		}
	}
	if it.SalePlace != "" {
		if err := runeLen("nơi bán", it.SalePlace, 1, 200); err != nil {
			return err
		}
	}
	if it.Kind != ItemBasic && it.Kind != ItemLocal {
		return fmt.Errorf("%w: loại mặt hàng %q (basic | local)", ErrInvalid, it.Kind)
	}
	if it.SortOrder < 0 || it.SortOrder > 1000000 {
		return fmt.Errorf("%w: thứ tự %d", ErrInvalid, it.SortOrder)
	}
	if it.ReplacesCode != "" && (!reItemCode.MatchString(it.ReplacesCode) || it.ReplacesCode == it.Code) {
		return fmt.Errorf("%w: \"thay thế cho\" %q", ErrInvalid, it.ReplacesCode)
	}
	if it.ValidFrom.IsZero() {
		return fmt.Errorf("%w: thiếu ngày hiệu lực", ErrInvalid)
	}
	return nil
}

func runeLen(field, s string, lo, hi int) error {
	if n := utf8.RuneCountInString(s); n < lo || n > hi || strings.TrimSpace(s) != s {
		return fmt.Errorf("%w: %s dài %d–%d ký tự, không có khoảng trắng đầu/cuối", ErrInvalid, field, lo, hi)
	}
	return nil
}

// ValidateText kiểm một trường văn bản tự do (lý do, ghi chú, tên bộ, vụ).
func ValidateText(field, s string, lo, hi int) error { return runeLen(field, s, lo, hi) }

// IdentityKey là khóa trùng của mặt hàng trong một nhóm: tên + giống/phẩm cấp + quy cách, không phân biệt hoa thường và
// khoảng trắng thừa (luồng ngoại lệ "Trùng tên và quy cách trong cùng nhóm → chặn").
func IdentityKey(it Item) string {
	norm := func(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
	return it.GroupCode + "|" + norm(it.Name) + "|" + norm(it.Variety) + "|" + norm(it.Spec)
}

// Ver là một phiên bản của một chuỗi có hiệu lực theo ngày.
type Ver[T any] struct {
	Key       string
	Seq       int64
	ValidFrom time.Time
	Status    string
	V         T
}

// newer: phiên bản a đứng sau b theo (valid_from, seq).
func newer[T any](a, b Ver[T]) bool {
	if !a.ValidFrom.Equal(b.ValidFrom) {
		return a.ValidFrom.After(b.ValidFrom)
	}
	return a.Seq > b.Seq
}

// EffectiveAt trả, cho mỗi khóa, phiên bản có hiệu lực tại ngày at (valid_from lớn nhất ≤ at, cùng ngày seq lớn nhất)
// — kể cả phiên bản 'retired' (bên gọi lọc).
func EffectiveAt[T any](vs []Ver[T], at time.Time) map[string]Ver[T] {
	out := map[string]Ver[T]{}
	for _, v := range vs {
		if v.ValidFrom.After(at) {
			continue
		}
		if cur, ok := out[v.Key]; !ok || newer(v, cur) {
			out[v.Key] = v
		}
	}
	return out
}

// Upcoming trả, cho mỗi khóa, phiên bản đầu tiên có valid_from > at (cùng ngày lấy seq lớn nhất).
func Upcoming[T any](vs []Ver[T], at time.Time) map[string]Ver[T] {
	out := map[string]Ver[T]{}
	for _, v := range vs {
		if !v.ValidFrom.After(at) {
			continue
		}
		cur, ok := out[v.Key]
		if !ok || v.ValidFrom.Before(cur.ValidFrom) || (v.ValidFrom.Equal(cur.ValidFrom) && v.Seq > cur.Seq) {
			out[v.Key] = v
		}
	}
	return out
}

// ItemState là mặt hàng nhìn từ một ngày: phiên bản hiển thị (có hiệu lực, hoặc phiên bản sắp hiệu lực nếu chưa có),
// trạng thái và ngày hiệu lực đến (ngày trước phiên bản 'retired' kế tiếp; zero = không thời hạn).
type ItemState struct {
	Item
	Version int64
	State   string // active | pending | retired
	ValidTo time.Time
	// Since là ngày đầu tiên mặt hàng có hiệu lực (valid_from nhỏ nhất của phiên bản active).
	Since time.Time
}

// ItemStates chiếu sổ mặt hàng tại ngày at.
func ItemStates(vs []Ver[Item], at time.Time) map[string]ItemState {
	eff := EffectiveAt(vs, at)
	up := Upcoming(vs, at)
	since := map[string]time.Time{}
	// Ngày ngưng kế tiếp sau at, chỉ tính khi không có phiên bản active nào xen sau nó trước đó (đủ cho dùng thực tế:
	// ngưng là thao tác cuối của một mặt hàng).
	retireAfter := map[string]time.Time{}
	for _, v := range vs {
		if v.Status == StatusActive {
			if s, ok := since[v.Key]; !ok || v.ValidFrom.Before(s) {
				since[v.Key] = v.ValidFrom
			}
		}
		if v.Status == StatusRetired && v.ValidFrom.After(at) {
			if r, ok := retireAfter[v.Key]; !ok || v.ValidFrom.Before(r) {
				retireAfter[v.Key] = v.ValidFrom
			}
		}
	}
	out := map[string]ItemState{}
	keys := map[string]bool{}
	for _, v := range vs {
		keys[v.Key] = true
	}
	for k := range keys {
		var st ItemState
		if e, ok := eff[k]; ok {
			st = ItemState{Item: e.V, Version: e.Seq, State: ItemStateActive}
			if e.Status == StatusRetired {
				st.State = ItemStateRetired
				st.ValidTo = e.ValidFrom.AddDate(0, 0, -1)
			}
		} else if u, ok := up[k]; ok {
			st = ItemState{Item: u.V, Version: u.Seq, State: ItemStatePending}
		} else {
			continue
		}
		if st.State != ItemStateRetired {
			if r, ok := retireAfter[k]; ok {
				st.ValidTo = r.AddDate(0, 0, -1)
			}
		}
		st.Since = since[k]
		out[k] = st
	}
	return out
}

// Assignment là một phiên bản gán mặt hàng Địa phương cho xã.
type Assignment struct {
	PointCode string
	ItemCode  string
}

// AssignKey là khóa chuỗi gán.
func AssignKey(point, item string) string { return point + ":" + item }

// ItemSet là nội dung một phiên bản bộ mặt hàng theo vụ.
type ItemSet struct {
	Code      string
	Name      string
	Season    string
	ItemCodes []string
	Enabled   bool
}

// Gate trả, từ các bộ đang hiệu lực (status active) tại ngày, tập mặt hàng bị chặn: mặt hàng thuộc ít nhất một bộ mà
// không bộ nào chứa nó đang bật (BR-08 — bật/tắt cả bộ theo ngày hiệu lực).
func Gate(sets map[string]Ver[ItemSet]) map[string]bool {
	member, on := map[string]bool{}, map[string]bool{}
	for _, s := range sets {
		if s.Status != StatusActive {
			continue
		}
		for _, c := range s.V.ItemCodes {
			member[c] = true
			if s.V.Enabled {
				on[c] = true
			}
		}
	}
	out := map[string]bool{}
	for c := range member {
		if !on[c] {
			out[c] = true
		}
	}
	return out
}

// PointItems trả mã mặt hàng xã point báo giá tại ngày (BR-03, BR-08): mặt hàng có hiệu lực, Cơ bản hoặc Địa phương đã
// gán cho xã, không bị bộ theo vụ chặn. Thứ tự: thứ tự nhóm (groupOrder), thứ tự mặt hàng, mã.
func PointItems(items map[string]ItemState, assigns map[string]Ver[Assignment], gate map[string]bool, point string,
	groupOrder map[string]int) []ItemState {
	var out []ItemState
	for code, it := range items {
		if it.State != ItemStateActive || gate[code] {
			continue
		}
		if it.Kind == ItemLocal {
			a, ok := assigns[AssignKey(point, code)]
			if !ok || a.Status != StatusActive {
				continue
			}
		}
		out = append(out, it)
	}
	SortItems(out, groupOrder)
	return out
}

// SortItems sắp mặt hàng theo thứ tự nhóm, thứ tự mặt hàng, mã (AC-04).
func SortItems(xs []ItemState, groupOrder map[string]int) {
	sort.Slice(xs, func(i, j int) bool {
		gi, gj := groupRank(groupOrder, xs[i].GroupCode), groupRank(groupOrder, xs[j].GroupCode)
		if gi != gj {
			return gi < gj
		}
		if xs[i].GroupCode != xs[j].GroupCode {
			return xs[i].GroupCode < xs[j].GroupCode
		}
		if xs[i].SortOrder != xs[j].SortOrder {
			return xs[i].SortOrder < xs[j].SortOrder
		}
		return xs[i].Code < xs[j].Code
	})
}

func groupRank(order map[string]int, code string) int {
	if r, ok := order[code]; ok {
		return r
	}
	return 1 << 30
}

// Proposal là một phiên bản đề xuất mặt hàng mới của Cấp Xã.
type Proposal struct {
	PointCode    string
	Name         string
	Variety      string
	GroupCode    string
	UnitCode     string
	Spec         string
	Reason       string
	Status       string
	ItemCode     string
	DecisionNote string
}
