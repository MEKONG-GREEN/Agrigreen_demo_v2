package app

import (
	"context"
	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
	"reflect"
	"testing"
	"time"
)

// Fixed role matrix preserves the behavior proved against the old adapter in C.
func TestRegisteredReportPolicyRoleMatrix(t *testing.T) {
	roles := []string{"registered_reader", "registered_manager", "registered_national"}
	svc := Service{Catalog: NewCatalogRegistry(contractCatalogContribution(t)), Policies: NewPolicyRegistry(port.Policy{
		ReaderRoles: roles, FormReaderRoles: roles, SchedulerRoles: roles[1:], OrganizationScopeRoles: roles,
		SourceRoles: map[string][]string{"extension": roles, "cropcycle": roles}, AttachmentLimitParam: "application.limit",
	})}
	cases := []struct {
		role                                                                         string
		read, schedule, form, extension, mechanization, cropcycle, organizationScope bool
	}{
		{"registered_reader", true, false, true, true, false, true, true},
		{"registered_manager", true, true, true, true, false, true, true},
		{"registered_national", true, true, true, true, false, true, true},
		{mechport.RoleCGHViewer, true, false, false, false, true, false, false},
		{mechport.RoleCGHAdmin, true, true, false, false, true, false, false},
		{ccport.RoleCoopManager, true, true, false, false, false, true, false},
		{port.RoleReportsAdmin, true, false, true, false, false, false, false},
		{port.RoleMasterdataAdmin, true, false, false, false, false, false, false},
		{authz.RolePlatformAdmin, true, true, true, true, true, true, true},
		{"unknown", false, false, false, false, false, false, false},
	}
	var cropSource string
	for source, spec := range svc.sourceSpecs() {
		if spec.Module == "cropcycle" {
			cropSource = source
			break
		}
	}
	if cropSource == "" {
		t.Fatal("missing HTX source")
	}
	for _, tc := range cases {
		for _, mixed := range []bool{false, true} {
			p := authz.Principal{ActorRef: uuid.New(), OrgRef: uuid.New(), Roles: []string{tc.role}}
			if mixed {
				p.Roles = append(p.Roles, ccport.RoleCoopManager)
			}
			wantCoop := (mixed || tc.role == ccport.RoleCoopManager) && !tc.organizationScope
			if hasAny(p, svc.readerRoles()) != (tc.read || mixed) || hasAny(p, svc.schedulerRoles()) != (tc.schedule || mixed) || hasAny(p, svc.formReaderRoles()) != tc.form || svc.coopOnly(p) != wantCoop {
				t.Fatalf("operation/scope matrix role=%s mixed=%v", tc.role, mixed)
			}
			for source, spec := range svc.sourceSpecs() {
				want := map[string]bool{"extension": tc.extension, "mechanization": tc.mechanization, "cropcycle": tc.cropcycle || mixed}[spec.Module]
				if hasAny(p, svc.sourceRoles(source)) != want {
					t.Fatalf("source matrix role=%s source=%s mixed=%v", tc.role, source, mixed)
				}
			}
			sc := authz.Scope{Units: []string{"89"}}
			query := domain.Params{}
			got, err := svc.jobScope(p, sc, domain.Definition{Source: cropSource}, &query)
			if err != nil {
				t.Fatal(err)
			}
			if wantCoop {
				if !got.Empty() || query.CooperativeRef != p.OrgRef.String() {
					t.Fatalf("HTX scope escaped role=%s mixed=%v", tc.role, mixed)
				}
			} else if !reflect.DeepEqual(got, sc) || query.CooperativeRef != "" {
				t.Fatalf("organization scope lost role=%s mixed=%v", tc.role, mixed)
			}
			job := domain.Job{RequestedBy: uuid.New(), Def: domain.Definition{Source: cropSource}, ScopeUnits: []string{"89"}, Params: domain.Params{CooperativeRef: p.OrgRef.String()}}
			if svc.jobVisible(p, sc, job) != ((tc.cropcycle || mixed) && !wantCoop) {
				t.Fatalf("export visibility role=%s mixed=%v", tc.role, mixed)
			}
			// A cooperative-only schedule can be shared inside the same cooperative,
			// while organization-scoped readers must not infer territory from zero units.
			sch := domain.Schedule{OwnerRef: uuid.New(), Params: domain.Params{CooperativeRef: p.OrgRef.String()}}
			if svc.scheduleVisible(p, sc, sch) != wantCoop {
				t.Fatalf("schedule visibility role=%s mixed=%v", tc.role, mixed)
			}
		}
	}
}

func TestRegisteredReportPolicyDoesNotLeakSources(t *testing.T) {
	svc := Service{Catalog: NewCatalogRegistry(contractCatalogContribution(t)), Policies: NewPolicyRegistry(port.Policy{ReaderRoles: []string{"module_reader"}, SourceRoles: map[string][]string{"extension": {"module_reader"}}})}
	p := authz.Principal{Roles: []string{"module_reader"}}
	if !hasAny(p, svc.readerRoles()) || hasAny(p, svc.schedulerRoles()) || hasAny(p, svc.formReaderRoles()) {
		t.Fatal("registration granted a different operation")
	}
	for source, spec := range svc.sourceSpecs() {
		if hasAny(p, svc.sourceRoles(source)) != (spec.Module == "extension") {
			t.Fatalf("source grant leaked: %s", source)
		}
	}
	if _, err := svc.attachmentLimit(context.Background(), time.Time{}); err != nil {
		t.Fatal(err)
	}
}
