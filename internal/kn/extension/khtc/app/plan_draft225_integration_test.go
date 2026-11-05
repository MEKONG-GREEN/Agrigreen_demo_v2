package app_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	khtcapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/app"
	khtcdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/domain"
	periodapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	fixture "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/khtcdeadline209"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// Fault boundary: the native DB COMMIT succeeds, then its acknowledgement is lost.
// No production hook, private Forms SQL or synthetic successful receipt is used.
type lostDraftAck225 struct {
	tenant.Beginner
	fired atomic.Bool
}
type lostDraftAckTx225 struct {
	pgx.Tx
	owner  *lostDraftAck225
	writes bool
}

func (b *lostDraftAck225) BeginTx(ctx context.Context, opt pgx.TxOptions) (pgx.Tx, error) {
	tx, e := b.Beginner.BeginTx(ctx, opt)
	if e != nil {
		return nil, e
	}
	return &lostDraftAckTx225{Tx: tx, owner: b}, nil
}
func (tx *lostDraftAckTx225) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "INSERT INTO pf_extension.khtc_plan_draft225_event") {
		tx.writes = true
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}
func (tx *lostDraftAckTx225) Commit(ctx context.Context) error {
	if e := tx.Tx.Commit(ctx); e != nil {
		return e
	}
	if tx.writes && tx.owner.fired.CompareAndSwap(false, true) {
		return context.DeadlineExceeded
	}
	return nil
}
func TestInitialDraft225LostCommitPoolOneStoredReplayAndHistoryPaging(t *testing.T) {
	env := pgtest.Get(t)
	base := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	cfg := base.Config().Copy()
	cfg.MaxConns = 1
	cfg.MinConns = 0
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
	tid, actor, org := ids.New(), ids.New(), ids.New()
	owner := authz.KNContext{ID: ids.New(), Role: authz.KNTinh, Department: "ke_hoach_tai_chinh", OrgRef: org, ExpiresAt: clk.Now().Add(time.Hour), Scope: authz.Scope{Units: []string{"X1"}}}
	p := fixture.Bind(t, authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, MFA: false, KN: &owner})
	ctx = authz.WithPrincipal(ctx, p)
	owned := db.ForModule(pool, extension.DBModule)
	fault := &lostDraftAck225{Beginner: owned}
	references := &periodapp.Service{DB: owned, Clock: clk}
	svc := &khtcapp.Service{DB: fault, Clock: clk, YearReferences: references}
	input := khtcapp.InitialDraftInput225{Year: 2036, ExpectedRevision: 0, IdemKey: "draft225-lost-commit", Entries: []khtcdomain.Entry{}, Note: "saved but ACK lost"}
	_, replay, err := svc.SaveInitialDraft225(ctx, input)
	if !errors.Is(err, context.DeadlineExceeded) || replay || !fault.fired.Load() {
		t.Fatalf("commit fault not exercised: replay=%t err=%T", replay, err)
	}
	first, replay, err := svc.SaveInitialDraft225(ctx, input)
	if err != nil || !replay || first.Event.Seq != 1 {
		t.Fatalf("exact commit recovery failed: replay=%t err=%T", replay, err)
	}
	for revision := int64(2); revision <= 101; revision++ {
		next, hit, err := svc.SaveInitialDraft225(ctx, khtcapp.InitialDraftInput225{Year: input.Year, DraftID: first.Event.DraftID, ExpectedRevision: revision - 1, Entries: []khtcdomain.Entry{}, IdemKey: fmt.Sprintf("draft225-page-%d", revision)})
		if err != nil || hit || next.Event.Seq != revision {
			t.Fatalf("history append revision=%d replay=%t err=%T", revision, hit, err)
		}
	}
	historical, hit, err := svc.SaveInitialDraft225(ctx, input)
	if err != nil || !hit || historical.Event.ID != first.Event.ID || historical.Event.Seq != 1 {
		t.Fatalf("old operation was promoted to latest: replay=%t err=%T", hit, err)
	}
	page, err := svc.InitialDraftHistory225(ctx, input.Year, 0, 100)
	if err != nil || len(page) != 100 || page[0].Seq != 101 || page[99].Seq != 2 {
		t.Fatalf("complete first history page size=%d err=%T", len(page), err)
	}
	tail, err := svc.InitialDraftHistory225(ctx, input.Year, page[99].Seq, 100)
	if err != nil || len(tail) != 1 || tail[0].ID != first.Event.ID {
		t.Fatalf("history tail size=%d err=%T", len(tail), err)
	}
	renewed := owner
	renewed.ID = ids.New()
	renewed.ExpiresAt = owner.ExpiresAt.Add(time.Hour)
	renewedP := fixture.Bind(t, authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, MFA: false, KN: &renewed})
	again, hit, err := svc.SaveInitialDraft225(authz.WithPrincipal(ctx, renewedP), input)
	if err != nil || !hit || again.Event.ID != first.Event.ID {
		t.Fatalf("same-authority renewed replay failed: replay=%t err=%T", hit, err)
	}
	renewed.ViewOnly = true
	readonly := fixture.Bind(t, authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, MFA: false, KN: &renewed})
	_, _, err = svc.SaveInitialDraft225(authz.WithPrincipal(ctx, readonly), input)
	var denied *problem.Problem
	if !errors.As(err, &denied) || denied.Status != 403 {
		t.Fatalf("current read-only authority was not enforced: err=%T", err)
	}
	var count int
	err = tenant.WithTenantTx(ctx, owned, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_plan_draft225_event WHERE plan_year=$1`, input.Year).Scan(&count)
	})
	if err != nil || count != 101 {
		t.Fatalf("receipt replays appended events: count=%d err=%T", count, err)
	}
}

type heldDraftYear225 struct {
	tenant.Beginner
	key     string
	hold    bool
	once    atomic.Bool
	before  chan uint32
	locked  chan struct{}
	release chan struct{}
}
type heldDraftYearTx225 struct {
	pgx.Tx
	owner *heldDraftYear225
}

func (b *heldDraftYear225) BeginTx(ctx context.Context, opt pgx.TxOptions) (pgx.Tx, error) {
	tx, e := b.Beginner.BeginTx(ctx, opt)
	if e != nil {
		return nil, e
	}
	return &heldDraftYearTx225{Tx: tx, owner: b}, nil
}
func (tx *heldDraftYearTx225) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	target := strings.Contains(sql, "pg_advisory_xact_lock") && len(args) == 1 && args[0] == tx.owner.key
	first := target && tx.owner.once.CompareAndSwap(false, true)
	if first {
		tx.owner.before <- tx.Conn().PgConn().PID()
	}
	tag, e := tx.Tx.Exec(ctx, sql, args...)
	if e == nil && first && tx.owner.hold {
		close(tx.owner.locked)
		select {
		case <-tx.owner.release:
		case <-ctx.Done():
			return tag, ctx.Err()
		}
	}
	return tag, e
}
func TestInitialDraft225CreateAndLegacyPublishSerializeBothOrders(t *testing.T) {
	for _, draftFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("draft_first_%t", draftFirst), func(t *testing.T) {
			env := pgtest.Get(t)
			pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
			clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
			tid, actor, org := ids.New(), ids.New(), ids.New()
			year := 2037
			owner := authz.KNContext{ID: ids.New(), Role: authz.KNTinh, Department: "ke_hoach_tai_chinh", OrgRef: org, ExpiresAt: clk.Now().Add(time.Hour), Scope: authz.Scope{Units: []string{"X1"}}}
			p := fixture.Bind(t, authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, KN: &owner})
			ctx, cancel := context.WithTimeout(authz.WithPrincipal(context.Background(), p), 15*time.Second)
			defer cancel()
			owned := db.ForModule(pool, extension.DBModule)
			firstDB := &heldDraftYear225{Beginner: owned, key: fmt.Sprintf("plan:%d", year), hold: true, before: make(chan uint32, 1), locked: make(chan struct{}), release: make(chan struct{})}
			secondDB := &heldDraftYear225{Beginner: owned, key: firstDB.key, before: make(chan uint32, 1)}
			var release sync.Once
			unlock := func() { release.Do(func() { close(firstDB.release) }) }
			defer unlock()
			refs := &periodapp.Service{DB: owned, Clock: clk}
			first := &khtcapp.Service{DB: firstDB, Clock: clk, YearReferences: refs}
			second := &khtcapp.Service{DB: secondDB, Clock: clk, YearReferences: refs}
			run := func(svc *khtcapp.Service, draft bool, key string) error {
				if draft {
					_, _, err := svc.SaveInitialDraft225(ctx, khtcapp.InitialDraftInput225{Year: year, IdemKey: key, Entries: []khtcdomain.Entry{}})
					return err
				}
				_, _, err := svc.SaveVersion(ctx, khtcapp.SaveInput{Year: year, IdemKey: key, Entries: []khtcdomain.Entry{}})
				return err
			}
			firstDone := make(chan error, 1)
			secondDone := make(chan error, 1)
			firstPending, secondPending := true, false
			defer func() {
				unlock()
				cancel()
				if firstPending {
					select {
					case <-firstDone:
					case <-time.After(time.Second):
					}
				}
				if secondPending {
					select {
					case <-secondDone:
					case <-time.After(time.Second):
					}
				}
			}()
			go func() { firstDone <- run(first, draftFirst, "draft225-race-first") }()
			select {
			case <-firstDB.locked:
			case err := <-firstDone:
				firstPending = false
				t.Fatalf("first did not acquire year lock: err=%T", err)
			case <-ctx.Done():
				t.Fatal("year lock barrier timed out")
			}
			secondPending = true
			go func() { secondDone <- run(second, !draftFirst, "draft225-race-second") }()
			var pid uint32
			select {
			case pid = <-secondDB.before:
			case err := <-secondDone:
				secondPending = false
				t.Fatalf("second did not request year lock: err=%T", err)
			case <-ctx.Done():
				t.Fatal("second lock request timed out")
			}
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			waiting := false
			for !waiting {
				if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory' AND NOT granted)`, pid).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if !waiting {
					select {
					case <-ticker.C:
					case <-ctx.Done():
						t.Fatal("real advisory waiter not observed")
					}
				}
			}
			unlock()
			select {
			case err := <-firstDone:
				firstPending = false
				if err != nil {
					t.Fatalf("first winner error=%T", err)
				}
			case <-ctx.Done():
				t.Fatal("first completion timed out")
			}
			select {
			case err := <-secondDone:
				secondPending = false
				var p *problem.Problem
				if !errors.As(err, &p) || p.Status != 409 {
					t.Fatalf("competing operation must conflict: err=%T", err)
				}
			case <-ctx.Done():
				t.Fatal("second completion timed out")
			}
			var drafts, versions int
			err := tenant.WithTenantTx(ctx, owned, tid, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM pf_extension.khtc_plan_draft225_event WHERE plan_year=$1),(SELECT count(*) FROM pf_extension.khtc_plan_version WHERE plan_year=$1)`, year).Scan(&drafts, &versions)
			})
			if err != nil || draftFirst && (drafts != 1 || versions != 0) || !draftFirst && (drafts != 0 || versions != 1) {
				t.Fatalf("year families both persisted: drafts=%d versions=%d err=%T", drafts, versions, err)
			}
		})
	}
}

// This direct public-app control deliberately bypasses the outer HTTP cache.
func TestInitialDraft225ImmutableAppReceiptAfterMasterRename(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	owned := db.ForModule(pool, extension.DBModule)
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
	tid, actor, org := ids.New(), ids.New(), ids.New()
	k := authz.KNContext{ID: ids.New(), Role: authz.KNTinh, Department: "ke_hoach_tai_chinh", OrgRef: org, ExpiresAt: clk.Now().Add(time.Hour), Scope: authz.Scope{Units: []string{"X1"}}}
	p := fixture.Bind(t, authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, KN: &k})
	ctx := authz.WithPrincipal(context.Background(), p)
	svc := &khtcapp.Service{DB: owned, Clock: clk, YearReferences: &periodapp.Service{DB: owned, Clock: clk}}
	program, _, err := svc.CreateProgram(ctx, khtcapp.ProgramInput{Program: khtcdomain.Program{Code: "P225", Name: "Tên CT gốc225", Kind: "program", Status: "active"}, IdemKey: "draft225-app-program"})
	if err != nil {
		t.Fatal(err)
	}
	in := khtcapp.InitialDraftInput225{Year: 2038, ExpectedRevision: 0, IdemKey: "draft225-app-create", Note: "initial", Entries: []khtcdomain.Entry{{Ref: ids.New(), Tab: khtcdomain.TabPrograms, Kind: khtcdomain.EntryProgram, ProgramRef: &program.Ref, Name: "Tên dòng đã chốt225", Code: "P225", SortOrder: 1, Tracking: "tracking", Communes: []string{}}}}
	first, hit, err := svc.SaveInitialDraft225(ctx, in)
	if err != nil || hit || len(first.Entries) != 1 {
		t.Fatalf("nonempty initial app write: replay=%t error=%T", hit, err)
	}
	_, _, err = svc.ReviseProgram(ctx, khtcapp.ProgramInput{Ref: program.Ref, Program: khtcdomain.Program{Code: "P225", Name: "Tên danh mục mới225", Kind: "program", Status: "retired"}, IdemKey: "draft225-app-retire"})
	if err != nil {
		t.Fatal(err)
	}
	own, hit, err := svc.SaveInitialDraft225(ctx, in)
	if err != nil || !hit || own.Event.ID != first.Event.ID || !bytes.Equal(own.Event.EntriesHash, first.Event.EntriesHash) || len(own.Entries) != 1 || own.Entries[0].Name != "Tên dòng đã chốt225" || own.Entries[0].Code != "P225" || own.Entries[0].ProgramName != "" {
		t.Fatalf("immutable nonempty app receipt replay: replay=%t error=%T", hit, err)
	}
	fresh := in
	fresh.DraftID = first.Event.DraftID
	fresh.ExpectedRevision = 1
	fresh.IdemKey = "draft225-app-retired-fresh"
	_, _, err = svc.SaveInitialDraft225(ctx, fresh)
	var bad *problem.Problem
	if !errors.As(err, &bad) || bad.Status != 422 {
		t.Fatalf("fresh retired master must be denied: error=%T", err)
	}
	current, err := svc.GetInitialDraft225(ctx, in.Year)
	if err != nil || current.Revision != 1 {
		t.Fatalf("replay/rejected new write changed revision: revision=%d error=%T", current.Revision, err)
	}
}
