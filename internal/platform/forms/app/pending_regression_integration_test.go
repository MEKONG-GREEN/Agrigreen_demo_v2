package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type pendingFX293 struct {
	*fx
	pub, leaf  context.Context
	template   uuid.UUID
	kind, unit string
}

func newPending293(t *testing.T, department bool) *pendingFX293 {
	f := newFx(t)
	kind, unit := port.UnitCommune, "U1"
	if department {
		kind, unit = port.UnitDepartment, "trong_trot"
		f.svc.profiles["fx"] = port.Profile{Name: "fx", ResolveActor: fixtureActorResolver, Hierarchy: []string{"TINH", "PHONG"},
			Roles: map[string]string{"tinh": "TINH", "xa": "PHONG"}, PublishBy: "TINH", CloseBy: "TINH", DefineBy: []string{"admin"},
			UnitKinds: []string{kind}, AllowFixtures: true, MaxSubmissions: 3}
	}
	pub := f.as(f.tid, uuid.Nil, "tinh", ids.New(), unit)
	leaf := f.as(f.tid, uuid.Nil, "xa", ids.New(), unit)
	if department {
		leaf = fixtureContext(leaf, mustPrincipal293(leaf), port.ResolvedActor{Role: "xa", Department: unit, Node: ids.New()})
	}
	pub = withMFA293(pub, true)
	leaf = withMFA293(leaf, true)
	ts, err := f.svc.InstallFixtures(f.as(f.tid, uuid.Nil, "admin", uuid.Nil), "fx", "")
	f.must(err, "fixtures")
	installPending293(f)
	return &pendingFX293{f, pub, leaf, ts[0].ID, kind, unit}
}

func mustPrincipal293(ctx context.Context) authz.Principal { p, _ := authz.FromContext(ctx); return p }
func withMFA293(ctx context.Context, on bool) context.Context {
	p := mustPrincipal293(ctx)
	p.MFA = on
	return authz.WithPrincipal(ctx, p)
}
func (f *pendingFX293) query() port.PendingQuery {
	return port.PendingQuery{Profile: "fx", UnitKind: f.kind, Limit: 1}
}
func (f *pendingFX293) publish() uuid.UUID {
	v, err := f.svc.Publish(f.pub, PublishInput{Profile: "fx", TemplateID: f.template, Label: "Pending native", DueAt: f.clk.Now(), Units: []string{f.unit}}, "")
	f.must(err, "publish")
	return v.Period.ID
}
func (f *pendingFX293) submit(pid uuid.UUID) SubmissionView {
	sb, err := f.svc.Submit(f.leaf, "fx", pid, f.unit, fx01, "")
	f.must(err, "submit")
	return sb
}
func (f *pendingFX293) page(q port.PendingQuery) port.PendingPage {
	p, err := (Control{f.svc}).PendingSubmissions(f.pub, q)
	f.must(err, "pending page")
	return p
}
func noPending293(t *testing.T, p port.PendingPage, err, want error) {
	t.Helper()
	matches := errors.Is(err, want)
	var actualProblem, wantProblem *problem.Problem
	if errors.As(want, &wantProblem) {
		matches = errors.As(err, &actualProblem) && actualProblem.Code == wantProblem.Code && actualProblem.Status == wantProblem.Status
	}
	if !matches || !reflect.DeepEqual(p, port.PendingPage{}) {
		t.Fatalf("must fail closed: page=%+v err=%v want=%v", p, err, want)
	}
}

func TestPending293NativeLifecycleAndLockedLimit(t *testing.T) {
	f := newPending293(t, true)
	a, b := f.publish(), f.publish()
	if p := f.page(f.query()); p.Count != 0 || !p.CoverageComplete || len(p.Items) != 0 {
		t.Fatalf("unsent draft counted: %+v", p)
	}
	sa, sb := f.submit(a), f.submit(b)
	if p := f.page(f.query()); p.Count != 2 {
		t.Fatalf("submitted count=%d", p.Count)
	}
	_, err := f.svc.Review(f.pub, "fx", sa.Submission.ID, "accepted", "", "")
	f.must(err, "accept")
	if p := f.page(f.query()); p.Count != 1 {
		t.Fatalf("accepted counted: %+v", p)
	}
	_, err = f.svc.Review(f.pub, "fx", sb.Submission.ID, "returned", "Bổ sung số liệu", "")
	f.must(err, "return")
	if p := f.page(f.query()); p.Count != 0 {
		t.Fatalf("returned counted: %+v", p)
	}
	second := f.submit(b)
	p := f.page(f.query())
	if p.Count != 1 || p.Items[0].SubmissionID != second.Submission.ID || p.Items[0].Attempt != 2 || p.Items[0].Status != "submitted" || p.Items[0].Decision != "" || !p.Items[0].SubmittedAt.Equal(second.Submission.EventTime) {
		t.Fatalf("latest attempt identity/time: %+v", p)
	}
	third := f.submit(b)
	if third.Submission.Version != 3 {
		t.Fatal("fixture did not reach native limit")
	}
	if p := f.page(f.query()); p.Count != 0 {
		t.Fatalf("self-locked 3/3 or superseded submission counted: %+v", p)
	}
	closed := f.publish()
	f.submit(closed)
	_, err = f.svc.Close(f.pub, "fx", closed, "")
	f.must(err, "close")
	_, err = f.svc.SubmitOffline(f.leaf, "fx", closed, f.unit, fx01, f.clk.Now(), "")
	f.must(err, "held offline")
	cancelled := f.publish()
	f.submit(cancelled)
	_, err = f.svc.Cancel(f.pub, "fx", cancelled, "Hủy kỳ kiểm thử", "")
	f.must(err, "cancel")
	if p := f.page(f.query()); p.Count != 0 {
		t.Fatalf("closed/held/cancelled evidence counted: %+v", p)
	}
}

func TestPending293NativeMutationInvalidatesContinuation(t *testing.T) {
	for _, action := range []string{"review", "resubmit", "cancel", "insert"} {
		t.Run(action, func(t *testing.T) {
			f := newPending293(t, false)
			records := map[uuid.UUID]SubmissionView{}
			for range 3 {
				id := f.publish()
				records[id] = f.submit(id)
			}
			first := f.page(f.query())
			if first.Count != 3 || first.NextCursor == "" {
				t.Fatalf("first page=%+v", first)
			}
			var target uuid.UUID
			for id := range records {
				if id != first.Items[0].PeriodID {
					target = id
					break
				}
			}
			switch action {
			case "review":
				_, err := f.svc.Review(f.pub, "fx", records[target].Submission.ID, "returned", "Bổ sung số liệu", "")
				f.must(err, "review")
			case "resubmit":
				f.submit(target)
			case "cancel":
				_, err := f.svc.Cancel(f.pub, "fx", target, "Hủy kỳ kiểm thử", "")
				f.must(err, "cancel")
			case "insert":
				f.submit(f.publish())
			}
			q := f.query()
			q.Cursor = first.NextCursor
			p, err := (Control{f.svc}).PendingSubmissions(f.pub, q)
			noPending293(t, p, err, port.ErrPendingChanged)
			latest := f.page(f.query())
			want := int64(2)
			if action == "resubmit" {
				want = 3
			}
			if action == "insert" {
				want = 4
			}
			if latest.Count != want || latest.Snapshot == first.Snapshot {
				t.Fatalf("mutation did not change snapshot/count: %+v", latest)
			}
		})
	}
}

func TestPending293NativeVisibilityCommitWithoutNewXID(t *testing.T) {
	f := newPending293(t, false)
	for range 2 {
		f.submit(f.publish())
	}
	pool := f.env.AdminPool(t, migrations.PF)
	active, err := pool.Begin(f.ctx)
	f.must(err, "active transaction")
	defer func() { _ = active.Rollback(f.ctx) }()
	var assigned string
	f.must(active.QueryRow(f.ctx, "SELECT pg_current_xact_id()::text").Scan(&assigned), "assign xid before first page")
	// Complete a later XID BEFORE page one so the earlier active XID is in
	// the snapshot's active list and xmax need not advance when it commits.
	probe, e := pool.Begin(f.ctx)
	f.must(e, "visibility probe")
	var probeID string
	f.must(probe.QueryRow(f.ctx, "SELECT pg_current_xact_id()::text").Scan(&probeID), "probe xid")
	f.must(probe.Commit(f.ctx), "probe commit before page one")
	first := f.page(f.query())
	var before, after string
	f.must(pool.QueryRow(f.ctx, "SELECT pg_snapshot_xmax(pg_current_snapshot())::text").Scan(&before), "before xmax")
	f.must(active.Commit(f.ctx), "commit already-visible active xid")
	f.must(pool.QueryRow(f.ctx, "SELECT pg_snapshot_xmax(pg_current_snapshot())::text").Scan(&after), "after xmax")
	if before != after {
		t.Fatal("test requires no new assigned XID between pages")
	}
	q := f.query()
	q.Cursor = first.NextCursor
	page, err := (Control{f.svc}).PendingSubmissions(f.pub, q)
	noPending293(t, page, err, port.ErrPendingChanged)
	if fresh := f.page(f.query()); fresh.Count != first.Count || fresh.Snapshot == first.Snapshot {
		t.Fatal("active-XID commit not reflected conservatively")
	}
}

func TestPending293CurrentAuthorityBeforeCursorAndData(t *testing.T) {
	f := newPending293(t, false)
	for range 2 {
		f.submit(f.publish())
	}
	original := f.svc.PendingPolicies["fx"]
	binding := "live-owner-v1"
	revoked, readOnly := false, false
	caller := mustPrincipal293(f.pub)
	subject := f.pub.Value(fixtureActorKey{}).(port.ResolvedActor)
	var calls int
	f.svc.PendingPolicies["fx"] = func(ctx context.Context, p authz.Principal, q port.PendingQuery) (port.PendingGrant, error) {
		calls++
		a := ctx.Value(fixtureActorKey{}).(port.ResolvedActor)
		if revoked || readOnly || p.ActorRef != caller.ActorRef || p.TenantID != caller.TenantID || a.Role != subject.Role || a.Node != subject.Node {
			return port.PendingGrant{}, problem.Forbidden
		}
		grant, err := original(ctx, p, q)
		grant.Binding = binding
		return grant, err
	}
	first := f.page(f.query())
	q := f.query()
	q.Cursor = first.NextCursor
	spy := &pendingBegin293{Beginner: f.svc.DB}
	f.svc.DB = spy
	cases := []struct {
		name   string
		ctx    context.Context
		change func()
		reset  func()
		want   error
	}{
		{"revoked", f.pub, func() { revoked = true }, func() { revoked = false }, problem.Forbidden},
		{"read-only owner denies", f.pub, func() { readOnly = true }, func() { readOnly = false }, problem.Forbidden},
		{"authority version", f.pub, func() { binding = "live-owner-v2" }, func() { binding = "live-owner-v1" }, port.ErrPendingChanged},
		{"MFA", withMFA293(f.pub, false), func() {}, func() {}, problem.Forbidden},
		{"other tenant", authz.WithPrincipal(f.pub, authz.Principal{ActorRef: caller.ActorRef, TenantID: ids.New(), MFA: true}), func() {}, func() {}, problem.Forbidden},
		{"other actor", authz.WithPrincipal(f.pub, authz.Principal{ActorRef: ids.New(), TenantID: caller.TenantID, MFA: true}), func() {}, func() {}, problem.Forbidden},
		{"other role", fixtureContext(f.pub, caller, port.ResolvedActor{Role: "xa", Scope: subject.Scope, Node: subject.Node}), func() {}, func() {}, problem.Forbidden},
		{"other node", fixtureContext(f.pub, caller, port.ResolvedActor{Role: subject.Role, Scope: subject.Scope, Node: ids.New()}), func() {}, func() {}, problem.Forbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.change()
			defer tc.reset()
			before := spy.begins.Load()
			p, e := (Control{f.svc}).PendingSubmissions(tc.ctx, q)
			noPending293(t, p, e, tc.want)
			if spy.begins.Load() != before {
				t.Fatal("authority failure opened native DB")
			}
		})
	}
	before := calls
	q.Cursor = "malformed"
	p, e := (Control{f.svc}).PendingSubmissions(f.pub, q)
	noPending293(t, p, e, problem.Validation)
	if calls != before+1 || spy.begins.Load() != 0 {
		t.Fatal("cursor parsed/replayed before current owner validation")
	}
	f.svc.PendingPolicies["fx"] = nil
	p, e = (Control{f.svc}).PendingSubmissions(f.pub, f.query())
	noPending293(t, p, e, port.ErrPendingUnavailable)
}

func TestPending293ScopeIntersectionAndConnectionReuse(t *testing.T) {
	f := newPending293(t, false)
	a, b := f.publish(), f.publish()
	f.submit(a)
	f.submit(b)
	q := f.query()
	q.PeriodIDs = []uuid.UUID{a, b}
	q.Units = []string{f.unit}
	q.TemplateCodes = []string{"FX-01"}
	grant := port.PendingGrant{Allowed: true, Complete: true, Binding: "finite-current", Scope: port.PendingScope{PeriodIDs: []uuid.UUID{b}, Units: []string{f.unit}, TemplateCodes: []string{"FX-01"}, OwnerNodes: []uuid.UUID{f.pub.Value(fixtureActorKey{}).(port.ResolvedActor).Node}}}
	f.svc.PendingPolicies["fx"] = func(context.Context, authz.Principal, port.PendingQuery) (port.PendingGrant, error) {
		return grant, nil
	}
	p := f.page(q)
	if p.Count != 1 || p.Items[0].PeriodID != b || p.NextCursor != "" {
		t.Fatalf("exact grant/query intersection=%+v", p)
	}
	// Unordered selectors and nil/empty selectors share canonical fingerprints.
	q.PeriodIDs = []uuid.UUID{b, a}
	again := f.page(q)
	if again.QueryFingerprint != p.QueryFingerprint || again.AuthorityFingerprint != p.AuthorityFingerprint {
		t.Fatal("selector order changed fingerprint")
	}
	grant.Scope.PeriodIDs = []uuid.UUID{}
	if empty := f.page(q); empty.Count != 0 || !empty.CoverageComplete {
		t.Fatalf("proven empty scope not exact zero: %+v", empty)
	}
	grant.Complete = false
	failed, err := (Control{f.svc}).PendingSubmissions(f.pub, q)
	noPending293(t, failed, err, port.ErrPendingUnavailable)
	grant.Complete = true
	grant.Scope.PeriodIDs = []uuid.UUID{a, b}
	// Same pool, tenant B has no rows. Native RLS must not reuse tenant A state.
	other := mustPrincipal293(f.pub)
	other.TenantID = ids.New()
	failed, err = (Control{f.svc}).PendingSubmissions(authz.WithPrincipal(f.pub, other), q)
	f.must(err, "tenant B exact read")
	if failed.Count != 0 || len(failed.Items) != 0 {
		t.Fatal("cross-tenant pending leak")
	}
	if latest := f.page(q); latest.Count != 2 {
		t.Fatal("tenant A context lost after reuse")
	}
	bounded := fixtureContext(f.pub, mustPrincipal293(f.pub), port.ResolvedActor{Role: "tinh", Node: ids.New(), Scope: authz.Scope{Units: []string{"U9"}}})
	failed, err = (Control{f.svc}).PendingSubmissions(bounded, q)
	f.must(err, "native scope intersection")
	if failed.Count != 0 {
		t.Fatal("owner grant bypassed native scope")
	}
}

func TestPending293PoolOneOwnerIOAndRLSCleanup(t *testing.T) {
	f := newPending293(t, false)
	f.submit(f.publish())
	f.submit(f.publish())
	pool := f.env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	spy := &pendingBegin293{Beginner: db.ForModule(pool, DBModule)}
	f.svc.DB = spy
	f.svc.PendingPolicies["fx"] = func(ctx context.Context, p authz.Principal, q port.PendingQuery) (port.PendingGrant, error) {
		if _, joined := db.JoinedTx(ctx); joined {
			t.Error("owner I/O in caller TX")
			return port.PendingGrant{}, port.ErrPendingUnavailable
		}
		err := f.svc.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var role, tid string
			if e := tx.QueryRow(ctx, "SELECT current_role,current_setting('app.tenant_id')").Scan(&role, &tid); e != nil {
				return e
			}
			if role != "r_forms" || tid != p.TenantID.String() {
				t.Error("wrong transaction role/tenant")
			}
			return nil
		})
		return port.PendingGrant{Allowed: true, Complete: true, Binding: "current-read-only-owner", Scope: port.PendingScope{AllPeriods: true, AllUnits: true, AllTemplates: true, AllOwners: true}}, err
	}
	ctx, cancel := context.WithTimeout(f.pub, 15*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			page, e := (Control{f.svc}).PendingSubmissions(ctx, f.query())
			if e != nil || page.Count != 2 {
				t.Errorf("pool1 page=%+v err=%v", page, e)
			}
		})
	}
	wg.Wait()
	if spy.active.Load() != 0 {
		t.Fatal("native reader still active")
	}
	var role, tid string
	f.must(pool.QueryRow(ctx, "SELECT current_role,coalesce(current_setting('app.tenant_id',true),'')").Scan(&role, &tid), "pool1 reused connection")
	if role != "app_platform" || tid != "" {
		t.Fatalf("SET LOCAL leak: role=%s tenant=%s", role, tid)
	}
	// A caller-owned transaction cannot make owner I/O run while it holds pool1.
	f.must(f.svc.tx(ctx, f.tid, func(tx pgx.Tx) error {
		p, e := (Control{f.svc}).PendingSubmissions(db.JoinTx(ctx, tx), f.query())
		noPending293(t, p, e, port.ErrPendingUnavailable)
		return nil
	}), "joined transaction denial")
}

func TestPending293BoundsBeforeOwnerOrDB(t *testing.T) {
	f := newPending293(t, false)
	spy := &pendingBegin293{Beginner: f.svc.DB}
	f.svc.DB = spy
	called := false
	f.svc.PendingPolicies["fx"] = func(context.Context, authz.Principal, port.PendingQuery) (port.PendingGrant, error) {
		called = true
		return port.PendingGrant{}, nil
	}
	q := f.query()
	queries := []port.PendingQuery{}
	for _, cursor := range []string{strings.Repeat("x", 2049), strings.Repeat("x", 1<<20)} {
		v := q
		v.Cursor = cursor
		queries = append(queries, v)
	}
	v := q
	v.PeriodIDs = make([]uuid.UUID, 201)
	queries = append(queries, v)
	v = q
	v.PeriodIDs = []uuid.UUID{ids.New(), uuid.Nil}
	queries = append(queries, v)
	v = q
	v.Units = []string{"U1", "U1"}
	queries = append(queries, v)
	v = q
	v.TemplateCodes = []string{strings.Repeat("X", 100000)}
	queries = append(queries, v)
	v = q
	v.Limit = 201
	queries = append(queries, v)
	v = q
	v.Profile = strings.Repeat("x", 41)
	queries = append(queries, v)
	for _, bad := range queries {
		p, e := (Control{f.svc}).PendingSubmissions(f.pub, bad)
		noPending293(t, p, e, problem.Validation)
	}
	if called || spy.begins.Load() != 0 {
		t.Fatal("unbounded input reached owner or DB")
	}
}

func TestPending293QueryAndCursorTampering(t *testing.T) {
	f := newPending293(t, false)
	for range 2 {
		f.submit(f.publish())
	}
	first := f.page(f.query())
	q := f.query()
	q.Cursor = first.NextCursor
	q.Limit = 2
	p, e := (Control{f.svc}).PendingSubmissions(f.pub, q)
	noPending293(t, p, e, port.ErrPendingChanged)
	cursor, err := decodePendingCursor(first.NextCursor)
	f.must(err, "cursor")
	for _, field := range []string{"authority", "snapshot", "anchor"} {
		c := cursor
		switch field {
		case "authority":
			c.Authority = strings.Repeat("0", 64)
		case "snapshot":
			c.Snapshot = strings.Repeat("0", 64)
		case "anchor":
			c.Period = ids.New()
		}
		raw, err := json.Marshal(c)
		f.must(err, "cursor encode")
		q = f.query()
		q.Cursor = base64.RawURLEncoding.EncodeToString(raw)
		p, e = (Control{f.svc}).PendingSubmissions(f.pub, q)
		noPending293(t, p, e, port.ErrPendingChanged)
	}
}

func TestPending293NativeInvalidCoverageAcrossCandidates(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing period state", true: "duplicate units beyond page"}[duplicate], func(t *testing.T) {
			f := newPending293(t, false)
			f.submit(f.publish())
			err := f.svc.tx(f.pub, f.tid, func(tx pgx.Tx) error {
				units := []string{f.unit}
				if duplicate {
					units = append(units, f.unit)
				}
				p := store.Period{ID: ids.New(), TemplateID: f.template, Label: "Incomplete native", DueAt: f.clk.Now(), Units: units, PublisherLevel: "TINH", ActorRef: ids.New(), EventTime: f.clk.Now()}
				if e := store.InsertPeriod(f.pub, tx, f.tid, &p, "invalid293:"+ids.New().String()); e != nil {
					return e
				}
				if duplicate {
					return store.InsertEvent(f.pub, tx, f.tid, p.ID, "fx", &domain.Event{ID: ids.New(), Action: "published", Mode: port.ModeManual, Actor: p.ActorRef, At: f.clk.Now()}, "event293:"+ids.New().String())
				}
				return nil
			})
			f.must(err, "native malformed fixture without disabling constraints")
			page, e := (Control{f.svc}).PendingSubmissions(f.pub, f.query())
			noPending293(t, page, e, port.ErrPendingUnavailable)
		})
	}
}

type pendingBegin293 struct {
	tenant.Beginner
	begins, active atomic.Int32
}
type pendingTx293 struct {
	pgx.Tx
	owner *pendingBegin293
	once  *sync.Once
}

func (b *pendingBegin293) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	b.begins.Add(1)
	tx, e := b.Beginner.BeginTx(ctx, o)
	if e != nil {
		return nil, e
	}
	b.active.Add(1)
	return pendingTx293{tx, b, &sync.Once{}}, nil
}
func (t pendingTx293) Commit(ctx context.Context) error {
	e := t.Tx.Commit(ctx)
	t.once.Do(func() { t.owner.active.Add(-1) })
	return e
}
func (t pendingTx293) Rollback(ctx context.Context) error {
	e := t.Tx.Rollback(ctx)
	t.once.Do(func() { t.owner.active.Add(-1) })
	return e
}

// Faults are injected only after a real native SELECT at its pgx row boundary.
// No native schema/ledger/check/RLS trigger is disabled or altered.
type pendingFaultBegin293 struct {
	tenant.Beginner
	fault string
}
type pendingFaultTx293 struct {
	pgx.Tx
	fault string
}
type pendingFaultRow293 struct {
	pgx.Row
	fault string
}

func (b pendingFaultBegin293) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, e := b.Beginner.BeginTx(ctx, o)
	if e != nil {
		return nil, e
	}
	return pendingFaultTx293{tx, b.fault}, nil
}
func (t pendingFaultTx293) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	row := t.Tx.QueryRow(ctx, q, args...)
	if strings.HasPrefix(q, "WITH periods AS MATERIALIZED") {
		return pendingFaultRow293{row, t.fault}
	}
	return row
}
func (r pendingFaultRow293) Scan(dest ...any) error {
	if err := r.Row.Scan(dest...); err != nil {
		return err
	}
	switch r.fault {
	case "unknown candidate beyond page":
		*dest[3].(*bool) = true
	case "unknown status":
		raw := dest[5].(*[]byte)
		*raw = []byte(strings.Replace(string(*raw), `"submitted"`, `"unknown"`, 1))
	case "duplicate identity":
		raw := dest[5].(*[]byte)
		var items []port.PendingSubmission
		if e := json.Unmarshal(*raw, &items); e != nil {
			return e
		}
		items = append(items, items[0])
		*raw, _ = json.Marshal(items)
	case "incomplete count":
		*dest[0].(*int64) = 0
	case "server restart visibility":
		*dest[2].(*string) = strings.Repeat("0", 64)
	}
	return nil
}

func TestPending293NativeBoundaryRejectsInvalidEvidence(t *testing.T) {
	f := newPending293(t, false)
	for range 2 {
		f.submit(f.publish())
	}
	first := f.page(f.query())
	original := f.svc.DB
	for _, fault := range []string{"unknown candidate beyond page", "unknown status", "duplicate identity", "incomplete count", "server restart visibility"} {
		t.Run(fault, func(t *testing.T) {
			f.svc.DB = pendingFaultBegin293{original, fault}
			defer func() { f.svc.DB = original }()
			q := f.query()
			want := error(port.ErrPendingUnavailable)
			if fault == "server restart visibility" {
				q.Cursor = first.NextCursor
				want = port.ErrPendingChanged
			}
			p, e := (Control{f.svc}).PendingSubmissions(f.pub, q)
			noPending293(t, p, e, want)
		})
	}
	incomplete := store.PendingResult{Count: 0, Candidates: 0, Visibility: strings.Repeat("0", 64)}
	if !errors.Is(validatePendingResult(incomplete, 1), port.ErrPendingUnavailable) {
		t.Fatal("incomplete coverage became fake zero")
	}
}

func TestPending293SelectedContextFingerprintIsNotSessionIdentity(t *testing.T) {
	f := newPending293(t, false)
	for range 2 {
		f.submit(f.publish())
	}
	p := mustPrincipal293(f.pub)
	subject := f.pub.Value(fixtureActorKey{}).(port.ResolvedActor)
	selected, err := authz.NewSession("fixture", ids.New(), authz.SessionScope{NodeID: subject.Node, RequiresNode: true})
	f.must(err, "selected context")
	selected = selected.WithPayload([]byte(`{"role":"reviewer","version":1}`))
	p.SelectedSession = &selected
	ctx := authz.WithPrincipal(f.pub, p)
	q := f.query()
	first, err := (Control{f.svc}).PendingSubmissions(ctx, q)
	f.must(err, "selected first page")
	q.Cursor = first.NextCursor
	stable, err := (Control{f.svc}).PendingSubmissions(ctx, q)
	f.must(err, "stable selected context")
	if stable.Count != 2 {
		t.Fatal("selected-context stable coverage")
	}
	// Same selected context ID, different owner payload: stamp changes but is
	// not an OIDC session identity or a substitute for current policy calls.
	changed := selected.WithPayload([]byte(`{"role":"reviewer","version":2}`))
	p.SelectedSession = &changed
	denied, e := (Control{f.svc}).PendingSubmissions(authz.WithPrincipal(f.pub, p), q)
	noPending293(t, denied, e, port.ErrPendingChanged)
	zero := authz.Session{}
	p.SelectedSession = &zero
	denied, e = (Control{f.svc}).PendingSubmissions(authz.WithPrincipal(f.pub, p), q)
	noPending293(t, denied, e, problem.Forbidden)
}

func TestPending293OwnerCannotMutateRequestOrSelectedProjection(t *testing.T) {
	f := newPending293(t, false)
	pid := f.publish()
	f.submit(pid)
	principal := mustPrincipal293(f.pub)
	principal.Roles = []string{"authenticated"}
	session, err := authz.NewSession("fixture", ids.New(), authz.SessionScope{})
	f.must(err, "session")
	principal.SelectedSession = &session
	ctx := authz.WithPrincipal(f.pub, principal)
	baseline := f.svc.PendingPolicies["fx"]
	f.svc.PendingPolicies["fx"] = func(ctx context.Context, p authz.Principal, q port.PendingQuery) (port.PendingGrant, error) {
		p.Roles[0] = "changed"
		*p.SelectedSession = authz.Session{}
		q.PeriodIDs[0] = uuid.Nil
		q.Units[0] = "outside"
		q.TemplateCodes[0] = "OTHER"
		return baseline(ctx, p, q)
	}
	q := f.query()
	q.PeriodIDs = []uuid.UUID{pid}
	q.Units = []string{f.unit}
	q.TemplateCodes = []string{"FX-01"}
	page, e := (Control{f.svc}).PendingSubmissions(ctx, q)
	f.must(e, "owner copies")
	if page.Count != 1 || q.PeriodIDs[0] != pid || q.Units[0] != f.unit || q.TemplateCodes[0] != "FX-01" || principal.Roles[0] != "authenticated" || !session.Valid() {
		t.Fatal("owner callback mutated request or principal")
	}
}
