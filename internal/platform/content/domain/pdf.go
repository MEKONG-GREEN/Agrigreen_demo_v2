package domain

import (
	"bytes"
	"errors"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"golang.org/x/text/unicode/norm"
)

// Trích chữ từ PDF có lớp chữ (US-LIB-03). Dùng github.com/ledongthuc/pdf
// (BSD-3) để đọc cấu trúc, phông và bảng ToUnicode; tự duyệt luồng nội dung để
// ghép dòng theo toạ độ dọc, tách đoạn theo khoảng cách dòng và nhận tiêu đề
// theo cỡ chữ. PDF ảnh scan (không có lớp chữ), PDF mã hóa và PDF có phông
// không giải mã được trả ErrExtractUnsupported kèm lý do rõ ràng.

// ExtractError là lỗi trích có lý do cho người dùng; Unwrap trả ErrExtractUnsupported/ErrExtractFailed.
type ExtractError struct {
	Kind error
	Msg  string
}

func (e *ExtractError) Error() string { return e.Kind.Error() + ": " + e.Msg }
func (e *ExtractError) Unwrap() error { return e.Kind }

// Lý do không trích được PDF.
var (
	ErrPDFEncrypted = &ExtractError{ErrExtractUnsupported, "Tệp PDF được mã hóa/đặt mật khẩu — hãy gỡ bảo vệ hoặc soạn thủ công"}
	ErrPDFNoText    = &ExtractError{ErrExtractUnsupported, "Tệp PDF không có lớp chữ (ảnh scan) — hệ thống không nhận dạng chữ (OCR), vui lòng soạn thủ công"}
	ErrPDFGarbled   = &ExtractError{ErrExtractUnsupported, "Không giải mã được phông chữ của tệp PDF (thiếu bảng ToUnicode) — vui lòng soạn thủ công hoặc xuất lại PDF từ Word"}
)

// Giới hạn khi trích PDF.
const maxPDFPages = 300

type pdfRun struct {
	page    int
	y, size float64
	text    string
	moved   bool // có lệnh định vị (Td/Tm/T*) kể từ lần hiện chữ trước
}

// ExtractPDF trích chữ của PDF thành Markdown: mỗi đoạn một khối, dòng cỡ chữ
// lớn thành tiêu đề "#"/"##".
func ExtractPDF(b []byte) (x Extracted, err error) {
	defer func() {
		if r := recover(); r != nil { // thư viện dùng panic cho PDF hỏng
			x, err = Extracted{}, ErrExtractFailed
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		if errors.Is(err, pdf.ErrInvalidPassword) || strings.Contains(strings.ToLower(err.Error()), "encrypt") {
			return Extracted{}, ErrPDFEncrypted
		}
		return Extracted{}, ErrExtractFailed
	}
	if !r.Trailer().Key("Encrypt").IsNull() {
		return Extracted{}, ErrPDFEncrypted
	}
	n := r.NumPage()
	if n <= 0 {
		return Extracted{}, ErrExtractFailed
	}
	var runs []pdfRun
	budget := MaxBodyBytes * 2
	for i := 1; i <= min(n, maxPDFPages) && budget > 0; i++ {
		p := r.Page(i)
		if p.V.IsNull() || p.V.Key("Contents").IsNull() {
			continue
		}
		pr := pageRuns(p, i)
		for _, run := range pr {
			budget -= len(run.text)
		}
		runs = append(runs, pr...)
	}
	return assemblePDF(runs)
}

func pageRuns(p pdf.Page, page int) []pdfRun {
	fonts := map[string]pdf.TextEncoding{}
	var enc pdf.TextEncoding
	var out []pdfRun
	var y, scale, fontSize, leading float64 = 0, 1, 0, 0
	moved := true
	emit := func(s string) {
		if enc != nil {
			s = enc.Decode(s)
		}
		if s == "" {
			return
		}
		out = append(out, pdfRun{page: page, y: y, size: math.Abs(fontSize * scale), text: s, moved: moved})
		moved = false
	}
	pdf.Interpret(p.V.Key("Contents"), func(stk *pdf.Stack, op string) {
		args := make([]pdf.Value, stk.Len())
		for i := len(args) - 1; i >= 0; i-- {
			args[i] = stk.Pop()
		}
		switch op {
		case "BT":
			y, scale, moved = 0, 1, true
		case "Tf":
			if len(args) == 2 {
				name := args[0].Name()
				e, ok := fonts[name]
				if !ok {
					e = fontEncoder(p.Font(name))
					fonts[name] = e
				}
				enc, fontSize = e, args[1].Float64()
			}
		case "Tm":
			if len(args) == 6 {
				scale = math.Hypot(args[2].Float64(), args[3].Float64())
				if scale == 0 {
					scale = 1
				}
				y, moved = args[5].Float64(), true
			}
		case "Td", "TD":
			if len(args) == 2 {
				ty := args[1].Float64()
				if op == "TD" {
					leading = -ty
				}
				if ty != 0 {
					y += ty * scale
				}
				moved = true
			}
		case "TL":
			if len(args) == 1 {
				leading = args[0].Float64()
			}
		case "T*":
			y -= leading * scale
			moved = true
		case "Tj":
			if len(args) == 1 {
				emit(args[0].RawString())
			}
		case "'", "\"":
			if len(args) >= 1 {
				y -= leading * scale
				moved = true
				emit(args[len(args)-1].RawString())
			}
		case "TJ":
			if len(args) != 1 {
				return
			}
			var sb strings.Builder
			for i := 0; i < args[0].Len(); i++ {
				v := args[0].Index(i)
				switch v.Kind() {
				case pdf.String:
					s := v.RawString()
					if enc != nil {
						s = enc.Decode(s)
					}
					sb.WriteString(s)
				case pdf.Integer, pdf.Real:
					if v.Float64() < -250 { // khoảng cách lớn giữa hai nhóm chữ = dấu cách
						sb.WriteByte(' ')
					}
				}
			}
			saved := enc
			enc = nil
			emit(sb.String())
			enc = saved
		}
	})
	return out
}

// assemblePDF ghép các đoạn chữ thành dòng, dòng thành đoạn.
func assemblePDF(runs []pdfRun) (Extracted, error) {
	type line struct {
		page    int
		y, size float64
		text    strings.Builder
	}
	var lines []*line
	for _, r := range runs {
		t := cleanPDFText(r.text)
		if t == "" {
			continue
		}
		var cur *line
		if len(lines) > 0 {
			cur = lines[len(lines)-1]
		}
		sz := math.Max(r.size, 1)
		if cur == nil || cur.page != r.page || math.Abs(cur.y-r.y) > 0.5*math.Max(sz, cur.size) {
			cur = &line{page: r.page, y: r.y, size: sz}
			lines = append(lines, cur)
		} else if r.moved {
			s := cur.text.String()
			if !strings.HasSuffix(s, " ") && !strings.HasPrefix(t, " ") {
				cur.text.WriteByte(' ')
			}
		}
		cur.size = math.Max(cur.size, sz)
		cur.text.WriteString(t)
	}
	if len(lines) == 0 {
		return Extracted{}, ErrPDFNoText
	}

	// Cỡ chữ thân bài = cỡ phổ biến nhất (theo số ký tự).
	weight := map[float64]int{}
	total, bad := 0, 0
	for _, l := range lines {
		s := l.text.String()
		weight[math.Round(l.size*2)/2] += utf8.RuneCountInString(s)
		for _, c := range s {
			total++
			if c == utf8.RuneError || unicode.IsControl(c) || unicode.Is(unicode.Co, c) {
				bad++
			}
		}
	}
	if total == 0 || strings.TrimSpace(joinLines(lines, func(l *line) string { return l.text.String() })) == "" {
		return Extracted{}, ErrPDFNoText
	}
	if bad*5 > total {
		return Extracted{}, ErrPDFGarbled
	}
	sizes := make([]float64, 0, len(weight))
	for s := range weight {
		sizes = append(sizes, s)
	}
	sort.Slice(sizes, func(i, j int) bool {
		if weight[sizes[i]] != weight[sizes[j]] {
			return weight[sizes[i]] > weight[sizes[j]]
		}
		return sizes[i] < sizes[j]
	})
	body := sizes[0]

	var paras []string
	var title, first string
	var cur strings.Builder
	curLevel := 0
	flush := func() {
		text := strings.Join(strings.Fields(cur.String()), " ")
		cur.Reset()
		if text == "" {
			return
		}
		text = escapeMarkdownLine(text)
		prefix := ""
		switch curLevel {
		case 1:
			prefix = "# "
		case 2:
			prefix = "## "
		}
		if title == "" && curLevel > 0 {
			title = text
		}
		if first == "" {
			first = text
		}
		paras = append(paras, prefix+text)
	}
	level := func(size float64) int {
		switch {
		case size >= body*1.6:
			return 1
		case size >= body*1.25:
			return 2
		}
		return 0
	}
	for i, l := range lines {
		lv := level(l.size)
		if i > 0 {
			prev := lines[i-1]
			gap := prev.y - l.y
			newPara := l.page != prev.page || lv != curLevel || gap < 0 || gap > 1.6*math.Max(l.size, prev.size)
			if newPara {
				flush()
			} else {
				cur.WriteByte(' ')
			}
		}
		curLevel = lv
		cur.WriteString(l.text.String())
	}
	flush()
	if len(paras) == 0 {
		return Extracted{}, ErrPDFNoText
	}
	if title == "" {
		title = first
	}
	if utf8.RuneCountInString(title) > MaxTitle {
		title = string([]rune(title)[:MaxTitle])
	}
	out := strings.Join(paras, "\n\n")
	if len(out) > MaxBodyBytes {
		out = out[:MaxBodyBytes]
		for !utf8.ValidString(out) {
			out = out[:len(out)-1]
		}
	}
	return Extracted{Title: title, Body: out, BodyFormat: FormatMarkdown}, nil
}

func joinLines[T any](ls []T, f func(T) string) string {
	var b strings.Builder
	for _, l := range ls {
		b.WriteString(f(l))
	}
	return b.String()
}

// cleanPDFText chuẩn hóa NFC (dấu tiếng Việt tổ hợp → dựng sẵn), bỏ ký tự NUL.
func cleanPDFText(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || r == ' ' {
			return ' '
		}
		return r
	}, s)
	return norm.NFC.String(s)
}
