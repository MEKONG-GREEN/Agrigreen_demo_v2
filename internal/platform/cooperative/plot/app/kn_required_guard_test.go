package app

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func TestKNFarmerRequiredBeforeVault(t *testing.T) {
	svc := &Service{}
	for _, name := range []string{"", " ", "\t\n"} {
		_, err := svc.farmerRef(context.Background(), name, "31117")
		var pr *problem.Problem
		if !errors.As(err, &pr) || pr.Status != 422 || pr.Code != problem.Validation.Code {
			t.Errorf("empty farmer must be rejected before vault access: %v", err)
		}
	}
}
