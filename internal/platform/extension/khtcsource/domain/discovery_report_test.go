package domain_test

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"slices"
	"testing"
)

func TestKH_ReportKeysDoNotRequireProgramPlan(t *testing.T) {
	periods := []port.KHTCPeriod{{Departments: []port.DepartmentFigures{
		{DepartmentCode: "chan_nuoi_thuy_san", Forms: []port.TargetFigure{{FormCode: "CNTS-BM04", Last: map[string]string{"tong_dan_heo": "100", "tiem_ta_heo_cp": "50"}}, {FormCode: "CNTS-BM17", Sum: map[string]string{"ca": "12", "tom": "3"}}, {FormCode: "CNTS-BM18", Last: map[string]string{"tong_san_luong_luy_ke_nam": "1000", "tong_san_luong_luy_ke_quy": "300", "nuoi_trong_dien_tich_luy_ke_nam": "50"}}}},
		{DepartmentCode: "trong_trot", Forms: []port.TargetFigure{{FormCode: "T01", Last: map[string]string{"lua_td/luy_ke": "200", "lua_td/kh_tinh_giao": "300", "lua_td/ty_le": "66.7"}}}},
		{DepartmentCode: "thong_tin_truyen_thong", Forms: []port.TargetFigure{{FormCode: "PRICE-BOARD", Last: map[string]string{"MH01/price_min": "8000", "MH01/price_avg": "9000"}}, {FormCode: "PRICE-TICKETS", Sum: map[string]string{"tickets_collected": "12", "days_approved": "3"}}}},
	}}}
	got := domain.ReportKeys(periods)
	want := []string{"cum:chan_nuoi_thuy_san:cnts-bm18:tong_san_luong_luy_ke_nam", "cum:chan_nuoi_thuy_san:cnts-bm18:nuoi_trong_dien_tich_luy_ke_nam", "cum:chan_nuoi_thuy_san:cnts-bm04:tong_dan_heo", "sum:chan_nuoi_thuy_san:cnts-bm17:ca", "sum:chan_nuoi_thuy_san:cnts-bm17:tom", "season:trong_trot:t01:lua_td:luy_ke", "price:thong_tin_truyen_thong:price-board:mh01", "sum:thong_tin_truyen_thong:price-tickets:tickets_collected"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("production stocks/flows only; no plan/ratio/vaccine summing: got %v want %v", got, want)
	}
}
