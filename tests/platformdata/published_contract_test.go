package platformdata_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	pp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	db "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

var vn = time.FixedZone("Asia/Ho_Chi_Minh", 7*3600)

func pubRead(t *testing.T, tid uuid.UUID, fn func(pgx.Tx) error) {
	t.Helper()
	pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), db.Module("pf/metrics"))
	if e := tenant.WithTenantTx(context.Background(), pool, tid, fn); e != nil {
		t.Fatal(e)
	}
}
func equal(t *testing.T, a, b any) {
	t.Helper()
	x, e := json.Marshal(a)
	if e != nil {
		t.Fatal(e)
	}
	y, e := json.Marshal(b)
	if e != nil {
		t.Fatal(e)
	}
	if string(x) != string(y) {
		t.Fatalf("published/source mismatch\n%s\n%s", x, y)
	}
}
func dateVN(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, vn).UTC()
}
func TestPublishedPlatformReadsReconcileNative(t *testing.T) {
	f := newFixture(t)
	for _, tid := range []uuid.UUID{f.tid, f.other, ids.New()} {
		ctx := tenant.WithID(context.Background(), tid)
		for _, ds := range []string{"2023-01-01T00:00:00Z", "2025-06-30T16:59:59Z", "2025-06-30T17:00:00Z", "2026-06-30T16:59:59Z", "2026-06-30T17:00:00Z"} {
			at, e := time.Parse(time.RFC3339, ds)
			if e != nil {
				t.Fatal(e)
			}
			for _, code := range []string{"89", "89001", "89011", "89012", "89013", "89100", "99999"} {
				var got pp.AdminUnit
				var absent bool
				pubRead(t, tid, func(tx pgx.Tx) error {
					var level string
					var parent *string
					var from time.Time
					var to *time.Time
					e := tx.QueryRow(ctx, `SELECT code,name,level,parent_code,valid_from,valid_to,successor_codes
      FROM pf_masterdata_pub.v1_admin_units WHERE code=$1 AND valid_from<=$2::date AND (valid_to IS NULL OR $2::date<valid_to)`, code, at.In(vn).Format(time.DateOnly)).Scan(&got.Code, &got.Name, &level, &parent, &from, &to, &got.SuccessorCodes)
					if errors.Is(e, pgx.ErrNoRows) {
						absent = true
						return nil
					}
					if e != nil {
						return e
					}
					got.Level = pp.Level(level)
					got.ValidFrom = dateVN(from)
					if parent != nil {
						got.ParentCode = *parent
					}
					if to != nil {
						v := dateVN(*to)
						got.ValidTo = &v
					}
					return nil
				})
				want, e := f.units.Get(ctx, code, at)
				if absent != (e != nil) {
					t.Fatalf("effective boundary %s/%s differs: %v", code, ds, e)
				}
				if !absent {
					equal(t, got, want)
				}
			}
		}
		for _, units := range []struct{ v []string }{{nil}, {[]string{}}, {[]string{"89011"}}, {[]string{"89012"}}, {[]string{"99999"}}} {
			for _, active := range []bool{false, true} {
				var got []pp.Org
				pubRead(t, tid, func(tx pgx.Tx) error {
					rows, e := tx.Query(ctx, `SELECT org_ref,kind,code,name,admin_unit_code,parent_ref,active FROM pf_party_pub.v1_org_tree
      WHERE kind='cooperative' AND ($1::boolean OR admin_unit_code=ANY($2::text[])) AND (NOT $3::boolean OR active) ORDER BY org_ref`, units.v == nil, units.v, active)
					if e != nil {
						return e
					}
					defer rows.Close()
					for rows.Next() {
						var o pp.Org
						var kind string
						var unit *string
						var parent *uuid.UUID
						if e := rows.Scan(&o.Ref, &kind, &o.Code, &o.Name, &unit, &parent, &o.Active); e != nil {
							return e
						}
						o.Kind = pp.OrgKind(kind)
						if unit != nil {
							o.AdminUnitCode = *unit
						}
						if parent != nil {
							o.ParentRef = *parent
						}
						got = append(got, o)
					}
					return rows.Err()
				})
				want, e := f.dir.OrgsOfKind(ctx, pp.OrgCooperative, units.v, active)
				if e != nil {
					t.Fatal(e)
				}
				if len(got) != len(want) {
					t.Fatal("org length differs")
				}
				if len(got) > 0 {
					equal(t, got, want)
				}
			}
		}
	}
}
func TestPublishedPlatformPrivileges(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	env := pgtest.Get(t)
	for _, module := range []string{"pf/extension", "pf/price", "pf/hrm", "pf/contact", "pf/metrics"} {
		pool := db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), db.Module(module))
		queries := []string{"SELECT count(*) FROM pf_masterdata.admin_unit", "SELECT count(*) FROM pf_party.org", "INSERT INTO pf_masterdata_pub.v1_admin_units DEFAULT VALUES", "INSERT INTO pf_party_pub.v1_org_tree DEFAULT VALUES"}
		if module != "pf/metrics" {
			queries = append(queries, "SELECT count(*) FROM pf_masterdata_pub.v1_admin_units", "SELECT count(*) FROM pf_party_pub.v1_org_tree")
		}
		for _, q := range queries {
			e := tenant.WithTenantTx(ctx, pool, f.tid, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, q); return e })
			var pe *pgconn.PgError
			if !errors.As(e, &pe) || pe.Code != "42501" {
				t.Errorf("%s %s: %v", module, q, e)
			}
		}
	}
}
func TestPlatformPublishedUpgradeKeepsSources(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.exec(`INSERT INTO pf_masterdata.admin_unit(tenant_id,code,valid_from,valid_to,name,level,parent_code,successor_codes,source,actor_ref)
	  SELECT tenant_id,'U'||n,valid_from,valid_to,name,level,parent_code,successor_codes,source,actor_ref
	  FROM pf_masterdata.admin_unit u CROSS JOIN generate_series(1,1000)n WHERE tenant_id=$1 AND code='89'`, f.tid)
	f.exec(`INSERT INTO pf_party.org(tenant_id,ref,kind,code,name,admin_unit_code,parent_ref,active,last_event_id,last_seq)
	  SELECT tenant_id,gen_random_uuid(),kind,'UP-'||n,name,admin_unit_code,parent_ref,active,last_event_id,last_seq
	  FROM pf_party.org o CROSS JOIN generate_series(1,1000)n WHERE tenant_id=$1 AND ref=$2`, f.tid, ref(103))
	tx, e := f.admin.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, e = tx.Exec(ctx, "DROP SCHEMA pf_masterdata_pub CASCADE; DROP SCHEMA pf_party_pub CASCADE"); e != nil {
		t.Fatal(e)
	}
	snapshot := func() string {
		t.Helper()
		s := ""
		for _, table := range []string{"pf_masterdata.admin_unit", "pf_party.org"} {
			var n int64
			var h string
			if e := tx.QueryRow(ctx, "SELECT count(*),md5(string_agg(to_jsonb(s)::text,'|' ORDER BY to_jsonb(s)::text)) FROM "+table+" s WHERE tenant_id=$1", f.tid).Scan(&n, &h); e != nil {
				t.Fatal(e)
			}
			s += fmt.Sprintf("%s:%d/%s;", table, n, h)
		}
		return s
	}
	before := snapshot()
	for _, file := range []string{"pf/20261018041412_pf_masterdata_pub_admin_units_v1.up.sql", "pf/20261018041437_pf_party_pub_org_tree_v1.up.sql"} {
		raw, e := migrations.FS.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	if after := snapshot(); after != before {
		t.Fatalf("source counts/checksums changed: %s/%s", before, after)
	}
	var units, orgs int
	if e := tx.QueryRow(ctx, "SELECT (SELECT count(*) FROM pf_masterdata_pub.v1_admin_units WHERE tenant_id=$1),(SELECT count(*) FROM pf_party_pub.v1_org_tree WHERE tenant_id=$1)", f.tid).Scan(&units, &orgs); e != nil || units != 1009 || orgs != 1007 {
		t.Fatalf("old data not published %d/%d %v", units, orgs, e)
	}
	t.Log("1000 OLD rows per source: counts/checksums unchanged across both migrations")
}
