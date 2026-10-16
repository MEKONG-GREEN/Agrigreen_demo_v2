package app

import (
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
)

// W11.11 FN-PN-03.01 B3: Nhập / Tiếp tục (Chưa nhập, Lưu nháp, Trễ hạn), Sửa, gửi lại (Yêu cầu bổ sung); phiếu Đã gửi,
// Đã xác nhận, Đã khóa, Không gửi chỉ có Xem. Phiên chỉ xem: mọi nút là Xem (vẫn tính "Cần xử lý").
func TestActionOf_FN_PN_03_01(t *testing.T) {
	cases := []struct {
		state string
		act   string
		needs bool
	}{
		{domain.FormNotStarted, ActionEnter, true},
		{domain.FormLate, ActionEnter, true},
		{domain.FormDraft, ActionContinue, true},
		{domain.FormSupplement, ActionResubmit, true},
		{domain.FormSubmitted, ActionView, false},
		{domain.FormConfirmed, ActionView, false},
		{domain.FormLocked, ActionView, false},
		{domain.FormNotSent, ActionView, false},
	}
	for _, c := range cases {
		if act, needs := actionOf(c.state, false); act != c.act || needs != c.needs {
			t.Fatalf("%s: %s %v", c.state, act, needs)
		}
		if act, needs := actionOf(c.state, true); act != ActionView || needs != c.needs {
			t.Fatalf("%s chỉ xem: %s %v", c.state, act, needs)
		}
		if ActionLabels[c.act] == "" {
			t.Fatalf("thiếu nhãn %s", c.act)
		}
	}
	for _, typ := range domain.Types {
		if TypeLabels[typ] == "" {
			t.Fatalf("thiếu nhãn loại %s", typ)
		}
	}
	id := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	if r := EntryRoute(id, "trong_trot", false); r != "/kn/khtc/phieu/"+id.String() {
		t.Fatal(r)
	}
	if r := EntryRoute(id, "trong_trot", true); r != "/kn/khtc/phieu/"+id.String()+"?department=trong_trot" {
		t.Fatal(r)
	}
}
