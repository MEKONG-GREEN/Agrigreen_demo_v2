package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Content là chữ đọc lại từ tệp .docx: mọi đoạn theo thứ tự tài liệu (kể cả đoạn trong ô bảng) và các bảng (ô = các
// đoạn nối bằng "\n"). Dùng cho kiểm thử khứ hồi và xem trước — không phải bộ đọc Word đầy đủ.
type Content struct {
	Paragraphs []string
	Tables     [][][]string
}

// Text nối mọi đoạn bằng "\n".
func (c Content) Text() string { return strings.Join(c.Paragraphs, "\n") }

// Giới hạn đọc (chống zip bomb).
const maxDocumentXML = 64 << 20

// Extract đọc word/document.xml của tệp .docx.
func Extract(body []byte) (Content, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return Content{}, fmt.Errorf("docx: không đọc được gói zip: %w", err)
	}
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
		}
	}
	if doc == nil {
		return Content{}, fmt.Errorf("docx: thiếu word/document.xml")
	}
	if doc.UncompressedSize64 > maxDocumentXML {
		return Content{}, fmt.Errorf("docx: word/document.xml quá lớn")
	}
	rc, err := doc.Open()
	if err != nil {
		return Content{}, err
	}
	defer func() { _ = rc.Close() }()
	return parse(io.LimitReader(rc, maxDocumentXML))
}

func parse(r io.Reader) (Content, error) {
	var out Content
	dec := xml.NewDecoder(r)
	var para *strings.Builder
	inText := false
	type tbl struct {
		rows [][]string
		cell []string
		in   bool
	}
	var tables []*tbl
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Content{}, fmt.Errorf("docx: XML hỏng: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != nsW {
				continue
			}
			switch t.Name.Local {
			case "p":
				para = &strings.Builder{}
			case "t":
				inText = true
			case "tab":
				if para != nil {
					para.WriteString("\t")
				}
			case "br":
				if para != nil && attr(t, "type") == "" {
					para.WriteString("\n")
				}
			case "tbl":
				tables = append(tables, &tbl{})
			case "tr":
				if n := len(tables); n > 0 {
					tables[n-1].rows = append(tables[n-1].rows, nil)
				}
			case "tc":
				if n := len(tables); n > 0 {
					tables[n-1].cell, tables[n-1].in = nil, true
				}
			}
		case xml.EndElement:
			if t.Name.Space != nsW {
				continue
			}
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if para != nil {
					s := para.String()
					out.Paragraphs = append(out.Paragraphs, s)
					if n := len(tables); n > 0 && tables[n-1].in {
						tables[n-1].cell = append(tables[n-1].cell, s)
					}
				}
				para = nil
			case "tc":
				if n := len(tables); n > 0 {
					tb := tables[n-1]
					if r := len(tb.rows); r > 0 {
						tb.rows[r-1] = append(tb.rows[r-1], strings.Join(tb.cell, "\n"))
					}
					tb.in = false
				}
			case "tbl":
				if n := len(tables); n > 0 {
					out.Tables = append(out.Tables, tables[n-1].rows)
					tables = tables[:n-1]
				}
			}
		case xml.CharData:
			if inText && para != nil {
				para.Write(t)
			}
		}
	}
	return out, nil
}

func attr(e xml.StartElement, local string) string {
	for _, a := range e.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}
