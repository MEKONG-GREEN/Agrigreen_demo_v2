package domain

import (
	"sort"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
)

// Giá trị khóa phân bổ và quy tắc ẩm.
const (
	AllocByMass  = "mass"  // theo khối lượng lô trên chặng / trong giao nhận (mặc định, chờ khách Q-2)
	AllocByEqual = "equal" // chia đều theo lô (phương án thay thế để chứng minh cơ chế tham số)
	MoistureNone = "none"  // C-4: chưa có công thức quy đổi / khấu trừ ẩm — không quy đổi
)

// ParamDef là định nghĩa một tham số có phiên bản.
type ParamDef struct {
	Key     string
	Default string // mặc định #0 — TẠM, CHỜ KHÁCH
	Source  string // nguồn mặc định (prototype)
	kind    string // pct | rate | days | alloc | moisture
}

// Params là bảng tham số của costing (docs/dev/g4-conventions.md §1.2).
var Params = []ParamDef{
	{Key: port.ParamAllocationKey, Default: AllocByMass, Source: "đề xuất G4-0 (Q-2)", kind: "alloc"},
	{Key: port.ParamMoistureBasePct, Default: MoistureNone, Source: "C-4 (chưa có công thức)", kind: "moisture"},
	{Key: port.ParamLossRatioPct, Default: "5", Source: "proto #19", kind: "pct"},
	{Key: port.ParamHubProcessingLossPct, Default: "2", Source: "proto #56", kind: "pct"},
	{Key: port.ParamHubProcessingRate, Default: "120000", Source: "proto #55", kind: "rate"},
	{Key: port.ParamSettlementTermDays, Default: "15", Source: "proto PAYMENT_TERM_DAYS", kind: "days"},
	{Key: port.ParamCarbonRetainedPct, Default: "45", Source: "proto CARBON-SHARE", kind: "pct"},
	{Key: port.ParamCarbonSplitFarmers, Default: "70", Source: "proto CARBON-SHARE", kind: "pct"},
	{Key: port.ParamCarbonSplitAuthority, Default: "15", Source: "proto CARBON-SHARE", kind: "pct"},
	{Key: port.ParamCarbonSplitVerifier, Default: "15", Source: "proto CARBON-SHARE (nhận phần dư làm tròn)", kind: "pct"},
}

// ParamDefFor trả định nghĩa tham số; ok = false nếu khóa lạ.
func ParamDefFor(key string) (ParamDef, bool) {
	for _, p := range Params {
		if p.Key == key {
			return p, true
		}
	}
	return ParamDef{}, false
}

// NormalizeParam kiểm và chuẩn hóa giá trị đề xuất của một tham số.
func NormalizeParam(key, value string) (string, error) {
	def, ok := ParamDefFor(key)
	if !ok {
		return "", fieldErr("key", "khóa tham số không có")
	}
	v := strings.TrimSpace(value)
	switch def.kind {
	case "alloc":
		if v != AllocByMass && v != AllocByEqual {
			return "", fieldErr("value", "mass hoặc equal")
		}
		return v, nil
	case "moisture":
		if v != MoistureNone {
			return "", fieldErr("value", "chỉ nhận none: chưa có công thức quy đổi ẩm (C-4, chờ khách)")
		}
		return v, nil
	case "days":
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 365 {
			return "", fieldErr("value", "số ngày nguyên 1..365")
		}
		return strconv.Itoa(n), nil
	}
	d, err := decimal.NewFromString(v)
	if err != nil {
		return "", fieldErr("value", "chuỗi thập phân")
	}
	switch def.kind {
	case "pct":
		if d.IsNegative() || d.GreaterThan(decimal.NewFromInt(100)) || !d.Equal(d.Truncate(2)) {
			return "", fieldErr("value", "phần trăm 0..100, tối đa 2 chữ số thập phân")
		}
		return d.StringFixed(2), nil
	default: // rate
		if d.Sign() <= 0 || !d.Equal(d.Truncate(4)) || d.GreaterThanOrEqual(decimal.New(1, 14)) {
			return "", fieldErr("value", "đơn giá > 0, tối đa 4 chữ số thập phân")
		}
		return d.StringFixed(4), nil
	}
}

// ParamValue là giá trị tham số áp dụng kèm số phiên bản (0 = mặc định tạm).
type ParamValue struct {
	Key   string
	Value string
	Seq   int64
}

// Pinned trả chuỗi ghim "<khóa>#<số>".
func (p ParamValue) Pinned() string { return p.Key + "#" + strconv.FormatInt(p.Seq, 10) }

// Decimal đọc giá trị số (tham số pct / rate).
func (p ParamValue) Decimal() decimal.Decimal {
	d, err := decimal.NewFromString(p.Value)
	if err != nil {
		return decimal.Zero
	}
	return d
}

// Int đọc giá trị số nguyên (tham số days).
func (p ParamValue) Int() int {
	n, _ := strconv.Atoi(p.Value)
	return n
}

// Default trả giá trị mặc định #0 của khóa.
func Default(key string) ParamValue {
	def, _ := ParamDefFor(key)
	v, err := NormalizeParam(key, def.Default)
	if err != nil {
		v = def.Default
	}
	return ParamValue{Key: key, Value: v, Seq: 0}
}

// Pins là tập tham số đã ghim vào một kết quả: khóa → số phiên bản.
type Pins map[string]int64

// Add ghim một tham số.
func (p Pins) Add(v ParamValue) Pins {
	p[v.Key] = v.Seq
	return p
}

// Equal so hai tập ghim.
func (p Pins) Equal(o Pins) bool {
	if len(p) != len(o) {
		return false
	}
	for k, v := range p {
		if ov, ok := o[k]; !ok || ov != v {
			return false
		}
	}
	return true
}

// Keys trả khóa đã ghim (sắp xếp).
func (p Pins) Keys() []string {
	out := make([]string, 0, len(p))
	for k := range p {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
