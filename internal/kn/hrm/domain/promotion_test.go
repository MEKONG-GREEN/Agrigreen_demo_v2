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
	return PromoSubject{StaffID: uuid.New(), Snap: Snapshot{Status: StatusActive, Grade: "01.003", Step: step, StepSince: since},
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
	if NLTHQuota(49, decimal.NewFromInt(10)) != 4 {
		t.Fatal("chỉ tiêu ⌊10 % × 49⌋")
	}
}

func TestComputeNLTX_W1125(t *testing.T) {
	s := sub(5, "2023-11-01")
	cs := Compute(s, testRules(), 2026)
	if len(cs) != 1 || cs[0].Kind != PromoNLTX || !cs[0].Due.Equal(d("2026-11-01")) || cs[0].Diff.String() != "0.33" || cs[0].Cond != CondOK {
		t.Fatalf("NLTX: %+v", cs)
	}
	// AC-01: KHTNV → Chưa đủ; thiếu năm → Cần xem xét.
	s.Ratings[2024] = "KHTNV"
	if c := Compute(s, testRules(), 2026)[0]; c.Cond != CondFail || !strings.Contains(strings.Join(c.Why, ","), "KHTNV 2024") {
		t.Fatalf("KHTNV: %+v", c.Why)
	}
	delete(s.Ratings, 2024)
	if c := Compute(s, testRules(), 2026)[0]; c.Cond != CondReview {
		t.Fatalf("thiếu xếp loại: %v", c.Cond)
	}
	// Kỷ luật trong thời gian giữ bậc kéo dài 6 tháng; đang thi hành tại ngày đến kỳ → Chưa đủ.
	s = sub(5, "2023-11-01")
	to := d("2027-12-31")
	s.Disciplines = []Event{{Kind: EventDiscipline, ValidFrom: d("2025-01-01"), ValidTo: &to, Payload: map[string]string{"form": "canh_cao"}}}
	if c := Compute(s, testRules(), 2026)[0]; !c.Due.Equal(d("2027-05-01")) || c.Cond != CondFail {
		t.Fatalf("kỷ luật: %v %v", c.Due, c.Cond)
	}
	// Thiếu mốc / bảng hệ số → không tính.
	s = sub(5, "")
	if len(Compute(s, testRules(), 2026)) != 0 {
		t.Fatal("thiếu mốc xếp bậc")
	}
}

func TestComputeTNVKAndNLTH_W1125(t *testing.T) {
	s := sub(9, "2023-06-01")
	c := Compute(s, testRules(), 2026)[0]
	if c.Kind != PromoTNVK || !c.Due.Equal(d("2026-06-01")) || c.PctTo.String() != "5" || c.Diff.String() != "0.25" {
		t.Fatalf("TNVK lần đầu: %+v", c)
	}
	s.TNVK = &TNVKState{Pct: decimal.NewFromInt(5), Since: d("2026-06-01")}
	if c := Compute(s, testRules(), 2026)[0]; !c.Due.Equal(d("2027-06-01")) || c.PctTo.String() != "6" || c.PctFrom.String() != "5" {
		t.Fatalf("TNVK lần sau: %+v", c)
	}
	n := sub(3, "2024-02-01")
	aw := PromoAward{EventID: uuid.New(), Form: "bang_khen_ubnd_tinh", Name: "Bằng khen UBND tỉnh", Year: 2024, Months: 12}
	n.Awards, n.Proposals = []PromoAward{aw}, []uuid.UUID{aw.EventID}
	cs := Compute(n, testRules(), 2026)
	if len(cs) != 2 || cs[1].Kind != PromoNLTH || !cs[1].Due.Equal(d("2026-02-01")) || cs[1].Planned != 0 || len(cs[1].Blocked) != 0 {
		t.Fatalf("NLTH: %+v", cs)
	}
	n.Awards[0].Used = true
	if c := Compute(n, testRules(), 2026)[1]; c.Cond != CondFail || len(c.Blocked) == 0 {
		t.Fatalf("thành tích đã dùng: %+v", c)
	}
	if c := Compute(n, testRules(), 2025)[1]; c.Planned != 2026 {
		t.Fatalf("dự kiến năm sau: %+v", c.Planned)
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
