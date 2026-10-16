package domain

import (
	"errors"
	"testing"
	"time"
)

func TestDefaultsValid(t *testing.T) {
	day := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	gs, us := DefaultGroups(day), DefaultUnits(day)
	if len(gs) != 5 {
		t.Fatalf("BRD FN-AD-04.03: 5 nhóm hàng mặc định, có %d", len(gs))
	}
	seen := map[string]bool{}
	for _, e := range append(gs, us...) {
		if err := Validate(e); err != nil {
			t.Errorf("%+v: %v", e, err)
		}
		k := string(e.Kind) + ":" + e.Code
		if seen[k] {
			t.Errorf("mã trùng %s", k)
		}
		seen[k] = true
	}
}

func TestValidateRejects(t *testing.T) {
	day := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	for _, e := range []Entry{
		{Kind: "item", Code: "x", Name: "x", ValidFrom: day},
		{Kind: KindUnit, Code: "KG", Name: "kg", ValidFrom: day},
		{Kind: KindUnit, Code: "a_very_long_unit_code_x", Name: "x", ValidFrom: day},
		{Kind: KindGroup, Code: "rau", Name: "", ValidFrom: day},
		{Kind: KindGroup, Code: "rau", Name: "Rau", SortOrder: -1, ValidFrom: day},
		{Kind: KindGroup, Code: "rau", Name: "Rau"},
	} {
		if err := Validate(e); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: muốn ErrInvalid, có %v", e, err)
		}
	}
}

// Ngày nghiệp vụ theo giờ VN: 18:00 UTC ngày 11 là 01:00 ngày 12 giờ VN.
func TestBusinessDay(t *testing.T) {
	got := BusinessDay(time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("BusinessDay = %v, muốn %v", got, want)
	}
}
