// Package domain: quy tắc thuần của mẫu biểu dự án khuyến nông quốc gia (KNQG) phòng Chăn nuôi – Thủy sản — W11.53,
// D4 CN13, BRD CNTS v3.7 FN-19, UR-TINH-20…24, UR-QG-01, Mục III.5 (vòng đời), quy tắc liên biểu dòng 9, 10, 11:
//   - Mẫu 01/TĐ-DA tiến độ tháng (BM-12): Đang lập → Chờ duyệt → Đã duyệt; Chờ duyệt → Trả lại (ý kiến); Đã duyệt → bản
//     điều chỉnh (Đang lập, phải trình duyệt lại). Mỗi dự án mỗi tháng một bản hiệu lực.
//   - Mẫu 03 kiểm tra, đánh giá (BM-13), Mẫu 04/DA-NTTH nghiệm thu lớp tập huấn (BM-14), Mẫu 05/DA-NTMH nghiệm thu điểm
//     trình diễn (BM-15): Đang lập → Chờ ký → Đã ký (tải bản scan); Đã ký → bản điều chỉnh (Đang lập, phải ký lại).
//   - Mẫu 07/NTKQ-DA nghiệm thu năm (BM-16): Đang lập → Đã gửi Quốc gia → Đã xác nhận (khóa hẳn); Đã gửi → Trả lại (ý kiến)
//     → sửa, gửi lại. Chỉ gửi khi MỌI điểm (chưa đóng) của dự án có Mẫu 05 Đã ký (bản điều chỉnh chưa ký → chặn).
//
// Nội dung một biểu là các ô (fields) và các bảng (tables) theo đặc tả FormSpec — đúng mục, bảng của mẫu Word gốc của TTKN
// Quốc gia (tệp "03/04/05/07 Mau … sửa đổi" của phòng; Mẫu 01 theo BRD UR-TINH-20). Cột Auto do máy chủ điền (thực tế từ
// nhật ký, tổng hợp từ Mẫu 05), giá trị máy khách gửi bị bỏ qua. Không phụ thuộc hạ tầng, không time.Now() (R-12).
package domain

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// RuleVersion là phiên bản quy tắc ghi vào sổ.
const RuleVersion = "cnts-knqg/1"

// Mã mẫu.
const (
	Form01 = "01"
	Form03 = "03"
	Form04 = "04"
	Form05 = "05"
	Form07 = "07"
)

// Forms là các mẫu theo thứ tự hiển thị.
var Forms = []string{Form01, Form03, Form04, Form05, Form07}

// Vòng đời.
const (
	LifecycleApprove  = "approve"  // Mẫu 01
	LifecycleSign     = "sign"     // Mẫu 03, 04, 05
	LifecycleNational = "national" // Mẫu 07
)

// Trạng thái.
const (
	StatusDraft        = "draft"         // Đang lập
	StatusSubmitted    = "submitted"     // Chờ duyệt (01)
	StatusApproved     = "approved"      // Đã duyệt (01)
	StatusReturned     = "returned"      // Trả lại (01, 07) — sửa được, trình/gửi lại
	StatusAwaitingSign = "awaiting_sign" // Chờ ký (03, 04, 05)
	StatusSigned       = "signed"        // Đã ký (03, 04, 05)
	StatusSent         = "sent"          // Đã gửi Quốc gia (07)
	StatusConfirmed    = "confirmed"     // Đã xác nhận (07) — khóa vĩnh viễn
)

// Sự việc của sổ.
const (
	EventCreated       = "created"
	EventUpdated       = "updated"
	EventSubmitted     = "submitted"
	EventApproved      = "approved"
	EventReturned      = "returned"
	EventSignRequested = "sign_requested"
	EventUnrequested   = "unrequested" // Chờ ký → Đang lập (sửa tiếp)
	EventSigned        = "signed"
	EventAdjusted      = "adjusted" // bản điều chỉnh (phiên bản biểu + 1)
	EventSent          = "sent"
	EventConfirmed     = "confirmed"
)

// Thao tác chuyển trạng thái (API /actions/{action}; ký là /signed có tệp).
const (
	ActSubmit      = "submit"
	ActApprove     = "approve"
	ActReturn      = "return"
	ActRequestSign = "request_sign"
	ActUnrequest   = "unrequest"
	ActSend        = "send"
	ActConfirm     = "confirm"
	ActAdjust      = "adjust"
	ActSign        = "sign"
)

// Giới hạn (lỗi #107/#109: mọi kích thước do người dùng điều khiển đều có trần).
const (
	MaxText      = 300
	MaxLong      = 4000
	MaxNumberLen = 22
	MaxReason    = 1000
	MinReason    = 5
	MaxContent   = 200 << 10 // byte JSON của nội dung một phiên bản
	MaxScanBytes = 10 << 20  // bản scan đã ký ≤ 10 MB
	MaxFileName  = 200
)

// Kiểu ô/cột.
const (
	KindText   = "text"
	KindLong   = "long"
	KindDate   = "date"
	KindNumber = "number" // số thập phân không âm (chuỗi, dấu chấm thập phân)
	KindInt    = "int"    // số nguyên không âm
	KindChoice = "choice"
)

// Field là đặc tả một ô.
type Field struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"`
	Options  []Option `json:"options,omitempty"`
	Required bool     `json:"required,omitempty"` // bắt buộc khi trình duyệt / trình ký / gửi
	Auto     bool     `json:"auto,omitempty"`     // máy chủ điền
	Hint     string   `json:"hint,omitempty"`
}

// Option là một lựa chọn của ô choice.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Column là đặc tả một cột bảng.
type Column struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Kind    string   `json:"kind"`
	Options []Option `json:"options,omitempty"`
	Auto    bool     `json:"auto,omitempty"`
}

// Table là đặc tả một bảng.
type Table struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Columns []Column `json:"columns"`
	MaxRows int      `json:"max_rows"`
	Auto    bool     `json:"auto,omitempty"` // cả bảng do máy chủ dựng (không sửa)
	Hint    string   `json:"hint,omitempty"`
}

// Section là một mục của biểu (nhóm ô, bảng theo bố cục mẫu Word).
type Section struct {
	Title  string   `json:"title"`
	Fields []string `json:"fields,omitempty"`
	Tables []string `json:"tables,omitempty"`
}

// Spec là đặc tả một mẫu.
type Spec struct {
	Form      string    `json:"form"`
	Code      string    `json:"code"` // ký hiệu mẫu trên văn bản, vd "Mẫu 05/DA-NTMH"
	BM        string    `json:"bm"`   // mã biểu BRD (BM-12…BM-16)
	Title     string    `json:"title"`
	Lifecycle string    `json:"lifecycle"`
	PerSite   bool      `json:"per_site"`           // Mẫu 05: một biên bản mỗi điểm
	Period    string    `json:"period,omitempty"`   // "month" (01), "year" (07), "" (khác)
	Unique    bool      `json:"unique"`             // một bản hiệu lực mỗi dự án × kỳ (01, 07) hoặc × điểm (05)
	Numbered  bool      `json:"numbered,omitempty"` // Word tự cấp số văn bản (Mẫu 03 — báo cáo)
	Fields    []Field   `json:"fields"`
	Tables    []Table   `json:"tables"`
	Sections  []Section `json:"sections"`
}

// FieldByKey trả đặc tả ô.
func (s Spec) FieldByKey(k string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Key == k {
			return f, true
		}
	}
	return Field{}, false
}

// TableByKey trả đặc tả bảng.
func (s Spec) TableByKey(k string) (Table, bool) {
	for _, t := range s.Tables {
		if t.Key == k {
			return t, true
		}
	}
	return Table{}, false
}

// Lookup trả đặc tả mẫu form.
func Lookup(form string) (Spec, bool) {
	s, ok := specs[form]
	return s, ok
}

// Specs trả mọi đặc tả theo thứ tự Forms.
func Specs() []Spec {
	out := make([]Spec, 0, len(Forms))
	for _, f := range Forms {
		out = append(out, specs[f])
	}
	return out
}

// ---------------------------------------------------------------------------
// Nội dung
// ---------------------------------------------------------------------------

// Row là một dòng bảng (cột → giá trị).
type Row map[string]string

// Content là nội dung một phiên bản biểu.
type Content struct {
	Fields map[string]string `json:"fields"`
	Tables map[string][]Row  `json:"tables"`
}

// Clone sao chép sâu.
func (c Content) Clone() Content {
	out := Content{Fields: make(map[string]string, len(c.Fields)), Tables: make(map[string][]Row, len(c.Tables))}
	for k, v := range c.Fields {
		out.Fields[k] = v
	}
	for k, rows := range c.Tables {
		cp := make([]Row, 0, len(rows))
		for _, r := range rows {
			x := make(Row, len(r))
			for a, b := range r {
				x[a] = b
			}
			cp = append(cp, x)
		}
		out.Tables[k] = cp
	}
	return out
}

// FieldErr là lỗi của một trường.
type FieldErr struct {
	Field  string
	Reason string
}

// Errors là danh sách lỗi (422).
type Errors []FieldErr

func (e Errors) Error() string {
	parts := make([]string, 0, len(e))
	for _, x := range e {
		parts = append(parts, x.Field+": "+x.Reason)
	}
	return strings.Join(parts, "; ")
}

func (e *Errors) add(field, reason string) { *e = append(*e, FieldErr{Field: field, Reason: reason}) }

var (
	dateRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	monthRe  = regexp.MustCompile(`^(\d{4})-(0[1-9]|1[0-2])$`)
	yearRe   = regexp.MustCompile(`^\d{4}$`)
	maxValue = decimal.New(1, 18)
)

// cell chuẩn hóa và kiểm một giá trị theo kiểu; trả giá trị đã chuẩn hóa và lý do lỗi ("" = hợp lệ).
func cell(kind string, opts []Option, v string) (string, string) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", ""
	}
	switch kind {
	case KindText:
		v = strings.Join(strings.Fields(v), " ")
		if utf8.RuneCountInString(v) > MaxText {
			return v, "tối đa " + strconv.Itoa(MaxText) + " ký tự"
		}
	case KindLong:
		v = strings.ReplaceAll(v, "\r\n", "\n")
		if utf8.RuneCountInString(v) > MaxLong {
			return v, "tối đa " + strconv.Itoa(MaxLong) + " ký tự"
		}
	case KindDate:
		if !dateRe.MatchString(v) {
			return v, "dạng YYYY-MM-DD"
		}
		if _, err := time.Parse(time.DateOnly, v); err != nil {
			return v, "ngày không hợp lệ"
		}
	case KindNumber, KindInt:
		if len(v) > MaxNumberLen {
			return v, "số quá dài"
		}
		d, err := decimal.NewFromString(v)
		if err != nil || d.IsNegative() || d.GreaterThanOrEqual(maxValue) {
			return v, "số không âm (dấu chấm thập phân)"
		}
		if kind == KindInt && !d.Equal(d.Truncate(0)) {
			return v, "số nguyên"
		}
		if d.Exponent() < -4 {
			return v, "tối đa 4 số lẻ"
		}
		v = d.String()
	case KindChoice:
		ok := false
		for _, o := range opts {
			if o.Value == v {
				ok = true
			}
		}
		if !ok {
			return v, "lựa chọn không hợp lệ"
		}
	}
	return v, ""
}

// Normalize kiểm và chuẩn hóa nội dung theo spec: khóa lạ bị từ chối, độ dài/kiểu đúng, số dòng ≤ trần, dòng rỗng bỏ.
// Cột/ô Auto bị xóa (máy chủ điền lại sau đó bằng Fill).
func (s Spec) Normalize(c *Content) Errors {
	var e Errors
	if c.Fields == nil {
		c.Fields = map[string]string{}
	}
	if c.Tables == nil {
		c.Tables = map[string][]Row{}
	}
	keys := make([]string, 0, len(c.Fields))
	for k := range c.Fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		f, ok := s.FieldByKey(k)
		if !ok {
			e.add("content.fields."+k, "ô không có trong mẫu")
			continue
		}
		if f.Auto {
			delete(c.Fields, k)
			continue
		}
		v, bad := cell(f.Kind, f.Options, c.Fields[k])
		if bad != "" {
			e.add("content.fields."+k, bad)
		}
		if v == "" {
			delete(c.Fields, k)
		} else {
			c.Fields[k] = v
		}
	}
	tkeys := make([]string, 0, len(c.Tables))
	for k := range c.Tables {
		tkeys = append(tkeys, k)
	}
	slices.Sort(tkeys)
	for _, k := range tkeys {
		t, ok := s.TableByKey(k)
		if !ok {
			e.add("content.tables."+k, "bảng không có trong mẫu")
			continue
		}
		if t.Auto {
			delete(c.Tables, k)
			continue
		}
		rows := c.Tables[k]
		if len(rows) > t.MaxRows {
			e.add("content.tables."+k, "tối đa "+strconv.Itoa(t.MaxRows)+" dòng")
			continue
		}
		out := make([]Row, 0, len(rows))
		for i, r := range rows {
			nr := Row{}
			for col, v := range r {
				cs, ok := colByKey(t, col)
				if !ok {
					e.add("content.tables."+k+"."+strconv.Itoa(i)+"."+col, "cột không có trong mẫu")
					continue
				}
				if cs.Auto {
					continue
				}
				nv, bad := cell(cs.Kind, cs.Options, v)
				if bad != "" {
					e.add("content.tables."+k+"."+strconv.Itoa(i)+"."+col, bad)
				}
				if nv != "" {
					nr[col] = nv
				}
			}
			// Dòng có mã (cột "ma" — dòng chuẩn của mẫu) giữ cả khi trống để máy chủ điền cột Auto.
			if len(nr) > 0 || r["ma"] != "" {
				if m := strings.TrimSpace(r["ma"]); m != "" {
					if _, ok := colByKey(t, "ma"); ok {
						nr["ma"] = m
					}
				}
				out = append(out, nr)
			}
		}
		c.Tables[k] = out
	}
	if b, _ := json.Marshal(c); len(b) > MaxContent {
		e.add("content", "nội dung quá lớn (tối đa 200 KB)")
	}
	return e
}

func colByKey(t Table, k string) (Column, bool) {
	for _, c := range t.Columns {
		if c.Key == k {
			return c, true
		}
	}
	return Column{}, false
}

// Texts trả mọi giá trị văn bản tự do (để chặn số điện thoại/CCCD — dữ liệu cá nhân phải ở kho định danh).
func (c Content) Texts() map[string]string {
	out := map[string]string{}
	for k, v := range c.Fields {
		out["content.fields."+k] = v
	}
	for k, rows := range c.Tables {
		for i, r := range rows {
			for col, v := range r {
				out["content.tables."+k+"."+strconv.Itoa(i)+"."+col] = v
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Kỳ
// ---------------------------------------------------------------------------

// NormalizePeriod kiểm kỳ theo mẫu: "YYYY-MM" (01), "YYYY" (07), rỗng (khác).
func (s Spec) NormalizePeriod(p string) (string, string) {
	p = strings.TrimSpace(p)
	switch s.Period {
	case "month":
		if !monthRe.MatchString(p) {
			return p, "tháng dạng YYYY-MM"
		}
	case "year":
		if !yearRe.MatchString(p) {
			return p, "năm dạng YYYY"
		}
	default:
		if p != "" {
			return p, "mẫu này không theo kỳ"
		}
	}
	return p, ""
}

// PeriodLabel trả nhãn kỳ ("tháng 10/2027", "năm 2027").
func PeriodLabel(p string) string {
	if m := monthRe.FindStringSubmatch(p); m != nil {
		return "tháng " + strings.TrimPrefix(m[2], "0") + "/" + m[1]
	}
	if yearRe.MatchString(p) {
		return "năm " + p
	}
	return ""
}

// ---------------------------------------------------------------------------
// Vòng đời
// ---------------------------------------------------------------------------

// Editable cho biết nội dung sửa được ở trạng thái status.
func Editable(status string) bool { return status == StatusDraft || status == StatusReturned }

// Step là kết quả một thao tác chuyển trạng thái.
type Step struct {
	Event    string
	Status   string
	Reason   bool // lý do/ý kiến bắt buộc
	Complete bool // kiểm đủ nội dung bắt buộc (Required, ghi chú chỉ tiêu) trước khi chuyển
	NewRev   bool // tạo bản điều chỉnh (phiên bản biểu + 1)
}

// Transition trả bước của thao tác act ở trạng thái status theo vòng đời lc (ok=false: không hợp lệ).
func Transition(lc, status, act string) (Step, bool) {
	switch lc {
	case LifecycleApprove:
		switch {
		case act == ActSubmit && Editable(status):
			return Step{Event: EventSubmitted, Status: StatusSubmitted, Complete: true}, true
		case act == ActApprove && status == StatusSubmitted:
			return Step{Event: EventApproved, Status: StatusApproved}, true
		case act == ActReturn && status == StatusSubmitted:
			return Step{Event: EventReturned, Status: StatusReturned, Reason: true}, true // FN-19: trả lại không ý kiến → chặn
		case act == ActAdjust && status == StatusApproved:
			return Step{Event: EventAdjusted, Status: StatusDraft, Reason: true, NewRev: true}, true
		}
	case LifecycleSign:
		switch {
		case act == ActRequestSign && Editable(status):
			return Step{Event: EventSignRequested, Status: StatusAwaitingSign, Complete: true}, true
		case act == ActUnrequest && status == StatusAwaitingSign:
			return Step{Event: EventUnrequested, Status: StatusDraft}, true
		case act == ActSign && status == StatusAwaitingSign:
			return Step{Event: EventSigned, Status: StatusSigned, Complete: true}, true
		case act == ActAdjust && status == StatusSigned:
			return Step{Event: EventAdjusted, Status: StatusDraft, Reason: true, NewRev: true}, true
		}
	case LifecycleNational:
		switch {
		case act == ActSend && Editable(status):
			return Step{Event: EventSent, Status: StatusSent, Complete: true}, true
		case act == ActConfirm && status == StatusSent:
			return Step{Event: EventConfirmed, Status: StatusConfirmed}, true
		case act == ActReturn && status == StatusSent:
			return Step{Event: EventReturned, Status: StatusReturned, Reason: true}, true
		}
	}
	return Step{}, false
}

// Actions trả các thao tác hợp lệ ở trạng thái (không xét quyền).
func Actions(lc, status string) []string {
	var out []string
	for _, a := range []string{ActSubmit, ActApprove, ActReturn, ActRequestSign, ActUnrequest, ActSign, ActSend, ActConfirm, ActAdjust} {
		if _, ok := Transition(lc, status, a); ok {
			out = append(out, a)
		}
	}
	return out
}

// ValidReason kiểm lý do/ý kiến.
func ValidReason(s string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= MinReason && n <= MaxReason
}

// ---------------------------------------------------------------------------
// Kiểm đủ trước khi trình/ký/gửi, cảnh báo
// ---------------------------------------------------------------------------

// Complete kiểm nội dung trước khi trình duyệt/trình ký/ký/gửi: ô Required có giá trị; Mẫu 05 — chỉ tiêu "thực tế" khác số
// lấy từ nhật ký phải có ghi chú (quy tắc liên biểu 9: "sửa có ghi chú"), chỉ tiêu Không đạt phải có ghi chú (UR-TINH-23).
func (s Spec) Complete(c Content) Errors {
	var e Errors
	for _, f := range s.Fields {
		if f.Required && strings.TrimSpace(c.Fields[f.Key]) == "" {
			e.add("content.fields."+f.Key, "bắt buộc ("+f.Label+")")
		}
	}
	if s.Form == Form05 {
		for i, r := range c.Tables["chi_tieu"] {
			name := r["chi_tieu"]
			if name == "" {
				name = "dòng " + strconv.Itoa(i+1)
			}
			auto, act := strings.TrimSpace(r["thuc_te_nhat_ky"]), strings.TrimSpace(r["thuc_te"])
			if auto != "" && act != "" && act != auto && strings.TrimSpace(r["ghi_chu"]) == "" {
				e.add("content.tables.chi_tieu."+strconv.Itoa(i)+".ghi_chu", "\""+name+"\": thực tế khác số từ nhật ký ("+auto+") — ghi chú lý do")
			}
			if r["dat"] == "khong" && strings.TrimSpace(r["ghi_chu"]) == "" {
				e.add("content.tables.chi_tieu."+strconv.Itoa(i)+".ghi_chu", "\""+name+"\": chỉ tiêu không đạt bắt buộc ghi chú")
			}
		}
	}
	return e
}

// Warnings trả cảnh báo không chặn: Mẫu 01 — vật tư đã cấp vượt kế hoạch (quy tắc liên biểu 11).
func (s Spec) Warnings(c Content) []string {
	var out []string
	if s.Form == Form01 {
		for _, r := range c.Tables["vat_tu"] {
			plan, err1 := decimal.NewFromString(r["ke_hoach"])
			done, err2 := decimal.NewFromString(r["da_cap"])
			if err1 == nil && err2 == nil && done.GreaterThan(plan) {
				name := r["ten"]
				if name == "" {
					name = "(chưa đặt tên)"
				}
				out = append(out, "Vật tư "+name+": đã cấp "+done.String()+" vượt kế hoạch "+plan.String()+" (quy tắc 11)")
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Tệp scan đã ký
// ---------------------------------------------------------------------------

var scanFormats = map[string]struct {
	ct    string
	magic []byte
}{
	".pdf":  {"application/pdf", []byte("%PDF-")},
	".jpg":  {"image/jpeg", []byte{0xFF, 0xD8, 0xFF}},
	".jpeg": {"image/jpeg", []byte{0xFF, 0xD8, 0xFF}},
	".png":  {"image/png", []byte("\x89PNG\r\n\x1a\n")},
}

// ScanExtensions là các đuôi tệp scan nhận được.
var ScanExtensions = []string{".pdf", ".jpg", ".jpeg", ".png"}

// ScanType kiểm tên và chữ ký đầu tệp (không tin Content-Type máy khách); trả kiểu nội dung và tên đã làm sạch.
func ScanType(fileName string, head []byte) (ct, name string, ok bool) {
	name = strings.ReplaceAll(fileName, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" || name == "." {
		return "", "", false
	}
	if utf8.RuneCountInString(name) > MaxFileName {
		name = string([]rune(name)[utf8.RuneCountInString(name)-MaxFileName:])
	}
	ext := strings.ToLower(name)
	if i := strings.LastIndex(ext, "."); i >= 0 {
		ext = ext[i:]
	} else {
		ext = ""
	}
	f, found := scanFormats[ext]
	if !found || len(head) < len(f.magic) || string(head[:len(f.magic)]) != string(f.magic) {
		return "", name, false
	}
	return f.ct, name, true
}
