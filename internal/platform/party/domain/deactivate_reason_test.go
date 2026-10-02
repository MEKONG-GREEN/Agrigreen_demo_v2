package domain

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestFN_XA_03_03_DeactivateReasonMinMatchesLedgerCheck (KN P8, quyết định 02/10/2026): lý do vô hiệu hóa HTX tối
// thiểu 20 ký tự ở MỌI tầng — BRD v7.7 không nêu số nên giữ giá trị chặt hơn của ràng buộc CHECK sổ pf_party.org_event
// (US-HTXSTATUS-01 AC-3). Ứng dụng phải chặn trước DB (422 validation_failed, không 500 do vi phạm CHECK); đếm ký tự
// (rune) có dấu, không đếm byte; khoảng trắng đầu/cuối không tính.
func TestFN_XA_03_03_DeactivateReasonMinMatchesLedgerCheck(t *testing.T) {
	raw, err := os.ReadFile("../../../../migrations/pf/000003_pf_party_orgs.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`action <> 'deactivated' OR char_length\(btrim\(reason\)\) >= (\d+)`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("không thấy CHECK lý do vô hiệu hóa trong migration 000003")
	}
	db, _ := strconv.Atoi(string(m[1]))
	if db != MinDeactivateReason || MinDeactivateReason != 20 {
		t.Fatalf("CHECK sổ org_event = %d, ứng dụng MinDeactivateReason = %d — phải bằng nhau (20)", db, MinDeactivateReason)
	}
	r20 := "HTX giải thể theo QĐ" // 20 rune, 24 byte
	if n := len([]rune(r20)); n != 20 {
		t.Fatalf("chuỗi mẫu: %d ký tự", n)
	}
	if err := ValidateDeactivateReason(r20); err != nil {
		t.Fatalf("20 ký tự có dấu phải hợp lệ: %v", err)
	}
	short := string([]rune(r20)[:19])
	if err := ValidateDeactivateReason(short); err == nil {
		t.Fatal("19 ký tự phải bị chặn")
	}
	if err := ValidateDeactivateReason("   " + short + "   "); err == nil || !strings.Contains(err.Error(), "20") {
		t.Fatalf("khoảng trắng đầu/cuối không tính, thông điệp nêu 20: %v", err)
	}
}
