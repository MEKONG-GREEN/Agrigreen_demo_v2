package app

import (
	"context"
	"slices"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type fixtureActorKey struct{}

func fixtureContext(ctx context.Context, principal authz.Principal, subject port.ResolvedActor) context.Context {
	return context.WithValue(authz.WithPrincipal(ctx, principal), fixtureActorKey{}, subject)
}

// Tests provide an independent application adapter rather than relying on a built-in application policy.
func fixtureActorResolver(ctx context.Context, _ authz.Principal, profile port.Profile) (port.ResolvedActor, bool, error) {
	subject, ok := ctx.Value(fixtureActorKey{}).(port.ResolvedActor)
	if !ok {
		return port.ResolvedActor{}, false, nil
	}
	if len(profile.Departments) > 0 && !slices.Contains(profile.DefineBy, subject.Role) && !slices.Contains(profile.Departments, subject.Department) {
		return port.ResolvedActor{}, true, problem.Forbidden
	}
	return subject, true, nil
}
