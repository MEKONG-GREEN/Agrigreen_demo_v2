package domain

import (
	"sort"
	"strings"

	"github.com/shopspring/decimal"
)

// VesselClass là một lớp phương tiện thủy (GIS BR-37; đặc tả prototype
// platform/geo/vessels.ts). Kích thước là GIẢ ĐỊNH kỹ thuật, chỉnh qua danh mục
// vessel_class / tham số waterway.vessel:* của pf/masterdata.
type VesselClass struct {
	Code      string          `json:"code"`
	Tons      decimal.Decimal `json:"tons"`        // tải trọng đăng ký
	BeamM     decimal.Decimal `json:"beam_m"`      // chiều rộng thân
	DraftM    decimal.Decimal `json:"draft_m"`     // mớn nước đầy tải
	AirDraftM decimal.Decimal `json:"air_draft_m"` // chiều cao tĩnh không từ mặt nước
}

// SafetyMargins là biên an toàn — kênh vừa khít trên giấy là không đi được ngoài thực địa.
type SafetyMargins struct {
	WidthFactor decimal.Decimal `json:"width_factor"` // kênh ≥ thân tàu × hệ số
	UnderKeelM  decimal.Decimal `json:"under_keel_m"` // nước dưới đáy tàu
	AirGapM     decimal.Decimal `json:"air_gap_m"`    // dự trữ dưới gầm cầu
}

// DefaultVessels là ba lớp phương tiện của chuỗi cung ứng rơm (ghe 100 t, sà lan 1.000 / 2.000 t).
func DefaultVessels() []VesselClass {
	return []VesselClass{
		{Code: "ghe_100t", Tons: decimal.NewFromInt(100), BeamM: decimal.RequireFromString("5.5"), DraftM: decimal.RequireFromString("1.4"), AirDraftM: decimal.RequireFromString("3.5")},
		{Code: "sa_lan_1000t", Tons: decimal.NewFromInt(1000), BeamM: decimal.NewFromInt(10), DraftM: decimal.RequireFromString("2.5"), AirDraftM: decimal.NewFromInt(6)},
		{Code: "sa_lan_2000t", Tons: decimal.NewFromInt(2000), BeamM: decimal.NewFromInt(12), DraftM: decimal.RequireFromString("3.2"), AirDraftM: decimal.NewFromInt(7)},
	}
}

// DefaultSafety là biên an toàn mặc định.
func DefaultSafety() SafetyMargins {
	return SafetyMargins{
		WidthFactor: decimal.RequireFromString("1.5"),
		UnderKeelM:  decimal.RequireFromString("0.4"),
		AirGapM:     decimal.RequireFromString("0.5"),
	}
}

// SortVessels xếp lớp phương tiện theo tải trọng tăng dần (rồi theo mã) — quy tắc
// "lớp lớn nhất còn qua được" dựa vào thứ tự này.
func SortVessels(vs []VesselClass) {
	sort.SliceStable(vs, func(i, j int) bool {
		if c := vs[i].Tons.Cmp(vs[j].Tons); c != 0 {
			return c < 0
		}
		return vs[i].Code < vs[j].Code
	})
}

// Constraints là ràng buộc BẤT LỢI NHẤT trên một cạnh: rộng hẹp nhất, sâu cạn
// nhất, tĩnh không thấp nhất. Nil = chưa khai báo. ClearanceM nil và
// ClearanceRequired = false nghĩa là không có công trình vượt sông (không ràng
// buộc) — khác hẳn "có cầu nhưng chưa đo tĩnh không" (ClearanceRequired = true).
type Constraints struct {
	WidthM            *decimal.Decimal
	DepthM            *decimal.Decimal
	ClearanceM        *decimal.Decimal
	ClearanceRequired bool
	// Nguồn của từng giá trị: "segment" hoặc mã công trình.
	WidthFrom, DepthFrom, ClearanceFrom string
}

// Dimension là thông số của ràng buộc.
const (
	DimWidth     = "width"
	DimDepth     = "depth"
	DimClearance = "clearance"
)

// PassCheck là kết quả một phương tiện qua một cạnh: QUA, KHÔNG QUA, hoặc CHƯA
// KẾT LUẬN (thiếu số liệu). Gộp "thiếu số liệu" vào "qua được" sẽ điều sà lan
// 2.000 tấn vào con kênh chưa ai đo độ sâu.
type PassCheck struct {
	Passes   bool
	Blockers []string // thông số chặn: width | depth | clearance
	Unknown  []string // thông số chưa khai báo
}

// CanPass đối chiếu một phương tiện với ràng buộc, có biên an toàn.
func CanPass(v VesselClass, c Constraints, m SafetyMargins) PassCheck {
	var pc PassCheck
	if c.WidthM == nil {
		pc.Unknown = append(pc.Unknown, DimWidth)
	} else if c.WidthM.LessThan(v.BeamM.Mul(m.WidthFactor)) {
		pc.Blockers = append(pc.Blockers, DimWidth)
	}
	if c.DepthM == nil {
		pc.Unknown = append(pc.Unknown, DimDepth)
	} else if c.DepthM.LessThan(v.DraftM.Add(m.UnderKeelM)) {
		pc.Blockers = append(pc.Blockers, DimDepth)
	}
	switch {
	case c.ClearanceM != nil:
		if c.ClearanceM.LessThan(v.AirDraftM.Add(m.AirGapM)) {
			pc.Blockers = append(pc.Blockers, DimClearance)
		}
	case c.ClearanceRequired:
		pc.Unknown = append(pc.Unknown, DimClearance)
	}
	pc.Passes = len(pc.Blockers) == 0 && len(pc.Unknown) == 0
	return pc
}

// Capacity là tải trọng lưu thông của một cạnh/đoạn sau khi áp quy tắc phương tiện.
type Capacity struct {
	// MaxLoadTons dùng để lọc; nil = chưa kết luận được.
	MaxLoadTons *decimal.Decimal
	// Derived là tải trọng suy ra từ thông số (nil khi thiếu số liệu).
	Derived      *decimal.Decimal
	Certain      bool
	VesselCode   string // lớp lớn nhất còn qua; rỗng nếu không lớp nào
	Limiting     string // width | depth | clearance | declared | none | unknown:<thông số,…>
	StructureRef string // mã công trình gây ràng buộc (nếu có)
}

// Limiting đặc biệt.
const (
	LimitNone     = "none"
	LimitDeclared = "declared"
	LimitUnknown  = "unknown:"
)

// Derive suy ra tải trọng lưu thông: lớp phương tiện LỚN NHẤT còn qua được với
// ràng buộc bất lợi nhất (GIS BR-37/38). Thiếu số liệu → không đoán: Derived nil;
// khi đó chỉ dùng tải trọng khai báo nếu có. Khai báo nhỏ hơn suy ra → lấy khai
// báo (hạn chế của cơ quan quản lý luồng).
func Derive(c Constraints, declared *decimal.Decimal, vessels []VesselClass, m SafetyMargins) Capacity {
	vs := append([]VesselClass(nil), vessels...)
	SortVessels(vs)
	var out Capacity
	var unknown []string
	var largest *VesselClass
	var firstFail *PassCheck
	for i := range vs {
		pc := CanPass(vs[i], c, m)
		for _, u := range pc.Unknown {
			if !containsStr(unknown, u) {
				unknown = append(unknown, u)
			}
		}
		if pc.Passes {
			largest = &vs[i]
			continue
		}
		if firstFail == nil && len(pc.Blockers) > 0 && len(pc.Unknown) == 0 {
			p := pc
			firstFail = &p
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		if declared != nil {
			d := *declared
			out.MaxLoadTons = &d
			out.Limiting = LimitDeclared
			return out
		}
		out.Limiting = LimitUnknown + strings.Join(unknown, ",")
		return out
	}
	derived := decimal.Zero
	if largest != nil {
		derived = largest.Tons
		out.VesselCode = largest.Code
	}
	out.Certain = true
	out.Derived = &derived
	eff := derived
	out.Limiting = LimitNone
	if firstFail != nil {
		dim := firstFail.Blockers[0]
		out.Limiting = dim
		out.StructureRef = c.source(dim)
	}
	if declared != nil && declared.LessThan(eff) {
		eff = *declared
		out.Limiting = LimitDeclared
		out.StructureRef = ""
	}
	out.MaxLoadTons = &eff
	return out
}

func (c Constraints) source(dim string) string {
	var s string
	switch dim {
	case DimWidth:
		s = c.WidthFrom
	case DimDepth:
		s = c.DepthFrom
	case DimClearance:
		s = c.ClearanceFrom
	}
	if s == "segment" {
		return ""
	}
	return s
}

// Worse trả tải trọng bất lợi hơn của hai năng lực (nil = chưa kết luận thắng).
func Worse(a, b Capacity) Capacity {
	switch {
	case a.MaxLoadTons == nil:
		return a
	case b.MaxLoadTons == nil:
		return b
	case b.MaxLoadTons.LessThan(*a.MaxLoadTons):
		return b
	}
	return a
}

func containsStr(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
