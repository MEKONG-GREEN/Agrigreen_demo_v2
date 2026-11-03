package app

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func receiptFixture212(t *testing.T) (*fx, ReceiptControl, Control, context.Context, port.PeriodOperationRequest) {
	t.Helper()
	f, control, ctx, id, before := controlFixture159(t)
	principal, _ := authz.FromContext(ctx)
	principal.MFA = true
	ctx = authz.WithPrincipal(ctx, principal)
	f.svc.PeriodOperationPolicies = map[string]port.PeriodOperationPolicy{"fx": func(ctx context.Context, _ authz.Principal, _ port.PeriodOperationRequest) (port.PeriodOperationGrant, error) {
		if _, joined := db.JoinedTx(ctx); joined {
			t.Error("receipt policy inside TX")
		}
		return port.PeriodOperationGrant{Allowed: true, Binding: "fixture-current-owner-v1"}, nil
	}}
	return f, ReceiptControl{f.svc}, control, ctx, port.PeriodOperationRequest{OperationID: ids.New(), Profile: "fx", PeriodID: id, Action: port.PeriodOperationChangeDue, Expected: before.Revision, DueAt: before.Progress.DueAt.Add(time.Hour), Reason: "deadline operation"}
}
func unknown212(t *testing.T, got port.PeriodOperationReceipt) {
	t.Helper()
	if !reflect.DeepEqual(got, unknownOperation()) {
		t.Fatalf("unknown leaked receipt metadata: %+v", got)
	}
}
func applied212(t *testing.T, got port.PeriodOperationReceipt, err error) {
	t.Helper()
	if err != nil || got.Outcome != port.PeriodOperationApplied || got.RecordedAt.IsZero() {
		t.Fatalf("expected durable applied receipt: %+v err=%v", got, err)
	}
}
func count212(f *fx, table string) int {
	return f.scalar("SELECT count(*) FROM pf_forms."+table+" WHERE tenant_id=$1", f.tid)
}

func TestPeriodOperation212_FamilyReplayUsesOwnOutcome(t *testing.T) {
	f, receipts, control, ctx, req := receiptFixture212(t)
	first, err := receipts.ExecutePeriodOperation(ctx, req)
	applied212(t, first, err)
	if first.Applied.DueSeq != 1 || !first.Result.DueAt.Equal(req.DueAt) {
		t.Fatalf("not exact native due result: %+v", first)
	}
	closeReq := req
	closeReq.OperationID = ids.New()
	closeReq.Action = port.PeriodOperationClose
	closeReq.Expected = first.Applied
	closeReq.DueAt = time.Time{}
	closeReq.Reason = ""
	closed, err := receipts.ExecutePeriodOperation(ctx, closeReq)
	applied212(t, closed, err)
	if closed.Result.Status != "closed" || closed.Result.SnapshotID == uuid.Nil || closed.Result.SnapshotNo != 1 || len(closed.Result.SnapshotHash) != 64 {
		t.Fatalf("close metadata absent: %+v", closed)
	}
	reopen := closeReq
	reopen.OperationID = ids.New()
	reopen.Action = port.PeriodOperationReopen
	reopen.Expected = closed.Applied
	reopen.Reason = "reopen original close"
	opened, err := receipts.ExecutePeriodOperation(ctx, reopen)
	applied212(t, opened, err)
	cancel := reopen
	cancel.OperationID = ids.New()
	cancel.Action = port.PeriodOperationCancel
	cancel.Expected = opened.Applied
	cancel.Reason = "cancel after reopened"
	cancelled, err := receipts.ExecutePeriodOperation(ctx, cancel)
	applied212(t, cancelled, err)
	if cancelled.Result.Status != "cancelled" {
		t.Fatal(cancelled)
	}
	for _, old := range []struct {
		req     port.PeriodOperationRequest
		receipt port.PeriodOperationReceipt
	}{{req, first}, {closeReq, closed}, {reopen, opened}, {cancel, cancelled}} {
		replay, err := receipts.ExecutePeriodOperation(ctx, old.req)
		f.must(err, "execute replay after successor")
		read, err := receipts.ReadPeriodOperation(ctx, old.req)
		f.must(err, "read receipt after successor")
		if !reflect.DeepEqual(replay, old.receipt) || !reflect.DeepEqual(read, old.receipt) {
			t.Fatalf("replay replaced own outcome with successor: old=%+v replay=%+v read=%+v", old.receipt, replay, read)
		}
	}
	current, err := control.ControlSnapshot(ctx, "fx", req.PeriodID)
	f.must(err, "current")
	if current.Revision != cancelled.Applied || count212(f, "period_operation_key") != 4 || count212(f, "period_operation_receipt") != 4 || count212(f, "close_snapshot") != 1 {
		t.Fatalf("replay wrote native/receipt/snapshot again: %+v", current)
	}
}
func TestPeriodOperation212_StaleABARejectionIsDurable(t *testing.T) {
	f, receipts, control, ctx, req := receiptFixture212(t)
	initial, err := control.ControlSnapshot(ctx, "fx", req.PeriodID)
	f.must(err, "initial")
	f.must(control.ChangeDue(ctx, "fx", req.PeriodID, req.DueAt, "legacy B", ids.New().String()), "B")
	f.must(control.ChangeDue(ctx, "fx", req.PeriodID, initial.Progress.DueAt, "legacy A", ids.New().String()), "A")
	rejected, err := receipts.ExecutePeriodOperation(ctx, req)
	f.must(err, "durable stale rejection")
	if rejected.Outcome != port.PeriodOperationRejected || rejected.RejectionCode != port.ErrPeriodConflict.Code || rejected.Applied != (port.PeriodRevision{}) {
		t.Fatalf("false applied: %+v", rejected)
	}
	successor, err := control.ControlSnapshot(ctx, "fx", req.PeriodID)
	f.must(err, "successor")
	f.must(control.ChangeDue(ctx, "fx", req.PeriodID, req.DueAt.Add(time.Hour), "successor", ids.New().String()), "successor")
	replay, err := receipts.ExecutePeriodOperation(ctx, req)
	f.must(err, "replay rejection")
	if !reflect.DeepEqual(rejected, replay) || successor.Revision.DueSeq != 2 || count212(f, "period_operation_receipt") != 1 {
		t.Fatalf("rejection changed: %+v %+v", rejected, replay)
	}
}
func TestPeriodOperation212_CurrentAuthorityAndOpaqueReads(t *testing.T) {
	f, receipts, _, ctx, req := receiptFixture212(t)
	first, err := receipts.ExecutePeriodOperation(ctx, req)
	applied212(t, first, err)
	t.Run("changed body", func(t *testing.T) {
		changed := req
		changed.Reason = "different original body"
		got, err := receipts.ReadPeriodOperation(ctx, changed)
		f.must(err, "opaque read")
		unknown212(t, got)
		got, err = receipts.ExecutePeriodOperation(ctx, changed)
		f.must(err, "opaque execute")
		unknown212(t, got)
	})
	t.Run("other actor", func(t *testing.T) {
		p, _ := authz.FromContext(ctx)
		p.ActorRef = ids.New()
		other := authz.WithPrincipal(ctx, p)
		got, err := receipts.ReadPeriodOperation(other, req)
		f.must(err, "other actor")
		unknown212(t, got)
		got, err = receipts.ExecutePeriodOperation(other, req)
		f.must(err, "other actor execute")
		unknown212(t, got)
	})
	t.Run("missing MFA", func(t *testing.T) {
		p, _ := authz.FromContext(ctx)
		p.MFA = false
		got, err := receipts.ReadPeriodOperation(authz.WithPrincipal(ctx, p), req)
		f.want(err, "forbidden", "MFA")
		unknown212(t, got)
	})
	t.Run("other tenant", func(t *testing.T) {
		p, _ := authz.FromContext(ctx)
		p.TenantID = ids.New()
		got, err := receipts.ReadPeriodOperation(authz.WithPrincipal(ctx, p), req)
		f.want(err, "forbidden", "tenant")
		unknown212(t, got)
	})
	t.Run("revoked", func(t *testing.T) {
		f.svc.PeriodOperationPolicies["fx"] = func(context.Context, authz.Principal, port.PeriodOperationRequest) (port.PeriodOperationGrant, error) {
			return port.PeriodOperationGrant{}, nil
		}
		got, err := receipts.ReadPeriodOperation(ctx, req)
		f.want(err, "forbidden", "revocation")
		unknown212(t, got)
	})
	t.Run("authority changed", func(t *testing.T) {
		f.svc.PeriodOperationPolicies["fx"] = func(context.Context, authz.Principal, port.PeriodOperationRequest) (port.PeriodOperationGrant, error) {
			return port.PeriodOperationGrant{Allowed: true, Binding: "fixture-owner-v2"}, nil
		}
		got, err := receipts.ReadPeriodOperation(ctx, req)
		f.must(err, "changed authority")
		unknown212(t, got)
		got, err = receipts.ExecutePeriodOperation(ctx, req)
		f.must(err, "changed authority execute")
		unknown212(t, got)
	})
	t.Run("missing policy", func(t *testing.T) {
		f.svc.PeriodOperationPolicies["fx"] = nil
		got, err := receipts.ExecutePeriodOperation(ctx, req)
		f.want(err, port.ErrPeriodOperationUnavailable.Code, "missing policy")
		unknown212(t, got)
	})
	if count212(f, "period_operation_receipt") != 1 || count212(f, "period_due") != 1 {
		t.Fatal("unauthorized retry wrote")
	}
}

// The database commits but transport loses ACK. Reads resolve only the receipt
// of that exact operation, even after another writer changes the period.
func TestPeriodOperation212_CommitACKAndConsumerCrashWindows(t *testing.T) {
	for _, committed := range []bool{true, false} {
		t.Run(map[bool]string{true: "committed_ACK_lost", false: "rollback_ACK_unknown"}[committed], func(t *testing.T) {
			f, receipts, control, ctx, req := receiptFixture212(t)
			copyService := *f.svc
			copyService.DB = ackBeginner212{Beginner: copyService.DB, commit: committed}
			uncertain := ReceiptControl{&copyService}
			got, err := uncertain.ExecutePeriodOperation(ctx, req)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("fault not reached: %v", err)
			}
			unknown212(t, got)
			read, err := receipts.ReadPeriodOperation(ctx, req)
			f.must(err, "resolve after consumer lost response")
			if !committed {
				unknown212(t, read)
				if count212(f, "period_due") != 0 || count212(f, "period_operation_key") != 0 {
					t.Fatal("rollback was not atomic")
				}
				return
			}
			applied212(t, read, nil)
			// Consumer crash before saving the result: persist/read the exact receipt,
			// not a fresh revision inferred from the successor's state.
			next, err := control.ChangeDueCAS(ctx, "fx", req.PeriodID, read.Applied, req.DueAt.Add(time.Hour), "independent successor", ids.New().String())
			f.must(err, "successor")
			replay, err := receipts.ExecutePeriodOperation(ctx, req)
			f.must(err, "retry original intent")
			if !reflect.DeepEqual(replay, read) || replay.Applied == next || count212(f, "period_due") != 2 || count212(f, "period_operation_receipt") != 1 {
				t.Fatal("receipt inferred successor or repeated mutation")
			}
		})
	}
}

type ackBeginner212 struct {
	tenant.Beginner
	commit bool
}
type ackTx212 struct {
	pgx.Tx
	commit bool
}

func (b ackBeginner212) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.Beginner.BeginTx(ctx, o)
	if err != nil {
		return nil, err
	}
	return ackTx212{tx, b.commit}, nil
}
func (tx ackTx212) Commit(ctx context.Context) error {
	if tx.commit {
		if err := tx.Tx.Commit(ctx); err != nil {
			return err
		}
	} else {
		if err := tx.Tx.Rollback(ctx); err != nil {
			return err
		}
	}
	return context.DeadlineExceeded
}

func TestPeriodOperation212_TwoActorsSameKeyDifferentBody(t *testing.T) {
	f, receipts, _, ctx, req := receiptFixture212(t)
	p, _ := authz.FromContext(ctx)
	p.ActorRef = ids.New()
	other := authz.WithPrincipal(ctx, p)
	changed := req
	changed.DueAt = changed.DueAt.Add(time.Hour)
	changed.Reason = "second actor original body"
	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan port.PeriodOperationReceipt, 2)
	errs := make(chan error, 2)
	for _, call := range []struct {
		ctx context.Context
		req port.PeriodOperationRequest
	}{{ctx, req}, {other, changed}} {
		go func() {
			defer wg.Done()
			got, err := receipts.ExecutePeriodOperation(call.ctx, call.req)
			results <- got
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		f.must(err, "concurrent execute")
	}
	applied, unknown := 0, 0
	for got := range results {
		if got.Outcome == port.PeriodOperationApplied {
			applied++
		} else {
			unknown212(t, got)
			unknown++
		}
	}
	if applied != 1 || unknown != 1 || count212(f, "period_due") != 1 || count212(f, "period_operation_receipt") != 1 || count212(f, "period_operation_key") != 1 {
		t.Fatalf("operation key executed twice: applied=%d unknown=%d", applied, unknown)
	}
}

// Missing/purged receipt is modeled at the store read boundary without disabling
// append-only triggers or deleting a partition. The permanent identity is real.
func TestPeriodOperation212_MissingReceiptAnchorBlocksReexecution(t *testing.T) {
	f, receipts, _, ctx, req := receiptFixture212(t)
	first, err := receipts.ExecutePeriodOperation(ctx, req)
	applied212(t, first, err)
	copyService := *f.svc
	copyService.DB = missingReceiptBeginner212{copyService.DB}
	missing := ReceiptControl{&copyService}
	got, err := missing.ReadPeriodOperation(ctx, req)
	f.must(err, "missing receipt read")
	unknown212(t, got)
	got, err = missing.ExecutePeriodOperation(ctx, req)
	f.must(err, "missing receipt execute")
	unknown212(t, got)
	if count212(f, "period_due") != 1 || count212(f, "period_operation_key") != 1 {
		t.Fatal("missing receipt anchor allowed reexecution")
	}
}

type missingReceiptBeginner212 struct{ tenant.Beginner }
type missingReceiptTx212 struct{ pgx.Tx }

func (b missingReceiptBeginner212) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.Beginner.BeginTx(ctx, o)
	if err != nil {
		return nil, err
	}
	return missingReceiptTx212{tx}, nil
}
func (tx missingReceiptTx212) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if q == "SELECT content,recorded_at FROM pf_forms.period_operation_receipt WHERE tenant_id=$1 AND id=$2 ORDER BY seq LIMIT 1" {
		return missingRow212{}
	}
	return tx.Tx.QueryRow(ctx, q, args...)
}

type missingRow212 struct{}

func (missingRow212) Scan(...any) error { return pgx.ErrNoRows }

func TestPeriodOperation212_PolicyOutsideTXWithOneConnection(t *testing.T) {
	f, receipts, _, ctx, req := receiptFixture212(t)
	f.svc.DB = db.ForModule(f.env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1), DBModule)
	f.svc.PeriodOperationPolicies["fx"] = func(ctx context.Context, _ authz.Principal, _ port.PeriodOperationRequest) (port.PeriodOperationGrant, error) {
		if _, joined := db.JoinedTx(ctx); joined {
			t.Fatal("owner I/O in native TX")
		}
		err := f.svc.tx(ctx, f.tid, func(tx pgx.Tx) error { var n int; return tx.QueryRow(ctx, "SELECT 1").Scan(&n) })
		return port.PeriodOperationGrant{Allowed: true, Binding: "fixture-current-owner-v1"}, err
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	got, err := receipts.ExecutePeriodOperation(bounded, req)
	applied212(t, got, err)
	read, err := receipts.ReadPeriodOperation(bounded, req)
	f.must(err, "receipt policy same pool read")
	if !reflect.DeepEqual(got, read) {
		t.Fatal("pool1 receipt changed")
	}
}

func TestPeriodOperation212_ReceiptFailureRollsBackNativeEffects(t *testing.T) {
	f, receipts, _, ctx, req := receiptFixture212(t)
	copyService := *f.svc
	copyService.DB = failReceiptBeginner212{copyService.DB}
	broken := ReceiptControl{&copyService}
	got, err := broken.ExecutePeriodOperation(ctx, req)
	if err == nil {
		t.Fatal("receipt insert failure ignored")
	}
	unknown212(t, got)
	if count212(f, "period_due") != 0 || count212(f, "period_operation_key") != 0 || count212(f, "period_operation_receipt") != 0 {
		t.Fatal("partial native write committed after receipt error")
	}
	read, err := receipts.ReadPeriodOperation(ctx, req)
	f.must(err, "read rollback")
	unknown212(t, read)
	got, err = receipts.ExecutePeriodOperation(ctx, req)
	applied212(t, got, err)
}

type failReceiptBeginner212 struct{ tenant.Beginner }
type failReceiptTx212 struct{ pgx.Tx }

func (b failReceiptBeginner212) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.Beginner.BeginTx(ctx, o)
	if err != nil {
		return nil, err
	}
	return failReceiptTx212{tx}, nil
}
func (tx failReceiptTx212) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if len(q) > len("INSERT INTO pf_forms.period_operation_key") && q[:len("INSERT INTO pf_forms.period_operation_key")] == "INSERT INTO pf_forms.period_operation_key" {
		return pgconn.CommandTag{}, errors.New("fixture receipt insert failed")
	}
	return tx.Tx.Exec(ctx, q, args...)
}

func TestPeriodOperation212_DisabledServiceAndMalformedProfile(t *testing.T) {
	f, receipts, _, ctx, req := receiptFixture212(t)
	got, err := (ReceiptControl{}).ExecutePeriodOperation(ctx, req)
	f.want(err, port.ErrPeriodOperationUnavailable.Code, "nil supplier service")
	unknown212(t, got)
	malformed := req
	malformed.Profile = string([]byte{'f', 'x', 0xff})
	malformed.Expected.Profile = malformed.Profile
	got, err = receipts.ExecutePeriodOperation(ctx, malformed)
	f.want(err, "validation_error", "malformed profile")
	unknown212(t, got)
	if count212(f, "period_operation_key") != 0 || count212(f, "period_due") != 0 {
		t.Fatal("disabled/malformed capability wrote")
	}
}
