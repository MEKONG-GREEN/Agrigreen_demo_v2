package contextdata_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
)

func projectOwner(t *testing.T, k *authz.KNContext) (authz.Principal, port.SessionContext) {
	t.Helper()
	p := authz.Principal{ActorRef: ref(1001), TenantID: ref(1002), OrgRef: ref(1003), Roles: []string{"kn_user"}, KN: k, MFA: true}
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	s, err := registry.ResolveSelected(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	p.SelectedSession = &s
	owned, err := port.ReadSessionContext(p)
	if err != nil {
		t.Fatal(err)
	}
	return p, owned
}

func assertSameContext(t *testing.T, k authz.KNContext, owned port.SessionContext) {
	t.Helper()
	a, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(owned)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("owner dropped/changed resolved fields:\n%s\n%s", a, b)
	}
}

func TestOwnedPayloadPreservesFrozenContexts(t *testing.T) {
	for _, c := range nativeCases() {
		if c.principal == nil || c.principal.KN == nil {
			continue
		}
		_, owned := projectOwner(t, c.principal.KN)
		assertSameContext(t, *c.principal.KN, owned)
	}
}

// Compare the independent owner policy against the existing policy across role,
// support, read-only, manager and target restrictions. Permission expectations do
// not come from the new payload implementation.
func TestOwnedPayloadPreservesResolvedPolicyAndIsolation(t *testing.T) {
	for _, role := range []string{authz.KNTW, authz.KNTinh, authz.KNXa, authz.KNAdmin} {
		for flags := 0; flags < 64; flags++ {
			k := authz.KNContext{ID: ref(10), AssignmentID: ref(11), Role: role, Department: "dept", OrgRef: ref(12), UnitRef: ref(13),
				Communes: []string{"X1", "X2"}, Scope: authz.Scope{All: flags&32 != 0, Units: []string{"X1"}}, DefaultRoute: "/kn", ExpiresAt: time.Date(2026, 12, 1, 2, 3, 4, 0, time.UTC),
				MgmtLevel: "leader", Manager: flags&1 != 0, ViewOnly: flags&2 != 0, ProvinceGroup: "leadership", Duties: []string{authz.DutyPriceAdmin, authz.DutyFormOwner, authz.DutyScheduleFollower},
				InternalUnit: "station", Powers: []string{authz.PowerApprove}}
			if flags&4 != 0 {
				k.Support = &authz.KNSupport{SessionID: ref(14), OnBehalfOf: ref(15), Reason: "synthetic reason"}
			}
			if flags&8 != 0 {
				k.Facilities = []uuid.UUID{ref(16)}
			}
			if flags&16 != 0 {
				k.Households = []uuid.UUID{ref(17)}
			}
			p, owned := projectOwner(t, &k)
			assertSameContext(t, k, owned)
			if owned.ReadOnly() != k.ReadOnly() {
				t.Fatal("read-only changed")
			}
			for _, dept := range []string{"dept", "other", ""} {
				for _, unit := range []string{"X1", "other", ""} {
					if owned.Allows(dept, unit) != k.Allows(dept, unit) || owned.AllowsDepartment(dept) != k.AllowsDepartment(dept) {
						t.Fatal("scope/department policy changed")
					}
				}
			}
			for _, value := range []string{authz.DutyPriceAdmin, authz.DutyFormOwner, authz.DutyScheduleFollower, "unknown"} {
				if owned.HasDuty(value) != k.HasDuty(value) {
					t.Fatal("duty changed")
				}
			}
			for _, value := range []string{authz.PowerApprove, authz.PowerReopenPeriod, "unknown"} {
				if owned.Can(value) != k.Can(value) {
					t.Fatal("delegated authority changed")
				}
			}
			for _, id := range []uuid.UUID{ref(16), ref(17), ref(18), uuid.Nil} {
				if owned.AllowsFacility(id) != k.AllowsFacility(id) || owned.AllowsHousehold(id) != k.AllowsHousehold(id) {
					t.Fatal("target restriction changed")
				}
			}
			// Neither legacy pointers nor returned mutable owner slices can rewrite the snapshot.
			raw := p.SelectedSession.Payload()
			k.Role = authz.KNAdmin
			k.Communes[0] = "changed"
			k.Scope.Units[0] = "changed"
			k.Duties[0] = "changed"
			k.Powers[0] = "changed"
			owned.Communes[0] = "changed"
			owned.Scope.Units[0] = "changed"
			owned.Duties[0] = "changed"
			owned.Powers[0] = "changed"
			if k.Support != nil {
				k.Support.Reason = "changed"
				owned.Support.Reason = "changed"
			}
			if len(k.Facilities) > 0 {
				k.Facilities[0] = uuid.Nil
				owned.Facilities[0] = uuid.Nil
			}
			if len(k.Households) > 0 {
				k.Households[0] = uuid.Nil
				owned.Households[0] = uuid.Nil
			}
			p.KN = nil
			_, again, err := port.RequireSession(authz.WithPrincipal(context.Background(), p))
			if err != nil || again.Role != role || again.Communes[0] != "X1" || again.Scope.Units[0] != "X1" || again.Duties[0] != authz.DutyPriceAdmin || again.Powers[0] != authz.PowerApprove || !reflect.DeepEqual(raw, p.SelectedSession.Payload()) {
				t.Fatalf("owner snapshot changed through alias: %v", err)
			}
			if p.Has("kn_officer") || !p.Has("kn_user") || !p.MFA || p.ActorRef != ref(1001) || p.TenantID != ref(1002) {
				t.Fatal("owner changed authentication")
			}
		}
	}
}

func TestOwnedPayloadRejectsMissingCorruptOrMismatchedBinding(t *testing.T) {
	k := port.SessionContext{ID: ref(20), Role: port.SessionRoleCommune, OrgRef: ref(21), Department: "dept", Communes: []string{"X1"}, Support: &port.SessionSupport{OnBehalfOf: ref(22)}}
	valid, err := port.NewSessionContext(k)
	if err != nil {
		t.Fatal(err)
	}
	altered := func(change func(*authz.SessionScope)) authz.Session {
		scope := valid.Scope()
		change(&scope)
		s, err := authz.NewSession("kn", k.ID, scope)
		if err != nil {
			t.Fatal(err)
		}
		return s.WithPayload(valid.Payload())
	}
	replacePayload := func(change func(map[string]any)) authz.Session {
		var v map[string]any
		if err := json.Unmarshal(valid.Payload(), &v); err != nil {
			t.Fatal(err)
		}
		change(v)
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return valid.WithPayload(raw)
	}
	other, err := authz.NewSession("other", k.ID, valid.Scope())
	if err != nil {
		t.Fatal(err)
	}
	cases := []authz.Session{
		{}, other.WithPayload(valid.Payload()), valid.WithPayload(nil), valid.WithPayload([]byte("{")), valid.WithPayload(append(valid.Payload(), []byte(" {}")...)),
		replacePayload(func(v map[string]any) { v["Version"] = 2 }), replacePayload(func(v map[string]any) { v["Unknown"] = true }),
		replacePayload(func(v map[string]any) { v["Context"].(map[string]any)["ID"] = ref(23).String() }),
		replacePayload(func(v map[string]any) { v["Context"].(map[string]any)["Role"] = "unknown" }),
		altered(func(s *authz.SessionScope) { s.NodeID = ref(23) }), altered(func(s *authz.SessionScope) { s.Department = "other" }),
		altered(func(s *authz.SessionScope) { s.UnitCodes = []string{"other"} }), altered(func(s *authz.SessionScope) { s.OnBehalfOfRef = ref(23) }), altered(func(s *authz.SessionScope) { s.RequiresNode = false }),
	}
	for i, s := range cases {
		p := authz.Principal{Roles: []string{"platform_admin"}, KN: &authz.KNContext{Role: authz.KNAdmin}, SelectedSession: &s}
		if _, err := port.ReadSessionContext(p); !errors.Is(err, authz.ErrInvalidSession) {
			t.Fatalf("case %d: invalid binding accepted: %v", i, err)
		}
		if _, _, err := port.RequireSession(authz.WithPrincipal(context.Background(), p)); err == nil {
			t.Fatalf("case %d: token/legacy fallback", i)
		}
	}
	if _, _, err := port.RequireSession(context.Background()); err == nil {
		t.Fatal("missing identity accepted")
	}
	if _, _, err := port.RequireSession(authz.WithPrincipal(context.Background(), authz.Principal{KN: &authz.KNContext{Role: authz.KNAdmin}})); err == nil {
		t.Fatal("legacy fallback without binding")
	}
	p := authz.Principal{SelectedSession: &valid}
	if _, _, err := port.RequireSession(authz.WithPrincipal(context.Background(), p), port.SessionRoleAdmin); err == nil {
		t.Fatal("wrong owner role accepted")
	}
}
