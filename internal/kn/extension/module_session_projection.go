package extension

import (
	"context"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// SessionProjection is trusted startup registration. The request must already
// have been resolved by its owner; this adapter never interprets client input.
// Runtime middleware wiring is deliberately separate from this additive step.
func SessionProjection() authz.SessionRegistration {
	return authz.SessionRegistration{Profile: "kn", Project: func(_ context.Context, p authz.Principal) (authz.Session, bool, error) {
		k := p.KN
		if k == nil {
			return authz.Session{}, false, nil
		}
		switch k.Role {
		case authz.KNTW, authz.KNTinh, authz.KNXa, authz.KNAdmin:
		default:
			return authz.Session{}, true, authz.ErrInvalidSession
		}
		scope := authz.SessionScope{NodeID: k.OrgRef, Department: k.Department, UnitCodes: k.Communes, RequiresNode: k.Role == authz.KNXa}
		if k.Support != nil {
			scope.OnBehalfOfRef = k.Support.OnBehalfOf
		}
		projected, err := authz.NewSession("kn", k.ID, scope)
		return projected, true, err
	}}
}
