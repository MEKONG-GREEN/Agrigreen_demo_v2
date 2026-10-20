package doctpl

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
)

// Cỡ chữ trong bảng (nửa điểm): 12pt.
const sizeTable = 24

// Twip của vùng chữ trang A4 (lề trái 3 cm, phải 1,5 cm): dọc 9354, ngang 14286.
const (
	widthPortrait  = 9354
	widthLandscape = 14286
)

type renderer struct {
	t     Template
	d     Data
	cur   map[string]Item // danh sách đang lặp → phần tử hiện tại
	out   []docx.Block
	items int
}

// Render dựng tài liệu từ mẫu (đã Validate) và dữ liệu. Thiếu biến bắt buộc → ErrData.
func Render(t Template, d Data) (docx.Document, error) {
	if err := Validate(t); err != nil {
		return docx.Document{}, err
	}
	for _, v := range t.Vars {
		if v.Required && strings.TrimSpace(d.Vars[v.Key]) == "" {
			return docx.Document{}, fmt.Errorf("%w: thiếu %s (%s)", ErrData, v.Key, v.Label)
		}
	}
	for k, v := range d.Vars {
		if utf8.RuneCountInString(v) > MaxValueRunes {
			return docx.Document{}, fmt.Errorf("%w: %s quá %d ký tự", ErrData, k, MaxValueRunes)
		}
	}
	total := 0
	for k, items := range d.Lists {
		total += len(items)
		for _, it := range items {
			for f, v := range it {
				if utf8.RuneCountInString(v) > MaxValueRunes {
					return docx.Document{}, fmt.Errorf("%w: %s.%s quá %d ký tự", ErrData, k, f, MaxValueRunes)
				}
			}
		}
	}
	if total > MaxItems {
		return docx.Document{}, fmt.Errorf("%w: quá %d phần tử", ErrData, MaxItems)
	}
	r := &renderer{t: t, d: d, cur: map[string]Item{}}
	if err := r.blocks(t.Body); err != nil {
		return docx.Document{}, err
	}
	return docx.Document{Blocks: r.out, Landscape: t.Landscape}, nil
}

// RenderDOCX = Render + docx.Write.
func RenderDOCX(t Template, d Data) ([]byte, error) {
	doc, err := Render(t, d)
	if err != nil {
		return nil, err
	}
	return docx.Write(doc)
}

func (r *renderer) lookup(ref string) (string, bool) {
	if list, field, ok := strings.Cut(ref, "."); ok {
		if it, ok := r.cur[list]; ok {
			return it[field], true
		}
		return "", false
	}
	return r.d.Vars[ref], true
}

func (r *renderer) present(ref string) bool {
	if items, ok := r.d.Lists[ref]; ok && !strings.Contains(ref, ".") {
		if _, declared := r.listSpec(ref); declared {
			return len(items) > 0
		}
	}
	v, _ := r.lookup(ref)
	return strings.TrimSpace(v) != ""
}

func (r *renderer) listSpec(key string) (List, bool) {
	for _, l := range r.t.Lists {
		if l.Key == key {
			return l, true
		}
	}
	return List{}, false
}

func (r *renderer) text(pattern string) (string, error) {
	return expand(pattern, r.lookup)
}

func align(a string) docx.Align {
	switch a {
	case AlignCenter:
		return docx.AlignCenter
	case AlignRight:
		return docx.AlignRight
	case AlignJustify:
		return docx.AlignJustify
	}
	return docx.AlignLeft
}

// paragraphs tách theo dòng; dòng trống giữa văn bản bị bỏ (đoạn rỗng không có nghĩa trong văn bản hành chính).
func paragraphs(s string, a docx.Align, bold, italic, indent bool, size int) []docx.Paragraph {
	var out []docx.Paragraph
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		p := docx.Paragraph{Runs: []docx.Run{{Text: strings.TrimRight(line, " \t"), Bold: bold, Italic: italic, Size: size}}, Align: a}
		if indent {
			p.FirstLine = 567
		}
		out = append(out, p)
	}
	return out
}

func (r *renderer) blocks(blocks []Block) error {
	for _, b := range blocks {
		if b.If != "" && !r.present(b.If) {
			continue
		}
		if err := r.block(b); err != nil {
			return err
		}
	}
	return nil
}

func (r *renderer) block(b Block) error {
	switch b.Kind {
	case KindParagraph:
		s, err := r.text(b.Text)
		if err != nil {
			return err
		}
		for _, p := range paragraphs(s, align(b.Align), b.Bold, b.Italic, b.Indent, 0) {
			r.out = append(r.out, p)
		}
	case KindHeading:
		s, err := r.text(b.Text)
		if err != nil {
			return err
		}
		for _, p := range paragraphs(s, align(b.Align), true, b.Italic, b.Indent, 0) {
			p.KeepNext = true
			r.out = append(r.out, p)
		}
	case KindSection:
		s, err := r.text(b.Text)
		if err != nil {
			return err
		}
		for _, p := range paragraphs(s, docx.AlignLeft, true, false, false, 0) {
			p.KeepNext = true
			r.out = append(r.out, p)
		}
		return r.blocks(b.Blocks)
	case KindLoop:
		for _, it := range r.d.Lists[b.Each] {
			r.cur[b.Each] = it
			if err := r.blocks(b.Blocks); err != nil {
				return err
			}
		}
		delete(r.cur, b.Each)
	case KindTable:
		return r.table(b)
	case KindColumns:
		return r.columns(b)
	case KindPageBreak:
		r.out = append(r.out, docx.PageBreak{})
	}
	return nil
}

func (r *renderer) table(b Block) error {
	widths := make([]int, len(b.Columns))
	header := docx.Row{}
	for i, c := range b.Columns {
		widths[i] = c.Width
		h, err := r.text(c.Header)
		if err != nil {
			return err
		}
		header.Cells = append(header.Cells, docx.Cell{Paragraphs: []docx.Paragraph{{Runs: []docx.Run{{Text: h, Bold: true, Size: sizeTable}}, Align: docx.AlignCenter}}})
	}
	t := docx.Table{Widths: widths, Borders: true, HeaderRows: 1, Rows: []docx.Row{header}}
	items := r.d.Lists[b.Each]
	for _, it := range items {
		r.cur[b.Each] = it
		row := docx.Row{}
		for _, c := range b.Columns {
			v, err := r.text(c.Value)
			if err != nil {
				return err
			}
			bold := c.BoldIf != "" && strings.TrimSpace(it[c.BoldIf]) != ""
			ps := paragraphs(v, align(c.Align), bold, false, false, sizeTable)
			row.Cells = append(row.Cells, docx.Cell{Paragraphs: ps})
		}
		t.Rows = append(t.Rows, row)
	}
	delete(r.cur, b.Each)
	if len(items) == 0 && b.EmptyText != "" {
		s, err := r.text(b.EmptyText)
		if err != nil {
			return err
		}
		t.Rows = append(t.Rows, docx.Row{Cells: []docx.Cell{{Span: len(widths), Paragraphs: paragraphs(s, docx.AlignCenter, false, true, false, sizeTable)}}})
	}
	r.out = append(r.out, t)
	return nil
}

func (r *renderer) columns(b Block) error {
	total := widthPortrait
	if r.t.Landscape {
		total = widthLandscape
	}
	left := b.Split
	if left <= 0 || left >= total {
		left = total / 2
	}
	side := func(lines []Line, a docx.Align) ([]docx.Paragraph, error) {
		var out []docx.Paragraph
		for _, l := range lines {
			if l.If != "" && !r.present(l.If) {
				continue
			}
			s, err := r.text(l.Text)
			if err != nil {
				return nil, err
			}
			ps := paragraphs(s, a, l.Bold, l.Italic, false, 0)
			if len(ps) == 0 {
				ps = []docx.Paragraph{{Align: a}} // dòng trống có chủ ý (chỗ ký)
			}
			for i := range ps {
				ps[i].SpaceAfter = 1
			}
			out = append(out, ps...)
		}
		return out, nil
	}
	lp, err := side(b.Left, docx.AlignCenter)
	if err != nil {
		return err
	}
	rp, err := side(b.Right, docx.AlignCenter)
	if err != nil {
		return err
	}
	r.out = append(r.out, docx.Table{Widths: []int{left, total - left}, Rows: []docx.Row{{Cells: []docx.Cell{{Paragraphs: lp}, {Paragraphs: rp}}}}})
	return nil
}

// Title là khối tiêu đề văn bản chuẩn (đậm, giữa) — tiện cho bên dựng mẫu.
func Title(text string) Block {
	return Block{Kind: KindParagraph, Text: text, Align: AlignCenter, Bold: true}
}
