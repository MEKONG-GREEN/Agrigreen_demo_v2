package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type staleControl159 struct{}

func casError159(_ port.PeriodRevision, err error) error { return err }

func TestPeriodControlCAS159_StaleDueAfterSuccessor(t *testing.T) {
	f := newFx(t)
	f.own = db.ForModule(f.env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1), DBModule)
	entered, release := make(chan struct{}), make(chan struct{})
	profile := fxProfile
	profile.ResolveActor = func(ctx context.Context, principal authz.Principal, registered port.Profile) (port.ResolvedActor, bool, error) {
		if _, joined := db.JoinedTx(ctx); joined {
			t.Error("application I/O callback ran inside native transaction")
		}
		if ctx.Value(staleControl159{}) != nil {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return port.ResolvedActor{}, true, ctx.Err()
			}
		}
		// Preserve the ordinary fixture role/owner/scope policy. The callback only
		// delays its existing application-owned actor resolution; it grants nothing.
		return fixtureActorResolver(ctx, principal, registered)
	}
	service, err := NewService(f.own, f.clk, sqlApprovals{}, []port.Profile{profile})
	f.must(err, "service")
	f.svc = service
	node := ids.New()
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	publisher := f.as(f.tid, uuid.Nil, "tinh", node, "U1")
	oldOwner := f.as(f.tid, uuid.Nil, "tinh", node, "U1")
	templates, err := service.InstallFixtures(admin, "fx", "")
	f.must(err, "templates")
	view, err := service.Publish(publisher, PublishInput{Profile: "fx", TemplateID: templates[0].ID, Label: "Control CAS 159", DueAt: f.clk.Now().Add(24 * time.Hour), Units: []string{"U1"}}, "")
	f.must(err, "publish")
	control := Control{Svc: service}
	before, err := control.ControlSnapshot(publisher, "fx", view.Period.ID)
	f.must(err, "snapshot before intent I/O")
	oldDue, nextDue := view.Period.DueAt.Add(time.Hour), view.Period.DueAt.Add(2*time.Hour)
	ctx, cancel := context.WithTimeout(oldOwner, 30*time.Second)
	defer cancel()
	oldResult := make(chan error, 1)
	go func() {
		oldResult <- casError159(control.ChangeDueCAS(context.WithValue(ctx, staleControl159{}, true), "fx", view.Period.ID, before.Revision, oldDue, "old intent", "old159"))
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("old request did not reach callback")
	}
	err = casError159(control.ChangeDueCAS(publisher, "fx", view.Period.ID, before.Revision, nextDue, "successor intent", "next159"))
	close(release)
	f.must(err, "successor commit")
	oldErr := <-oldResult
	current, err := control.Progress(publisher, "fx", view.Period.ID)
	f.must(err, "read current")
	if !isProblem(oldErr, "forms.period_conflict") || !current.DueAt.Equal(nextDue) {
		t.Fatalf("stale native write must be rejected without caller reconciliation: err=%v due=%v successor=%v", oldErr, current.DueAt, nextDue)
	}
}

func controlFixture159(t *testing.T) (*fx, Control, context.Context, uuid.UUID, port.PeriodControlSnapshot) {
	t.Helper()
	f := newFx(t)
	profile := fxProfile
	profile.RequireReviewedBeforeClose = false
	service, err := NewService(f.own, f.clk, sqlApprovals{}, []port.Profile{profile})
	f.must(err, "service")
	f.svc = service
	node := ids.New()
	publisher := f.as(f.tid, uuid.Nil, "tinh", node, "U1")
	templates, err := service.InstallFixtures(f.as(f.tid, uuid.Nil, "admin", uuid.Nil), "fx", "")
	f.must(err, "templates")
	view, err := service.Publish(publisher, PublishInput{Profile: "fx", TemplateID: templates[0].ID, Label: "CAS159", DueAt: f.clk.Now().Add(24 * time.Hour), Units: []string{"U1"}}, "")
	f.must(err, "publish")
	control := Control{Svc: service}
	snapshot, err := control.ControlSnapshot(publisher, "fx", view.Period.ID)
	f.must(err, "initial snapshot")
	if snapshot.Revision.EventSeq != 1 || snapshot.Revision.DueSeq != 0 || snapshot.Revision.TenantID != f.tid || snapshot.Revision.PeriodID != view.Period.ID || snapshot.Revision.Profile != "fx" {
		t.Fatalf("revision is not the native ledger heads: %+v", snapshot)
	}
	return f, control, publisher, view.Period.ID, snapshot
}

func TestPeriodControlCAS159_DueABARejectsEveryMutation(t *testing.T) {
	f, control, ctx, id, initial := controlFixture159(t)
	// A -> B -> A is indistinguishable by status/due values, but not ledger heads.
	f.must(control.ChangeDue(ctx, "fx", id, initial.Progress.DueAt.Add(time.Hour), "legacy B", "legacy159B"), "legacy B")
	f.must(control.ChangeDue(ctx, "fx", id, initial.Progress.DueAt, "legacy A", "legacy159A"), "legacy A")
	current, err := control.ControlSnapshot(ctx, "fx", id)
	f.must(err, "current snapshot")
	if current.Progress.Status != initial.Progress.Status || !current.Progress.DueAt.Equal(initial.Progress.DueAt) || current.Revision.DueSeq != 2 {
		t.Fatalf("not an ABA fixture: initial=%+v current=%+v", initial, current)
	}
	for name, mutate := range map[string]func() error{
		"due": func() error {
			return casError159(control.ChangeDueCAS(ctx, "fx", id, initial.Revision, initial.Progress.DueAt.Add(3*time.Hour), "stale due", "stale159due"))
		},
		"close": func() error {
			return casError159(control.ClosePeriodCAS(ctx, "fx", id, initial.Revision, "stale159close"))
		},
		"cancel": func() error {
			return casError159(control.CancelPeriodCAS(ctx, "fx", id, initial.Revision, "stale cancellation reason", "stale159cancel"))
		},
		"reopen": func() error {
			return casError159(control.ReopenPeriodCAS(ctx, "fx", id, initial.Revision, "stale reopening reason", "stale159reopen"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutate(); !isProblem(err, "forms.period_conflict") {
				t.Fatalf("stale %s: %v", name, err)
			}
		})
	}
	if f.scalar(`SELECT count(*) FROM pf_forms.period_due WHERE tenant_id=$1 AND period_id=$2`, f.tid, id) != 2 || f.scalar(`SELECT count(*) FROM pf_forms.period_event WHERE tenant_id=$1 AND period_ref=$2`, f.tid, id) != 1 || f.scalar(`SELECT count(*) FROM pf_forms.close_snapshot WHERE tenant_id=$1 AND period_ref=$2`, f.tid, id) != 0 || f.scalar(`SELECT count(*) FROM shared.outbox WHERE tenant_id=$1 AND event_type='pf.forms.period.closed'`, f.tid) != 0 {
		t.Fatal("rejected CAS left a due/event/snapshot/outbox write")
	}
	// Reconciliation by a still-owned intent can deliberately adopt a new snapshot.
	f.must(casError159(control.ChangeDueCAS(ctx, "fx", id, current.Revision, initial.Progress.DueAt.Add(4*time.Hour), "current intent", "current159due")), "current CAS")
}

func TestPeriodControlCAS159_StatusABAAndFrozenSnapshots(t *testing.T) {
	f, control, ctx, id, initial := controlFixture159(t)
	f.must(casError159(control.ClosePeriodCAS(ctx, "fx", id, initial.Revision, "close159first")), "close")
	first, err := control.ControlSnapshot(ctx, "fx", id)
	f.must(err, "first closed")
	view, err := f.svc.Period(ctx, "fx", id)
	f.must(err, "first snapshot")
	firstHash := fmt.Sprintf("%x", view.Snapshots[0].Hash)
	firstID := view.Snapshots[0].ID
	f.must(casError159(control.ReopenPeriodCAS(ctx, "fx", id, first.Revision, "explicit native reopening", "reopen159")), "reopen")
	open, err := control.ControlSnapshot(ctx, "fx", id)
	f.must(err, "reopened snapshot")
	f.must(casError159(control.ClosePeriodCAS(ctx, "fx", id, open.Revision, "close159second")), "close again")
	last, err := control.ControlSnapshot(ctx, "fx", id)
	f.must(err, "last closed")
	if first.Progress.Status != "closed" || last.Progress.Status != first.Progress.Status || last.Revision.EventSeq <= first.Revision.EventSeq {
		t.Fatalf("status ABA did not advance real events: %+v %+v", first, last)
	}
	f.want(casError159(control.ReopenPeriodCAS(ctx, "fx", id, first.Revision, "obsolete closed revision", "stale159reopenclosed")), "forms.period_conflict", "status ABA")
	view, err = f.svc.Period(ctx, "fx", id)
	f.must(err, "snapshots unchanged")
	foundFirst := false
	for _, snapshot := range view.Snapshots {
		if snapshot.ID == firstID {
			foundFirst = fmt.Sprintf("%x", snapshot.Hash) == firstHash
		}
	}
	if len(view.Snapshots) != 2 || !foundFirst {
		t.Fatal("CAS rejection changed a frozen snapshot")
	}
	if f.scalar(`SELECT count(*) FROM shared.outbox WHERE tenant_id=$1 AND event_type='pf.forms.period.closed'`, f.tid) != 2 {
		t.Fatal("CAS rejection emitted a duplicate close event")
	}
}

func TestPeriodControlCAS159_ConcurrentWritersOneCommit(t *testing.T) {
	f, control, ctx, id, before := controlFixture159(t)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for n := 0; n < 2; n++ {
		workers.Add(1)
		go func(n int) {
			defer workers.Done()
			<-start
			results <- casError159(control.ChangeDueCAS(ctx, "fx", id, before.Revision, before.Progress.DueAt.Add(time.Duration(n+1)*time.Hour), "concurrent intent", fmt.Sprintf("concurrent159-%d", n)))
		}(n)
	}
	close(start)
	workers.Wait()
	close(results)
	ok, conflict := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else if isProblem(err, "forms.period_conflict") {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflict != 1 || f.scalar(`SELECT count(*) FROM pf_forms.period_due WHERE tenant_id=$1 AND period_id=$2`, f.tid, id) != 1 {
		t.Fatalf("lost native CAS: success=%d conflict=%d", ok, conflict)
	}
}

func TestPeriodControlCAS159_ChainsOwnCommittedRevision(t *testing.T) {
	f, control, ctx, id, before := controlFixture159(t)
	closed, err := control.ClosePeriodCAS(ctx, "fx", id, before.Revision, "chain159close")
	f.must(err, "close result")
	if closed.EventSeq != 2 || closed.DueSeq != 0 {
		t.Fatalf("close did not return its own heads: %+v", closed)
	}
	reopened, err := control.ReopenPeriodCAS(ctx, "fx", id, closed, "chain reopening reason", "chain159reopen")
	f.must(err, "reopen result")
	if reopened.EventSeq != 3 || reopened.DueSeq != 0 {
		t.Fatalf("reopen did not return its own heads: %+v", reopened)
	}
	// A successor commits between two native calls of the old request. The old
	// request retains its OWN prior result, never a fresh snapshot of the successor.
	nextDue := before.Progress.DueAt.Add(2 * time.Hour)
	successor, err := control.ChangeDueCAS(ctx, "fx", id, reopened, nextDue, "successor chain", "chain159successor")
	f.must(err, "successor result")
	if successor.EventSeq != 3 || successor.DueSeq != 1 {
		t.Fatalf("due did not return actual ledger heads: %+v", successor)
	}
	failed, err := control.ChangeDueCAS(ctx, "fx", id, reopened, before.Progress.DueAt.Add(time.Hour), "old second step", "chain159old")
	f.want(err, "forms.period_conflict", "old second native step")
	if failed != (port.PeriodRevision{}) {
		t.Fatal("failed CAS exposed a usable revision")
	}
	view, err := control.ControlSnapshot(ctx, "fx", id)
	f.must(err, "read successor")
	if view.Revision != successor || !view.Progress.DueAt.Equal(nextDue) {
		t.Fatalf("old chain overwrote successor: %+v", view)
	}
}

type uncertainCommit159 struct{}
type uncertainBeginner159 struct{ tenant.Beginner }
type uncertainTx159 struct{ pgx.Tx }

func (b uncertainBeginner159) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.Beginner.BeginTx(ctx, options)
	if err != nil || ctx.Value(uncertainCommit159{}) == nil {
		return tx, err
	}
	return uncertainTx159{tx}, nil
}

func (tx uncertainTx159) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	// Deterministic transport fault: PostgreSQL committed, but the caller lost
	// the acknowledgement. No rollback assumption or reconciliation is supplied.
	return context.DeadlineExceeded
}

func TestPeriodControlCAS159_UncertainCommitCannotReplayOverSuccessor(t *testing.T) {
	f, control, ctx, id, before := controlFixture159(t)
	copyService := *control.Svc
	copyService.DB = uncertainBeginner159{copyService.DB}
	uncertain := Control{Svc: &copyService}
	oldCtx := context.WithValue(ctx, uncertainCommit159{}, true)
	result, err := uncertain.ChangeDueCAS(oldCtx, "fx", id, before.Revision, before.Progress.DueAt.Add(time.Hour), "lost acknowledgement", "unknown159first")
	if !errors.Is(err, context.DeadlineExceeded) || result != (port.PeriodRevision{}) {
		t.Fatalf("uncertain commit exposed success: %+v %v", result, err)
	}
	committed, err := control.ControlSnapshot(ctx, "fx", id)
	f.must(err, "read committed despite lost response")
	if committed.Revision.DueSeq != 1 {
		t.Fatalf("fault did not commit native write: %+v", committed)
	}
	nextDue := before.Progress.DueAt.Add(2 * time.Hour)
	_, err = control.ChangeDueCAS(ctx, "fx", id, committed.Revision, nextDue, "successor after uncertain result", "unknown159next")
	f.must(err, "successor")
	result, err = uncertain.ChangeDueCAS(oldCtx, "fx", id, before.Revision, before.Progress.DueAt.Add(time.Hour), "lost acknowledgement", "unknown159first")
	f.want(err, "forms.period_conflict", "late replay after successor")
	if result != (port.PeriodRevision{}) {
		t.Fatal("rejected replay returned usable revision")
	}
	last, err := control.ControlSnapshot(ctx, "fx", id)
	f.must(err, "read last")
	if last.Revision.DueSeq != 2 || !last.Progress.DueAt.Equal(nextDue) {
		t.Fatalf("late replay changed successor: %+v", last)
	}
}

type delayedAck159 struct{}
type delayedAckBeginner159 struct {
	tenant.Beginner
	committed, release chan struct{}
}
type delayedAckTx159 struct {
	pgx.Tx
	committed, release chan struct{}
}

func (b delayedAckBeginner159) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.Beginner.BeginTx(ctx, options)
	if err != nil || ctx.Value(delayedAck159{}) == nil {
		return tx, err
	}
	return delayedAckTx159{tx, b.committed, b.release}, nil
}
func (tx delayedAckTx159) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	close(tx.committed)
	select {
	case <-tx.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestPeriodControlCAS159_AppliedRevisionBelongsToOwnCommit(t *testing.T) {
	f, control, ctx, id, before := controlFixture159(t)
	control.Svc.DB = db.ForModule(f.env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1), DBModule)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	committed, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	copyService := *control.Svc
	copyService.DB = delayedAckBeginner159{copyService.DB, committed, release}
	delayed := Control{Svc: &copyService}
	type writeResult struct {
		revision port.PeriodRevision
		err      error
	}
	old := make(chan writeResult, 1)
	go func() {
		revision, err := delayed.ChangeDueCAS(context.WithValue(ctx, delayedAck159{}, true), "fx", id, before.Revision, before.Progress.DueAt.Add(time.Hour), "old commit delayed ack", "ack159old")
		old <- writeResult{revision, err}
	}()
	select {
	case <-committed:
	case <-ctx.Done():
		t.Fatal("old native write did not commit")
	}
	// The old caller is still waiting for ACK, but the transaction and its pool1
	// connection are already released. A successor can really commit meanwhile.
	first, err := control.ControlSnapshot(ctx, "fx", id)
	f.must(err, "own native commit")
	nextDue := before.Progress.DueAt.Add(2 * time.Hour)
	next, err := control.ChangeDueCAS(ctx, "fx", id, first.Revision, nextDue, "successor before old ack", "ack159next")
	f.must(err, "successor commit")
	unblock()
	result := <-old
	f.must(result.err, "old acknowledgement")
	if result.revision != first.Revision || result.revision == next {
		t.Fatalf("old caller received a fresh successor token: own=%+v result=%+v successor=%+v", first.Revision, result.revision, next)
	}
	_, err = control.ChangeDueCAS(ctx, "fx", id, result.revision, before.Progress.DueAt.Add(3*time.Hour), "old chain after late ack", "ack159stale")
	f.want(err, "forms.period_conflict", "old chain retains its own revision")
}

type snapshotPause159 struct{}
type writerPID159 struct{}
type snapshotBeginner159 struct {
	tenant.Beginner
	entered, release chan struct{}
	writerPID        chan int32
}
type snapshotTx159 struct {
	pgx.Tx
	entered, release chan struct{}
}

func (b snapshotBeginner159) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.Beginner.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	if ctx.Value(writerPID159{}) != nil {
		var pid int32
		if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		b.writerPID <- pid
	}
	if ctx.Value(snapshotPause159{}) != nil {
		return snapshotTx159{tx, b.entered, b.release}, nil
	}
	return tx, nil
}

func (tx snapshotTx159) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "FROM pf_forms.period_event") && strings.Contains(sql, "FROM pf_forms.period_due") {
		// Pause after the progress data was read but before its revision is read.
		close(tx.entered)
		select {
		case <-tx.release:
		case <-ctx.Done():
		}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func TestPeriodControlCAS159_SnapshotDataAndRevisionStayAtomic(t *testing.T) {
	f, control, ctx, id, before := controlFixture159(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	entered, release, pid := make(chan struct{}), make(chan struct{}), make(chan int32, 1)
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	copyService := *control.Svc
	copyService.DB = snapshotBeginner159{copyService.DB, entered, release, pid}
	scheduled := Control{Svc: &copyService}
	type readResult struct {
		value port.PeriodControlSnapshot
		err   error
	}
	read := make(chan readResult, 1)
	go func() {
		value, err := scheduled.ControlSnapshot(context.WithValue(ctx, snapshotPause159{}, true), "fx", id)
		read <- readResult{value, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("snapshot did not reach controlled read point")
	}
	write := make(chan error, 1)
	nextDue := before.Progress.DueAt.Add(time.Hour)
	go func() {
		write <- casError159(scheduled.ChangeDueCAS(context.WithValue(ctx, writerPID159{}, true), "fx", id, before.Revision, nextDue, "writer during snapshot", "atomic159"))
	}()
	var writerPID int32
	select {
	case writerPID = <-pid:
	case <-ctx.Done():
		t.Fatal("writer did not acquire its connection")
	}
	// Synchronize using PostgreSQL's real wait state, not a sleep-based ordering.
	for {
		var blocked bool
		err := f.env.AdminPool(t, migrations.PF).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory' AND NOT granted)`, writerPID).Scan(&blocked)
		f.must(err, "writer advisory wait")
		if blocked {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("writer bypassed snapshot period lock")
		case <-time.After(20 * time.Millisecond):
		}
	}
	unblock()
	seen := <-read
	f.must(seen.err, "snapshot")
	if seen.value.Revision != before.Revision || !seen.value.Progress.DueAt.Equal(before.Progress.DueAt) {
		t.Fatalf("mixed snapshot data/revision: before=%+v seen=%+v", before, seen.value)
	}
	f.must(<-write, "writer after snapshot commit")
	last, err := control.ControlSnapshot(ctx, "fx", id)
	f.must(err, "read after writer")
	if last.Revision.DueSeq != 1 || !last.Progress.DueAt.Equal(nextDue) {
		t.Fatalf("writer did not commit after snapshot: %+v", last)
	}
}

func TestPeriodControlCAS159_IdentityBoundsAndPolicy(t *testing.T) {
	f, control, ctx, id, before := controlFixture159(t)
	for name, change := range map[string]func(*port.PeriodRevision){
		"zero":           func(v *port.PeriodRevision) { *v = port.PeriodRevision{} },
		"tenant":         func(v *port.PeriodRevision) { v.TenantID = ids.New() },
		"period":         func(v *port.PeriodRevision) { v.PeriodID = ids.New() },
		"profile":        func(v *port.PeriodRevision) { v.Profile = "other" },
		"negative event": func(v *port.PeriodRevision) { v.EventSeq = -1 },
		"zero event":     func(v *port.PeriodRevision) { v.EventSeq = 0 },
		"negative due":   func(v *port.PeriodRevision) { v.DueSeq = -1 },
		"large":          func(v *port.PeriodRevision) { v.EventSeq = 1<<63 - 1 },
		"large due":      func(v *port.PeriodRevision) { v.DueSeq = 1<<63 - 1 },
	} {
		t.Run(name, func(t *testing.T) {
			expected := before.Revision
			change(&expected)
			f.want(casError159(control.ClosePeriodCAS(ctx, "fx", id, expected, "bad159-"+name)), "forms.period_conflict", "bad revision")
		})
	}
	leaf := f.as(f.tid, uuid.Nil, "xa", ids.New(), "U1")
	f.want(casError159(control.ClosePeriodCAS(leaf, "fx", id, before.Revision, "forbidden159")), "forbidden", "no control permission")
	other := f.as(ids.New(), uuid.Nil, "admin", uuid.Nil)
	_, err := control.ControlSnapshot(other, "fx", id)
	f.want(err, "forms.period_not_found", "cross tenant snapshot")
	f.want(casError159(control.ChangeDueCAS(other, "fx", id, before.Revision, before.Progress.DueAt.Add(time.Hour), "other tenant", "other159")), "forms.period_not_found", "cross tenant CAS")
	f.must(casError159(control.CancelPeriodCAS(ctx, "fx", id, before.Revision, "current owner cancels period", "cancel159")), "current cancel")
	last, err := control.ControlSnapshot(ctx, "fx", id)
	f.must(err, "cancelled snapshot")
	if last.Progress.Status != "cancelled" || last.Revision.EventSeq != 2 {
		t.Fatalf("cancellation did not commit: %+v", last)
	}
}
