package domain

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

// Trích nội dung từ tệp có sẵn để điền sẵn bản nháp (KN US-LIB-03, HTX US-CMS-02).
// Kết quả CHỈ để điền bộ soạn thảo — không bao giờ tự xuất bản.

// Lỗi trích xuất.
var (
	// ErrExtractUnsupported: định dạng chưa hỗ trợ trích (PDF: chưa có thư viện
	// thuần Go giấy phép phù hợp đọc đúng phông tiếng Việt — soạn thủ công).
	ErrExtractUnsupported = errors.New("content: định dạng chưa hỗ trợ trích nội dung")
	// ErrExtractFailed: tệp hỏng, không đọc được hoặc không có chữ (ảnh scan).
	ErrExtractFailed = errors.New("content: không trích được nội dung")
)

// Extracted là kết quả trích.
type Extracted struct {
	Title      string
	Body       string // Markdown
	BodyFormat BodyFormat
}

// Giới hạn đọc word/document.xml sau giải nén (chống zip bomb).
const maxDocXML = 32 << 20

// FileKind nhận diện loại tệp theo chữ ký đầu tệp.
func FileKind(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("%PDF-")):
		return "pdf"
	case bytes.HasPrefix(b, []byte("PK\x03\x04")):
		return "zip"
	case bytes.HasPrefix(b, []byte{0xD0, 0xCF, 0x11, 0xE0}):
		return "ole" // .doc cũ
	}
	return ""
}

// Extract trích nội dung theo chữ ký tệp.
func Extract(b []byte) (Extracted, error) {
	switch FileKind(b) {
	case "zip":
		return ExtractDOCX(b)
	case "pdf", "ole":
		return Extracted{}, ErrExtractUnsupported
	}
	return Extracted{}, ErrExtractUnsupported
}

// ExtractDOCX đọc word/document.xml của tệp .docx: đoạn văn thành đoạn
// Markdown, Heading1..4/Title thành #..####, đoạn có đánh số thành "- ".
func ExtractDOCX(b []byte) (Extracted, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return Extracted{}, ErrExtractFailed
	}
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return Extracted{}, ErrExtractFailed
	}
	rc, err := doc.Open()
	if err != nil {
		return Extracted{}, ErrExtractFailed
	}
	defer func() { _ = rc.Close() }()

	dec := xml.NewDecoder(io.LimitReader(rc, maxDocXML))
	var (
		paras []string
		title string
		first string
		cur   strings.Builder
		style string
		list  bool
		inT   bool
	)
	flush := func() {
		text := strings.TrimSpace(cur.String())
		cur.Reset()
		if text != "" {
			text = escapeMarkdownLine(text)
			prefix := ""
			switch strings.ToLower(strings.ReplaceAll(style, " ", "")) {
			case "title", "heading1", "tieude1":
				prefix = "# "
			case "heading2", "tieude2":
				prefix = "## "
			case "heading3", "tieude3":
				prefix = "### "
			case "heading4", "tieude4":
				prefix = "#### "
			default:
				if list {
					prefix = "- "
				}
			}
			if title == "" && strings.HasPrefix(prefix, "#") {
				title = text
			}
			if first == "" {
				first = text
			}
			paras = append(paras, prefix+text)
		}
		style, list = "", false
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Extracted{}, ErrExtractFailed
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				cur.Reset()
				style, list = "", false
			case "pStyle":
				style = attr(t, "val")
			case "numPr":
				list = true
			case "t":
				inT = true
			case "tab":
				cur.WriteByte(' ')
			case "br", "cr":
				cur.WriteByte(' ')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inT = false
			case "p":
				flush()
			}
		case xml.CharData:
			if inT {
				cur.Write(t)
			}
		}
	}
	if len(paras) == 0 {
		return Extracted{}, ErrExtractFailed
	}
	if title == "" {
		title = first
	}
	if utf8.RuneCountInString(title) > MaxTitle {
		title = string([]rune(title)[:MaxTitle])
	}
	body := strings.Join(paras, "\n\n")
	if len(body) > MaxBodyBytes {
		body = body[:MaxBodyBytes]
		for !utf8.ValidString(body) {
			body = body[:len(body)-1]
		}
	}
	return Extracted{Title: title, Body: body, BodyFormat: FormatMarkdown}, nil
}

func attr(e xml.StartElement, local string) string {
	for _, a := range e.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// escapeMarkdownLine chặn văn bản gốc vô tình thành thẻ HTML khi hiển thị Markdown.
func escapeMarkdownLine(s string) string {
	return strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(s)
}
