// Package domain là quy tắc thuần của Kỳ KHTC (W11.62 — BRD KHTC v2.2 FN-TI-02.03, Mục II.4 "Lịch kỳ và cách đặt tên
// kỳ", Mục II.5 "Luồng trạng thái"; D4 KH2): chọn kỳ bằng ô chọn (loại, năm, tuần/tháng/quý, phạm vi, đến ngày), hệ
// thống tự điền khoảng thời gian, tên kỳ, ngày chốt, hạn nộp, hạn xuất; ba máy trạng thái (kỳ báo cáo, phiếu số liệu của
// phòng, bản báo cáo). Không I/O, không time.Now (R-12).
//
// Lịch: tuần báo cáo KHTC Thứ Tư – Thứ Ba (QĐ CEO 13/10 D7) — dùng lại quy tắc chia kỳ v2 của lịch kỳ hệ thống W11.03
// (extension/reportcycle/domain.PeriodRule), tháng/quý/năm gồm các tuần có Thứ Ba thuộc kỳ (BR-DL-01, BR-DL-06).
package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
)

// RuleVersion là phiên bản quy tắc ghi vào sổ Kỳ KHTC.
const RuleVersion = "khtc-period-2026.10"

// ProfileKHTC là profile cap/forms của phiếu "Báo cáo cho KHTC" (W11.05, gốc lắp ráp KNKHTCProfile).
const ProfileKHTC = "kn_khtc"

// TemplateCode là mã biểu cap/forms của phiếu cơ sở (W11.62). Mẫu phiếu có cột "Từ Kế hoạch năm", "Lũy kế kỳ trước"…
// thuộc W11.63 (KH3) — sẽ cài phiên bản mới của cùng mã hoặc mã khác chọn khi mở kỳ.
const TemplateCode = "KHTC-PHIEU"

// Loại báo cáo của kỳ KHTC (Mục II.4).
const (
	TypeWeek    = "week"    // Mẫu 1
	TypeMonth   = "month"   // Mẫu 2
	TypeQuarter = "quarter" // Mẫu 3 (phạm vi quý / 6 tháng / 9 tháng)
	TypeYear    = "year"    // Mẫu 4
	TypeProgram = "program" // Mẫu 5 — tiến độ các CT đến ngày
	TypeAdhoc   = "adhoc"   // báo cáo phát sinh
)

// Types là các loại báo cáo theo thứ tự hiển thị.
var Types = []string{TypeWeek, TypeMonth, TypeQuarter, TypeYear, TypeProgram, TypeAdhoc}

// Phạm vi của báo cáo quý.
const (
	ScopeQuarter = "quarter"     // Quý
	ScopeHalf    = "half"        // 6 tháng đầu năm (chỉ quý II)
	ScopeNine    = "nine_months" // 9 tháng đầu năm (chỉ quý III)
)

// Request là lựa chọn của KHTC ở popup "Mở kỳ" (B2).
type Request struct {
	Type   string
	Year   int        // năm (năm kế hoạch với Mẫu 5)
	No     int        // số tuần (1–52/53), tháng (1–12), quý (1–4)
	Scope  string     // quý: ScopeQuarter (mặc định) | ScopeHalf | ScopeNine
	ToDate *time.Time // Mẫu 5: "Đến ngày"
	Name   string     // phát sinh: tên báo cáo
	From   *time.Time // phát sinh: từ ngày
	To     *time.Time // phát sinh: đến ngày
}

// Plan là kỳ do hệ thống tự điền (B3): khoảng thời gian, tên kỳ, khóa kỳ, ngày chốt, hạn mặc định.
type Plan struct {
	Type, Scope string
	Year, No    int
	// Key: khóa chống mở trùng (cùng loại, năm, số tuần/tháng/quý — Mục II.4); rỗng với Mẫu 5 và phát sinh.
	Key string
	// FormsKind, FormsKey: loại/khóa kỳ ghi vào đặc tả kỳ native cap/forms (period_spec).
	FormsKind, FormsKey string
	Name                string
	From, To            time.Time // ngày dương lịch (00:00 UTC)
	Cutoff              time.Time // ngày chốt
	DueAt               time.Time // hạn nộp phiếu (UTC)
	ExportDueAt         time.Time // hạn xuất (UTC)
	// DueRequired: hạn nộp do KHTC nhập (Mẫu 5 "theo văn bản của Sở", phát sinh "theo hạn chỉ đạo") — DueAt chỉ là gợi ý.
	DueRequired bool
	Rule        string // thẻ quy tắc chia kỳ (rc.PeriodRule.Tag)
}

// ErrInput là lỗi lựa chọn (field, thông báo).
type ErrInput struct{ Field, Msg string }

func (e ErrInput) Error() string { return e.Field + ": " + e.Msg }

var romanQ = [...]string{"I", "II", "III", "IV"}

func vn(d time.Time) string { return d.Format("02/01/2006") }

// at trả mốc giờ hh:mm (giờ Việt Nam loc) của ngày d, đổi sang UTC.
func at(d time.Time, hh, mm int, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, loc).UTC()
}

// Giờ hạn mặc định (Mục II.4): hạn nộp phiếu Thứ Tư kế tiếp 10:00, hạn xuất Thứ Tư kế tiếp 16:00.
const (
	DueHour    = 10
	ExportHour = 16
)

// PlanOf dựng kỳ từ lựa chọn theo quy tắc chia kỳ rule (v2: tuần Thứ Tư – Thứ Ba, tháng theo Thứ Ba cuối).
func PlanOf(rule rc.PeriodRule, r Request, loc *time.Location) (Plan, error) {
	if rule.Version != rc.RuleVersionV2 {
		return Plan{}, ErrInput{"period_rule", "Kỳ KHTC cần quy tắc chia kỳ v2 (tuần Thứ Tư – Thứ Ba)"}
	}
	p := Plan{Type: r.Type, Year: r.Year, No: r.No, Rule: rule.Tag()}
	if r.Type != TypeAdhoc && (r.Year < 2000 || r.Year > 2100) {
		return Plan{}, ErrInput{"year", "năm 2000–2100"}
	}
	switch r.Type {
	case TypeWeek:
		w, err := rule.Week(r.Year, r.No)
		if err != nil {
			return Plan{}, ErrInput{"no", fmt.Sprintf("tuần 1–%d của năm %d", rule.WeeksInYear(r.Year), r.Year)}
		}
		p.From, p.To = w.From, w.To
		p.Key = fmt.Sprintf("%d-W%02d", w.Year, w.No)
		p.FormsKind, p.FormsKey = rc.PeriodWeek, p.Key
		p.Name = fmt.Sprintf("Tuần %d năm %d (từ ngày %s đến ngày %s)", w.No, w.Year, vn(w.From), vn(w.To))
	case TypeMonth:
		if r.No < 1 || r.No > 12 {
			return Plan{}, ErrInput{"no", "tháng 1–12"}
		}
		s, err := rule.PeriodOf(rc.PeriodMonth, rc.Date(r.Year, time.Month(r.No), 1), nil)
		if err != nil {
			return Plan{}, ErrInput{"no", err.Error()}
		}
		p.From, p.To, p.Key = s.From, s.To, s.Key
		p.FormsKind, p.FormsKey = rc.PeriodMonth, s.Key
		p.Name = fmt.Sprintf("Tháng %d năm %d (số liệu đến ngày %s)", r.No, r.Year, vn(s.To))
	case TypeQuarter:
		if r.No < 1 || r.No > 4 {
			return Plan{}, ErrInput{"no", "quý 1–4"}
		}
		p.Scope = r.Scope
		if p.Scope == "" {
			p.Scope = ScopeQuarter
		}
		q, err := rule.PeriodOf(rc.PeriodQuarter, rc.Date(r.Year, time.Month(r.No*3-2), 1), nil)
		if err != nil {
			return Plan{}, ErrInput{"no", err.Error()}
		}
		p.To = q.To
		switch p.Scope {
		case ScopeQuarter:
			p.From, p.Key = q.From, q.Key
			p.Name = fmt.Sprintf("Quý %s năm %d (số liệu đến ngày %s)", romanQ[r.No-1], r.Year, vn(q.To))
		case ScopeHalf, ScopeNine:
			want, label, suffix := 2, "6 tháng", "H1"
			if p.Scope == ScopeNine {
				want, label, suffix = 3, "9 tháng", "9M"
			}
			if r.No != want {
				return Plan{}, ErrInput{"scope", fmt.Sprintf("%s đầu năm đi với quý %s", label, romanQ[want-1])}
			}
			y, err := rule.PeriodOf(rc.PeriodYear, rc.Date(r.Year, 1, 1), nil)
			if err != nil {
				return Plan{}, ErrInput{"year", err.Error()}
			}
			p.From, p.Key = y.From, fmt.Sprintf("%d-%s", r.Year, suffix)
			p.Name = fmt.Sprintf("%s đầu năm %d (số liệu đến ngày %s)", label, r.Year, vn(q.To))
		default:
			return Plan{}, ErrInput{"scope", "quarter | half | nine_months"}
		}
		p.FormsKind, p.FormsKey = rc.PeriodQuarter, p.Key
	case TypeYear:
		s, err := rule.PeriodOf(rc.PeriodYear, rc.Date(r.Year, 1, 1), nil)
		if err != nil {
			return Plan{}, ErrInput{"year", err.Error()}
		}
		p.From, p.To, p.Key = s.From, s.To, s.Key
		p.FormsKind, p.FormsKey = rc.PeriodYear, s.Key
		p.Name = fmt.Sprintf("Năm %d (số liệu đến ngày %s)", r.Year, vn(s.To))
	case TypeProgram:
		if r.ToDate == nil {
			return Plan{}, ErrInput{"to_date", "Mẫu 5 cần \"Đến ngày\""}
		}
		d := rc.Date(r.ToDate.Year(), r.ToDate.Month(), r.ToDate.Day())
		w := rule.WeekOf(d)
		if w.To.After(d) { // tuần chứa ngày chọn chưa hết → tuần trước (tuần có Thứ Ba không sau ngày chọn)
			w = rule.WeekOf(d.AddDate(0, 0, -7))
		}
		first, err := rule.Week(r.Year, 1)
		if err != nil {
			return Plan{}, ErrInput{"year", err.Error()}
		}
		if w.Year != r.Year {
			return Plan{}, ErrInput{"to_date", fmt.Sprintf("đến ngày phải có ít nhất một tuần báo cáo của năm %d đã kết thúc", r.Year)}
		}
		p.From, p.To = first.From, w.To
		p.FormsKind, p.FormsKey = rc.PeriodAdhoc, fmt.Sprintf("%d-CT-%s", r.Year, d.Format("0102"))
		p.Name = fmt.Sprintf("Đến ngày %s (số liệu đến ngày %s)", vn(d), vn(w.To))
		p.DueRequired = true
	case TypeAdhoc:
		name := strings.TrimSpace(r.Name)
		if n := utf8.RuneCountInString(name); n < 3 || n > 150 {
			return Plan{}, ErrInput{"name", "tên báo cáo 3–150 ký tự"}
		}
		if r.From == nil || r.To == nil || r.To.Before(*r.From) {
			return Plan{}, ErrInput{"to_date", "từ ngày, đến ngày bắt buộc; đến ngày ≥ từ ngày"}
		}
		p.From, p.To = rc.Date(r.From.Year(), r.From.Month(), r.From.Day()), rc.Date(r.To.Year(), r.To.Month(), r.To.Day())
		if p.To.Sub(p.From) > 366*24*time.Hour {
			return Plan{}, ErrInput{"to_date", "tối đa một năm"}
		}
		p.Year = p.To.Year()
		p.FormsKind = rc.PeriodAdhoc
		p.Name = name
		p.DueRequired = true
	default:
		return Plan{}, ErrInput{"type", strings.Join(Types, " | ")}
	}
	p.Cutoff = p.To
	next := p.To.AddDate(0, 0, 1) // Thứ Tư kế tiếp với tuần/tháng/quý/năm
	p.DueAt, p.ExportDueAt = at(next, DueHour, 0, loc), at(next, ExportHour, 0, loc)
	return p, nil
}

// Overrides là ngày chốt, hạn nộp, hạn xuất KHTC sửa ở popup (B3).
type Overrides struct {
	Cutoff      *time.Time
	DueAt       *time.Time
	ExportDueAt *time.Time
}

// Apply áp phần sửa của KHTC và kiểm (Mục II.4): ngày chốt không trước ngày kết thúc khoảng thời gian (trừ Mẫu 5);
// hạn nộp sau đầu ngày chốt; hạn xuất không trước hạn nộp; Mẫu 5 và phát sinh bắt buộc nhập hạn nộp.
func (p Plan) Apply(o Overrides, loc *time.Location) (Plan, error) {
	if o.Cutoff != nil {
		p.Cutoff = rc.Date(o.Cutoff.Year(), o.Cutoff.Month(), o.Cutoff.Day())
	}
	if p.Type != TypeProgram && p.Cutoff.Before(p.To) {
		return p, ErrInput{"cutoff_date", "ngày chốt không được trước ngày kết thúc khoảng thời gian " + vn(p.To)}
	}
	if o.DueAt != nil {
		p.DueAt = o.DueAt.UTC()
	} else if p.DueRequired {
		return p, ErrInput{"due_at", "nhập hạn nộp theo văn bản yêu cầu / hạn chỉ đạo"}
	}
	if o.ExportDueAt != nil {
		p.ExportDueAt = o.ExportDueAt.UTC()
	} else if o.DueAt != nil && p.ExportDueAt.Before(p.DueAt) {
		p.ExportDueAt = p.DueAt
	}
	return p, ValidateDeadlines(p.Cutoff, p.DueAt, p.ExportDueAt, loc)
}

// ValidateDeadlines kiểm cặp hạn: hạn nộp không trước đầu ngày chốt (giờ VN), hạn xuất không trước hạn nộp.
func ValidateDeadlines(cutoff, due, export time.Time, loc *time.Location) error {
	if due.Before(at(cutoff, 0, 0, loc)) {
		return ErrInput{"due_at", "hạn nộp không được trước ngày chốt " + vn(cutoff)}
	}
	if export.Before(due) {
		return ErrInput{"export_due_at", "hạn xuất không được trước hạn nộp"}
	}
	return nil
}

// ValidReason kiểm lý do (đóng kỳ ngoại lệ, hủy kỳ, sửa hạn): 10–500 ký tự sau khi bỏ khoảng trắng hai đầu.
func ValidReason(s string) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n >= 10 && n <= 500
}

// QuantityEnd is the date through which full source weeks contribute. Program
// reports alone permit a cutoff before the requested period end; preserve To
// for their identity and narrative. Zero cutoff retains legacy pure callers.
func QuantityEnd(reportType string, to, cutoff time.Time) time.Time {
	if reportType == TypeProgram && !cutoff.IsZero() && cutoff.Before(to) {
		return cutoff
	}
	return to
}
