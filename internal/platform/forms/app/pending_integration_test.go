package app

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
)

// Baseline defect: Progress on one selected period cannot count native pending
// submissions beyond the page. This assertion requires the additive supplier;
// absence is an assertion failure after real native setup, not a build failure.
func TestPending293NativeCountBeyondPage(t *testing.T) {
	f := newFx(t)
	node := ids.New()
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	pub := f.as(f.tid, uuid.Nil, "tinh", node, "U1", "U2")
	leaf := f.as(f.tid, uuid.Nil, "xa", ids.New(), "U1")
	templates, err := f.svc.InstallFixtures(admin, "fx", "")
	f.must(err, "templates")
	idsExpected := map[uuid.UUID]bool{}
	for range 3 {
		pv, err := f.svc.Publish(pub, PublishInput{Profile: "fx", TemplateID: templates[0].ID, Label: "Native pending", DueAt: f.clk.Now(), Units: []string{"U1", "U2"}}, "")
		f.must(err, "publish")
		sb, err := f.svc.Submit(leaf, "fx", pv.Period.ID, "U1", fx01, "")
		f.must(err, "native submit")
		idsExpected[sb.Submission.ID] = true
	}
	reader, ok := any(Control{Svc: f.svc}).(port.PendingReader)
	if !ok {
		t.Fatal("native Forms has no caller-authorized batch/count supplier: one Progress/page cannot establish the whole pending set")
	}
	installPending293(f)
	principal, _ := authz.FromContext(pub)
	principal.MFA = true
	pub = authz.WithPrincipal(pub, principal)
	q := port.PendingQuery{Profile: "fx", UnitKind: port.UnitCommune, Limit: 1}
	seen := map[uuid.UUID]bool{}
	for {
		page, err := reader.PendingSubmissions(pub, q)
		f.must(err, "native page")
		if page.Count != 3 || !page.CoverageComplete || len(page.Items) != 1 {
			t.Fatalf("whole pending count must stay 3 beyond each page: %+v", page)
		}
		id := page.Items[0].SubmissionID
		if !idsExpected[id] || seen[id] {
			t.Fatalf("foreign or duplicate latest identity %s", id)
		}
		seen[id] = true
		if page.NextCursor == "" {
			break
		}
		q.Cursor = page.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("coverage: %d", len(seen))
	}
}

// Installed through a fixture-owned adapter, never fabricated KN authority.
func installPending293(f *fx) {
	f.svc.PendingPolicies = map[string]port.PendingPolicy{"fx": func(context.Context, authz.Principal, port.PendingQuery) (port.PendingGrant, error) {
		return port.PendingGrant{Allowed: true, Complete: true, Binding: "fixture-current-1", Scope: port.PendingScope{AllPeriods: true, AllUnits: true, AllTemplates: true, AllOwners: true}}, nil
	}}
}
