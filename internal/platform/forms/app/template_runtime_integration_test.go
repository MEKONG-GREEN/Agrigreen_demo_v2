package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This owner is a contract fixture, not evidence of production account authority.
// Native account/session and real owner policy coverage lives in tests/kn77.
func runtimeFixture303(t *testing.T) (*fx, context.Context, TemplateInput) {
	t.Helper()
	f := newFx(t)
	ctx := f.as(f.tid, ids.New(), "admin", ids.New())
	f.svc.DefinitionPolicies = map[string]port.TemplateDefinitionPolicy{"fx": func(ctx context.Context, r port.TemplateDefinitionRequest) (port.TemplateDefinitionDecision, error) {
		if _, joined := db.JoinedTx(ctx); joined {
			t.Fatal("definition owner I/O in transaction")
		}
		return port.TemplateDefinitionDecision{Allowed: true, RuntimeValidated: r.Proposed.Runtime != nil && r.Proposed.Runtime.Contract == "sample.runtime/v1", Binding: "fixture-current-definition-authority"}, nil
	}}
	f.svc.TemplateUsePolicies = map[string]port.TemplateUsePolicy{"fx": func(ctx context.Context, r port.TemplateUsePolicyRequest) (port.TemplateUseDecision, error) {
		if _, joined := db.JoinedTx(ctx); joined {
			t.Fatal("runtime owner I/O in transaction")
		}
		return port.TemplateUseDecision{Allowed: r.Request.Reference != uuid.Nil, Binding: "fixture-verified-runtime-resource", HasData: r.Request.Kind == port.TemplateUseData}, nil
	}}
	base := int64(0)
	in := TemplateInput{Profile: "fx", Code: "RUNTIME-303", Title: "Runtime original", Fields: []domain.Field{{Key: "count", Label: "Count", Type: domain.TypeInteger}, {Key: "kind", Label: "Kind", Type: domain.TypeChoice, Options: []string{"one", "two"}}}, BaseVersion: &base, Runtime: &port.RuntimeDefinition{Schema: port.RuntimeDefinitionV1, Contract: "sample.runtime/v1", Config: json.RawMessage(`{"frequency":"daily","gps":true,"photo":true,"program":"verified-by-owner","threshold":"12.50"}`)}}
	return f, ctx, in
}
func TestRuntime303RoundTripCopyCASAndDurableIntent(t *testing.T) {
	f, ctx, in := runtimeFixture303(t)
	pb := Publisher{Svc: f.svc}
	first, err := f.svc.DefineTemplate(ctx, in, "runtime303-original")
	f.must(err, "original runtime")
	if first.Digest == "" || first.RuntimeDigest == "" || first.SchemaDigest == "" || first.TenantID != f.tid || first.Runtime == nil || !first.Tracked {
		t.Fatalf("missing immutable identity: %+v", first)
	}
	original := first.Identity()
	base := int64(1)
	next := in
	next.BaseVersion = &base
	next.Title = "Next"
	next.Runtime = &port.RuntimeDefinition{Schema: port.RuntimeDefinitionV1, Contract: "sample.runtime/v1", Config: json.RawMessage(`{"frequency":"weekly","gps":false,"photo":true,"threshold":"22.75"}`)}
	second, err := f.svc.DefineTemplate(ctx, next, "runtime303-next")
	f.must(err, "next runtime")
	if second.Digest == first.Digest || second.RuntimeDigest == first.RuntimeDigest || second.SchemaDigest != first.SchemaDigest {
		t.Fatal("runtime/schema digests do not describe immutable content")
	}
	replay, err := f.svc.DefineTemplate(ctx, in, "runtime303-original")
	f.must(err, "replay after head advance")
	if replay.Identity() != original {
		t.Fatal("replay returned current head")
	}
	changed := in
	zero := int64(0)
	one := int64(1)
	changed.BaseVersion = &one
	_, err = f.svc.DefineTemplate(ctx, changed, "runtime303-original")
	f.want(err, "idempotency_key_reused", "changed base intent")
	changed = in
	changed.Runtime = next.Runtime
	_, err = f.svc.DefineTemplate(ctx, changed, "runtime303-original")
	f.want(err, "idempotency_key_reused", "changed config intent")
	copyInput := port.TemplateCopy{Source: original, Code: "COPY-303", Title: "Exact copy", BaseVersion: 0}
	copied, err := pb.CopyTemplate(ctx, copyInput, "runtime303-copy")
	f.must(err, "copy exact old source")
	if copied.Runtime == nil || copied.Identity.RuntimeDigest != original.RuntimeDigest || copied.Identity.SchemaDigest != original.SchemaDigest || copied.Identity.Digest == original.Digest {
		t.Fatal("copy dropped metadata or reused source identity")
	}
	copyNext := copyInput
	copyNext.Title = "Advanced copy"
	copyNext.BaseVersion = 1
	_, err = pb.CopyTemplate(ctx, copyNext, "runtime303-copy-next")
	f.must(err, "advance copied head")
	retry, err := pb.CopyTemplate(ctx, copyInput, "runtime303-copy")
	f.must(err, "copy retry after both heads advanced")
	if retry.Identity != copied.Identity {
		t.Fatal("copy replay did not retain original result")
	}
	crossCode := in
	crossCode.Code = "DIFFERENT-303"
	_, err = f.svc.DefineTemplate(ctx, crossCode, "runtime303-original")
	f.want(err, "idempotency_key_reused", "typed runtime key cannot change code")
	copyChanged := copyInput
	copyChanged.Code = "OTHER-COPY-303"
	_, err = pb.CopyTemplate(ctx, copyChanged, "runtime303-copy")
	f.want(err, "idempotency_key_reused", "copy key cannot change destination")
	copyInput.Source = second.Identity()
	_, err = pb.CopyTemplate(ctx, copyInput, "runtime303-copy")
	f.want(err, "idempotency_key_reused", "copy source intent conflict")
	copyInput.Source = original
	copyInput.Source.Digest = strings.Repeat("0", 64)
	_, err = pb.CopyTemplate(ctx, copyInput, "runtime303-copy-bad")
	f.want(err, "conflict", "source digest verified")
	foreign := f.as(ids.New(), ids.New(), "admin", ids.New())
	_, err = pb.CopyTemplate(foreign, port.TemplateCopy{Source: original, Code: "FOREIGN-303", Title: "No", BaseVersion: 0}, "runtime303-foreign")
	f.want(err, port.ErrTemplateNotFound.Code, "foreign source scope")
	other := f.as(f.tid, ids.New(), "admin", ids.New())
	_, err = f.svc.DefineTemplate(other, in, "runtime303-original")
	f.want(err, "idempotency_key_reused", "actor binding")
	entries, err := pb.ListTemplates(ctx, "fx")
	f.must(err, "catalog runtime")
	if len(entries) != 2 {
		t.Fatalf("catalog membership %d", len(entries))
	}
	history, err := pb.TemplateHistory(ctx, "fx", in.Code, 0, 2)
	f.must(err, "runtime history")
	if len(history) != 2 || history[1].Identity != original || history[1].Runtime == nil {
		t.Fatal("history lost old config")
	}
	// An absent base is a different legacy intent even when its actual base was 0.
	f.svc.DefinitionPolicies = nil
	legacy := TemplateInput{Profile: "fx", Code: "LEGACY-INTENT-303", Title: "Legacy", Fields: in.Fields}
	saved, err := f.svc.DefineTemplate(ctx, legacy, "runtime303-legacy")
	f.must(err, "legacy without base")
	legacy.BaseVersion = &zero
	_, err = f.svc.DefineTemplate(ctx, legacy, "runtime303-legacy")
	f.want(err, "idempotency_key_reused", "actual receipt distinguishes absent base")
	if saved.Version != 1 {
		t.Fatal("legacy version")
	}
}
func TestRuntime303UsageDistinguishesRuntimeDataAndUnknownLegacy(t *testing.T) {
	f, ctx, in := runtimeFixture303(t)
	pb := Publisher{Svc: f.svc}
	first, err := f.svc.DefineTemplate(ctx, in, "usage303-seed")
	f.must(err, "seed")
	usage, err := pb.TemplateUsage(ctx, "fx", first.ID)
	f.must(err, "unused evidence")
	if usage.State != port.TemplateUnused || usage.HasData == nil || *usage.HasData {
		t.Fatal("new coverage is not known unused")
	}
	request := port.TemplateUseRequest{Identity: first.Identity(), Runtime: "sample.runtime/v1", Reference: ids.New(), Kind: port.TemplateUseRuntime}
	receipt, err := pb.RecordTemplateUse(ctx, request, "usage303-runtime")
	f.must(err, "owner verified runtime use")
	if receipt.HasData {
		t.Fatal("runtime instantiation asserted data")
	}
	usage, err = pb.TemplateUsage(ctx, "fx", first.ID)
	f.must(err, "runtime used evidence")
	if usage.State != port.TemplateUsed || !usage.Runtime || usage.HasData == nil || *usage.HasData || usage.FirstUsedAt == nil {
		t.Fatal("runtime and data conflated")
	}
	// No data yet: a structural successor is legal, and the old version remains usable.
	next := in
	base := int64(1)
	next.BaseVersion = &base
	next.Fields = append([]domain.Field(nil), in.Fields[:1]...)
	newer, err := f.svc.DefineTemplate(ctx, next, "usage303-before-data-edit")
	f.must(err, "runtime-only does not imply data")
	request.Kind = port.TemplateUseData
	dataReceipt, err := pb.RecordTemplateUse(ctx, request, "usage303-first-data")
	f.must(err, "first data on pinned old version")
	if !dataReceipt.HasData {
		t.Fatal("owner data evidence lost")
	}
	replay, err := pb.RecordTemplateUse(ctx, request, "usage303-first-data")
	f.must(err, "usage replay")
	if replay.ID != dataReceipt.ID {
		t.Fatal("usage receipt changed")
	}
	request.Reference = ids.New()
	_, err = pb.RecordTemplateUse(ctx, request, "usage303-first-data")
	f.want(err, "idempotency_key_reused", "usage payload binding")
	usage, err = pb.TemplateUsage(ctx, "fx", first.ID)
	f.must(err, "data evidence")
	if usage.HasData == nil || !*usage.HasData || usage.FirstDataAt == nil {
		t.Fatal("actual owner data missing")
	}
	// After data exists anywhere in the stream, edits can only add to their real base.
	base = 2
	next.BaseVersion = &base
	next.Fields = []domain.Field{{Key: "replacement", Label: "Replacement", Type: domain.TypeText}}
	_, err = f.svc.DefineTemplate(ctx, next, "usage303-after-data-delete")
	f.want(err, "conflict", "remove current column after data")
	next.Fields = append(append([]domain.Field(nil), newer.Fields...), domain.Field{Key: "added", Label: "Added", Type: domain.TypeText})
	_, err = f.svc.DefineTemplate(ctx, next, "usage303-after-data-add")
	f.must(err, "add to actual base")
	f.svc.TemplateUsePolicies["fx"] = func(context.Context, port.TemplateUsePolicyRequest) (port.TemplateUseDecision, error) {
		return port.TemplateUseDecision{}, nil
	}
	request.Reference = dataReceipt.Request.Reference
	_, err = pb.RecordTemplateUse(ctx, request, "usage303-first-data")
	f.want(err, "forbidden", "revoked before durable use replay")
	// Represents a pre-contract row. Migration intentionally never backfills a proof.
	legacy := store.Template{ID: ids.New(), Profile: "fx", Code: "UNKNOWN-303", Title: "Unindexed historical definition", Fields: in.Fields, ActorRef: first.ActorRef, EventTime: f.clk.Now()}
	f.must(f.svc.tx(ctx, f.tid, func(tx pgx.Tx) error { return store.InsertTemplate(ctx, tx, f.tid, &legacy, "usage303-old-ledger") }), "legacy native fixture")
	unknown, err := pb.TemplateUsage(ctx, "fx", legacy.ID)
	f.must(err, "legacy usage")
	if unknown.State != port.TemplateUsageUnknown || unknown.HasData != nil {
		t.Fatal("missing legacy data evidence asserted unused")
	}
	foreign := f.as(ids.New(), ids.New(), "admin", ids.New())
	_, err = pb.TemplateUsage(foreign, "fx", first.ID)
	f.want(err, port.ErrTemplateNotFound.Code, "usage RLS")
}
func TestRuntime303NativePinnedPeriodAndFrozenContentSurviveEdit(t *testing.T) {
	f, ctx, in := runtimeFixture303(t)
	first, err := f.svc.DefineTemplate(ctx, in, "native303-seed")
	f.must(err, "seed")
	pub := f.as(f.tid, ids.New(), "tinh", ids.New(), "U1")
	xa := f.as(f.tid, ids.New(), "xa", ids.New(), "U1")
	old, err := f.svc.Publish(pub, PublishInput{Profile: "fx", TemplateID: first.ID, Label: "Pinned", DueAt: f.clk.Now().Add(time.Hour), Units: []string{"U1"}}, "native303-publish")
	f.must(err, "publish old")
	usage, err := f.svc.TemplateUsage(ctx, "fx", first.ID)
	f.must(err, "native runtime use")
	if !usage.Native || usage.HasData == nil || *usage.HasData {
		t.Fatal("period falsely counted as data")
	}
	next := in
	base := int64(1)
	next.BaseVersion = &base
	next.Fields = append([]domain.Field(nil), in.Fields[:1]...)
	second, err := f.svc.DefineTemplate(ctx, next, "native303-edit-before-data")
	f.must(err, "edit after period but before data")
	submitted, err := f.svc.Submit(xa, "fx", old.Period.ID, "U1", map[string]string{"count": "7", "kind": "two"}, "native303-old-submission")
	f.must(err, "old period still accepts its original schema")
	_, err = f.svc.Review(pub, "fx", submitted.Submission.ID, "accepted", "", "native303-review")
	f.must(err, "review original")
	closed, err := f.svc.Close(pub, "fx", old.Period.ID, "native303-close")
	f.must(err, "freeze old")
	snapshots, err := f.svc.Snapshots(pub, "fx", old.Period.ID)
	f.must(err, "original snapshots")
	if len(snapshots) != 1 {
		t.Fatal("missing frozen snapshot")
	}
	original, err := f.svc.Snapshot(pub, "fx", snapshots[0].ID)
	f.must(err, "original frozen content")
	next.Fields = append(append([]domain.Field(nil), second.Fields...), domain.Field{Key: "added", Label: "Added", Type: domain.TypeText})
	base = 2
	next.BaseVersion = &base
	_, err = f.svc.DefineTemplate(ctx, next, "native303-add-after-data")
	f.must(err, "add after data")
	after, err := f.svc.Snapshot(pub, "fx", snapshots[0].ID)
	f.must(err, "frozen content after new schema")
	if !bytes.Equal(original.Content, after.Content) || !bytes.Equal(original.Hash, after.Hash) || closed.Period.TemplateID != first.ID {
		t.Fatal("historical data/schema/hash changed")
	}
}

type hookBeginner303 struct {
	tenant.Beginner
	target, mode string
	hook         func()
}
type hookTx303 struct {
	pgx.Tx
	target, mode string
	hook         func()
	effects      bool
}

func (b hookBeginner303) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.Beginner.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &hookTx303{Tx: tx, target: b.target, mode: b.mode, hook: b.hook}, nil
}
func (tx *hookTx303) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(q, "INSERT INTO "+tx.target) {
		tx.effects = true
		if tx.mode == "audit" {
			return tx.Tx.Exec(ctx, "SELECT 1/0")
		}
	}
	return tx.Tx.Exec(ctx, q, args...)
}
func (tx *hookTx303) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if strings.Contains(q, "INSERT INTO "+tx.target) {
		tx.effects = true
	}
	return tx.Tx.QueryRow(ctx, q, args...)
}
func (tx *hookTx303) Commit(ctx context.Context) error {
	if tx.effects {
		if tx.hook != nil {
			tx.hook()
		}
		if tx.mode == "commit" {
			return context.DeadlineExceeded
		}
	}
	err := tx.Tx.Commit(ctx)
	if err == nil && tx.effects && tx.mode == "ack" {
		return context.DeadlineExceeded
	}
	return err
}
func TestRuntime303AtomicAuditCommitAndLostACK(t *testing.T) {
	for _, mode := range []string{"audit", "commit", "ack"} {
		t.Run(mode, func(t *testing.T) {
			f, ctx, in := runtimeFixture303(t)
			fault := *f.svc
			fault.DB = hookBeginner303{Beginner: f.svc.DB, target: "pf_forms.template_definition", mode: mode}
			failed, err := fault.DefineTemplate(ctx, in, "atomic303-"+mode)
			if err == nil || failed.ID != uuid.Nil {
				t.Fatal("failed commit returned a success identity")
			}
			rows := f.scalar(`SELECT count(*) FROM pf_forms.template WHERE tenant_id=$1`, f.tid)
			metadata := f.scalar(`SELECT count(*) FROM pf_forms.template_definition WHERE tenant_id=$1`, f.tid)
			if mode == "ack" {
				if rows != 1 || metadata != 1 {
					t.Fatal("committed receipt lost after transport fault")
				}
				replay, err := f.svc.DefineTemplate(ctx, in, "atomic303-"+mode)
				f.must(err, "replay after lost ACK")
				if replay.Version != 1 {
					t.Fatal("replay appended")
				}
			} else if rows != 0 || metadata != 0 {
				t.Fatalf("partial native/config/audit/receipt: %d/%d", rows, metadata)
			}
		})
	}
}
func TestRuntime303FirstDataAndEditLinearizeBothOrders(t *testing.T) {
	for _, firstOperation := range []string{"data", "edit"} {
		t.Run(firstOperation, func(t *testing.T) {
			f, ctx, in := runtimeFixture303(t)
			first, err := f.svc.DefineTemplate(ctx, in, "fence303-seed")
			f.must(err, "seed")
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			blocked := *f.svc
			target := "pf_forms.template_use"
			if firstOperation == "edit" {
				target = "pf_forms.template_definition"
			}
			blocked.DB = hookBeginner303{Beginner: f.svc.DB, target: target, hook: func() { once.Do(func() { close(entered); <-release }) }}
			base := int64(1)
			next := in
			next.BaseVersion = &base
			next.Fields = append([]domain.Field(nil), in.Fields[:1]...)
			request := port.TemplateUseRequest{Identity: first.Identity(), Runtime: "sample.runtime/v1", Reference: ids.New(), Kind: port.TemplateUseData}
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			if firstOperation == "data" {
				go func() {
					_, e := (Publisher{Svc: &blocked}).RecordTemplateUse(ctx, request, "fence303-data")
					firstDone <- e
				}()
			} else {
				go func() { _, e := blocked.DefineTemplate(ctx, next, "fence303-edit"); firstDone <- e }()
			}
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("first transaction did not reach protected commit")
			}
			if firstOperation == "data" {
				go func() { _, e := f.svc.DefineTemplate(ctx, next, "fence303-edit"); secondDone <- e }()
			} else {
				go func() {
					_, e := (Publisher{Svc: f.svc}).RecordTemplateUse(ctx, request, "fence303-data")
					secondDone <- e
				}()
			}
			close(release)
			f.must(<-firstDone, "first operation")
			if firstOperation == "data" {
				f.want(<-secondDone, "conflict", "data-before-edit enforces add-only")
			} else {
				f.must(<-secondDone, "edit-before-data preserves valid old version")
			}
			old, err := f.svc.Template(ctx, "fx", first.ID)
			f.must(err, "original identity")
			if old.Identity() != first.Identity() {
				t.Fatal("fence mutated historical definition")
			}
		})
	}
}
