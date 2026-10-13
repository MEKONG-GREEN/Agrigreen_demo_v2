package domain

import (
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
)

// Số của khóa nguồn theo từng tuần KHTC và cho cả kỳ KHTC (docs/uat/khtc-week-mapping.md, golden tests/golden/khtc):
//
//   - Q3: kỳ nguồn tính trọn vào tuần KHTC chứa ngày cuối (port KHTCSources đã quy đổi — crosswalk v1).
//   - Q4/Q5 (D18): lũy kế tuần = lũy kế của kỳ nguồn MỚI NHẤT quy vào tuần; không có kỳ nguồn → giữ lũy kế tuần trước,
//     trong kỳ 0. Trong kỳ = lũy kế − gốc; gốc = lũy kế ĐÃ ĐÓNG của tuần trước nếu tuần đó đã đóng, không thì lũy kế hiện
//     tại của tuần trước; chuỗi lũy kế nguồn mới (năm mới với ModeCumulative) → gốc 0. Trong kỳ có thể âm.
//   - Q6 (D17): tuần "đã chốt" khi mọi kỳ nguồn (mọi biểu của phòng) quy vào tuần đã chốt; không có kỳ nguồn → đã chốt.
//   - Q7: tuần KHTC đã đóng giữ số (Frozen) — nguồn đổi sau đó chỉ gắn SourceChanged, phần chênh vào tuần mở kế tiếp.
//   - Lũy kế năm KHTC = Σ trong kỳ của các tuần cùng năm KHTC (năm của ngày cuối tuần).
//   - Q8/D22: giá = min/max toàn tỉnh của mặt hàng ở bảng giá đã duyệt; hai ngày đã duyệt gần nhất không sau cuối kỳ.
//   - Q9/D2: phần "Chưa xác nhận" tách riêng (Unconfirmed), vẫn tính vào tổng.

// Frozen là số đã chốt của khóa nguồn ở một tuần KHTC đã đóng (ảnh chụp cuối của kỳ tuần đó).
type Frozen struct {
	Cumulative  decimal.Decimal
	Increment   *decimal.Decimal // nil → lũy kế − gốc
	YearToDate  *decimal.Decimal // nil → tính
	Unconfirmed *decimal.Decimal
}

// PriceDay là một cột "Ngày …" của giá (bảng giá đã duyệt).
type PriceDay struct {
	Day      string
	Min, Max decimal.Decimal
}

// Week là số của khóa nguồn ở một tuần KHTC.
type Week struct {
	Key           string
	From, To      time.Time
	SourceKeys    []string // danh tính kỳ nguồn quy vào (D8: "<lịch>:v<n>:<khóa gốc>")
	NoSource      bool     // không kỳ nguồn nào (có ô) quy vào tuần — lũy kế giữ tuần trước
	Frozen        bool     // tuần KHTC đã đóng — giữ số
	SourceChanged bool     // tuần đã đóng mà lũy kế nguồn hiện khác số đã giữ (BR-DL-04)
	Closed        bool
	Unavailable   bool // nguồn chưa công bố view
	Fallback      bool // giá: không ngày duyệt trong tuần → ngày duyệt gần nhất trước tuần (D22)
	Cumulative    decimal.Decimal
	Base          decimal.Decimal
	BaseRef       string // frozen:<tuần> | current:<tuần> | reset | start
	Increment     decimal.Decimal
	YearToDate    decimal.Decimal
	Unconfirmed   decimal.Decimal
	Sum           decimal.Decimal
	Series        string
}

// Value là số của khóa nguồn cho một kỳ KHTC (một hoặc nhiều tuần liên tiếp).
type Value struct {
	SourceKey     string
	Mode          string
	Cumulative    *decimal.Decimal // cum, season
	Base          *decimal.Decimal
	BaseRef       string
	Increment     *decimal.Decimal // cum, season: Σ trong kỳ các tuần của kỳ
	YearToDate    *decimal.Decimal
	Sum           *decimal.Decimal // sum
	Min, Max      *decimal.Decimal // price: ngày đã duyệt gần nhất không sau cuối kỳ
	PriceDay      string
	PriceDays     []PriceDay // price: hai ngày đã duyệt gần nhất
	Unconfirmed   *decimal.Decimal
	Closed        bool
	NoSource      bool
	Frozen        bool
	SourceChanged bool
	Fallback      bool
	Unavailable   bool
	SourceKeys    []string
	Weeks         []Week
}

// Provisional: số "Tạm tính" (BR-DL-03) — nguồn chưa chốt hoặc chưa công bố.
func (v Value) Provisional() bool { return !v.Closed || v.Unavailable }

func dayOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func dec(s string) (decimal.Decimal, bool) {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	return d, err == nil
}

func ptr(d decimal.Decimal) *decimal.Decimal { return &d }

// identity: danh tính D8 của kỳ nguồn.
func identity(s port.SourcePeriodRef) string {
	v := "-"
	if s.CalendarVersion != nil {
		v = "v" + itoa(*s.CalendarVersion)
	}
	return s.CalendarID + ":" + v + ":" + s.NativeKey
}

func itoa(n int) string { return decimal.NewFromInt(int64(n)).String() }

// find trả phòng và biểu của khóa nguồn trong một kỳ KHTC (nil nếu vắng).
func find(p port.KHTCPeriod, k SourceKey) (*port.DepartmentFigures, *port.TargetFigure) {
	for i := range p.Departments {
		d := &p.Departments[i]
		if d.DepartmentCode != k.Department {
			continue
		}
		for j := range d.Forms {
			if strings.EqualFold(d.Forms[j].FormCode, k.Form) {
				return d, &d.Forms[j]
			}
		}
		return d, nil
	}
	return nil, nil
}

// seriesOf: chuỗi lũy kế của kỳ nguồn — ModeCumulative theo năm của khóa gốc (lũy kế từ đầu năm), ModeSeason một chuỗi.
func seriesOf(mode string, s port.SourcePeriodRef) string {
	if mode == ModeSeason {
		return "season"
	}
	if len(s.NativeKey) >= 4 {
		return s.NativeKey[:4]
	}
	return s.To.Format("2006")
}

// Evaluate tính số của khóa nguồn k từ các kỳ KHTC (tuần liên tiếp, sắp theo thời gian, gồm các tuần trước kỳ để có
// gốc và lũy kế năm) và số đã giữ của tuần KHTC đã đóng (khóa tuần → Frozen). Kỳ KHTC = các tuần có ngày cuối trong
// [from, to].
func Evaluate(k SourceKey, periods []port.KHTCPeriod, frozen map[string]Frozen, from, to time.Time) Value {
	from, to = dayOnly(from), dayOnly(to)
	weeks := make([]Week, 0, len(periods))
	for i, p := range periods {
		var prev *Week
		if i > 0 {
			prev = &weeks[i-1]
		}
		weeks = append(weeks, week(k, p, prev, frozen))
	}
	v := Value{SourceKey: k.Raw, Mode: k.Mode, Closed: true, NoSource: true, Frozen: true}
	var in []Week
	for _, w := range weeks {
		if !dayOnly(w.To).Before(from) && !dayOnly(w.To).After(to) {
			in = append(in, w)
		}
	}
	if len(in) == 0 {
		v.Frozen = false
		return v
	}
	v.Weeks = in
	seen := map[string]bool{}
	inc, sum := decimal.Zero, decimal.Zero
	for _, w := range in {
		v.Closed = v.Closed && w.Closed
		v.NoSource = v.NoSource && w.NoSource
		v.Frozen = v.Frozen && w.Frozen
		v.SourceChanged = v.SourceChanged || w.SourceChanged
		v.Unavailable = v.Unavailable || w.Unavailable
		inc, sum = inc.Add(w.Increment), sum.Add(w.Sum)
		for _, s := range w.SourceKeys {
			if !seen[s] {
				seen[s] = true
				v.SourceKeys = append(v.SourceKeys, s)
			}
		}
	}
	first, last := in[0], in[len(in)-1]
	switch k.Mode {
	case ModeSum:
		v.Sum = ptr(sum)
	case ModePrice:
		price(&v, k, periods, from, to)
	default:
		v.Cumulative, v.Base, v.BaseRef, v.Increment = ptr(last.Cumulative), ptr(first.Base), first.BaseRef, ptr(inc)
		v.YearToDate, v.Unconfirmed = ptr(last.YearToDate), ptr(last.Unconfirmed)
	}
	return v
}

// week tính số của khóa nguồn ở một tuần KHTC.
func week(k SourceKey, p port.KHTCPeriod, prev *Week, frozen map[string]Frozen) Week {
	w := Week{Key: p.TargetKey, From: p.TargetFrom, To: p.TargetTo, NoSource: true, Closed: true}
	df, f := find(p, k)
	if df != nil {
		w.Unavailable, w.Fallback = df.Unavailable, df.Fallback
		w.Closed = df.Closed
	}
	var latest *port.SourcePeriodRef
	var latestVal decimal.Decimal
	if f != nil {
		w.Closed = f.KeyClosed
		srcs := append([]port.SourcePeriodRef(nil), f.Sources...)
		sort.SliceStable(srcs, func(i, j int) bool { return srcs[i].To.Before(srcs[j].To) })
		for i := range srcs {
			s := srcs[i]
			w.SourceKeys = append(w.SourceKeys, identity(s))
			x, ok := cellOf(s.Cells, k.Cell)
			if !ok {
				continue
			}
			d, ok := dec(x)
			if !ok {
				continue
			}
			w.NoSource = false
			w.Sum = w.Sum.Add(d)
			latest, latestVal = &srcs[i], d
		}
		if latest != nil {
			if x, ok := cellOf(f.LastUnconfirmed, k.Cell); ok {
				w.Unconfirmed, _ = dec(x)
			}
		}
	}
	if w.Unavailable {
		w.Closed = false
	}
	if k.Mode == ModeSum || k.Mode == ModePrice {
		return w
	}
	// Lũy kế, gốc, trong kỳ.
	switch {
	case latest != nil:
		w.Cumulative, w.Series = latestVal, seriesOf(k.Mode, *latest)
	case prev != nil:
		w.Cumulative, w.Series, w.Unconfirmed = prev.Cumulative, prev.Series, prev.Unconfirmed
	}
	switch {
	case prev == nil:
		w.BaseRef = "start"
	case latest != nil && prev.Series != "" && w.Series != prev.Series:
		w.BaseRef = "reset"
	case prev.Frozen:
		w.Base, w.BaseRef = prev.Cumulative, "frozen:"+prev.Key
	default:
		w.Base, w.BaseRef = prev.Cumulative, "current:"+prev.Key
	}
	fr, isFrozen := frozen[p.TargetKey]
	if isFrozen {
		w.Frozen, w.Closed = true, true
		w.SourceChanged = latest != nil && !latestVal.Equal(fr.Cumulative)
		w.Cumulative = fr.Cumulative
		if fr.Unconfirmed != nil {
			w.Unconfirmed = *fr.Unconfirmed
		}
	}
	w.Increment = w.Cumulative.Sub(w.Base)
	if isFrozen && fr.Increment != nil {
		w.Increment = *fr.Increment
	}
	switch {
	case isFrozen && fr.YearToDate != nil:
		w.YearToDate = *fr.YearToDate
	case prev != nil && prev.To.Year() == w.To.Year():
		w.YearToDate = prev.YearToDate.Add(w.Increment)
	default:
		w.YearToDate = w.Increment
	}
	return w
}

// price điền giá của mặt hàng: các ngày bảng giá đã duyệt (kỳ nguồn PRICE-BOARD đã chốt có ô của mặt hàng) không sau
// cuối kỳ; giá của kỳ = ngày đã duyệt gần nhất, hai cột = hai ngày gần nhất (Q8, M4). Không ngày duyệt trong kỳ →
// Fallback (D22).
func price(v *Value, k SourceKey, periods []port.KHTCPeriod, from, to time.Time) {
	days := map[string]PriceDay{}
	for _, p := range periods {
		_, f := find(p, k)
		if f == nil {
			continue
		}
		for _, s := range f.Sources {
			if !s.Closed || dayOnly(s.To).After(to) {
				continue
			}
			lo, ok1 := cellOf(s.Cells, k.Cell+"/price_min")
			hi, ok2 := cellOf(s.Cells, k.Cell+"/price_max")
			dlo, ok3 := dec(lo)
			dhi, ok4 := dec(hi)
			if ok1 && ok2 && ok3 && ok4 {
				day := dayOnly(s.To).Format(time.DateOnly)
				days[day] = PriceDay{Day: day, Min: dlo, Max: dhi}
			}
		}
	}
	keys := make([]string, 0, len(days))
	for d := range days {
		keys = append(keys, d)
	}
	sort.Strings(keys)
	if len(keys) > 2 {
		keys = keys[len(keys)-2:]
	}
	for _, d := range keys {
		v.PriceDays = append(v.PriceDays, days[d])
	}
	v.Fallback = true
	if n := len(v.PriceDays); n > 0 {
		last := v.PriceDays[n-1]
		v.Min, v.Max, v.PriceDay = ptr(last.Min), ptr(last.Max), last.Day
		v.Fallback = last.Day < from.Format(time.DateOnly)
	}
}
