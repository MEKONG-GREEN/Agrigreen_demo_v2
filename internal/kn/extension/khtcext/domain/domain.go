// Package domain là quy tắc thuần của "Số liệu nguồn ngoài" KHTC (W11.67 — BRD KHTC v2.2 FN-TI-02.11, BR-DL-19; D4 KH7):
// chỉ tiêu nguồn ngoài, giá trị theo chỉ tiêu × kỳ KHTC kèm nguồn bắt buộc (cơ quan, văn bản hoặc kênh, ngày) và minh
// chứng, kiểm từng dòng của "Nạp Excel tổng hợp" hai bước. Số là decimal (R-10). Không I/O, không time.Now (R-12).
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RuleVersion là phiên bản quy tắc ghi vào sổ.
const RuleVersion = "khtc-ext/2026.10"

// Giới hạn.
const (
	MaxEvidence     = 10
	MaxEvidenceSize = 15 << 20
	MaxImportRows   = 1000
	MaxImportBytes  = 5 << 20
	MinReason       = 10
	// Giá trị số nhập (API, Excel): độ dài chuỗi, chữ số phần nguyên (|giá trị| < 10^17, khớp numeric(22,4) và CHECK
	// của bảng) và chữ số thập phân.
	MaxValueLen        = 64
	MaxValueIntDigits  = 17
	MaxValueFracDigits = 4
)

// Hành động của sổ số liệu.
const (
	ActionSet      = "set"
	ActionWithdraw = "withdraw"
)

// ErrInput: dữ liệu nhập không hợp lệ (chi tiết ở FieldErrors).
var ErrInput = errors.New("số liệu nguồn ngoài không hợp lệ")

// FieldError là lỗi của một trường.
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// FieldErrors là danh sách lỗi trường.
type FieldErrors []FieldError

func (e FieldErrors) Error() string {
	parts := make([]string, 0, len(e))
	for _, f := range e {
		parts = append(parts, f.Field+": "+f.Reason)
	}
	return strings.Join(parts, "; ")
}
func (e FieldErrors) Unwrap() error { return ErrInput }

func (e *FieldErrors) add(field, reason string) {
	*e = append(*e, FieldError{Field: field, Reason: reason})
}

var codeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)

// NameKey chuẩn hóa tên chỉ tiêu để so trùng (chữ thường, gộp khoảng trắng).
func NameKey(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func badText(s string, max int) bool {
	return utf8.RuneCountInString(s) > max || strings.ContainsRune(s, 0)
}

// IndicatorInput là chỉ tiêu mới ("Thêm chỉ tiêu mới ngay trên màn": tên, ĐVT, nhóm).
type IndicatorInput struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Unit  string `json:"unit"`
	Group string `json:"group"`
}

// Normalize kiểm và chuẩn hóa chỉ tiêu; mã rỗng → sinh từ gen.
func (in IndicatorInput) Normalize(gen func() string) (IndicatorInput, error) {
	var errs FieldErrors
	in.Code, in.Name, in.Unit, in.Group = strings.ToLower(strings.TrimSpace(in.Code)), clean(in.Name), clean(in.Unit), clean(in.Group)
	if in.Code == "" && gen != nil {
		in.Code = gen()
	}
	if !codeRe.MatchString(in.Code) {
		errs.add("code", "mã chữ thường, số, . _ - (tối đa 40 ký tự)")
	}
	if in.Name == "" || badText(in.Name, 300) {
		errs.add("name", "bắt buộc, tối đa 300 ký tự")
	}
	if badText(in.Unit, 50) {
		errs.add("unit", "tối đa 50 ký tự")
	}
	if badText(in.Group, 200) {
		errs.add("group", "tối đa 200 ký tự")
	}
	if len(errs) > 0 {
		return in, errs
	}
	return in, nil
}

// ParseValue đọc giá trị số: "48215", "48215.5", "48.215,5" (kiểu Việt Nam), "26 400". Tối đa 4 chữ số thập phân.
// Không nhận dạng số mũ; tối đa MaxValueLen byte (sau khi bỏ khoảng trắng đầu/cuối), |giá trị| < 10^17.
func ParseValue(raw string) (decimal.Decimal, error) {
	if len(strings.TrimSpace(raw)) > MaxValueLen {
		return decimal.Zero, fmt.Errorf("tối đa %d ký tự", MaxValueLen)
	}
	s := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(raw), " ", ""), " ", "")
	if s == "" {
		return decimal.Zero, errors.New("bắt buộc")
	}
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.ReplaceAll(s, ",", ".")
	} else if strings.Count(s, ".") > 1 {
		s = strings.ReplaceAll(s, ".", "")
	}
	// Kiểm cú pháp và số chữ số trên chuỗi TRƯỚC mọi phép decimal (AUTO-REVIEW §4): không nhận số mũ (1e2, 1e100000).
	intDigits, fracDigits, ok := scanPlainValue(s)
	if !ok {
		return decimal.Zero, errors.New("không phải số")
	}
	if len(strings.TrimRight(fracDigits, "0")) > MaxValueFracDigits {
		return decimal.Zero, errors.New("tối đa 4 chữ số thập phân")
	}
	if len(strings.TrimLeft(intDigits, "0")) > MaxValueIntDigits {
		return decimal.Zero, errors.New("giá trị quá lớn")
	}
	// s đã ≤ MaxValueLen byte, chỉ gồm dấu, chữ số và một dấu chấm → dựng decimal rẻ, |s| < 10^17.
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, errors.New("không phải số")
	}
	return d, nil
}

// scanPlainValue kiểm s (đã chuẩn hóa dấu phân cách) có dạng [+-]chữ số[.chữ số] với ít nhất một chữ số, không số mũ,
// và trả phần nguyên, phần thập phân. Không đọc giá trị.
func scanPlainValue(s string) (intDigits, fracDigits string, ok bool) {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	intDigits = s[start:i]
	if i < len(s) && s[i] == '.' {
		i++
		fs := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		fracDigits = s[fs:i]
	}
	if i != len(s) || intDigits == "" && fracDigits == "" {
		return "", "", false
	}
	return intDigits, fracDigits, true
}

// ParseDate đọc ngày của nguồn: YYYY-MM-DD hoặc DD/MM/YYYY.
func ParseDate(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	for _, layout := range []string{time.DateOnly, "2/1/2006", "02/01/2006", "2-1-2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("ngày YYYY-MM-DD hoặc DD/MM/YYYY")
}

// Source là nguồn bắt buộc của số liệu nhập ngoài (BR-DL-19).
type Source struct {
	Agency   string    // cơ quan cung cấp
	Document string    // văn bản hoặc kênh
	Date     time.Time // ngày của văn bản / kênh
}

// FigureInput là một lần "Thêm số liệu" / "Sửa" (B2, B4).
type FigureInput struct {
	PeriodID    uuid.UUID   `json:"period_id"`
	IndicatorID uuid.UUID   `json:"indicator_id"`
	Value       string      `json:"value"`
	Unit        string      `json:"unit"`
	Agency      string      `json:"agency"`
	Document    string      `json:"document"`
	SourceDate  string      `json:"source_date"`
	Evidence    []uuid.UUID `json:"evidence_ids"`
	Reason      string      `json:"reason"`
}

// Figure là số liệu đã kiểm, sẵn sàng ghi sổ.
type Figure struct {
	PeriodID    uuid.UUID
	IndicatorID uuid.UUID
	Value       decimal.Decimal
	Unit        string
	Source      Source
	Evidence    []uuid.UUID
	Reason      string
}

// Validate kiểm một lần nhập. now: ngày hiện tại (ngày nguồn không được sau hôm nay).
func (in FigureInput) Validate(now time.Time) (Figure, error) {
	var errs FieldErrors
	f := Figure{PeriodID: in.PeriodID, IndicatorID: in.IndicatorID, Unit: clean(in.Unit), Reason: strings.TrimSpace(in.Reason)}
	if in.PeriodID == uuid.Nil {
		errs.add("period_id", "chọn kỳ")
	}
	if in.IndicatorID == uuid.Nil {
		errs.add("indicator_id", "chọn chỉ tiêu")
	}
	v, err := ParseValue(in.Value)
	if err != nil {
		errs.add("value", err.Error())
	}
	f.Value = v
	if badText(f.Unit, 50) {
		errs.add("unit", "tối đa 50 ký tự")
	}
	src, serrs := validSource(in.Agency, in.Document, in.SourceDate, now)
	errs = append(errs, serrs...)
	f.Source = src
	seen := map[uuid.UUID]bool{}
	for _, id := range in.Evidence {
		if id == uuid.Nil || seen[id] {
			errs.add("evidence_ids", "tệp minh chứng trùng hoặc rỗng")
			break
		}
		seen[id] = true
	}
	if len(in.Evidence) > MaxEvidence {
		errs.add("evidence_ids", fmt.Sprintf("tối đa %d tệp", MaxEvidence))
	}
	f.Evidence = append([]uuid.UUID{}, in.Evidence...)
	if f.Reason != "" && (utf8.RuneCountInString(f.Reason) < MinReason || badText(f.Reason, 1000)) {
		errs.add("reason", "lý do 10–1000 ký tự")
	}
	if len(errs) > 0 {
		return f, errs
	}
	return f, nil
}

func validSource(agency, document, date string, now time.Time) (Source, FieldErrors) {
	var errs FieldErrors
	s := Source{Agency: clean(agency), Document: clean(document)}
	if s.Agency == "" || badText(s.Agency, 300) {
		errs.add("agency", "thiếu nguồn: cơ quan cung cấp (tối đa 300 ký tự)")
	}
	if s.Document == "" || badText(s.Document, 300) {
		errs.add("document", "thiếu nguồn: văn bản hoặc kênh (tối đa 300 ký tự)")
	}
	if strings.TrimSpace(date) == "" {
		errs.add("source_date", "thiếu nguồn: ngày")
	} else if d, err := ParseDate(date); err != nil {
		errs.add("source_date", err.Error())
	} else if !now.IsZero() && d.After(now) {
		errs.add("source_date", "ngày nguồn sau hôm nay")
	} else {
		s.Date = d
	}
	return s, errs
}

// NeedsReason: ghi khi đã có giá trị hiệu lực (sửa, ghi đè) phải có lý do (BR-DL-19, "trùng chỉ tiêu và kỳ → hỏi ghi
// đè, lưu lịch sử").
func NeedsReason(hasCurrent bool, reason string) bool {
	return hasCurrent && utf8.RuneCountInString(strings.TrimSpace(reason)) < MinReason
}
