package domain

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

// Row là một dòng của ảnh chụp "Cập nhật số liệu": một kỳ nguồn × biểu × phòng quy vào một tuần KHTC của kỳ (nguồn gốc
// ADR-0053 §8), hoặc một dòng mức phòng khi phòng không có số (Missing) hay nguồn chưa công bố (Unavailable).
type Row struct {
	TargetKey       string
	Department      string
	SourceModule    string
	FormCode        string // rỗng với dòng mức phòng
	CalendarID      string
	CalendarVersion *int
	NativeKey       string
	PeriodType      string
	From, To        *time.Time
	Closed          bool
	Converted       bool
	Missing         bool
	Unavailable     bool
	Fallback        bool // bảng giá ngày duyệt gần nhất trước tuần (D22) — không phải kỳ nguồn của tuần
	ClosedAt        *time.Time
	SnapshotNo      *int64
	FiguresHash     *string
	StatusHash      *string
	Confirmation    string
	Cells           map[string]string
}

// Identity là danh tính dòng để so hai ảnh chụp (không gồm băm).
func (r Row) Identity() string {
	v := "-"
	if r.CalendarVersion != nil {
		v = itoa(*r.CalendarVersion)
	}
	fb := ""
	if r.Fallback {
		fb = "|fallback"
	}
	return r.TargetKey + "|" + r.Department + "|" + r.SourceModule + "|" + r.FormCode + "|" + r.CalendarID + "|" + v + "|" + r.NativeKey + fb
}

// HasSource: dòng là một kỳ nguồn (không phải dòng mức phòng).
func (r Row) HasSource() bool { return r.FormCode != "" && r.NativeKey != "" }

// Rows dựng dòng ảnh chụp từ các kỳ KHTC có ngày cuối trong [from, to].
func Rows(periods []port.KHTCPeriod, from, to time.Time) []Row {
	from, to = dayOnly(from), dayOnly(to)
	var out []Row
	for _, p := range periods {
		if dayOnly(p.TargetTo).Before(from) || dayOnly(p.TargetTo).After(to) {
			continue
		}
		for _, d := range p.Departments {
			if len(d.Forms) == 0 {
				out = append(out, Row{TargetKey: p.TargetKey, Department: d.DepartmentCode, SourceModule: d.SourceModule,
					Missing: d.Missing, Unavailable: d.Unavailable, Closed: d.Closed})
				continue
			}
			for _, f := range d.Forms {
				fb := d.Fallback && f.FormCode == port.PriceFormBoard
				for _, s := range f.Sources {
					sf, st := s.From, s.To
					out = append(out, Row{TargetKey: p.TargetKey, Department: d.DepartmentCode, SourceModule: s.SourceModule, FormCode: s.FormCode,
						CalendarID: s.CalendarID, CalendarVersion: s.CalendarVersion, NativeKey: s.NativeKey, PeriodType: s.PeriodType,
						From: &sf, To: &st, Closed: s.Closed, Converted: s.Converted, Fallback: fb, ClosedAt: s.ClosedAt, SnapshotNo: s.SnapshotNo,
						FiguresHash: s.FiguresHash, StatusHash: s.StatusHash, Confirmation: s.ConfirmationStatus, Cells: s.Cells})
				}
			}
		}
	}
	return out
}

// Thay đổi của nguồn so với ảnh chụp.
const (
	ChangeAdded   = "added"   // kỳ nguồn mới quy vào kỳ (vd nguồn chốt kỳ mới, ngày giá mới được duyệt)
	ChangeRemoved = "removed" // kỳ nguồn không còn (thu hồi, hủy, mở lại một ngày giá)
	ChangeFigures = "figures" // số đã chốt đổi (figures_hash khác)
)

// SourceChange là một kỳ nguồn khác ảnh chụp.
type SourceChange struct {
	Kind string
	Row  Row // dòng hiện tại (removed: dòng trong ảnh chụp)
	Was  *Row
}

func hashOf(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Changes so dòng hiện tại của port với ảnh chụp: chỉ so figures_hash và tập kỳ nguồn — đổi status_hash (nhận HTX,
// D2) KHÔNG phải "Nguồn đã thay đổi". Dòng mức phòng không so. Chỉ cảnh báo, không ghi đè (BR-DL-04).
func Changes(snap, live []Row) []SourceChange {
	was := map[string]Row{}
	for _, r := range snap {
		if r.HasSource() {
			was[r.Identity()] = r
		}
	}
	var out []SourceChange
	seen := map[string]bool{}
	for _, r := range live {
		if !r.HasSource() {
			continue
		}
		id := r.Identity()
		seen[id] = true
		o, ok := was[id]
		switch {
		case !ok:
			out = append(out, SourceChange{Kind: ChangeAdded, Row: r})
		case hashOf(o.FiguresHash) != hashOf(r.FiguresHash):
			out = append(out, SourceChange{Kind: ChangeFigures, Row: r, Was: &o})
		}
	}
	for _, r := range snap {
		if r.HasSource() && !seen[r.Identity()] {
			out = append(out, SourceChange{Kind: ChangeRemoved, Row: r})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Row.Identity() < out[j].Row.Identity() })
	return out
}

// Trường của lịch sử thay đổi (giá trị cũ → mới).
const (
	FieldCumulative = "cumulative"
	FieldIncrement  = "increment"
	FieldSum        = "sum"
	FieldPriceMin   = "price_min"
	FieldPriceMax   = "price_max"
)

// Change là một dòng lịch sử "Cập nhật số liệu" (golden DiffEntry): khóa nguồn, trường, cũ → mới. Old nil = lần đầu
// có số; New nil = không còn số.
type Change struct {
	SourceKey string
	Field     string
	Old, New  *decimal.Decimal
}

// Diff so số của từng khóa nguồn giữa ảnh chụp trước (prev) và ảnh chụp mới (cur). Lần chụp đầu (prev nil) không có
// lịch sử. Thứ tự: khóa nguồn, rồi thứ tự trường cố định.
func Diff(prev, cur map[string]Value) []Change {
	if prev == nil {
		return nil
	}
	keys := map[string]bool{}
	for k := range prev {
		keys[k] = true
	}
	for k := range cur {
		keys[k] = true
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	var out []Change
	for _, k := range names {
		a, b := prev[k], cur[k]
		for _, f := range []struct {
			name string
			a, b *decimal.Decimal
		}{
			{FieldCumulative, a.Cumulative, b.Cumulative}, {FieldIncrement, a.Increment, b.Increment}, {FieldSum, a.Sum, b.Sum},
			{FieldPriceMin, a.Min, b.Min}, {FieldPriceMax, a.Max, b.Max},
		} {
			if f.a == nil && f.b == nil || f.a != nil && f.b != nil && f.a.Equal(*f.b) {
				continue
			}
			out = append(out, Change{SourceKey: k, Field: f.name, Old: f.a, New: f.b})
		}
	}
	return out
}
