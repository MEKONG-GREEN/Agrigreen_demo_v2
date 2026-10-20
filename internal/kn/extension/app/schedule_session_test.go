package app

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type scheduleResolverProbe struct{ calls int }

func (r *scheduleResolverProbe) KNContext(context.Context) (accountsport.KNContext, bool, error) {
	r.calls++
	return accountsport.KNContext{Role: port.KNRoleAdmin}, true, nil
}

type scheduleOrgsProbe struct {
	items map[uuid.UUID]partyport.Org
	calls []uuid.UUID
	err   error
}

func (r *scheduleOrgsProbe) Get(_ context.Context, id uuid.UUID) (partyport.Org, error) {
	r.calls = append(r.calls, id)
	if r.err != nil {
		return partyport.Org{}, r.err
	}
	o, ok := r.items[id]
	if !ok {
		return partyport.Org{}, partyport.ErrOrgNotFound
	}
	return o, nil
}
func scheduleSelected(t *testing.T, k port.SessionContext) authz.Principal {
	t.Helper()
	k.ID = uuid.New()
	selected, err := port.NewSessionContext(k)
	if err != nil {
		t.Fatal(err)
	}
	return authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), Roles: []string{"platform_admin"}, SelectedSession: &selected}
}
func scheduleStatus(t *testing.T, err error, want int) {
	t.Helper()
	var p *problem.Problem
	if !errors.As(err, &p) || p.Status != want {
		t.Fatalf("error=%v want status%d", err, want)
	}
}

func TestScheduleOwnerPrecedesLegacyResolver(t *testing.T) {
	unit, province := uuid.New(), uuid.New()
	orgs := &scheduleOrgsProbe{items: map[uuid.UUID]partyport.Org{
		unit: {ParentRef: province}, province: {Kind: partyport.OrgTTKNProvince, AdminUnitCode: "89"},
	}}
	resolver := &scheduleResolverProbe{}
	s := &Service{Orgs: orgs, KNContext: resolver}
	p := scheduleSelected(t, port.SessionContext{Role: port.SessionRoleProvince, Department: "chan_nuoi_thuy_san",
		OrgRef: uuid.New(), UnitRef: unit, MgmtLevel: " TRUONG_PHONG ", InternalUnit: "station-1",
		Duties: []string{port.SessionDutyScheduleFollower}, Communes: []string{"30001"}})
	// Legacy state grants the opposite policy; raw realm state is also broader.
	p.KN = &authz.KNContext{Role: authz.KNAdmin, Manager: true}
	a, err := s.knActor(authz.WithPrincipal(context.Background(), p))
	if err != nil {
		t.Fatal(err)
	}
	if a.Role != port.KNRoleTinh || a.Department != "chan_nuoi_thuy_san" || a.ProvinceCode != "89" ||
		a.MgmtLevel != "truong_phong" || a.InternalUnit != "station-1" || a.ActorRef != p.ActorRef || a.TenantID != p.TenantID ||
		resolver.calls != 0 || len(orgs.calls) != 2 || orgs.calls[0] != unit {
		t.Fatalf("owner projection/identity/resolver mismatch: actor=%+v resolver=%d orgcalls=%v", a, resolver.calls, orgs.calls)
	}
	follower := ScheduleSettings{ApproverRule: port.ApproverRuleFollower}
	if !a.mayApprove(follower) {
		t.Fatal("owner follower duty denied")
	}
	p.KN = nil
	a, err = s.knActor(authz.WithPrincipal(context.Background(), p))
	if err != nil || !a.mayApprove(follower) || resolver.calls != 0 {
		t.Fatalf("owner without legacy: %v", err)
	}
	// No duty in the owner must not borrow the legacy manager or follower flags.
	p = scheduleSelected(t, port.SessionContext{Role: port.SessionRoleProvince, UnitRef: unit})
	p.KN = &authz.KNContext{Role: authz.KNTinh, Manager: true, Duties: []string{authz.DutyScheduleFollower}}
	a, err = s.knActor(authz.WithPrincipal(context.Background(), p))
	if err != nil || a.mayApprove(follower) {
		t.Fatalf("legacy policy borrowed: %v", err)
	}
}

func TestScheduleInvalidOwnerNeverFallsBack(t *testing.T) {
	valid := scheduleSelected(t, port.SessionContext{Role: port.SessionRoleAdmin})
	wrong, err := authz.NewSession("other", uuid.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	zero, empty, bad := authz.Session{}, valid.SelectedSession.WithPayload(nil), valid.SelectedSession.WithPayload([]byte("{"))
	for _, tc := range []struct {
		name     string
		selected *authz.Session
		status   int
	}{
		{"legacy only", nil, 428}, {"zero", &zero, 403}, {"wrong profile", &wrong, 403},
		{"empty payload", &empty, 403}, {"malformed", &bad, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &scheduleResolverProbe{}
			orgs := &scheduleOrgsProbe{}
			s := &Service{KNContext: resolver, Orgs: orgs}
			p := valid
			p.SelectedSession = tc.selected
			p.KN = &authz.KNContext{Role: authz.KNAdmin}
			_, err := s.knActor(authz.WithPrincipal(context.Background(), p))
			scheduleStatus(t, err, tc.status)
			if resolver.calls != 0 || len(orgs.calls) != 0 {
				t.Fatal("invalid owner reached dependency")
			}
		})
	}
	resolver := &scheduleResolverProbe{}
	s := &Service{KNContext: resolver}
	p := valid
	p.SelectedSession, p.KN = nil, nil
	a, err := s.knActor(authz.WithPrincipal(context.Background(), p))
	if err != nil || a.Role != port.KNRoleAdmin || resolver.calls != 1 {
		t.Fatalf("native resolver compatibility: %v", err)
	}
	_, err = s.knActor(context.Background())
	scheduleStatus(t, err, 401)
}

func TestScheduleViewOnlyCannotWriteOrApprove(t *testing.T) {
	for _, manager := range []bool{false, true} {
		t.Run(map[bool]string{false: "follower", true: "manager"}[manager], func(t *testing.T) {
			node := uuid.New()
			s := &Service{Orgs: &scheduleOrgsProbe{items: map[uuid.UUID]partyport.Org{node: {Kind: partyport.OrgTTKNProvince, AdminUnitCode: "89"}}}}
			p := scheduleSelected(t, port.SessionContext{Role: port.SessionRoleProvince, Department: "chan_nuoi_thuy_san",
				UnitRef: node, ViewOnly: true, Manager: manager, Duties: []string{port.SessionDutyScheduleFollower}})
			ctx := authz.WithPrincipal(context.Background(), p)
			a, err := s.knActor(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !a.ReadOnly || a.mayApprove(ScheduleSettings{ApproverRule: port.ApproverRuleFollower}) || a.mayApprove(ScheduleSettings{ApproverRule: port.ApproverRuleLevels}) {
				t.Fatal("view-only retained approval")
			}
			sc := domain.Schedule{Department: a.Department, ProvinceCode: a.ProvinceCode, OwnerRef: uuid.New()}
			if a.canReview(sc, ScheduleSettings{}) == nil {
				t.Fatal("view-only can review")
			}
			// No DB/clock configured: denial must happen before validation or I/O.
			_, err = s.SaveSchedule(ctx, ScheduleSaveInput{})
			scheduleStatus(t, err, 403)
			_, err = s.ReviewSchedule(ctx, ReviewInput{})
			scheduleStatus(t, err, 403)
			_, err = s.BulkApprove(ctx, nil, "")
			scheduleStatus(t, err, 403)
			_, err = s.CopyPreviousWeek(ctx, "", "")
			scheduleStatus(t, err, 403)
			_, err = s.Checkin(ctx, CheckinRequest{})
			scheduleStatus(t, err, 403)
		})
	}
}

func TestScheduleOwnerProvinceFailureAndBoundedWalk(t *testing.T) {
	node := uuid.New()
	p := scheduleSelected(t, port.SessionContext{Role: port.SessionRoleProvince, UnitRef: node})
	ctx := authz.WithPrincipal(context.Background(), p)
	s := &Service{}
	_, err := s.knActor(ctx)
	scheduleStatus(t, err, 503)
	failure := errors.New("lookup unavailable")
	orgs := &scheduleOrgsProbe{err: failure}
	s.Orgs = orgs
	if _, err = s.knActor(ctx); !errors.Is(err, failure) {
		t.Fatalf("lookup error swallowed: %v", err)
	}
	orgs.err = nil
	a, err := s.knActor(ctx)
	if err != nil || a.ProvinceCode != "" {
		t.Fatalf("unknown node province: %v", err)
	}
	orgs.items = map[uuid.UUID]partyport.Org{node: {ParentRef: node}}
	orgs.calls = nil
	a, err = s.knActor(ctx)
	if err != nil || a.ProvinceCode != "" || len(orgs.calls) != 4 {
		t.Fatalf("unbounded ancestry: %v %d", err, len(orgs.calls))
	}
}
