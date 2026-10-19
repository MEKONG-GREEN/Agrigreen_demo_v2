package handler

import (
	"context"
	"testing"

	sessionport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
)

// Bind the synthetic fixture through the actual registry and owner codec. The
// generated context ID is test-only; production never manufactures a context.
func bindTestSession(t *testing.T, p authz.Principal) authz.Principal {
	t.Helper()
	if p.KN == nil {
		return p
	}
	kc := *p.KN
	if kc.ID == uuid.Nil {
		kc.ID = ids.New()
	}
	p.KN = &kc
	registry, err := authz.NewSessionRegistry(authz.SessionRegistration{Profile: "kn", Project: func(_ context.Context, p authz.Principal) (authz.Session, bool, error) {
		k := p.KN
		owned := sessionport.SessionContext{ID: k.ID, Role: k.Role, Department: k.Department, OrgRef: k.OrgRef, UnitRef: k.UnitRef,
			Communes: k.Communes, Scope: k.Scope, Duties: k.Duties, InternalUnit: k.InternalUnit, Manager: k.Manager, ViewOnly: k.ViewOnly}
		if k.Support != nil {
			owned.Support = &sessionport.SessionSupport{SessionID: k.Support.SessionID, OnBehalfOf: k.Support.OnBehalfOf, Reason: k.Support.Reason}
		}
		selected, err := sessionport.NewSessionContext(owned)
		return selected, true, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := registry.ResolveSelected(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	p.SelectedSession = &selected
	return p
}

func testOwnerSession(ctx context.Context) (port.SessionContext, error) {
	_, k, err := sessionport.RequireSession(ctx)
	if err != nil {
		return port.SessionContext{}, err
	}
	return port.SessionContext{Role: k.Role, ReadOnly: k.ReadOnly(), Duties: k.Duties}, nil
}
