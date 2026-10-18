package htxdata_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	kn "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
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
