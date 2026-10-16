package assembly

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// A generic profile must not infer an application session unless that application registers its resolver.
func TestFormsRequiresRegisteredSessionProjection(t *testing.T) {
	profile := formsport.Profile{Name: "isolated", Hierarchy: []string{"UPPER", "LEAF"},
		Roles: map[string]string{authz.KNTinh: "UPPER"}, PublishBy: "UPPER", CloseBy: "UPPER", DefineBy: []string{"author"}}
	module, err := forms.New(nil, nil, forms.Deps{Profiles: []formsport.Profile{profile}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: ids.New(),
		KN: &authz.KNContext{Role: authz.KNTinh, Scope: authz.Scope{All: true}, OrgRef: ids.New()}})
	_, err = module.Publisher().LatestTemplate(ctx, profile.Name, "unused")
	var p *problem.Problem
	if !errors.As(err, &p) || p.Code != problem.Forbidden.Code {
		t.Fatalf("unregistered application session inferred: %v", err)
	}
}
