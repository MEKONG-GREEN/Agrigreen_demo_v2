// Package domain: quy tắc hồ sơ KN riêng cho năm sản xuất máy (TT-10) và cây trồng thửa (TT-11) — gói
// BF-SRV-KN-ATTRS (#119). Hồ sơ thuộc KN (schema pf_extension), không ghi dữ liệu HTX; khai báo số lượng máy và
// hình thửa vẫn ở module nền (mechanization, cooperative/plot).
package domain

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RuleVersion ghim vào mọi dòng sổ kn_machine_year / kn_plot_crop.
const RuleVersion = "extension.kn_attrs.v1"

// MinYearMade là năm sản xuất nhỏ nhất nhận được (tạm, chờ khách; khớp CHECK của migration).
const MinYearMade = 1900

// MaxMachineLines là số loại máy tối đa trong hồ sơ năm sản xuất của một HTX.
const MaxMachineLines = 200

// Sự kiện trên sổ.
const (
	EventSet     = "set"
	EventRemoved = "removed"
)

// VN là giờ Việt Nam (UTC+7, không đổi giờ).
var VN = time.FixedZone("ICT", 7*3600)

// CurrentYear là năm hiện tại theo giờ Việt Nam.
func CurrentYear(now time.Time) int { return now.In(VN).Year() }

// Lỗi kiểm năm sản xuất (app ánh xạ sang problem theo trường).
var (
	ErrYearRequired = errors.New("bắt buộc")
	ErrYearInteger  = errors.New("năm sản xuất phải là số nguyên")
	ErrYearRange    = errors.New("năm sản xuất phải từ 1900 đến năm hiện tại")
)

// ParseYear đọc năm sản xuất (số JSON): bắt buộc, nguyên, MinYearMade ≤ năm ≤ năm hiện tại (giờ VN).
func ParseYear(raw string, now time.Time) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, ErrYearRequired
	}
	y, err := strconv.Atoi(raw)
	if err != nil {
		return 0, ErrYearInteger
	}
	if y < MinYearMade || y > CurrentYear(now) {
		return 0, ErrYearRange
	}
	return y, nil
}

var codeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,59}$`)

// ValidCode: mã danh mục (loại máy, cây trồng) 1–60 ký tự chữ/số/_.-.
func ValidCode(s string) bool { return codeRe.MatchString(s) }

// MachineLine là năm sản xuất của một loại máy trong hồ sơ.
type MachineLine struct {
	MachineType string
	TypeName    string
	YearMade    int
}

// MachineChange là một dòng sổ sẽ ghi: set (năm mới/đổi) hoặc removed (loại máy bỏ khỏi hồ sơ).
type MachineChange struct {
	Event       string
	MachineType string
	TypeName    string
	YearMade    *int
}

// PlanMachines so hồ sơ hiện hành với hồ sơ thay thế; không đổi gì → rỗng (không ghi sổ). Thứ tự theo loại máy.
func PlanMachines(cur map[string]MachineLine, next []MachineLine) []MachineChange {
	out := []MachineChange{}
	seen := map[string]bool{}
	for _, l := range next {
		seen[l.MachineType] = true
		if c, ok := cur[l.MachineType]; ok && c.YearMade == l.YearMade {
			continue
		}
		y := l.YearMade
		out = append(out, MachineChange{Event: EventSet, MachineType: l.MachineType, TypeName: l.TypeName, YearMade: &y})
	}
	for t, c := range cur {
		if !seen[t] {
			out = append(out, MachineChange{Event: EventRemoved, MachineType: t, TypeName: c.TypeName})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MachineType < out[j].MachineType })
	return out
}
