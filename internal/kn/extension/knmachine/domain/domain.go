// Package domain: quy tắc khai báo SỐ LƯỢNG máy theo loại của HTX (Trồng trọt FN-XA-03.04) ở kho KN —
// KN-STANDALONE-5-TT-DATA-MAY (#306). Giữ hành vi trước khi chuyển kho: số lượng là số nguyên lớn hơn 0, tối đa
// 1.000.000; mỗi loại máy một dòng; ghi chú tối đa 500 ký tự.
package domain

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// RuleVersion là phiên bản quy tắc của sổ kn_machine_count_event.
const RuleVersion = "kn.machine_count/v1"

// Giới hạn khai báo.
const (
	MaxLines    = 100
	MaxQuantity = 1_000_000
	MaxNote     = 500
)

// Sự việc của sổ kn_machine_count_event.
const (
	Migrated = "migrated" // dòng chép một lần từ module CGH
	Set      = "set"
	Removed  = "removed"
)

// Lỗi kiểm số lượng (handler/app ánh xạ sang problem).
var (
	ErrQuantityNotInteger = errors.New("số lượng phải là số nguyên lớn hơn 0")
	ErrQuantityRange      = errors.New("số lượng phải trong khoảng 1–1.000.000")
)

// quantityRe: chỉ chữ số thập phân thường, tối đa 7 chữ số — kiểm TRƯỚC khi parse (không số mũ, dấu, phần lẻ).
var (
	quantityRe = regexp.MustCompile(`^[0-9]{1,7}$`)
	digitsRe   = regexp.MustCompile(`^[0-9]+$`)
)

// ParseQuantity đọc số lượng khai báo: số nguyên lớn hơn 0, không quá MaxQuantity. Cú pháp kiểm trước khi parse nên
// `1e100000`, `1e-100000`, chuỗi rất dài bị chặn mà không tính toán gì.
func ParseQuantity(s string) (int, error) {
	s = strings.TrimSpace(s)
	if !quantityRe.MatchString(s) {
		if digitsRe.MatchString(s) { // toàn chữ số nhưng quá 7 chữ số
			return 0, ErrQuantityRange
		}
		return 0, ErrQuantityNotInteger
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, ErrQuantityNotInteger
	}
	if n > MaxQuantity {
		return 0, ErrQuantityRange
	}
	return n, nil
}

// Line là một dòng khai báo: loại máy × số lượng × ghi chú.
type Line struct {
	MachineType string
	Quantity    int
	Note        string
}

// Change là một sự việc cần ghi khi thay toàn bộ khai báo của HTX.
type Change struct {
	MachineType  string
	Action       string // Set | Removed
	Quantity     int
	PrevQuantity *int
	Note         string
	PrevNote     *string
}

// DuplicateType trả loại máy đầu tiên bị lặp trong lines ("" nếu không lặp).
func DuplicateType(lines []Line) string {
	seen := map[string]bool{}
	for _, l := range lines {
		if seen[l.MachineType] {
			return l.MachineType
		}
		seen[l.MachineType] = true
	}
	return ""
}

// ValidNote kiểm độ dài ghi chú.
func ValidNote(s string) bool { return utf8.RuneCountInString(s) <= MaxNote }

// Plan so khai báo mới (desired) với khai báo hiện hành (current, chỉ dòng đang có) và trả các sự việc: loại mới hoặc
// đổi số lượng/ghi chú → Set; loại hiện có không còn trong desired → Removed. Sắp theo mã loại.
func Plan(current map[string]Line, desired []Line) []Change {
	out := []Change{}
	want := map[string]bool{}
	for _, d := range desired {
		want[d.MachineType] = true
		cur, ok := current[d.MachineType]
		if ok && cur.Quantity == d.Quantity && cur.Note == d.Note {
			continue
		}
		ch := Change{MachineType: d.MachineType, Action: Set, Quantity: d.Quantity, Note: d.Note}
		if ok {
			q, n := cur.Quantity, cur.Note
			ch.PrevQuantity, ch.PrevNote = &q, &n
		}
		out = append(out, ch)
	}
	for t, cur := range current {
		if want[t] {
			continue
		}
		q, n := cur.Quantity, cur.Note
		out = append(out, Change{MachineType: t, Action: Removed, Quantity: 0, PrevQuantity: &q, PrevNote: &n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MachineType < out[j].MachineType })
	return out
}
