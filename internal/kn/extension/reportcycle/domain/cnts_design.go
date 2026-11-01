package domain

// BF-CN-FORM-DESIGN: đọc cấu trúc Excel để xem trước, chưa ghi phiên bản.
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
		if len(r.Cells) > len(DesignHeaders) {
			issue(r.Line, "row", "Dòng có cột thừa")
		}
		if !designKey.MatchString(f.Key) || seen[f.Key] {
			issue(r.Line, "key", "Mã [a-z][a-z0-9_]*, tối đa 64 ký tự, không trùng")
		}
		seen[f.Key] = true
		if n := utf8.RuneCountInString(f.Label); n < 1 || n > 300 {
			issue(r.Line, "label", "Tên chỉ tiêu cần 1–300 ký tự")
		}
		if utf8.RuneCountInString(f.Unit) > 80 {
			issue(r.Line, "unit", "Đơn vị tối đa 80 ký tự")
		}
		if utf8.RuneCountInString(f.Group) > 200 {
			issue(r.Line, "group", "Nhóm tối đa 200 ký tự")
		}
		boolean := func(i int, name string) bool {
			v := cell(i)
			if v != "" && v != "true" && v != "false" {
				issue(r.Line, name, "Chỉ nhận true hoặc false (rỗng là false)")
			}
			return v == "true"
		}
		f.Required = boolean(3, "required")
		f.NonNegative = boolean(6, "non_negative")
		switch f.Type {
		case "text", "integer", "decimal", "date", "choice":
		default:
			issue(r.Line, "type", "text | integer | decimal | date | choice")
		}
		raw := cell(7)
		if len(raw) > 10000 {
			issue(r.Line, "options", "Danh sách lựa chọn quá dài")
		} else if raw != "" {
			for _, v := range strings.Split(raw, "|") {
				f.Options = append(f.Options, strings.TrimSpace(v))
			}
		}
		if f.Type == "choice" {
			if len(f.Options) < 1 || len(f.Options) > 100 {
				issue(r.Line, "options", "choice cần 1–100 lựa chọn, phân cách bằng |")
			}
			opts := map[string]bool{}
			for _, v := range f.Options {
				if v == "" || utf8.RuneCountInString(v) > 100 || opts[v] {
					issue(r.Line, "options", "Lựa chọn 1–100 ký tự, không rỗng/trùng")
					break
				}
				opts[v] = true
			}
		} else if raw != "" {
			issue(r.Line, "options", "Chỉ kiểu choice có lựa chọn")
		}
		if f.NonNegative && f.Type != "integer" && f.Type != "decimal" {
			issue(r.Line, "non_negative", "Chỉ áp dụng cho integer hoặc decimal")
		}
		out.Fields = append(out.Fields, f)
	}
	out.Valid = len(out.Issues) == 0
	if !out.Valid {
		out.Fields = []CNTSField{}
	}
	return out
}
