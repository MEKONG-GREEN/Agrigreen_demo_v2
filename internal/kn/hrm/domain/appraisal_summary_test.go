package domain

import "testing"

func TestAppraisalMS05MS06EightColumns_W1126(t *testing.T) {
	for _, kind := range []string{"MS05", "MS06"} {
		form, err := ValidateAppraisalSummary(DefaultAppraisalSummary(kind))
		if err != nil || len(form.Columns) != 8 || len(form.Ratings) != 4 || form.Signature == "" {
			t.Fatalf("%s: %+v %v", kind, form, err)
		}
		for _, c := range form.Columns {
			if c.Key == "points" || c.Key == "suggested" {
				t.Fatal("điểm phiếu đã lọt vào mẫu giấy")
			}
		}
	}
}

func TestAppraisalSummaryDefensiveCopyAndCloning_W1126(t *testing.T) {
	original := DefaultAppraisalSummary("MS05")
	cloned, err := ValidateAppraisalSummary(original)
	if err != nil {
		t.Fatal(err)
	}
	cloned.Code, cloned.Number = "ms05_moi", "07"
	cloned.Columns[1].Title = "Tên người được đánh giá"
	cloned.Ratings["HTNV"] = "Hoàn thành"
	if original.Columns[1].Title != "Họ và tên" || original.Ratings["HTNV"] != "Hoàn thành nhiệm vụ" {
		t.Fatal("sửa mẫu mới làm đổi bản cũ")
	}
	if _, err := ValidateAppraisalSummary(cloned); err != nil {
		t.Fatal(err)
	}
	fresh := DefaultAppraisalSummary("MS05")
	original.Columns[0].Title = "đổi"
	if fresh.Columns[0].Title != "STT" {
		t.Fatal("mẫu mặc định chia sẻ bộ nhớ")
	}
}

func TestAppraisalSummaryRejectsMissingOrDuplicatePaperColumns_W1126(t *testing.T) {
	for _, change := range []func(*AppraisalSummaryTemplate){
		func(f *AppraisalSummaryTemplate) { f.Columns = f.Columns[:7] },
		func(f *AppraisalSummaryTemplate) { f.Columns[1].Key = f.Columns[0].Key },
		func(f *AppraisalSummaryTemplate) { f.Columns[1].Key = "points" },
		func(f *AppraisalSummaryTemplate) { delete(f.Ratings, "HTNV") },
	} {
		form := DefaultAppraisalSummary("MS05")
		change(&form)
		if _, err := ValidateAppraisalSummary(form); err == nil {
			t.Fatal("mẫu sai vẫn được chấp nhận")
		}
	}
}
