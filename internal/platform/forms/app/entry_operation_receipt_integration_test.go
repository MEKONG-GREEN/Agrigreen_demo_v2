package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
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

var _ port.EntryOperations = Entry{}

// This is an independent synthetic owner adapter. Production KN authority is
// separately exercised through Authenticate + accounts middleware in assembly.
func entryFixture288(t *testing.T) (*fx, Entry, context.Context, context.Context, port.ReviewOperationRequest) {
	t.Helper()
	f, _, ctx, period, _ := controlFixture159(t)
	p, _ := authz.FromContext(ctx)
	p.MFA = true
	selected, err := authz.NewSession("fixture", ids.New(), authz.SessionScope{NodeID: ids.New()})
	f.must(err, "fixture owner context")
	p.SelectedSession = &selected
	ctx = authz.WithPrincipal(ctx, p)
	f.svc.ReviewOperationPolicies = map[string]port.ReviewOperationPolicy{"fx": func(ctx context.Context, _ authz.Principal, _ port.ReviewOperationRequest) (port.ReviewOperationGrant, error) {
		if _, joined := db.JoinedTx(ctx); joined {
			t.Error("receipt owner policy called inside DBTX")
		}
		return port.ReviewOperationGrant{Allowed: true, Binding: "fixture-authority-v1"}, nil
	}}
	leaf := f.as(f.tid, uuid.Nil, "xa", ids.New(), "U1")
	sb, err := f.svc.Submit(leaf, "fx", period, "U1", fx01, "")
	f.must(err, "initial native submission")
	return f, Entry{f.svc}, ctx, leaf, port.ReviewOperationRequest{OperationID: ids.New(), Profile: "fx", PeriodID: period, Unit: "U1", SubmissionID: sb.Submission.ID, ExpectedVersion: sb.Submission.Version, Decision: "returned", Note: "Bổ sung số liệu"}
}
func unknown288(t *testing.T, r port.ReviewOperationReceipt) {
	t.Helper()
	if !reflect.DeepEqual(r, unknownReviewOperation()) {
		t.Fatalf("unknown exposed operation metadata: %+v", r)
	}
}
func applied288(t *testing.T, r port.ReviewOperationReceipt, err error) {
	t.Helper()
	if err != nil || r.Outcome != port.ReviewOperationApplied || r.RecordedAt.IsZero() || r.Result.ReviewID == uuid.Nil || r.Result.ReviewSeq != 1 || r.Result.Reviewer != r.ActorRef || r.Result.ReviewedAt.IsZero() || r.Result.ReviewRecordedAt.IsZero() || r.Result.WorkflowRequestID == nil {
		t.Fatalf("not exact native applied evidence: %+v %v", r, err)
	}
}
func reviewCounts288(f *fx) []int {
	return []int{count212(f, "review"), count212(f, "review_operation_key"), count212(f, "review_operation_receipt"), f.scalar(`SELECT count(*) FROM pf_workflow.decision WHERE tenant_id=$1`, f.tid), f.scalar(`SELECT count(*) FROM shared.outbox WHERE tenant_id=$1`, f.tid), count212(f, "sync_feed")}
}

func TestEntryOperation288_NativeLostACKRequiresImmutableReceipt(t *testing.T) {
	f, entry, ctx, leaf, req := entryFixture288(t)
	copyService := *f.svc
	copyService.DB = ackBeginner212{Beginner: f.svc.DB, commit: true}
	lost, err := (Entry{&copyService}).ExecuteReviewOperation(ctx, req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("actual COMMIT ACK loss not exercised: %v", err)
	}
	unknown288(t, lost)
	first, err := entry.ReadReviewOperation(ctx, req)
	applied288(t, first, err)
	if first.OperationID != req.OperationID || first.SubmissionID != req.SubmissionID || first.ExpectedVersion != 1 || first.Decision != "returned" {
		t.Fatal(first)
	}
	// Exact timestamp/actor/sequence are read from the committed native row,
	// including PostgreSQL precision, rather than reconstructed from a clock.
	var id, reviewer uuid.UUID
	var at, recorded time.Time
	var seq int64
	err = f.env.AdminPool(t, migrations.PF).QueryRow(ctx, `SELECT id,seq,reviewer_ref,event_time,recorded_at FROM pf_forms.review WHERE tenant_id=$1 AND submission_id=$2`, f.tid, req.SubmissionID).Scan(&id, &seq, &reviewer, &at, &recorded)
	f.must(err, "exact native oracle")
	if id != first.Result.ReviewID || seq != first.Result.ReviewSeq || reviewer != first.ActorRef || !at.Equal(first.Result.ReviewedAt) || !recorded.Equal(first.Result.ReviewRecordedAt) {
		t.Fatalf("native receipt identity/time mismatch: %+v", first)
	}
	for n := 0; n < 2; n++ {
		sb, err := f.svc.Submit(leaf, "fx", req.PeriodID, "U1", fx01, "")
		f.must(err, "successor submission")
		next := req
		next.OperationID, next.SubmissionID, next.ExpectedVersion = ids.New(), sb.Submission.ID, sb.Submission.Version
		r, err := entry.ExecuteReviewOperation(ctx, next)
		applied288(t, r, err)
		if r.Decision != first.Decision || r.Result.ReviewID == first.Result.ReviewID {
			t.Fatal("ABA scenario did not preserve distinct identities")
		}
	}
	before := reviewCounts288(f)
	for _, read := range []bool{false, true} {
		var got port.ReviewOperationReceipt
		if read {
			got, err = entry.ReadReviewOperation(ctx, req)
		} else {
			got, err = entry.ExecuteReviewOperation(ctx, req)
		}
		f.must(err, "old operation after ABA")
		if !reflect.DeepEqual(first, got) {
			t.Fatalf("successor replaced old proof: first=%+v got=%+v", first, got)
		}
	}
	if !reflect.DeepEqual(before, reviewCounts288(f)) {
		t.Fatal("replay duplicated native review/workflow/outbox/audit")
	}
	raw, err := json.Marshal(first)
	f.must(err, "receipt json")
	if strings.Contains(string(raw), req.Note) || strings.Contains(string(raw), "area_sown") {
		t.Fatal("receipt duplicated note or form payload")
	}
}

func TestEntryOperation288_ReadAbsentDoesNotMutateAndRollbackACKUnknown(t *testing.T) {
	f, entry, ctx, _, req := entryFixture288(t)
	before := reviewCounts288(f)
	got, err := entry.ReadReviewOperation(ctx, req)
	f.must(err, "absent read")
	unknown288(t, got)
	copyService := *f.svc
	copyService.DB = ackBeginner212{Beginner: f.svc.DB, commit: false}
	got, err = (Entry{&copyService}).ExecuteReviewOperation(ctx, req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	unknown288(t, got)
	got, err = entry.ReadReviewOperation(ctx, req)
	f.must(err, "unknown rollback read")
	unknown288(t, got)
	if !reflect.DeepEqual(before, reviewCounts288(f)) {
		t.Fatal("read or rollback leaked effects")
	}
}

func TestEntryOperation288_RequestActorTenantAndCurrentAuthority(t *testing.T) {
	f, entry, ctx, _, req := entryFixture288(t)
	first, err := entry.ExecuteReviewOperation(ctx, req)
	applied288(t, first, err)
	before := reviewCounts288(f)
	for _, field := range []string{"note", "decision", "version", "profile", "unit", "period", "submission", "actor", "tenant", "selected_context", "selected_payload", "policy_binding"} {
		t.Run(field, func(t *testing.T) {
			c, r := ctx, req
			p, _ := authz.FromContext(ctx)
			switch field {
			case "note":
				r.Note = "Other normalized note"
			case "decision":
				r.Decision = "accepted"
			case "version":
				r.ExpectedVersion++
			case "profile":
				r.Profile = "prod"
			case "unit":
				r.Unit = "OUTSIDE"
			case "period":
				r.PeriodID = ids.New()
			case "submission":
				r.SubmissionID = ids.New()
			case "actor":
				p.ActorRef = ids.New()
			case "tenant":
				p.TenantID = ids.New()
			case "selected_context":
				s, e := authz.NewSession("fixture", ids.New(), p.SelectedSession.Scope())
				f.must(e, "new context")
				p.SelectedSession = &s
			case "selected_payload":
				s := p.SelectedSession.WithPayload([]byte(`{"assignment_revision":2}`))
				p.SelectedSession = &s
			case "policy_binding":
				old := f.svc.ReviewOperationPolicies["fx"]
				f.svc.ReviewOperationPolicies["fx"] = func(context.Context, authz.Principal, port.ReviewOperationRequest) (port.ReviewOperationGrant, error) {
					return port.ReviewOperationGrant{Allowed: true, Binding: "fixture-authority-v2"}, nil
				}
				defer func() { f.svc.ReviewOperationPolicies["fx"] = old }()
			}
			c = authz.WithPrincipal(c, p)
			for _, read := range []bool{true, false} {
				var got port.ReviewOperationReceipt
				if read {
					got, err = entry.ReadReviewOperation(c, r)
				} else {
					got, err = entry.ExecuteReviewOperation(c, r)
				}
				unknown288(t, got)
			}
		})
	}
	normalized := req
	normalized.Note = " " + req.Note + " "
	normalized.Decision = " returned "
	normalized.Unit = " U1 "
	got, err := entry.ExecuteReviewOperation(ctx, normalized)
	f.must(err, "normalized retry")
	if !reflect.DeepEqual(first, got) || !reflect.DeepEqual(before, reviewCounts288(f)) {
		t.Fatal("binding mismatch mutated history or canonical retry failed")
	}
}

type countBeginner288 struct {
	tenant.Beginner
	begins *atomic.Int32
}

func (b countBeginner288) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	b.begins.Add(1)
	return b.Beginner.BeginTx(ctx, o)
}

func TestEntryOperation288_RevocationMFAReadonlyRoleBeforeDB(t *testing.T) {
	f, entry, ctx, _, req := entryFixture288(t)
	first, err := entry.ExecuteReviewOperation(ctx, req)
	applied288(t, first, err)
	for _, mode := range []string{"anonymous", "mfa", "no_selected_context", "invalid_selected_context", "role", "revoked", "readonly", "nil_policy"} {
		t.Run(mode, func(t *testing.T) {
			var begins atomic.Int32
			svc := *f.svc
			svc.DB = countBeginner288{Beginner: f.svc.DB, begins: &begins}
			svc.ReviewOperationPolicies = map[string]port.ReviewOperationPolicy{"fx": f.svc.ReviewOperationPolicies["fx"]}
			c := ctx
			p, _ := authz.FromContext(ctx)
			switch mode {
			case "anonymous":
				c = context.Background()
			case "mfa":
				p.MFA = false
				c = authz.WithPrincipal(c, p)
			case "no_selected_context":
				p.SelectedSession = nil
				c = authz.WithPrincipal(c, p)
			case "invalid_selected_context":
				p.SelectedSession = &authz.Session{}
				c = authz.WithPrincipal(c, p)
			case "role":
				c = fixtureContext(c, p, port.ResolvedActor{Role: "xa", Scope: authz.Scope{Units: []string{"U1"}}})
			case "revoked", "readonly":
				svc.ReviewOperationPolicies["fx"] = func(context.Context, authz.Principal, port.ReviewOperationRequest) (port.ReviewOperationGrant, error) {
					return port.ReviewOperationGrant{}, problem.Forbidden
				}
			case "nil_policy":
				svc.ReviewOperationPolicies = nil
			}
			for _, read := range []bool{true, false} {
				var r port.ReviewOperationReceipt
				if read {
					r, err = (Entry{&svc}).ReadReviewOperation(c, req)
				} else {
					r, err = (Entry{&svc}).ExecuteReviewOperation(c, req)
				}
				if err == nil {
					t.Fatal("revoked authority accessed cached applied receipt")
				}
				unknown288(t, r)
			}
			if begins.Load() != 0 {
				t.Fatalf("authority denial reached DB before rejection: %d", begins.Load())
			}
		})
	}
}

func TestEntryOperation288_NativeRejectionAndTerminalRules(t *testing.T) {
	for _, mode := range []string{"version", "self", "already_reviewed", "superseded", "closed", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			f, entry, ctx, leaf, req := entryFixture288(t)
			want := ""
			switch mode {
			case "version":
				req.ExpectedVersion++
				want = port.ErrVersionConflict.Code
			case "self":
				p, _ := authz.FromContext(ctx)
				submitter, _ := authz.FromContext(leaf)
				p.ActorRef = submitter.ActorRef
				ctx = authz.WithPrincipal(ctx, p)
				want = problem.SelfApproval.Code
			case "already_reviewed":
				_, err := f.svc.Review(ctx, "fx", req.SubmissionID, "returned", req.Note, "")
				f.must(err, "legacy native review")
				want = port.ErrAlreadyReviewed.Code
			case "superseded":
				_, err := f.svc.Review(ctx, "fx", req.SubmissionID, "returned", req.Note, "")
				f.must(err, "legacy native review")
				_, err = f.svc.Submit(leaf, "fx", req.PeriodID, "U1", fx01, "")
				f.must(err, "native successor")
				want = port.ErrSuperseded.Code
			case "closed":
				_, err := f.svc.Close(ctx, "fx", req.PeriodID, "")
				f.must(err, "native close")
				want = port.ErrPeriodClosed.Code
			case "cancelled":
				err := (Control{f.svc}).CancelPeriod(ctx, "fx", req.PeriodID, "cancel native test period", "")
				f.must(err, "native cancel")
				want = port.ErrPeriodCancelled.Code
			}
			before := reviewCounts288(f)
			got, err := entry.ExecuteReviewOperation(ctx, req)
			f.must(err, "durable pre-effect native rejection")
			if got.Outcome != port.ReviewOperationRejected || got.RejectionCode != want || got.Result != (port.ReviewOperationResult{}) {
				t.Fatalf("false native proof: %+v want %s", got, want)
			}
			read, err := entry.ReadReviewOperation(ctx, req)
			f.must(err, "rejected read")
			retry, err := entry.ExecuteReviewOperation(ctx, req)
			f.must(err, "rejected retry")
			after := reviewCounts288(f)
			if !reflect.DeepEqual(read, got) || !reflect.DeepEqual(retry, got) || after[0] != before[0] || after[3] != before[3] || after[4] != before[4] || after[5] != before[5] || after[1] != before[1]+1 || after[2] != before[2]+1 {
				t.Fatalf("rejected operation changed native effects: %v -> %v", before, after)
			}
		})
	}
}

type reviewFault288 struct {
	tenant.Beginner
	mode string
	hits *atomic.Int32
}
type reviewFaultTx288 struct {
	pgx.Tx
	fault reviewFault288
}

func (b reviewFault288) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, e := b.Beginner.BeginTx(ctx, o)
	if e != nil {
		return nil, e
	}
	return reviewFaultTx288{tx, b}, nil
}
func (tx reviewFaultTx288) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if (tx.fault.mode == "native_insert" && strings.HasPrefix(q, "INSERT INTO pf_forms.review (")) || (tx.fault.mode == "anchor_insert" && strings.HasPrefix(q, "INSERT INTO pf_forms.review_operation_key")) {
		tx.fault.hits.Add(1)
		return tx.Tx.Exec(ctx, `SELECT 1/0`)
	}
	return tx.Tx.Exec(ctx, q, args...)
}
func (tx reviewFaultTx288) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if tx.fault.mode == "receipt_insert" && strings.HasPrefix(q, "INSERT INTO pf_forms.review_operation_receipt") {
		tx.fault.hits.Add(1)
		return tx.Tx.QueryRow(ctx, `SELECT 1/0`)
	}
	if strings.HasPrefix(q, "SELECT content,recorded_at FROM pf_forms.review_operation_receipt") {
		if tx.fault.mode == "missing" {
			tx.fault.hits.Add(1)
			return missingRow212{}
		}
		if tx.fault.mode == "corrupt" {
			tx.fault.hits.Add(1)
			return corruptReviewRow288{tx.Tx.QueryRow(ctx, q, args...)}
		}
	}
	return tx.Tx.QueryRow(ctx, q, args...)
}
func (tx reviewFaultTx288) Commit(ctx context.Context) error {
	if tx.fault.mode == "commit_failure" {
		tx.fault.hits.Add(1)
		// A real PostgreSQL deferred FK violation occurs only on COMMIT,
		// after native review/workflow and receipt INSERT have succeeded.
		_, err := tx.Tx.Exec(ctx, `CREATE TEMP TABLE review_commit_parent288(id integer PRIMARY KEY) ON COMMIT DROP;
CREATE TEMP TABLE review_commit_child288(id integer REFERENCES review_commit_parent288(id) DEFERRABLE INITIALLY DEFERRED) ON COMMIT DROP;
INSERT INTO review_commit_child288 VALUES(1)`)
		if err != nil {
			return err
		}
	}
	return tx.Tx.Commit(ctx)
}

type corruptReviewRow288 struct{ pgx.Row }

func (r corruptReviewRow288) Scan(dest ...any) error {
	if err := r.Row.Scan(dest...); err != nil {
		return err
	}
	raw, ok := dest[0].(*[]byte)
	if !ok {
		return errors.New("wrong fault row shape")
	}
	var receipt port.ReviewOperationReceipt
	if err := json.Unmarshal(*raw, &receipt); err != nil {
		return err
	}
	receipt.Result.ReviewID = uuid.Nil
	encoded, err := json.Marshal(receipt)
	*raw = encoded
	return err
}

func TestEntryOperation288_InsertAndCommitFailureRollbackRecovery(t *testing.T) {
	for _, mode := range []string{"native_insert", "anchor_insert", "receipt_insert", "commit_failure"} {
		t.Run(mode, func(t *testing.T) {
			f, entry, ctx, _, req := entryFixture288(t)
			before := reviewCounts288(f)
			svc := *f.svc
			var hits atomic.Int32
			svc.DB = reviewFault288{f.svc.DB, mode, &hits}
			got, err := (Entry{&svc}).ExecuteReviewOperation(ctx, req)
			if err == nil || hits.Load() != 1 {
				t.Fatalf("fault not exercised: %s hits=%d err=%v", mode, hits.Load(), err)
			}
			unknown288(t, got)
			if !reflect.DeepEqual(before, reviewCounts288(f)) {
				t.Fatalf("partial native/audit/workflow/outbox commit: %v -> %v", before, reviewCounts288(f))
			}
			read, err := entry.ReadReviewOperation(ctx, req)
			f.must(err, "after failed commit read")
			unknown288(t, read)
			recovered, err := entry.ExecuteReviewOperation(ctx, req)
			applied288(t, recovered, err)
			retry, err := entry.ExecuteReviewOperation(ctx, req)
			f.must(err, "recovery retry")
			if !reflect.DeepEqual(recovered, retry) || count212(f, "review") != 1 || count212(f, "review_operation_key") != 1 || count212(f, "review_operation_receipt") != 1 {
				t.Fatal("recovery duplicated native effects")
			}
		})
	}
}

func TestEntryOperation288_MissingPurgedCorruptReceiptNeverReexecutes(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			f, entry, ctx, _, req := entryFixture288(t)
			r, err := entry.ExecuteReviewOperation(ctx, req)
			applied288(t, r, err)
			before := reviewCounts288(f)
			svc := *f.svc
			var hits atomic.Int32
			svc.DB = reviewFault288{f.svc.DB, mode, &hits}
			got, err := (Entry{&svc}).ReadReviewOperation(ctx, req)
			f.must(err, "unavailable history read")
			unknown288(t, got)
			got, err = (Entry{&svc}).ExecuteReviewOperation(ctx, req)
			f.must(err, "unavailable history replay")
			unknown288(t, got)
			if hits.Load() != 2 || !reflect.DeepEqual(before, reviewCounts288(f)) {
				t.Fatal("permanent anchor did not block re-execution")
			}
		})
	}
}

func TestEntryOperation288_PoolOneOwnerIOConcurrentRetryAndRLS(t *testing.T) {
	f, entry, ctx, _, req := entryFixture288(t)
	pool := f.env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	f.svc.DB = db.ForModule(pool, DBModule)
	f.svc.ReviewOperationPolicies["fx"] = func(ctx context.Context, _ authz.Principal, _ port.ReviewOperationRequest) (port.ReviewOperationGrant, error) {
		if _, joined := db.JoinedTx(ctx); joined {
			t.Error("external owner I/O inside native TX")
		}
		err := f.svc.tx(ctx, f.tid, func(tx pgx.Tx) error { var n int; return tx.QueryRow(ctx, `SELECT 1`).Scan(&n) })
		return port.ReviewOperationGrant{Allowed: true, Binding: "pool-one-owner"}, err
	}
	bounded, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	receipts := make(chan port.ReviewOperationReceipt, 4)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := entry.ExecuteReviewOperation(bounded, req)
			receipts <- r
			errs <- err
		}()
	}
	wg.Wait()
	close(receipts)
	close(errs)
	for err := range errs {
		f.must(err, "parallel pool1 retry")
	}
	var first *port.ReviewOperationReceipt
	for r := range receipts {
		applied288(t, r, nil)
		if first == nil {
			copy := r
			first = &copy
		} else if !reflect.DeepEqual(*first, r) {
			t.Fatal("concurrent retry replaced original receipt")
		}
	}
	if count212(f, "review") != 1 || count212(f, "review_operation_key") != 1 || count212(f, "review_operation_receipt") != 1 {
		t.Fatal("concurrent retry duplicated review")
	}
	p, _ := authz.FromContext(ctx)
	foreign := p
	p.TenantID = ids.New()
	other := authz.WithPrincipal(ctx, p)
	// Check the same physical connection under the foreign tenant and then back.
	err := db.WithModuleTenantTx(other, pool, DBModule, p.TenantID, func(tx pgx.Tx) error {
		var n int
		if e := tx.QueryRow(other, `SELECT (SELECT count(*) FROM pf_forms.review_operation_key)+(SELECT count(*) FROM pf_forms.review_operation_receipt)`).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			t.Fatal("anchor RLS leak")
		}
		return nil
	})
	f.must(err, "foreign anchor RLS")
	got, err := entry.ReadReviewOperation(authz.WithPrincipal(ctx, foreign), req)
	f.must(err, "same connection restored tenant")
	if !reflect.DeepEqual(*first, got) {
		t.Fatal("connection reuse changed receipt")
	}
}

func TestEntryOperation288_ConcurrentDifferentActorBodyOnlyOneReview(t *testing.T) {
	f, entry, ctx, _, req := entryFixture288(t)
	f.svc.DB = db.ForModule(f.env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 2), DBModule)
	p, _ := authz.FromContext(ctx)
	p.ActorRef = ids.New()
	other := authz.WithPrincipal(ctx, p)
	changed := req
	changed.Note = "Second actor original request"
	start := make(chan struct{})
	results := make(chan port.ReviewOperationReceipt, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, call := range []struct {
		ctx context.Context
		req port.ReviewOperationRequest
	}{{ctx, req}, {other, changed}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, err := entry.ExecuteReviewOperation(call.ctx, call.req)
			results <- r
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		f.must(err, "contending actor")
	}
	applied, unknown := 0, 0
	for r := range results {
		switch r.Outcome {
		case port.ReviewOperationApplied:
			applied++
			applied288(t, r, nil)
		case port.ReviewOperationUnknown:
			unknown++
			unknown288(t, r)
		default:
			t.Fatal(r)
		}
	}
	if applied != 1 || unknown != 1 || count212(f, "review") != 1 || count212(f, "review_operation_key") != 1 || count212(f, "review_operation_receipt") != 1 {
		t.Fatal("same key different actors/bodies duplicated native effects")
	}
}

func TestEntryOperation288_LimitFrozenAndJoinedTXCompatibility(t *testing.T) {
	f, entry, ctx, leaf, req := entryFixture288(t)
	templates, err := f.svc.Templates(ctx, "fx")
	f.must(err, "templates")
	prof := f.svc.profiles["fx"]
	prof.MaxSubmissions = 1
	f.svc.profiles["fx"] = prof
	period, err := f.svc.Publish(ctx, PublishInput{Profile: "fx", TemplateID: templates[0].ID, Label: "last submission native rule", DueAt: f.clk.Now().Add(time.Hour), Units: []string{"U1"}}, "")
	f.must(err, "limited period")
	sb, err := f.svc.Submit(leaf, "fx", period.Period.ID, "U1", fx01, "")
	f.must(err, "limited submission")
	req.PeriodID, req.SubmissionID = period.Period.ID, sb.Submission.ID
	got, err := entry.ExecuteReviewOperation(ctx, req)
	f.must(err, "native limit rejection")
	if got.Outcome != port.ReviewOperationRejected || got.RejectionCode != port.ErrSubmissionLimit.Code {
		t.Fatalf("native last-version return rule changed: %+v", got)
	}
	req.OperationID = ids.New()
	req.Decision = "accepted"
	req.Note = ""
	got, err = entry.ExecuteReviewOperation(ctx, req)
	applied288(t, got, err)
	_, err = f.svc.Submit(leaf, "fx", req.PeriodID, "U1", fx01, "")
	f.want(err, port.ErrSubmissionLocked.Code, "native accepted submission stays frozen")
	before := reviewCounts288(f)
	err = f.svc.tx(ctx, f.tid, func(tx pgx.Tx) error {
		r, e := entry.ReadReviewOperation(db.JoinTx(ctx, tx), req)
		if e == nil || !isProblem(e, port.ErrReviewOperationUnavailable.Code) {
			t.Fatalf("joined receipt I/O allowed: %v", e)
		}
		unknown288(t, r)
		return nil
	})
	f.must(err, "joined caller control")
	if !reflect.DeepEqual(before, reviewCounts288(f)) {
		t.Fatal("joined caller mutated receipt")
	}
}

func TestEntryOperation288_ImmutableAnchorAndLedgerAudit(t *testing.T) {
	f, entry, ctx, _, req := entryFixture288(t)
	r, err := entry.ExecuteReviewOperation(ctx, req)
	applied288(t, r, err)
	before := reviewCounts288(f)
	for _, q := range []string{
		`UPDATE pf_forms.review_operation_key SET receipt_id=receipt_id WHERE tenant_id=$1`,
		`DELETE FROM pf_forms.review_operation_key WHERE tenant_id=$1`,
		`UPDATE pf_forms.review_operation_receipt SET outcome=outcome WHERE tenant_id=$1`,
		`DELETE FROM pf_forms.review_operation_receipt WHERE tenant_id=$1`,
	} {
		_, err = f.env.AdminPool(t, migrations.PF).Exec(ctx, q, f.tid)
		if err == nil {
			t.Fatal("history change bypassed immutable guard")
		}
	}
	for _, q := range []string{`TRUNCATE pf_forms.review_operation_key`, `TRUNCATE pf_forms.review_operation_receipt`} {
		_, err = f.env.AdminPool(t, migrations.PF).Exec(ctx, q)
		if err == nil {
			t.Fatal("TRUNCATE bypassed history guard")
		}
	}
	if !reflect.DeepEqual(before, reviewCounts288(f)) {
		t.Fatal("immutable history changed")
	}
	got, err := entry.ReadReviewOperation(ctx, req)
	f.must(err, "immutable receipt remains readable")
	if !reflect.DeepEqual(r, got) {
		t.Fatal("immutable receipt changed")
	}
}
