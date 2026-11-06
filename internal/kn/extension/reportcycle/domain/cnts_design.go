package domain

// BF-CN-FORM-DESIGN (#174): đọc cấu trúc Excel để xem trước. BF-CN-FORM-DESIGN-EDIT (#192, BRD CNTS v4.0 FN-07,
// UR-TINH-07): khóa mẫu KNQG (BR-02), biểu điều tra mã mới, kiểm trường và quy tắc "mẫu đã có phiên bản chỉ thêm cột".
import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const MaxDesignFields = 500

var DesignHeaders = []string{"key", "label", "type", "required", "unit", "group", "non_negative", "options"}
var designKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type DesignIssue struct {
	Line    int    `json:"line"`
	Field   string `json:"field"`
	Message string `json:"message"`
}
type DesignImport struct {
	Fields []CNTSField   `json:"fields"`
	Issues []DesignIssue `json:"issues"`
	Valid  bool          `json:"valid"`
}

// ParseDesign trả mọi lỗi theo dòng; có lỗi thì không trả cấu trúc có thể dùng nhầm như bản hợp lệ.
func ParseDesign(tb *tabular.Table) DesignImport {
	out := DesignImport{Fields: []CNTSField{}, Issues: []DesignIssue{}}
	issue := func(line int, field, msg string) { out.Issues = append(out.Issues, DesignIssue{line, field, msg}) }
	if !slices.Equal(tb.Keys, DesignHeaders) {
		issue(1, "header", "Cần đúng thứ tự cột: "+strings.Join(DesignHeaders, ", "))
		return out
	}
	if len(tb.Rows) == 0 || len(tb.Rows) > MaxDesignFields {
		issue(1, "rows", "Cần 1–500 dòng chỉ tiêu")
		return out
	}
	seen := map[string]bool{}
	for _, r := range tb.Rows {
		cell := func(i int) string {
			if i >= len(r.Cells) {
				return ""
			}
			return strings.TrimSpace(r.Cells[i])
		}
		f := CNTSField{Key: cell(0), Label: cell(1), Type: cell(2), Unit: cell(4), Group: cell(5)}
		line := r.Line
		if len(r.Cells) > len(DesignHeaders) {
			issue(line, "row", "Dòng có cột thừa")
		}
		boolean := func(i int, name string) bool {
			v := cell(i)
			if v != "" && v != "true" && v != "false" {
				issue(line, name, "Chỉ nhận true hoặc false (rỗng là false)")
			}
			return v == "true"
		}
		f.Required = boolean(3, "required")
		f.NonNegative = boolean(6, "non_negative")
		raw := cell(7)
		if len(raw) > 10000 {
			issue(line, "options", "Danh sách lựa chọn quá dài")
		} else if raw != "" {
			for _, v := range strings.Split(raw, "|") {
				f.Options = append(f.Options, strings.TrimSpace(v))
			}
		}
		checkField(f, seen, func(field, msg string) { issue(line, field, msg) })
		out.Fields = append(out.Fields, f)
	}
	out.Valid = len(out.Issues) == 0
	if !out.Valid {
		out.Fields = []CNTSField{}
	}
	return out
}

// checkField kiểm một chỉ tiêu (dùng chung cho tệp Excel và lưu phiên bản từ màn thiết kế).
func checkField(f CNTSField, seen map[string]bool, issue func(field, msg string)) {
	if !designKey.MatchString(f.Key) || seen[f.Key] {
		issue("key", "Mã [a-z][a-z0-9_]*, tối đa 64 ký tự, không trùng")
	}
	seen[f.Key] = true
	if n := utf8.RuneCountInString(f.Label); n < 1 || n > 300 {
		issue("label", "Tên chỉ tiêu cần 1–300 ký tự")
	}
	if utf8.RuneCountInString(f.Unit) > 80 {
		issue("unit", "Đơn vị tối đa 80 ký tự")
	}
	if utf8.RuneCountInString(f.Group) > 200 {
		issue("group", "Nhóm tối đa 200 ký tự")
	}
	switch f.Type {
	case "text", "integer", "decimal", "date", "choice":
	default:
		issue("type", "text | integer | decimal | date | choice")
	}
	if f.Type == "choice" {
		if len(f.Options) < 1 || len(f.Options) > 100 {
			issue("options", "choice cần 1–100 lựa chọn, phân cách bằng |")
		}
		opts := map[string]bool{}
		for _, v := range f.Options {
			if v == "" || utf8.RuneCountInString(v) > 100 || opts[v] {
				issue("options", "Lựa chọn 1–100 ký tự, không rỗng/trùng")
				break
			}
			opts[v] = true
		}
	} else if len(f.Options) > 0 {
		issue("options", "Chỉ kiểu choice có lựa chọn")
	}
	if f.NonNegative && f.Type != "integer" && f.Type != "decimal" {
		issue("non_negative", "Chỉ áp dụng cho integer hoặc decimal")
	}
}

// ValidateDesignFields kiểm cấu trúc lưu từ màn thiết kế (1–500 chỉ tiêu). Line là số thứ tự chỉ tiêu (từ 1).
func ValidateDesignFields(fs []CNTSField) []DesignIssue {
	if len(fs) == 0 || len(fs) > MaxDesignFields {
		return []DesignIssue{{Line: 0, Field: "fields", Message: "Cần 1–500 chỉ tiêu"}}
	}
	out := []DesignIssue{}
	seen := map[string]bool{}
	for i, f := range fs {
		checkField(f, seen, func(field, msg string) { out = append(out, DesignIssue{Line: i + 1, Field: field, Message: msg}) })
	}
	return out
}

// CNTSLockedCodes là mẫu cố định theo TTKN Quốc gia (UR-TINH-07 "Mẫu 01, 03, 04, 05, 07 cố định"; FN-07 BR-02 "Mẫu KNQG
// không sửa cấu trúc") trong số biểu kỳ cap/forms của phòng: BM-01, BM-04, hai phần BM-05. BM-03 (lịch trực) và BM-07
// (mẫu Word KNQG) không phải biểu cap/forms.
var CNTSLockedCodes = []string{CNTSBM01, CNTSBM04, CNTSBM05I, CNTSBM05II}

// CNTSLocked cho biết mã biểu bị khóa cấu trúc.
func CNTSLocked(code string) bool { return slices.Contains(CNTSLockedCodes, code) }

// SurveyCodePrefix là tiền tố mã biểu điều tra do Cấp Tỉnh tạo/sao chép (FN-07 luồng phụ, FN-21 "biểu điều tra thiết kế tại
// FN-07"). Mã do máy chủ ghép: tiền tố + hậu tố người dùng nhập.
const SurveyCodePrefix = "CNTS-DT-"

var surveySuffix = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{0,39}$`)

// SurveyCode ghép mã biểu điều tra từ hậu tố (chữ hoa, số, gạch ngang; tối đa 40 ký tự).
func SurveyCode(suffix string) (string, bool) {
	suffix = strings.ToUpper(strings.TrimSpace(suffix))
	if !surveySuffix.MatchString(suffix) {
		return "", false
	}
	return SurveyCodePrefix + suffix, true
}

// IsSurveyCode cho biết mã là biểu điều tra.
func IsSurveyCode(code string) bool {
	s, ok := strings.CutPrefix(code, SurveyCodePrefix)
	return ok && surveySuffix.MatchString(s)
}

// ProvinceDesignable: Cấp Tỉnh (cán bộ tổng hợp) được lưu phiên bản cho biểu kỳ không khóa (BM-17) và biểu điều tra.
func ProvinceDesignable(code string) bool { return code == CNTSBM17 || IsSurveyCode(code) }

// DesignChange kiểm quy tắc UR-TINH-07 "Mẫu đang có dữ liệu chỉ thêm cột, không xóa": mọi chỉ tiêu của phiên bản hiện hành
// phải còn, cùng kiểu. Được đổi tên, đơn vị, nhóm, bắt buộc, lựa chọn; thêm chỉ tiêu mới.
func DesignChange(cur, next []CNTSField) []DesignIssue {
	kinds := make(map[string]string, len(next))
	for _, f := range next {
		kinds[f.Key] = f.Type
	}
	out := []DesignIssue{}
	for _, f := range cur {
		t, ok := kinds[f.Key]
		switch {
		case !ok:
			out = append(out, DesignIssue{Field: f.Key, Message: "Không xóa chỉ tiêu của mẫu đã phát hành; chỉ được thêm chỉ tiêu mới"})
		case t != f.Type:
			out = append(out, DesignIssue{Field: f.Key, Message: "Không đổi kiểu dữ liệu của chỉ tiêu đã phát hành"})
		}
	}
	return out
}
