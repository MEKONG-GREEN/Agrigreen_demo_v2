// Package doctpl là ngôn ngữ mẫu văn bản của `cap/export` (W10.17, ADR-0051): mẫu có khóa + phiên bản, biến khai báo,
// khuôn câu có biến, mục, lặp theo danh sách, bảng theo danh sách, khối quốc hiệu/chữ ký hai cột. Render dựng
// docx.Document tất định (internal/shared/docx).
//
// Thư viện thuần (không DB, không đồng hồ): module pf/export giữ sổ phiên bản mẫu và sổ lần xuất; module nghiệp vụ dựng
// mẫu + dữ liệu của mình. Dữ liệu chỉ là chuỗi — bên gọi định dạng số thập phân chính xác và ngày dd/mm/yyyy (DateVN).
//
// Khuôn câu: `{bien}` là biến vô hướng; trong khối lặp/bảng theo danh sách `ds`, `{ds.truong}` là trường của phần tử
// hiện tại; `{{` và `}}` là dấu ngoặc nhọn. Mọi tham chiếu được kiểm khi Validate (mẫu sai không đăng ký được).
package doctpl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Loại khối.
const (
	KindParagraph = "paragraph"  // khuôn câu; mỗi dòng ("\n") thành một đoạn
	KindHeading   = "heading"    // tiêu đề mục (đậm, giữ cùng trang với đoạn sau)
	KindSection   = "section"    // mục: Title + Blocks; bỏ qua cả mục khi If rỗng
	KindLoop      = "loop"       // lặp Blocks cho mỗi phần tử của Each
	KindTable     = "table"      // bảng: một dòng cho mỗi phần tử của Each
	KindColumns   = "columns"    // hai cột không viền (quốc hiệu, nơi nhận/chữ ký)
	KindPageBreak = "page_break" // ngắt trang
)

// Căn lề.
const (
	AlignLeft    = ""
	AlignCenter  = "center"
	AlignRight   = "right"
	AlignJustify = "justify"
)

// Template là một phiên bản mẫu. Đổi nội dung = tăng Version (pf/export từ chối cùng khóa+phiên bản khác băm).
type Template struct {
	Key       string  `json:"key"`
	Version   int     `json:"version"`
	Title     string  `json:"title"`
	Owner     string  `json:"owner"` // module đăng ký, vd pf/extension
	Landscape bool    `json:"landscape,omitempty"`
	Vars      []Var   `json:"vars"`
	Lists     []List  `json:"lists"`
	Body      []Block `json:"body"`
}

// Var là biến vô hướng. Required: Render lỗi nếu dữ liệu không có hoặc rỗng.
type Var struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required,omitempty"`
}

// List là danh sách phần tử (bản ghi trường chuỗi) dùng cho lặp và bảng.
type List struct {
	Key    string   `json:"key"`
	Label  string   `json:"label"`
	Fields []string `json:"fields"`
}

// Block là một khối của mẫu.
type Block struct {
	Kind      string   `json:"kind"`
	Key       string   `json:"key,omitempty"`   // khóa mục (section)
	Text      string   `json:"text,omitempty"`  // khuôn câu (paragraph, heading) hoặc tiêu đề mục (section)
	Align     string   `json:"align,omitempty"` // paragraph, heading
	Bold      bool     `json:"bold,omitempty"`
	Italic    bool     `json:"italic,omitempty"`
	Indent    bool     `json:"indent,omitempty"` // thụt dòng đầu 1 cm
	If        string   `json:"if,omitempty"`     // chỉ dựng khi biến/danh sách/trường khác rỗng
	Each      string   `json:"each,omitempty"`   // loop, table
	Columns   []Column `json:"columns,omitempty"`
	Left      []Line   `json:"left,omitempty"`  // columns
	Right     []Line   `json:"right,omitempty"` // columns
	Split     int      `json:"split,omitempty"` // columns: độ rộng cột trái (twip), 0 = một nửa
	Blocks    []Block  `json:"blocks,omitempty"`
	EmptyText string   `json:"empty_text,omitempty"` // table: dòng khi danh sách rỗng
}

// Column là một cột bảng: tiêu đề + khuôn câu theo trường của phần tử.
type Column struct {
	Header string `json:"header"`
	Value  string `json:"value"`
	Width  int    `json:"width"` // twip (567 = 1 cm)
	Align  string `json:"align,omitempty"`
	// BoldIf: tên trường của phần tử; khác rỗng → ô đậm (dòng nhóm/chương trình).
	BoldIf string `json:"bold_if,omitempty"`
}

// Line là một dòng của khối hai cột.
type Line struct {
	Text   string `json:"text"`
	Bold   bool   `json:"bold,omitempty"`
	Italic bool   `json:"italic,omitempty"`
	If     string `json:"if,omitempty"`
}

// Item là một phần tử của danh sách.
type Item map[string]string

// Data là dữ liệu của một lần dựng.
type Data struct {
	Vars  map[string]string `json:"vars"`
	Lists map[string][]Item `json:"lists"`
}

var (
	// ErrTemplate: mẫu sai (khóa, phiên bản, tham chiếu biến, cấu trúc).
	ErrTemplate = errors.New("doctpl: mẫu không hợp lệ")
	// ErrData: dữ liệu thiếu biến bắt buộc hoặc vượt giới hạn.
	ErrData = errors.New("doctpl: dữ liệu không hợp lệ")
)

// Giới hạn.
const (
	MaxDepth      = 4
	MaxBlocks     = 2000
	MaxItems      = 20000
	MaxValueRunes = 20000
)

var (
	keyRe   = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z0-9_]+)*$`)
	identRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Hash là SHA-256 (hex) của định nghĩa mẫu dạng JSON chuẩn của struct — dấu phiên bản.
func (t Template) Hash() string { return hashJSON(t) }

// Hash là SHA-256 (hex) của dữ liệu (khóa map đã sắp khi mã hóa JSON) — dấu đầu vào của lần xuất.
func (d Data) Hash() string { return hashJSON(d) }

func hashJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err) // chỉ chuỗi, bool, số nguyên: không thể lỗi
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type scope struct {
	vars  map[string]bool
	lists map[string]List
	items []string // danh sách đang lặp (từ ngoài vào trong)
}

// Validate kiểm mẫu: khóa, phiên bản, biến/danh sách không trùng, mọi khuôn câu và If tham chiếu đúng phạm vi.
func Validate(t Template) error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s v%d: %s", ErrTemplate, t.Key, t.Version, fmt.Sprintf(format, a...))
	}
	if !keyRe.MatchString(t.Key) || len(t.Key) > 80 {
		return bad("khóa phải dạng a.b_c (chữ thường, số, _), tối đa 80 ký tự")
	}
	if t.Version < 1 {
		return bad("phiên bản bắt đầu từ 1")
	}
	if strings.TrimSpace(t.Title) == "" || utf8.RuneCountInString(t.Title) > 300 {
		return bad("tên mẫu 1–300 ký tự")
	}
	sc := scope{vars: map[string]bool{}, lists: map[string]List{}}
	for _, v := range t.Vars {
		if !identRe.MatchString(v.Key) || sc.vars[v.Key] {
			return bad("biến %q sai tên hoặc trùng", v.Key)
		}
		sc.vars[v.Key] = true
	}
	for _, l := range t.Lists {
		if !identRe.MatchString(l.Key) || sc.vars[l.Key] {
			return bad("danh sách %q sai tên hoặc trùng biến", l.Key)
		}
		if _, dup := sc.lists[l.Key]; dup {
			return bad("danh sách %q trùng", l.Key)
		}
		seen := map[string]bool{}
		for _, f := range l.Fields {
			if !identRe.MatchString(f) || seen[f] {
				return bad("trường %q của %q sai tên hoặc trùng", f, l.Key)
			}
			seen[f] = true
		}
		sc.lists[l.Key] = l
	}
	if len(t.Body) == 0 {
		return bad("mẫu rỗng")
	}
	n := 0
	return validateBlocks(t.Body, sc, 0, &n, bad)
}

func validateBlocks(blocks []Block, sc scope, depth int, n *int, bad func(string, ...any) error) error {
	if depth > MaxDepth {
		return bad("lồng quá %d mức", MaxDepth)
	}
	for _, b := range blocks {
		*n++
		if *n > MaxBlocks {
			return bad("quá %d khối", MaxBlocks)
		}
		if b.If != "" && !sc.resolvable(b.If, true) {
			return bad("If %q không có trong phạm vi", b.If)
		}
		if !validAlign(b.Align) {
			return bad("căn lề %q", b.Align)
		}
		check := func(pattern string) error {
			refs, err := references(pattern)
			if err != nil {
				return bad("khuôn câu %q: %v", pattern, err)
			}
			for _, r := range refs {
				if !sc.resolvable(r, false) {
					return bad("khuôn câu %q: biến {%s} không có trong phạm vi", pattern, r)
				}
			}
			return nil
		}
		switch b.Kind {
		case KindParagraph, KindHeading:
			if err := check(b.Text); err != nil {
				return err
			}
		case KindSection:
			if err := check(b.Text); err != nil {
				return err
			}
			if err := validateBlocks(b.Blocks, sc, depth+1, n, bad); err != nil {
				return err
			}
		case KindLoop, KindTable:
			if _, ok := sc.lists[b.Each]; !ok {
				return bad("%s theo danh sách %q chưa khai", b.Kind, b.Each)
			}
			for _, outer := range sc.items {
				if outer == b.Each {
					return bad("lặp lồng cùng danh sách %q", b.Each)
				}
			}
			inner := sc
			inner.items = append(append([]string{}, sc.items...), b.Each)
			if b.Kind == KindLoop {
				if err := validateBlocks(b.Blocks, inner, depth+1, n, bad); err != nil {
					return err
				}
				continue
			}
			if len(b.Columns) == 0 || len(b.Columns) > 30 {
				return bad("bảng %q cần 1–30 cột", b.Each)
			}
			for _, c := range b.Columns {
				if c.Width <= 0 || !validAlign(c.Align) {
					return bad("cột %q: độ rộng dương, căn lề hợp lệ", c.Header)
				}
				if err := check(c.Header); err != nil {
					return err
				}
				refs, err := references(c.Value)
				if err != nil {
					return bad("cột %q: %v", c.Header, err)
				}
				for _, r := range refs {
					if !inner.resolvable(r, false) {
						return bad("cột %q: biến {%s} không có trong phạm vi", c.Header, r)
					}
				}
				if c.BoldIf != "" && !inner.resolvable(b.Each+"."+c.BoldIf, false) {
					return bad("cột %q: bold_if %q", c.Header, c.BoldIf)
				}
			}
			if err := check(b.EmptyText); err != nil {
				return err
			}
		case KindColumns:
			for _, l := range append(append([]Line{}, b.Left...), b.Right...) {
				if err := check(l.Text); err != nil {
					return err
				}
				if l.If != "" && !sc.resolvable(l.If, true) {
					return bad("If %q không có trong phạm vi", l.If)
				}
			}
			if b.Split < 0 {
				return bad("split âm")
			}
		case KindPageBreak:
		default:
			return bad("loại khối %q", b.Kind)
		}
	}
	return nil
}

func validAlign(a string) bool {
	return a == AlignLeft || a == AlignCenter || a == AlignRight || a == AlignJustify
}

// resolvable: tên biến vô hướng, `ds.truong` của danh sách đang lặp, hoặc (khi listOK) tên danh sách (If).
func (s scope) resolvable(ref string, listOK bool) bool {
	if s.vars[ref] {
		return true
	}
	if _, ok := s.lists[ref]; ok && listOK {
		return true
	}
	list, field, ok := strings.Cut(ref, ".")
	if !ok {
		return false
	}
	for _, it := range s.items {
		if it == list {
			for _, f := range s.lists[list].Fields {
				if f == field {
					return true
				}
			}
		}
	}
	return false
}

// references trả các tham chiếu `{x}` của khuôn câu theo thứ tự.
func references(pattern string) ([]string, error) {
	var out []string
	_, err := expand(pattern, func(ref string) (string, bool) { out = append(out, ref); return "", true })
	return out, err
}

// expand thay `{x}` bằng giá trị; `{{`, `}}` là ngoặc nhọn.
func expand(pattern string, lookup func(string) (string, bool)) (string, error) {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '{' && i+1 < len(pattern) && pattern[i+1] == '{':
			b.WriteByte('{')
			i++
		case c == '}' && i+1 < len(pattern) && pattern[i+1] == '}':
			b.WriteByte('}')
			i++
		case c == '{':
			end := strings.IndexByte(pattern[i+1:], '}')
			if end < 0 {
				return "", fmt.Errorf("thiếu }")
			}
			ref := pattern[i+1 : i+1+end]
			if ref == "" || strings.ContainsAny(ref, "{ ") {
				return "", fmt.Errorf("tham chiếu {%s}", ref)
			}
			v, ok := lookup(ref)
			if !ok {
				return "", fmt.Errorf("{%s} không có giá trị", ref)
			}
			b.WriteString(v)
			i += end + 1
		case c == '}':
			return "", fmt.Errorf("} lẻ")
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

// DateVN định dạng ngày dd/mm/yyyy theo giờ Việt Nam (Asia/Ho_Chi_Minh).
func DateVN(t time.Time) string { return t.In(clock.Zone()).Format("02/01/2006") }

// PlaceDateLine là dòng địa danh, ngày tháng của văn bản hành chính: "An Giang, ngày 05 tháng 03 năm 2026" (giờ Việt Nam).
func PlaceDateLine(place string, t time.Time) string {
	v := t.In(clock.Zone())
	line := fmt.Sprintf("ngày %02d tháng %02d năm %d", v.Day(), int(v.Month()), v.Year())
	if strings.TrimSpace(place) == "" {
		return line
	}
	return strings.TrimSpace(place) + ", " + line
}
