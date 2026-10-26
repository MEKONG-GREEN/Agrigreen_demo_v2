// Package docx ghi tệp Word .docx (Office Open XML, WordprocessingML) thuần Go, TẤT ĐỊNH, và đọc lại chữ của tệp đã
// ghi (W10.17, ADR-0051).
//
// Tất định: cùng Document → cùng byte. Mọi phần của gói zip có thứ tự cố định, mốc thời gian cố định (01/01/1980),
// phương thức Store (không nén — byte không phụ thuộc phiên bản compress/flate của Go), không có docProps với ngày
// tạo/sửa, không có mã ngẫu nhiên (rsid). Băm SHA-256 của tệp vì vậy là dấu của nội dung.
//
// Gói không có quy tắc nghiệp vụ và không tính toán số: bên gọi truyền chuỗi đã định dạng (số thập phân chính xác,
// ngày dd/mm/yyyy). Ký tự không hợp lệ trong XML 1.0 bị bỏ; "\n" trong một Run là ngắt dòng, "\t" là tab.
package docx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ContentType là kiểu MIME của tệp .docx.
const ContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// Giới hạn chống tài liệu bất thường (lỗi lập trình hoặc dữ liệu vào quá lớn).
const (
	MaxBlocks   = 20000
	MaxCells    = 200000
	MaxTextSize = 16 << 20
)

// Căn lề đoạn.
type Align string

const (
	AlignLeft    Align = ""
	AlignCenter  Align = "center"
	AlignRight   Align = "right"
	AlignJustify Align = "both"
)

// Run là một đoạn chữ cùng định dạng. Size tính bằng nửa điểm (26 = 13pt); 0 = cỡ mặc định của tài liệu.
type Run struct {
	Text      string
	Bold      bool
	Italic    bool
	Underline bool
	Size      int
}

// Block là phần tử cấp thân tài liệu: Paragraph, Table hoặc PageBreak.
type Block interface{ block() }

// Paragraph là một đoạn văn. FirstLine: thụt dòng đầu (twip, 567 = 1 cm). SpaceAfter: khoảng sau đoạn (twip, 0 =
// mặc định 6pt). KeepNext giữ đoạn cùng trang với đoạn sau (tiêu đề mục).
type Paragraph struct {
	Runs       []Run
	Align      Align
	FirstLine  int
	SpaceAfter int
	KeepNext   bool
}

// Table là bảng. Widths: độ rộng cột (twip), quyết định số cột. HeaderRows dòng đầu lặp lại ở mỗi trang. Borders
// false = bảng không viền (khối quốc hiệu, khối chữ ký).
type Table struct {
	Widths     []int
	Rows       []Row
	Borders    bool
	HeaderRows int
}

// Row là một dòng bảng.
type Row struct{ Cells []Cell }

// Cell là một ô: các đoạn văn, Span cột gộp (0/1 = một cột).
type Cell struct {
	Paragraphs []Paragraph
	Span       int
}

// PageBreak ngắt trang.
type PageBreak struct{}

func (Paragraph) block() {}
func (Table) block()     {}
func (PageBreak) block() {}

// Document là tài liệu. Landscape: trang A4 ngang (bảng rộng).
type Document struct {
	Blocks    []Block
	Landscape bool
}

// Text tạo đoạn một Run.
func Text(s string, align Align, bold bool) Paragraph {
	return Paragraph{Runs: []Run{{Text: s, Bold: bold}}, Align: align}
}

// ErrInvalid: tài liệu vượt giới hạn hoặc bảng sai cấu trúc.
var ErrInvalid = errors.New("docx: tài liệu không hợp lệ")

// fixedTime là mốc thời gian của mọi mục zip (giá trị nhỏ nhất của định dạng DOS) — tất định.
var fixedTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

const (
	nsW   = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsRel = "http://schemas.openxmlformats.org/package/2006/relationships"
	xmlPI = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
)

const contentTypes = xmlPI + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>` +
	`</Types>`

const rootRels = xmlPI + `<Relationships xmlns="` + nsRel + `">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`</Relationships>`

const documentRels = xmlPI + `<Relationships xmlns="` + nsRel + `">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>` +
	`</Relationships>`

// Phông Times New Roman 13pt, tiếng Việt — thể thức văn bản hành chính (NĐ 30/2020/NĐ-CP, Phụ lục I).
const styles = xmlPI + `<w:styles xmlns:w="` + nsW + `">` +
	`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:eastAsia="Times New Roman" w:cs="Times New Roman"/>` +
	`<w:sz w:val="26"/><w:szCs w:val="26"/><w:lang w:val="vi-VN" w:eastAsia="vi-VN" w:bidi="ar-SA"/></w:rPr></w:rPrDefault>` +
	`<w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="276" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>` +
	`<w:style w:type="table" w:default="1" w:styleId="TableNormal"><w:name w:val="Normal Table"/><w:tblPr><w:tblInd w:w="0" w:type="dxa"/>` +
	`<w:tblCellMar><w:top w:w="0" w:type="dxa"/><w:left w:w="80" w:type="dxa"/><w:bottom w:w="0" w:type="dxa"/><w:right w:w="80" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>` +
	`</w:styles>`

const settings = xmlPI + `<w:settings xmlns:w="` + nsW + `"><w:defaultTabStop w:val="720"/><w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`

// Write ghi tài liệu thành byte .docx tất định.
func Write(doc Document) ([]byte, error) {
	body, err := documentXML(doc)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	parts := []struct{ name, data string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rootRels},
		{"word/document.xml", body},
		{"word/_rels/document.xml.rels", documentRels},
		{"word/styles.xml", styles},
		{"word/settings.xml", settings},
	}
	for _, p := range parts {
		h := &zip.FileHeader{Name: p.name, Method: zip.Store, Modified: fixedTime}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(p.data)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type xmlBuilder struct {
	b      strings.Builder
	blocks int
	cells  int
}

func documentXML(doc Document) (string, error) {
	x := &xmlBuilder{}
	x.b.WriteString(xmlPI)
	x.b.WriteString(`<w:document xmlns:w="` + nsW + `"><w:body>`)
	if len(doc.Blocks) > MaxBlocks {
		return "", fmt.Errorf("%w: quá %d khối", ErrInvalid, MaxBlocks)
	}
	for _, b := range doc.Blocks {
		if err := x.block(b); err != nil {
			return "", err
		}
	}
	w, h, orient := 11906, 16838, ""
	if doc.Landscape {
		w, h, orient = 16838, 11906, ` w:orient="landscape"`
	}
	fmt.Fprintf(&x.b, `<w:sectPr><w:pgSz w:w="%d" w:h="%d"%s/><w:pgMar w:top="1134" w:right="851" w:bottom="1134" w:left="1701" w:header="709" w:footer="709" w:gutter="0"/></w:sectPr>`, w, h, orient)
	x.b.WriteString(`</w:body></w:document>`)
	if x.b.Len() > MaxTextSize {
		return "", fmt.Errorf("%w: nội dung quá %d byte", ErrInvalid, MaxTextSize)
	}
	return x.b.String(), nil
}

func (x *xmlBuilder) block(b Block) error {
	x.blocks++
	if x.blocks > MaxBlocks*4 {
		return fmt.Errorf("%w: quá nhiều khối", ErrInvalid)
	}
	switch v := b.(type) {
	case Paragraph:
		x.paragraph(v)
	case *Paragraph:
		x.paragraph(*v)
	case Table:
		return x.table(v)
	case *Table:
		return x.table(*v)
	case PageBreak, *PageBreak:
		x.b.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
	default:
		return fmt.Errorf("%w: khối %T", ErrInvalid, b)
	}
	return nil
}

func (x *xmlBuilder) paragraph(p Paragraph) {
	x.b.WriteString(`<w:p>`)
	var ppr strings.Builder
	if p.KeepNext {
		ppr.WriteString(`<w:keepNext/>`)
	}
	if p.SpaceAfter > 0 {
		fmt.Fprintf(&ppr, `<w:spacing w:after="%d"/>`, p.SpaceAfter)
	}
	if p.FirstLine > 0 {
		fmt.Fprintf(&ppr, `<w:ind w:firstLine="%d"/>`, p.FirstLine)
	}
	if p.Align != AlignLeft {
		ppr.WriteString(`<w:jc w:val="` + string(p.Align) + `"/>`)
	}
	if ppr.Len() > 0 {
		x.b.WriteString(`<w:pPr>` + ppr.String() + `</w:pPr>`)
	}
	for _, r := range p.Runs {
		x.run(r)
	}
	x.b.WriteString(`</w:p>`)
}

func (x *xmlBuilder) run(r Run) {
	if r.Text == "" {
		return
	}
	x.b.WriteString(`<w:r>`)
	var rpr strings.Builder
	if r.Bold {
		rpr.WriteString(`<w:b/><w:bCs/>`)
	}
	if r.Italic {
		rpr.WriteString(`<w:i/><w:iCs/>`)
	}
	if r.Underline {
		rpr.WriteString(`<w:u w:val="single"/>`)
	}
	if r.Size > 0 {
		fmt.Fprintf(&rpr, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, r.Size, r.Size)
	}
	if rpr.Len() > 0 {
		x.b.WriteString(`<w:rPr>` + rpr.String() + `</w:rPr>`)
	}
	// Tách theo \n (ngắt dòng) và \t (tab); phần chữ giữ khoảng trắng.
	text := strings.ReplaceAll(Clean(r.Text), "\r\n", "\n")
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			x.b.WriteString(`<w:br/>`)
		}
		for j, seg := range strings.Split(line, "\t") {
			if j > 0 {
				x.b.WriteString(`<w:tab/>`)
			}
			if seg != "" {
				x.b.WriteString(`<w:t xml:space="preserve">`)
				escape(&x.b, seg)
				x.b.WriteString(`</w:t>`)
			}
		}
	}
	x.b.WriteString(`</w:r>`)
}

func (x *xmlBuilder) table(t Table) error {
	cols := len(t.Widths)
	if cols == 0 || cols > 63 {
		return fmt.Errorf("%w: bảng cần 1–63 cột", ErrInvalid)
	}
	total := 0
	for _, w := range t.Widths {
		if w <= 0 {
			return fmt.Errorf("%w: độ rộng cột phải dương", ErrInvalid)
		}
		total += w
	}
	x.b.WriteString(`<w:tbl><w:tblPr>`)
	fmt.Fprintf(&x.b, `<w:tblW w:w="%d" w:type="dxa"/><w:tblLayout w:type="fixed"/>`, total)
	if t.Borders {
		x.b.WriteString(`<w:tblBorders>`)
		for _, side := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
			x.b.WriteString(`<w:` + side + ` w:val="single" w:sz="4" w:space="0" w:color="000000"/>`)
		}
		x.b.WriteString(`</w:tblBorders>`)
	}
	x.b.WriteString(`</w:tblPr><w:tblGrid>`)
	for _, w := range t.Widths {
		fmt.Fprintf(&x.b, `<w:gridCol w:w="%d"/>`, w)
	}
	x.b.WriteString(`</w:tblGrid>`)
	for i, row := range t.Rows {
		used := 0
		for _, c := range row.Cells {
			used += max(c.Span, 1)
		}
		if used != cols {
			return fmt.Errorf("%w: dòng %d có %d cột, bảng có %d", ErrInvalid, i+1, used, cols)
		}
		x.b.WriteString(`<w:tr>`)
		if i < t.HeaderRows {
			x.b.WriteString(`<w:trPr><w:tblHeader/><w:cantSplit/></w:trPr>`)
		}
		col := 0
		for _, c := range row.Cells {
			x.cells++
			if x.cells > MaxCells {
				return fmt.Errorf("%w: quá %d ô", ErrInvalid, MaxCells)
			}
			span := max(c.Span, 1)
			width := 0
			for k := col; k < col+span; k++ {
				width += t.Widths[k]
			}
			col += span
			fmt.Fprintf(&x.b, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>`, width)
			if span > 1 {
				x.b.WriteString(`<w:gridSpan w:val="` + strconv.Itoa(span) + `"/>`)
			}
			x.b.WriteString(`</w:tcPr>`)
			if len(c.Paragraphs) == 0 {
				x.b.WriteString(`<w:p/>`) // ô Word bắt buộc có ít nhất một đoạn
			}
			for _, p := range c.Paragraphs {
				p.SpaceAfter = max(p.SpaceAfter, 1)
				x.paragraph(p)
			}
			x.b.WriteString(`</w:tc>`)
		}
		x.b.WriteString(`</w:tr>`)
	}
	x.b.WriteString(`</w:tbl>`)
	// Word cần một đoạn sau bảng trước bảng/sectPr kế tiếp để không gộp hai bảng.
	x.b.WriteString(`<w:p><w:pPr><w:spacing w:after="0"/></w:pPr></w:p>`)
	return nil
}

// Clean bỏ ký tự không hợp lệ trong XML 1.0 và chuỗi UTF-8 hỏng (giữ \t, \n, \r).
func Clean(s string) string {
	ok := true
	for _, r := range s {
		if !validXML(r) {
			ok = false
			break
		}
	}
	if ok && utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s, "") {
		if validXML(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func validXML(r rune) bool {
	return r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0x10FFFF)
}

func escape(b *strings.Builder, s string) {
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\r':
		default:
			b.WriteRune(r)
		}
	}
}
