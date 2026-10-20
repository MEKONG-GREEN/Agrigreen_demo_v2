package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParseValue(t *testing.T) {
	for raw, want := range map[string]string{"48215": "48215", "48.215,5": "48215.5", "26 400": "26400", "1.234.567": "1234567", "-3.25": "-3.25", "0,1234": "0.1234"} {
		got, err := ParseValue(raw)
		if err != nil || got.String() != want {
			t.Fatalf("ParseValue(%q) = %v, %v; muốn %s", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "abc", "1.23456", "1e20"} {
		if _, err := ParseValue(raw); err == nil {
			t.Fatalf("ParseValue(%q) phải lỗi", raw)
		}
	}
}

func TestFigureInputRequiresSource(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	in := FigureInput{PeriodID: uuid.New(), IndicatorID: uuid.New(), Value: "12450"}
	_, err := in.Validate(now)
	var fe FieldErrors
	if !errors.As(err, &fe) || !errors.Is(err, ErrInput) || len(fe) != 3 {
		t.Fatalf("thiếu nguồn phải báo 3 lỗi (cơ quan, văn bản, ngày): %v", err)
	}
	in.Agency, in.Document, in.SourceDate = "Cục Thống kê", "285/BC-CTK", "28/09/2026"
	f, err := in.Validate(now)
	if err != nil || f.Source.Date.Format(time.DateOnly) != "2026-09-28" || f.Value.String() != "12450" {
		t.Fatalf("hợp lệ: %+v %v", f, err)
	}
	in.SourceDate = "2026-10-21"
	if _, err := in.Validate(now); err == nil {
		t.Fatal("ngày nguồn sau hôm nay phải lỗi")
	}
	in.SourceDate, in.Reason = "2026-09-28", "ngắn"
	if _, err := in.Validate(now); err == nil {
		t.Fatal("lý do dưới 10 ký tự phải lỗi")
	}
	if !NeedsReason(true, "") || NeedsReason(false, "") || NeedsReason(true, "Chi cục cập nhật số liệu") {
		t.Fatal("NeedsReason sai")
	}
}

func TestPeriodLabel(t *testing.T) {
	cases := map[string]string{
		"Tuần 39 năm 2026 (từ ngày 23/09/2026 đến ngày 29/09/2026)": "tuần 39 năm 2026",
		"Tuần 39/2026":   "tuần 39 năm 2026",
		"tuần 09 / 2026": "tuần 9 năm 2026",
		"Quý III/2026":   "quý iii năm 2026",
		"Năm 2026 (số liệu đến ngày 15/12/2026)": "năm 2026",
	}
	for in, want := range cases {
		if got := PeriodLabel(in); got != want {
			t.Fatalf("PeriodLabel(%q) = %q; muốn %q", in, got, want)
		}
	}
}

// KH-FN-TI-02.11 tiêu chí 1 (miền thuần): tệp tuần 39 có 4 chỉ tiêu, 1 dòng thiếu nguồn → ghi nhận 3, báo lỗi 1.
func TestCheckRowsWeek39(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	w39 := PeriodRef{ID: uuid.New(), Type: "week", Year: 2026, Key: "2026-W39", Name: "Tuần 39 năm 2026 (từ ngày 23/09/2026 đến ngày 29/09/2026)"}
	closed := PeriodRef{ID: uuid.New(), Type: "week", Year: 2026, Key: "2026-W38", Name: "Tuần 38 năm 2026 (…)", Closed: true}
	inds := []IndicatorRef{{ID: uuid.New(), Code: "ng-lua", Name: "Diện tích lúa Thu Đông", Unit: "ha"}, {ID: uuid.New(), Code: "ng-tom", Name: "Số hộ nuôi tôm càng xanh", Unit: "hộ"},
		{ID: uuid.New(), Code: "ng-htx", Name: "Số HTX tham gia chuỗi lúa CLC", Unit: "HTX"}, {ID: uuid.New(), Code: "ng-gao", Name: "Giá trị xuất khẩu gạo", Unit: "triệu USD"}}
	rows := []RawRow{
		{Line: 2, Indicator: "ng-lua", Period: "Tuần 39/2026", Value: "12450", Agency: "Cục Thống kê", Document: "file tổng hợp", SourceDate: "2026-09-29"},
		{Line: 3, Indicator: "Số hộ nuôi tôm càng xanh", Period: "2026-W39", Value: "812", Agency: "Chi cục Thủy sản", Document: "email", SourceDate: "2026-09-29"},
		{Line: 4, Indicator: "ng-htx", Period: "tuần 39 năm 2026", Value: "34", Agency: "Sở NN&MT", Document: "CV 512", SourceDate: "2026-09-29"},
		{Line: 5, Indicator: "ng-gao", Period: "Tuần 39/2026", Value: "210", Agency: "", Document: "", SourceDate: ""},
	}
	res := CheckRows(rows, []PeriodRef{w39, closed}, inds, map[string]string{FigureKey(w39.ID, inds[2].ID): "30"}, now)
	if res.Valid != 2 || res.Overwrites != 1 || res.Errors != 1 {
		t.Fatalf("muốn 2 ghi mới + 1 ghi đè + 1 lỗi: %+v", res)
	}
	if res.Rows[0].Unit != "ha" || res.Rows[0].Normalized != "12450" || res.Rows[2].Current == nil || *res.Rows[2].Current != "30" {
		t.Fatalf("dòng chuẩn hóa sai: %+v", res.Rows)
	}
	if res.Rows[3].Status != RowError || len(res.Rows[3].Errors) != 3 {
		t.Fatalf("dòng 5 thiếu nguồn phải lỗi: %+v", res.Rows[3])
	}
	more := CheckRows([]RawRow{
		{Line: 2, Indicator: "không có", Period: "Tuần 39/2026", Value: "1", Agency: "a", Document: "b", SourceDate: "2026-09-29"},
		{Line: 3, Indicator: "ng-lua", Period: "2026-W38", Value: "1", Agency: "a", Document: "b", SourceDate: "2026-09-29"},
		{Line: 4, Indicator: "ng-lua", Period: "", Value: "x", Agency: "a", Document: "b", SourceDate: "2026-09-29"},
		{Line: 5, Indicator: "ng-lua", Period: "2026-W39", Value: "1", Agency: "a", Document: "b", SourceDate: "2026-09-29"},
		{Line: 6, Indicator: "ng-lua", Period: "2026-W39", Value: "2", Agency: "a", Document: "b", SourceDate: "2026-09-29"},
	}, []PeriodRef{w39, closed}, inds, nil, now)
	if more.Errors != 4 || more.Valid != 1 {
		t.Fatalf("chỉ tiêu ngoài danh mục, kỳ đóng, thiếu kỳ, trùng dòng phải lỗi: %+v", more)
	}
}
