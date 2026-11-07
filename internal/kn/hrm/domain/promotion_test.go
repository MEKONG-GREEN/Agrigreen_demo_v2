package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func d(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

func testRules() PromotionRules {
	return PromotionRules{MinRating: "HTNV", RatingYears: 3, AchievementYears: 5, DisciplineExtensionMonths: map[string]int{"canh_cao": 6},
		NLTHQuotaPct: decimal.NewFromInt(10), TNVKStartPct: decimal.NewFromInt(5), TNVKStepPct: decimal.NewFromInt(1), TNVKCycleMonths: 12,
		TNVKFirstMonths: 36, RemindMonths: []int{1, 3, 6}}
}

func sub(step int, since string) PromoSubject {
	return PromoSubject{StaffID: uuid.New(), Snap: Snapshot{Status: StatusActive, Grade: "01.003", Step: step, StepSince: since, PromotionMonths: 36},
		Coefficients: coefs("2.34", "0.33", 9), HoldMonths: 36, Ratings: map[int]string{2023: "HTNV", 2024: "HTNV", 2025: "HTTNV"}}
}

func TestAddMonthsAndKeys_W1125(t *testing.T) {
	if got := AddMonths(d("2026-01-31"), 1); !got.Equal(d("2026-02-28")) {
		t.Fatalf("31/01 + 1: %v", got)
	}
	if got := AddMonths(d("2027-02-01"), -12); !got.Equal(d("2026-02-01")) {
		t.Fatalf("NL03-03: %v", got)
	}
	id := uuid.New()
	k := CaseKey(id, PromoNLTX, 6, d("2026-11-01"), uuid.Nil)
	if s, kind, ok := ParseCaseKey(k); !ok || s != id || kind != PromoNLTX || !strings.HasSuffix(k, ":nltx:b6-20261101") {
		t.Fatalf("khóa kỳ: %s", k)
	}
	if _, _, ok := ParseCaseKey("x:nltx:1"); ok {
		t.Fatal("khóa sai dạng")
	}
	if MonthsEnjoyed(d("2026-11-16")) != "0.5" || MonthsEnjoyed(d("2026-11-01")) != "1" {
		t.Fatal("số tháng được hưởng")
	}
}

func TestComputeRegularPersonalMonthsV4(t *testing.T) {
	for _, tc := range []struct {
		months int
		due    string
	}{{36, "2028-03-01"}, {24, "2027-03-01"}, {1, "2025-04-01"}, {120, "2035-03-01"}} {
		s := sub(5, "2025-03-01")
		s.Snap.PromotionMonths = tc.months
		s.HoldMonths = 99
		cs := Compute(s, testRules(), 2026)
		if len(cs) != 1 || !cs[0].Due.Equal(d(tc.due)) || cs[0].Diff.String() != "0.33" || len(cs[0].Basis) != 2 {
			t.Fatalf("NB07 %d: %+v", tc.months, cs)
		}
	}
	for _, since := range []string{"", "2025", "2025-03"} {
		s := sub(5, since)
		if len(Compute(s, testRules(), 2026)) != 0 {
			t.Fatal("missing full NB06")
		}
	}
	for _, months := range []int{0, -1, 121} {
		s := sub(5, "2025-03-01")
		s.Snap.PromotionMonths = months
		if len(Compute(s, testRules(), 2026)) != 0 {
			t.Fatal("missing/invalid NB07")
		}
	}
	s := sub(5, "2024-01-31")
	s.Snap.PromotionMonths = 1
	if cs := Compute(s, testRules(), 2026); len(cs) != 1 || !cs[0].Due.Equal(d("2024-02-29")) {
		t.Fatal("end-of-month clamp")
	}
}

func TestPromotionNumbersV4(t *testing.T) {
	for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 10000), "0", "121", "1.0", "-1"} {
		if _, ok := ValidPromotionMonths(raw); ok {
			t.Fatal("NB07 accepted malformed numeric")
		}
		_, _, err := NormalizeEvent(StaffVienChuc, EventInput{Kind: EventGrade, ValidFrom: "2025-01-01", Payload: map[string]string{"grade": "01.003", "step": "1", "promotion_months": raw}})
		if err == nil {
			t.Fatal("event accepted malformed NB07")
		}
	}
	for _, raw := range []string{"1", "36", "120"} {
		if _, ok := ValidPromotionMonths(raw); !ok {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 10000)} {
		if _, ok := ValidMonthsEnjoyed(raw); ok {
			t.Fatal("months accepted malformed numeric")
		}
	}
}

func TestBuildForm_W1125(t *testing.T) {
	for form, n := range map[string]int{"mau1": 20, "mau2": 20, "mau4": 17} {
		tb := BuildForm(form, d("2026-07-01"), d("2026-07-01"), []ExportItem{{StaffID: uuid.New(), FromCoeff: "3.86", ToCoeff: "4.06",
			Diff: "0.20", StepSince: "2023-07-01", NextDue: "2026-07-01", MonthsEnjoyed: "1"}}, nil)
		if len(tb.Columns) != n || len(tb.Rows[0].Cells) != n {
			t.Fatalf("%s: %d cột", form, len(tb.Columns))
		}
		if form == "mau1" && (tb.Rows[0].Cells[18] != "" || tb.Rows[0].Cells[16] != "0,20") {
			t.Fatalf("Mẫu số 1: %v", tb.Rows[0].Cells)
		}
	}
	if FormFileName("mau4", d("2026-07-01")) != "MauSo4_T07_2026.xlsx" {
		t.Fatal("tên tệp")
	}
}
