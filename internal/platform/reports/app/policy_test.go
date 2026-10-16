package app

import (
	"context"
	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
	"reflect"
	"testing"
	"time"
)

func TestRegisteredReportPolicyLegacyParity(t *testing.T) {
	old := Service{}
	registered := Service{Policies: NewPolicyRegistry(port.Policy{
		ReaderRoles: knRoles, FormReaderRoles: knRoles,
		SchedulerRoles: knRoles[1:3], OrganizationScopeRoles: knRoles,
		SourceRoles:          map[string][]string{"extension": knRoles, "cropcycle": knRoles},
		AttachmentLimitParam: legacyAttachmentLimitParam(),
	})}
	var cropSource string
	for source, spec := range domain.Sources {
		if spec.Module == "cropcycle" {
			cropSource = source
			break
		}
	}
	if cropSource == "" {
		t.Fatal("missing HTX source")
	}
	for _, role := range append(append([]string{}, allReaders...), "unknown") {
		for _, mixed := range []bool{false, true} {
			p := authz.Principal{ActorRef: uuid.New(), OrgRef: uuid.New(), Roles: []string{role}}
			if mixed {
				p.Roles = append(p.Roles, ccport.RoleCoopManager)
			}
			if hasAny(p, old.readerRoles()) != hasAny(p, registered.readerRoles()) || hasAny(p, old.schedulerRoles()) != hasAny(p, registered.schedulerRoles()) || hasAny(p, old.formReaderRoles()) != hasAny(p, registered.formReaderRoles()) || old.coopOnly(p) != registered.coopOnly(p) {
				t.Fatalf("role/scope parity lost role=%s mixed=%v", role, mixed)
			}
			for source := range domain.Sources {
				if hasAny(p, old.sourceRoles(source)) != hasAny(p, registered.sourceRoles(source)) {
					t.Fatalf("source policy changed role=%s source=%s mixed=%v", role, source, mixed)
				}
			}
			sc := authz.Scope{Units: []string{"89"}}
			before, after := domain.Params{}, domain.Params{}
			a, errA := old.jobScope(p, sc, domain.Definition{Source: cropSource}, &before)
			b, errB := registered.jobScope(p, sc, domain.Definition{Source: cropSource}, &after)
			if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(before, after) || (errA == nil) != (errB == nil) {
				t.Fatalf("HTX scope changed role=%s mixed=%v", role, mixed)
			}
			job := domain.Job{RequestedBy: uuid.New(), Def: domain.Definition{Source: cropSource}, ScopeUnits: []string{"89"}, Params: domain.Params{CooperativeRef: p.OrgRef.String()}}
			if old.jobVisible(p, sc, job) != registered.jobVisible(p, sc, job) {
				t.Fatalf("export visibility changed role=%s mixed=%v", role, mixed)
			}

			sch := domain.Schedule{OwnerRef: uuid.New(), ScopeUnits: []string{"89"}, Params: domain.Params{CooperativeRef: p.OrgRef.String()}}
			if old.scheduleVisible(p, sc, sch) != registered.scheduleVisible(p, sc, sch) {
				t.Fatalf("schedule visibility changed role=%s mixed=%v", role, mixed)
			}
		}
	}
	if registered.Policies.AttachmentLimitParam() != legacyAttachmentLimitParam() {
		t.Fatal("attachment parameter changed")
	}
}

func TestRegisteredReportPolicyDoesNotLeakSources(t *testing.T) {
	svc := Service{Policies: NewPolicyRegistry(port.Policy{ReaderRoles: []string{"module_reader"}, SourceRoles: map[string][]string{"extension": {"module_reader"}}})}
	p := authz.Principal{Roles: []string{"module_reader"}}
	if !hasAny(p, svc.readerRoles()) || hasAny(p, svc.schedulerRoles()) || hasAny(p, svc.formReaderRoles()) {
		t.Fatal("registration granted a different operation")
	}
	for source, spec := range domain.Sources {
		if hasAny(p, svc.sourceRoles(source)) != (spec.Module == "extension") {
			t.Fatalf("source grant leaked: %s", source)
		}
	}
	if _, err := svc.attachmentLimit(context.Background(), time.Time{}); err != nil {
		t.Fatal(err)
	}
}
