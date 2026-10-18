package platformdata_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	pp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func sameResult(t *testing.T, got any, ge error, want any, we error) {
	t.Helper()
	equal(t, result(got, ge), result(want, we))
	if errors.Is(ge, pp.ErrUnitNotFound) != errors.Is(we, pp.ErrUnitNotFound) || errors.Is(ge, pp.ErrOrgNotFound) != errors.Is(we, pp.ErrOrgNotFound) {
		t.Fatalf("lost public not-found cause: %v/%v", ge, we)
	}
}
func TestKNPublishedAdminUnitsReconcileNative(t *testing.T) {
	f := newFixture(t)
	r := extension.NewPublishedAdminUnits(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform))
	nativeTree := f.units.(pp.AdminUnitTree)
	checks := 0
	for _, tid := range []uuid.UUID{f.tid, f.other, ids.New()} {
		ctx := tenant.WithID(context.Background(), tid)
		for _, ds := range []string{"2023-01-01T00:00:00Z", "2025-06-30T16:59:59Z", "2025-06-30T17:00:00Z", "2026-06-30T16:59:59Z", "2026-06-30T17:00:00Z"} {
			at, err := time.Parse(time.RFC3339, ds)
			if err != nil {
				t.Fatal(err)
			}
			for _, code := range []string{"89", "89001", "89011", "89012", "89013", "89100", "99999"} {
				got, ge := r.Get(ctx, code, at)
				want, we := f.units.Get(ctx, code, at)
				sameResult(t, got, ge, want, we)
				checks++
				gc, gce := r.Current(ctx, code, at)
				wc, wce := f.units.Current(ctx, code, at)
				sameResult(t, gc, gce, wc, wce)
				checks++
			}
			for _, code := range []string{"89", "92", "89001", "99999"} {
				got, ge := r.Descendants(ctx, code, at)
				want, we := f.units.Descendants(ctx, code, at)
				sameResult(t, got, ge, want, we)
				checks++
			}
			for _, parent := range []string{"", "89", "92", "99999"} {
				got, ge := r.Children(ctx, parent, at)
				want, we := nativeTree.Children(ctx, parent, at)
				sameResult(t, got, ge, want, we)
				checks++
			}
		}
	}
	t.Logf("%d metadata comparisons, including effective-date VN boundaries and public errors", checks)
}

func TestKNPublishedOrganizationsReconcileNative(t *testing.T) {
	f := newFixture(t)
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	r := extension.NewPublishedOrganizations(pool)
	native := party.New(pool, clock.System{}).Orgs()
	codes := native.(pp.OrgCodes)
	checks := 0
	for _, tid := range []uuid.UUID{f.tid, f.other, ids.New()} {
		ctx := tenant.WithID(context.Background(), tid)
		for _, id := range []uuid.UUID{ref(101), ref(102), ref(103), ref(104), ref(105), ref(106), ref(107), ref(999), uuid.Nil} {
			got, ge := r.Get(ctx, id)
			want, we := native.Get(ctx, id)
			sameResult(t, got, ge, want, we)
			checks++
		}
		for _, code := range []string{"O101", "O103", "O104", "O105", "missing", ""} {
			got, ge := r.ByCode(ctx, code)
			want, we := codes.ByCode(ctx, code)
			sameResult(t, got, ge, want, we)
			checks++
		}
		for _, refs := range [][]uuid.UUID{nil, {}, {ref(101), ref(103), ref(104), ref(105), ref(999), ref(101)}} {
			got, ge := r.OrgsByRefs(ctx, refs)
			want, we := f.dir.OrgsByRefs(ctx, refs)
			sameResult(t, got, ge, want, we)
			checks++
		}
		for _, kind := range []pp.OrgKind{pp.OrgCooperative, pp.OrgTTKNNational, pp.OrgTTKNProvince, pp.OrgPartner, "missing"} {
			for _, units := range [][]string{nil, {}, {"89011"}, {"89012"}, {"92001"}, {""}, {"unknown"}, {"89011", "92001", "89011"}} {
				for _, active := range []bool{false, true} {
					got, ge := r.OrgsOfKind(ctx, kind, units, active)
					want, we := f.dir.OrgsOfKind(ctx, kind, units, active)
					sameResult(t, got, ge, want, we)
					checks++
				}
			}
		}
	}
	t.Logf("%d metadata comparisons, including nil/empty/bulk filters, inactive and duplicate references", checks)
}

func TestKNPublishedMetadataDoesNotExposeRepresentative(t *testing.T) {
	f := newFixture(t)
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	f.exec("UPDATE pf_party.org SET representative_ref=$3 WHERE tenant_id=$1 AND ref=$2", f.tid, ref(103), ref(9001))
	ctx := tenant.WithID(context.Background(), f.tid)
	native, e := party.New(pool, clock.System{}).Orgs().Get(ctx, ref(103))
	if e != nil {
		t.Fatal(e)
	}
	if native.RepresentativeRef != ref(9001) {
		t.Fatal("canary not in native source")
	}
	native.RepresentativeRef = uuid.Nil
	r := extension.NewPublishedOrganizations(pool)
	got, e := r.Get(ctx, ref(103))
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, native)
	got, e = r.ByCode(ctx, "O103")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, native)
	refs, e := r.OrgsByRefs(ctx, []uuid.UUID{ref(103)})
	if e != nil {
		t.Fatal(e)
	}
	equal(t, refs[ref(103)], native)
	rows, e := r.OrgsOfKind(ctx, pp.OrgCooperative, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	for _, o := range rows {
		if o.RepresentativeRef != uuid.Nil {
			t.Fatal("representative leaked through public directory")
		}
	}
}

func TestKNPublishedUnitsLargeChildrenAndSuccessorBound(t *testing.T) {
	f := newFixture(t)
	ctx := tenant.WithID(context.Background(), f.tid)
	f.exec(`INSERT INTO pf_masterdata.admin_unit(tenant_id,code,valid_from,name,level,parent_code,successor_codes,source,actor_ref)
  SELECT $1,'U'||lpad(n::text,4,'0'),'2026-01-01','Many commune','commune','89','{}'::text[],'reader-large-test',$2
  FROM generate_series(1,1001)n`, f.tid, ref(900))
	r := extension.NewPublishedAdminUnits(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform))
	at := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	got, ge := r.Children(ctx, "89", at)
	want, we := f.units.(pp.AdminUnitTree).Children(ctx, "89", at)
	sameResult(t, got, ge, want, we)
	if len(got) != 1004 {
		t.Fatalf("children truncated: %d", len(got))
	}
	f.exec(`INSERT INTO pf_masterdata.admin_unit(tenant_id,code,valid_from,valid_to,name,level,parent_code,successor_codes,source,actor_ref)
  SELECT $1,'C'||n,date '2020-01-01'+n,CASE WHEN n<66 THEN date '2020-01-01'+n+1 END,
    'Long chain','commune','89',CASE WHEN n<66 THEN ARRAY['C'||(n+1)] ELSE '{}'::text[] END,'reader-chain-test',$2
  FROM generate_series(0,66)n`, f.tid, ref(900))
	gc, gce := r.Current(ctx, "C0", at)
	wc, wce := f.units.Current(ctx, "C0", at)
	sameResult(t, gc, gce, wc, wce)
	if gce == nil {
		t.Fatal("successor bound not enforced")
	}
}

type unavailableDB struct {
	opts *pgx.TxOptions
	err  error
}

func (b unavailableDB) BeginTx(_ context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	*b.opts = opts
	return nil, b.err
}
func TestKNPublishedMetadataFailsClosed(t *testing.T) {
	failure := errors.New("metadata publications unavailable")
	opts := pgx.TxOptions{}
	db := unavailableDB{&opts, failure}
	u := extension.NewPublishedAdminUnits(db)
	o := extension.NewPublishedOrganizations(db)
	tid := ids.New()
	at := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	calls := []func(context.Context) error{
		func(ctx context.Context) error { _, e := u.Get(ctx, "89", at); return e },
		func(ctx context.Context) error { _, e := u.Children(ctx, "", at); return e },
		func(ctx context.Context) error { _, e := u.Descendants(ctx, "89", at); return e },
		func(ctx context.Context) error { _, e := u.Current(ctx, "89001", at); return e },
		func(ctx context.Context) error { _, e := o.Get(ctx, ref(103)); return e },
		func(ctx context.Context) error { _, e := o.ByCode(ctx, "O103"); return e },
		func(ctx context.Context) error { _, e := o.OrgsByRefs(ctx, nil); return e },
		func(ctx context.Context) error {
			_, e := o.OrgsOfKind(ctx, pp.OrgCooperative, []string{}, true)
			return e
		},
	}
	for i, call := range calls {
		e := call(tenant.WithID(context.Background(), tid))
		if !errors.Is(e, failure) || opts.AccessMode != pgx.ReadOnly {
			t.Fatalf("call%d swallowed source failure or opened write transaction: %v %+v", i, e, opts)
		}
		if e = call(context.Background()); !errors.Is(e, tenant.ErrMissing) {
			t.Fatalf("call%d missing tenant: %v", i, e)
		}
	}
}
