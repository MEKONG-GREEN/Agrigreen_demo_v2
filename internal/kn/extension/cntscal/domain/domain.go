// Package domain: quy tắc thuần của lịch trực KSGM (BM-03) và lịch tiêm phòng (BM-07) — W11.46, D4 CN6, BRD CNTS v3.7
// FN-15, UR-TINH-10, UR-TINH-11. Không phụ thuộc hạ tầng, không time.Now() (R-12).
package domain

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// RuleVersion là phiên bản quy tắc ghi vào sổ.
const RuleVersion = "cnts-cal/1"

// Ca trực (UR-TINH-10): N (ngày), Đ (đêm), NĐ (cả ngày đêm); ô trống = nghỉ. Ca đêm qua nửa đêm thuộc ngày bắt đầu ca.
const (
	ShiftDay   = "n"
	ShiftNight = "d"
	ShiftBoth  = "nd"
)

// Giới hạn.
const (
	MaxCells      = 20000
	MaxNoteLen    = 1000
	MaxDoses      = 60
	MaxDoseNote   = 300
	MaxAgeDays    = 3650
	MaxLocality   = 300
	MaxHeadCount  = 100000000
	SourceGrid    = "grid"
	SourceCopy    = "copy"
	SourceExcel   = "excel"
	monthLayout   = "2006-01"
	maxBackMonths = 24
)

// FieldErr là lỗi của một trường.
type FieldErr struct {
	Field  string
	Reason string
}

// Errors là danh sách lỗi trường (422).
type Errors []FieldErr

func (e Errors) Error() string {
	parts := make([]string, 0, len(e))
	for _, x := range e {
		parts = append(parts, x.Field+": "+x.Reason)
	}
	return strings.Join(parts, "; ")
}

func (e *Errors) add(field, reason string) { *e = append(*e, FieldErr{Field: field, Reason: reason}) }

// ---------------------------------------------------------------------------
// Lịch trực KSGM (BM-03)
// ---------------------------------------------------------------------------

// ParseMonth đọc tháng "YYYY-MM" → ngày 1 (UTC).
func ParseMonth(s string) (time.Time, error) {
	t, err := time.Parse(monthLayout, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, errors.New("tháng dạng YYYY-MM")
	}
	return t, nil
}

// MonthKey là "YYYY-MM".
func MonthKey(m time.Time) string { return m.Format(monthLayout) }

// DaysIn là số ngày của tháng.
func DaysIn(m time.Time) int { return m.AddDate(0, 1, -1).Day() }

// CheckMonth: tháng lập lịch không quá 24 tháng trước, không quá 12 tháng tới (chặn nhập nhầm năm).
func CheckMonth(m, today time.Time) error {
	cur := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	if m.Before(cur.AddDate(0, -maxBackMonths, 0)) || m.After(cur.AddDate(0, 12, 0)) {
		return errors.New("chỉ lập lịch từ 24 tháng trước đến 12 tháng tới")
	}
	return nil
}

// Cell là một ô lưới: viên chức trực cơ sở vào ngày (1..31) với ca.
type Cell struct {
	Facility uuid.UUID `json:"f"`
	Officer  uuid.UUID `json:"o"`
	Day      int       `json:"d"`
	Shift    string    `json:"s"`
}

// Date là ngày của ô trong tháng m.
func (c Cell) Date(m time.Time) time.Time { return m.AddDate(0, 0, c.Day-1) }

// NormalizeCells kiểm và sắp ô: ngày thuộc tháng, ca hợp lệ, cơ sở/viên chức có mã, không trùng (cơ sở, viên chức,
// ngày). Ca viết hoa/có dấu ("N", "Đ", "NĐ") được chuẩn hóa.
func NormalizeCells(m time.Time, cells []Cell) ([]Cell, Errors) {
	var es Errors
	if len(cells) > MaxCells {
		es.add("cells", fmt.Sprintf("tối đa %d ô", MaxCells))
		return nil, es
	}
	days := DaysIn(m)
	seen := map[[3]string]bool{}
	out := make([]Cell, 0, len(cells))
	for i, c := range cells {
		f := fmt.Sprintf("cells[%d]", i)
		c.Shift = NormalizeShift(c.Shift)
		switch {
		case c.Facility == uuid.Nil:
			es.add(f+".facility_ref", "bắt buộc")
		case c.Officer == uuid.Nil:
			es.add(f+".officer_ref", "bắt buộc")
		case c.Day < 1 || c.Day > days:
			es.add(f+".day", fmt.Sprintf("ngày 1–%d của tháng", days))
		case c.Shift == "":
			es.add(f+".shift", "ca N, Đ hoặc NĐ")
		default:
			k := [3]string{c.Facility.String(), c.Officer.String(), fmt.Sprint(c.Day)}
			if seen[k] {
				es.add(f, "trùng ô (cơ sở, viên chức, ngày)")
				continue
			}
			seen[k] = true
			out = append(out, c)
		}
	}
	sortCells(out)
	return out, es
}

// NormalizeShift chuẩn hóa mã ca; không hợp lệ → "".
func NormalizeShift(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "N":
		return ShiftDay
	case "D", "Đ":
		return ShiftNight
	case "ND", "NĐ":
		return ShiftBoth
	}
	return ""
}

func sortCells(cs []Cell) {
	sort.Slice(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.Facility != b.Facility {
			return a.Facility.String() < b.Facility.String()
		}
		if a.Officer != b.Officer {
			return a.Officer.String() < b.Officer.String()
		}
		return a.Day < b.Day
	})
}

// overlap: hai ca cùng ngày giao nhau (NĐ giao mọi ca).
func overlap(a, b string) bool { return a == ShiftBoth || b == ShiftBoth || a == b }

// Facility là cơ sở giết mổ đang hoạt động trong tháng (danh mục W11.41).
type Facility struct {
	Ref     uuid.UUID
	Commune string
}

// Grant cho biết viên chức được cấp quyền cơ sở (W11.07 phạm vi Cấp Xã). known = false: không biết (chưa nối danh bạ).
type Grant func(officer, facility uuid.UUID, commune string) (granted, known bool)

// Mã cảnh báo lịch trực (không chặn lưu — UR-TINH-10, FN-15 BR-01/BR-04).
const (
	WarnUncovered  = "facility_uncovered" // cơ sở hoạt động chưa có người trực trong ngày
	WarnDouble     = "double_shift"       // một viên chức trực 2 cơ sở cùng ca
	WarnNotGranted = "officer_not_granted"
)

// Warning là một cảnh báo lịch trực.
type Warning struct {
	Code     string
	Message  string
	Facility uuid.UUID
	Officer  uuid.UUID
	Days     []int
}

// RosterWarnings tính cảnh báo của lịch tháng m.
func RosterWarnings(m time.Time, cells []Cell, facilities []Facility, grant Grant) []Warning {
	var out []Warning
	days := DaysIn(m)
	covered := map[uuid.UUID]map[int]bool{}
	byOfficerDay := map[uuid.UUID]map[int][]Cell{}
	for _, c := range cells {
		if covered[c.Facility] == nil {
			covered[c.Facility] = map[int]bool{}
		}
		covered[c.Facility][c.Day] = true
		if byOfficerDay[c.Officer] == nil {
			byOfficerDay[c.Officer] = map[int][]Cell{}
		}
		byOfficerDay[c.Officer][c.Day] = append(byOfficerDay[c.Officer][c.Day], c)
	}
	for _, f := range facilities {
		var missing []int
		for d := 1; d <= days; d++ {
			if !covered[f.Ref][d] {
				missing = append(missing, d)
			}
		}
		if len(missing) > 0 {
			out = append(out, Warning{Code: WarnUncovered, Facility: f.Ref, Days: missing,
				Message: fmt.Sprintf("Cơ sở chưa có người trực %d ngày", len(missing))})
		}
	}
	officers := make([]uuid.UUID, 0, len(byOfficerDay))
	for o := range byOfficerDay {
		officers = append(officers, o)
	}
	slices.SortFunc(officers, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, o := range officers {
		var double []int
		for d := 1; d <= days; d++ {
			cs := byOfficerDay[o][d]
		outer:
			for i := range cs {
				for j := i + 1; j < len(cs); j++ {
					if cs[i].Facility != cs[j].Facility && overlap(cs[i].Shift, cs[j].Shift) {
						double = append(double, d)
						break outer
					}
				}
			}
		}
		if len(double) > 0 {
			out = append(out, Warning{Code: WarnDouble, Officer: o, Days: double,
				Message: fmt.Sprintf("Viên chức trực 2 cơ sở cùng ca %d ngày", len(double))})
		}
	}
	if grant != nil {
		commune := map[uuid.UUID]string{}
		for _, f := range facilities {
			commune[f.Ref] = f.Commune
		}
		seen := map[[2]uuid.UUID]bool{}
		for _, c := range cells {
			k := [2]uuid.UUID{c.Officer, c.Facility}
			if seen[k] {
				continue
			}
			seen[k] = true
			if ok, known := grant(c.Officer, c.Facility, commune[c.Facility]); known && !ok {
				out = append(out, Warning{Code: WarnNotGranted, Officer: c.Officer, Facility: c.Facility,
					Message: "Viên chức chưa được cấp quyền cơ sở này (FN-14) — gợi ý cấp quyền"})
			}
		}
	}
	return out
}

// ChangedOfficers trả viên chức có ô thay đổi giữa hai bản (đổi ca → thông báo viên chức liên quan), đã sắp.
func ChangedOfficers(old, cur []Cell) []uuid.UUID {
	key := func(c Cell) string { return c.Facility.String() + "|" + c.Officer.String() + "|" + fmt.Sprint(c.Day) }
	a, b := map[string]Cell{}, map[string]Cell{}
	for _, c := range old {
		a[key(c)] = c
	}
	for _, c := range cur {
		b[key(c)] = c
	}
	set := map[uuid.UUID]bool{}
	for k, c := range a {
		if n, ok := b[k]; !ok || n.Shift != c.Shift {
			set[c.Officer] = true
		}
	}
	for k, c := range b {
		if _, ok := a[k]; !ok {
			set[c.Officer] = true
		}
	}
	out := make([]uuid.UUID, 0, len(set))
	for o := range set {
		out = append(out, o)
	}
	slices.SortFunc(out, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
	return out
}

// CopyCells sao chép lịch tháng trước sang tháng m theo số ngày (ngày không có trong tháng m bị bỏ).
func CopyCells(prev []Cell, m time.Time) []Cell {
	days := DaysIn(m)
	out := make([]Cell, 0, len(prev))
	for _, c := range prev {
		if c.Day <= days {
			out = append(out, c)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Lịch tiêm phòng (BM-07)
// ---------------------------------------------------------------------------

// Sự việc của lứa.
const (
	BatchCreated      = "created"
	BatchUpdated      = "updated"
	BatchDoseRecorded = "dose_recorded"
	BatchCancelled    = "cancelled"
)

// Trạng thái hiển thị của lứa.
const (
	StatusOpen      = "open"      // còn mũi chưa tiêm, chưa quá hạn
	StatusOverdue   = "overdue"   // có mũi quá hạn chưa tiêm (nhãn đỏ)
	StatusDone      = "done"      // mọi mũi đã có ngày thực tế
	StatusCancelled = "cancelled" // đã hủy
)

// PlanDose là một mũi của quy trình (vaccine, lần, ngày tuổi).
type PlanDose struct {
	Vaccine string
	Shot    int
	AgeDays int
}

// Dose là một mũi của lịch tiêm một lứa.
type Dose struct {
	Vaccine  string     `json:"vaccine"`
	Shot     int        `json:"shot"`
	AgeDays  int        `json:"age_days"`
	Expected time.Time  `json:"-"`
	Actual   *time.Time `json:"-"`
	Note     string     `json:"note,omitempty"`
}

// Key là "<vaccine>/<lần>".
func (d Dose) Key() string { return fmt.Sprintf("%s/%d", d.Vaccine, d.Shot) }

// Overdue: quá ngày dự kiến mà chưa có ngày tiêm thực tế (BRD bảng cảnh báo số 11).
func (d Dose) Overdue(today time.Time) bool { return d.Actual == nil && d.Expected.Before(today) }

// GenerateDoses sinh lịch tiêm từ quy trình: ngày dự kiến = ngày nhận + ngày tuổi − tuổi lúc nhận (UR-TINH-11, FN-15
// AC-03). Mũi có ngày tuổi nhỏ hơn tuổi lúc nhận (đã qua trước khi nhận giống) không sinh.
func GenerateDoses(plan []PlanDose, received time.Time, ageAtReceipt int) []Dose {
	out := make([]Dose, 0, len(plan))
	for _, p := range plan {
		if p.AgeDays < ageAtReceipt {
			continue
		}
		out = append(out, Dose{Vaccine: p.Vaccine, Shot: p.Shot, AgeDays: p.AgeDays, Expected: received.AddDate(0, 0, p.AgeDays-ageAtReceipt)})
	}
	sortDoses(out)
	return out
}

func sortDoses(ds []Dose) {
	sort.SliceStable(ds, func(i, j int) bool {
		if !ds[i].Expected.Equal(ds[j].Expected) {
			return ds[i].Expected.Before(ds[j].Expected)
		}
		return ds[i].Key() < ds[j].Key()
	})
}

// StatusOf là trạng thái hiển thị của lứa.
func StatusOf(event string, doses []Dose, today time.Time) string {
	if event == BatchCancelled {
		return StatusCancelled
	}
	done := true
	for _, d := range doses {
		if d.Overdue(today) {
			return StatusOverdue
		}
		if d.Actual == nil {
			done = false
		}
	}
	if done {
		return StatusDone
	}
	return StatusOpen
}

// NextDose là mũi chưa tiêm sớm nhất (nil = hết).
func NextDose(doses []Dose) *Dose {
	for i := range doses {
		if doses[i].Actual == nil {
			d := doses[i]
			return &d
		}
	}
	return nil
}

// DoseUpdate là thay đổi một mũi: ngày dự kiến (Cấp Tỉnh), ngày thực tế, ghi chú (Cấp Xã, Cấp Tỉnh).
type DoseUpdate struct {
	Vaccine     string
	Shot        int
	Expected    *time.Time
	Actual      *time.Time
	ClearActual bool
	Note        *string
}

// ApplyDoseUpdates áp thay đổi lên bản hiện hành. canPlan = người lập lịch (được sửa ngày dự kiến từng hộ). Ngày thực
// tế không ở tương lai, không trước ngày nhận giống. Trả bản mới và cờ có thay đổi.
func ApplyDoseUpdates(cur []Dose, ups []DoseUpdate, canPlan bool, received, today time.Time) ([]Dose, bool, Errors) {
	var es Errors
	out := slices.Clone(cur)
	idx := map[string]int{}
	for i, d := range out {
		idx[d.Key()] = i
	}
	changed := false
	for k, u := range ups {
		f := fmt.Sprintf("doses[%d]", k)
		i, ok := idx[fmt.Sprintf("%s/%d", u.Vaccine, u.Shot)]
		if !ok {
			es.add(f, "không có mũi này trong lịch của lứa")
			continue
		}
		d := out[i]
		if u.Expected != nil && !u.Expected.Equal(d.Expected) {
			if !canPlan {
				es.add(f+".expected", "chỉ người lập lịch (Cấp Tỉnh) sửa ngày dự kiến")
				continue
			}
			if u.Expected.Before(received) {
				es.add(f+".expected", "không trước ngày nhận giống")
				continue
			}
			d.Expected, changed = *u.Expected, true
		}
		if u.ClearActual && d.Actual != nil {
			d.Actual, changed = nil, true
		}
		if u.Actual != nil && (d.Actual == nil || !u.Actual.Equal(*d.Actual)) {
			if u.Actual.After(today) {
				es.add(f+".actual", "ngày tiêm thực tế không ở tương lai")
				continue
			}
			if u.Actual.Before(received) {
				es.add(f+".actual", "ngày tiêm thực tế không trước ngày nhận giống")
				continue
			}
			a := *u.Actual
			d.Actual, changed = &a, true
		}
		if u.Note != nil {
			n := strings.TrimSpace(*u.Note)
			if utf8.RuneCountInString(n) > MaxDoseNote {
				es.add(f+".note", fmt.Sprintf("tối đa %d ký tự", MaxDoseNote))
				continue
			}
			if n != d.Note {
				d.Note, changed = n, true
			}
		}
		out[i] = d
	}
	sortDoses(out)
	return out, changed, es
}

// doseJSON là dạng lưu ở cột doses (jsonb).
type doseJSON struct {
	Vaccine  string  `json:"vaccine"`
	Shot     int     `json:"shot"`
	AgeDays  int     `json:"age_days"`
	Expected string  `json:"expected"`
	Actual   *string `json:"actual"`
	Note     string  `json:"note,omitempty"`
}

// EncodeDoses chuyển mũi sang dạng lưu.
func EncodeDoses(ds []Dose) []any {
	out := make([]any, 0, len(ds))
	for _, d := range ds {
		j := doseJSON{Vaccine: d.Vaccine, Shot: d.Shot, AgeDays: d.AgeDays, Expected: d.Expected.Format(time.DateOnly), Note: d.Note}
		if d.Actual != nil {
			a := d.Actual.Format(time.DateOnly)
			j.Actual = &a
		}
		out = append(out, j)
	}
	return out
}

// DecodeDose đọc một mũi từ dạng lưu.
func DecodeDose(vaccine string, shot, ageDays int, expected string, actual *string, note string) (Dose, error) {
	e, err := time.Parse(time.DateOnly, expected)
	if err != nil {
		return Dose{}, err
	}
	d := Dose{Vaccine: vaccine, Shot: shot, AgeDays: ageDays, Expected: e, Note: note}
	if actual != nil {
		a, err := time.Parse(time.DateOnly, *actual)
		if err != nil {
			return Dose{}, err
		}
		d.Actual = &a
	}
	return d, nil
}
