package app

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type assignmentRuleProbe struct {
	calls              int
	seen               port.AssignmentRuleValues
	denied, nodeDenied error
	mutate             bool
}

func (r *assignmentRuleProbe) ValidateAssignment(v port.AssignmentRuleValues) error {
	r.calls++
	r.seen = v
	r.seen.Departments = slices.Clone(v.Departments)
	r.seen.Communes = slices.Clone(v.Communes)
	if r.mutate {
		v.Role, v.OrgRef, v.MgmtLevel = "admin", ids.New(), "changed"
		if len(v.Departments) > 0 {
			v.Departments[0] = "changed"
		}
		if len(v.Communes) > 0 {
			v.Communes[0] = "99999"
		}
	}
	return r.denied
}
func (r *assignmentRuleProbe) ValidateNodeKind(_, _ string) error { return r.nodeDenied }

func TestAssignmentRulesNilPreservesLegacyBehaviorAndErrorOrder(t *testing.T) {
	now := time.Date(2026, 10, 21, 3, 0, 0, 999, time.UTC)
	past := now.Add(-time.Hour)
	org := ids.New()
	cases := []domain.AssignmentInput{
		{Role: port.KNXa, OrgRef: org, Departments: []string{"depta", " depta "}, Communes: []string{"30301", "30292", "30301"}},
		{Role: "bad_role", Departments: []string{"!"}, ValidTo: &past},
		{Role: port.KNTinh, OrgRef: org, Departments: []string{"depta"}, MgmtLevel: "invalid", ValidTo: &past},
		{Role: port.KNAdmin, Departments: []string{"depta"}, Communes: []string{"30292"}},
		{Role: port.KNTinh},
		{Role: port.KNXa, OrgRef: org, Departments: []string{"depta"}, ValidTo: &past},
	}
	svc := &Service{}
	for _, in := range cases {
		want, wantErr := domain.NormalizeAssignment(in, now)
		got, gotErr := svc.normalizeAssignment(in, now)
		if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErr, wantErr) {
			t.Fatalf("legacy behavior changed: %+v %v want %+v %v", got, gotErr, want, wantErr)
		}
	}
	for _, role := range []string{port.KNTW, port.KNXa, port.KNAdmin} {
		for _, kind := range []string{"ttkn_national", "ttkn_province", "extension_team"} {
			if !reflect.DeepEqual(svc.validateAssignmentNodeKind(role, kind), domain.KNRoleFitsOrg(role, kind)) {
				t.Fatal("legacy node-kind behavior changed")
			}
		}
	}
}

func TestAssignmentRulesNormalizeBeforeProviderAndIgnoreMutation(t *testing.T) {
	zone := time.FixedZone("fixture", 7*3600)
	now := time.Date(2026, 10, 21, 10, 0, 0, 123456789, zone)
	until := now.Add(time.Hour)
	in := domain.AssignmentInput{Role: " kn_xa ", OrgRef: ids.New(), MgmtLevel: " fixture_level ",
		Departments: []string{"deptb", " depta ", "deptb"}, Communes: []string{"30301", "30292", "30301"}, ValidTo: &until}
	provider := &assignmentRuleProbe{mutate: true}
	svc := &Service{AssignmentRules: provider}
	got, err := svc.normalizeAssignment(in, now)
	if err != nil || provider.calls != 1 {
		t.Fatal("configured provider was not used", err)
	}
	if got.Role != "kn_xa" || got.OrgRef != in.OrgRef || got.MgmtLevel != "fixture_level" ||
		!slices.Equal(got.Departments, []string{"depta", "deptb"}) || !slices.Equal(got.Communes, []string{"30292", "30301"}) ||
		got.ValidFrom.Location() != time.UTC || !got.ValidFrom.Equal(now.UTC().Truncate(time.Microsecond)) ||
		got.ValidTo == in.ValidTo || !got.ValidTo.Equal(until.UTC().Truncate(time.Microsecond)) {
		t.Fatalf("provider altered normalized data or validity: %+v", got)
	}
	if !slices.Equal(in.Departments, []string{"deptb", " depta ", "deptb"}) ||
		!slices.Equal(in.Communes, []string{"30301", "30292", "30301"}) || !until.Equal(now.Add(time.Hour)) {
		t.Fatal("normalization mutated caller input")
	}
	if provider.seen.Role != got.Role || provider.seen.MgmtLevel != got.MgmtLevel ||
		!slices.Equal(provider.seen.Departments, got.Departments) || !slices.Equal(provider.seen.Communes, got.Communes) {
		t.Fatal("provider did not see normalized values")
	}

}

func TestAssignmentRulesRejectsInvalidStructureBeforePermissiveProvider(t *testing.T) {
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	past, same := now.Add(-time.Hour), now
	cases := []domain.AssignmentInput{
		{Role: port.KNTinh, ValidTo: &past},
		{Role: port.KNTinh, ValidTo: &same},
		{Role: port.KNTinh, Departments: []string{"bad code"}},
		{Role: port.KNXa, Communes: []string{"bad"}},
		{Role: "bad role"},
		{Role: port.KNTinh, MgmtLevel: "bad code"},
	}
	for _, in := range cases {
		rules := &assignmentRuleProbe{}
		_, err := (&Service{AssignmentRules: rules}).normalizeAssignment(in, now)
		var validation domain.FieldError
		if !errors.As(err, &validation) || rules.calls != 0 {
			t.Fatalf("permissive provider bypassed structural validation: %v calls%d", err, rules.calls)
		}
	}
}

func TestAssignmentRulesConfiguredDenialNeverFallsBack(t *testing.T) {
	now := time.Now()
	in := domain.AssignmentInput{Role: port.KNTinh, OrgRef: ids.New(), Departments: []string{"depta"}}
	if _, err := domain.NormalizeAssignment(in, now); err != nil {
		t.Fatal("fixture must pass legacy validation", err)
	}
	denied := errors.New("fixture owner unavailable")
	provider := &assignmentRuleProbe{denied: denied, nodeDenied: denied}
	svc := &Service{AssignmentRules: provider}
	if _, err := svc.normalizeAssignment(in, now); !errors.Is(err, denied) || provider.calls != 1 {
		t.Fatal("configured error fell back to legacy", err)
	}
	if err := svc.validateAssignmentNodeKind(port.KNTinh, "ttkn_province"); !errors.Is(err, denied) {
		t.Fatal("configured node-kind error fell back to legacy", err)
	}
	for _, err := range []error{port.AssignmentRuleError{Field: "role", Msg: "fixture denied"}, &port.AssignmentRuleError{Field: "role", Msg: "fixture denied"}} {
		var validation *problem.Problem
		if got := fieldProblem(err); !errors.As(got, &validation) || validation.Code != "validation_failed" ||
			len(validation.Errors) != 1 || validation.Errors[0].Field != "role" || validation.Errors[0].Reason != "fixture denied" {
			t.Fatal("owner validation error was not mapped with its field", got)
		}
	}
}
