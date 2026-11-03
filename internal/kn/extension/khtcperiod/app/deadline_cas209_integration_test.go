package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/jackc/pgx/v5"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	khtcpapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	fixture "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/khtcdeadline209"
)

type casProbe209 struct {
	formsport.ConditionalPeriodControl
	snapshotHook func(context.Context, formsport.PeriodControlSnapshot)
	changeHook   func(context.Context, int, formsport.PeriodRevision)
	afterHook    func(int, formsport.PeriodRevision, error) (formsport.PeriodRevision, error)
	mu           sync.Mutex
	calls        int
}

func (p *casProbe209) ControlSnapshot(ctx context.Context, profile string, id uuid.UUID) (formsport.PeriodControlSnapshot, error) {
	s, e := p.ConditionalPeriodControl.ControlSnapshot(ctx, profile, id)
	if e == nil && p.snapshotHook != nil {
		p.snapshotHook(ctx, s)
	}
	return s, e
}
func (p *casProbe209) ChangeDueCAS(ctx context.Context, profile string, id uuid.UUID, expected formsport.PeriodRevision, due time.Time, reason, key string) (formsport.PeriodRevision, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	p.mu.Unlock()
	if p.changeHook != nil {
		p.changeHook(ctx, n, expected)
	}
	r, e := p.ConditionalPeriodControl.ChangeDueCAS(ctx, profile, id, expected, due, reason, key)
	if p.afterHook != nil {
		return p.afterHook(n, r, e)
	}
	return r, e
}
func (p *casProbe209) count() int { p.mu.Lock(); defer p.mu.Unlock(); return p.calls }
func (f *deadlineFx209) consumer209(control formsport.ConditionalPeriodControl) (*khtcpapp.Service, context.Context) {
	svc := &khtcpapp.Service{DB: f.roleDB, Clock: f.clk, Forms: f.fm.Publisher(), Control: f.fm.Control(), ConditionalControl: control, MaxSubmissions: 3}
	p := fixture.Bind(f.t, authz.Principal{ActorRef: f.actor, TenantID: f.tid, KN: &authz.KNContext{ID: uuid.NewSHA1(f.tid, []byte("deadline209")), Role: authz.KNTinh, Department: khtcDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1"}}}})
	return svc, authz.WithPrincipal(context.Background(), p)
}
func mustChange209(t *testing.T, s *khtcpapp.Service, ctx context.Context, p khtcPeriod, due time.Time, key string) khtcpapp.Detail {
	t.Helper()
	d, e := s.ChangeDeadline(ctx, uuid.MustParse(p.ID), khtcpapp.DeadlineInput{DueAt: due, Reason: "Sở thay đổi hạn nộp báo cáo"}, key)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func assertConflict209(t *testing.T, e error) {
	t.Helper()
	var p *problem.Problem
	if !errors.As(e, &p) || p.Status != 409 {
		t.Fatalf("want409, got%v", e)
	}
}
func (f *deadlineFx209) ownPhase209(t *testing.T) string {
	t.Helper()
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	var phase string
	if e := admin.QueryRow(context.Background(), `SELECT phase FROM pf_extension.khtc_deadline_intent WHERE tenant_id=$1 ORDER BY seq DESC LIMIT 1`, f.tid).Scan(&phase); e != nil {
		t.Fatal(e)
	}
	return phase
}

func TestDeadline209StoredReplayAndSuccessor(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	probe := &casProbe209{ConditionalPeriodControl: f.fm.ConditionalControl()}
	s, ctx := f.consumer209(probe)
	first := mustChange209(t, s, ctx, p, p.DueAt.Add(2*time.Hour), "first209")
	if !first.DueAt.Equal(p.DueAt.Add(2*time.Hour)) || first.Overdue || len(first.Events) != 1 || first.Events[0].Seq < 1 {
		t.Fatalf("first outcome projection: %+v", first)
	}
	f.clk.Advance(48 * time.Hour)
	second := mustChange209(t, s, ctx, p, p.DueAt.Add(72*time.Hour), "second209")
	if !second.DueAt.After(first.DueAt) || second.Overdue || len(second.Events) != 2 || second.Events[1].Seq <= first.Events[0].Seq {
		t.Fatalf("successor projection: %+v", second)
	}
	replay := mustChange209(t, s, ctx, p, p.DueAt.Add(2*time.Hour), "first209")
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(replay)
	if string(a) != string(b) || probe.count() != 2 {
		t.Fatalf("not exact stored operation replay: first%s replay%s calls%d", a, b, probe.count())
	}
	_, e := s.ChangeDeadline(ctx, uuid.MustParse(p.ID), khtcpapp.DeadlineInput{DueAt: p.DueAt.Add(99 * time.Hour)}, "first209")
	assertConflict209(t, e)
	before, _ := authz.FromContext(ctx)
	changed := before
	copyKN := *before.KN
	copyKN.ViewOnly = true
	changed.KN = &copyKN
	changed = fixture.Bind(t, changed)
	_, e = s.ChangeDeadline(authz.WithPrincipal(context.Background(), changed), uuid.MustParse(p.ID), khtcpapp.DeadlineInput{DueAt: p.DueAt.Add(2 * time.Hour), Reason: "Sở thay đổi hạn nộp báo cáo"}, "first209")
	var pr *problem.Problem
	if !errors.As(e, &pr) || pr.Status != 403 {
		t.Fatalf("new readonly request replay%v", e)
	}
}

func TestDeadline209StaleAndDueABA(t *testing.T) {
	for _, aba := range []bool{false, true} {
		t.Run(fmt.Sprint("ABA=", aba), func(t *testing.T) {
			f := newDeadlineFx209(t)
			p := f.open209()
			native := f.fm.ConditionalControl()
			probe := &casProbe209{ConditionalPeriodControl: native}
			s, ctx := f.consumer209(probe)
			probe.snapshotHook = func(c context.Context, snapshot formsport.PeriodControlSnapshot) {
				applied, e := native.ChangeDueCAS(c, "kn_khtc", uuid.MustParse(p.FormsPeriodID), snapshot.Revision, p.DueAt.Add(6*time.Hour), "successor209", "successor209")
				if e != nil {
					t.Fatal(e)
				}
				if aba {
					_, e = native.ChangeDueCAS(c, "kn_khtc", uuid.MustParse(p.FormsPeriodID), applied, p.DueAt, "ABA successor209", "aba209")
					if e != nil {
						t.Fatal(e)
					}
				}
			}
			_, e := s.ChangeDeadline(ctx, uuid.MustParse(p.ID), khtcpapp.DeadlineInput{DueAt: p.DueAt.Add(time.Hour)}, "stale209")
			assertConflict209(t, e)
			if f.ownPhase209(t) != "unresolved" {
				t.Fatal("stale revision not retained")
			}
			_, e = s.ChangeDeadline(ctx, uuid.MustParse(p.ID), khtcpapp.DeadlineInput{DueAt: p.DueAt.Add(time.Hour)}, "stale209")
			assertConflict209(t, e)
			if probe.count() != 1 {
				t.Fatalf("stale retry called native again: %d", probe.count())
			}
		})
	}
}

func TestDeadline209LostACKAndBlockedNewIntent(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	probe := &casProbe209{ConditionalPeriodControl: f.fm.ConditionalControl()}
	probe.afterHook = func(_ int, r formsport.PeriodRevision, e error) (formsport.PeriodRevision, error) {
		if e != nil {
			return r, e
		}
		return formsport.PeriodRevision{}, errors.New("synthetic lost ACK after native commit")
	}
	s, ctx := f.consumer209(probe)
	in := khtcpapp.DeadlineInput{DueAt: p.DueAt.Add(time.Hour)}
	_, e := s.ChangeDeadline(ctx, uuid.MustParse(p.ID), in, "lost209")
	if e == nil {
		t.Fatal("lost ACK claimed success")
	}
	_, e = s.ChangeDeadline(ctx, uuid.MustParse(p.ID), in, "lost209")
	assertConflict209(t, e)
	_, e = s.ChangeDeadline(ctx, uuid.MustParse(p.ID), in, "new209")
	assertConflict209(t, e)
	if probe.count() != 1 || f.ownPhase209(t) != "unresolved" {
		t.Fatalf("uncertain request retried/forgotten: %d", probe.count())
	}
	actual, e := f.fm.ConditionalControl().ControlSnapshot(ctx, "kn_khtc", uuid.MustParse(p.FormsPeriodID))
	if e != nil || !actual.Progress.DueAt.Equal(in.DueAt) {
		t.Fatalf("native commit missing: %+v %v", actual, e)
	}
}

// Admin-only deterministic failures are scoped to this synthetic tenant, never
// production hooks or edits to nativeForms. All trigger DDL is removed by cleanup.
func (f *deadlineFx209) failOwnWrite209(t *testing.T, table, condition string) {
	t.Helper()
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	name := "reject209_" + fmt.Sprintf("%x", time.Now().UnixNano())
	fn := `CREATE FUNCTION pf_extension.` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.tenant_id='` + f.tid.String() + `'::uuid AND ` + condition + ` THEN RAISE EXCEPTION 'synthetic own failure209' USING ERRCODE='23514'; END IF; RETURN NEW;END $$;`
	if _, e := admin.Exec(context.Background(), fn); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(context.Background(), `CREATE TRIGGER `+name+` BEFORE INSERT ON pf_extension.`+table+` FOR EACH ROW EXECUTE FUNCTION pf_extension.`+name+`() `); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP TRIGGER `+name+` ON pf_extension.`+table+`; DROP FUNCTION pf_extension.`+name+`() `)
	})
}

func TestDeadline209ConditionalUndoAndUncertainPersistence(t *testing.T) {
	for _, mode := range []string{"undo", "successorBlocksUndo", "persistApplied"} {
		t.Run(mode, func(t *testing.T) {
			f := newDeadlineFx209(t)
			p := f.open209()
			native := f.fm.ConditionalControl()
			probe := &casProbe209{ConditionalPeriodControl: native}
			s, ctx := f.consumer209(probe)
			if mode == "persistApplied" {
				f.failOwnWrite209(t, "khtc_deadline_intent", "NEW.phase='applied'")
			} else {
				f.failOwnWrite209(t, "khtc_period_event", "NEW.action='deadline_changed'")
			}
			var original, applied formsport.PeriodRevision
			probe.changeHook = func(c context.Context, n int, expected formsport.PeriodRevision) {
				if n == 1 {
					original = expected
				}
				if n == 2 && !reflect.DeepEqual(expected, applied) {
					t.Fatalf("undo did not use own returned applied revision: %+v vs%+v", expected, applied)
				}
				if n == 2 && mode == "successorBlocksUndo" {
					_, e := native.ChangeDueCAS(c, "kn_khtc", uuid.MustParse(p.FormsPeriodID), expected, p.DueAt.Add(8*time.Hour), "successor before undo209", "beforeundo209")
					if e != nil {
						t.Fatal(e)
					}
				}
			}
			probe.afterHook = func(n int, r formsport.PeriodRevision, e error) (formsport.PeriodRevision, error) {
				if n == 1 && e == nil {
					applied = r
				}
				return r, e
			}
			_, e := s.ChangeDeadline(ctx, uuid.MustParse(p.ID), khtcpapp.DeadlineInput{DueAt: p.DueAt.Add(time.Hour)}, "undo209")
			if e == nil {
				t.Fatal("local failure claimed success")
			}
			phase := f.ownPhase209(t)
			if mode == "undo" {
				if phase != "undone" || probe.count() != 2 || reflect.DeepEqual(original, applied) {
					t.Fatalf("undo proof: phase%s calls%d", phase, probe.count())
				}
			}
			if mode == "persistApplied" && probe.count() != 1 {
				t.Fatal("blind undo after failed applied persistence")
			}
			if mode == "successorBlocksUndo" && phase != "unresolved" {
				t.Fatalf("undo overwrote successor: %s", phase)
			}
			_, e = s.ChangeDeadline(ctx, uuid.MustParse(p.ID), khtcpapp.DeadlineInput{DueAt: p.DueAt.Add(time.Hour)}, "undo209")
			assertConflict209(t, e)
		})
	}
}

const khtcDept = "ke_hoach_tai_chinh"

type khtcPeriod struct {
	ID, FormsPeriodID string
	DueAt             time.Time
}
type deadlineFx209 struct {
	t                *testing.T
	fm               *fixture.Native
	roleDB           tenant.Beginner
	clk              *clock.Fixed
	tid, node, actor uuid.UUID
}

func newDeadlineFx209(t *testing.T) *deadlineFx209 {
	n := fixture.New(t)
	return &deadlineFx209{t: t, fm: n, roleDB: n.DB, clk: n.Clock, tid: uuid.New(), node: uuid.New(), actor: uuid.New()}
}
func (f *deadlineFx209) open209() khtcPeriod {
	s, ctx := f.consumer209(f.fm.ConditionalControl())
	admin := fixture.Bind(f.t, authz.Principal{ActorRef: f.actor, TenantID: f.tid, MFA: true, KN: &authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}}})
	if _, e := s.InstallTemplate(authz.WithPrincipal(context.Background(), admin), "install209"); e != nil {
		f.t.Fatal(e)
	}
	d, e := s.Open(ctx, khtcpapp.OpenInput{Request: domain.Request{Type: domain.TypeWeek, Year: 2026, No: 41}, Departments: []string{"trong_trot"}}, "open209")
	if e != nil {
		f.t.Fatal(e)
	}
	return khtcPeriod{ID: d.Period.ID.String(), FormsPeriodID: d.Period.FormsPeriodRef.String(), DueAt: d.DueAt}
}

// Unknown local commit outcome must never take the known-rollback undo branch.
type localACK209 struct {
	tenant.Beginner
	mode string
}
type localACKTx209 struct {
	pgx.Tx
	mode  string
	final bool
}

func (b localACK209) BeginTx(c context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	x, e := b.Beginner.BeginTx(c, o)
	if e != nil {
		return nil, e
	}
	return &localACKTx209{Tx: x, mode: b.mode}, nil
}
func (x *localACKTx209) QueryRow(c context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "INSERT INTO pf_extension.khtc_period_event") {
		x.final = true
	}
	return x.Tx.QueryRow(c, sql, args...)
}
func (x *localACKTx209) Commit(c context.Context) error {
	if !x.final {
		return x.Tx.Commit(c)
	}
	if x.mode == "commitLostACK" {
		if e := x.Tx.Commit(c); e != nil {
			return e
		}
	} else {
		if e := x.Tx.Rollback(c); e != nil {
			return e
		}
	}
	return errors.New("synthetic unknown LOCAL commit ACK209")
}
func TestDeadline209UnknownLocalCommitNoUndo(t *testing.T) {
	for _, mode := range []string{"commitLostACK", "rollbackUnknown"} {
		t.Run(mode, func(t *testing.T) {
			f := newDeadlineFx209(t)
			p := f.open209()
			probe := &casProbe209{ConditionalPeriodControl: f.fm.ConditionalControl()}
			s, ctx := f.consumer209(probe)
			s.DB = localACK209{Beginner: s.DB, mode: mode}
			in := khtcpapp.DeadlineInput{DueAt: p.DueAt.Add(time.Hour)}
			_, e := s.ChangeDeadline(ctx, uuid.MustParse(p.ID), in, "localack209")
			assertConflict209(t, e)
			if probe.count() != 1 {
				t.Fatalf("unknown LOCAL commit triggered undo:%d", probe.count())
			}
			phase := f.ownPhase209(t)
			out, e := s.ChangeDeadline(ctx, uuid.MustParse(p.ID), in, "localack209")
			if mode == "commitLostACK" {
				if phase != "completed" || e != nil || !out.DueAt.Equal(in.DueAt) {
					t.Fatalf("stored completion lost: phase%s outcome%+v error%v", phase, out, e)
				}
			} else {
				assertConflict209(t, e)
				if phase != "unresolved" {
					t.Fatalf("rollback/unknown not blocked:%s", phase)
				}
			}
			if probe.count() != 1 {
				t.Fatal("replay dispatched native after unknown local ACK")
			}
		})
	}
}

// Transactions can start before their predecessor commits. now()/recorded_at
// then moves backwards relative to stream seq, without a clock hook or UPDATE.
// Clone valid consumer-produced metadata as a store fixture; this test proves
// phase selection, not a second business operation's native receipt.
func TestDeadline209LatestPhaseFollowsSeq(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	s, ctx := f.consumer209(f.fm.ConditionalControl())
	_ = mustChange209(t, s, ctx, p, p.DueAt.Add(time.Hour), "clocksource209")
	oldKey, _ := formsport.OperationHash("clocksource209")
	var row *store.DeadlineIntent
	e := tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error { var err error; row, err = store.DeadlineByKey(ctx, tx, oldKey); return err })
	if e != nil || row == nil || row.Phase != "completed" {
		t.Fatalf("source intent:%+v %v", row, e)
	}
	row.OperationID = uuid.New()
	row.RequestKey, _ = formsport.OperationHash("clockfixture209")
	prepared := *row
	prepared.ID = uuid.New()
	prepared.Phase = "prepared"
	prepared.Outcome = nil
	var payload map[string]json.RawMessage
	if e = json.Unmarshal(row.Payload, &payload); e != nil {
		t.Fatal(e)
	}
	delete(payload, "applied")
	delete(payload, "undo_applied")
	prepared.Payload, e = json.Marshal(payload)
	if e != nil {
		t.Fatal(e)
	}
	startedA, startedC, preparedDone, appliedDone := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	errorsA, errorsC := make(chan error, 1), make(chan error, 1)
	go func() {
		defer close(appliedDone)
		errorsA <- tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
			close(startedA)
			<-preparedDone
			v := *row
			v.ID = uuid.New()
			v.Phase = "applied"
			v.Outcome = nil
			return store.InsertDeadline(ctx, tx, f.tid, v)
		})
	}()
	<-startedA
	go func() {
		errorsC <- tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
			close(startedC)
			<-appliedDone
			v := *row
			v.ID = uuid.New()
			return store.InsertDeadline(ctx, tx, f.tid, v)
		})
	}()
	<-startedC
	e = tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error { return store.InsertDeadline(ctx, tx, f.tid, prepared) })
	close(preparedDone)
	if e != nil {
		t.Fatal(e)
	}
	if e = <-errorsA; e != nil {
		t.Fatal(e)
	}
	if e = <-errorsC; e != nil {
		t.Fatal(e)
	}
	var first, last time.Time
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	if e = admin.QueryRow(ctx, `SELECT (array_agg(recorded_at ORDER BY seq))[1],(array_agg(recorded_at ORDER BY seq DESC))[1] FROM pf_extension.khtc_deadline_intent WHERE tenant_id=$1 AND operation_id=$2`, f.tid, row.OperationID).Scan(&first, &last); e != nil {
		t.Fatal(e)
	}
	if !first.After(last) {
		t.Fatalf("fixture not nonmonotonic: first%v last%v", first, last)
	}
	var latest *store.DeadlineIntent
	e = tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
		var err error
		latest, err = store.DeadlineByKey(ctx, tx, row.RequestKey)
		return err
	})
	if e != nil || latest == nil || latest.Phase != "completed" {
		t.Fatalf("latest committed phase must follow seq, not recorded_at: %+v error%v", latest, e)
	}
}

func TestDeadline209PoolOneAndPendingConcurrency(t *testing.T) {
	t.Run("same native/consumer pool of one", func(t *testing.T) {
		n := fixture.NewSized(t, 1)
		f := &deadlineFx209{t: t, fm: n, roleDB: n.DB, clk: n.Clock, tid: uuid.New(), node: uuid.New(), actor: uuid.New()}
		p := f.open209()
		s, ctx := f.consumer209(n.ConditionalControl())
		ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		d := mustChange209(t, s, ctx, p, p.DueAt.Add(time.Hour), "poolone209")
		if !d.DueAt.Equal(p.DueAt.Add(time.Hour)) {
			t.Fatal("wrong due on pool1")
		}
	})
	t.Run("prepared intent excludes another request", func(t *testing.T) {
		f := newDeadlineFx209(t)
		p := f.open209()
		probe := &casProbe209{ConditionalPeriodControl: f.fm.ConditionalControl()}
		s, ctx := f.consumer209(probe)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		probe.changeHook = func(_ context.Context, n int, _ formsport.PeriodRevision) {
			if n == 1 {
				close(entered)
				<-release
			}
		}
		result := make(chan error, 1)
		go func() {
			_, e := s.ChangeDeadline(ctx, uuid.MustParse(p.ID), khtcpapp.DeadlineInput{DueAt: p.DueAt.Add(time.Hour)}, "pendingfirst209")
			result <- e
		}()
		defer once.Do(func() { close(release) })
		select {
		case <-entered:
		case <-time.After(8 * time.Second):
			t.Fatal("native barrier not reached")
		}
		_, e := s.ChangeDeadline(ctx, uuid.MustParse(p.ID), khtcpapp.DeadlineInput{DueAt: p.DueAt.Add(2 * time.Hour)}, "pendingsecond209")
		assertConflict209(t, e)
		once.Do(func() { close(release) })
		if e = <-result; e != nil {
			t.Fatal(e)
		}
		if probe.count() != 1 {
			t.Fatalf("pending resource allowed second native writer:%d", probe.count())
		}
	})
}
