package app_test

import (
	"context"
	khtcpapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	fixture "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/khtcdeadline209"
	"github.com/google/uuid"
	"testing"
)

func TestClose211FreshMFAFixtureBinding(t *testing.T) {
	for _, mfa := range []bool{true, false} {
		p := fixture.Bind(t, authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), MFA: mfa, KN: &authz.KNContext{ID: uuid.New(), Role: authz.KNTinh, Department: khtcDept, OrgRef: uuid.New(), Scope: authz.Scope{Units: []string{"X1"}}}})
		actual, k, err := ext.RequireSession(authz.WithPrincipal(context.Background(), p))
		if err != nil || actual.MFA != mfa || k.Role != authz.KNTinh {
			t.Fatalf("fresh owner binding rejected MFA=%v: %v", mfa, err)
		}
	}
}
func TestClose211MissingReceiptBeforeDatabase(t *testing.T) {
	db := &noDB209{}
	s := khtcpapp.Service{DB: db}
	if _, err := s.Close(context.Background(), uuid.New(), "không ghi khi thiếu cổng", "missing"); err == nil || db.calls != 0 {
		t.Fatal("missing receipt touched DB")
	}
}
