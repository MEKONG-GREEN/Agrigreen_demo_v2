package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/xuri/excelize/v2"
	"strings"
	"testing"
)

// Synthetic multi-period input: never imports historical results from the official workbook.
func TestWeeklyImportSelectsOnePeriod284(t *testing.T) {
	data := []byte("Tên hạng mục,Tiến độ thực hiện tuần 25,Kinh phí tuần 25,Đánh giá tuần 25,Tiến độ thực hiện tuần 26,Kinh phí tuần 26,Đánh giá tuần 26,Lũy kế kinh phí tuần 25\r\nHoạt động fixture284 (lớp),2,100,Đánh giá fixture25,3,200,Đánh giá fixture26,1e100000\r\n")
	for _, tc := range []struct {
		week                        int
		progress, money, evaluation string
	}{
		{25, "2", "100", "Đánh giá fixture25"}, {26, "3", "200", "Đánh giá fixture26"}, {27, "3", "200", "Đánh giá fixture26"},
	} {
		rows, source, err := weeklyImportRows(data, "training", tc.week)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatal("expected one synthetic data row", rows)
		}
		if !strings.Contains(source, "Tiến độ:") || (tc.week == 27 && !strings.Contains(source, "dùng cặp tuần cuối")) {
			t.Fatal("preview must identify columns and fallback", source)
		}
		v := WeeklyView{Program: "training", Version: 1, Report: &domain.WeeklyProjection{WeeklyReport: domain.WeeklyReport{ID: "fixture-week", Number: tc.week}, Rows: []domain.WeeklyRow{{WeeklyItem: domain.WeeklyItem{ID: "fixture-item", Name: "Hoạt động fixture284 (lớp)"}}}}}
		preview, err := makeWeeklyPreview(v, data)
		if err != nil || preview.ValidRows != 1 || preview.ErrorRows != 0 || preview.SourceColumns != source {
			t.Fatalf("preview must identify the chosen period before confirm: %+v %v", preview, err)
		}
		p := preview.patches[0]
		if p.Progress == nil || *p.Progress != tc.progress || p.Money == nil || *p.Money != tc.money || p.Evaluation == nil || *p.Evaluation != tc.evaluation {
			t.Errorf("week %d must select one matching period: %+v", tc.week, p)
		}
	}
}

// Official sheets I/II place evaluation columns after all progress/money pairs.
// Values below are synthetic; no historical workbook results are imported.
func TestWeeklyImportDetachedEvaluations284(t *testing.T) {
	data := []byte("Tên hạng mục,Tiến độ thực hiện tuần 37,Kinh phí tuần 37,Tiến độ thực hiện tuần 38,Kinh phí tuần 38,Đánh giá tuần 37,Đánh giá tuần 38,Lũy kế kinh phí tuần 37\r\nHoạt động fixture284 (lớp),2,100,3,200,Fixture kỳ 37,Fixture kỳ 38,1e100000\r\n")
	for _, tc := range []struct {
		week                        int
		progress, money, evaluation string
	}{{37, "2", "100", "Fixture kỳ 37"}, {38, "3", "200", "Fixture kỳ 38"}, {39, "3", "200", "Fixture kỳ 38"}} {
		rows, _, err := weeklyImportRows(data, "training", tc.week)
		if err != nil || len(rows) != 1 {
			t.Fatalf("week%d rows: %+v %v", tc.week, rows, err)
		}
		p := rows[0].patch
		if p.Progress == nil || *p.Progress != tc.progress || p.Money == nil || *p.Money != tc.money || p.Evaluation == nil || *p.Evaluation != tc.evaluation {
			t.Fatalf("week%d must import detached evaluation of the same period: %+v", tc.week, p)
		}
	}
	missing := []byte("Tên hạng mục,Tiến độ thực hiện tuần 37,Kinh phí tuần 37,Tiến độ thực hiện tuần 38,Kinh phí tuần 38,Đánh giá tuần 37,Lũy kế\r\nHoạt động fixture284 (lớp),2,100,3,200,Chỉ thuộc kỳ 37,1e100000\r\n")
	rows, _, err := weeklyImportRows(missing, "training", 38)
	if err != nil || len(rows) != 1 || rows[0].patch.Evaluation != nil {
		t.Fatalf("missing evaluation must preserve existing text, never use another week's column: %+v %v", rows, err)
	}
}

func TestWeeklyImportOfficialMultiSheetShape284(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if err := f.SetSheetName("Sheet1", "I"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.NewSheet("II"); err != nil {
		t.Fatal(err)
	}
	for _, sheet := range []string{"I", "II"} {
		header := []string{"TT", "TÊN CHƯƠNG TRÌNH KHUYẾN NÔNG", "QUY MÔ", "TIẾN ĐỘ THỰC HIỆN TUẦN 37", "", "TIẾN ĐỘ THỰC HIỆN TUẦN 38", "", "ĐÁNH GIÁ TUẦN 37", "ĐÁNH GIÁ TUẦN 38", "LŨY KẾ tiến độ tuần 37", "LŨY KẾ kinh phí"}
		for c, value := range header {
			ref, _ := excelize.CoordinatesToCellName(c+1, 1)
			if err := f.SetCellStr(sheet, ref, value); err != nil {
				t.Fatal(err)
			}
		}
		for _, pair := range [][2]string{{"D1", "E1"}, {"F1", "G1"}} {
			if err := f.MergeCell(sheet, pair[0], pair[1]); err != nil {
				t.Fatal(err)
			}
		}
		title, name := "Chương trình thông tin", "Khác chương trình fixture284 (lớp)"
		if sheet == "II" {
			title = "Chương trình đào tạo huấn luyện và hợp tác"
			name = "Hoạt động fixture284 (lớp)"
		}
		for ref, value := range map[string]string{"A2": sheet, "B2": title, "D2": "Tiến độ thực hiện", "E2": "Kinh phí thực hiện (đồng)", "F2": "Tiến độ thực hiện", "G2": "Kinh phí thực hiện (đồng)", "A3": "1", "B3": name, "C3": "12", "D3": "2", "E3": "100", "F3": "3", "G3": "200", "H3": "Fixture kỳ 37", "I3": "Fixture kỳ 38"} {
			if err := f.SetCellStr(sheet, ref, value); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.SetCellFormula(sheet, "J3", "1/0"); err != nil {
			t.Fatal(err)
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	rows, source, err := weeklyImportRows(buf.Bytes(), "training", 37)
	if err != nil || len(rows) != 1 || rows[0].name != "Hoạt động fixture284 (lớp)" {
		t.Fatalf("must choose sheet II and skip the program/subheader: %+v %v", rows, err)
	}
	p := rows[0].patch
	if p.Progress == nil || *p.Progress != "2" || p.Money == nil || *p.Money != "100" || p.Evaluation == nil || *p.Evaluation != "Fixture kỳ 37" || !strings.Contains(source, "H ĐÁNH GIÁ TUẦN 37") {
		t.Fatalf("actual two-tier/detached-column shape: %+v %s", p, source)
	}
}
