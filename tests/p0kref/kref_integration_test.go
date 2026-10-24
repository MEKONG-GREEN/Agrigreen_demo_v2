package p0kref_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	partyapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// P0 regressions assemble the real source and profile modules using deployment
// module roles. They intentionally do not use the party handler's fake profile.
func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type p0KRefFixture struct {
	env      *pgtest.Env
	mod      *party.Module
	profiles port.CoopProfiles
	tenant   uuid.UUID
}

type p0KRefUnits struct{}

func (p0KRefUnits) Get(_ context.Context, code string, _ time.Time) (port.AdminUnit, error) {
	switch code {
	case "89":
		return port.AdminUnit{Code: code, Name: "Province", Level: port.LevelProvince}, nil
	case "30001", "30002":
		return port.AdminUnit{Code: code, Name: "Commune", Level: port.LevelCommune, ParentCode: "89"}, nil
	default:
		return port.AdminUnit{}, port.ErrUnitNotFound
	}
}
func (u p0KRefUnits) Descendants(ctx context.Context, code string, at time.Time) ([]string, error) {
	if _, err := u.Get(ctx, code, at); err != nil {
		return nil, err
	}
	if code == "89" {
		return []string{"89", "30001", "30002"}, nil
	}
	return []string{code}, nil
}
func (u p0KRefUnits) Current(ctx context.Context, code string, at time.Time) ([]string, error) {
	if _, err := u.Get(ctx, code, at); err != nil {
		return nil, err
	}
	return []string{code}, nil
}
func newP0KRefFixture(t *testing.T) *p0KRefFixture {
	t.Helper()
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	mod := party.New(pool, clock.System{}, party.Deps{AdminUnits: p0KRefUnits{}})
	coop := cooperative.New(pool, clock.System{}, cooperative.Deps{})
	mod.UseCoopProfiles(coop.CoopProfiles())
	return &p0KRefFixture{env: env, mod: mod, profiles: coop.CoopProfiles(), tenant: ids.New()}
}

func key() string { return "p0-" + ids.New().String() }

func p0KRefContext(tid, node uuid.UUID, roles ...string) context.Context {
	return tenant.WithID(authz.WithPrincipal(context.Background(), authz.Principal{
		ActorRef: ids.New(), TenantID: tid, OrgRef: node, Roles: roles, MFA: true,
	}), tid)
}

func p0KRefParent(t *testing.T, f *p0KRefFixture) uuid.UUID {
	t.Helper()
	return p0KRefCreate(t, f, p0KRefContext(f.tenant, uuid.Nil, authz.RolePlatformAdmin), partyapp.CreateOrgInput{
		Kind: port.OrgTTKNProvince, Code: "P0-" + ids.New().String()[:8], Name: "P0 province", AdminUnitCode: "89", IdemKey: key(),
	}).Org.Ref
}

func p0KRefCreate(t *testing.T, f *p0KRefFixture, ctx context.Context, in partyapp.CreateOrgInput) partyapp.OrgResult {
	t.Helper()
	reg := f.mod.Orgs().(interface {
		CreateOrg(context.Context, partyapp.CreateOrgInput) (partyapp.OrgResult, error)
	})
	result, err := reg.CreateOrg(ctx, in)
	if err != nil {
		t.Fatalf("native CreateOrg with real cooperative profile: %v", err)
	}
	return result
}

func p0KRefAssertProjection(t *testing.T, f *p0KRefFixture, ref, parent uuid.UUID) {
	t.Helper()
	ctx := p0KRefContext(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	err := db.WithContextTx(ctx, f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), party.DBModule, func(tx pgx.Tx) error {
		var orgs, self, ancestors int
		if err := tx.QueryRow(ctx, `SELECT
		  (SELECT count(*) FROM k_ref.org WHERE tenant_id=$1 AND ref=$2),
		  (SELECT count(*) FROM k_ref.node_closure WHERE tenant_id=$1 AND ancestor=$2 AND descendant=$2 AND depth=0),
		  (SELECT count(*) FROM k_ref.node_closure WHERE tenant_id=$1 AND ancestor=$3 AND descendant=$2 AND depth=1)`,
			f.tenant, ref, parent).Scan(&orgs, &self, &ancestors); err != nil {
			return err
		}
		if orgs != 1 || self != 1 || (parent != uuid.Nil && ancestors != 1) {
			return fmt.Errorf("projection unavailable before any worker: org=%d self=%d parent=%d", orgs, self, ancestors)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestP0KRefCreateVisibleBeforeWorkerAndDelayedEvent(t *testing.T) {
	f := newP0KRefFixture(t)
	parent := p0KRefParent(t, f)
	p0KRefAssertProjection(t, f, parent, uuid.Nil)
	// A native principal can be bound to the parent node; no KN write path is used.
	ctx := p0KRefContext(f.tenant, parent, authz.RolePlatformAdmin, port.RoleRegistryAdmin)
	created := p0KRefCreate(t, f, ctx, partyapp.CreateOrgInput{
		Kind: port.OrgCooperative, Name: "P0 synchronous cooperative", AdminUnitCode: "30001", ParentRef: parent, IdemKey: key(),
	})
	p0KRefAssertProjection(t, f, created.Org.Ref, parent)

	var profiles int
	pool := f.env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	if err := db.WithContextTx(ctx, pool, cooperative.DBModule, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pf_coop.cooperative WHERE org_ref=$1`, created.Org.Ref).Scan(&profiles)
	}); err != nil || profiles != 1 {
		t.Fatalf("real profile must exist through populated node RLS: count=%d err=%v", profiles, err)
	}

	// The old outbox event is still delivered. Its same seq cannot replace the
	// projection already committed by the source transaction.
	worker := f.env.Pool(t, migrations.PF, dbadmin.AppWorker)
	mod := kref.New(worker, clock.System{})
	payload, err := json.Marshal(map[string]any{"org_ref": created.Org.Ref, "kind": "cooperative", "code": "stale-code",
		"admin_unit_code": "30002", "parent_ref": parent, "active": false, "seq": created.Event.Seq})
	if err != nil {
		t.Fatal(err)
	}
	env := outbox.Envelope{EventID: ids.New(), EventType: kref.EventOrgCreated, SchemaVersion: 1,
		TenantID: f.tenant, OccurredAt: time.Now().UTC(), Producer: "pf/party", Payload: payload}
	if _, err := inbox.Process(context.Background(), db.ForModule(worker, kref.DBModule), kref.Consumer, env, mod.Handler()); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContextTx(ctx, pool, party.DBModule, func(tx pgx.Tx) error {
		var code string
		var active bool
		var seq int64
		if err := tx.QueryRow(ctx, `SELECT code,active,source_seq FROM k_ref.org WHERE ref=$1`, created.Org.Ref).Scan(&code, &active, &seq); err != nil {
			return err
		}
		if code != created.Org.Code || !active || seq != created.Event.Seq {
			return fmt.Errorf("same-seq event overwrote synchronous projection: code=%s active=%v seq=%d", code, active, seq)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p0KRefAssertProjection(t, f, created.Org.Ref, parent)
}

func TestP0KRefCreateRollbackIsAtomic(t *testing.T) {
	f := newP0KRefFixture(t)
	ctx := p0KRefContext(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	ref, eventID, outboxID := ids.New(), ids.New(), ids.New()
	abort := errors.New("intentional source transaction abort")
	pool := f.env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	err := db.WithContextTx(ctx, pool, party.DBModule, func(tx pgx.Tx) error {
		o := port.Org{Ref: ref, Kind: port.OrgMKG, Code: "ROLLBACK-" + ref.String()[:8], Name: "Rollback node", Active: true}
		ev := domain.OrgEvent{ID: eventID, TenantID: f.tenant, Org: o, Action: domain.ActionCreated,
			EventTime: time.Now().UTC(), ActorRef: ids.New(), IdemKey: key()}
		if err := store.InsertOrgEvent(ctx, tx, &ev); err != nil {
			return err
		}
		if err := store.InsertOrg(ctx, tx, f.tenant, o, ev); err != nil {
			return err
		}
		if err := outbox.Enqueue(ctx, tx, outbox.Event{ID: outboxID, TenantID: f.tenant, Type: port.EventOrgCreated,
			SchemaVersion: 1, OccurredAt: ev.EventTime, Producer: "pf/party", Payload: map[string]any{"org_ref": ref}}); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM k_ref.node_closure WHERE tenant_id=$1 AND ancestor=$2 AND descendant=$2`, f.tenant, ref).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("source tx cannot see synchronous closure: %d", n)
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatalf("expected intentional abort after observing projection, got %v", err)
	}
	if err := db.WithContextTx(ctx, pool, party.DBModule, func(tx pgx.Tx) error {
		var source, events, projected, closure, queued int
		if err := tx.QueryRow(ctx, `SELECT
		 (SELECT count(*) FROM pf_party.org WHERE ref=$1),
		 (SELECT count(*) FROM pf_party.org_event WHERE id=$2),
		 (SELECT count(*) FROM k_ref.org WHERE ref=$1),
		 (SELECT count(*) FROM k_ref.node_closure WHERE ancestor=$1 OR descendant=$1),
		 (SELECT count(*) FROM shared.outbox WHERE id=$3)`, ref, eventID, outboxID).
			Scan(&source, &events, &projected, &closure, &queued); err != nil {
			return err
		}
		if source+events+projected+closure+queued != 0 {
			return fmt.Errorf("rollback leaked source=%d events=%d projection=%d closure=%d outbox=%d", source, events, projected, closure, queued)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestP0KRefConcurrentCooperativesRealProfiles(t *testing.T) {
	f := newP0KRefFixture(t)
	parent := p0KRefParent(t, f)
	p0KRefAssertProjection(t, f, parent, uuid.Nil)
	ctx, cancel := context.WithTimeout(p0KRefContext(f.tenant, parent, authz.RolePlatformAdmin, port.RoleRegistryAdmin), 30*time.Second)
	defer cancel()
	const distinct, replays = 12, 12
	type outcome struct {
		ref  uuid.UUID
		err  error
		same bool
	}
	results := make(chan outcome, distinct+replays)
	start := make(chan struct{})
	var wg sync.WaitGroup
	sharedKey := key()
	for i := range distinct + replays {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			name, idem, same := fmt.Sprintf("P0 distinct %02d", i), key(), false
			if i >= distinct {
				name, idem, same = "P0 repeated submission", sharedKey, true
			}
			ref, err := f.mod.CooperativeRegistrar().RegisterCooperative(ctx, port.CooperativeRegistration{
				Name: name, CommuneCode: "30001", ParentRef: parent, IdemKey: idem,
			})
			results <- outcome{ref: ref, err: err, same: same}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	all, replayRef := map[uuid.UUID]bool{}, uuid.Nil
	for result := range results {
		if result.err != nil || result.ref == uuid.Nil {
			t.Errorf("valid concurrent native registration returned conflict/dependency failure: %v", result.err)
			continue
		}
		if result.same {
			if replayRef != uuid.Nil && result.ref != replayRef {
				t.Errorf("same idempotency key produced distinct nodes: %s and %s", replayRef, result.ref)
			}
			replayRef = result.ref
		}
		all[result.ref] = true
	}
	if len(all) != distinct+1 {
		t.Errorf("expected %d distinct nodes after replays, got %d", distinct+1, len(all))
	}
	for ref := range all {
		p0KRefAssertProjection(t, f, ref, parent)
	}
	if err := db.WithContextTx(ctx, f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), party.DBModule, func(tx pgx.Tx) error {
		var nodes, events, queued int
		if err := tx.QueryRow(ctx, `SELECT
		 (SELECT count(*) FROM pf_party.org WHERE kind='cooperative'),
		 (SELECT count(*) FROM pf_party.org_event WHERE kind='cooperative'),
		 (SELECT count(*) FROM shared.outbox WHERE event_type=$1 AND payload->>'kind'='cooperative')`, port.EventOrgCreated).
			Scan(&nodes, &events, &queued); err != nil {
			return err
		}
		if nodes != distinct+1 || events != nodes || queued != nodes {
			return fmt.Errorf("concurrent registration leaked node/event/outbox: %d/%d/%d", nodes, events, queued)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContextTx(ctx, f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), cooperative.DBModule, func(tx pgx.Tx) error {
		var profiles int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_coop.cooperative`).Scan(&profiles); err != nil {
			return err
		}
		if profiles != distinct+1 {
			return fmt.Errorf("native registration left orphan node: profiles=%d want=%d", profiles, distinct+1)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestP0KRefEmptyProjectionScopedReadWriteFailClosed(t *testing.T) {
	f := newP0KRefFixture(t)
	created := p0KRefCreate(t, f, p0KRefContext(f.tenant, uuid.Nil, authz.RolePlatformAdmin), partyapp.CreateOrgInput{
		Kind: port.OrgCooperative, Name: "P0 empty projection cooperative", AdminUnitCode: "30001", IdemKey: key(),
	})
	// Fault injection only: emulate a tenant whose projection has not been built.
	admin := f.env.AdminPool(t, migrations.PF)
	for _, relation := range []string{"node_closure", "org"} {
		if _, err := admin.Exec(context.Background(), "DELETE FROM k_ref."+relation+" WHERE tenant_id=$1", f.tenant); err != nil {
			t.Fatal(err)
		}
	}
	ctx := p0KRefContext(f.tenant, created.Org.Ref, port.RoleCoopManager)
	pool := f.env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	if err := db.WithContextTx(ctx, pool, cooperative.DBModule, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_coop.cooperative`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("empty k_ref opened tenant-wide scoped read: %d", n)
		}
		tag, err := tx.Exec(ctx, `UPDATE pf_coop.cooperative SET head_office='must-not-write' WHERE org_ref=$1`, created.Org.Ref)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 0 {
			return errors.New("empty k_ref opened scoped UPDATE")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err := db.WithContextTx(ctx, pool, cooperative.DBModule, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_coop.cooperative (tenant_id,org_ref) VALUES ($1,$2)`, f.tenant, created.Org.Ref)
		return err
	})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Fatalf("scoped INSERT with empty projection must fail RLS42501, got %v", err)
	}
}

func TestP0KRefSourceRollbackWhenParentProjectionMissing(t *testing.T) {
	f := newP0KRefFixture(t)
	parent := p0KRefParent(t, f)
	admin := f.env.AdminPool(t, migrations.PF)
	for _, relation := range []string{"node_closure", "org"} {
		if _, err := admin.Exec(context.Background(), "DELETE FROM k_ref."+relation+" WHERE tenant_id=$1", f.tenant); err != nil {
			t.Fatal(err)
		}
	}
	ctx := p0KRefContext(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	reg := f.mod.Orgs().(interface {
		CreateOrg(context.Context, partyapp.CreateOrgInput) (partyapp.OrgResult, error)
	})
	idem := key()
	_, err := reg.CreateOrg(ctx, partyapp.CreateOrgInput{Kind: port.OrgExtensionTeam, Code: "MISSING-PARENT",
		Name: "Child must roll back", AdminUnitCode: "30001", ParentRef: parent, IdemKey: idem})
	if err == nil {
		t.Fatal("source committed child even though parent projection is absent")
	}
	if p := problem.From(err); p.Status != http.StatusServiceUnavailable || p.Code != "party.dependency_unavailable" {
		t.Fatalf("missing parent projection must return dependency503, got %d %s", p.Status, p.Code)
	}
	if err := db.WithContextTx(ctx, f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), party.DBModule, func(tx pgx.Tx) error {
		var nodes, events, projected int
		if err := tx.QueryRow(ctx, `SELECT
		 (SELECT count(*) FROM pf_party.org WHERE parent_ref=$1),
		 (SELECT count(*) FROM pf_party.org_event WHERE idem_key=$2),
		 (SELECT count(*) FROM k_ref.org)`, parent, idem).Scan(&nodes, &events, &projected); err != nil {
			return err
		}
		if nodes+events+projected != 0 {
			return fmt.Errorf("missing parent left partial node/event/projection: %d/%d/%d", nodes, events, projected)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestP0KRefProjectionPrivilegesAndTenantIsolation(t *testing.T) {
	f := newP0KRefFixture(t)
	parent := p0KRefParent(t, f)
	pool := f.env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	ctx := p0KRefContext(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	for name, run := range map[string]func(pgx.Tx) error{
		"party cannot write projection directly": func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO k_ref.org (tenant_id,ref,kind,code,active,source_seq) VALUES ($1,$2,'mkg','forbidden',true,1)`, f.tenant, ids.New())
			return err
		},
		"platform cannot become projection worker": func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "SET LOCAL ROLE r_kref")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := db.WithContextTx(ctx, pool, party.DBModule, run)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "42501" {
				t.Fatalf("DB privilege boundary must remain42501, got %v", err)
			}
		})
	}
	foreign := p0KRefContext(ids.New(), parent, port.RoleCoopManager)
	if err := db.WithContextTx(foreign, pool, party.DBModule, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(foreign, `SELECT count(*) FROM k_ref.org WHERE ref=$1`, parent).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return errors.New("projection leaked across tenant")
		}
		_, err := tx.Exec(foreign, `INSERT INTO pf_party.org (tenant_id,ref,kind,code,name,active,last_event_id,last_seq)
		 VALUES ($1,$2,'mkg','foreign','foreign',true,$3,1)`, f.tenant, ids.New(), ids.New())
		return err
	}); err == nil {
		t.Fatal("cross-tenant source write unexpectedly succeeded")
	} else {
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "42501" {
			t.Fatalf("cross-tenant write must fail42501, got %v", err)
		}
	}
}

// The first request pauses only before its initial TaxCodeOwner read. All reads
// and writes themselves still use the real cooperative module and its RLS.
type p0KRefBlockedTaxOwner struct {
	port.CoopProfiles
	mu      sync.Mutex
	blocked bool
	entered chan struct{}
	release chan struct{}
}

func (p *p0KRefBlockedTaxOwner) TaxCodeOwner(ctx context.Context, tax string) (uuid.UUID, error) {
	p.mu.Lock()
	first := !p.blocked
	p.blocked = true
	p.mu.Unlock()
	if first {
		close(p.entered)
		select {
		case <-p.release:
		case <-ctx.Done():
			return uuid.Nil, ctx.Err()
		}
	}
	return p.CoopProfiles.TaxCodeOwner(ctx, tax)
}

func TestP0KRefSameKeyReplayWinsTaxCodePrecheckRace(t *testing.T) {
	f := newP0KRefFixture(t)
	parent := p0KRefParent(t, f)
	p0KRefAssertProjection(t, f, parent, uuid.Nil)
	gate := &p0KRefBlockedTaxOwner{CoopProfiles: f.profiles, entered: make(chan struct{}), release: make(chan struct{})}
	f.mod.UseCoopProfiles(gate)
	ctx, cancel := context.WithTimeout(p0KRefContext(f.tenant, parent, authz.RolePlatformAdmin, port.RoleRegistryAdmin), 30*time.Second)
	defer cancel()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate.release) }) }
	defer release()
	input := port.CooperativeRegistration{Name: "P0 stable tax replay", CommuneCode: "30001", ParentRef: parent, TaxCode: "1234567890", IdemKey: key()}
	type result struct {
		ref uuid.UUID
		err error
	}
	waiting := make(chan result, 1)
	go func() {
		ref, err := f.mod.CooperativeRegistrar().RegisterCooperative(ctx, input)
		waiting <- result{ref, err}
	}()
	select {
	case <-gate.entered:
	case <-ctx.Done():
		t.Fatal("first registration never reached initial real tax-owner read")
	}
	// Same-key winner completes BOTH native source and real profile writes while
	// the waiting request has already passed its initial replay lookup.
	winner, err := f.mod.CooperativeRegistrar().RegisterCooperative(ctx, input)
	if err != nil {
		t.Fatalf("same-key winner: %v", err)
	}
	release()
	select {
	case loser := <-waiting:
		if loser.err != nil || loser.ref != winner {
			t.Fatalf("same-key replay became tax conflict after winner commit: winner=%s replay=%s err=%v", winner, loser.ref, loser.err)
		}
	case <-ctx.Done():
		t.Fatal("waiting same-key registration did not complete")
	}
	p0KRefAssertProjection(t, f, winner, parent)
	if err := db.WithContextTx(ctx, f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), party.DBModule, func(tx pgx.Tx) error {
		var nodes, events int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM pf_party.org WHERE kind='cooperative'), (SELECT count(*) FROM pf_party.org_event WHERE kind='cooperative')`).Scan(&nodes, &events); err != nil {
			return err
		}
		if nodes != 1 || events != 1 {
			return fmt.Errorf("same-key tax race created extra native node/event: %d/%d", nodes, events)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
