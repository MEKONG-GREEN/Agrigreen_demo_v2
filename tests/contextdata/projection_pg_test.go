package contextdata_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Both modes use the private direct server. exec disables statement caching as
// for transaction pooling; this does not claim an actual PgBouncer deployment.
func oneConnection(t *testing.T, execMode bool) *pgxpool.Pool {
	t.Helper()
	env := pgtest.Get(t)
	dsn, err := dbadmin.RoleURL(env.DirectURL, migrations.PF, env.Suffix, dbadmin.AppPlatform)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.Open(context.Background(), dsn, db.Options{ViaPgBouncer: execMode, MaxConns: 1, AppName: "context-native-contract"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

const rowSQL = `SELECT current_user,
 coalesce(current_setting('app.tenant_id', true), ''),
 coalesce(current_setting('app.node_id', true), ''),
 coalesce(current_setting('app.dept', true), ''),
 coalesce(current_setting('app.xa_ids', true), ''),
 coalesce(current_setting('app.purpose', true), ''),
 coalesce(current_setting('app.actor_ref', true), ''),
 coalesce(current_setting('app.on_behalf_of_ref', true), '')`

// Test-only adapter: pgx exposes savepoints through Tx.Begin, not BeginTx.
type savepointBeginner struct{ pgx.Tx }

func (s savepointBeginner) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return s.Begin(ctx)
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func checkRow(ctx context.Context, q rowQuerier, role, tid string, want map[string]string) error {
	values := make([]string, 8)
	ptrs := make([]any, len(values))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := q.QueryRow(ctx, rowSQL).Scan(ptrs...); err != nil {
		return err
	}
	if values[0] != role || values[1] != tid {
		return fmt.Errorf("role/tenant = %s/%s, want %s/%s", values[0], values[1], role, tid)
	}
	for i, key := range settingNames {
		if values[i+2] != want[key] {
			return fmt.Errorf("%s=%q, want %q", key, values[i+2], want[key])
		}
	}
	return nil
}

func TestFrozenContextProjectionOnPostgres(t *testing.T) { runProjectionOnPostgres(t, nativeCases()) }

func runProjectionOnPostgres(t *testing.T, cases []contextCase) {
	t.Helper()
	expected := golden(t)
	if len(cases) != len(expected) {
		t.Fatal("fixture/input count differs")
	}
	for _, execMode := range []bool{true, false} {
		t.Run(fmt.Sprintf("exec/%t", execMode), func(t *testing.T) {
			p := oneConnection(t, execMode)
			// Deliberately differs from Principal.TenantID. Projection cannot change the
			// tenant which authentication/the trusted job has attached to the context.
			tid := ref(999)
			rollback := errors.New("synthetic rollback")
			denied, accepted := 0, 0
			for i, c := range cases {
				want := expected[i]
				if want.Name != c.name {
					t.Fatalf("case order: %s != %s", c.name, want.Name)
				}
				ctx := tenant.WithID(c.context(), tid)
				for _, abort := range []bool{false, true} {
					entered := false
					err := db.WithContextTx(ctx, p, db.Module("pf/content"), func(tx pgx.Tx) error {
						entered = true
						if err := checkRow(ctx, tx, "r_content", tid.String(), want.Settings); err != nil {
							return err
						}
						if abort {
							return rollback
						}
						return nil
					})
					if want.RequiresNode && want.Settings[db.VarNodeID] == "" {
						pb := problem.From(err)
						if entered || !errors.Is(err, db.ErrNodeScopeRequired) || pb.Code != "node_scope_required" || pb.Status != 403 {
							t.Fatalf("%s: missing-node protection entered=%t err=%v", c.name, entered, err)
						}
						denied++
					} else {
						var wantErr error
						if abort {
							wantErr = rollback
						}
						if !entered || !errors.Is(err, wantErr) {
							t.Fatalf("%s rollback=%t entered=%t: %v", c.name, abort, entered, err)
						}
						accepted++
					}
					// Same physical connection, after either COMMIT, ROLLBACK or denied BEGIN.
					if err := checkRow(context.Background(), p, dbadmin.AppPlatform, "", map[string]string{}); err != nil {
						t.Fatalf("%s leaked context: %v", c.name, err)
					}
				}
			}
			if denied == 0 || accepted == 0 {
				t.Fatal("scope matrix did not exercise both allow and deny")
			}
			t.Logf("%d frozen vectors, %d accepted/%d denied transactions; no pool leakage", len(cases), accepted, denied)
		})
	}
}

func TestNestedProjectionResetsEverySetting(t *testing.T) {
	for _, execMode := range []bool{true, false} {
		t.Run(fmt.Sprintf("exec/%t", execMode), func(t *testing.T) {
			p := oneConnection(t, execMode)
			var full contextCase
			var want contextResult
			for i, c := range nativeCases() {
				if c.name == "values/support" {
					full = c
					want = golden(t)[i]
					break
				}
			}
			if full.principal == nil || want.Settings[db.VarOnBehalfOfRef] == "" {
				t.Fatal("missing support canary")
			}
			tid := ref(999)
			outer := tenant.WithID(full.context(), tid)
			empty := tenant.WithID(context.Background(), tid)
			rollback := errors.New("synthetic savepoint rollback")
			err := db.WithContextTx(outer, p, db.Module("pf/content"), func(tx pgx.Tx) error {
				if err := checkRow(outer, tx, "r_content", tid.String(), want.Settings); err != nil {
					return err
				}
				for _, abort := range []bool{true, false} {
					// pgx.Tx.Begin creates a savepoint through the test adapter. A successful inner transaction keeps
					// its LOCAL empty values until outer COMMIT, while rollback restores full.
					err := db.WithContextTx(empty, savepointBeginner{tx}, db.Module("pf/content"), func(sp pgx.Tx) error {
						if err := checkRow(empty, sp, "r_content", tid.String(), map[string]string{}); err != nil {
							return err
						}
						if abort {
							return rollback
						}
						return nil
					})
					if abort {
						if !errors.Is(err, rollback) {
							return fmt.Errorf("savepoint rollback: %w", err)
						}
						if err := checkRow(outer, tx, "r_content", tid.String(), want.Settings); err != nil {
							return err
						}
					} else {
						if err != nil {
							return err
						}
						if err := checkRow(outer, tx, "r_content", tid.String(), map[string]string{}); err != nil {
							return err
						}
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := checkRow(context.Background(), p, dbadmin.AppPlatform, "", map[string]string{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSelectedContextKeepsAuthenticatedIdentity(t *testing.T) {
	for _, c := range nativeCases() {
		if c.principal == nil {
			continue
		}
		original := *c.principal
		p, ok := authz.FromContext(c.context())
		if !ok || !reflect.DeepEqual(p, original) {
			t.Fatalf("%s changed authenticated identity", c.name)
		}
		if p.KN != nil && len(p.Roles) == 1 && p.Roles[0] == "kn_user" && p.Has("kn_officer") {
			t.Fatal("selection minted a realm role")
		}
		if p.ActorRef == uuid.Nil && c.name != "values/zero-actor" {
			t.Fatal("actor fixture unexpectedly empty")
		}
	}
}
