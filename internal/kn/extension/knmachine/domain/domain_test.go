package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestParseQuantity(t *testing.T) {
	for in, want := range map[string]int{"1": 1, " 25 ": 25, "1000000": 1_000_000, "007": 7} {
		if got, err := ParseQuantity(in); err != nil || got != want {
			t.Errorf("%q: %d, %v (muốn %d)", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "-1", "1.5", "2.0", "1e3", "1e100000", "1e-100000", "+5", "abc", "١٢", "0x10", strings.Repeat("9", 100_000) + "x"} {
		if _, err := ParseQuantity(in); !errors.Is(err, ErrQuantityNotInteger) {
			t.Errorf("%.20q: phải bị chặn là không phải số nguyên > 0, nhận %v", in, err)
		}
	}
	for _, in := range []string{"1000001", "9999999", "12345678", strings.Repeat("9", 100_000)} {
		if _, err := ParseQuantity(in); !errors.Is(err, ErrQuantityRange) {
			t.Errorf("%.20q: phải bị chặn vì vượt 1.000.000, nhận %v", in, err)
		}
	}
}

func TestPlan(t *testing.T) {
	cur := map[string]Line{"MLD": {MachineType: "MLD", Quantity: 2, Note: "cũ"}, "MGD": {MachineType: "MGD", Quantity: 1}}
	got := Plan(cur, []Line{{MachineType: "MLD", Quantity: 3, Note: "cũ"}, {MachineType: "MC", Quantity: 4}})
	if len(got) != 3 || got[0].MachineType != "MC" || got[0].Action != Set || got[0].PrevQuantity != nil ||
		got[1].MachineType != "MGD" || got[1].Action != Removed || got[1].Quantity != 0 || *got[1].PrevQuantity != 1 ||
		got[2].MachineType != "MLD" || got[2].Action != Set || got[2].Quantity != 3 || *got[2].PrevQuantity != 2 {
		t.Fatalf("kế hoạch ghi: %+v", got)
	}
	if ch := Plan(cur, []Line{{MachineType: "MLD", Quantity: 2, Note: "cũ"}, {MachineType: "MGD", Quantity: 1}}); len(ch) != 0 {
		t.Fatalf("không đổi thì không ghi: %+v", ch)
	}
	if d := DuplicateType([]Line{{MachineType: "MLD"}, {MachineType: "MC"}, {MachineType: "MLD"}}); d != "MLD" {
		t.Fatalf("loại trùng: %q", d)
	}
	if !ValidNote(strings.Repeat("ă", MaxNote)) || ValidNote(strings.Repeat("ă", MaxNote+1)) {
		t.Fatal("giới hạn ghi chú 500 ký tự")
	}
}
