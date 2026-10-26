package app

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func TestContextNavigationErrorsCannotFallBackOrChangeIdentity(t *testing.T) {
	p := authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), Roles: []string{authz.RolePlatformAdmin}, MFA: true}
	ctx := authz.WithPrincipal(context.Background(), p)
	calls := 0
	svc := Service{Navigation: func(got context.Context, in partyport.NavigationRequest) (string, error) {
		principal, ok := authz.FromContext(got)
		if !ok || principal.ActorRef != p.ActorRef || principal.TenantID != p.TenantID || !principal.MFA || !principal.Has(authz.RolePlatformAdmin) {
			t.Fatal("navigation changed identity")
		}
		if in.Audience != port.KNAdmin || in.Department.Code != "" {
			t.Fatal("unvalidated context projected")
		}
		calls++
		return "", problem.Forbidden
	}}
	if _, err := svc.contextView(ctx, store.Context{Role: port.KNAdmin}, nil); !errors.Is(err, problem.Forbidden) || calls != 1 {
		t.Fatalf("navigation denial fell back:%d %v", calls, err)
	}
	svc.Navigation = nil
	v, err := svc.contextView(ctx, store.Context{Role: port.KNAdmin}, nil)
	if err != nil || v.DefaultRoute != "/kn/tong-quan" {
		t.Fatalf("unconfigured native changed:%+v %v", v, err)
	}
}
