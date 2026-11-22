package domain

// BF-NOTIFY-KN-ADAPTER (#121): mẫu nhắc theo biểu chỉ có ba biến không PII ({bieu}, {ky}, {han}); thay một lượt (giá trị
// chứa "{ky}" không bị thay tiếp), ≤ 500 ký tự.

import (
	"strings"
	"testing"
	"time"
)

func TestFormReminderRenderVariables_BF_NOTIFY_KN_ADAPTER(t *testing.T) {
	loc := time.FixedZone("ICT", 7*3600)
	due := time.Date(2026, 10, 30, 3, 0, 0, 0, time.UTC)
	r := FormReminder{Message: "Nộp {bieu} kỳ {ky} trước {han}; {khac}"}
	got := r.Render("BM-04 Tổng đàn", "Tuần 44 {ky}", due, loc)
	if got != "Nộp BM-04 Tổng đàn kỳ Tuần 44 {ky} trước 10:00 30/10/2026; {khac}" {
		t.Fatalf("thay biến: %q", got)
	}
	long := FormReminder{Message: strings.Repeat("ắ", 499) + "{bieu}"}
	if n := len([]rune(long.Render("BM-01", "", due, loc))); n != 500 {
		t.Fatalf("cắt 500 ký tự: %d", n)
	}
}
