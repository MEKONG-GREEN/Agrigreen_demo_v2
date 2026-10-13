package domain

// Tổng hợp CNTS Cấp Tỉnh (W11.44 — BRD CNTS v3.7 UR-TINH-14, UR-TINH-15, FN-16, Mục III.5 quy tắc 2, 3, 5, 6; D4 CN4):
//   - BM-02 KSGM tuần (4 tuần/tháng — tuần KSGM, H04), tháng: theo cơ sở giết mổ = tổng BM-01 ĐÃ GỬI của các ngày +
//     dòng điều chỉnh (cộng/trừ, có lý do, chỉ ghi thêm — không ghi đè số tự cộng); số ngày có báo cáo, "Thiếu n ngày";
//     BM-01 đổi sau khi đã điều chỉnh → dòng điều chỉnh "cần xem lại".
//   - BM-18 Phụ lục 2 phần III thủy sản theo tuần CNTS (Thứ Ba – Thứ Hai, D8): khai thác = tổng BM-17 của các xã (không
//     sửa tay), nuôi trồng nhập theo đối tượng; cột Tuần trước liền kề, Trong kỳ, Lũy kế từ đầu quý, Lũy kế từ đầu năm.
//     Lũy kế = cộng các tuần đã nộp; tuần vắt quý/năm tính theo NGÀY CUỐI tuần; tuần không nộp tính 0, hiện "Thiếu kỳ";
//     tuần cũ đổi → lũy kế các tuần sau tính lại (tính khi đọc). Tổng sản lượng = khai thác + nuôi trồng (đơn vị tấn).
//
// Hàm thuần: app đọc số liệu (cap/forms FiguresReader + sổ pf_extension) rồi gọi các hàm ở đây. Số decimal chính xác.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Mã biểu tổng hợp (không phải biểu cap/forms — số do hệ thống tính).
const (
	CNTSBM02 = "CNTS-BM02"
	CNTSBM18 = "CNTS-BM18"
)

// AggRuleVersion là phiên bản quy tắc tổng hợp (ghi ở sổ, đổi cách tính = phiên bản mới).
const AggRuleVersion = "kn.cnts_agg/v1"

// AquaCatalogVersion là phiên bản danh sách đối tượng nuôi trồng của Phụ lục 2 (mẫu 2026).
const AquaCatalogVersion = "cnts.bm18_objects/v1"

// Cơ sở số liệu của bảng tổng hợp.
const (
	BasisSubmitted = "submitted" // bài nộp cuối (Đã nộp, Đã xác thực — bỏ bài "Yêu cầu điều chỉnh")
	BasisClosed    = "closed"    // ảnh chụp đóng băng mới nhất của kỳ nguồn (số đã chốt — D17, D18)
)

// Loại kỳ của bảng tổng hợp.
const (
	AggKSGMWeek = PeriodKSGMWeek // BM-02 tuần KSGM "2026-10-K2"
	AggMonth    = PeriodMonth    // BM-02 tháng "2026-10" (tháng dương lịch = 4 tuần KSGM)
	AggWeek     = PeriodWeek     // BM-18 tuần CNTS "2026-W41"
)

// Key là một cột số (khóa, nhãn).
type Key struct{ Key, Label string }

// KSGMSpecies là loài của BM-01 (thứ tự cột theo mẫu BCSLKSGM: Trâu bò, Heo, Gia cầm, Dê — KS-06…09).
var KSGMSpecies = []Key{{"trau_bo", "Trâu bò"}, {"heo", "Heo"}, {"gia_cam", "Gia cầm"}, {"de", "Dê"}}

// CatchGroups là nhóm khai thác của BM-17 (TS-01…04).
var CatchGroups = []Key{{"ca", "Cá các loại"}, {"tom", "Tôm các loại"}, {"muc", "Mực"}, {"khac", "Thủy sản khác"}}

// CatchTotal là khóa tổng khai thác (TS-05).
const CatchTotal = "tong"

// Đơn vị của đối tượng nuôi trồng.
const (
	UnitHa         = "ha"
	UnitCage       = "lồng"
	UnitTon        = "tấn"
	UnitThousandPc = "1.000 viên"
)

// AquaObject là một đối tượng nuôi trồng của Phụ lục 2 (UR-TINH-15).
type AquaObject struct {
	Code, Label string
	Group       string // mã nhóm (rỗng = dòng độc lập)
	AreaUnit    string // ha | lồng
	OutputUnit  string // tấn | 1.000 viên
}

// InTotal: sản lượng cộng vào "Tổng sản lượng thủy sản" (đơn vị tấn).
func (o AquaObject) InTotal() bool { return o.OutputUnit == UnitTon }

// AquaGroups là nhóm đối tượng (nhãn).
var AquaGroups = []Key{{"nuoi_tom", "Nuôi tôm"}, {"nhuyen_the", "Nhuyễn thể"}, {"ca_ao_ruong_veo", "Cá ao, ruộng, vèo"}}

// AquaObjects là danh sách đối tượng nuôi trồng mẫu 2026 (thứ tự dòng của Phụ lục 2).
var AquaObjects = []AquaObject{
	{"tom_tc_btc", "Tôm thâm canh – bán thâm canh", "nuoi_tom", UnitHa, UnitTon},
	{"tom_cnc", "Tôm công nghệ cao", "nuoi_tom", UnitHa, UnitTon},
	{"tom_lua", "Tôm – lúa", "nuoi_tom", UnitHa, UnitTon},
	{"tom_cang_xanh", "Tôm càng xanh", "nuoi_tom", UnitHa, UnitTon},
	{"tom_qccc", "Tôm quảng canh cải tiến", "nuoi_tom", UnitHa, UnitTon},
	{"tom_rung", "Tôm rừng", "nuoi_tom", UnitHa, UnitTon},
	{"ca_long_bien", "Cá lồng bè trên biển", "", UnitCage, UnitTon},
	{"ngoc_trai", "Ngọc trai", "", UnitHa, UnitThousandPc},
	{"long_be_nuoc_ngot", "Lồng bè nước ngọt", "", UnitCage, UnitTon},
	{"ca_tra", "Cá tra", "", UnitHa, UnitTon},
	{"ca_ro_phi", "Cá rô phi", "", UnitHa, UnitTon},
	{"hen", "Hến", "nhuyen_the", UnitHa, UnitTon},
	{"so_huyet", "Sò huyết", "nhuyen_the", UnitHa, UnitTon},
	{"so_long", "Sò lông", "nhuyen_the", UnitHa, UnitTon},
	{"vem", "Vẹm", "nhuyen_the", UnitHa, UnitTon},
	{"ngheu", "Nghêu", "nhuyen_the", UnitHa, UnitTon},
	{"cua_bien", "Cua biển", "", UnitHa, UnitTon},
	{"ca_loc", "Cá lóc", "ca_ao_ruong_veo", UnitHa, UnitTon},
	{"ech", "Ếch", "ca_ao_ruong_veo", UnitHa, UnitTon},
	{"luon", "Lươn", "", UnitHa, UnitTon},
}

// AquaObjectByCode tìm đối tượng nuôi trồng.
func AquaObjectByCode(code string) (AquaObject, bool) {
	i := slices.IndexFunc(AquaObjects, func(o AquaObject) bool { return o.Code == code })
	if i < 0 {
		return AquaObject{}, false
	}
	return AquaObjects[i], true
}

// Dec là map khóa → số decimal.
type Dec map[string]decimal.Decimal

func (d Dec) add(o Dec) {
	for k, v := range o {
		d[k] = d[k].Add(v)
	}
}

func (d Dec) clone() Dec {
	out := Dec{}
	out.add(d)
	return out
}

// Strings trả bản chuỗi thập phân của các khóa keys (vắng = "0").
func (d Dec) Strings(keys []string) map[string]string {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = d[k].String()
	}
	return out
}

// ParseDec đọc chuỗi thập phân (rỗng/sai → 0, false).
func ParseDec(s string) (decimal.Decimal, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return decimal.Zero, false
	}
	v, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, false
	}
	return v, true
}

func decOf(values map[string]string, keys []Key) Dec {
	out := Dec{}
	for _, k := range keys {
		if v, ok := ParseDec(values[k.Key]); ok {
			out[k.Key] = v
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Nguồn
// ---------------------------------------------------------------------------

// SourceUnit là số liệu của một đơn vị trong kỳ nguồn (theo cơ sở số liệu đã chọn).
type SourceUnit struct {
	Values    map[string]string
	NilReport bool
}

// SourcePeriod là một kỳ nguồn (BM-01 ngày, BM-17 tuần) đã quy về cơ sở số liệu.
type SourcePeriod struct {
	ID       uuid.UUID
	Key      string
	From, To time.Time
	Status   string // open | closed | reopened
	ClosedNo int64  // ảnh chụp mới nhất (0 = chưa chốt)
	Units    []string
	Parents  map[string]string
	Data     map[string]SourceUnit
}

// Closed: kỳ đang ở trạng thái đã chốt.
func (p SourcePeriod) Closed() bool { return p.Status == "closed" }

// ---------------------------------------------------------------------------
// BM-02 KSGM tuần, tháng
// ---------------------------------------------------------------------------

// BM02Adjustment là một dòng điều chỉnh (chỉ ghi thêm): cộng/trừ cho một cơ sở trong một tuần KSGM.
type BM02Adjustment struct {
	ID       uuid.UUID
	Facility string
	Commune  string
	WeekKey  string
	Deltas   Dec
	Reason   string
	Base     Dec // số tự cộng của cơ sở trong tuần LÚC điều chỉnh (phát hiện BM-01 đổi sau đó)
	ActorRef uuid.UUID
	At       time.Time
}

// BM02Row là một cơ sở giết mổ.
type BM02Row struct {
	Facility, Commune string
	Auto, Adjust      Dec // tự cộng BM-01; tổng điều chỉnh
	DaysExpected      int // số ngày cơ sở có trong kỳ ngày đã phát hành
	DaysReported      int // số ngày có bài (kể cả "không hoạt động")
	Adjustments       []uuid.UUID
	Stale             bool // có dòng điều chỉnh cần xem lại (BM-01 đổi sau khi điều chỉnh)
}

// Total = Auto + Adjust.
func (r BM02Row) Total() Dec {
	out := r.Auto.clone()
	out.add(r.Adjust)
	return out
}

// MissingDays = số ngày thiếu ("Thiếu n ngày").
func (r BM02Row) MissingDays() int { return max(0, r.DaysExpected-r.DaysReported) }

// BM02Result là bảng BM-02 của một tuần KSGM hoặc một tháng.
type BM02Result struct {
	Rows          []BM02Row
	Auto, Adjust  Dec
	SourceCount   int  // số kỳ ngày BM-01 trong khoảng
	SourceClosed  int  // số kỳ đang chốt
	AllClosed     bool // mọi kỳ ngày trong khoảng đã chốt (và có ít nhất một kỳ) — D17
	UnitsTotal    int
	UnitsReported int // cơ sở có ít nhất một ngày có bài
}

// Total = Auto + Adjust (toàn tỉnh).
func (r BM02Result) Total() Dec {
	out := r.Auto.clone()
	out.add(r.Adjust)
	return out
}

// WeekOfDay trả khóa tuần KSGM chứa ngày d trong danh sách tuần ("" nếu ngoài).
func WeekOfDay(weeks []Span, d time.Time) string {
	for _, w := range weeks {
		if !d.Before(w.From) && !d.After(w.To) {
			return w.Key
		}
	}
	return ""
}

// AutoByFacilityWeek cộng BM-01 theo cơ sở × tuần KSGM (dùng cho Base của dòng điều chỉnh và cờ "cần xem lại").
func AutoByFacilityWeek(weeks []Span, days []SourcePeriod) map[string]map[string]Dec {
	out := map[string]map[string]Dec{}
	for _, d := range days {
		wk := WeekOfDay(weeks, d.From)
		if wk == "" {
			continue
		}
		for u, x := range d.Data {
			if out[u] == nil {
				out[u] = map[string]Dec{}
			}
			if out[u][wk] == nil {
				out[u][wk] = Dec{}
			}
			out[u][wk].add(decOf(x.Values, KSGMSpecies))
		}
	}
	return out
}

func sameDec(a, b Dec, keys []Key) bool {
	for _, k := range keys {
		if !a[k.Key].Equal(b[k.Key]) {
			return false
		}
	}
	return true
}

// BM02 tổng hợp KSGM của các tuần KSGM weeks (một tuần = BM-02 tuần; 4 tuần = tháng) từ kỳ ngày BM-01 days (cơ sở số
// liệu đã chọn) và dòng điều chỉnh adjs. current = số tự cộng THEO BÀI NỘP HIỆN HÀNH (cho cờ "cần xem lại" — luôn so
// với bài nộp hiện hành dù đang xem số đã chốt); nil = không tính cờ.
func BM02(weeks []Span, days []SourcePeriod, adjs []BM02Adjustment, current map[string]map[string]Dec) BM02Result {
	rows := map[string]*BM02Row{}
	get := func(fac, commune string) *BM02Row {
		r := rows[fac]
		if r == nil {
			r = &BM02Row{Facility: fac, Commune: commune, Auto: Dec{}, Adjust: Dec{}}
			rows[fac] = r
		}
		if r.Commune == "" {
			r.Commune = commune
		}
		return r
	}
	res := BM02Result{Auto: Dec{}, Adjust: Dec{}}
	for _, d := range days {
		if WeekOfDay(weeks, d.From) == "" {
			continue
		}
		res.SourceCount++
		if d.Closed() {
			res.SourceClosed++
		}
		for _, u := range d.Units {
			r := get(u, d.Parents[u])
			r.DaysExpected++
			if x, ok := d.Data[u]; ok {
				r.DaysReported++
				r.Auto.add(decOf(x.Values, KSGMSpecies))
			}
		}
	}
	for _, a := range adjs {
		if !slices.ContainsFunc(weeks, func(w Span) bool { return w.Key == a.WeekKey }) {
			continue
		}
		r := get(a.Facility, a.Commune)
		r.Adjust.add(a.Deltas)
		r.Adjustments = append(r.Adjustments, a.ID)
		if current != nil && !sameDec(a.Base, current[a.Facility][a.WeekKey], KSGMSpecies) {
			r.Stale = true
		}
	}
	res.AllClosed = res.SourceCount > 0 && res.SourceClosed == res.SourceCount
	for _, r := range rows {
		res.Rows = append(res.Rows, *r)
		res.Auto.add(r.Auto)
		res.Adjust.add(r.Adjust)
		res.UnitsTotal++
		if r.DaysReported > 0 {
			res.UnitsReported++
		}
	}
	sort.Slice(res.Rows, func(i, j int) bool {
		a, b := res.Rows[i], res.Rows[j]
		if a.Commune != b.Commune {
			return a.Commune < b.Commune
		}
		return a.Facility < b.Facility
	})
	return res
}

// StaleAdjustments trả mã dòng điều chỉnh có Base khác số tự cộng hiện hành của cơ sở × tuần.
func StaleAdjustments(adjs []BM02Adjustment, current map[string]map[string]Dec) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for _, a := range adjs {
		if !sameDec(a.Base, current[a.Facility][a.WeekKey], KSGMSpecies) {
			out[a.ID] = true
		}
	}
	return out
}

// MonthKSGMWeeks trả 4 tuần KSGM của tháng (ranh giới đã sửa) — khóa "YYYY-MM-Kn".
func MonthKSGMWeeks(y int, m time.Month, starts []int) ([]Span, error) {
	if starts == nil {
		starts = DefaultKSGMStarts
	}
	return KSGMWeeks(y, m, starts)
}

// ---------------------------------------------------------------------------
// BM-18 Phụ lục 2
// ---------------------------------------------------------------------------

// Cols là bốn cột của Phụ lục 2.
type Cols struct{ Prev, Cur, QTD, YTD Dec }

func newCols() Cols { return Cols{Prev: Dec{}, Cur: Dec{}, QTD: Dec{}, YTD: Dec{}} }

func (c Cols) add(o Cols) {
	c.Prev.add(o.Prev)
	c.Cur.add(o.Cur)
	c.QTD.add(o.QTD)
	c.YTD.add(o.YTD)
}

// AquaEntry là số nuôi trồng đã nhập (dòng hiện hành) của một đối tượng trong một tuần.
type AquaEntry struct {
	WeekKey      string
	WeekTo       time.Time
	Object       string
	Area, Output *decimal.Decimal
}

// BM18CommuneRow là một xã (khai thác).
type BM18CommuneRow struct {
	Commune, Region string
	Cols            Cols // khóa CatchGroups + CatchTotal
	Reported        bool // có bài tuần này
	NilReport       bool
	Expected        bool     // xã có trong kỳ BM-17 của tuần này
	MissingWeeks    []string // "Thiếu kỳ": tuần trong năm (tới tuần này) xã có kỳ nhưng không có bài
	ByWeek          map[string]Dec
}

// BM18AquaRow là một đối tượng nuôi trồng.
type BM18AquaRow struct {
	Object AquaObject
	Area   Cols // khóa AquaKey
	Output Cols // khóa AquaKey
}

// BM18RegionTotal là dòng tổng Khu vực.
type BM18RegionTotal struct {
	Region string
	Cols   Cols
}

// BM18Result là bảng Phụ lục 2 của một tuần.
type BM18Result struct {
	Week          Span
	Prev          *Span
	Weeks         []Span // các tuần trong năm tới tuần này (sheet theo tuần)
	Communes      []BM18CommuneRow
	Regions       []BM18RegionTotal
	Catch         Cols // tổng khai thác (khóa CatchGroups + CatchTotal)
	Aqua          []BM18AquaRow
	AquaArea      Cols // tổng diện tích (đối tượng tính bằng ha), khóa AquaKey
	AquaOutput    Cols // tổng sản lượng nuôi trồng (tấn), khóa AquaKey
	Total         Cols // tổng sản lượng thủy sản = khai thác + nuôi trồng (tấn), khóa AquaKey
	SourceCount   int  // số kỳ BM-17 của tuần
	SourceClosed  int
	AllClosed     bool // mọi kỳ BM-17 của tuần đã chốt (và có ít nhất một kỳ) — D17
	UnitsTotal    int  // xã có trong kỳ tuần này
	UnitsReported int
	MissingUnits  []string
}

// AquaKey là khóa ô của cột nuôi trồng/tổng (một giá trị mỗi cột).
const AquaKey = "v"

// BM18Input là đầu vào của Phụ lục 2.
type BM18Input struct {
	Week    Span           // tuần chọn
	Periods []SourcePeriod // kỳ BM-17 có ngày cuối từ tuần trước liền kề tới tuần chọn (gồm cả năm)
	Aqua    []AquaEntry    // số nuôi trồng hiện hành (một dòng mỗi tuần × đối tượng)
	Regions map[string]string
}

func quarterOf(d time.Time) int { return (int(d.Month())-1)/3 + 1 }

// BM18 tính Phụ lục 2. Tuần trước liền kề = tuần kết thúc ngay trước ngày đầu tuần chọn; lũy kế quý/năm theo ngày cuối
// tuần (cùng năm, cùng quý dương lịch của ngày cuối), tới tuần chọn.
func BM18(in BM18Input) BM18Result {
	w := in.Week
	res := BM18Result{Week: w, Catch: newCols(), AquaArea: newCols(), AquaOutput: newCols(), Total: newCols()}
	prevEnd := w.From.AddDate(0, 0, -1)
	inYTD := func(to time.Time) bool { return to.Year() == w.To.Year() && !to.After(w.To) }
	inQTD := func(to time.Time) bool { return inYTD(to) && quarterOf(to) == quarterOf(w.To) }
	keys := append(slices.Clone(CatchGroups), Key{Key: CatchTotal})
	rows := map[string]*BM18CommuneRow{}
	get := func(c string) *BM18CommuneRow {
		r := rows[c]
		if r == nil {
			r = &BM18CommuneRow{Commune: c, Region: in.Regions[c], Cols: newCols(), ByWeek: map[string]Dec{}}
			rows[c] = r
		}
		return r
	}
	weeks := map[string]Span{}
	// Gộp các kỳ cùng khóa (nhiều lô): đơn vị hợp, số của kỳ phát hành sau thắng.
	sort.SliceStable(in.Periods, func(i, j int) bool { return in.Periods[i].From.Before(in.Periods[j].From) })
	type wk struct {
		span     Span
		units    map[string]bool
		data     map[string]SourceUnit
		n, close int
	}
	byKey := map[string]*wk{}
	var order []string
	for _, p := range in.Periods {
		x := byKey[p.Key]
		if x == nil {
			x = &wk{span: Span{Key: p.Key, From: p.From, To: p.To}, units: map[string]bool{}, data: map[string]SourceUnit{}}
			byKey[p.Key] = x
			order = append(order, p.Key)
		}
		x.n++
		if p.Closed() {
			x.close++
		}
		for _, u := range p.Units {
			x.units[u] = true
		}
		for u, d := range p.Data {
			x.data[u] = d
		}
	}
	for _, k := range order {
		x := byKey[k]
		to := x.span.To
		isCur, isPrev := k == w.Key, to.Equal(prevEnd)
		ytd, qtd := inYTD(to), inQTD(to)
		if !isCur && !isPrev && !ytd {
			continue
		}
		if ytd {
			weeks[k] = x.span
		}
		if isCur {
			res.SourceCount, res.SourceClosed = x.n, x.close
			res.AllClosed = x.n > 0 && x.n == x.close
		}
		if isPrev {
			s := x.span
			res.Prev = &s
		}
		for u := range x.units {
			r := get(u)
			d, ok := x.data[u]
			if isCur {
				r.Expected = true
				r.Reported, r.NilReport = ok, ok && d.NilReport
			}
			if !ok {
				if ytd {
					r.MissingWeeks = append(r.MissingWeeks, k)
				}
				continue
			}
			vals := decOf(d.Values, CatchGroups)
			tot := decimal.Zero
			for _, g := range CatchGroups {
				tot = tot.Add(vals[g.Key])
			}
			vals[CatchTotal] = tot
			if isCur {
				r.Cols.Cur.add(vals)
			}
			if isPrev {
				r.Cols.Prev.add(vals)
			}
			if qtd {
				r.Cols.QTD.add(vals)
			}
			if ytd {
				r.Cols.YTD.add(vals)
				r.ByWeek[k] = vals
			}
		}
	}
	for _, s := range weeks {
		res.Weeks = append(res.Weeks, s)
	}
	sort.Slice(res.Weeks, func(i, j int) bool { return res.Weeks[i].To.Before(res.Weeks[j].To) })
	regTot := map[string]Cols{}
	for _, r := range rows {
		for _, k := range keys {
			for _, c := range []Dec{r.Cols.Prev, r.Cols.Cur, r.Cols.QTD, r.Cols.YTD} {
				if _, ok := c[k.Key]; !ok {
					c[k.Key] = decimal.Zero
				}
			}
		}
		sort.Strings(r.MissingWeeks)
		res.Communes = append(res.Communes, *r)
		res.Catch.add(r.Cols)
		rt, ok := regTot[r.Region]
		if !ok {
			rt = newCols()
		}
		rt.add(r.Cols)
		regTot[r.Region] = rt
		if r.Expected {
			res.UnitsTotal++
			if r.Reported {
				res.UnitsReported++
			} else {
				res.MissingUnits = append(res.MissingUnits, r.Commune)
			}
		}
	}
	sort.Strings(res.MissingUnits)
	sort.Slice(res.Communes, func(i, j int) bool {
		a, b := res.Communes[i], res.Communes[j]
		if a.Region != b.Region {
			return a.Region < b.Region
		}
		return a.Commune < b.Commune
	})
	for reg, c := range regTot {
		res.Regions = append(res.Regions, BM18RegionTotal{Region: reg, Cols: c})
	}
	sort.Slice(res.Regions, func(i, j int) bool { return res.Regions[i].Region < res.Regions[j].Region })
	// Nuôi trồng.
	aqua := map[string]*BM18AquaRow{}
	for _, o := range AquaObjects {
		aqua[o.Code] = &BM18AquaRow{Object: o, Area: newCols(), Output: newCols()}
	}
	for _, e := range in.Aqua {
		r := aqua[e.Object]
		if r == nil {
			continue
		}
		isCur, isPrev := e.WeekKey == w.Key, e.WeekTo.Equal(prevEnd)
		ytd, qtd := inYTD(e.WeekTo), inQTD(e.WeekTo)
		for _, pair := range []struct {
			c *Cols
			x *decimal.Decimal
		}{{&r.Area, e.Area}, {&r.Output, e.Output}} {
			if pair.x == nil {
				continue
			}
			d := Dec{AquaKey: *pair.x}
			if isCur {
				pair.c.Cur.add(d)
			}
			if isPrev {
				pair.c.Prev.add(d)
			}
			if qtd {
				pair.c.QTD.add(d)
			}
			if ytd {
				pair.c.YTD.add(d)
			}
		}
	}
	for _, o := range AquaObjects {
		r := aqua[o.Code]
		res.Aqua = append(res.Aqua, *r)
		if o.AreaUnit == UnitHa {
			res.AquaArea.add(r.Area)
		}
		if o.InTotal() {
			res.AquaOutput.add(r.Output)
		}
	}
	tc := func(c Dec) Dec { return Dec{AquaKey: c[CatchTotal]} }
	res.Total.add(Cols{Prev: tc(res.Catch.Prev), Cur: tc(res.Catch.Cur), QTD: tc(res.Catch.QTD), YTD: tc(res.Catch.YTD)})
	res.Total.add(res.AquaOutput)
	return res
}

// ---------------------------------------------------------------------------
// Hợp đồng closed_figures/v1 (ADR-0053): dòng công bố của bảng tổng hợp
// ---------------------------------------------------------------------------

// PubRow là một phần tử `rows` của họ closed_figures/v1 (không gồm `confirmation` — view thêm).
type PubRow struct {
	Key       string            `json:"key"`
	Unit      string            `json:"unit"`
	UnitKind  string            `json:"unit_kind"`
	Parent    *string           `json:"parent"`
	Region    *string           `json:"region"`
	Indicator *string           `json:"indicator"`
	NilReport bool              `json:"nil_report"`
	Cells     map[string]string `json:"cells"`
}

// PubTotal là một phần tử `totals` (dòng Khu vực/Toàn tỉnh do nguồn tính).
type PubTotal struct {
	Key       string            `json:"key"`
	Kind      string            `json:"kind"`
	Region    *string           `json:"region"`
	Indicator *string           `json:"indicator"`
	Members   []string          `json:"members"`
	Cells     map[string]string `json:"cells"`
}

// Pub là nội dung công bố của một lần chốt bảng tổng hợp.
type Pub struct {
	Rows          []PubRow
	Totals        []PubTotal
	UnitsTotal    int
	UnitsWithData int
	MissingUnits  []string
	Provisional   bool
}

func strp(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Hậu tố ô lũy kế (Q-047/D18: KHTC đọc lũy kế của nguồn — dùng "Last" trên các ô này).
const (
	SuffixQTD = "_luy_ke_quy"
	SuffixYTD = "_luy_ke_nam"
)

func colCells(prefix string, c Cols, key string, out map[string]string) {
	out[prefix] = c.Cur[key].String()
	out[prefix+SuffixQTD] = c.QTD[key].String()
	out[prefix+SuffixYTD] = c.YTD[key].String()
}

// UnitKindAquaObject là loại đơn vị của dòng nuôi trồng trong hợp đồng (unit = mã đối tượng).
const UnitKindAquaObject = "aquaculture_object"

// PubBM18 dựng dòng công bố của Phụ lục 2: mỗi xã một dòng (khai thác theo nhóm + tổng, kỳ và lũy kế), mỗi đối tượng
// nuôi trồng một dòng; ô "tong_san_luong" (tấn) có ở cả hai loại dòng nên Σ rows = tổng sản lượng thủy sản.
func PubBM18(r BM18Result) Pub {
	p := Pub{UnitsTotal: r.UnitsTotal, UnitsWithData: r.UnitsReported, MissingUnits: slices.Clone(r.MissingUnits)}
	if p.MissingUnits == nil {
		p.MissingUnits = []string{}
	}
	p.Provisional = len(p.MissingUnits) > 0
	for _, c := range r.Communes {
		if len(c.MissingWeeks) > 0 {
			p.Provisional = true // lũy kế có tuần thiếu ("Thiếu kỳ")
		}
		cells := map[string]string{}
		for _, g := range CatchGroups {
			colCells("khai_thac_"+g.Key, c.Cols, g.Key, cells)
		}
		colCells("khai_thac_tong", c.Cols, CatchTotal, cells)
		colCells("tong_san_luong", c.Cols, CatchTotal, cells)
		p.Rows = append(p.Rows, PubRow{Key: c.Commune, Unit: c.Commune, UnitKind: UnitCommune, Region: strp(c.Region),
			NilReport: c.NilReport, Cells: cells})
	}
	for _, a := range r.Aqua {
		cells := map[string]string{}
		if a.Object.AreaUnit == UnitHa {
			colCells("nuoi_trong_dien_tich", a.Area, AquaKey, cells)
		} else {
			colCells("nuoi_trong_so_long", a.Area, AquaKey, cells)
		}
		if a.Object.InTotal() {
			colCells("nuoi_trong_san_luong", a.Output, AquaKey, cells)
			colCells("tong_san_luong", a.Output, AquaKey, cells)
		} else {
			colCells("nuoi_trong_ngoc_trai_nghin_vien", a.Output, AquaKey, cells)
		}
		p.Rows = append(p.Rows, PubRow{Key: "nuoi_trong:" + a.Object.Code, Unit: a.Object.Code, UnitKind: UnitKindAquaObject,
			Parent: strp(a.Object.Group), Cells: cells})
	}
	for _, rg := range r.Regions {
		cells := map[string]string{}
		colCells("khai_thac_tong", rg.Cols, CatchTotal, cells)
		var members []string
		for _, c := range r.Communes {
			if c.Region == rg.Region {
				members = append(members, c.Commune)
			}
		}
		p.Totals = append(p.Totals, PubTotal{Key: "region:" + rg.Region, Kind: "region", Region: strp(rg.Region), Members: members, Cells: cells})
	}
	cells := map[string]string{}
	for _, g := range CatchGroups {
		colCells("khai_thac_"+g.Key, r.Catch, g.Key, cells)
	}
	colCells("khai_thac_tong", r.Catch, CatchTotal, cells)
	colCells("nuoi_trong_dien_tich", r.AquaArea, AquaKey, cells)
	colCells("nuoi_trong_san_luong", r.AquaOutput, AquaKey, cells)
	colCells("tong_san_luong", r.Total, AquaKey, cells)
	p.Totals = append(p.Totals, PubTotal{Key: "province", Kind: "province", Members: []string{}, Cells: cells})
	if p.Rows == nil {
		p.Rows = []PubRow{}
	}
	return p
}

// PubBM02 dựng dòng công bố của BM-02 (một tuần KSGM): mỗi cơ sở một dòng, ô = số sau điều chỉnh từng loài, phần điều
// chỉnh ("<loài>_dieu_chinh") và số ngày có báo cáo.
func PubBM02(r BM02Result) Pub {
	p := Pub{UnitsTotal: r.UnitsTotal, UnitsWithData: r.UnitsReported, MissingUnits: []string{}}
	for _, row := range r.Rows {
		cells := map[string]string{}
		tot := row.Total()
		for _, s := range KSGMSpecies {
			cells[s.Key] = tot[s.Key].String()
			cells[s.Key+"_dieu_chinh"] = row.Adjust[s.Key].String()
		}
		cells["so_ngay_bao_cao"] = fmt.Sprint(row.DaysReported)
		if row.DaysReported == 0 {
			p.MissingUnits = append(p.MissingUnits, row.Facility)
		}
		if row.MissingDays() > 0 {
			p.Provisional = true
		}
		p.Rows = append(p.Rows, PubRow{Key: row.Facility, Unit: row.Facility, UnitKind: UnitFacility, Parent: strp(row.Commune), Cells: cells})
	}
	if len(p.MissingUnits) > 0 {
		p.Provisional = true
	}
	sort.Strings(p.MissingUnits)
	tot := r.Total()
	cells := map[string]string{}
	for _, s := range KSGMSpecies {
		cells[s.Key] = tot[s.Key].String()
		cells[s.Key+"_dieu_chinh"] = r.Adjust[s.Key].String()
	}
	p.Totals = []PubTotal{{Key: "province", Kind: "province", Members: []string{}, Cells: cells}}
	if p.Rows == nil {
		p.Rows = []PubRow{}
	}
	return p
}

// FiguresHash là sha256 (hex) của [rows, totals] — đổi khi và chỉ khi số công bố đổi (ADR-0053 §8).
func FiguresHash(rows []PubRow, totals []PubTotal) (string, error) {
	b, err := json.Marshal([]any{rows, totals})
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// aggNS là không gian tên UUIDv5 cho mã kỳ tổng hợp và mã định nghĩa (ổn định qua các lần chốt).
var aggNS = uuid.MustParse("6f1d2c1e-44a1-5c4b-9b8e-0c0b11440000")

// AggPeriodID là mã kỳ tổng hợp ổn định (source_period_id của hợp đồng) theo tenant × tỉnh × biểu × loại kỳ × khóa.
func AggPeriodID(tenant uuid.UUID, province, form, periodType, key string) uuid.UUID {
	return uuid.NewSHA1(aggNS, []byte(strings.Join([]string{tenant.String(), province, form, periodType, key}, "|")))
}

// AggDefinitionID là mã "phiên bản biểu" của bảng tổng hợp (form_version_id của hợp đồng) — đổi khi quy tắc/danh sách
// đối tượng đổi phiên bản.
func AggDefinitionID(form string) uuid.UUID {
	ver := AggRuleVersion
	if form == CNTSBM18 {
		ver += "|" + AquaCatalogVersion
	}
	return uuid.NewSHA1(aggNS, []byte(form+"|"+ver))
}
