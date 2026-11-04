package app_test

import (
	"context"
	"errors"
	fixture "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/khtcdeadline209"
	"testing"

	khtcpapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type noDB209 struct{ calls int }

func (d *noDB209) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	d.calls++
	return nil, errors.New("unexpected database access without conditional port")
}

type noPublisher209 struct{ formsport.Publisher }
type noControl209 struct{ formsport.PeriodControl }

// Authorized caller; dependency rejection must happen before any intent/native
// write or database read, so this test has no DB URL and cannot silently SKIP.
func TestDeadline209MissingCASBeforeDatabase(t *testing.T) {
	p := fixture.Bind(t, authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), KN: &authz.KNContext{Role: authz.KNTinh, Department: khtcDept, OrgRef: uuid.New(), Scope: authz.Scope{Units: []string{"X1"}}}})
	db := &noDB209{}
	s := khtcpapp.Service{DB: db, Forms: noPublisher209{}, Control: noControl209{}}
	_, err := s.ChangeDeadline(authz.WithPrincipal(context.Background(), p), uuid.New(), khtcpapp.DeadlineInput{}, "pure209")
	var pr *problem.Problem
	if !errors.As(err, &pr) || pr.Status != 503 || db.calls != 0 {
		t.Fatalf("missing conditional capability: error=%v dbCalls=%d; want503 before DB", err, db.calls)
	}
}
