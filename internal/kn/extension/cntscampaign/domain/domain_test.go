package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPeriodKeyAndState(t *testing.T) {
	id := uuid.MustParse("01920000-0000-7000-8000-00000000abcd")
	k := PeriodKey(id, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if k != "DT-20261001-00abcd" || !IsCampaignKey(k) || len(k) > 20 {
		t.Fatalf("khóa: %q", k)
	}
	if IsCampaignKey("2026-W40") || IsCampaignKey("2026-10-05") {
		t.Fatal("khóa kỳ định kỳ không phải khóa đợt")
	}
	cases := []struct {
		acts []string
		want string
	}{
		{nil, StateDraft},
		{[]string{ActPublished, ActReminded, ActExtended}, StateCollecting},
		{[]string{ActPublished, ActClosed}, StateClosed},
		{[]string{ActPublished, ActClosed, ActReopened}, StateCollecting},
	}
	for _, c := range cases {
		if got := StateOf(c.acts); got != c.want {
			t.Fatalf("%v → %s, muốn %s", c.acts, got, c.want)
		}
	}
	if CanApply(StateDraft, ActClosed) || !CanApply(StateClosed, ActReopened) || CanApply(StateCollecting, ActPublished) {
		t.Fatal("máy trạng thái")
	}
}

func TestDraftValidate(t *testing.T) {
	ok := Draft{Name: "Điều tra tổng đàn 01/10/2026", Field: "chan_nuoi", TemplateCode: "CNTS-DT-TONGDAN", Units: []string{"X1"},
		From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC),
		DueAt: time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)}
	ok.Normalize()
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := ok
	bad.ScopeMode, bad.Regions = ScopeRegions, nil
	if err := bad.Validate(); err == nil {
		t.Fatal("khu vực rỗng phải lỗi")
	}
	bad = ok
	bad.To = ok.From.AddDate(0, 0, -1)
	if err := bad.Validate(); err == nil {
		t.Fatal("đến ngày trước từ ngày phải lỗi")
	}
	bad = ok
	bad.Field = "trong_trot"
	if err := bad.Validate(); err == nil {
		t.Fatal("lĩnh vực ngoài danh sách phải lỗi")
	}
	if _, ok := Reason("ngắn"); ok {
		t.Fatal("lý do < 10 ký tự")
	}
}
