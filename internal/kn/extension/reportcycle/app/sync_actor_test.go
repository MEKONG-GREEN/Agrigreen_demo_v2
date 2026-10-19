package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Sync keeps 403 for missing selection so old devices can sync unrelated entities.
// The device scope intersects the owned communes and can never enlarge them.
func TestReportSyncOwnedScopeIntersection(t *testing.T) {
	actor, tid := ids.New(), ids.New()
	k := port.SessionContext{ID: ids.New(), Role: port.SessionRoleCommune, OrgRef: ids.New(), Department: "trong_trot", Communes: []string{"30001", "30001", "30002"}, Scope: authz.Scope{Units: []string{"30001", "30002"}}}
	owned, err := port.NewSessionContext(k)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := port.NewSessionContext(port.SessionContext{ID: ids.New(), Role: port.SessionRoleAdmin, Scope: authz.Scope{All: true}})
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := authz.NewSession("other", ids.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	zero, empty, bad := authz.Session{}, owned.WithPayload(nil), owned.WithPayload([]byte("{"))
	principal := func(s *authz.Session) *authz.Principal {
		return &authz.Principal{ActorRef: actor, TenantID: tid, MFA: true, Roles: []string{"platform_admin", "kn_officer"}, KN: &authz.KNContext{Role: authz.KNXa, Communes: []string{"30001", "30002", "30003"}}, SelectedSession: s}
	}
	for _, tc := range []struct {
		name   string
		p      *authz.Principal
		scope  authz.Scope
		status int
		want   []string
	}{
		{"no identity", nil, authz.Scope{All: true}, 401, nil},
		{"legacy commune only", principal(nil), authz.Scope{All: true}, 403, nil},
		{"zero selected", principal(&zero), authz.Scope{All: true}, 403, nil},
		{"other profile", principal(&wrong), authz.Scope{All: true}, 403, nil},
		{"missing payload", principal(&empty), authz.Scope{All: true}, 403, nil},
		{"corrupt payload", principal(&bad), authz.Scope{All: true}, 403, nil},
		{"owner admin cannot borrow legacy commune", principal(&admin), authz.Scope{All: true}, 403, nil},
		{"device all keeps owned communes", principal(&owned), authz.Scope{All: true}, 0, []string{"30001", "30002"}},
		{"intersection does not borrow legacy extra", principal(&owned), authz.Scope{Units: []string{"30002", "30003"}}, 0, []string{"30002"}},
		{"disjoint", principal(&owned), authz.Scope{Units: []string{"30003"}}, 0, []string{}},
		{"empty device scope", principal(&owned), authz.Scope{}, 0, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.p != nil {
				ctx = authz.WithPrincipal(ctx, *tc.p)
			}
			p, got, communes, err := syncActor(ctx, tc.scope)
			if tc.status != 0 {
				if err == nil || problem.From(err).Status != tc.status {
					t.Fatalf("error=%v want status%d", err, tc.status)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(communes, tc.want) {
				t.Fatalf("communes=%v want%v err=%v", communes, tc.want, err)
			}
			if p.ActorRef != actor || p.TenantID != tid || !p.MFA || !p.Has("platform_admin") || got.ID != k.ID {
				t.Fatal("identity or owner changed")
			}
			// Removing legacy state must leave the same selected owner and intersection.
			p.KN = nil
			_, _, without, err := syncActor(authz.WithPrincipal(context.Background(), p), tc.scope)
			if err != nil || !reflect.DeepEqual(without, tc.want) {
				t.Fatalf("without legacy=%v err=%v", without, err)
			}
		})
	}
}
