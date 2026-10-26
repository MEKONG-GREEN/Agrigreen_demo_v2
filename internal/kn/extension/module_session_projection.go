package extension

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// SessionProjection bridges the validated legacy account resolver to the KN
// owner. New consumers read only the immutable owner payload. Account ownership
// migration is separate; no payload comes from a client or overrides identity.
func SessionProjection() authz.SessionRegistration {
	return authz.SessionRegistration{Profile: "kn", Project: func(_ context.Context, p authz.Principal) (authz.Session, bool, error) {
		k := p.KN
		if k == nil {
			return authz.Session{}, false, nil
		}
		owned := port.SessionContext{
			ID: k.ID, AssignmentID: k.AssignmentID, Role: k.Role, Department: k.Department,
			OrgRef: k.OrgRef, UnitRef: k.UnitRef, Communes: k.Communes, Scope: k.Scope,
			DefaultRoute: k.DefaultRoute, ExpiresAt: k.ExpiresAt, MgmtLevel: k.MgmtLevel,
			Manager: k.Manager, ProvinceGroup: k.ProvinceGroup, Duties: k.Duties,
			InternalUnit: k.InternalUnit, Facilities: k.Facilities, Households: k.Households,
			Powers: k.Powers, ViewOnly: k.ViewOnly,
		}
		if k.Support != nil {
			owned.Support = &port.SessionSupport{SessionID: k.Support.SessionID, OnBehalfOf: k.Support.OnBehalfOf, Reason: k.Support.Reason}
		}
		s, err := port.NewSessionContext(owned)
		return s, true, err
	}}
}
