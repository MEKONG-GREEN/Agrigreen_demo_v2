package htxdata_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	cp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

func ref(n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%012d", n))
}

type fixture struct {
	t          *testing.T
	admin      *pgxpool.Pool
	native     *cooperative.Module
	tid, other uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	directory := party.New(pool, clock.System{}).OrgDirectory()
	f := &fixture{t: t, admin: env.AdminPool(t, migrations.PF), tid: ids.New(), other: ids.New()}
	f.native = cooperative.New(pool, clock.System{}, cooperative.Deps{Dir: directory})
	f.seed(f.tid, false)
	f.seed(f.other, true)
	return f
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.admin.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("seed: %v", err)
	}
}

func (f *fixture) seed(tid uuid.UUID, other bool) {
	for n := 101; n <= 105; n++ {
		unit := "89001"
		if n == 103 {
			unit = "92001"
		}
		if n == 105 {
			unit = ""
		}
		active := n != 104
		if other {
			active = n == 101
		}
		f.exec(`INSERT INTO pf_party.org(tenant_id,ref,kind,code,name,admin_unit_code,active,last_event_id,last_seq)
		 VALUES($1,$2,'cooperative',$3,'HTX fixture',NULLIF($4,''),$5,$6,1)`,
			tid, ref(n), fmt.Sprintf("C%d", n), unit, active, ref(n+1000))
		if n != 102 {
			f.exec("INSERT INTO pf_coop.cooperative(tenant_id,org_ref) VALUES($1,$2)", tid, ref(n))
		}
	}
	for n := 201; n <= 207; n++ {
		if other && n != 201 {
			continue
		}
		active := n != 202
		f.exec(`INSERT INTO pf_coop.household(tenant_id,household_ref,commune_code,active,created_by)
		 VALUES($1,$2,'89001',$3,$4)`, tid, ref(n), active, ref(900))
		coop := 101
		if n == 204 {
			coop = 102
		}
		if n == 205 {
			coop = 104
		}
		if n == 206 {
			coop = 105
		}
		var until *time.Time
		if n == 203 {
			d := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
			until = &d
		}
		f.exec(`INSERT INTO pf_coop.household_membership(tenant_id,id,household_ref,org_ref,since_date,until_date)
		 VALUES($1,$2,$3,$4,'2026-01-01',$5)`, tid, ref(n+1000), ref(n), ref(coop), until)
	}
	for n := 301; n <= 305; n++ {
		if other && n != 301 {
			continue
		}
		area, unit, status := "1.2345", "89001", "active"
		var coop any = ref(101)
		switch n {
		case 302:
			area, status, coop = "2.0001", "draft", nil
		case 303:
			area, status = "9.9999", "retired"
		case 304:
			area, unit, coop = "0.0001", "92001", ref(103)
		case 305:
			area, coop = "3.3333", ref(102)
		}
		if other {
			area = "99.9999"
		}
		f.exec(`INSERT INTO pf_coop.plot(tenant_id,ref,code,commune_code,cooperative_ref,status,current_version,current_boundary_id,
		  geom,area_ha,source,created_by) VALUES($1,$2,$3,$4,$5,$6,1,$7,
		  ST_Multi(ST_GeomFromText('POLYGON((105 10,105.001 10,105.001 10.001,105 10.001,105 10))',4326)),$8,'web',$9)`,
			tid, ref(n), fmt.Sprintf("P%d", n), unit, coop, status, ref(n+1000), area, ref(900))
	}
	for n := 401; n <= 404; n++ {
		if other && n != 401 {
			continue
		}
		status, coop, unit, season, year, variety, area := "open", 101, "89001", "HT", 2026, "OM1", "1.2345"
		var actual any
		expected := "1234.500"
		steps, done := 3, 1
		plot := 301
		switch n {
		case 402:
			status, plot, season, area, actual, steps, done = "completed", 305, "DT", "2.0000", "1000.125", 4, 4
		case 403:
			status, plot, coop, unit, year, variety, area, actual, steps, done = "completed", 304, 103, "92001", 2025, "OM2", "3.0000", "0.000", 0, 0
		case 404:
			status, plot, coop, area, actual = "cancelled", 302, 102, "99.0000", "9000.000"
		}
		if other {
			area, expected = "50.0000", "90000.000"
		}
		var harvest any
		if status == "completed" {
			harvest = ref(n + 3000)
		}
		f.exec(`INSERT INTO pf_coop.season_current(tenant_id,season_ref,plot_ref,plot_code,household_ref,cooperative_ref,commune_code,
		 season_code,season_year,variety_code,sop_template_ref,sop_version_id,sop_version_no,sowing_date,expected_harvest_date,
		 harvest_open_date,area_ha,status,current_event_id,version,location_status,gps_source,on_behalf,opened_by,opened_at,harvest_id)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1,'2026-07-01','2026-10-15','2026-10-01',
		 $13,$14,$15,1,'matched','manual',false,$16,'2026-07-01T01:00:00Z',$17)`,
			tid, ref(n), ref(plot), fmt.Sprintf("P%d", plot), ref(201), ref(coop), unit, season, year, variety, ref(800), ref(801), area, status, ref(n+1000), ref(900), harvest)
		f.exec(`INSERT INTO pf_coop.yield_summary(tenant_id,season_ref,plot_ref,household_ref,cooperative_ref,commune_code,
		 season_code,season_year,variety_code,status,area_ha,expected_kg,actual_kg,sop_steps,sop_mandatory_steps,
		 sop_steps_done,sop_mandatory_done,approved_logs,pending_logs)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,0,$15,0,2,1)`,
			tid, ref(n), ref(plot), ref(201), ref(coop), unit, season, year, variety, status, area, expected, actual, steps, done)
	}
	for n := 1; n <= 3; n++ {
		status := "claimed"
		var claimed, account, actor, mst any
		if n < 3 && !other {
			at := time.Date(2026, 10, 14, 16, 59, 59, 0, time.UTC)
			if n == 2 {
				at = at.Add(time.Second)
			}
			claimed, account, actor, mst = at, ref(700+n), ref(900), fmt.Sprintf("031234560%d", n)
		} else {
			status = "provisional"
		}
		key := fmt.Sprintf("anchor-%d", n)
		if mst != nil {
			key = mst.(string)
		}
		org := 101
		if n == 2 {
			org = 103
		}
		if n == 3 {
			org = 104
		}
		current := org
		if n == 1 && !other {
			current = 102
		}
		f.exec(`INSERT INTO pf_coop.htx_anchor(tenant_id,anchor_id,anchor_key,mst,status,created_by_kind,created_by,
		 provisional_org_ref,org_ref,htx_account_ref,claimed_at,claimed_by,last_event_id,last_seq)
		 VALUES($1,$2,$3,$4,$5,'mkg',$6,$7,$8,$9,$10,$11,$12,1)`,
			tid, ref(600+n), key, mst, status, ref(900), ref(org), ref(current), account, claimed, actor, ref(1600+n))
		f.exec(`INSERT INTO pf_coop.htx_anchor_alias(tenant_id,org_ref,anchor_id,role,event_id)
		 VALUES($1,$2,$3,'provisional',$4)`, tid, ref(org), ref(600+n), ref(1600+n))
		if n == 1 && !other {
			f.exec(`INSERT INTO pf_coop.htx_anchor_alias(tenant_id,org_ref,anchor_id,role,event_id)
			 VALUES($1,$2,$3,'claimed',$4)`, tid, ref(102), ref(601), ref(1601))
		}
	}
}

func must[T any](t *testing.T, value T, err error) T {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestLegacyHTXReadContract(t *testing.T) {
	f := newFixture(t)
	ctx := tenant.WithID(context.Background(), f.tid)
	other := tenant.WithID(context.Background(), f.other)
	emptyID := ids.New()
	emptyTenant := tenant.WithID(context.Background(), emptyID)
	snap := map[string]any{}
	for _, c := range []struct {
		name  string
		units []string
	}{
		{"all", nil}, {"empty", []string{}}, {"one", []string{"89001"}}, {"second", []string{"92001"}}, {"unknown", []string{"99999"}},
	} {
		v, e := f.native.Cooperatives().ByUnit(ctx, c.units)
		snap["coops/"+c.name] = must(t, v, e)
		p, e := f.native.Plots().DigitizedByCommune(ctx, c.units)
		snap["plots/"+c.name] = must(t, p, e)
	}
	coops, e := f.native.Cooperatives().ByUnit(ctx, nil)
	if e != nil || len(coops) != 2 || coops[0].Cooperatives != 2 || coops[0].Households != 3 {
		t.Fatalf("native edge noProfile/inactive/closed membership: %+v %v", coops, e)
	}
	plots, e := f.native.Plots().DigitizedByCommune(ctx, []string{"89001"})
	if e != nil || len(plots) != 1 || plots[0].Plots != 3 || plots[0].AreaHa.String() != "6.5679" {
		t.Fatalf("native draft/retired/decimal: %+v %v", plots, e)
	}
	for _, c := range []struct {
		name string
		ctx  context.Context
	}{{"other", other}, {"emptyTenant", emptyTenant}} {
		v, e := f.native.Cooperatives().ByUnit(c.ctx, nil)
		snap["coops/"+c.name] = must(t, v, e)
		p, e := f.native.Plots().DigitizedByCommune(c.ctx, nil)
		snap["plots/"+c.name] = must(t, p, e)
	}
	all := []uuid.UUID{ref(101), ref(102), ref(103), ref(104), ref(105), ref(999)}
	current, e := f.native.HTXConfirmations().Confirmations(ctx, all)
	snap["confirm/current"] = must(t, current, e)
	historical := f.native.HTXConfirmations().(interface {
		ConfirmationsAt(context.Context, []uuid.UUID, time.Time) (map[uuid.UUID]string, error)
	})
	cutoff := time.Date(2026, 10, 14, 16, 59, 59, 999999000, time.UTC)
	for _, c := range []struct {
		name string
		at   time.Time
	}{{"before", cutoff.Add(-time.Second)}, {"cutoff", cutoff}, {"nextDay", cutoff.Add(time.Second)}} {
		v, e := historical.ConfirmationsAt(ctx, all, c.at)
		snap["confirm/"+c.name] = must(t, v, e)
	}
	v, e := historical.ConfirmationsAt(ctx, all, cutoff)
	if e != nil || v[ref(101)] != cp.HTXConfirmed || v[ref(102)] != cp.HTXConfirmed || v[ref(103)] != cp.HTXUnconfirmed {
		t.Fatalf("native aliases and VN cutoff: %v %v", v, e)
	}
	for _, c := range []struct {
		name string
		ctx  context.Context
	}{{"other", other}, {"emptyTenant", emptyTenant}} {
		v, e := historical.ConfirmationsAt(c.ctx, all, cutoff)
		snap["confirm/"+c.name] = must(t, v, e)
	}
	for flags := 0; flags < 8; flags++ {
		q := cp.ProductionQuery{TenantID: f.tid, Scope: authz.Scope{All: true}, ByCooperative: flags&1 != 0, BySeason: flags&2 != 0, ByVariety: flags&4 != 0}
		v, e := f.native.Seasons().Production(ctx, q)
		snap[fmt.Sprintf("production/group%d", flags)] = must(t, v, e)
	}
	for _, c := range []struct {
		name    string
		scope   authz.Scope
		coop    uuid.UUID
		season  string
		year    int
		variety string
	}{
		{"none", authz.Scope{}, uuid.Nil, "", 0, ""},
		{"commune", authz.Scope{Units: []string{"89001"}}, uuid.Nil, "", 0, ""},
		{"province", authz.Scope{Units: []string{"89"}}, uuid.Nil, "", 0, ""},
		{"filter", authz.Scope{All: true}, ref(101), "DT", 2026, "OM1"},
		{"unknown", authz.Scope{All: true}, ref(999), "", 0, ""},
	} {
		q := cp.ProductionQuery{TenantID: f.tid, Scope: c.scope, CooperativeRef: c.coop, SeasonCode: c.season, SeasonYear: c.year, VarietyCode: c.variety, ByCooperative: true, BySeason: true, ByVariety: true}
		v, e := f.native.Seasons().Production(ctx, q)
		snap["production/"+c.name] = must(t, v, e)
	}
	for _, c := range []struct {
		name string
		ctx  context.Context
		tid  uuid.UUID
	}{{"other", other, f.other}, {"emptyTenant", emptyTenant, emptyID}} {
		q := cp.ProductionQuery{TenantID: c.tid, Scope: authz.Scope{All: true}, ByCooperative: true, BySeason: true, ByVariety: true}
		v, e := f.native.Seasons().Production(c.ctx, q)
		snap["production/"+c.name] = must(t, v, e)
	}
	codes, e := f.native.Seasons().Codes(ctx, []uuid.UUID{ref(401), ref(402), ref(403), ref(404), ref(999)})
	snap["season/codes"] = must(t, codes, e)
	for _, c := range []struct {
		name string
		at   time.Time
	}{{"all", time.Time{}}, {"before", time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)}} {
		v, e := f.native.Seasons().Progress(ctx, c.at)
		snap["season/progress/"+c.name] = must(t, v, e)
	}
	blob, e := json.MarshalIndent(snap, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	blob = append(blob, '\n')
	want, e := os.ReadFile(filepath.Join("..", "golden", "htx-read-native.json"))
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(blob, want) {
		t.Fatal("native HTX read contract differs from frozen baseline")
	}
}
