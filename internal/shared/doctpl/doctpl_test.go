package doctpl

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
)

func demo() Template {
	return Template{
		Key: "demo.bao_cao", Version: 1, Title: "Báo cáo mẫu", Owner: "pf/export",
		Vars: []Var{{Key: "so", Label: "Số"}, {Key: "dia_danh_ngay", Label: "Địa danh, ngày", Required: true}, {Key: "ky", Label: "Kỳ"}, {Key: "kinh_gui", Label: "Kính gửi"}, {Key: "loi_van", Label: "Lời văn"}},
		Lists: []List{{Key: "chi_tieu", Label: "Chỉ tiêu", Fields: []string{"tt", "ten", "ke_hoach", "thuc_hien", "nhom"}},
			{Key: "xa", Label: "Xã", Fields: []string{"ten"}}},
		Body: []Block{
			{Kind: KindColumns, Split: 3800, Left: []Line{{Text: "TRUNG TÂM KHUYẾN NÔNG", Bold: true}, {Text: "Số: {so}/BC-TTKN"}},
				Right: []Line{{Text: "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", Bold: true}, {Text: "{dia_danh_ngay}", Italic: true}}},
			Title("BÁO CÁO"),
			{Kind: KindParagraph, Text: "Kết quả {ky} — {{giữ ngoặc}}", Align: AlignCenter},
			{Kind: KindParagraph, Text: "Kính gửi: {kinh_gui}", If: "kinh_gui", Align: AlignCenter},
			{Kind: KindSection, Key: "I", Text: "I. Kết quả", Blocks: []Block{
				{Kind: KindTable, Each: "chi_tieu", EmptyText: "Không có chỉ tiêu", Columns: []Column{
					{Header: "TT", Value: "{chi_tieu.tt}", Width: 700, Align: AlignCenter},
					{Header: "Chỉ tiêu", Value: "{chi_tieu.ten}", Width: 4000, BoldIf: "nhom"},
					{Header: "Kế hoạch", Value: "{chi_tieu.ke_hoach}", Width: 1500, Align: AlignRight},
					{Header: "Thực hiện", Value: "{chi_tieu.thuc_hien}", Width: 1500, Align: AlignRight}}},
				{Kind: KindParagraph, Text: "{loi_van}", Indent: true, Align: AlignJustify},
			}},
			{Kind: KindSection, Key: "II", Text: "II. Địa bàn", If: "xa", Blocks: []Block{
				{Kind: KindLoop, Each: "xa", Blocks: []Block{{Kind: KindParagraph, Text: "- Xã {xa.ten}"}}},
			}},
		},
	}
}

func demoData() Data {
	return Data{
		Vars: map[string]string{"so": "7", "dia_danh_ngay": PlaceDateLine("An Giang", time.Date(2026, 3, 4, 20, 0, 0, 0, time.UTC)), "ky": "tháng 3/2026", "loi_van": "Đoạn một.\n\nĐoạn hai."},
		Lists: map[string][]Item{
			"chi_tieu": {{"tt": "I", "ten": "Trồng trọt", "nhom": "1"}, {"tt": "1", "ten": "Lúa", "ke_hoach": "1000", "thuc_hien": "1234.50"}},
			"xa":       {{"ten": "Mỹ Hòa Hưng"}, {"ten": "Phú Tân"}},
		},
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(demo()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Template){
		"khóa":              func(x *Template) { x.Key = "Sai Khoa" },
		"phiên bản":         func(x *Template) { x.Version = 0 },
		"biến lạ":           func(x *Template) { x.Body[2].Text = "{khong_co}" },
		"trường ngoài lặp":  func(x *Template) { x.Body[2].Text = "{chi_tieu.ten}" },
		"trường lạ":         func(x *Template) { x.Body[4].Blocks[0].Columns[0].Value = "{chi_tieu.khong}" },
		"danh sách lạ":      func(x *Template) { x.Body[5].Blocks[0].Each = "khong" },
		"ngoặc lẻ":          func(x *Template) { x.Body[2].Text = "a } b" },
		"thiếu ngoặc đóng":  func(x *Template) { x.Body[2].Text = "a {ky" },
		"loại khối":         func(x *Template) { x.Body[1].Kind = "image" },
		"biến trùng":        func(x *Template) { x.Vars = append(x.Vars, Var{Key: "so"}) },
		"cột không độ rộng": func(x *Template) { x.Body[4].Blocks[0].Columns[0].Width = 0 },
		"if lạ":             func(x *Template) { x.Body[3].If = "khong" },
		"lặp lồng cùng ds": func(x *Template) {
			x.Body[5].Blocks[0].Blocks = []Block{{Kind: KindLoop, Each: "xa"}}
		},
	}
	for name, mutate := range cases {
		tpl := demo()
		mutate(&tpl)
		if err := Validate(tpl); !errors.Is(err, ErrTemplate) {
			t.Errorf("%s: cần ErrTemplate, có %v", name, err)
		}
	}
}

// TestRenderGolden: cùng mẫu + dữ liệu → cùng byte (băm khóa cứng); đọc lại đúng chữ và số.
func TestRenderGolden(t *testing.T) {
	a, err := RenderDOCX(demo(), demoData())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := RenderDOCX(demo(), demoData())
	if !bytes.Equal(a, b) {
		t.Fatal("không tất định")
	}
	sum := sha256.Sum256(a)
	const golden = "973456a3bdf1841bcca8306dcb0bac56dd4e0460fcb8e6997ec51bb38568f20d"
	if got := hex.EncodeToString(sum[:]); got != golden {
		t.Fatalf("băm = %s (golden %s)", got, golden)
	}
	c, err := docx.Extract(a)
	if err != nil {
		t.Fatal(err)
	}
	text := c.Text()
	for _, want := range []string{"Số: 7/BC-TTKN", "An Giang, ngày 05 tháng 03 năm 2026", "Kết quả tháng 3/2026 — {giữ ngoặc}", "I. Kết quả", "Đoạn một.\nĐoạn hai.", "II. Địa bàn", "- Xã Mỹ Hòa Hưng\n- Xã Phú Tân"} {
		if !strings.Contains(text, want) {
			t.Fatalf("thiếu %q trong\n%s", want, text)
		}
	}
	if strings.Contains(text, "Kính gửi") {
		t.Fatal("If rỗng vẫn dựng")
	}
	grid := c.Tables[1]
	if len(grid) != 3 || grid[2][3] != "1234.50" || grid[2][2] != "1000" || grid[1][1] != "Trồng trọt" || grid[0][0] != "TT" {
		t.Fatalf("bảng = %#v", grid)
	}
}

func TestRenderData(t *testing.T) {
	d := demoData()
	delete(d.Vars, "dia_danh_ngay")
	if _, err := Render(demo(), d); !errors.Is(err, ErrData) {
		t.Fatalf("thiếu biến bắt buộc phải ErrData, có %v", err)
	}
	d = demoData()
	d.Lists["chi_tieu"] = nil
	d.Lists["xa"] = nil
	body, err := RenderDOCX(demo(), d)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := docx.Extract(body)
	if !strings.Contains(c.Text(), "Không có chỉ tiêu") || strings.Contains(c.Text(), "II. Địa bàn") {
		t.Fatalf("danh sách rỗng:\n%s", c.Text())
	}
	if a, b := demoData(), demoData(); a.Hash() != b.Hash() || demo().Hash() == (Template{}).Hash() {
		t.Fatal("băm không ổn định")
	}
}

func TestDates(t *testing.T) {
	at := time.Date(2026, 12, 31, 18, 30, 0, 0, time.UTC) // 01:30 ngày 01/01/2027 giờ Việt Nam
	if got := DateVN(at); got != "01/01/2027" {
		t.Fatalf("DateVN = %s", got)
	}
	if got := PlaceDateLine("An Giang", at); got != "An Giang, ngày 01 tháng 01 năm 2027" {
		t.Fatalf("PlaceDateLine = %s", got)
	}
	if got := PlaceDateLine("", at); got != "ngày 01 tháng 01 năm 2027" {
		t.Fatalf("PlaceDateLine rỗng = %s", got)
	}
}
