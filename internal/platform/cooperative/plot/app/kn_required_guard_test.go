package app

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
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

// R-A1 (#113): declared area ≤ 0 is rejected before the farmer name reaches the identity vault. With no vault wired,
// a missing guard would surface as 503 dependency instead of the 422 field error.
func TestKNDeclaredAreaPositiveBeforeVault(t *testing.T) {
	svc := &Service{}
	c := knCaller{a: access{sc: authz.Scope{Units: []string{"31117"}}}, k: authz.KNContext{Role: authz.KNXa, Department: "trong_trot"}}
	for _, raw := range []string{"-1", "0", "-0.0001"} {
		d := decimal.RequireFromString(raw)
		_, err := svc.knCreateAttrs(context.Background(), c, KNCreateInput{CommuneCode: "31117", LotName: "Lô A", FarmerName: "Nguyễn Văn An",
			DeclaredAreaHa: &d, IdemKey: "k-declared"})
		var pr *problem.Problem
		if !errors.As(err, &pr) || pr.Status != 422 || len(pr.Errors) != 1 || pr.Errors[0].Field != "declared_area_ha" {
			t.Errorf("declared_area_ha %s: muốn 422 theo trường trước kho định danh, nhận %v", raw, err)
		}
	}
}
