package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// CNTS-V4-OUTBREAK-MULTI (#252, ADR-0056 mục 8): pf 20261106200819 thêm bảng con cnts_outbreak_household và ba cột cho phép
// NULL vào sổ cnts_outbreak. Ổ dịch cũ một hộ (rule_version v1) không chuyển đổi: số ổ dịch, số phiên bản, số phiên bản
// v1, tổng quy mô đàn / số con chết trước và sau migration bằng nhau; cột mới NULL; verify_chain sạch; ổ dịch v2 ghi được
// sau nâng cấp, v2 thiếu DB-15 hoặc còn household_ref bị ràng buộc chặn.

const pfBeforeOutbreakMulti = 20261106164138 // pf_extension_cnts_livestock_household (#251) — migration liền trước

type outbreakCounts struct {
	rows, outbreaks, v1, herd, dead int64
}

func countOutbreaks(t *testing.T, pool *pgxpool.Pool, tid uuid.UUID) outbreakCounts {
	t.Helper()
	ctx := context.Background()
	var c outbreakCounts
	if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*), count(DISTINCT outbreak_ref),
		    count(*) FILTER (WHERE rule_version = 'kn.cnts.outbreak/v1'), coalesce(sum(herd_size), 0), coalesce(sum(dead), 0)
		  FROM pf_extension.cnts_outbreak`).Scan(&c.rows, &c.outbreaks, &c.v1, &c.herd, &c.dead)
	}); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestOutbreakMultiUpgradeKeepsLegacy(t *testing.T) {
	ctx := context.Background()
	pool, dsn := upgradeEnv(t, migrations.PF, pfBeforeOutbreakMulti)
	tid := ids.New()
	ins := func(tx pgx.Tx, ref uuid.UUID, event string, herd, dead int, hh *uuid.UUID, locality, reason *string) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_outbreak (id, tenant_id, stream_key, outbreak_ref, event, commune_code,
		    disease_code, species_code, received_at, source, household_ref, locality, location, location_source, herd_size, dead,
		    symptoms, as_of, status_reason, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, $3, $4, $5, '30001', 'dtlcp', 'heo', $6, 'can_bo', $7, $8, ST_SetSRID(ST_MakePoint(105.97, 10.25), 4326),
		    'gps', $9, $10, 'Sốt', '2027-10-10', $11, $6, $12, $13, 'kn.cnts.outbreak/v1')`,
			ids.New(), tid, "outbreak:"+ref.String(), ref, event, time.Now().UTC().Add(-time.Hour), hh, locality, herd, dead, reason,
			ids.New(), ids.New().String())
		return err
	}
	o1, o2, hh := ids.New(), ids.New(), ids.New()
	ap, why := "Ấp cũ", "Hết dịch"
	if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
		for _, step := range []func() error{
			func() error { return ins(tx, o1, "reported", 120, 5, &hh, nil, nil) },
			func() error { return ins(tx, o1, "updated", 120, 30, &hh, nil, nil) },
			func() error { return ins(tx, o1, "closed", 120, 30, &hh, nil, &why) },
			func() error { return ins(tx, o2, "reported", 40, 2, nil, &ap, nil) },
		} {
			if err := step(); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := countOutbreaks(t, pool, tid)
	if before != (outbreakCounts{rows: 4, outbreaks: 2, v1: 4, herd: 400, dead: 67}) {
		t.Fatalf("trước nâng cấp: %+v", before)
	}
	if got := verifyAll(t, pool); got["pf_extension.cnts_outbreak"] != 4 {
		t.Fatalf("trước nâng cấp: số dòng đã kiểm %v", got)
	}

	migrateDir(t, dsn, "pf", 0) // nâng cấp lên đầu: bảng con + cột DB-15, DB-16

	after := countOutbreaks(t, pool, tid)
	if after != before {
		t.Fatalf("migration đổi dữ liệu cũ: trước %+v, sau %+v", before, after)
	}
	var set, children int64
	if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.cnts_outbreak
		  WHERE affected_households IS NOT NULL OR aqua_farmed_ha IS NOT NULL OR aqua_wild_ha IS NOT NULL`).Scan(&set); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.cnts_outbreak_household`).Scan(&children)
	}); err != nil {
		t.Fatal(err)
	}
	if set != 0 || children != 0 {
		t.Fatalf("không chuyển dữ liệu: cột mới có giá trị ở %d dòng, bảng con %d dòng", set, children)
	}
	if got := verifyAll(t, pool); got["pf_extension.cnts_outbreak"] != 4 {
		t.Fatalf("sau nâng cấp, trước khi ghi thêm: số dòng đã kiểm %v", got)
	}

	// Sau nâng cấp: phiên bản v2 (DB-15, DB-16) ghi được trên cùng chuỗi; v2 thiếu DB-15 hoặc còn household_ref → chặn.
	insV2 := func(tx pgx.Tx, affected *int, hhRef *uuid.UUID) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_outbreak (id, tenant_id, stream_key, outbreak_ref, event, commune_code,
		    disease_code, species_code, received_at, source, household_ref, location, location_source, herd_size, dead, symptoms,
		    as_of, event_time, actor_ref, idem_key, rule_version, affected_households, aqua_farmed_ha, aqua_wild_ha)
		  VALUES ($1, $2, $3, $4, 'updated', '30001', 'dtlcp', 'heo', $5, 'can_bo', $6, ST_SetSRID(ST_MakePoint(105.97, 10.25), 4326),
		    'gps', 40, 3, 'Sốt', '2027-10-11', $5, $7, $8, 'kn.cnts.outbreak/v2', $9, 1.5, 0)`,
			ids.New(), tid, "outbreak:"+o2.String(), o2, time.Now().UTC(), hhRef, ids.New(), ids.New().String(), affected)
		return err
	}
	one := 1
	for name, bad := range map[string]func(pgx.Tx) error{
		"v2 thiếu DB-15":      func(tx pgx.Tx) error { return insV2(tx, nil, nil) },
		"v2 có household_ref": func(tx pgx.Tx) error { return insV2(tx, &one, &hh) },
	} {
		if err := tenant.WithTenantTx(ctx, pool, tid, bad); err == nil {
			t.Errorf("%s: phải bị ràng buộc cnts_outbreak_db04 chặn", name)
		}
	}
	if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error { return insV2(tx, &one, nil) }); err != nil {
		t.Fatal(err)
	}
	if got := verifyAll(t, pool); got["pf_extension.cnts_outbreak"] != 5 {
		t.Fatalf("sau nâng cấp: số dòng đã kiểm %v", got)
	}
}
