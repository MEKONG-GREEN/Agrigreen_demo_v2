package docx

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func sample() Document {
	return Document{Blocks: []Block{
		Table{Widths: []int{4000, 5300}, Rows: []Row{{Cells: []Cell{
			{Paragraphs: []Paragraph{Text("TRUNG TÂM KHUYẾN NÔNG", AlignCenter, true), Text("Số: 12/BC-TTKN", AlignCenter, false)}},
			{Paragraphs: []Paragraph{Text("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", AlignCenter, true), {Runs: []Run{{Text: "An Giang, ngày 20 tháng 10 năm 2026", Italic: true}}, Align: AlignRight}}},
		}}}},
		Text("BÁO CÁO", AlignCenter, true),
		Paragraph{Runs: []Run{{Text: "Dòng 1\nDòng 2\tcó tab & <thẻ> \"nháy\""}}, FirstLine: 567, Align: AlignJustify},
		Table{Widths: []int{800, 4000, 1500}, Borders: true, HeaderRows: 1, Rows: []Row{
			{Cells: []Cell{{Paragraphs: []Paragraph{Text("TT", AlignCenter, true)}}, {Paragraphs: []Paragraph{Text("Chỉ tiêu", AlignCenter, true)}}, {Paragraphs: []Paragraph{Text("Lũy kế", AlignCenter, true)}}}},
			{Cells: []Cell{{Paragraphs: []Paragraph{Text("1", AlignCenter, false)}}, {Paragraphs: []Paragraph{Text("Diện tích lúa", AlignLeft, false)}}, {Paragraphs: []Paragraph{Text("1234.5", AlignRight, false)}}}},
			{Cells: []Cell{{Span: 2, Paragraphs: []Paragraph{Text("Cộng", AlignLeft, true)}}, {}}},
		}},
		PageBreak{},
		Text("Phụ lục", AlignCenter, true),
	}}
}

// TestDeterministic: cùng tài liệu → cùng byte; băm khóa cứng (đổi bộ ghi phải cập nhật có chủ ý — byte cũ đã lưu ở sổ
// lần xuất không đổi theo).
func TestDeterministic(t *testing.T) {
	a, err := Write(sample())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Write(sample())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("hai lần ghi khác byte")
	}
	sum := sha256.Sum256(a)
	const golden = "4f5dfd67c0f2bb8efb35cdedebf9be33817892a3d51083855458364c2d69c5e0"
	if got := hex.EncodeToString(sum[:]); got != golden {
		t.Fatalf("băm .docx = %s (golden %s)", got, golden)
	}
	zr, err := zip.NewReader(bytes.NewReader(a), int64(len(a)))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Method != zip.Store || !f.Modified.Equal(fixedTime) {
			t.Fatalf("%s: method %d, modified %v", f.Name, f.Method, f.Modified)
		}
	}
	if strings.Join(names, ",") != "[Content_Types].xml,_rels/.rels,word/document.xml,word/_rels/document.xml.rels,word/styles.xml,word/settings.xml" {
		t.Fatalf("thứ tự phần: %v", names)
	}
}

func TestExtractRoundTrip(t *testing.T) {
	body, err := Write(sample())
	if err != nil {
		t.Fatal(err)
	}
	c, err := Extract(body)
	if err != nil {
		t.Fatal(err)
	}
	text := c.Text()
	for _, want := range []string{"Số: 12/BC-TTKN", "An Giang, ngày 20 tháng 10 năm 2026", "BÁO CÁO", "Dòng 1\nDòng 2\tcó tab & <thẻ> \"nháy\"", "Phụ lục"} {
		if !strings.Contains(text, want) {
			t.Fatalf("thiếu %q trong\n%s", want, text)
		}
	}
	if len(c.Tables) != 2 {
		t.Fatalf("số bảng = %d", len(c.Tables))
	}
	grid := c.Tables[1]
	if len(grid) != 3 || grid[1][2] != "1234.5" || grid[1][1] != "Diện tích lúa" || grid[2][0] != "Cộng" || len(grid[2]) != 2 {
		t.Fatalf("bảng = %#v", grid)
	}
	if c.Tables[0][0][0] != "TRUNG TÂM KHUYẾN NÔNG\nSố: 12/BC-TTKN" {
		t.Fatalf("ô quốc hiệu = %q", c.Tables[0][0][0])
	}
}

func TestCleanAndInvalid(t *testing.T) {
	if got := Clean("a\x00b\x1fc￾d\te"); got != "abcd\te" {
		t.Fatalf("Clean = %q", got)
	}
	body, err := Write(Document{Blocks: []Block{Text("x\x01y\xffz", AlignLeft, false)}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Extract(body)
	if err != nil || c.Paragraphs[0] != "xyz" {
		t.Fatalf("%v %#v", err, c.Paragraphs)
	}
	_, err = Write(Document{Blocks: []Block{Table{Widths: []int{100, 100}, Rows: []Row{{Cells: []Cell{{}}}}}}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("thiếu ô phải lỗi, có %v", err)
	}
	_, err = Write(Document{Blocks: []Block{Table{}}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("bảng không cột phải lỗi, có %v", err)
	}
	if _, err := Extract([]byte("không phải zip")); err == nil {
		t.Fatal("tệp hỏng phải lỗi")
	}
}

func TestLandscape(t *testing.T) {
	body, err := Write(Document{Landscape: true, Blocks: []Block{Text("x", AlignLeft, false)}})
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	rc, _ := zr.File[2].Open()
	var b bytes.Buffer
	_, _ = b.ReadFrom(rc)
	if !strings.Contains(b.String(), `w:orient="landscape"`) {
		t.Fatal("thiếu trang ngang")
	}
}
