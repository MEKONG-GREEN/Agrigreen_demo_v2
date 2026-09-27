package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestDueAtDecimalHours(t *testing.T) {
	c := time.Date(2026, 9, 15, 3, 0, 0, 0, time.UTC)
	if got := DueAt(c, decimal.RequireFromString("24")); !got.Equal(c.Add(24 * time.Hour)) {
		t.Fatal(got)
	}
	if got := DueAt(c, decimal.RequireFromString("0.25")); !got.Equal(c.Add(15 * time.Minute)) {
		t.Fatal(got)
	}
}

// US-TASK-02 AC-1: chỉ đóng khi có kết quả; không thao tác trên nhiệm vụ đã đóng.
func TestTransitions(t *testing.T) {
	cases := []struct {
		cur    Status
		a      Action
		note   string
		want   Status
		wantOK bool
	}{
		{StatusNew, ActionAssigned, "", StatusAssigned, true},
		{StatusNew, ActionAccepted, "", "", false},
		{StatusAssigned, ActionAccepted, "", StatusAccepted, true},
		{StatusAccepted, ActionProgress, "", "", false},
		{StatusAccepted, ActionProgress, "đang xử lý", StatusInProgress, true},
		{StatusInProgress, ActionResolved, "ngắn", "", false},
		{StatusInProgress, ActionResolved, "Đã hướng dẫn phun thuốc", StatusResolved, true},
		{StatusResolved, ActionEscalated, "", "", false},
		{StatusAccepted, ActionEscalated, "", StatusAccepted, true},
		{StatusResolved, ActionReopened, "Nông dân báo vẫn còn rầy", StatusAssigned, true},
		{StatusResolved, ActionAssigned, "", "", false},
	}
	for _, c := range cases {
		got, err := Transition(c.cur, c.a, c.note, true)
		if (err == nil) != c.wantOK || got != c.want {
			t.Errorf("%s --%s--> %s, %v; muốn %s ok=%v", c.cur, c.a, got, err, c.want, c.wantOK)
		}
	}
}

func TestContainsPII(t *testing.T) {
	for _, s := range []string{"gọi 0912345678", "SĐT +84 912 345 678", "CCCD 079123456789", "0987.654.321"} {
		if !ContainsPII(s) {
			t.Errorf("phải phát hiện: %q", s)
		}
	}
	for _, s := range []string{"Rầy nâu mật độ 3000 con/m2 trên 12,5 ha", "Vụ Hè Thu 2026, giống OM5451"} {
		if ContainsPII(s) {
			t.Errorf("không phải PII: %q", s)
		}
	}
}

// US-DIR-01/03: tự khai còn hạn thắng; hết hạn → theo ca; ngoài ca → không xác định.
func TestDutyAt(t *testing.T) {
	now := time.Date(2026, 9, 15, 3, 0, 0, 0, time.UTC)
	m := &ManualStatus{Status: DutyOff, SetAt: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour)}
	shift := []Shift{{StartsAt: now.Add(-2 * time.Hour), EndsAt: now.Add(2 * time.Hour)}}
	if s, src, _ := DutyAt(now, m, shift); s != DutyOff || src != DutySourceManual {
		t.Fatal(s, src)
	}
	if s, src, _ := DutyAt(now.Add(90*time.Minute), m, shift); s != DutyOn || src != DutySourceShift {
		t.Fatal(s, src)
	}
	if s, _, _ := DutyAt(now.Add(3*time.Hour), m, shift); s != DutyUnknown {
		t.Fatal(s)
	}
}

func TestAckRate(t *testing.T) {
	if AckRate(1, 0) != nil {
		t.Fatal("không có đối tượng → nil")
	}
	if r := AckRate(1, 3); r.String() != "0.3333" {
		t.Fatal(r)
	}
}
