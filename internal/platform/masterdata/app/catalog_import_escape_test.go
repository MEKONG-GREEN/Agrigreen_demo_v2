package app

import (
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
)

// Review Codex 7e29 (#170): nút "Xuất danh mục" (toCsv) thêm dấu nháy trước ô bắt đầu bằng = + - @ tab CR; nạp lại
// chính tệp đó phải coi ô có dấu nháy là cùng nội dung với mục đã có, nhưng không nới cho ô khác.
func TestCSVFormulaEscapedAs(t *testing.T) {
	cur := domain.Item{Name: "- Chỉ tiêu gạch", Attrs: map[string]string{"description": "- Theo dõi nhiệt độ ao mỗi ngày", "unit": "°C", "note": "@x"}}
	got := csvFormulaEscapedAs(domain.Item{Name: " '- Chỉ tiêu gạch ", Attrs: map[string]string{"description": "'- Theo dõi nhiệt độ ao mỗi ngày", "unit": "°C", "note": "'@x"}}, cur)
	if !cur.SameContent(got) {
		t.Fatalf("ô có dấu nháy chống công thức phải cùng nội dung: %+v", got)
	}
	for _, c := range []struct{ cell, stored, want string }{
		{"'=1+1", "=1+1", "=1+1"},
		{"'+ đo sáng", "+ đo sáng", "+ đo sáng"},
		{"'\tx", "\tx", "\tx"},
		{"'abc", "abc", "'abc"},       // giá trị đã lưu không bắt đầu bằng ký tự công thức → giữ nguyên ô
		{"''- a", "- a", "''- a"},     // chỉ bỏ đúng một dấu nháy do tệp xuất thêm
		{"'- a", "'- a", "'- a"},      // giá trị đã lưu có sẵn dấu nháy → so nguyên
		{"'- khác", "- a", "'- khác"}, // nội dung khác vẫn khác
		{"'-", "", "'-"},              // mục chưa có thuộc tính
		{strings.Repeat("x", 3), "-", strings.Repeat("x", 3)},
		// Review Codex 7058: encoding/csv đổi CRLF trong ô ngoặc kép thành LF.
		{"dòng 1\ndòng 2", "dòng 1\r\ndòng 2", "dòng 1\r\ndòng 2"},
		{"'- dòng 1\ndòng 2", "- dòng 1\r\ndòng 2", "- dòng 1\r\ndòng 2"},
		{"dòng 1\ndòng 2", "dòng 1\ndòng 2", "dòng 1\ndòng 2"},
		{"dòng 1\ndòng 3", "dòng 1\r\ndòng 2", "dòng 1\ndòng 3"},
		{"dòng 1\rdòng 2", "dòng 1\r\ndòng 2", "dòng 1\rdòng 2"},
	} {
		if g := csvUnescapeAs(c.cell, c.stored); g != c.want {
			t.Errorf("csvUnescapeAs(%q, %q) = %q, muốn %q", c.cell, c.stored, g, c.want)
		}
	}
	// Mục mới (không có mục đã lưu để so) giữ nguyên ô của tệp.
	if g := csvFormulaEscapedAs(domain.Item{Name: "'- mới"}, domain.Item{}); g.Name != "'- mới" {
		t.Fatalf("không có mục đã lưu: %q", g.Name)
	}
}
