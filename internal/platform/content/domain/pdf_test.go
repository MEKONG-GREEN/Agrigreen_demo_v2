package domain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/font/sfnt"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/pdftest"
)

// Nội dung của tệp mẫu testdata/quy-trinh-vi.pdf.
const (
	vnHeading  = "Quy trình canh tác lúa giảm phát thải"
	vnPara1    = "Làm đất, san phẳng mặt ruộng; tưới ngập khô xen kẽ (AWD) giúp giảm phát thải khí mê-tan. Người nông dân cần theo dõi mực nước ở ống quan trắc, rút nước khi mực nước xuống dưới 15 cm và bơm lại khi ruộng khô nứt chân chim."
	vnPara2    = "Sạ hàng 60 kg/ha; bón phân theo bảng so màu lá. Ưu tiên giống ngắn ngày, chống đổ ngã."
	vnHeading2 = "Thu hoạch và xử lý rơm rạ"
	vnPara3    = "Không đốt rơm rạ; thu gom để ủ phân hữu cơ hoặc trồng nấm rơm. Đường, Ơn, Ưng, Ỷ, Ỹ."
)

// VietnameseFixture là đường dẫn tệp mẫu (dùng chung với kiểm thử tích hợp).
const vietnameseFixture = "testdata/quy-trinh-vi.pdf"

func vnBlocks() []pdftest.Block {
	return []pdftest.Block{
		{Heading: true, Text: vnHeading}, {Text: vnPara1}, {Text: vnPara2},
		{Heading: true, Text: vnHeading2}, {Text: vnPara3 + " <script>"},
	}
}

// vnFixture đọc tệp mẫu. Đặt AG_REGEN_PDF_FIXTURE=1 để sinh lại bằng phông hệ
// thống có đủ chữ tiếng Việt (AG_PDF_FONT, AG_PDF_FONT_BOLD; mặc định Arial của
// Windows — PDF chỉ nhúng tập con glyph, được phép theo quyền nhúng của phông).
func vnFixture(t *testing.T) []byte {
	t.Helper()
	if os.Getenv("AG_REGEN_PDF_FIXTURE") != "" {
		reg := envOr("AG_PDF_FONT", `C:\Windows\Fonts\arial.ttf`)
		bold := envOr("AG_PDF_FONT_BOLD", `C:\Windows\Fonts\arialbd.ttf`)
		fonts := [2][]byte{}
		for i, p := range []string{reg, bold} {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			f, err := sfnt.Parse(b)
			if err != nil {
				t.Fatal(err)
			}
			var buf sfnt.Buffer
			for _, b := range vnBlocks() {
				for _, r := range b.Text {
					if gi, err := f.GlyphIndex(&buf, r); err != nil || gi == 0 {
						t.Fatalf("phông %s thiếu chữ %q", p, r)
					}
				}
			}
			fonts[i] = b
		}
		data, err := pdftest.Generate(vnBlocks(), pdftest.Options{Regular: fonts[0], Bold: fonts[1], NewPage: []int{3}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(vietnameseFixture), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vietnameseFixture, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(vietnameseFixture)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// US-LIB-03: trích PDF tiếng Việt có dấu — tiêu đề, đoạn nhiều dòng, sang trang.
func TestExtractPDFVietnamese(t *testing.T) {
	got, err := Extract(vnFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != vnHeading || got.BodyFormat != FormatMarkdown {
		t.Fatalf("tiêu đề: %q", got.Title)
	}
	want := "# " + vnHeading + "\n\n" + vnPara1 + "\n\n" + vnPara2 + "\n\n# " + vnHeading2 + "\n\n" + vnPara3 + " &lt;script&gt;"
	if got.Body != want {
		t.Fatalf("thân sai:\n%s\n--- muốn ---\n%s", got.Body, want)
	}
}

// ToUnicode: bfrange cộng đích như số nguyên (không cắt 8 bit), bfchar, dạng mảng.
func TestParseToUnicode(t *testing.T) {
	m := parseToUnicode(`1 begincodespacerange <0000> <FFFF> endcodespacerange
2 beginbfchar <0003> <0020> <0101> <0110> endbfchar
2 beginbfrange <0000> <FFFF> <0000> <0200> <0202> [<1EA3> <01B0> <0111>] endbfrange`, 2)
	if m == nil {
		t.Fatal("không đọc được CMap")
	}
	got := m.Decode("\x1e\xa3\x00\x03\x01\x01\x02\x00\x02\x01\x02\x02")
	if got != "ả Đảưđ" {
		t.Fatalf("giải mã: %q", got)
	}
}

func TestExtractPDFRejects(t *testing.T) {
	var xe *ExtractError
	enc, err := pdftest.Generate([]pdftest.Block{{Text: "Noi dung bi mat"}}, pdftest.Options{Password: "bi-mat"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(enc); !errors.Is(err, ErrExtractUnsupported) || !errors.As(err, &xe) || !strings.Contains(xe.Msg, "mã hóa") {
		t.Fatalf("PDF mã hóa: %v", err)
	}
	scan, err := pdftest.Generate(nil, pdftest.Options{ImageOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(scan); !errors.Is(err, ErrExtractUnsupported) || !errors.As(err, &xe) || !strings.Contains(xe.Msg, "scan") {
		t.Fatalf("PDF ảnh scan: %v", err)
	}
	if _, err := Extract([]byte("%PDF-1.7\nhỏng")); !errors.Is(err, ErrExtractFailed) {
		t.Fatalf("PDF hỏng: %v", err)
	}
	// Chữ ASCII với phông Go: trích đúng (đường sinh tệp không phụ thuộc phông hệ thống).
	ascii, err := pdftest.Generate([]pdftest.Block{{Heading: true, Text: "Huong dan"}, {Text: "Bon phan dung luc."}}, pdftest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Extract(ascii); err != nil || got.Body != "# Huong dan\n\nBon phan dung luc." {
		t.Fatalf("PDF ASCII: %v %q", err, got.Body)
	}
}
