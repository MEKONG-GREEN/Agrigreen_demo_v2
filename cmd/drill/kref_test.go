package main

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// W2.5 (ADR-0029 §5): bước `drill kref` trên cụm pf đã "phục hồi" (k_ref trống): trước lệch nguồn (chỉ báo),
// sau dựng lại khớp bản dựng độc lập; chạy lại thì không còn lệch trước.
func TestDrillKRefRebuildMatchesSource_W25(t *testing.T) {
	env := pgtest.Get(t)
	ctx := context.Background()
	tid := ids.New()
	if _, err := masterdata.New(env.Pool(t, migrations.PF, masterdata.DBRole), clock.System{}).Seed(ctx, tid); err != nil {
		t.Fatal(err)
	}
	// Nguồn cây Nút ghi thẳng bảng hiện hành (như dữ liệu có trước W2.5, chưa từng phát sự kiện created).
	party := env.Pool(t, migrations.PF, "party_app")
	root, prov := ids.New(), ids.New()
	if err := tenant.WithTenantTx(ctx, party, tid, func(tx pgx.Tx) error {
		for _, o := range []struct {
			ref, parent uuid.UUID
			kind, code  string
		}{{root, uuid.Nil, "ttkn_national", "QG"}, {prov, root, "ttkn_province", "P92"}, {ids.New(), prov, "extension_team", "T1"}} {
			var parent *uuid.UUID
			if o.parent != uuid.Nil {
				parent = &o.parent
			}
			if _, err := tx.Exec(ctx, `INSERT INTO pf_party.org (tenant_id, ref, kind, code, name, parent_ref, active, last_event_id, last_seq)
			  VALUES ($1, $2, $3, $4, $4, $5, true, $6, 1)`, tid, o.ref, o.kind, o.code, parent, ids.New()); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// P0-HTX-KREF projects source INSERTs synchronously. Explicitly emulate a
	// restored legacy tenant with a missing projection so this drill still proves
	// Before=0 -> After=6; never weaken the production trigger or expected drift.
	admin := env.AdminPool(t, migrations.PF)
	if _, err := admin.Exec(ctx, `DELETE FROM k_ref.node_closure WHERE tenant_id=$1`, tid); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `DELETE FROM k_ref.org WHERE tenant_id=$1`, tid); err != nil {
		t.Fatal(err)
	}
	pool := func(role string) (*pgxpool.Pool, error) { return env.Pool(t, migrations.PF, role), nil }
	cfg := KRefConfig{Pool: pool, Tenants: []uuid.UUID{tid}, Database: "ag_pf"}

	rep, err := RunKRef(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK || rep.Drifted == 0 || len(rep.Tenants) != 1 {
		t.Fatalf("lần 1 (k_ref trống): %+v", rep)
	}
	byRel := map[string]KRefRelation{}
	for _, r := range rep.Tenants[0].Relations {
		byRel[r.Relation] = r
	}
	if r := byRel["node_closure"]; !r.Match || r.After.Rows != 6 || r.Before.Rows != 0 {
		t.Fatalf("node_closure: %+v", r)
	}
	if r := byRel["admin_unit"]; !r.Match || r.After.Rows == 0 {
		t.Fatalf("admin_unit: %+v", r)
	}
	if !strings.Contains(rep.Markdown(), "ĐẠT") {
		t.Fatal(rep.Markdown())
	}
	rep, err = RunKRef(ctx, cfg)
	if err != nil || !rep.OK || rep.Drifted != 0 {
		t.Fatalf("lần 2: %+v %v", rep, err)
	}
}
