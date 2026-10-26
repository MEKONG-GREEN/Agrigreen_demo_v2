package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
)

func strictTestFreeze(_ context.Context, q port.SourceFreezeRequest) (json.RawMessage, error) {
	var v struct {
		Filter string `json:"filter"`
		AsOf   string `json:"as_of"`
	}
	d := json.NewDecoder(strings.NewReader(string(q.SourceArgs)))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return nil, problem.Validation
	}
	if v.Filter == "" {
		return nil, problem.Validation
	}
	if v.AsOf == "" {
		v.AsOf = q.RequestedAt.Format(time.RFC3339Nano)
	}
	return json.Marshal(v)
}
func ownerPolicyService(now time.Time, authorize func(context.Context, port.SourceJobAccess) error, build port.SourceBuilder) *Service {
	return &Service{Clock: clock.NewFixed(now), Catalog: NewCatalogRegistry(testCatalogRegistration()),
		Builders: NewSourceBuilderRegistry(port.SourceBuilderRegistration{Source: "example.measure", Build: build,
			JobPolicy: &port.SourceJobPolicy{Version: 1, ScopeMode: port.SourceScopeTenant, Freeze: strictTestFreeze, AuthorizeJob: authorize}})}
}
func sourceTestPrincipal() authz.Principal {
	return authz.Principal{TenantID: uuid.New(), ActorRef: uuid.New(), Roles: []string{authz.RolePlatformAdmin}, MFA: true}
}
func sourceTestContext(p authz.Principal) context.Context {
	return authz.WithPrincipal(tenant.WithID(context.Background(), p.TenantID), p)
}
func TestSourcePolicyRegistryClosedAndCopied(t *testing.T) {
	build := func(context.Context, port.SourceBuildQuery) (port.SourceData, error) { return port.SourceData{}, nil }
	good := port.SourceJobPolicy{Version: 1, ScopeMode: port.SourceScopeTenant, Freeze: strictTestFreeze, AuthorizeJob: func(context.Context, port.SourceJobAccess) error { return nil }}
	for _, change := range []func(*port.SourceJobPolicy){
		func(p *port.SourceJobPolicy) { p.Version = 0 }, func(p *port.SourceJobPolicy) { p.ScopeMode = "" },
		func(p *port.SourceJobPolicy) { p.Freeze = nil }, func(p *port.SourceJobPolicy) { p.AuthorizeJob = nil },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("incomplete owner policy accepted")
				}
			}()
			bad := good
			change(&bad)
			NewSourceBuilderRegistry(port.SourceBuilderRegistration{Source: "example.measure", Build: build, JobPolicy: &bad})
		}()
	}
	r := NewSourceBuilderRegistry(port.SourceBuilderRegistration{Source: "example.measure", Build: build, JobPolicy: &good})
	good.Version = 99
	good.AuthorizeJob = nil
	snapshot, ok := r.jobPolicy("example.measure")
	if !ok || snapshot.Version != 1 || snapshot.AuthorizeJob == nil {
		t.Fatal("registry retained policy pointer")
	}
}
func TestSourcePolicyBindsRequestReplayAndHidesErrors(t *testing.T) {
	now := time.Date(2026, 10, 21, 3, 0, 0, 123456000, time.UTC)
	p := sourceTestPrincipal()
	ctx := sourceTestContext(p)
	calls := 0
	build := func(context.Context, port.SourceBuildQuery) (port.SourceData, error) {
		return port.SourceData{}, errors.New("private-marker in builder")
	}
	svc := ownerPolicyService(now, func(_ context.Context, a port.SourceJobAccess) error {
		calls++
		if (a.Operation != port.SourceJobBuild && a.ActorRef != p.ActorRef) || (a.Operation == port.SourceJobBuild && a.ActorRef != uuid.Nil) || a.TenantID != p.TenantID {
			t.Fatal("owner identity not verified")
		}
		a.SourceArgs[2] = 'X'
		return nil
	}, build)
	tpl, _ := svc.builtinTemplate("example.layout")
	in := ExportInput{TemplateKey: tpl.Key, Format: "xlsx", Params: domain.Params{Level: "province"}, SourceArgs: json.RawMessage(`{"filter":"private-marker"}`), IdemKey: "source-policy-01"}
	j, err := svc.sourceRequestJob(ctx, p, tpl, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if j.ScopeAll || len(j.ScopeUnits) > 0 || j.SourceArgs == nil || calls != 1 {
		t.Fatal("tenant source forged geography or missed authorization")
	}
	svc.Clock = clock.NewFixed(now.Add(24 * time.Hour))
	if err = svc.checkSourceReplay(ctx, p, in, j); err != nil {
		t.Fatal(err)
	}
	// PostgreSQL may scan timestamptz with the machine's +07 location.
	// Freeze defaults must still receive canonical UTC server metadata.
	localJob := j
	localJob.RequestedAt = j.RequestedAt.In(time.FixedZone("VN", 7*60*60))
	if err = svc.checkSourceReplay(ctx, p, in, localJob); err != nil {
		t.Fatalf("same instant localzone changed snapshot: %v", err)
	}
	if calls != 3 {
		t.Fatalf("replay skipped current authorization: %d", calls)
	}
	changed := in
	changed.SourceArgs = json.RawMessage(`{"filter":"other"}`)
	if err = svc.checkSourceReplay(ctx, p, changed, j); !errors.Is(err, problem.IdempotencyConflict) {
		t.Fatalf("changed filter replay: %v", err)
	}
	foreign := p
	foreign.ActorRef = uuid.New()
	if err = svc.checkSourceReplay(sourceTestContext(foreign), foreign, in, j); !errors.Is(err, problem.IdempotencyConflict) {
		t.Fatal("foreign actor replay accepted")
	}
	// A forged context cannot call the owner with an arbitrary reader identity.
	if _, err = svc.authorizeSourceJob(ctx, j, uuid.New(), port.SourceJobDetail); err == nil {
		t.Fatal("forged actor accepted")
	}
	if _, err = svc.authorizeSourceJob(tenant.WithID(ctx, uuid.New()), j, p.ActorRef, port.SourceJobDetail); err == nil {
		t.Fatal("tenant mismatch accepted")
	}
	versioned := j
	frozen := *j.SourceArgs
	frozen.Version = 99
	versioned.SourceArgs = &frozen
	if _, err = svc.authorizeSourceJob(ctx, versioned, p.ActorRef, port.SourceJobDetail); err == nil {
		t.Fatal("unknown version accepted")
	}
	// Native callers cannot supply an ignored filter extension.
	native := *svc
	native.Builders = NewSourceBuilderRegistry(port.SourceBuilderRegistration{Source: "example.measure", Build: build})
	if _, err = native.authorizeSourceJob(ctx, j, p.ActorRef, port.SourceJobDetail); err == nil {
		t.Fatal("removed policy fell back to native")
	}
	if _, err = native.sourceRequestJob(ctx, p, tpl, in, nil); err == nil {
		t.Fatal("native dropped custom args")
	}
	// Owner builder errors never leak sensitive arguments; native errors retain behavior.
	if _, err = svc.buildDataset(ctx, j); err == nil || strings.Contains(err.Error(), "private-marker") {
		t.Fatalf("private builder error escaped: %v", err)
	}
	if _, err = native.buildDataset(ctx, domain.Job{Def: tpl.Def, TenantID: p.TenantID}); err == nil || !strings.Contains(err.Error(), "private-marker") {
		t.Fatal("native builder errors changed")
	}
}

type sourceScopeFunc func(context.Context) (authz.Scope, error)

func (f sourceScopeFunc) Scope(ctx context.Context) (authz.Scope, error) { return f(ctx) }

var _ partyport.ScopeResolver = sourceScopeFunc(nil)

func TestNativeSourceReplayRetainsCurrentScopePreconditions(t *testing.T) {
	principal := sourceTestPrincipal()
	principal.Roles = []string{port.RoleReportsAdmin}
	ctx := sourceTestContext(principal)
	job := domain.Job{TenantID: principal.TenantID, RequestedBy: principal.ActorRef, Def: domain.Definition{Source: "native.source"}}
	svc := Service{}
	input := ExportInput{IdemKey: "native-replay-01"}
	for _, resolve := range []sourceScopeFunc{
		func(context.Context) (authz.Scope, error) { return authz.Scope{}, nil },
		func(context.Context) (authz.Scope, error) { return authz.Scope{}, ErrDependency },
	} {
		svc.Scope = resolve
		if err := svc.checkSourceReplay(ctx, principal, input, job); err == nil {
			t.Fatal("native replay bypassed lost/error current scope")
		}
	}
	svc.Scope = sourceScopeFunc(func(context.Context) (authz.Scope, error) { return authz.Scope{Units: []string{"89"}}, nil })
	if err := svc.checkSourceReplay(ctx, principal, input, job); err != nil {
		t.Fatal(err)
	}
}

// Shared canonicalization must preserve ordinary integer JSON for an owner's
// strict decoder; converting 1000 to 1e3 would make encoding/json reject int.
func TestSourceFreezeReceivesExactTypedIntegers(t *testing.T) {
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	principal := sourceTestPrincipal()
	ctx := sourceTestContext(principal)
	for _, limit := range []int{100, 1000} {
		svc := ownerPolicyService(now, func(context.Context, port.SourceJobAccess) error { return nil },
			func(context.Context, port.SourceBuildQuery) (port.SourceData, error) { return port.SourceData{}, nil })
		policy, _ := svc.Builders.jobPolicy("example.measure")
		policy.Freeze = func(_ context.Context, q port.SourceFreezeRequest) (json.RawMessage, error) {
			var args struct {
				Limit int `json:"limit"`
			}
			d := json.NewDecoder(strings.NewReader(string(q.SourceArgs)))
			d.DisallowUnknownFields()
			if err := d.Decode(&args); err != nil {
				t.Fatalf("typed int changed by shared normalization: %s: %v", q.SourceArgs, err)
			}
			if args.Limit != limit {
				t.Fatalf("integer lost precision: %d", args.Limit)
			}
			return json.Marshal(args)
		}
		svc.Builders.policies["example.measure"] = policy
		template, _ := svc.builtinTemplate("example.layout")
		raw, _ := json.Marshal(map[string]int{"limit": limit})
		if _, err := svc.sourceRequestJob(ctx, principal, template, ExportInput{TemplateKey: template.Key, Format: "xlsx", SourceArgs: raw}, nil); err != nil {
			t.Fatal(err)
		}
	}
}
