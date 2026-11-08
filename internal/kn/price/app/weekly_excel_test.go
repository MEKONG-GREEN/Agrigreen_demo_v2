package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
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
