package app_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
)

// #276 Q9: genuine encrypted revisions, current Resolve roles/MFA/quota, RLS on a reused pool1,
// KMS outside transactions, and required audit commit before any plaintext is returned.
func TestResolveRevisionNames_276(t *testing.T) {
	s, gate := singleConnService(t)
	tid := ids.New()
	ctx := as(tid, true, allRoles...)
	c, err := s.Create(ctx, client.PurposeContact, client.Identity{Kind: client.SubjectHousehold, FullName: "Hộ kiểm thử An", Phone: "0990000123", AreaCode: "30001"})
	if err != nil {
		t.Fatal(err)
	}
	r1, err := s.AmendRevision(ctx, client.PurposeContact, c.Ref, client.Identity{FullName: "Hộ kiểm thử Bình", Phone: "0991000456"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AmendRevision(ctx, client.PurposeContact, c.Ref, client.Identity{FullName: "Hộ kiểm thử Cường", Phone: "0992000789"}); err != nil {
		t.Fatal(err)
	}
	refs := []uuid.UUID{r1, ids.New()}
	check := func(got []client.RevisionNames, err error) {
		t.Helper()
		if err != nil || len(got) != 1 || got[0].Ref != r1 || got[0].SubjectRef != c.Ref || got[0].PrevName != "Hộ kiểm thử An" || got[0].NextName != "Hộ kiểm thử Bình" {
			t.Fatalf("exact historical revision unavailable or changed: count=%d err=%v", len(got), err)
		}
	}
	check(s.ResolveRevisions(ctx, client.PurposeFieldSupport, refs))
	// Existing masked contract is unchanged, including its weaker Display role.
	masked, err := s.Revisions(as(tid, false, client.RoleDisplay), client.PurposeContact, []uuid.UUID{r1})
	if err != nil || len(masked) != 1 || masked[0].PrevLabel != "Hộ K. T. A." || masked[0].PrevPhone != "0990•••123" || masked[0].NextPhone != "0991•••456" {
		t.Fatal("masked contract changed", err)
	}
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		purpose client.Purpose
		refs    []uuid.UUID
		want    string
	}{
		{"missing purpose", ctx, "", refs, "vault.purpose_required"},
		{"wrong purpose", ctx, client.PurposeContact, refs, "validation_failed"},
		{"display only", as(tid, true, client.RoleDisplay), client.PurposeFieldSupport, refs, "forbidden"},
		{"evidence only", as(tid, true, client.RoleEvidenceViewer), client.PurposeFieldSupport, refs, "forbidden"},
		{"too many", ctx, client.PurposeFieldSupport, make([]uuid.UUID, 201), "validation_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ResolveRevisions(tc.ctx, tc.purpose, tc.refs)
			if len(got) != 0 || code(err) != tc.want {
				t.Fatalf("denial: count=%d code=%s", len(got), code(err))
			}
		})
	}
	s.RequireMFASensitive = true
	if got, err := s.ResolveRevisions(as(tid, false, client.RoleRegistrar), client.PurposeFieldSupport, refs); len(got) != 0 || code(err) != "mfa_required" {
		t.Fatal("MFA did not deny")
	}
	// No tenant settings survive reuse of the only connection.
	if got, err := s.ResolveRevisions(as(ids.New(), true, client.RoleRegistrar), client.PurposeFieldSupport, refs); err != nil || len(got) != 0 {
		t.Fatal("cross tenant revision", err)
	}
	check(s.ResolveRevisions(ctx, client.PurposeFieldSupport, refs))
	fixed := clock.NewFixed(s.Clock.Now())
	s.Quota = app.NewQuota(fixed, nil, 1, 1)
	if got, err := s.ResolveRevisions(ctx, client.PurposeFieldSupport, refs); len(got) != 0 || code(err) != client.CodeQuotaExceeded {
		t.Fatal("batch bypassed Resolve quota")
	}
	s.Quota = nil

	gate.armed.Store(true)
	type result struct {
		names []client.RevisionNames
		err   error
	}
	done := make(chan result, 1)
	go func() {
		names, err := s.ResolveRevisions(ctx, client.PurposeFieldSupport, []uuid.UUID{r1})
		done <- result{names, err}
	}()
	poolFree(t, s, gate, tid)
	r := <-done
	check(r.names, r.err)
	gate.fail.Store(true)
	if got, err := s.ResolveRevisions(ctx, client.PurposeFieldSupport, refs); err == nil || len(got) != 0 {
		t.Fatal("KMS failure returned names")
	}
	gate.fail.Store(false)
	if !slices.Contains(accessOps(t, s, tid, c.Ref), "resolve/ok") {
		t.Fatal("missing required Resolve audit")
	}

	// Real PostgreSQL audit INSERT fails after successful decryption; no names may escape.
	env := pgtest.Get(t)
	admin := env.AdminPool(t, migrations.Vault)
	fn := `CREATE FUNCTION vault.reject_hcn09_audit276() RETURNS trigger LANGUAGE plpgsql AS $f$ BEGIN
      IF NEW.tenant_id = '%s'::uuid AND NEW.op = 'resolve' THEN RAISE EXCEPTION 'fixture required audit unavailable'; END IF;
      RETURN NEW; END $f$`
	if _, err := admin.Exec(context.Background(), fmt.Sprintf(fn, tid)); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(context.Background(), `CREATE TRIGGER reject_hcn09_audit276 BEFORE INSERT ON vault.access_log FOR EACH ROW EXECUTE FUNCTION vault.reject_hcn09_audit276()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP TRIGGER IF EXISTS reject_hcn09_audit276 ON vault.access_log; DROP FUNCTION IF EXISTS vault.reject_hcn09_audit276()`)
	})
	if got, err := s.ResolveRevisions(ctx, client.PurposeFieldSupport, []uuid.UUID{r1}); err == nil || len(got) != 0 {
		t.Fatal("required audit failure returned plaintext names")
	}
	if _, err := admin.Exec(context.Background(), `DROP TRIGGER reject_hcn09_audit276 ON vault.access_log; DROP FUNCTION vault.reject_hcn09_audit276()`); err != nil {
		t.Fatal(err)
	}
	check(s.ResolveRevisions(ctx, client.PurposeFieldSupport, refs))
	if err := s.Erase(ctx, c.Ref, "Synthetic erasure fixture"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ResolveRevisions(ctx, client.PurposeFieldSupport, refs); err != nil || len(got) != 0 {
		t.Fatal("erased revisions returned names", err)
	}
}
