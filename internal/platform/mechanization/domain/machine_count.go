package domain

// KN v7.7 FN-XA-03.04 — khai báo SỐ LƯỢNG máy theo loại của HTX (mô hình v7.7, khác hồ sơ máy từng chiếc).

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// MachineCountRuleVersion là phiên bản quy tắc của sổ khai báo số lượng máy.
const MachineCountRuleVersion = "kn.machine_count/v1"

// Giới hạn khai báo.
const (
	MaxMachineCountLines = 100
	MaxMachineQuantity   = 1_000_000
	MaxMachineNote       = 500
)

// Hành động của sổ coop_machine_count_event.
const (
	CountSet     = "set"
	CountRemoved = "removed"
)

// Lỗi kiểm số lượng (handler/app ánh xạ sang problem).
var (
	ErrQuantityNotInteger = errors.New("số lượng phải là số nguyên ≥ 0")
	ErrQuantityRange      = errors.New("số lượng phải trong khoảng 0–1.000.000")
)

// ParseQuantity đọc số lượng (chuỗi số JSON) — âm, không nguyên hoặc quá lớn → lỗi (BRD: "số lượng âm/không
// nguyên → chặn").
func ParseQuantity(s string) (int, error) {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil || !d.IsInteger() {
		return 0, ErrQuantityNotInteger
	}
	if d.IsNegative() {
		return 0, ErrQuantityNotInteger
	}
	if d.GreaterThan(decimal.NewFromInt(MaxMachineQuantity)) {
		return 0, ErrQuantityRange
	}
	return int(d.IntPart()), nil
}

// CountLine là một dòng khai báo: loại máy × số lượng × ghi chú.
type CountLine struct {
	MachineType string
	Quantity    int
	Note        string
}

// CountChange là một sự kiện cần ghi khi thay toàn bộ khai báo của HTX.
type CountChange struct {
	MachineType  string
	Action       string // set | removed
	Quantity     int
	PrevQuantity *int
	Note         string
	PrevNote     *string
}

// DuplicateType trả loại máy đầu tiên bị lặp trong lines ("" nếu không lặp).
func DuplicateType(lines []CountLine) string {
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
func ValidNote(s string) bool { return utf8.RuneCountInString(s) <= MaxMachineNote }

// PlanCounts so khai báo mới (desired) với khai báo hiện hành (current, chỉ dòng đang có) và trả các sự kiện:
// loại mới hoặc đổi số lượng/ghi chú → set; loại hiện có không còn trong desired → removed. Sắp theo mã loại.
func PlanCounts(current map[string]CountLine, desired []CountLine) []CountChange {
	out := []CountChange{}
	want := map[string]bool{}
	for _, d := range desired {
		want[d.MachineType] = true
		cur, ok := current[d.MachineType]
		if ok && cur.Quantity == d.Quantity && cur.Note == d.Note {
			continue
		}
		ch := CountChange{MachineType: d.MachineType, Action: CountSet, Quantity: d.Quantity, Note: d.Note}
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
		out = append(out, CountChange{MachineType: t, Action: CountRemoved, Quantity: 0, PrevQuantity: &q, PrevNote: &n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MachineType < out[j].MachineType })
	return out
}
