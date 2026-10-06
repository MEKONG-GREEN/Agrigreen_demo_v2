package app

import (
	"errors"
	"fmt"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Số tiền tính ra vượt numeric(18,2) (vd đơn giá thỏa thuận quá lớn) → 422 costing.amount_out_of_range, không 500.
func TestMapErrOutOfRange(t *testing.T) {
	err := mapErr(fmt.Errorf("refigure: %w", domain.ErrOutOfRange))
	var p *problem.Problem
	if !errors.As(err, &p) || p.Status != 422 || p.Code != "costing.amount_out_of_range" {
		t.Fatalf("mapErr = %#v", err)
	}
}
