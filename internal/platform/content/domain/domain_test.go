package domain

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
)

// Bộ lọc thân bài: bỏ script/style/iframe cả nội dung, bỏ on*, chặn javascript:.
func TestSanitizeHTML(t *testing.T) {
	in := `<p onclick="alert(1)" style="color:red">Bón phân <b>đợt 1</b><script>alert('x')</script></p>` +
		`<img src="javascript:alert(1)" onerror="alert(2)"><img src="https://cdn.example.vn/a.png" alt="lá lúa">` +
		`<a href="jav&#x61;script:alert(1)">xấu</a><a href="https://ttkn.gov.vn/qt" target="_blank">tốt</a>` +
		`<iframe src="https://evil"></iframe><style>body{}</style><svg><script>1</script></svg>` +
		`<div><em>chữ trong div</em></div><!-- ghi chú --><table><tr><td colspan="2" onmouseover="x">ô</td></tr></table><ul><li>mục`
	out := Sanitize(in, FormatHTML)
	for _, bad := range []string{"<script", "alert(", "onclick", "onerror", "onmouseover", "style", "javascript", "iframe", "<svg", "<div", "target=", "ghi chú"} {
		if strings.Contains(strings.ToLower(out), bad) {
			t.Errorf("còn %q trong kết quả: %s", bad, out)
		}
	}
	for _, good := range []string{
		"<p>Bón phân <b>đợt 1</b></p>", `<img src="https://cdn.example.vn/a.png" alt="lá lúa" />`,
		`<a href="https://ttkn.gov.vn/qt" rel="noopener noreferrer nofollow">tốt</a>`, "<em>chữ trong div</em>",
		`<td colspan="2">ô</td>`, "<ul><li>mục</li></ul>",
	} {
		if !strings.Contains(out, good) {
			t.Errorf("thiếu %q trong kết quả: %s", good, out)
		}
	}
}

func TestSanitizeMarkdown(t *testing.T) {
	in := "# Quy trình\n\nBước 1 & 2: a < b\n\n<script>alert(1)</script>[bấm](javascript:alert(1)) " +
		"![ảnh](att:0191e5a0-0000-7000-8000-000000000001) [ttkn](https://ttkn.gov.vn)\n\n[r]: vbscript:msgbox\n<b onclick=\"x\">đậm</b>"
	out := Sanitize(in, FormatMarkdown)
	for _, bad := range []string{"<script", "alert", "javascript", "vbscript", "onclick"} {
		if strings.Contains(out, bad) {
			t.Errorf("còn %q: %s", bad, out)
		}
	}
	for _, good := range []string{"# Quy trình", "Bước 1 & 2: a &lt; b", "[bấm](#)", "(att:0191e5a0-0000-7000-8000-000000000001)", "[ttkn](https://ttkn.gov.vn)", "[r]: #", "<b>đậm</b>"} {
		if !strings.Contains(out, good) {
			t.Errorf("thiếu %q: %s", good, out)
		}
	}
}

// Tìm kiếm không dấu (US-LIB-04): "lua" khớp "lúa", "dong" khớp "Đồng".
func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"Lúa Đông Xuân 2026":            "lua dong xuan 2026",
		"  Quy trình — kỹ thuật!!  ":    "quy trinh ky thuat",
		"Sâu cuốn lá nhỏ; rầy nâu":      "sau cuon la nho ray nau",
		"ĐỒNG BẰNG SÔNG CỬU LONG":       "dong bang song cuu long",
		"Phòng trừ bệnh đạo ôn (lá/cổ)": "phong tru benh dao on la co",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, muốn %q", in, got, want)
		}
	}
	if got := SearchQuery("Lúa  đông"); got != "lua:* & dong:*" {
		t.Errorf("SearchQuery = %q", got)
	}
	if got := SearchQuery(" ' & | ! "); got != "" {
		t.Errorf("SearchQuery ký tự đặc biệt = %q", got)
	}
}

func docx(t *testing.T, documentXML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("[Content_Types].xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(`<?xml version="1.0"?><Types/>`))
	w, _ = zw.Create("word/document.xml")
	_, _ = w.Write([]byte(documentXML))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// DocXML dựng word/document.xml tối thiểu.
func DocXML(paras ...string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		strings.Join(paras, "") + `</w:body></w:document>`
}

// US-LIB-03: trích nội dung từ .docx.
func TestExtractDOCX(t *testing.T) {
	b := docx(t, DocXML(
		`<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Quy trình canh tác lúa</w:t></w:r><w:r><w:t xml:space="preserve"> giảm phát thải</w:t></w:r></w:p>`,
		`<w:p><w:r><w:t>Làm đất, san phẳng mặt ruộng.</w:t></w:r></w:p>`,
		`<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/></w:numPr></w:pPr><w:r><w:t>Sạ hàng 60 kg/ha</w:t></w:r></w:p>`,
		`<w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>Tưới &lt;AWD&gt;</w:t></w:r></w:p>`,
		`<w:p></w:p>`,
	))
	got, err := Extract(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Quy trình canh tác lúa giảm phát thải" || got.BodyFormat != FormatMarkdown {
		t.Fatalf("tiêu đề/định dạng sai: %+v", got)
	}
	want := "# Quy trình canh tác lúa giảm phát thải\n\nLàm đất, san phẳng mặt ruộng.\n\n- Sạ hàng 60 kg/ha\n\n## Tưới &lt;AWD&gt;"
	if got.Body != want {
		t.Fatalf("thân sai:\n%s\n--- muốn ---\n%s", got.Body, want)
	}
}

func TestExtractRejects(t *testing.T) {
	if _, err := Extract([]byte("%PDF-1.7 ...")); !errors.Is(err, ErrExtractUnsupported) {
		t.Fatalf("PDF: %v", err)
	}
	if _, err := Extract([]byte("PK\x03\x04hỏng")); !errors.Is(err, ErrExtractFailed) {
		t.Fatalf("zip hỏng: %v", err)
	}
	if _, err := Extract(docx(t, DocXML(`<w:p></w:p>`))); !errors.Is(err, ErrExtractFailed) {
		t.Fatalf("docx rỗng (ảnh scan): %v", err)
	}
	if _, err := Extract([]byte("văn bản thường")); !errors.Is(err, ErrExtractUnsupported) {
		t.Fatalf("txt: %v", err)
	}
}

func TestValidateAudience(t *testing.T) {
	parent := uuid.New()
	cases := []struct {
		name   string
		kind   port.Kind
		a      Audience
		parent *uuid.UUID
		ok     bool
	}{
		{"quy trình toàn quốc", port.KindProcedure, Audience{Level: port.AudienceNational}, nil, true},
		{"BR-06 quy trình cấp tỉnh", port.KindProcedure, Audience{Level: port.AudienceProvince, Units: []string{"89"}}, nil, false},
		{"bổ sung cần gốc", port.KindSupplement, Audience{Level: port.AudienceProvince, Units: []string{"89"}}, nil, false},
		{"bổ sung toàn quốc", port.KindSupplement, Audience{Level: port.AudienceNational}, &parent, false},
		{"bổ sung tỉnh", port.KindSupplement, Audience{Level: port.AudienceProvince, Units: []string{"89"}}, &parent, true},
		{"tin gắn gốc", port.KindNews, Audience{Level: port.AudienceNational}, &parent, false},
		{"tỉnh hai mã", port.KindGuide, Audience{Level: port.AudienceProvince, Units: []string{"89", "92"}}, nil, false},
		{"xã lặp", port.KindAlert, Audience{Level: port.AudienceCommune, Units: []string{"30001", "30001"}}, nil, false},
		{"xã", port.KindAlert, Audience{Level: port.AudienceCommune, Units: []string{"30001", "30002"}}, nil, true},
	}
	for _, c := range cases {
		if err := ValidateAudience(c.kind, c.a, c.parent); (err == nil) != c.ok {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
}

func TestVersionStatuses(t *testing.T) {
	n := func(i int) *int { return &i }
	vs := []Version{{VersionNo: 1}, {VersionNo: 2}, {VersionNo: 3}, {VersionNo: 4}, {VersionNo: 5}}
	evs := []Event{
		{Action: ActionSubmitted, VersionNo: n(2)}, {Action: ActionPublished, VersionNo: n(2)},
		{Action: ActionSubmitted, VersionNo: n(3)}, {Action: ActionReturned, VersionNo: n(3)},
		{Action: ActionSubmitted, VersionNo: n(4)}, {Action: ActionPublished, VersionNo: n(4)},
	}
	got := VersionStatuses(vs, evs)
	want := map[int]VersionStatus{1: VersionDiscarded, 2: VersionSuperseded, 3: VersionDiscarded, 4: VersionPublished, 5: VersionDraft}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("phiên bản %d: %s, muốn %s", k, got[k], v)
		}
	}
}
