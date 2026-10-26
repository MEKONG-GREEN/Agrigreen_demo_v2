package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type unitPolicyDepartments struct {
	calls int
	unit  uuid.UUID
}

func (d *unitPolicyDepartments) DepartmentUnit(context.Context, uuid.UUID) (uuid.UUID, error) {
	d.calls++
	return d.unit, nil
}
func (*unitPolicyDepartments) UnitDepartments(context.Context, uuid.UUID) ([]partyport.Department, error) {
	return nil, nil
}
func (*unitPolicyDepartments) DepartmentByCode(context.Context, uuid.UUID, string) (partyport.Department, error) {
	return partyport.Department{}, nil
}

type unitPolicyDB struct {
	calls int
	err   error
}

func (d *unitPolicyDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	d.calls++
	return nil, d.err
}

// A configured provider may decline native traffic, but cannot turn a selected
// owner into token-org authority. Reject before native lookup or transaction.
func TestListInternalUnitsRejectsUnhandledSelectedOwner(t *testing.T) {
	selected, err := authz.NewSession("synthetic_owner", uuid.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	nativeUnit := uuid.New()
	dbErr := errors.New("synthetic database boundary reached")
	unhandled := func(context.Context) (uuid.UUID, bool, error) { return uuid.Nil, false, nil }
	for _, tc := range []struct {
		name     string
		selected bool
		callback bool
		explicit bool
	}{
		{"selected without resolver", true, false, false},
		{"selected declined by resolver", true, true, false},
		{"explicit query cannot bypass declined owner", true, true, true},
		{"native without resolver", false, false, false},
		{"native declined by resolver", false, true, false},
		{"native explicit query", false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := &unitPolicyDepartments{unit: nativeUnit}
			db := &unitPolicyDB{err: dbErr}
			svc := app.Service{DB: db, Departments: deps}
			if tc.callback {
				svc.SessionPolicies = port.SessionPolicies{DefaultUnit: unhandled}
			}
			p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), OrgRef: uuid.New()}
			if tc.selected {
				p.SelectedSession = &selected
			}
			query := uuid.Nil
			if tc.explicit {
				query = nativeUnit
			}
			_, gotUnit, err := svc.ListInternalUnits(authz.WithPrincipal(context.Background(), p), query, false)
			if tc.selected {
				if !errors.Is(err, problem.Forbidden) || deps.calls != 0 || db.calls != 0 || gotUnit != uuid.Nil {
					t.Fatalf("selected owner fell back: err=%v ancestors=%d tx=%d unit=%s", err, deps.calls, db.calls, gotUnit)
				}
				return
			}
			ancestors := 1
			if tc.explicit {
				ancestors = 0
			}
			if !errors.Is(err, dbErr) || deps.calls != ancestors || db.calls != 1 || gotUnit != nativeUnit {
				t.Fatalf("native behavior changed: err=%v ancestors=%d tx=%d unit=%s", err, deps.calls, db.calls, gotUnit)
			}
		})
	}
}

// The configured owner remains authoritative even while the compatibility bridge exists.
func TestUnitPolicyDoesNotFallBackToLegacy(t *testing.T) {
	selected, err := authz.NewSession("synthetic_owner", uuid.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	legacyUnit, ownerUnit := uuid.New(), uuid.New()
	denied, dbErr := errors.New("owner denied"), errors.New("database boundary")
	for _, tc := range []struct {
		name    string
		policy  func(context.Context) (uuid.UUID, bool, error)
		want    error
		unit    uuid.UUID
		dbCalls int
	}{
		{"provider error", func(context.Context) (uuid.UUID, bool, error) { return uuid.Nil, true, denied }, denied, uuid.Nil, 0},
		{"provider declined", func(context.Context) (uuid.UUID, bool, error) { return uuid.Nil, false, nil }, problem.Forbidden, uuid.Nil, 0},
		{"owner unit wins", func(context.Context) (uuid.UUID, bool, error) { return ownerUnit, true, nil }, dbErr, ownerUnit, 1},
		{"unconfigured compatibility", nil, dbErr, legacyUnit, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := &unitPolicyDepartments{unit: uuid.New()}
			database := &unitPolicyDB{err: dbErr}
			svc := app.Service{DB: database, Departments: deps, SessionPolicies: port.SessionPolicies{DefaultUnit: tc.policy}}
			p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), OrgRef: uuid.New(), SelectedSession: &selected, KN: &authz.KNContext{UnitRef: legacyUnit}}
			_, unit, got := svc.ListInternalUnits(authz.WithPrincipal(context.Background(), p), uuid.Nil, false)
			if !errors.Is(got, tc.want) || unit != tc.unit || database.calls != tc.dbCalls || deps.calls != 0 {
				t.Fatalf("err=%v unit=%s db=%d nativeLookup=%d", got, unit, database.calls, deps.calls)
			}
		})
	}
}

func TestSupportPolicyErrorCannotUseLegacyAudit(t *testing.T) {
	denied := errors.New("owner denied support")
	database := &unitPolicyDB{}
	svc := app.Service{DB: database, SessionPolicies: port.SessionPolicies{SupportAudit: func(context.Context) (port.SupportAuditContext, error) { return port.SupportAuditContext{}, denied }}}
	p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New()}
	// Legacy support presence must not suppress a configured owner error.
	p.KN = &authz.KNContext{Support: &authz.KNSupport{}}
	err := svc.LogSupportAction(authz.WithPrincipal(context.Background(), p), app.SupportActionInput{Phase: "attempted"})
	if problem.From(err).Code != problem.Forbidden.Code || !errors.Is(err, denied) || database.calls != 0 {
		t.Fatalf("err=%v db=%d", err, database.calls)
	}
}

func TestUnconfiguredUnitPolicyKeepsEmptyLegacyUnitFallback(t *testing.T) {
	selected, err := authz.NewSession("synthetic_owner", uuid.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	nativeUnit := uuid.New()
	deps := &unitPolicyDepartments{unit: nativeUnit}
	dbErr := errors.New("database boundary")
	database := &unitPolicyDB{err: dbErr}
	svc := app.Service{DB: database, Departments: deps}
	p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), OrgRef: uuid.New(), SelectedSession: &selected, KN: &authz.KNContext{}}
	_, unit, got := svc.ListInternalUnits(authz.WithPrincipal(context.Background(), p), uuid.Nil, false)
	if !errors.Is(got, dbErr) || unit != nativeUnit || deps.calls != 1 || database.calls != 1 {
		t.Fatalf("legacy empty-unit behavior changed: err=%v unit=%s native=%d db=%d", got, unit, deps.calls, database.calls)
	}
}
