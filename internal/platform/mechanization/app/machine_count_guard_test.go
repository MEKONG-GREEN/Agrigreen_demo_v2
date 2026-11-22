package app

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// An internal caller bypassing JSON parsing must also be rejected before any
// catalogue, party facade, ledger or database dependency is reached.
func TestMachineQuantityGuardBeforeDependencies(t *testing.T) {
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{KN: &authz.KNContext{Role: authz.KNXa}})
	svc := &Service{}
	for _, quantity := range []int{-1, 0, domain.MaxMachineQuantity + 1} {
		_, err := svc.PutMachineCounts(ctx, PutMachineCountsInput{Lines: []domain.CountLine{{MachineType: "tractor", Quantity: quantity}}})
		var pr *problem.Problem
		if !errors.As(err, &pr) || pr.Code != ErrQuantityInvalid.Code || pr.Status != 422 {
			t.Errorf("quantity %d: expected field validation before dependencies, got %v", quantity, err)
		}
	}
}
