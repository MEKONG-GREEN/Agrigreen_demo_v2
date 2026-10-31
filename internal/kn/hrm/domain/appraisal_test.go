package domain

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func appraisalExample() AppraisalForm {
	f := AppraisalForm{Code: "phieu_xa", Name: "Phiếu cán bộ xã", Group: AppraisalCommune,
		Bands:     []AppraisalBand{{"81", "HTXSNV"}, {"63", "HTTNV"}, {"45", "HTNV"}, {"0", "KHTNV"}},
		Questions: []string{"Nhận xét kết quả công việc"}}
	for i, max := range []string{"10", "10", "15", "10", "20", "25"} {
		f.Criteria = append(f.Criteria, AppraisalCriterion{Code: "tc_" + strconv.Itoa(i), Title: "Tiêu chí " + strconv.Itoa(i+1), Maximum: max})
	}
	return f
}

func appraisalAnswers(total int64) []AppraisalAnswer {
	f := appraisalExample()
	out := make([]AppraisalAnswer, 0, len(f.Criteria))
	for _, c := range f.Criteria {
		maximum, _ := ParseAppraisalPoints(c.Maximum, AppraisalPointLimit)
		points := min(total, maximum)
		total -= points
		out = append(out, AppraisalAnswer{Code: c.Code, Points: AppraisalPoints(points)})
	}
	return out
}

func TestAppraisalPointsBeforeCalculation_W1126(t *testing.T) {
	for _, value := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), "100.01", "-1", "NaN", "Inf", "1.001", "0x10", ".1", "1.", "1,5"} {
		t.Run("reject-"+strconv.Itoa(len(value))+"-"+value[:min(12, len(value))], func(t *testing.T) {
			if _, err := ParseAppraisalPoints(value, 10000); err == nil {
				t.Fatal("đầu vào sai đã qua kiểm trước tính")
			}
		})
	}
	for _, tc := range []struct {
		text   string
		points int64
	}{{"0", 0}, {"0.01", 1}, {"100.00", 10000}, {" 99.99 ", 9999}} {
		got, err := ParseAppraisalPoints(tc.text, 10000)
		if err != nil || got != tc.points {
			t.Fatalf("%q: points=%d err=%v", tc.text, got, err)
		}
	}
	if _, err := ParseAppraisalPoints("1000000", AppraisalPointLimit); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAppraisalPoints("1000001", AppraisalPointLimit); err == nil {
		t.Fatal("vượt trần kỹ thuật")
	}
}

func TestAppraisal82Of90AndRatingBoundaries_W1126(t *testing.T) {
	for _, tc := range []struct {
		total  int64
		rating string
	}{{9000, "HTXSNV"}, {8200, "HTXSNV"}, {8100, "HTXSNV"}, {8099, "HTTNV"}, {6300, "HTTNV"}, {6299, "HTNV"}, {4500, "HTNV"}, {4499, "KHTNV"}, {0, "KHTNV"}} {
		t.Run(AppraisalPoints(tc.total), func(t *testing.T) {
			got, err := ScoreAppraisal(appraisalExample(), appraisalAnswers(tc.total), true)
			if err != nil || !got.Complete || got.Total != AppraisalPoints(tc.total) || got.Suggested != tc.rating {
				t.Fatalf("got=%+v err=%v", got, err)
			}
		})
	}
}

func TestAppraisalFormVersionDefensiveCopy_W1126(t *testing.T) {
	original := appraisalExample()
	original.Criteria[0].Maximum = "10.00"
	canonical, err := ValidateAppraisalForm(original, true)
	if err != nil {
		t.Fatal(err)
	}
	if original.Criteria[0].Maximum != "10.00" || canonical.Criteria[0].Maximum != "10" {
		t.Fatal("chuẩn hóa đã sửa mẫu nguồn hoặc không canonical")
	}
	canonical.Criteria[0].Title = "đổi"
	canonical.Bands[0].Rating = "KHTNV"
	canonical.Questions[0] = "đổi"
	if original.Criteria[0].Title == "đổi" || original.Bands[0].Rating != "HTXSNV" || original.Questions[0] == "đổi" {
		t.Fatal("phiên bản dùng chung slice với người gọi")
	}
}

func TestAppraisalFormRejectsUncoveredOrUnsafeScale_W1126(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*AppraisalForm)
	}{
		{"empty", func(f *AppraisalForm) { f.Criteria = nil }},
		{"duplicate-code", func(f *AppraisalForm) { f.Criteria[1].Code = f.Criteria[0].Code }},
		{"exponent-maximum", func(f *AppraisalForm) { f.Criteria[0].Maximum = "1e100000" }},
		{"long-maximum", func(f *AppraisalForm) { f.Criteria[0].Maximum = strings.Repeat("9", 100000) }},
		{"zero-maximum", func(f *AppraisalForm) { f.Criteria[0].Maximum = "0" }},
		{"missing-rating", func(f *AppraisalForm) { f.Bands = f.Bands[:3] }},
		{"uncovered-zero", func(f *AppraisalForm) { f.Bands[3].Minimum = "1" }},
		{"above-total", func(f *AppraisalForm) { f.Bands[0].Minimum = "91" }},
		{"duplicate-bound", func(f *AppraisalForm) { f.Bands[1].Minimum = "81" }},
		{"unsafe-bound", func(f *AppraisalForm) { f.Bands[0].Minimum = "1e-100000" }},
		{"wrong-rating-order", func(f *AppraisalForm) { f.Bands[0].Rating = "KHTNV" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := appraisalExample()
			tc.change(&f)
			if _, err := ValidateAppraisalForm(f, true); err == nil {
				t.Fatal("phiếu sai vẫn phát hành được")
			}
		})
	}
	draft := AppraisalForm{Code: "nhap", Name: "Phiếu nháp", Group: AppraisalCivil}
	if _, err := ValidateAppraisalForm(draft, false); err != nil {
		t.Fatal(err)
	}
}

func TestAppraisalScoringRejectsUnknownDuplicateAndOverMaximum_W1126(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func([]AppraisalAnswer) []AppraisalAnswer
		field  string
	}{
		{"above-criterion", func(a []AppraisalAnswer) []AppraisalAnswer { a[0].Points = "10.01"; return a }, "answers.0.points"},
		{"exponent", func(a []AppraisalAnswer) []AppraisalAnswer { a[0].Points = "1e100000"; return a }, "answers.0.points"},
		{"unknown", func(a []AppraisalAnswer) []AppraisalAnswer { a[0].Code = "other"; return a }, "answers"},
		{"duplicate", func(a []AppraisalAnswer) []AppraisalAnswer { a[1].Code = a[0].Code; return a }, "answers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ScoreAppraisal(appraisalExample(), tc.change(appraisalAnswers(8200)), false)
			var field AppraisalInputError
			if !errors.As(err, &field) || field.Field != tc.field {
				t.Fatalf("field=%+v err=%v", field, err)
			}
		})
	}
	answers := []AppraisalAnswer{{Code: "tc_0", Note: "Ghi nhận trước khi chấm"}}
	got, err := ScoreAppraisal(appraisalExample(), answers, false)
	if err != nil || got.Complete || got.Suggested != "" || !reflect.DeepEqual(got.Answers, answers) {
		t.Fatalf("nháp mất nhận xét hoặc gợi ý thiếu dữ liệu: %+v %v", got, err)
	}
	if _, err := ScoreAppraisal(appraisalExample(), answers, true); err == nil {
		t.Fatal("nộp phiếu thiếu điểm")
	}
}

func TestAppraisalProposalAndSummaryForms_W1126(t *testing.T) {
	for _, tc := range []struct {
		self, collective, reason string
		wantError                bool
	}{
		{"HTXSNV", "HTTNV", "", true}, {"HTTNV", "KHTNV", "", true}, {"HTNV", "HTTNV", "", false},
		{"HTTNV", "KHTNV", "Không hoàn thành nhiệm vụ được giao", false}, {"HTTNV", "", "", true}, {"bad", "HTNV", "", true},
	} {
		err := ValidateAppraisalProposal(tc.self, tc.collective, tc.reason, true)
		if (err != nil) != tc.wantError {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	if AppraisalSummaryCode(StaffVienChuc, true) != "MS06" || AppraisalSummaryCode(StaffVienChuc, false) != "MS05" || AppraisalSummaryCode(StaffCanBoXa, true) != "MS05" {
		t.Fatal("chia mẫu sai đối tượng")
	}
}
