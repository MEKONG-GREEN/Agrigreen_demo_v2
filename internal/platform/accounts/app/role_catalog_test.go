package app

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

func TestRoleCatalogInvalidConfigurationFailsBeforeDependencies(t *testing.T) {
	s := &Service{Roles: &port.RoleCatalog{}}
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{
		ActorRef: ids.New(), TenantID: ids.New(), Roles: []string{authz.RolePlatformAdmin}, MFA: true,
	})
	if _, err := s.caller(ctx); !errors.Is(err, ErrDependency) {
		t.Fatalf("invalid configured catalog did not fail closed: %v", err)
	}
	if _, err := s.HandleOrgStatus(context.Background(), nil, outbox.Envelope{}); !errors.Is(err, ErrDependency) {
		t.Fatalf("worker invalid catalog reached dependencies or fell back: %v", err)
	}
	if s.RoleCatalog().Valid() || len(s.RoleCatalog().Grantable([]string{authz.RolePlatformAdmin})) != 0 {
		t.Fatal("configured zero catalog acquired legacy grants")
	}
}
