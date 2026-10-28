package domain

import (
	"errors"
	"testing"
	"time"
)

// 31/12/2026 23:30 giờ Việt Nam = 16:30 UTC: năm hiện tại là 2026 dù UTC cùng ngày.
var lateDec = time.Date(2026, 12, 31, 16, 30, 0, 0, time.UTC)

// 31/12/2026 17:30 UTC = 01/01/2027 00:30 giờ Việt Nam: năm hiện tại đã là 2027.
var newYearVN = time.Date(2026, 12, 31, 17, 30, 0, 0, time.UTC)

func TestParseYearTT10(t *testing.T) {
	bad := map[string]error{
		"": ErrYearRequired, " ": ErrYearRequired, "2020.5": ErrYearInteger, "2020.0": ErrYearInteger, "abc": ErrYearInteger,
		"-2020": ErrYearRange, "0": ErrYearRange, "1899": ErrYearRange, "2027": ErrYearRange,
	}
	for raw, want := range bad {
		if _, err := ParseYear(raw, lateDec); !errors.Is(err, want) {
			t.Errorf("ParseYear(%q) = %v, want %v", raw, err, want)
		}
	}
	for _, raw := range []string{"1900", "2015", "2026"} {
		if _, err := ParseYear(raw, lateDec); err != nil {
			t.Errorf("ParseYear(%q): %v", raw, err)
		}
	}
	if y, err := ParseYear("2027", newYearVN); err != nil || y != 2027 {
		t.Errorf("năm hiện tại theo giờ VN: %d %v", y, err)
	}
}

func TestPlanMachines(t *testing.T) {
	cur := map[string]MachineLine{
		"MLD": {MachineType: "MLD", TypeName: "Máy làm đất", YearMade: 2015},
		"MGD": {MachineType: "MGD", TypeName: "Máy gặt đập", YearMade: 2018},
	}
	if got := PlanMachines(cur, []MachineLine{{MachineType: "MLD", YearMade: 2015}, {MachineType: "MGD", YearMade: 2018}}); len(got) != 0 {
		t.Fatalf("không đổi phải không ghi sổ: %+v", got)
	}
	got := PlanMachines(cur, []MachineLine{{MachineType: "MLD", TypeName: "Máy làm đất", YearMade: 2016}, {MachineType: "MBP", TypeName: "Máy bơm", YearMade: 2020}})
	if len(got) != 3 {
		t.Fatalf("cần 3 dòng: %+v", got)
	}
	if got[0].MachineType != "MBP" || got[0].Event != EventSet || *got[0].YearMade != 2020 {
		t.Errorf("thêm loại: %+v", got[0])
	}
	if got[1].MachineType != "MGD" || got[1].Event != EventRemoved || got[1].YearMade != nil || got[1].TypeName != "Máy gặt đập" {
		t.Errorf("bỏ loại: %+v", got[1])
	}
	if got[2].MachineType != "MLD" || got[2].Event != EventSet || *got[2].YearMade != 2016 {
		t.Errorf("đổi năm: %+v", got[2])
	}
}

func TestValidCode(t *testing.T) {
	for _, s := range []string{"lua", "MLD", "cay_an_qua", "rice.v2"} {
		if !ValidCode(s) {
			t.Errorf("%q phải hợp lệ", s)
		}
	}
	for _, s := range []string{"", " lua", "_x", "a b", "x;drop"} {
		if ValidCode(s) {
			t.Errorf("%q phải bị từ chối", s)
		}
	}
}
