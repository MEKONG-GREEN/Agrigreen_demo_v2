package extension

import (
	"slices"
	"testing"
	"time"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
)

// KN-STANDALONE-1-ADMIN-TOOLS (#264, Q5): màn /quan-tri/tham-so đã gỡ khỏi KN nên tham số KN chạy bằng giá trị nạp sẵn có
// phiên bản; giá trị đó phải đúng số BRD 2.0 (docs/kn/gia-dinh/KN-STANDALONE-1-ADMIN-TOOLS.md):
//   - CNTS v4.0 FN-10 BR-02, Mục III.5: lần ghi nhật ký chưa xác thực sửa trong 7 ngày.
//   - CNTS v4.0 Mục III.3.0 H04: tuần KSGM mặc định 1–7, 8–14, 15–21, 22–cuối tháng; BM-04/BM-17 tuần thứ Ba–thứ Hai,
//     tuần vắt quý/năm tính theo ngày cuối tuần.
//   - KHTC v2.3 Mục I.6, GD-05, BR-DL-06: lịch chung tuần Thứ Tư–Thứ Ba, tháng theo ngày kết thúc tuần.
func TestKNParamsMatchBRD2(t *testing.T) {
	seeds := map[string]string{}
	for _, s := range CNTSDiaryMasterdataParams().Seeds {
		if s.Value != nil {
			seeds[s.Key] = *s.Value
		}
	}
	for _, s := range MasterdataParams().Seeds {
		if s.Text != nil {
			seeds[s.Key] = *s.Text
		}
	}
	if seeds[extport.ParamCNTSDiaryEditWindowDays] != "7" {
		t.Fatalf("hạn sửa nhật ký CNTS phải 7 ngày (CNTS v4.0 FN-10 BR-02): %q", seeds[extport.ParamCNTSDiaryEditWindowDays])
	}
	global, err := domain.ParseRule(seeds["extension.report_period_rule"])
	if err != nil || global.WeekStart != time.Wednesday || global.MonthAlign != domain.MonthAlignWeekEnd {
		t.Fatalf("lịch chung phải tuần Thứ Tư–Thứ Ba, tháng theo ngày kết thúc tuần (KHTC v2.3 GD-05, BR-DL-06): %+v %v", global, err)
	}
	cnts, err := domain.ParseRule(seeds["extension.report_period_rule:chan_nuoi_thuy_san"])
	if err != nil || cnts.WeekStart != time.Tuesday || cnts.MonthAlign != domain.MonthAlignWeekEnd ||
		domain.CNTSRule.WeekStart != time.Tuesday || domain.KHTCRule.WeekStart != time.Wednesday {
		t.Fatalf("lịch CNTS phải tuần thứ Ba–thứ Hai (CNTS v4.0 H04): %+v %v", cnts, err)
	}
	if !slices.Equal(domain.DefaultKSGMStarts, []int{1, 8, 15, 22}) {
		t.Fatalf("tuần KSGM mặc định phải 1, 8, 15, 22 (CNTS v4.0 H04): %v", domain.DefaultKSGMStarts)
	}
}
