package htxdata_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	kn "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	cp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/cropcyclesrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func productionQueries() []cropcyclesrc.ProductionQuery {
	return []cropcyclesrc.ProductionQuery{
		{Scope: authz.Scope{All: true}},
		{},
		{Scope: authz.Scope{Units: []string{}}},
		{Scope: authz.Scope{Units: []string{"89001"}}},
		{Scope: authz.Scope{Units: []string{"92001"}}},
		{Scope: authz.Scope{Units: []string{"89"}}},
		{Scope: authz.Scope{Units: []string{"89001", "92001", "89001"}}},
		{Scope: authz.Scope{All: true}, CooperativeRef: ref(101)},
		{Scope: authz.Scope{All: true}, CooperativeRef: ref(101), SeasonCode: "DT", SeasonYear: 2026, VarietyCode: "OM1"},
		{Scope: authz.Scope{All: true}, SeasonYear: 2025},
		{Scope: authz.Scope{All: true}, VarietyCode: "OM2"},
		{Scope: authz.Scope{All: true}, CooperativeRef: ref(999)},
		{Scope: authz.Scope{All: true}, SeasonCode: "MISSING"},
		{Scope: authz.Scope{All: true}, SeasonYear: 2040},
		{Scope: authz.Scope{All: true}, VarietyCode: "MISSING"},
		{Scope: authz.Scope{Units: []string{"92001"}}, CoopOnly: true, CooperativeRef: ref(101)},
		{Scope: authz.Scope{All: true}, CoopOnly: true, CooperativeRef: ref(103)},
		{CoopOnly: true, CooperativeRef: ref(999)},
	}
}

func compareProduction(t *testing.T, f *fixture, r cropcyclesrc.ReportData, ctx context.Context, q cropcyclesrc.ProductionQuery) cropcyclesrc.ProductionData {
	t.Helper()
	native, err := f.native.Seasons().Production(ctx, cp.ProductionQuery(q))
	if err != nil {
		t.Fatal(err)
	}
	want := cropcyclesrc.ProductionData{Rows: []cropcyclesrc.ProductionRow{}, Totals: cropcyclesrc.ProductionFigures(native.Totals)}
	for _, v := range native.Rows {
		want.Rows = append(want.Rows, cropcyclesrc.ProductionRow{CooperativeRef: v.CooperativeRef, SeasonCode: v.SeasonCode, SeasonYear: v.SeasonYear, VarietyCode: v.VarietyCode, ProductionFigures: cropcyclesrc.ProductionFigures(v.ProductionFigures)})
	}
	got, err := r.Production(ctx, q)
	if err != nil {
		t.Fatalf("query %+v: %v", q, err)
	}
	equal(t, got, want)
	return got
}

func TestKNPublishedProductionReconcilesNative(t *testing.T) {
	f := newFixture(t)
	r := extension.NewPublishedHTXProduction(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform))
	checks := 0
	for _, tid := range []uuid.UUID{f.tid, f.other, ids.New()} {
		ctx := tenant.WithID(context.Background(), tid)
		for _, q := range productionQueries() {
			q.TenantID = tid
			for flags := 0; flags < 8; flags++ {
				q.ByCooperative, q.BySeason, q.ByVariety = flags&1 != 0, flags&2 != 0, flags&4 != 0
				compareProduction(t, f, r, ctx, q)
				checks++
			}
		}
	}
	ctx := tenant.WithID(context.Background(), f.tid)
	q := cropcyclesrc.ProductionQuery{TenantID: f.tid, Scope: authz.Scope{All: true}, ByCooperative: true, BySeason: true, ByVariety: true}
	d := compareProduction(t, f, r, ctx, q)
	var rowHouseholds int64
	for _, v := range d.Rows {
		rowHouseholds += v.Households
	}
	if len(d.Rows) != 3 || d.Totals.Seasons != 3 || d.Totals.Households != 1 || rowHouseholds != 3 ||
		!d.Totals.AreaHa.Equal(decimal.RequireFromString("6.2345")) || !d.Totals.HarvestedAreaHa.Equal(decimal.NewFromInt(5)) ||
		d.Totals.ExpectedKg.String() != "3703.500" || d.Totals.ActualKg.String() != "1000.125" ||
		d.Totals.ActualYieldTPerHa == nil || !d.Totals.ActualYieldTPerHa.Equal(decimal.RequireFromString("0.2000")) ||
		d.Totals.SOPProgressPct == nil || !d.Totals.SOPProgressPct.Equal(decimal.RequireFromString("71.4")) {
		t.Fatalf("lost native totals/precision/distinct households: %+v rowsHouseholds=%d", d, rowHouseholds)
	}
	q.VarietyCode = "OM2"
	zero := compareProduction(t, f, r, ctx, q)
	if zero.Totals.ActualYieldTPerHa == nil || !zero.Totals.ActualYieldTPerHa.IsZero() || !zero.Totals.HarvestedAreaHa.Equal(decimal.NewFromInt(3)) || zero.Totals.SOPProgressPct != nil {
		t.Fatalf("zero actual is a harvest, no SOP denominator: %+v", zero)
	}
	q.VarietyCode, q.SeasonCode = "OM1", "HT"
	missing := compareProduction(t, f, r, ctx, q)
	if missing.Totals.ActualYieldTPerHa != nil || !missing.Totals.HarvestedAreaHa.IsZero() || missing.Totals.SOPProgressPct == nil || !missing.Totals.SOPProgressPct.Equal(decimal.RequireFromString("33.3")) {
		t.Fatalf("NULL actual must not invent harvest/yield: %+v", missing)
	}
	t.Logf("%d native/publication reconciliations plus fixed totals, zero/NULL and ratios", checks)
}

func TestKNPublishedProductionUsesIndependentProjection(t *testing.T) {
	f := newFixture(t)
	// The production side must survive a missing/different season_current side.
	f.exec(`INSERT INTO pf_coop.yield_summary SELECT (jsonb_populate_record(NULL::pf_coop.yield_summary,
  to_jsonb(y)||jsonb_build_object('season_ref',$2::uuid))).* FROM pf_coop.yield_summary y WHERE tenant_id=$1 AND season_ref=$3`, f.tid, ref(405), ref(401))
	f.exec(`INSERT INTO pf_coop.season_current SELECT (jsonb_populate_record(NULL::pf_coop.season_current,
  to_jsonb(s)||jsonb_build_object('season_ref',$2::uuid,'status','cancelled','harvest_id',NULL))).* FROM pf_coop.season_current s WHERE tenant_id=$1 AND season_ref=$3`, f.tid, ref(406), ref(401))
	f.exec("UPDATE pf_coop.season_current SET status='cancelled' WHERE tenant_id=$1 AND season_ref=$2", f.tid, ref(401))
	f.exec(`UPDATE pf_coop.yield_summary SET cooperative_ref=$3,commune_code='92001',season_code='DX',season_year=2024,variety_code='DIFFERENT'
  WHERE tenant_id=$1 AND season_ref=$2`, f.tid, ref(401), ref(103))
	r := extension.NewPublishedHTXProduction(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform))
	ctx := tenant.WithID(context.Background(), f.tid)
	qs := append(productionQueries(), cropcyclesrc.ProductionQuery{Scope: authz.Scope{All: true}, SeasonCode: "DX", SeasonYear: 2024, VarietyCode: "DIFFERENT"})
	for _, q := range qs {
		q.TenantID = f.tid
		for flags := 0; flags < 8; flags++ {
			q.ByCooperative, q.BySeason, q.ByVariety = flags&1 != 0, flags&2 != 0, flags&4 != 0
			compareProduction(t, f, r, ctx, q)
		}
	}
	d := compareProduction(t, f, r, ctx, cropcyclesrc.ProductionQuery{TenantID: f.tid, Scope: authz.Scope{All: true}})
	if d.Totals.Seasons != 4 {
		t.Fatalf("missing/extra production side: %+v", d)
	}
}

func TestKNPublishedProductionPrincipalScopes(t *testing.T) {
	f := newFixture(t)
	r := extension.NewPublishedHTXProduction(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform))
	for _, tid := range []uuid.UUID{f.tid, f.other} {
		f.seedPrincipalScopeProjection(tid)
		principals := []authz.Principal{
			{ActorRef: ref(900), OrgRef: ref(990), Roles: []string{kn.RoleKNOfficer}},
			{ActorRef: ref(900), OrgRef: ref(101), Roles: []string{kn.RoleCoopManager}},
			{ActorRef: ref(900), KN: &authz.KNContext{Role: authz.KNXa, OrgRef: ref(990), Department: "trong_trot", Communes: []string{"89001"}, Scope: authz.Scope{Units: []string{"89001"}}}},
			{ActorRef: ref(900), KN: &authz.KNContext{Role: authz.KNXa, OrgRef: ref(998), Department: "trong_trot", Communes: []string{"89001"}, Scope: authz.Scope{Units: []string{"89001"}}}},
			{ActorRef: ref(900), KN: &authz.KNContext{Role: authz.KNTinh, OrgRef: ref(990), Department: "trong_trot", Scope: authz.Scope{Units: []string{"89"}}}},
			{ActorRef: ref(900), KN: &authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}}},
		}
		for _, p := range principals {
			p.TenantID = tid
			ctx := authz.WithPrincipal(tenant.WithID(context.Background(), tid), p)
			for _, sc := range []authz.Scope{{All: true}, {Units: []string{"89001"}}, {Units: []string{"92001"}}} {
				for flags := 0; flags < 8; flags++ {
					q := cropcyclesrc.ProductionQuery{TenantID: tid, Scope: sc, ByCooperative: flags&1 != 0, BySeason: flags&2 != 0, ByVariety: flags&4 != 0}
					d := compareProduction(t, f, r, ctx, q)
					if tid == f.tid && p.KN != nil && p.KN.Role == authz.KNXa && sc.All && d.Totals.Seasons != 2 {
						t.Fatalf("KN commune escaped actual node RLS: %+v", d)
					}
				}
			}
		}
	}
	t.Log("288 reconciliations under real node RLS, including KN context with no node projection")
}

type productionUnavailableDB struct {
	opts *pgx.TxOptions
	err  error
}

func (b productionUnavailableDB) BeginTx(_ context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	*b.opts = opts
	return nil, b.err
}

func TestKNPublishedProductionFailsClosed(t *testing.T) {
	failure := errors.New("production publication unavailable")
	opts := pgx.TxOptions{}
	r := extension.NewPublishedHTXProduction(productionUnavailableDB{&opts, failure})
	tid := ids.New()
	ctx := tenant.WithID(context.Background(), tid)
	valid := cropcyclesrc.ProductionQuery{TenantID: tid, Scope: authz.Scope{All: true}}
	d, e := r.Production(ctx, valid)
	if !errors.Is(e, failure) || opts.AccessMode != pgx.ReadOnly {
		t.Fatalf("missing source or write transaction: %+v %v", opts, e)
	}
	equal(t, d, cropcyclesrc.ProductionData{})
	if _, e = r.Production(context.Background(), valid); !errors.Is(e, tenant.ErrMissing) {
		t.Fatalf("missing trusted context: %v", e)
	}
	for _, q := range []cropcyclesrc.ProductionQuery{
		{}, {TenantID: ids.New()}, {TenantID: tid, CoopOnly: true},
		{TenantID: tid, SeasonCode: "HT-1"}, {TenantID: tid, SeasonCode: strings.Repeat("A", 41)},
		{TenantID: tid, VarietyCode: "bad code"}, {TenantID: tid, VarietyCode: strings.Repeat("A", 41)},
	} {
		d, e = r.Production(ctx, q)
		if e == nil || errors.Is(e, failure) {
			t.Fatalf("invalid query reached DB: %+v %v", q, e)
		}
		equal(t, d, cropcyclesrc.ProductionData{})
	}
}
