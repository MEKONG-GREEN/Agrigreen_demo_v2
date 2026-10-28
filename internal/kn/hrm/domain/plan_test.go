package domain

import (
	"strings"
	"testing"
	"time"
)

func TestPlanValuesRoundTrip_W1127(t *testing.T) {
	v := PlanValues{Current: 12, Retiring: 2, PlanInput: PlanInput{Leaving: 1, Additional: 3, Note: "ghi chú",
		Positions: []PlanPosition{{Title: "Kỹ sư", Level: "Đại học", Count: 2, When: "Quý I/2027", Reason: "thay người"}}}}
	if v.Demand() != 12 {
		t.Fatalf("nhu cầu = hiện có − nghỉ hưu − nghỉ khác + bổ sung: %d", v.Demand())
	}
	m, err := v.FormValues()
	if err != nil {
		t.Fatal(err)
	}
	if issues := ValidatePlanForm(PlanTemplateCode, m); len(issues) != 0 {
		t.Fatalf("validator: %v", issues)
	}
	back, err := PlanValuesFromForm(m)
	if err != nil || back.Current != 12 || back.Positions[0].Title != "Kỹ sư" || back.Note != "ghi chú" {
		t.Fatalf("đọc lại: %+v %v", back, err)
	}
	if ValidatePlanForm("KHAC", m) == nil {
		t.Fatal("biểu khác phải bị chặn")
	}
	m[PlanPositions] = `[{"title":"x","count":0}]`
	if ValidatePlanForm(PlanTemplateCode, m) == nil {
		t.Fatal("số lượng 0 phải bị chặn")
	}
	m[PlanPositions] = `[{"title":"x","count":1,"la":1}]`
	if ValidatePlanForm(PlanTemplateCode, m) == nil {
		t.Fatal("trường lạ phải bị chặn")
	}
}

func TestNormalizePlanInput_W1127(t *testing.T) {
	cases := map[string]PlanInput{
		"âm":        {Leaving: -1},
		"quá trần":  {Additional: 10000},
		"vị trí":    {Positions: []PlanPosition{{Title: " ", Count: 1}}},
		"ghi chú":   {Note: strings.Repeat("a", 501)},
		"11 vị trí": {Positions: make([]PlanPosition, 11)},
	}
	for name, in := range cases {
		if _, err := NormalizePlanInput(in); err == nil {
			t.Errorf("%s: phải lỗi", name)
		}
	}
	out, err := NormalizePlanInput(PlanInput{Positions: []PlanPosition{{Title: "  Kế toán ", Count: 1}}, Note: "  x "})
	if err != nil || out.Positions[0].Title != "Kế toán" || out.Note != "x" {
		t.Fatalf("chuẩn hóa: %+v %v", out, err)
	}
}

func TestRetiringIn_W1127(t *testing.T) {
	r := RetirementAges{MaleMonths: 61*12 + 6, FemaleMonths: 57 * 12}
	b := func(s string) *time.Time { d, _ := time.Parse(time.DateOnly, s); return &d }
	for _, c := range []struct {
		p           Person
		ret, known  bool
		description string
	}{
		{Person{Gender: "nu", Birth: b("1970-05-01")}, true, true, "nữ đủ 57 tuổi 05/2027"},
		{Person{Gender: "nu", Birth: b("1971-01-01")}, false, true, "nữ đủ tuổi 2028"},
		{Person{Gender: "nam", Birth: b("1966-01-15")}, true, true, "nam đủ 61 tuổi 6 tháng 07/2027"},
		{Person{Gender: "nam", Birth: b("1960-01-01")}, true, true, "đã quá tuổi vẫn công tác"},
		{Person{Birth: b("1960-01-01")}, false, false, "thiếu giới tính"},
		{Person{Gender: "nu"}, false, false, "thiếu ngày sinh"},
	} {
		ret, known := r.RetiringIn(c.p, 2027)
		if ret != c.ret || known != c.known {
			t.Errorf("%s: %v %v", c.description, ret, known)
		}
	}
}

func TestPlanStatus_W1127(t *testing.T) {
	for _, c := range []struct {
		closed   bool
		versions int64
		decision string
		draft    bool
		want     string
	}{
		{false, 0, "", false, PlanStatusNone},
		{false, 0, "", true, PlanStatusDraft},
		{false, 1, "", false, PlanStatusSubmitted},
		{false, 1, "returned", false, PlanStatusReturned},
		{false, 1, "returned", true, PlanStatusDraft},
		{false, 2, "accepted", false, PlanStatusAccepted},
		{true, 0, "", true, PlanStatusMissed},
		{true, 2, "accepted", false, PlanStatusClosed},
	} {
		if got := PlanStatus(c.closed, c.versions, c.decision, c.draft); got != c.want {
			t.Errorf("%+v: %s", c, got)
		}
	}
}
