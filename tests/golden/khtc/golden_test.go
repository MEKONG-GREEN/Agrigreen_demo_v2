package khtcgolden

import (
	"testing"

	"github.com/shopspring/decimal"
)

// TestKHTCQA_GoldenDatasetConsistent: bộ dữ liệu nạp được và số tính tay nhất quán (Validate). Quy đổi tuần bằng hàm
// lịch kỳ W11.03 kiểm ở reportcycle/domain (TestKHTCQA_WeekCrosswalkGolden).
func TestKHTCQA_GoldenDatasetConsistent(t *testing.T) {
	d, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"G01", "G02", "G02B", "G03", "G04", "G05", "G05B", "G05C", "G06"}
	if len(d.Cases) != len(want) {
		t.Fatalf("có %d ca, cần %d", len(d.Cases), len(want))
	}
	for _, id := range want {
		if _, ok := d.Case(id); !ok {
			t.Errorf("thiếu ca %s", id)
		}
	}
	if len(d.CrosswalkOf("G05C")) == 0 || len(d.CrosswalkOf("G02B")) != 5 {
		t.Errorf("crosswalk: G05C %d dòng, G02B %d dòng", len(d.CrosswalkOf("G05C")), len(d.CrosswalkOf("G02B")))
	}
}

// TestKHTCQA_ValidateCatchesArithmetic: Validate bắt lỗi tính tay (đổi một số trong kỳ).
func TestKHTCQA_ValidateCatchesArithmetic(t *testing.T) {
	d, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	zero := decimal.Zero
	d.Cases[0].Steps[0].Expected[0].Increment = &zero
	if err := d.Validate(); err == nil {
		t.Fatal("Validate phải báo trong kỳ ≠ lũy kế − gốc")
	}
}
