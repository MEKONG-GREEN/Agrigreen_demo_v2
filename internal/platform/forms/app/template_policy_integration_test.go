package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func TestTemplateDefinitionPolicyVersionsAndDurableRetry(t *testing.T) {
	f := newFx(t)
	owner := ids.New()
	ctx := templateScopeCtx(f, owner, "tinh", ids.New(), authz.Scope{All: true}, func(string) bool { return false }, nil)
	policyCalls := 0
	f.svc.DefinitionPolicies = map[string]port.TemplateDefinitionPolicy{"fx": func(ctx context.Context, r port.TemplateDefinitionRequest) (port.TemplateDefinitionDecision, error) {
		if _, joined := db.JoinedTx(ctx); joined {
			t.Fatal("owner callback called in forms transaction")
		}
		policyCalls++
		return port.TemplateDefinitionDecision{Allowed: r.Actor.Role == "tinh" && r.Principal.ActorRef == owner && r.Proposed.Code != "LOCKED-191"}, nil
	}}
	in := TemplateInput{Profile: "fx", Code: "CUSTOM-191", Title: "Original", Fields: domain.Fixtures()[0].Fields}
	base := int64(0)
	first, err := f.svc.defineTemplateVersion(ctx, in, &base, "key-first")
	f.must(err, "policy can define independently of read filter and DefineBy")
	if first.Version != 1 {
		t.Fatalf("first version %d", first.Version)
	}
	in.Title = "Next"
	base = 1
	second, err := f.svc.defineTemplateVersion(ctx, in, &base, "key-second")
	f.must(err, "next version")
	if second.Version != 2 || second.ID == first.ID {
		t.Fatal("immutable version not appended")
	}
	in.Title = "Original"
	base = 0
	retry, err := f.svc.defineTemplateVersion(ctx, in, &base, "key-first")
	f.must(err, "durable retry after latest advanced")
	if retry.ID != first.ID || retry.Version != 1 {
		t.Fatal("retry selected current instead of original version")
	}
	in.Title = "Different payload"
	_, err = f.svc.defineTemplateVersion(ctx, in, &base, "key-first")
	f.want(err, "idempotency_key_reused", "key payload binding")
	_, err = f.svc.defineTemplateVersion(ctx, in, &base, "stale-new-key")
	f.want(err, "conflict", "stale base")
	in.Code = "LOCKED-191"
	_, err = f.svc.defineTemplateVersion(ctx, in, &base, "locked")
	f.want(err, "forbidden", "owner structural lock")
	f.svc.DefinitionPolicies["fx"] = func(context.Context, port.TemplateDefinitionRequest) (port.TemplateDefinitionDecision, error) {
		return port.TemplateDefinitionDecision{}, nil
	}
	in.Code = "CUSTOM-191"
	in.Title = "Original"
	_, err = f.svc.defineTemplateVersion(ctx, in, &base, "key-first")
	f.want(err, "forbidden", "retry rechecks current permission")
	if policyCalls < 6 {
		t.Fatalf("policy calls %d", policyCalls)
	}
}

func TestTemplateDefinitionRequiredPolicyAndMissingBaseFailClosed(t *testing.T) {
	f := newFx(t)
	ctx := f.as(f.tid, ids.New(), "admin", ids.New())
	in := TemplateInput{Profile: "fx", Code: "REQUIRED-191", Title: "Required", Fields: domain.Fixtures()[0].Fields}
	f.svc.RequiredDefinitionPolicies = map[string]bool{"fx": true}
	base := int64(0)
	_, err := f.svc.defineTemplateVersion(ctx, in, &base, "missing")
	f.want(err, "forbidden", "required policy missing does not fallback admin")
	sentinel := errors.New("owner unavailable")
	f.svc.DefinitionPolicies = map[string]port.TemplateDefinitionPolicy{"fx": func(context.Context, port.TemplateDefinitionRequest) (port.TemplateDefinitionDecision, error) {
		return port.TemplateDefinitionDecision{Allowed: true}, sentinel
	}}
	_, err = f.svc.defineTemplateVersion(ctx, in, &base, "error")
	if !errors.Is(err, sentinel) {
		t.Fatalf("policy error fallback: %v", err)
	}
	f.svc.DefinitionPolicies["fx"] = func(context.Context, port.TemplateDefinitionRequest) (port.TemplateDefinitionDecision, error) {
		return port.TemplateDefinitionDecision{Allowed: true}, nil
	}
	_, err = f.svc.defineTemplateVersion(ctx, in, nil, "no-base")
	f.want(err, "validation_failed", "required base")
	if n := f.scalar(`SELECT count(*) FROM pf_forms.template WHERE tenant_id=$1`, f.tid); n != 0 {
		t.Fatalf("denials wrote %d rows", n)
	}
}

func TestTemplateDefinitionConcurrentBaseHasOneWinner(t *testing.T) {
	f := newFx(t)
	ctx := f.as(f.tid, ids.New(), "admin", ids.New())
	f.svc.DefinitionPolicies = map[string]port.TemplateDefinitionPolicy{"fx": func(context.Context, port.TemplateDefinitionRequest) (port.TemplateDefinitionDecision, error) {
		return port.TemplateDefinitionDecision{Allowed: true}, nil
	}}
	base := int64(0)
	in := TemplateInput{Profile: "fx", Code: "RACE-191", Title: "Original", Fields: domain.Fixtures()[0].Fields}
	_, err := f.svc.defineTemplateVersion(ctx, in, &base, "seed")
	f.must(err, "seed")
	base = 1
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, title := range []string{"Writer A", "Writer B"} {
		wg.Add(1)
		go func(title string) {
			defer wg.Done()
			<-start
			candidate := in
			candidate.Title = title
			_, e := f.svc.defineTemplateVersion(ctx, candidate, &base, "race-"+title)
			results <- e
		}(title)
	}
	close(start)
	wg.Wait()
	close(results)
	winners, conflicts := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else if isProblem(err, "conflict") {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
	if n := f.scalar(`SELECT count(*) FROM pf_forms.template WHERE tenant_id=$1 AND code='RACE-191'`, f.tid); n != 2 {
		t.Fatalf("lost update or duplicate version: %d", n)
	}
}

func TestTemplateDefinitionKeepsPublishedSchemaAndSubmissions(t *testing.T) {
	f := newFx(t)
	node := ids.New()
	pub := f.as(f.tid, ids.New(), "tinh", node, "U1")
	xa := f.as(f.tid, ids.New(), "xa", ids.New(), "U1")
	f.svc.DefinitionPolicies = map[string]port.TemplateDefinitionPolicy{"fx": func(context.Context, port.TemplateDefinitionRequest) (port.TemplateDefinitionDecision, error) {
		return port.TemplateDefinitionDecision{Allowed: true}, nil
	}}
	in := TemplateInput{Profile: "fx", Code: "PINNED-191", Title: "Pinned", Fields: []domain.Field{{Key: "original", Label: "Original", Type: domain.TypeText, Required: true}}}
	base := int64(0)
	first, err := f.svc.defineTemplateVersion(pub, in, &base, "pin-first")
	f.must(err, "original schema")
	oldPeriod, err := f.svc.Publish(pub, PublishInput{Profile: "fx", TemplateID: first.ID, Label: "Old period", DueAt: f.clk.Now().Add(time.Hour), Units: []string{"U1"}}, "old-period")
	f.must(err, "publish old")
	original, err := f.svc.Submit(xa, "fx", oldPeriod.Period.ID, "U1", map[string]string{"original": "saved before edit"}, "old-submission")
	f.must(err, "submit before version")
	in.Fields = append(in.Fields, domain.Field{Key: "new_required", Label: "New required", Type: domain.TypeInteger, Required: true})
	base = 1
	second, err := f.svc.defineTemplateVersion(pub, in, &base, "pin-second")
	f.must(err, "new schema")
	oldAgain, err := f.svc.Period(pub, "fx", oldPeriod.Period.ID)
	f.must(err, "old period after definition")
	if oldAgain.Period.TemplateID != first.ID || oldAgain.Period.TemplateVersion != 1 {
		t.Fatal("old period rebound to current template")
	}
	oldTemplate, err := f.svc.Template(pub, "fx", first.ID)
	f.must(err, "old template after definition")
	if !sameTemplateDefinition(first, oldTemplate) || oldTemplate.Version != first.Version {
		t.Fatal("old schema mutated")
	}
	submissions, err := f.svc.Submissions(pub, "fx", oldPeriod.Period.ID, "U1")
	f.must(err, "old submission history")
	if len(submissions) != 1 || submissions[0].Submission.ID != original.Submission.ID || submissions[0].Submission.Values["original"] != "saved before edit" {
		t.Fatal("old submission changed")
	}
	_, err = f.svc.Submit(xa, "fx", oldPeriod.Period.ID, "U1", map[string]string{"original": "still old schema"}, "old-after-edit")
	f.must(err, "old period still validates original schema")
	newPeriod, err := f.svc.Publish(pub, PublishInput{Profile: "fx", TemplateID: second.ID, Label: "New period", DueAt: f.clk.Now().Add(time.Hour), Units: []string{"U1"}}, "new-period")
	f.must(err, "publish selected new id")
	if newPeriod.Period.TemplateID != second.ID || newPeriod.Period.TemplateVersion != 2 {
		t.Fatal("new period did not pin selected version")
	}
	_, err = f.svc.Submit(xa, "fx", newPeriod.Period.ID, "U1", map[string]string{"original": "missing new field"}, "new-invalid")
	if err == nil {
		t.Fatal("new period accepted old schema")
	}
	_, err = f.svc.Submit(xa, "fx", newPeriod.Period.ID, "U1", map[string]string{"original": "new schema", "new_required": "7"}, "new-valid")
	f.must(err, "new selected schema")
}
