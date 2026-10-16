package app

import (
	"context"
	"errors"
	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"testing"
	"time"
)

type policyParamSpy struct{ calls int }

func (p *policyParamSpy) At(context.Context, string, time.Time) (params.ParamValue, error) {
	p.calls++
	return params.ParamValue{}, params.ErrParamNotFound
}

func TestReportPolicyRequiresRegistration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		registry *PolicyRegistry
	}{{name: "missing"}, {name: "empty", registry: NewPolicyRegistry()}} {
		t.Run(tc.name, func(t *testing.T) {
			svc := Service{Catalog: NewCatalogRegistry(contractCatalogContribution(t)), Policies: tc.registry}
			for _, role := range []string{"kn_officer", "kn_province_admin", "kn_national_admin"} {
				p := authz.Principal{Roles: []string{role}}
				if hasAny(p, svc.readerRoles()) || hasAny(p, svc.schedulerRoles()) || hasAny(p, svc.formReaderRoles()) {
					t.Errorf("unregistered role inferred operation grant: %s", role)
				}
				for source := range svc.sourceSpecs() {
					if hasAny(p, svc.sourceRoles(source)) {
						t.Errorf("unregistered role inferred source grant: %s %s", role, source)
					}
				}
				if _, _, err := svc.formReader(authz.WithPrincipal(context.Background(), p)); !errors.Is(err, problem.Forbidden) {
					t.Errorf("unregistered realm form read: %s %v", role, err)
				}
				p.Roles = append(p.Roles, ccport.RoleCoopManager)
				if !svc.coopOnly(p) {
					t.Errorf("unregistered role widened HTX scope: %s", role)
				}
			}
			spy := &policyParamSpy{}
			svc.Params = spy
			if _, err := svc.attachmentLimit(context.Background(), time.Time{}); err != nil {
				t.Fatal(err)
			}
			if spy.calls != 0 {
				t.Errorf("unregistered application parameter was queried %d times", spy.calls)
			}
		})
	}
}
