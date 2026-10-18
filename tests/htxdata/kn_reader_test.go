package htxdata_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	kn "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestKNPublishedHTXStatisticsReconcileNative(t *testing.T) {
	f := newFixture(t)
	r := extension.NewPublishedHTXData(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform))
	for _, tid := range []uuid.UUID{f.tid, f.other, ids.New()} {
		ctx := tenant.WithID(context.Background(), tid)
		for _, units := range [][]string{nil, {}, {"89001"}, {"92001"}, {"89001", "92001"}, {"unknown"}, {"89001", "89001"}} {
			native, err := f.native.Cooperatives().ByUnit(ctx, units)
			if err != nil {
				t.Fatal(err)
			}
			want := []kn.UnitCooperatives{}
			for _, v := range native {
				want = append(want, kn.UnitCooperatives{UnitCode: v.UnitCode, Cooperatives: v.Cooperatives, Households: v.Households})
			}
			got, err := r.ByUnit(ctx, units)
			if err != nil {
				t.Fatal(err)
			}
			equal(t, got, want)
			plots, err := f.native.Plots().DigitizedByCommune(ctx, units)
			if err != nil {
				t.Fatal(err)
			}
			wantPlots := []kn.CommunePlots{}
			for _, v := range plots {
				wantPlots = append(wantPlots, kn.CommunePlots{CommuneCode: v.CommuneCode, Plots: v.Plots, AreaHa: v.AreaHa})
			}
			gotPlots, err := r.DigitizedByCommune(ctx, units)
			if err != nil {
				t.Fatal(err)
			}
			equal(t, gotPlots, wantPlots)
		}
	}
}

func TestKNPublishedHTXConfirmationsReconcileNative(t *testing.T) {
	f := newFixture(t)
	r := extension.NewPublishedHTXData(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform))
	historic := f.native.HTXConfirmations().(interface {
		ConfirmationsAt(context.Context, []uuid.UUID, time.Time) (map[uuid.UUID]string, error)
	})
	for _, tid := range []uuid.UUID{f.tid, f.other, ids.New()} {
		ctx := tenant.WithID(context.Background(), tid)
		for _, refs := range [][]uuid.UUID{nil, {}, {ref(101), ref(102), ref(103), ref(104), ref(105), ref(999), ref(101)}} {
			want, err := f.native.HTXConfirmations().Confirmations(ctx, refs)
			if err != nil {
				t.Fatal(err)
			}
			got, err := r.Confirmations(ctx, refs)
			if err != nil {
				t.Fatal(err)
			}
			equal(t, got, want)
			for _, at := range []time.Time{{}, time.Date(2026, 10, 14, 16, 59, 58, 999999000, time.UTC), time.Date(2026, 10, 14, 16, 59, 59, 999999000, time.UTC), time.Date(2026, 10, 14, 17, 0, 0, 0, time.UTC)} {
				want, err := historic.ConfirmationsAt(ctx, refs, at)
				if err != nil {
					t.Fatal(err)
				}
				got, err := r.ConfirmationsAt(ctx, refs, at)
				if err != nil {
					t.Fatal(err)
				}
				equal(t, got, want)
			}
		}
	}
}

type unavailableDB struct{ err error }

func (b unavailableDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) { return nil, b.err }

func TestKNPublishedHTXReadsFailClosed(t *testing.T) {
	failure := errors.New("reader database unavailable")
	r := extension.NewPublishedHTXData(unavailableDB{failure})
	ctx := tenant.WithID(context.Background(), ids.New())
	c, e := r.ByUnit(ctx, nil)
	if !errors.Is(e, failure) || c != nil {
		t.Fatalf("cooperatives: %v %v", c, e)
	}
	p, e := r.DigitizedByCommune(ctx, nil)
	if !errors.Is(e, failure) || p != nil {
		t.Fatalf("plots: %v %v", p, e)
	}
	h, e := r.Confirmations(ctx, []uuid.UUID{ref(101)})
	if !errors.Is(e, failure) || h != nil {
		t.Fatalf("current: %v %v", h, e)
	}
	h, e = r.ConfirmationsAt(ctx, []uuid.UUID{ref(101)}, time.Now())
	if !errors.Is(e, failure) || h != nil {
		t.Fatalf("historical: %v %v", h, e)
	}
	// Even an empty requested set cannot read without an authenticated tenant.
	for _, call := range []func() error{
		func() error { _, e := r.ByUnit(context.Background(), nil); return e },
		func() error { _, e := r.DigitizedByCommune(context.Background(), nil); return e },
		func() error { _, e := r.Confirmations(context.Background(), nil); return e },
		func() error { _, e := r.ConfirmationsAt(context.Background(), nil, time.Time{}); return e },
	} {
		if e := call(); !errors.Is(e, tenant.ErrMissing) {
			t.Fatalf("missing tenant: %v", e)
		}
	}
}

// Real node projections force p_scope out of its pre-projection compatibility branch.
func TestKNPublishedHTXPrincipalScopesReconcileNative(t *testing.T) {
	f := newFixture(t)
	reader := extension.NewPublishedHTXData(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform))
	for _, tid := range []uuid.UUID{f.tid, f.other} {
		for _, n := range []int{990, 991, 101, 102, 103, 104, 105} {
			kind, unit := "cooperative", "89001"
			if n >= 990 {
				kind, unit = "ttkn_province", "89"
			}
			if n == 103 {
				unit = "92001"
			}
			f.exec(`INSERT INTO k_ref.org(tenant_id,ref,kind,code,admin_unit_code,active,source_seq)
    VALUES($1,$2,$3,$4,$5,true,1)`, tid, ref(n), kind, fmt.Sprintf("N%d", n), unit)
			f.exec("INSERT INTO k_ref.node_closure(tenant_id,ancestor,descendant,depth)VALUES($1,$2,$2,0)", tid, ref(n))
			if n < 990 {
				root := 990
				if n >= 103 {
					root = 991
				}
				f.exec("INSERT INTO k_ref.node_closure(tenant_id,ancestor,descendant,depth)VALUES($1,$2,$3,1)", tid, ref(root), ref(n))
			}
		}
		principals := []authz.Principal{
			{ActorRef: ref(900), OrgRef: ref(990), Roles: []string{kn.RoleKNOfficer}},
			{ActorRef: ref(900), OrgRef: ref(101), Roles: []string{kn.RoleCoopManager}},
			{ActorRef: ref(900), KN: &authz.KNContext{Role: authz.KNXa, OrgRef: ref(990), Department: "trong_trot", Communes: []string{"89001"}, Scope: authz.Scope{Units: []string{"89001"}}}},
			{ActorRef: ref(900), KN: &authz.KNContext{Role: authz.KNXa, OrgRef: ref(998), Department: "trong_trot", Communes: []string{"89001"}, Scope: authz.Scope{Units: []string{"89001"}}}},
			{ActorRef: ref(900), KN: &authz.KNContext{Role: authz.KNTinh, OrgRef: ref(990), Department: "trong_trot", Scope: authz.Scope{Units: []string{"89"}}}},
			{ActorRef: ref(900), KN: &authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}}},
		}
		for _, p := range principals {
			ctx := authz.WithPrincipal(tenant.WithID(context.Background(), tid), p)
			for _, units := range [][]string{nil, {}, {"89001"}, {"92001"}, {"unknown"}} {
				native, e := f.native.Cooperatives().ByUnit(ctx, units)
				if e != nil {
					t.Fatal(e)
				}
				want := []kn.UnitCooperatives{}
				for _, v := range native {
					want = append(want, kn.UnitCooperatives{UnitCode: v.UnitCode, Cooperatives: v.Cooperatives, Households: v.Households})
				}
				got, e := reader.ByUnit(ctx, units)
				if e != nil {
					t.Fatal(e)
				}
				equal(t, got, want)
				plots, e := f.native.Plots().DigitizedByCommune(ctx, units)
				if e != nil {
					t.Fatal(e)
				}
				wantPlots := []kn.CommunePlots{}
				for _, v := range plots {
					wantPlots = append(wantPlots, kn.CommunePlots{CommuneCode: v.CommuneCode, Plots: v.Plots, AreaHa: v.AreaHa})
				}
				gotPlots, e := reader.DigitizedByCommune(ctx, units)
				if e != nil {
					t.Fatal(e)
				}
				equal(t, gotPlots, wantPlots)
				if tid == f.tid && p.KN != nil && p.KN.Role == authz.KNXa && units == nil {
					if len(gotPlots) != 1 || gotPlots[0].CommuneCode != "89001" || gotPlots[0].Plots != 3 {
						t.Fatalf("commune RLS escaped: %+v", gotPlots)
					}
				}
			}
		}
	}
}
