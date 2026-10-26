package htxdata_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	cp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	pp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	db "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func pubRead(t *testing.T, tid uuid.UUID, fn func(pgx.Tx) error) {
	t.Helper()
	env := pgtest.Get(t)
	pool := db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), db.Module("pf/metrics"))
	if err := tenant.WithTenantTx(context.Background(), pool, tid, fn); err != nil {
		t.Fatal(err)
	}
}
func equal(t *testing.T, got, want any) {
	t.Helper()
	a, e := json.Marshal(got)
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(want)
	if e != nil {
		t.Fatal(e)
	}
	if string(a) != string(b) {
		t.Fatalf("published/source mismatch\ngot %s\nwant %s", a, b)
	}
}

func TestPublishedHTXReadReconcilesNative(t *testing.T) {
	f := newFixture(t)
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	directory := party.New(pool, clock.System{}).OrgDirectory()
	for _, tid := range []uuid.UUID{f.tid, f.other, ids.New()} {
		ctx := tenant.WithID(context.Background(), tid)
		for _, units := range [][]string{nil, {}, {"89001"}, {"92001"}, {"99999"}} {
			want, e := f.native.Plots().DigitizedByCommune(ctx, units)
			if e != nil {
				t.Fatal(e)
			}
			var got []cp.PlotStat
			pubRead(t, tid, func(tx pgx.Tx) error {
				rows, e := tx.Query(ctx, `SELECT commune_code,count(*),sum(area_ha)::numeric(14,4) FROM pf_cooperative_pub.v1_plots
     WHERE status<>'retired' AND ($1::boolean OR commune_code=ANY($2::text[])) GROUP BY commune_code ORDER BY commune_code`, units == nil, units)
				if e != nil {
					return e
				}
				defer rows.Close()
				for rows.Next() {
					var p cp.PlotStat
					if e := rows.Scan(&p.CommuneCode, &p.Plots, &p.AreaHa); e != nil {
						return e
					}
					got = append(got, p)
				}
				return rows.Err()
			})
			if len(got) != len(want) {
				t.Fatalf("plot lengths %v/%v", got, want)
			}
			if len(got) > 0 {
				equal(t, got, want)
			}
			// Directory activity is owned by platform; no SQL JOIN to its private tables.
			orgs, e := directory.OrgsOfKind(ctx, pp.OrgCooperative, units, true)
			if e != nil {
				t.Fatal(e)
			}
			members := map[uuid.UUID]int64{}
			pubRead(t, tid, func(tx pgx.Tx) error {
				rows, e := tx.Query(ctx, "SELECT org_ref,member_households FROM pf_cooperative_pub.v1_cooperatives")
				if e != nil {
					return e
				}
				defer rows.Close()
				for rows.Next() {
					var id uuid.UUID
					var n int64
					if e := rows.Scan(&id, &n); e != nil {
						return e
					}
					members[id] = n
				}
				return rows.Err()
			})
			by := map[string]cp.UnitCooperatives{}
			for _, o := range orgs {
				if o.AdminUnitCode == "" {
					continue
				}
				v := by[o.AdminUnitCode]
				v.UnitCode = o.AdminUnitCode
				v.Cooperatives++
				v.Households += members[o.Ref]
				by[o.AdminUnitCode] = v
			}
			var gotCoops []cp.UnitCooperatives
			for _, v := range by {
				gotCoops = append(gotCoops, v)
			}
			slices.SortFunc(gotCoops, func(a, b cp.UnitCooperatives) int { return strings.Compare(a.UnitCode, b.UnitCode) })
			wantCoops, e := f.native.Cooperatives().ByUnit(ctx, units)
			if e != nil {
				t.Fatal(e)
			}
			if len(gotCoops) != len(wantCoops) {
				t.Fatal("cooperative lengths differ")
			}
			if len(gotCoops) > 0 {
				equal(t, gotCoops, wantCoops)
			}
		}
		all := []uuid.UUID{ref(101), ref(102), ref(103), ref(104), ref(105), ref(999)}
		historic := f.native.HTXConfirmations().(interface {
			ConfirmationsAt(context.Context, []uuid.UUID, time.Time) (map[uuid.UUID]string, error)
		})
		for _, at := range []time.Time{{}, time.Date(2026, 10, 14, 16, 59, 58, 999999000, time.UTC), time.Date(2026, 10, 14, 16, 59, 59, 999999000, time.UTC), time.Date(2026, 10, 14, 17, 0, 0, 0, time.UTC)} {
			got := map[uuid.UUID]string{}
			for _, id := range all {
				got[id] = cp.HTXUnconfirmed
			}
			var cut *time.Time
			if !at.IsZero() {
				cut = &at
			}
			pubRead(t, tid, func(tx pgx.Tx) error {
				rows, e := tx.Query(ctx, `SELECT org_ref FROM pf_cooperative_pub.v1_confirmations
      WHERE org_ref=ANY($1::text[]::uuid[]) AND ($2::timestamptz IS NULL OR claimed_at<=$2)`, uuidStrings(all), cut)
				if e != nil {
					return e
				}
				defer rows.Close()
				for rows.Next() {
					var id uuid.UUID
					if e := rows.Scan(&id); e != nil {
						return e
					}
					got[id] = cp.HTXConfirmed
				}
				return rows.Err()
			})
			var want map[uuid.UUID]string
			var e error
			if at.IsZero() {
				want, e = f.native.HTXConfirmations().Confirmations(ctx, all)
			} else {
				want, e = historic.ConfirmationsAt(ctx, all, at)
			}
			if e != nil {
				t.Fatal(e)
			}
			equal(t, got, want)
		}
		refs := []uuid.UUID{ref(401), ref(402), ref(403), ref(404), ref(999)}
		want, e := f.native.Seasons().Codes(ctx, refs)
		if e != nil {
			t.Fatal(e)
		}
		gotCodes := map[uuid.UUID]cp.SeasonKey{}
		pubRead(t, tid, func(tx pgx.Tx) error {
			rows, e := tx.Query(ctx, `SELECT season_ref,season_code,season_year FROM pf_cooperative_pub.v1_seasons
    WHERE season_present AND season_ref=ANY($1::text[]::uuid[])`, uuidStrings(refs))
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var id uuid.UUID
				var k cp.SeasonKey
				if e := rows.Scan(&id, &k.SeasonCode, &k.SeasonYear); e != nil {
					return e
				}
				gotCodes[id] = k
			}
			return rows.Err()
		})
		equal(t, gotCodes, want)
		for _, at := range []time.Time{{}, time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)} {
			var got []cp.SeasonProgress
			var cut *time.Time
			if !at.IsZero() {
				cut = &at
			}
			pubRead(t, tid, func(tx pgx.Tx) error {
				rows, e := tx.Query(ctx, `SELECT commune_code,season_code,season_year,household_ref,area_ha,status='completed',expected_harvest_date
     FROM pf_cooperative_pub.v1_seasons WHERE season_present AND status<>'cancelled'
     AND ($1::timestamptz IS NULL OR opened_at<=$1) ORDER BY season_ref`, cut)
				if e != nil {
					return e
				}
				defer rows.Close()
				for rows.Next() {
					var p cp.SeasonProgress
					if e := rows.Scan(&p.CommuneCode, &p.SeasonCode, &p.SeasonYear, &p.HouseholdRef, &p.AreaHa, &p.Completed, &p.ExpectedHarvestDate); e != nil {
						return e
					}
					got = append(got, p)
				}
				return rows.Err()
			})
			want, e := f.native.Seasons().Progress(ctx, at)
			if e != nil {
				t.Fatal(e)
			}
			if len(got) != len(want) {
				t.Fatalf("progress lengths %v/%v", got, want)
			}
			if len(got) > 0 {
				equal(t, got, want)
			}
		}
		reconcileProduction(t, f, tid)
	}
}

func reconcileProduction(t *testing.T, f *fixture, tid uuid.UUID) {
	t.Helper()
	ctx := tenant.WithID(context.Background(), tid)
	// Aggregating published facts is reconciled against the native API, not a second expected view.
	for flags := 0; flags < 8; flags++ {
		q := cp.ProductionQuery{TenantID: tid, Scope: authz.Scope{All: true}, ByCooperative: flags&1 != 0, BySeason: flags&2 != 0, ByVariety: flags&4 != 0}
		native, e := f.native.Seasons().Production(ctx, q)
		if e != nil {
			t.Fatal(e)
		}
		cols := []string{"NULL::uuid", "''::text", "0::integer", "''::text"}
		var group []string
		if q.ByCooperative {
			cols[0] = "production_cooperative_ref"
			group = append(group, cols[0])
		}
		if q.BySeason {
			cols[1] = "production_season_code"
			cols[2] = "production_season_year"
			group = append(group, cols[2], cols[1])
		}
		if q.ByVariety {
			cols[3] = "production_variety_code"
			group = append(group, cols[3])
		}
		suffix := ""
		if len(group) > 0 {
			suffix = " GROUP BY " + strings.Join(group, ",") + " ORDER BY " + strings.Join(group, ",")
		}
		sql := "SELECT " + strings.Join(cols, ",") + `,count(*),count(*) FILTER(WHERE production_status='open'),
   count(*) FILTER(WHERE production_status='completed'),count(DISTINCT production_household_ref),
   coalesce(sum(production_area_ha),0),coalesce(sum(production_area_ha) FILTER(WHERE actual_kg IS NOT NULL),0),
   coalesce(sum(expected_kg),0),coalesce(sum(actual_kg),0),coalesce(sum(sop_steps),0),coalesce(sum(sop_steps_done),0)
   FROM pf_cooperative_pub.v1_seasons WHERE production_present AND production_status<>'cancelled'` + suffix
		var got []cp.ProductionRow
		pubRead(t, tid, func(tx pgx.Tx) error {
			rows, e := tx.Query(ctx, sql)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var r cp.ProductionRow
				var coop *uuid.UUID
				var exp, act decimal.Decimal
				if e := rows.Scan(&coop, &r.SeasonCode, &r.SeasonYear, &r.VarietyCode, &r.Seasons, &r.OpenSeasons, &r.CompletedSeasons,
					&r.Households, &r.AreaHa, &r.HarvestedAreaHa, &exp, &act, &r.SOPSteps, &r.SOPStepsDone); e != nil {
					return e
				}
				if r.Seasons == 0 {
					continue
				}
				if coop != nil {
					r.CooperativeRef = *coop
				}
				r.ExpectedKg = mass.MustKg(exp.String())
				r.ActualKg = mass.MustKg(act.String())
				got = append(got, r)
			}
			return rows.Err()
		})
		want := native.Rows
		for i := range want {
			want[i].ActualYieldTPerHa = nil
			want[i].SOPProgressPct = nil
		}
		if len(got) != len(want) {
			t.Fatalf("production/group%d lengths %v/%v", flags, got, want)
		}
		if len(got) > 0 {
			equal(t, got, want)
		}
	}
}

func TestPublishedSeasonProjectionSidesIndependent(t *testing.T) {
	f := newFixture(t)
	// Missing sides and different denormalized dimensions must not drop or blend native facts.
	f.exec(`INSERT INTO pf_coop.yield_summary SELECT (jsonb_populate_record(NULL::pf_coop.yield_summary,
   to_jsonb(y)||jsonb_build_object('season_ref',$2::uuid))).* FROM pf_coop.yield_summary y
   WHERE tenant_id=$1 AND season_ref=$3`, f.tid, ref(405), ref(401))
	f.exec(`INSERT INTO pf_coop.season_current SELECT (jsonb_populate_record(NULL::pf_coop.season_current,
   to_jsonb(s)||jsonb_build_object('season_ref',$2::uuid,'status','cancelled','harvest_id',NULL))).*
   FROM pf_coop.season_current s WHERE tenant_id=$1 AND season_ref=$3`, f.tid, ref(406), ref(401))
	f.exec("UPDATE pf_coop.yield_summary SET variety_code='DIFFERENT' WHERE tenant_id=$1 AND season_ref=$2", f.tid, ref(401))
	pubRead(t, f.tid, func(tx pgx.Tx) error {
		var a, b int
		if e := tx.QueryRow(context.Background(), `SELECT count(*) FILTER(WHERE season_present AND NOT production_present),
    count(*) FILTER(WHERE production_present AND NOT season_present) FROM pf_cooperative_pub.v1_seasons`).Scan(&a, &b); e != nil {
			return e
		}
		if a != 1 || b != 1 {
			return fmt.Errorf("lost missing side: %d/%d", a, b)
		}
		var s, p string
		if e := tx.QueryRow(context.Background(), "SELECT variety_code,production_variety_code FROM pf_cooperative_pub.v1_seasons WHERE season_ref=$1", ref(401)).Scan(&s, &p); e != nil {
			return e
		}
		if s != "OM1" || p != "DIFFERENT" {
			return fmt.Errorf("mixed projection facts: %s/%s", s, p)
		}
		return nil
	})
	reconcileProduction(t, f, f.tid)
}

func TestPublishedHTXPrivileges(t *testing.T) {
	f := newFixture(t)
	env := pgtest.Get(t)
	ctx := context.Background()
	for _, role := range []string{"pf/extension", "pf/price", "pf/hrm", "pf/contact", "pf/metrics"} {
		dbi := db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), db.Module(role))
		queries := []string{"SELECT count(*) FROM pf_coop.plot", "INSERT INTO pf_cooperative_pub.v1_plots DEFAULT VALUES"}
		if role != "pf/metrics" && role != "pf/extension" {
			queries = append(queries, "SELECT count(*) FROM pf_cooperative_pub.v1_plots")
		}
		for _, q := range queries {
			err := tenant.WithTenantTx(ctx, dbi, f.tid, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, q); return e })
			var pe *pgconn.PgError
			if !errors.As(err, &pe) || pe.Code != "42501" {
				t.Errorf("%s %s: expected permission denied, got %v", role, q, err)
			}
		}
	}
}

func TestHTXPublishedUpgradeKeepsSources(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.exec(`INSERT INTO pf_coop.plot SELECT (jsonb_populate_record(NULL::pf_coop.plot,
   to_jsonb(p)||jsonb_build_object('ref',gen_random_uuid(),'code','UPGRADE-'||n))).*
   FROM pf_coop.plot p CROSS JOIN generate_series(1,1000)n WHERE tenant_id=$1 AND ref=$2`, f.tid, ref(301))
	tx, e := f.admin.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Only new published objects are removed in this private transaction to reproduce pre-DDL state.
	if _, e = tx.Exec(ctx, "DROP SCHEMA pf_cooperative_pub CASCADE"); e != nil {
		t.Fatal(e)
	}
	tables := []string{"cooperative", "household", "household_membership", "plot", "season_current", "yield_summary", "htx_anchor", "htx_anchor_alias"}
	snapshot := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		for _, table := range tables {
			var n int64
			var hash string
			if e := tx.QueryRow(ctx, "SELECT count(*),coalesce(md5(string_agg(to_jsonb(s)::text,'|' ORDER BY to_jsonb(s)::text)),'') FROM pf_coop."+table+" s WHERE tenant_id=$1", f.tid).Scan(&n, &hash); e != nil {
				t.Fatal(e)
			}
			out[table] = fmt.Sprintf("%d/%s", n, hash)
		}
		return out
	}
	before := snapshot()
	migration, e := migrations.FS.ReadFile("pf/20261018030844_pf_cooperative_pub_htx_data_v1.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, string(migration)); e != nil {
		t.Fatal(e)
	}
	after := snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("source changed across view DDL: %v/%v", before, after)
	}
	var n int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM pf_cooperative_pub.v1_plots WHERE tenant_id=$1", f.tid).Scan(&n); e != nil || n != 1005 {
		t.Fatalf("old data not readable: %d %v", n, e)
	}
	t.Logf("1000 added OLD plot rows, all 8 source counts/checksums unchanged")
}

func uuidStrings(refs []uuid.UUID) []string {
	out := make([]string, len(refs))
	for i, id := range refs {
		out[i] = id.String()
	}
	return out
}
