package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func fd(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// CGH FN-09 (dự báo, phương pháp tạm cgh.forecast/v1).
func TestProjectAreaFN09(t *testing.T) {
	// 2023: 100, 2024: 110 (+10%), 2025: 121 (+10%) → dự báo 2026 = 121 × 1,1 = 133,1.
	p := ProjectArea(map[int]decimal.Decimal{2023: fd("100"), 2024: fd("110"), 2025: fd("121")}, 2026)
	if p.Forecast == nil || !p.Forecast.Equal(fd("133.1")) || p.Capped || len(p.Rates) != 2 {
		t.Fatalf("tăng 10%%/năm: %+v", p)
	}
	// Tăng mạnh (+100%) → chặn +20%.
	p = ProjectArea(map[int]decimal.Decimal{2024: fd("50"), 2025: fd("100")}, 2026)
	if !p.Capped || !p.Forecast.Equal(fd("120")) {
		t.Fatalf("chặn +20%%: %+v", p)
	}
	// Giảm mạnh (−80%) → chặn −20%.
	p = ProjectArea(map[int]decimal.Decimal{2024: fd("500"), 2025: fd("100")}, 2026)
	if !p.Capped || !p.Forecast.Equal(fd("80")) {
		t.Fatalf("chặn −20%%: %+v", p)
	}
	// Chỉ có năm trước → tăng 0.
	p = ProjectArea(map[int]decimal.Decimal{2025: fd("42.5")}, 2026)
	if p.RawGrowth != nil || !p.Forecast.Equal(fd("42.5")) {
		t.Fatalf("không có lịch sử tăng: %+v", p)
	}
	// Tối đa 3 vụ: năm 2021→2022 (+300%) không được dùng.
	p = ProjectArea(map[int]decimal.Decimal{2021: fd("10"), 2022: fd("40"), 2023: fd("40"), 2024: fd("40"), 2025: fd("40")}, 2026)
	if !p.Growth.IsZero() || len(p.Rates) != 3 {
		t.Fatalf("chỉ 3 vụ gần nhất: %+v", p)
	}
	// Thiếu năm trước → không dự báo.
	if p := ProjectArea(map[int]decimal.Decimal{2024: fd("10")}, 2026); p.Forecast != nil {
		t.Fatalf("thiếu năm trước phải no_data: %+v", p)
	}
	r, w, g, st := ForecastRequired(fptr(fd("133.1")), &ParamRef{Value: fd("120")}, 1)
	if st != ForecastOK || !r.Equal(fd("1.1092")) || *w != 2 || *g != -1 {
		t.Fatalf("số máy cần: %v %v %v %s", r, *w, *g, st)
	}
	if _, _, _, st := ForecastRequired(fptr(fd("1")), nil, 0); st != ForecastNoNorm {
		t.Fatal("thiếu định mức phải no_norm")
	}
}

func fptr(x decimal.Decimal) *decimal.Decimal { return &x }
