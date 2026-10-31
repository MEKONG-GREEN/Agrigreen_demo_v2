package app_test

// Native ledger/concurrency checks stay inside the owning module. Public HTTP
// and report-consumer behavior remains in tests/kn77.
import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	templateapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/app"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	templatestore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type templateFixture struct {
	tid, khtcUser uuid.UUID
	clk           clock.Clock
}

func newTemplateFixture() *templateFixture {
	return &templateFixture{tid: ids.New(), khtcUser: ids.New(), clk: clock.NewFixed(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))}
}
func templateContext(t *testing.T, tid, actor uuid.UUID, role string, viewOnly bool) context.Context {
	t.Helper()
	selected, err := port.NewSessionContext(port.SessionContext{ID: ids.New(), Role: role, Scope: authz.Scope{All: true}, ViewOnly: viewOnly})
	if err != nil {
		t.Fatal(err)
	}
	return tenant.WithID(authz.WithPrincipal(context.Background(), authz.Principal{TenantID: tid, ActorRef: actor, SelectedSession: &selected}), tid)
}
func TestW1165_ConcurrencyIdempotencyReadOnlyAndNativeSession(t *testing.T) {
	f := newTemplateFixture()
	env := pgtest.Get(t)
	config := env.Pool(t, migrations.PF, dbadmin.AppPlatform).Config()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s := &templateapp.Service{DB: db.ForModule(pool, extension.DBModule), Clock: f.clk}
	ctx := templateContext(t, f.tid, f.khtcUser, authz.KNAdmin, false)
	in := templateapp.SaveInput{Definition: templatedomain.Seed("mau_1")}
	key := "save-" + ids.New().String()
	first, err := s.Save(ctx, "mau_1", in, key)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.Save(ctx, "mau_1", in, key)
	if err != nil || replay.ID != first.ID {
		t.Fatalf("lost response retry created another version: %v", err)
	}
	changed := in
	changed.Definition = templatedomain.Canonical(in.Definition)
	changed.Definition.Nodes[0].Body = "Đổi nội dung"
	if _, err = s.Save(ctx, "mau_1", changed, key); !errors.Is(err, problem.IdempotencyConflict) {
		t.Fatalf("same key changed payload: %v", err)
	}
	results := make(chan error, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Save(ctx, "mau_1", templateapp.SaveInput{BaseVersion: 1, Definition: templatedomain.Seed("mau_1")}, ids.New().String())
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	passed, conflict := 0, 0
	for e := range results {
		if e == nil {
			passed++
		} else if errors.Is(e, templateapp.ErrVersion) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if passed != 1 || conflict != 3 {
		t.Fatalf("concurrent writes: pass=%d conflict=%d", passed, conflict)
	}
	readonly := templateContext(t, f.tid, f.khtcUser, authz.KNAdmin, true)
	var denied *problem.Problem
	if _, err = s.Save(readonly, "mau_1", in, ids.New().String()); !errors.As(err, &denied) || denied.Status != http.StatusForbidden || denied.Code != "forbidden" {
		t.Fatalf("read-only write: %v", err)
	}
	native := authz.WithPrincipal(context.Background(), authz.Principal{TenantID: f.tid, ActorRef: f.khtcUser, Roles: []string{authz.RolePlatformAdmin}, KN: &authz.KNContext{Role: authz.KNAdmin}})
	if _, err = s.Save(native, "mau_1", in, ids.New().String()); err == nil {
		t.Fatal("native/legacy context bypassed the KN owner projection")
	}
}

func TestW1165_DatabaseRejectsMismatchedPublishedVersion(t *testing.T) {
	f := newTemplateFixture()
	s := templateapp.Service{DB: db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule), Clock: f.clk}
	v, err := s.Save(templateContext(t, f.tid, f.khtcUser, authz.KNAdmin, false), "mau_1", templateapp.SaveInput{Definition: templatedomain.Seed("mau_1")}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	ctx := templateContext(t, f.tid, f.khtcUser, authz.KNAdmin, false)
	p := templatestore.Publication{ID: ids.New(), Key: "mau_1", VersionID: v.ID, Version: v.Version + 1, Hash: v.Hash, RequestHash: strings.Repeat("0", 64), AppliesFrom: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), Actor: f.khtcUser, At: f.clk.Now()}
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	err = tenant.WithTenantTx(ctx, db.ForModule(pool, extension.DBModule), f.tid, func(tx pgx.Tx) error {
		return templatestore.Publish(ctx, tx, f.tid, &p, ids.New().String(), templatestore.Support{})
	})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.ConstraintName != "khtc_template_publication_version" {
		t.Fatalf("publication version mismatch accepted: %v", err)
	}
}
