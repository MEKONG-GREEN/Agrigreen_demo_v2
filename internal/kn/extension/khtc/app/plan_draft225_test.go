package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/google/uuid"
	"reflect"
	"testing"
	"time"
)

func draftCapabilityFixture225(t *testing.T) (*app.Service, authz.Principal, port.SessionContext) {
	t.Helper()
	now := time.Date(2030, 6, 1, 3, 0, 0, 0, time.UTC)
	k := port.SessionContext{ID: uuid.New(), AssignmentID: uuid.New(), Role: port.SessionRoleProvince, Department: "ke_hoach_tai_chinh", OrgRef: uuid.New(), UnitRef: uuid.New(), ExpiresAt: now.Add(time.Hour), Scope: authz.Scope{Units: []string{"X2", "X1"}}, Communes: []string{"X2", "X1"}, Duties: []string{"two", "one"}, Powers: []string{"p2", "p1"}}
	p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), OrgRef: k.OrgRef, Roles: []string{"kn_user", "staff"}, MFA: true}
	return &app.Service{Clock: clock.NewFixed(now)}, p, k
}
func bindDraftCapability225(t *testing.T, p authz.Principal, k port.SessionContext) (context.Context, authz.Principal) {
	t.Helper()
	s, e := port.NewSessionContext(k)
	if e != nil {
		t.Fatal(e)
	}
	p.SelectedSession = &s
	return authz.WithPrincipal(context.Background(), p), p
}
func TestDraft225CapabilityStableRenewalAndCompleteOwner(t *testing.T) {
	svc, p, k := draftCapabilityFixture225(t)
	ctx, bound := bindDraftCapability225(t, p, k)
	before := bound.SelectedSession.Payload()
	roles := append([]string{}, p.Roles...)
	original, e := svc.AuthorizeInitialDraft225(ctx, true)
	if e != nil {
		t.Fatal(e)
	}
	renewed := k
	renewed.ID = uuid.New()
	renewed.ExpiresAt = k.ExpiresAt.Add(time.Hour)
	renewCtx, _ := bindDraftCapability225(t, p, renewed)
	again, e := svc.AuthorizeInitialDraft225(renewCtx, true)
	if e != nil || !bytes.Equal(original, again) {
		t.Fatal("same-authority renewal stranded a durable command")
	}
	if !bytes.Equal(before, bound.SelectedSession.Payload()) || !reflect.DeepEqual(roles, p.Roles) {
		t.Fatal("canonicalization mutated the live principal/owner")
	}
	for name, change := range map[string]func(*authz.Principal, *port.SessionContext){"actor": func(p *authz.Principal, _ *port.SessionContext) { p.ActorRef = uuid.New() }, "tenant": func(p *authz.Principal, _ *port.SessionContext) { p.TenantID = uuid.New() }, "principalOrg": func(p *authz.Principal, _ *port.SessionContext) { p.OrgRef = uuid.New() }, "roles": func(p *authz.Principal, _ *port.SessionContext) { p.Roles = []string{"kn_user"} }, "assignment": func(_ *authz.Principal, k *port.SessionContext) { k.AssignmentID = uuid.New() }, "scope": func(_ *authz.Principal, k *port.SessionContext) { k.Scope = authz.Scope{All: true} }, "powers": func(_ *authz.Principal, k *port.SessionContext) { k.Powers = []string{"other"} }, "support": func(_ *authz.Principal, k *port.SessionContext) {
		k.Support = &port.SessionSupport{SessionID: uuid.New(), OnBehalfOf: uuid.New(), Reason: "Hỗ trợ lập kế hoạch"}
	}} {
		t.Run(name, func(t *testing.T) {
			nextP, nextK := p, k
			change(&nextP, &nextK)
			next, _ := bindDraftCapability225(t, nextP, nextK)
			stamp, err := svc.AuthorizeInitialDraft225(next, true)
			if err == nil && bytes.Equal(original, stamp) {
				t.Fatal("changed complete authority reused a stamp")
			}
		})
	}
}
func TestDraft225CapabilityPolicyAndUnknownOwner(t *testing.T) {
	svc, p, k := draftCapabilityFixture225(t)
	for name, change := range map[string]func(*authz.Principal, *port.SessionContext){"supportMFA": func(p *authz.Principal, k *port.SessionContext) {
		p.MFA = false
		k.Support = &port.SessionSupport{SessionID: uuid.New(), OnBehalfOf: uuid.New(), Reason: "Hỗ trợ lập kế hoạch"}
	}, "adminMFA": func(p *authz.Principal, k *port.SessionContext) { p.MFA = false; k.Role = port.SessionRoleAdmin }, "expired": func(_ *authz.Principal, k *port.SessionContext) { k.ExpiresAt = svc.Clock.Now() }, "missingExpiry": func(_ *authz.Principal, k *port.SessionContext) { k.ExpiresAt = time.Time{} }, "otherDepartment": func(_ *authz.Principal, k *port.SessionContext) { k.Department = "trong_trot" }, "readonly": func(_ *authz.Principal, k *port.SessionContext) { k.ViewOnly = true }} {
		t.Run(name, func(t *testing.T) {
			nextP, nextK := p, k
			change(&nextP, &nextK)
			ctx, _ := bindDraftCapability225(t, nextP, nextK)
			if _, e := svc.AuthorizeInitialDraft225(ctx, true); e == nil {
				t.Fatal("current denied authority permitted a write")
			}
		})
	}
	ordinary := p
	ordinary.MFA = false
	ordinaryCtx, _ := bindDraftCapability225(t, ordinary, k)
	ordinaryStamp, ordinaryErr := svc.AuthorizeInitialDraft225(ordinaryCtx, true)
	if ordinaryErr != nil {
		t.Fatal("ordinary province staff acquired an unsupported MFA enrollment requirement")
	}
	mfaCtx, _ := bindDraftCapability225(t, p, k)
	mfaStamp, _ := svc.AuthorizeInitialDraft225(mfaCtx, true)
	if bytes.Equal(ordinaryStamp, mfaStamp) {
		t.Fatal("MFA capability change lost binding")
	}
	readonly := k
	readonly.ViewOnly = true
	ctx, _ := bindDraftCapability225(t, p, readonly)
	if _, e := svc.AuthorizeInitialDraft225(ctx, false); e != nil {
		t.Fatal("read-only KHTC cannot read its draft")
	}
	for _, mode := range []string{"unknownField", "futureVersion"} {
		t.Run(mode, func(t *testing.T) {
			_, bound := bindDraftCapability225(t, p, k)
			var body map[string]json.RawMessage
			if e := json.Unmarshal(bound.SelectedSession.Payload(), &body); e != nil {
				t.Fatal(e)
			}
			if mode == "futureVersion" {
				body["Version"] = json.RawMessage(`2`)
			} else {
				body["UnreviewedGrant"] = json.RawMessage(`true`)
			}
			raw, e := json.Marshal(body)
			if e != nil {
				t.Fatal(e)
			}
			changed := bound.SelectedSession.WithPayload(raw)
			bound.SelectedSession = &changed
			if _, e := svc.AuthorizeInitialDraft225(authz.WithPrincipal(context.Background(), bound), false); e == nil {
				t.Fatal("unknown owner projection was silently dropped")
			}
		})
	}
}
