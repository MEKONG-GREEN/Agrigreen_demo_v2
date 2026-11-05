package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func publishFixture224(t *testing.T) (*fx, PublicationReceipts, context.Context, port.PublishOperationRequest) {
	t.Helper()
	f := newFx(t)
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	templates, err := f.svc.InstallFixtures(admin, "fx", "")
	f.must(err, "actual native template")
	ctx := f.as(f.tid, ids.New(), "tinh", ids.New(), "U1", "U2")
	p, _ := authz.FromContext(ctx)
	p.MFA = true
	ctx = authz.WithPrincipal(ctx, p)
	f.svc.PublishOperationPolicies = map[string]port.PublishOperationPolicy{"fx": func(ctx context.Context, _ authz.Principal, _ port.PublishOperationRequest) (port.PublishOperationGrant, error) {
		if _, joined := db.JoinedTx(ctx); joined {
			return port.PublishOperationGrant{}, errors.New("owner policy inside native TX")
		}
		return port.PublishOperationGrant{Allowed: true, Binding: "current-publication-owner-v1"}, nil
	}}
	zero := 0
	req := port.PublishOperationRequest{OperationID: ids.New(), TenantID: f.tid, Input: port.BatchInput{Profile: "fx", TemplateID: templates[0].ID, UnitKind: port.UnitCommune, Units: []string{"U1", "U2"}, MaxSubmissions: &zero}}
	for i := 0; i < 2; i++ {
		from := time.Date(2026, 10, 5+7*i, 0, 0, 0, 0, time.UTC)
		req.Input.Periods = append(req.Input.Periods, port.PeriodDef{Label: fmt.Sprintf("Receipt kỳ %d", i+1), Kind: "week", Key: fmt.Sprintf("2026-W%d", 41+i), From: from, To: from.AddDate(0, 0, 6), DueAt: from.AddDate(0, 0, 8), Rule: "fixture/v1"})
	}
	return f, PublicationReceipts{f.svc}, ctx, req
}
func appliedPublish224(t *testing.T, got port.PublishOperationReceipt, err error, want int) {
	t.Helper()
	if err != nil || got.Outcome != port.PublishOperationApplied || got.RecordedAt.IsZero() || len(got.Items) != want {
		t.Fatalf("applied whole receipt outcome=%s items=%d err=%v", got.Outcome, len(got.Items), err)
	}
}
func unknownPublishTest224(t *testing.T, got port.PublishOperationReceipt) {
	t.Helper()
	if !reflect.DeepEqual(got, unknownPublish224()) {
		t.Fatal("unknown exposed operation or partial item metadata")
	}
}
func nativeCounts224(f *fx) []int {
	return []int{count212(f, "period"), count212(f, "period_spec"), count212(f, "period_event"), count212(f, "publish_operation_key"), count212(f, "publish_operation_receipt"), f.scalar("SELECT count(*) FROM shared.outbox WHERE tenant_id=$1", f.tid)}
}

// This is also the ONE controlled-fault oracle: refreshing a stored creation
// token from current heads would let compensation cancel a successor after ABA.
func TestPublishOperation224_CreationReceiptSurvivesSuccessorABA(t *testing.T) {
	f, receipts, ctx, req := publishFixture224(t)
	got, err := receipts.ExecutePublishOperation(ctx, req)
	appliedPublish224(t, got, err, 2)
	control := Control{f.svc}
	for _, item := range got.Items {
		var eventSeq int64
		var recordedAt time.Time
		err := f.env.AdminPool(t, migrations.PF).QueryRow(ctx, `SELECT seq,recorded_at FROM pf_forms.period_event WHERE tenant_id=$1 AND id=$2 AND period_ref=$3 AND action='published' AND origin='native'`, f.tid, item.EventID, item.ID).Scan(&eventSeq, &recordedAt)
		f.must(err, "own original native INSERT evidence")
		if eventSeq != item.Applied.EventSeq || !recordedAt.Equal(item.EventRecordedAt) || item.Applied.DueSeq != 0 {
			t.Fatal("creation evidence was inferred instead of original INSERT tuple")
		}
		fresh, e := control.ControlSnapshot(ctx, "fx", item.ID)
		f.must(e, "fresh current native control")
		if fresh.Revision != item.Applied {
			t.Fatal("native creation token differs from own initial heads")
		}
	}
	first := got.Items[0]
	b, err := control.ChangeDueCAS(ctx, "fx", first.ID, first.Applied, first.DueAt.Add(time.Hour), "successor B", ids.New().String())
	f.must(err, "successor B")
	afterABA, err := control.ChangeDueCAS(ctx, "fx", first.ID, b, first.DueAt, "successor A again", ids.New().String())
	f.must(err, "due ABA")
	if afterABA == first.Applied {
		t.Fatal("ABA did not advance native revision")
	}
	f.must(control.ClosePeriod(ctx, "fx", got.Items[1].ID, ids.New().String()), "independent sibling closed")
	before := nativeCounts224(f)
	read, err := receipts.ReadPublishOperation(ctx, req)
	f.must(err, "original receipt after mixed successors")
	replay, err := receipts.ExecutePublishOperation(ctx, req)
	f.must(err, "execute known anchored replay")
	if !reflect.DeepEqual(read, got) || !reflect.DeepEqual(replay, got) {
		t.Error("receipt was promoted to successor rather than original creation outcome")
	}
	if len(read.Items) != 2 {
		t.Fatal("partial receipt")
	}
	_, err = control.CancelPeriodCAS(ctx, "fx", first.ID, read.Items[0].Applied, "compensate only owned creation", ids.New().String())
	if !isProblem(err, port.ErrPeriodConflict.Code) {
		t.Errorf("creation token cancelled a successor: %v", err)
	}
	current, err := control.ControlSnapshot(ctx, "fx", first.ID)
	f.must(err, "successor remains")
	if current.Revision != afterABA || current.Progress.Status != "open" || !reflect.DeepEqual(nativeCounts224(f), before) {
		t.Fatal("replay or compensation altered successor/native/outbox/receipt")
	}
}

func TestPublishOperation224_UnknownCommitAndAtomicFailure(t *testing.T) {
	for _, committed := range []bool{true, false} {
		t.Run(fmt.Sprintf("real_commit_%t", committed), func(t *testing.T) {
			f, receipts, ctx, req := publishFixture224(t)
			copyService := *f.svc
			copyService.DB = ackBeginner212{Beginner: copyService.DB, commit: committed}
			unknown, err := (PublicationReceipts{&copyService}).ExecutePublishOperation(ctx, req)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("ACK boundary not reached: %v", err)
			}
			unknownPublishTest224(t, unknown)
			read, err := receipts.ReadPublishOperation(ctx, req)
			f.must(err, "read only after opaque unknown")
			if !committed {
				unknownPublishTest224(t, read)
				if !reflect.DeepEqual(nativeCounts224(f), []int{0, 0, 0, 0, 0, 0}) {
					t.Fatal("rollback unknown leaked native/anchor/receipt/outbox")
				}
				return
			}
			appliedPublish224(t, read, nil, 2)
			before := nativeCounts224(f)
			again, err := receipts.ExecutePublishOperation(ctx, req)
			f.must(err, "anchored replay after lost real COMMIT ACK")
			if !reflect.DeepEqual(again, read) || !reflect.DeepEqual(before, nativeCounts224(f)) {
				t.Fatal("lost ACK caused duplicate batch")
			}
		})
	}
	for _, mode := range []string{"late_native", "receipt_insert", "key_insert"} {
		t.Run(mode, func(t *testing.T) {
			f, receipts, ctx, req := publishFixture224(t)
			copyService := *f.svc
			probe := &publishFault224{Beginner: f.svc.DB, mode: mode}
			copyService.DB = probe
			got, err := (PublicationReceipts{&copyService}).ExecutePublishOperation(ctx, req)
			if err == nil || probe.hits.Load() == 0 {
				t.Fatal("late native/receipt boundary not reached")
			}
			unknownPublishTest224(t, got)
			if !reflect.DeepEqual(nativeCounts224(f), []int{0, 0, 0, 0, 0, 0}) {
				t.Fatal("partial atomic batch/receipt/key/outbox committed")
			}
			read, err := receipts.ReadPublishOperation(ctx, req)
			f.must(err, "read after proven fixture rollback")
			unknownPublishTest224(t, read)
		})
	}
}

func TestPublishOperation224_RetainedAnchorMissingOrBadItem(t *testing.T) {
	f, _, ctx, req := publishFixture224(t)
	healthy := PublicationReceipts{f.svc}
	got, err := healthy.ExecutePublishOperation(ctx, req)
	appliedPublish224(t, got, err, 2)
	before := nativeCounts224(f)
	for _, mode := range []string{"missing_receipt", "bad_last_item"} {
		t.Run(mode, func(t *testing.T) {
			copyService := *f.svc
			probe := &publishFault224{Beginner: f.svc.DB, mode: mode}
			copyService.DB = probe
			masked := PublicationReceipts{&copyService}
			read, err := masked.ReadPublishOperation(ctx, req)
			f.must(err, "opaque missing evidence read")
			unknownPublishTest224(t, read)
			replay, err := masked.ExecutePublishOperation(ctx, req)
			f.must(err, "retained permanent anchor stops re-execution")
			unknownPublishTest224(t, replay)
			if probe.hits.Load() != 2 || !reflect.DeepEqual(before, nativeCounts224(f)) {
				t.Fatal("masked receipt boundary missing or native batch executed again")
			}
		})
	}
	// QueryRow masks model unavailable/damaged receipt evidence, not actual retention GC.
}

func TestPublishOperation224_CurrentAuthorityAndFullCommand(t *testing.T) {
	f, receipts, ctx, req := publishFixture224(t)
	got, err := receipts.ExecutePublishOperation(ctx, req)
	appliedPublish224(t, got, err, 2)
	before := nativeCounts224(f)
	changed, err := port.NormalizePublishOperation(req)
	f.must(err, "owned body")
	changed.Input.Periods[1].Label = "different body"
	bad, err := receipts.ExecutePublishOperation(ctx, changed)
	f.must(err, "same op other body is opaque")
	unknownPublishTest224(t, bad)
	principal, _ := authz.FromContext(ctx)
	other := principal
	other.ActorRef = ids.New()
	bad, err = receipts.ReadPublishOperation(authz.WithPrincipal(ctx, other), req)
	f.must(err, "other actor opaque")
	unknownPublishTest224(t, bad)
	noMFA := principal
	noMFA.MFA = false
	bad, err = receipts.ExecutePublishOperation(authz.WithPrincipal(ctx, noMFA), req)
	f.want(err, "forbidden", "current MFA required before receipt")
	unknownPublishTest224(t, bad)
	foreign := principal
	foreign.TenantID = ids.New()
	bad, err = receipts.ReadPublishOperation(authz.WithPrincipal(ctx, foreign), req)
	f.want(err, "forbidden", "explicit tenant binding")
	unknownPublishTest224(t, bad)
	roleCtx := fixtureContext(ctx, principal, port.ResolvedActor{Role: "xa", Scope: authz.Scope{Units: []string{"U1", "U2"}}})
	bad, err = receipts.ReadPublishOperation(roleCtx, req)
	f.want(err, "forbidden", "current native publication level before receipt")
	unknownPublishTest224(t, bad)
	scopeCtx := fixtureContext(ctx, principal, port.ResolvedActor{Role: "tinh", Scope: authz.Scope{Units: []string{"U1"}}})
	bad, err = receipts.ReadPublishOperation(scopeCtx, req)
	f.want(err, "forbidden", "current target scope before receipt")
	unknownPublishTest224(t, bad)
	expires := f.clk.Now().Add(time.Second)
	f.svc.PublishOperationPolicies["fx"] = func(context.Context, authz.Principal, port.PublishOperationRequest) (port.PublishOperationGrant, error) {
		if !f.clk.Now().Before(expires) {
			return port.PublishOperationGrant{}, problem.Forbidden
		}
		return port.PublishOperationGrant{Allowed: true, Binding: "current-publication-owner-v1"}, nil
	}
	beforeExpiry, err := receipts.ReadPublishOperation(ctx, req)
	f.must(err, "current verified owner grant before expiry")
	if !reflect.DeepEqual(beforeExpiry, got) {
		t.Fatal("valid grant could not read original receipt")
	}
	f.clk.Advance(2 * time.Second)
	bad, err = receipts.ReadPublishOperation(ctx, req)
	f.want(err, "forbidden", "current owner grant expiry before receipt")
	unknownPublishTest224(t, bad)
	f.svc.PublishOperationPolicies["fx"] = func(context.Context, authz.Principal, port.PublishOperationRequest) (port.PublishOperationGrant, error) {
		return port.PublishOperationGrant{Allowed: true, Binding: "changed-assignment-v2"}, nil
	}
	bad, err = receipts.ExecutePublishOperation(ctx, req)
	f.must(err, "changed authority opaque")
	unknownPublishTest224(t, bad)
	f.svc.PublishOperationPolicies["fx"] = func(context.Context, authz.Principal, port.PublishOperationRequest) (port.PublishOperationGrant, error) {
		return port.PublishOperationGrant{}, problem.Forbidden
	}
	bad, err = receipts.ReadPublishOperation(ctx, req)
	f.want(err, "forbidden", "owner revocation/expiry before receipt")
	unknownPublishTest224(t, bad)
	f.svc.PublishOperationPolicies["fx"] = nil
	bad, err = receipts.ExecutePublishOperation(ctx, req)
	f.want(err, port.ErrPublishOperationUnavailable.Code, "nil current owner policy")
	unknownPublishTest224(t, bad)
	if !reflect.DeepEqual(before, nativeCounts224(f)) {
		t.Fatal("changed body/current authority wrote native data")
	}
}

func TestPublishOperation224_PoolOneConcurrentAndPolicyCopy(t *testing.T) {
	f, receipts, ctx, req := publishFixture224(t)
	f.svc.DB = db.ForModule(f.env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1), DBModule)
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	f.svc.PublishOperationPolicies["fx"] = func(ctx context.Context, _ authz.Principal, command port.PublishOperationRequest) (port.PublishOperationGrant, error) {
		if _, joined := db.JoinedTx(ctx); joined {
			return port.PublishOperationGrant{}, errors.New("owner callback inside native TX")
		}
		err := f.svc.tx(ctx, f.tid, func(tx pgx.Tx) error { var one int; return tx.QueryRow(ctx, "SELECT 1").Scan(&one) })
		if err != nil {
			return port.PublishOperationGrant{}, err
		}
		command.Input.Units[0] = "changed-by-policy"
		command.Input.Periods[0].Label = "changed-by-policy"
		*command.Input.MaxSubmissions = 3
		if calls.Add(1) <= 2 {
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return port.PublishOperationGrant{}, ctx.Err()
			}
		}
		return port.PublishOperationGrant{Allowed: true, Binding: "current-publication-owner-v1"}, nil
	}
	type result struct {
		receipt port.PublishOperationReceipt
		err     error
	}
	done := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() { r, e := receipts.ExecutePublishOperation(bounded, req); done <- result{r, e} }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-bounded.Done():
			close(release)
			t.Fatal("concurrent policy barrier not reached")
		}
	}
	close(release)
	first, second := <-done, <-done
	appliedPublish224(t, first.receipt, first.err, 2)
	appliedPublish224(t, second.receipt, second.err, 2)
	if !reflect.DeepEqual(first.receipt, second.receipt) || req.Input.Units[0] != "U1" || req.Input.Periods[0].Label != "Receipt kỳ 1" || *req.Input.MaxSubmissions != 0 || first.receipt.Items[0].Label != req.Input.Periods[0].Label {
		t.Fatal("policy mutated canonical command or concurrent batch duplicated")
	}
	if !reflect.DeepEqual(nativeCounts224(f), []int{2, 2, 2, 1, 1, 0}) {
		t.Fatal("same operation concurrent native/receipt/outbox count")
	}
	read, err := receipts.ReadPublishOperation(bounded, req)
	f.must(err, "same pool1 read/policy I/O")
	if !reflect.DeepEqual(read, first.receipt) {
		t.Fatal("pool1 read changed receipt")
	}
	var otherCount, ownCount int
	foreign := ids.New()
	err = db.WithModuleTenantTx(bounded, f.svc.DB, DBModule, foreign, func(tx pgx.Tx) error {
		return tx.QueryRow(bounded, `SELECT count(*) FROM pf_forms.publish_operation_key`).Scan(&otherCount)
	})
	f.must(err, "reused pool1 foreign tenant")
	err = db.WithModuleTenantTx(bounded, f.svc.DB, DBModule, f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(bounded, `SELECT count(*) FROM pf_forms.publish_operation_key`).Scan(&ownCount)
	})
	f.must(err, "reused pool1 original tenant")
	if otherCount != 0 || ownCount != 1 {
		t.Fatal("RLS/context leaked on reused connection")
	}
}

func TestPublishOperation224_SQLProofAndImmutableHeader(t *testing.T) {
	f, receipts, ctx, req := publishFixture224(t)
	got, err := receipts.ExecutePublishOperation(ctx, req)
	appliedPublish224(t, got, err, 2)
	forgedReq, err := port.NormalizePublishOperation(req)
	f.must(err, "copy command")
	forgedReq.OperationID = ids.New()
	forged := got
	forged.OperationID = forgedReq.OperationID
	forged.RequestDigest, err = port.PublishOperationDigest(forgedReq)
	f.must(err, "forged operation digest")
	err = f.svc.tx(ctx, f.tid, func(tx pgx.Tx) error {
		return store.InsertPublishOperation224(ctx, tx, forgedReq, &forged, f.clk.Now())
	})
	code, constraint := pgCode(err)
	if code != "23514" || constraint != "publish_operation_native_proof224" {
		t.Fatalf("foreign operation native evidence accepted: %s/%s", code, constraint)
	}
	_, err = f.env.AdminPool(t, migrations.PF).Exec(ctx, `UPDATE pf_forms.publish_operation_key SET authority_hash=repeat('0',64) WHERE tenant_id=$1 AND operation_id=$2`, f.tid, req.OperationID)
	code, constraint = pgCode(err)
	if code != "23514" || constraint != "publish_operation_key_immutable224" {
		t.Fatalf("permanent key mutable: %s/%s", code, constraint)
	}
	_, err = f.env.AdminPool(t, migrations.PF).Exec(ctx, `DELETE FROM pf_forms.publish_operation_key WHERE tenant_id=$1 AND operation_id=$2`, f.tid, req.OperationID)
	code, constraint = pgCode(err)
	if code != "23514" || constraint != "publish_operation_key_immutable224" {
		t.Fatalf("permanent key deletable: %s/%s", code, constraint)
	}
	_, err = f.env.AdminPool(t, migrations.PF).Exec(ctx, `UPDATE pf_forms.publish_operation_receipt SET content='{}'::jsonb WHERE tenant_id=$1 AND operation_id=$2`, f.tid, req.OperationID)
	if err == nil {
		t.Fatal("receipt ledger update accepted")
	}
	if !reflect.DeepEqual(nativeCounts224(f), []int{2, 2, 2, 1, 1, 0}) {
		t.Fatal("SQL negative appended or altered evidence")
	}
}

type publishFault224 struct {
	tenant.Beginner
	mode         string
	hits         atomic.Int32
	nativeEvents atomic.Int32
}
type publishFaultTx224 struct {
	pgx.Tx
	probe *publishFault224
}

func (b *publishFault224) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.Beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return publishFaultTx224{tx, b}, nil
}
func (tx publishFaultTx224) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	p := tx.probe
	if strings.Contains(q, "SELECT r.content,r.recorded_at FROM pf_forms.publish_operation_receipt") {
		if p.mode == "missing_receipt" {
			p.hits.Add(1)
			return missingRow212{}
		}
		if p.mode == "bad_last_item" {
			p.hits.Add(1)
			return corruptPublishRow224{tx.Tx.QueryRow(ctx, q, args...)}
		}
	}
	if p.mode == "late_native" && strings.HasPrefix(q, "INSERT INTO pf_forms.period_event") && p.nativeEvents.Add(1) == 2 {
		p.hits.Add(1)
		return failedPublishRow224{}
	}
	if p.mode == "receipt_insert" && strings.HasPrefix(q, "INSERT INTO pf_forms.publish_operation_receipt") {
		p.hits.Add(1)
		return failedPublishRow224{}
	}
	return tx.Tx.QueryRow(ctx, q, args...)
}
func (tx publishFaultTx224) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if tx.probe.mode == "key_insert" && strings.HasPrefix(q, "INSERT INTO pf_forms.publish_operation_key") {
		tx.probe.hits.Add(1)
		return pgconn.CommandTag{}, errors.New("fixture late permanent key insert failure224")
	}
	return tx.Tx.Exec(ctx, q, args...)
}

type failedPublishRow224 struct{}

func (failedPublishRow224) Scan(...any) error {
	return errors.New("fixture late publication insert failure224")
}

type corruptPublishRow224 struct{ pgx.Row }

func (row corruptPublishRow224) Scan(dest ...any) error {
	if err := row.Row.Scan(dest...); err != nil {
		return err
	}
	raw, ok := dest[0].(*[]byte)
	if !ok {
		return errors.New("bad receipt fault destination")
	}
	var receipt port.PublishOperationReceipt
	if err := json.Unmarshal(*raw, &receipt); err != nil {
		return err
	}
	if len(receipt.Items) == 0 {
		return errors.New("missing last item fault boundary")
	}
	receipt.Items[len(receipt.Items)-1].Applied.EventSeq++
	encoded, err := json.Marshal(receipt)
	*raw = encoded
	return err
}

func TestPublishOperation224_ManyPeriodsAndPinnedDefaults(t *testing.T) {
	f, receipts, ctx, req := publishFixture224(t)
	profile := f.svc.profiles["fx"]
	profile.MaxSubmissions = 3
	f.svc.profiles["fx"] = profile
	req.Input.MaxSubmissions = nil
	req.Input.Periods = nil
	for i := 0; i < 32; i++ {
		from := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i)
		req.Input.Periods = append(req.Input.Periods, port.PeriodDef{Label: fmt.Sprintf("Kỳ %d", i), Kind: "day", Key: fmt.Sprintf("B-%02d", i), From: from, To: from, DueAt: from.AddDate(0, 0, 2), Rule: "many/v1"})
	}
	got, err := receipts.ExecutePublishOperation(ctx, req)
	appliedPublish224(t, got, err, 32)
	if !reflect.DeepEqual(nativeCounts224(f), []int{32, 32, 32, 1, 1, 0}) || f.scalar(`SELECT count(*) FROM pf_forms.period_spec WHERE tenant_id=$1 AND max_submissions=3`, f.tid) != 32 {
		t.Fatal("many batch/default atomicity")
	}
	profile.MaxSubmissions = 5
	f.svc.profiles["fx"] = profile
	read, err := receipts.ReadPublishOperation(ctx, req)
	f.must(err, "original receipt after profile default change")
	if !reflect.DeepEqual(read, got) {
		t.Fatal("receipt was rebuilt with a current default")
	}
	changed, err := port.NormalizePublishOperation(req)
	f.must(err, "owned command copy")
	zero := 0
	changed.Input.MaxSubmissions = &zero
	unknown, err := receipts.ExecutePublishOperation(ctx, changed)
	f.must(err, "nil versus zero body binding")
	unknownPublishTest224(t, unknown)
	if count212(f, "period") != 32 || count212(f, "publish_operation_key") != 1 {
		t.Fatal("nil/zero mismatch appended")
	}
}

func TestPublishOperation224_NilAndJoinedTransactionFailClosed(t *testing.T) {
	f, receipts, ctx, req := publishFixture224(t)
	unknown, err := (PublicationReceipts{}).ExecutePublishOperation(ctx, req)
	f.want(err, port.ErrPublishOperationUnavailable.Code, "nil service")
	unknownPublishTest224(t, unknown)
	var got port.PublishOperationReceipt
	var callErr error
	err = f.svc.tx(ctx, f.tid, func(tx pgx.Tx) error {
		got, callErr = receipts.ExecutePublishOperation(db.JoinTx(ctx, tx), req)
		return nil
	})
	f.must(err, "joined caller TX control")
	f.want(callErr, port.ErrPublishOperationUnavailable.Code, "joined caller capability refused")
	unknownPublishTest224(t, got)
	if !reflect.DeepEqual(nativeCounts224(f), []int{0, 0, 0, 0, 0, 0}) {
		t.Fatal("unavailable capability performed native effect")
	}
}

func TestPublishOperation224_TwoActorsDifferentBodiesContendOnOwnOperation(t *testing.T) {
	f, receipts, ctx, req := publishFixture224(t)
	f.svc.DB = db.ForModule(f.env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 2), DBModule)
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	entered := make(chan int, 1)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	copyService := *f.svc
	copyService.DB = holdPublishLock224{Beginner: f.svc.DB, entered: entered, release: release}
	type result struct {
		who     string
		receipt port.PublishOperationReceipt
		err     error
	}
	done := make(chan result, 2)
	started, joined := 0, 0
	defer func() {
		unblock()
		cancel()
		for joined < started {
			select {
			case <-done:
				joined++
			case <-time.After(5 * time.Second):
				t.Error("publication fixture caller failed to exit after cancellation")
				return
			}
		}
	}()
	started++
	go func() {
		r, e := (PublicationReceipts{&copyService}).ExecutePublishOperation(bounded, req)
		done <- result{"original", r, e}
	}()
	var holder int
	select {
	case holder = <-entered:
	case result := <-done:
		joined++
		t.Fatalf("first operation failed before lock barrier: %v", result.err)
	case <-bounded.Done():
		t.Fatal("native operation lock not reached")
	}
	other, err := port.NormalizePublishOperation(req)
	f.must(err, "different command copy")
	other.Input.Periods[0].Label = "Contending actor body"
	principal, _ := authz.FromContext(ctx)
	principal.ActorRef = ids.New()
	otherCtx := authz.WithPrincipal(bounded, principal)
	started++
	go func() { r, e := receipts.ExecutePublishOperation(otherCtx, other); done <- result{"other", r, e} }()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	waiting := false
	for !waiting {
		err = f.env.AdminPool(t, migrations.PF).QueryRow(bounded, `SELECT EXISTS(SELECT 1 FROM pg_locks w JOIN pg_locks h
   ON w.locktype=h.locktype AND w.database IS NOT DISTINCT FROM h.database AND w.classid IS NOT DISTINCT FROM h.classid
   AND w.objid IS NOT DISTINCT FROM h.objid AND w.objsubid IS NOT DISTINCT FROM h.objsubid
   WHERE h.pid=$1 AND h.granted AND NOT w.granted AND w.pid<>h.pid AND w.locktype='advisory')`, holder).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ticker.C:
		case <-bounded.Done():
			t.Fatal("second actor never waited on actual native operation lock")
		}
	}
	unblock()
	var original port.PublishOperationReceipt
	for joined < started {
		select {
		case result := <-done:
			joined++
			if result.who == "original" {
				appliedPublish224(t, result.receipt, result.err, 2)
				original = result.receipt
			} else {
				f.must(result.err, "contending actor is opaque")
				unknownPublishTest224(t, result.receipt)
			}
		case <-bounded.Done():
			t.Fatal("contending operation did not finish")
		}
	}
	if !reflect.DeepEqual(nativeCounts224(f), []int{2, 2, 2, 1, 1, 0}) {
		t.Fatal("contending actor/body created another native batch")
	}
	read, err := receipts.ReadPublishOperation(bounded, req)
	f.must(err, "winner original receipt after actual waiter")
	if !reflect.DeepEqual(read, original) {
		t.Fatal("loser changed the winner receipt")
	}
}

type holdPublishLock224 struct {
	tenant.Beginner
	entered chan<- int
	release <-chan struct{}
}
type holdPublishLockTx224 struct {
	pgx.Tx
	owner holdPublishLock224
}

func (b holdPublishLock224) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.Beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return holdPublishLockTx224{tx, b}, nil
}
func (tx holdPublishLockTx224) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, q, args...)
	if err != nil {
		return tag, err
	}
	if strings.Contains(q, "SELECT pg_advisory_xact_lock(hashtextextended('pf_forms.publish_operation:'") {
		var pid int
		if err := tx.Tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
			return tag, err
		}
		select {
		case tx.owner.entered <- pid:
		case <-ctx.Done():
			return tag, ctx.Err()
		}
		select {
		case <-tx.owner.release:
		case <-ctx.Done():
			return tag, ctx.Err()
		}
	}
	return tag, nil
}
