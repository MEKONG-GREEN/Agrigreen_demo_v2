package extension

import (
	"context"

	contactport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	hrmport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
)

// PriceSession supplies only the price policy inputs from the KN owner. The
// generic capability never interprets owner bytes or legacy Principal.KN.
func PriceSession(ctx context.Context) (priceport.SessionContext, error) {
	_, k, err := port.RequireSession(ctx)
	if err != nil {
		return priceport.SessionContext{}, err
	}
	return priceport.SessionContext{Role: k.Role, ReadOnly: k.ReadOnly(), Duties: k.Duties}, nil
}

// HRMSession supplies HRM's group, department and internal-unit policy inputs.
func HRMSession(ctx context.Context) (hrmport.SessionContext, error) {
	_, k, err := port.RequireSession(ctx)
	if err != nil {
		return hrmport.SessionContext{}, err
	}
	return hrmport.SessionContext{Role: k.Role, Department: k.Department, UnitRef: k.UnitRef,
		InternalUnit: k.InternalUnit, Manager: k.Manager, ViewOnly: k.ViewOnly, ReadOnly: k.ReadOnly()}, nil
}

// ContactSession supplies the field-set and directory management policy inputs.
func ContactSession(ctx context.Context) (contactport.SessionContext, error) {
	_, k, err := port.RequireSession(ctx)
	if err != nil {
		return contactport.SessionContext{}, err
	}
	return contactport.SessionContext{Role: k.Role, Department: k.Department, ReadOnly: k.ReadOnly()}, nil
}
