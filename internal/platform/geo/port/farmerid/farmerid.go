// Package farmerid là bộ điều hợp port.FarmerIdentity của pf/geo trên vault client (R-04): tên nông dân của
// thửa KN (KN v7.7 FN-XA-03.05). Tách khỏi gốc module để cmd/api-public (dựng pf/geo) không kéo vault (RB-3).
package farmerid

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

type adapter struct{ c client.Client }

// New bọc vault client; c nil → nil (pf/geo coi như chưa nối kho định danh).
func New(c client.Client) port.FarmerIdentity {
	if c == nil {
		return nil
	}
	return adapter{c: c}
}

func (a adapter) RegisterFarmer(ctx context.Context, fullName, areaCode string) (uuid.UUID, error) {
	cr, err := a.c.Create(ctx, client.PurposeContact, client.Identity{Kind: client.SubjectPerson, FullName: fullName, AreaCode: areaCode})
	if err != nil {
		if errors.Is(err, client.ErrDuplicate) && cr.Ref != uuid.Nil {
			return cr.Ref, nil
		}
		return uuid.Nil, err
	}
	return cr.Ref, nil
}

func (a adapter) FarmerLabel(ctx context.Context, ref uuid.UUID) (string, error) {
	d, err := a.c.Display(ctx, client.PurposeContact, ref)
	return d.Label, err
}
