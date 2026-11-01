package extension

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// #60: adapter chủ KN của accountsport.SessionPolicies — đọc payload phiên chủ, không bao giờ đọc Principal.KN hay
// vai realm để suy ra giá trị; mọi phiên đã chọn nhưng hỏng đều bị từ chối (handled), không quay về đường cũ.
func TestAccountSessionPoliciesReadOwnerAndFailClosed(t *testing.T) {
	homeOrg, lostOrg, brokenOrg, homeUnit := ids.New(), ids.New(), ids.New(), ids.New()
	home := &fakeHomeUnits{units: map[uuid.UUID]uuid.UUID{homeOrg: homeUnit}, broken: brokenOrg}
	pol := AccountSessionPolicies(home)
	if pol.SupportAudit == nil || pol.DefaultUnit == nil {
		t.Fatal("adapter must configure both callbacks (nil selects the legacy bridge)")
	}
	unit, otherUnit, support, onBehalf := ids.New(), ids.New(), ids.New(), ids.New()
	owner := func(k extport.SessionContext) authz.Session {
		t.Helper()
		if k.ID == uuid.Nil {
			k.ID = ids.New()
		}
		s, err := extport.NewSessionContext(k)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	plain := owner(extport.SessionContext{Role: extport.SessionRoleProvince, Department: "trong_trot", OrgRef: unit, UnitRef: unit})
	supported := owner(extport.SessionContext{Role: extport.SessionRoleCommune, Department: "trong_trot", OrgRef: unit, UnitRef: unit,
		Communes: []string{"30001"}, Scope: authz.Scope{Units: []string{"30001"}},
		Support: &extport.SessionSupport{SessionID: support, OnBehalfOf: onBehalf, Reason: "Hỗ trợ theo cuộc gọi"}})
	noSupportID := owner(extport.SessionContext{Role: extport.SessionRoleAdmin, UnitRef: unit,
		Support: &extport.SessionSupport{OnBehalfOf: onBehalf}})
	emptyUnit := owner(extport.SessionContext{Role: extport.SessionRoleAdmin})
	foreign, err := authz.NewSession("foreign", ids.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	zero := authz.Session{}
	malformed, missing := plain.WithPayload([]byte("{")), plain.WithPayload(nil)
	// Ngữ cảnh cũ rộng hơn/khác hẳn phiên chủ: không bao giờ được dùng.
	legacyConflict := &authz.KNContext{ID: ids.New(), Role: authz.KNAdmin, UnitRef: otherUnit, Scope: authz.Scope{All: true},
		Support: &authz.KNSupport{SessionID: ids.New(), OnBehalfOf: ids.New(), Reason: "legacy"}}
	ctxAt := func(org uuid.UUID, sel *authz.Session, legacy *authz.KNContext) context.Context {
		return authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), OrgRef: org,
			Roles: []string{authz.RolePlatformAdmin}, MFA: true, SelectedSession: sel, KN: legacy})
	}
	ctxOf := func(sel *authz.Session, legacy *authz.KNContext) context.Context { return ctxAt(homeOrg, sel, legacy) }
	supportNoUnit := owner(extport.SessionContext{Role: extport.SessionRoleAdmin,
		Support: &extport.SessionSupport{SessionID: support, OnBehalfOf: onBehalf}})
	communeNoUnit := owner(extport.SessionContext{Role: extport.SessionRoleCommune, Department: "trong_trot"})

	t.Run("default unit", func(t *testing.T) {
		cases := []struct {
			name    string
			ctx     context.Context
			unit    uuid.UUID
			handled bool
			status  int // 0 = no error
		}{
			{"unauthenticated", context.Background(), uuid.Nil, false, 401},
			{"native request", ctxOf(nil, nil), uuid.Nil, false, 0},
			{"owner unit", ctxOf(&plain, nil), unit, true, 0},
			{"owner wins over conflicting legacy", ctxOf(&plain, legacyConflict), unit, true, 0},
			{"admin without unit uses authenticated home node unit", ctxOf(&emptyUnit, nil), homeUnit, true, 0},
			{"admin home unit ignores conflicting legacy", ctxOf(&emptyUnit, legacyConflict), homeUnit, true, 0},
			{"admin without home node has no default", ctxAt(uuid.Nil, &emptyUnit, nil), uuid.Nil, true, 0},
			{"admin home node outside any unit has no default", ctxAt(lostOrg, &emptyUnit, nil), uuid.Nil, true, 0},
			{"admin home lookup failure is final", ctxAt(brokenOrg, &emptyUnit, nil), uuid.Nil, true, 500},
			{"support without unit never uses home node", ctxOf(&supportNoUnit, nil), uuid.Nil, true, 0},
			{"non-admin without unit never uses home node", ctxOf(&communeNoUnit, nil), uuid.Nil, true, 0},
			{"legacy only cannot fall back", ctxOf(nil, legacyConflict), uuid.Nil, true, 428},
			{"zero session", ctxOf(&zero, nil), uuid.Nil, true, 403},
			{"foreign profile", ctxOf(&foreign, legacyConflict), uuid.Nil, true, 403},
			{"malformed payload", ctxOf(&malformed, legacyConflict), uuid.Nil, true, 403},
			{"missing payload", ctxOf(&missing, nil), uuid.Nil, true, 403},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				home.calls = 0
				got, handled, err := pol.DefaultUnit(tc.ctx)
				if handled != tc.handled || statusOf(err) != tc.status {
					t.Fatalf("handled=%v err=%v want handled=%v status=%d", handled, err, tc.handled, tc.status)
				}
				if err == nil && got != tc.unit {
					t.Fatalf("unit=%s want %s", got, tc.unit)
				}
				if err != nil && got != uuid.Nil {
					t.Fatal("error result leaked a unit")
				}
				if wantCall := tc.ctx != context.Background() && strings.HasPrefix(tc.name, "admin") && !strings.Contains(tc.name, "without home node"); (home.calls == 1) != wantCall {
					t.Fatalf("home lookups=%d", home.calls)
				}
			})
		}
		// Không nối pf/party: Admin không gắn đơn vị không có đơn vị mặc định (không đọc token).
		if got, handled, err := AccountSessionPolicies(nil).DefaultUnit(ctxOf(&emptyUnit, nil)); err != nil || !handled || got != uuid.Nil {
			t.Fatalf("nil home: %s %v %v", got, handled, err)
		}
	})

	t.Run("support audit", func(t *testing.T) {
		got, err := pol.SupportAudit(ctxOf(&supported, legacyConflict))
		if err != nil {
			t.Fatal(err)
		}
		want := accountsport.SupportAuditContext{ContextID: supported.ID(), SessionID: support, OnBehalfOf: onBehalf,
			Audience: extport.SessionRoleCommune, Department: "trong_trot", Reason: "Hỗ trợ theo cuộc gọi"}
		if got != want {
			t.Fatalf("audit=%+v want %+v", got, want)
		}
		for _, tc := range []struct {
			name   string
			ctx    context.Context
			status int
		}{
			{"unauthenticated", context.Background(), 401},
			{"native request", ctxOf(nil, nil), 428},
			{"legacy support only", ctxOf(nil, legacyConflict), 428},
			{"owner not in support mode", ctxOf(&plain, legacyConflict), 403},
			{"support without session id", ctxOf(&noSupportID, nil), 403},
			{"zero session", ctxOf(&zero, legacyConflict), 403},
			{"foreign profile", ctxOf(&foreign, legacyConflict), 403},
			{"malformed payload", ctxOf(&malformed, legacyConflict), 403},
			{"missing payload", ctxOf(&missing, legacyConflict), 403},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got, err := pol.SupportAudit(tc.ctx)
				if statusOf(err) != tc.status || got != (accountsport.SupportAuditContext{}) {
					t.Fatalf("audit=%+v err=%v want status %d and empty audit", got, err, tc.status)
				}
			})
		}
	})
}

type fakeHomeUnits struct {
	units  map[uuid.UUID]uuid.UUID
	broken uuid.UUID
	calls  int
}

func (f *fakeHomeUnits) DepartmentUnit(_ context.Context, org uuid.UUID) (uuid.UUID, error) {
	f.calls++
	if org == f.broken {
		return uuid.Nil, problem.Internal
	}
	if u, ok := f.units[org]; ok {
		return u, nil
	}
	return uuid.Nil, partyport.ErrOrgNotFound
}

func statusOf(err error) int {
	if err == nil {
		return 0
	}
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Status
	}
	return -1
}
