// Package farmerid là bộ điều hợp port.FarmerIdentity, port.HouseholdIdentities của pf/cooperative trên vault client (R-04): tên nông dân của
// thửa KN (KN v7.7 FN-XA-03.05; chuyển từ geo/port/farmerid ở W3.3). Tách khỏi gốc module để cmd/api-public (dựng
// pf/cooperative) không kéo vault (RB-3).
package farmerid

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

type adapter struct{ c client.Client }

// New bọc vault client; c nil → nil (pf/cooperative coi như chưa nối kho định danh).
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

// Households bọc vault client thành port.HouseholdIdentities (cooperative/member); c nil → nil.
func Households(c client.Client) port.HouseholdIdentities {
	if c == nil {
		return nil
	}
	return households{c: c}
}

type households struct{ c client.Client }

func (h households) RegisterHousehold(ctx context.Context, id port.HouseholdIdentity) (uuid.UUID, error) {
	cr, err := h.c.Create(ctx, client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectHousehold, FullName: id.FullName,
		Phone: id.Phone, NationalID: id.NationalID, Address: id.Address, AreaCode: id.AreaCode})
	if errors.Is(err, client.ErrDuplicate) {
		return cr.Ref, port.ErrIdentityDuplicate
	}
	return cr.Ref, err
}

func (h households) Label(ctx context.Context, purpose port.IdentityPurpose, ref uuid.UUID) (port.IdentityLabel, error) {
	p := client.PurposeContact
	if purpose == port.IdentityFieldSupport {
		p = client.PurposeFieldSupport
	}
	d, err := h.c.Display(ctx, p, ref)
	return port.IdentityLabel{Label: d.Label, MaskedPhone: d.MaskedPhone}, err
}

func (h households) Phone(ctx context.Context, ref uuid.UUID) (string, error) {
	id, err := h.c.Resolve(ctx, client.PurposeContact, ref)
	if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrErased) {
		return "", port.ErrIdentityNotFound
	}
	return id.Phone, err
}

func (h households) LookupPhone(ctx context.Context, phone string) (uuid.UUID, error) {
	ref, err := h.c.Lookup(ctx, client.PurposeRegistryAdmin, phone)
	if errors.Is(err, client.ErrNotFound) {
		return uuid.Nil, port.ErrIdentityNotFound
	}
	return ref, err
}
