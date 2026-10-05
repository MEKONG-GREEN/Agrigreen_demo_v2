package migrations_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// pf 000089 (W2.2, DB-06, ADR-0032): EXCLUDE trên pf_masterdata.admin_unit, bảng
// admin_unit_succession chỉ ghi thêm có RLS, điền từ successor_codes.

const succMigration = "pf/000089_pf_masterdata_admin_unit_succession.up.sql"

// insertUnit ghi một phiên bản đơn vị (đường ghi thường, trigger bật).
func insertUnit(ctx context.Context, tx pgx.Tx, tid uuid.UUID, code, from string, to *string, level, parent string, succ []string) error {
	var p *string
	if parent != "" {
		p = &parent
	}
	if succ == nil {
		succ = []string{}
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_masterdata.admin_unit
	    (tenant_id, code, valid_from, valid_to, name, level, parent_code, successor_codes, source, actor_ref)
	  VALUES ($1, $2, $3::date, $4::date, $2, $5, $6, $7::text[], 'kiểm thử 000089', $1)`, tid, code, from, to, level, p, succ)
	return err
}

func strp(s string) *string { return &s }

// Hai phiên bản cùng (tenant, mã) chồng hiệu lực bị DB từ chối ngay cả khi trigger
// fn_admin_unit_guard bị vòng qua (session_replication_role = replica); valid_to NULL = vô hạn.
func TestAdminUnitExcludeRejectsOverlapWithoutTrigger(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	admin := env.AdminPool(t, migrations.PF)
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
		t.Fatal(err)
	}
	ta, tb := ids.New(), ids.New()
	if err := insertUnit(ctx, tx, ta, "KT89", "2020-01-01", nil, "province", "", nil); err != nil {
		t.Fatal(err)
	}
	// Tenant khác, cùng mã: được.
	if err := insertUnit(ctx, tx, tb, "KT89", "2020-01-01", nil, "province", "", nil); err != nil {
		t.Fatalf("tenant khác cùng mã phải được: %v", err)
	}
	if _, err := tx.Exec(ctx, "SAVEPOINT s"); err != nil {
		t.Fatal(err)
	}
	err = insertUnit(ctx, tx, ta, "KT89", "2030-01-01", nil, "province", "", nil)
	var pe *pgconn.PgError
	if pgCode(err) != "23P01" || !errors.As(err, &pe) || pe.ConstraintName != "admin_unit_no_overlap" {
		t.Fatalf("chồng lấp với phiên bản mở (vô hạn) phải bị EXCLUDE từ chối, nhận %v", err)
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT s"); err != nil {
		t.Fatal(err)
	}
	// Khoảng [) liền kề không chồng: [2010, 2020) với [2020, ∞).
	if err := insertUnit(ctx, tx, ta, "KT89", "2010-01-01", strp("2020-01-01"), "province", "", nil); err != nil {
		t.Fatalf("khoảng liền kề phải được: %v", err)
	}
}

// Câu INSERT … SELECT điền từ successor_codes (giữa hai dấu backfill:begin/end của
// migration) tạo đúng dòng 'pending_c9', bỏ qua mã kế thừa không có hiệu lực, chạy lặp
// không trùng.
func TestAdminUnitSuccessionBackfill(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	raw, err := migrations.FS.ReadFile(succMigration)
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	b, e := strings.Index(src, "-- backfill:begin"), strings.Index(src, "-- backfill:end")
	if b < 0 || e < b {
		t.Fatal("thiếu dấu backfill:begin/end trong migration")
	}
	backfill := src[b:e]
	if strings.Contains(strings.ToUpper(backfill), "UPDATE ") {
		t.Fatal("điền dữ liệu không được UPDATE")
	}

	admin := env.AdminPool(t, migrations.PF)
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tid := ids.New()
	// Dữ liệu như trước 000089: tỉnh P cũ đóng ngày 2025-07-01, kế thừa Q (và một mã
	// không có hiệu lực ngày đó — bị bỏ qua); Q mở từ 2025-07-01.
	for _, u := range []struct {
		code, from string
		to         *string
		succ       []string
	}{
		{"KTQ", "2025-07-01", nil, nil},
		{"KTP", "2000-01-01", strp("2025-07-01"), []string{"KTQ", "KTKHONG"}},
	} {
		if err := insertUnit(ctx, tx, tid, u.code, u.from, u.to, "province", "", u.succ); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 { // chạy lặp: ON CONFLICT DO NOTHING
		if _, err := tx.Exec(ctx, backfill); err != nil {
			t.Fatalf("backfill: %v", err)
		}
	}
	rows, err := tx.Query(ctx, `SELECT predecessor_code, predecessor_valid_from::text, successor_code, successor_valid_from::text,
	    effective_on::text, area_share IS NULL, legal_basis, basis_status
	  FROM pf_masterdata.admin_unit_succession WHERE tenant_id = $1`, tid)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var pc, pf, sc, sf, eff, basis, status string
		var noShare bool
		if err := rows.Scan(&pc, &pf, &sc, &sf, &eff, &noShare, &basis, &status); err != nil {
			t.Fatal(err)
		}
		got = append(got, strings.Join([]string{pc, pf, sc, sf, eff, basis, status}, "|"))
		if !noShare {
			t.Error("area_share phải NULL khi điền từ successor_codes")
		}
	}
	rows.Close()
	want := "KTP|2000-01-01|KTQ|2025-07-01|2025-07-01|successor_codes (pf 000002)|pending_c9"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("điền: %v; muốn [%s]", got, want)
	}
}

// Bảng kế thừa: RLS theo tenant, chỉ ghi thêm (vai ứng dụng và owner), trigger kiểm
// tiền thân đã đóng đúng ngày; policy tạm của migration đã bỏ.
func TestAdminUnitSuccessionRLSAndImmutability(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	app := env.Pool(t, migrations.PF, "masterdata_app")
	owner := env.Pool(t, migrations.PF, dbadmin.Migrator)
	ta, tb := ids.New(), ids.New()
	ins := `INSERT INTO pf_masterdata.admin_unit_succession
	    (tenant_id, predecessor_code, predecessor_valid_from, successor_code, successor_valid_from,
	     effective_on, area_share, legal_basis, basis_status, actor_ref)
	  VALUES ($1, $2, $3::date, $4, $5::date, $6::date, $7::numeric, 'NQ kiểm thử', $8, $1)`
	err := tenant.WithTenantTx(ctx, app, ta, func(tx pgx.Tx) error {
		for _, u := range []struct {
			code, from string
			to         *string
			succ       []string
		}{
			{"KTB", "2025-07-01", nil, nil},
			{"KTC", "2025-07-01", nil, nil},
			{"KTA", "2000-01-01", strp("2025-07-01"), []string{"KTB", "KTC"}},
		} {
			if err := insertUnit(ctx, tx, ta, u.code, u.from, u.to, "province", "", u.succ); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, ins, ta, "KTA", "2000-01-01", "KTB", "2025-07-01", "2025-07-01", "0.6", "official"); err != nil {
			return err
		}
		// pending_c9 và official cùng cặp cùng tồn tại (bản ghi bù).
		_, err := tx.Exec(ctx, ins, ta, "KTA", "2000-01-01", "KTB", "2025-07-01", "2025-07-01", nil, "pending_c9")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Trigger: effective_on khác valid_to của tiền thân; kế thừa chưa có hiệu lực.
	for _, bad := range [][]any{
		{ta, "KTA", "2000-01-01", "KTC", "2025-07-01", "2025-08-01", nil, "official"},
		{ta, "KTB", "2025-07-01", "KTC", "2025-07-01", "2025-07-01", nil, "official"},
		{ta, "KTA", "2000-01-01", "KTC", "2025-07-01", "2025-07-01", "1.5", "official"},
		{ta, "KTA", "2000-01-01", "KTC", "2025-07-01", "2025-07-01", nil, "draft"},
	} {
		err := tenant.WithTenantTx(ctx, app, ta, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, ins, bad...); return e })
		if c := pgCode(err); c != "23514" {
			t.Errorf("dòng sai %v phải bị từ chối 23514, nhận %v", bad[1:], err)
		}
	}
	// RLS: tenant khác không thấy, không ghi được dòng của tenant A.
	var n int
	if err := tenant.WithTenantTx(ctx, app, tb, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM pf_masterdata.admin_unit_succession WHERE tenant_id = $1", ta).Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("tenant B thấy %d dòng của A (%v)", n, err)
	}
	if err := tenant.WithTenantTx(ctx, app, ta, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM pf_masterdata.admin_unit_succession").Scan(&n)
	}); err != nil || n != 2 {
		t.Fatalf("tenant A thấy %d dòng (%v), muốn 2", n, err)
	}
	err = tenant.WithTenantTx(ctx, app, tb, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, ins, ta, "KTA", "2000-01-01", "KTC", "2025-07-01", "2025-07-01", nil, "official")
		return e
	})
	// Trigger BEFORE INSERT chạy trước WITH CHECK và dưới RLS không thấy admin_unit của tenant
	// A, nên lớp chặn đầu tiên là 23514; nếu trigger bị vòng qua thì WITH CHECK trả 42501.
	if c := pgCode(err); c != "23514" && c != "42501" {
		t.Fatalf("ghi dòng tenant khác phải bị từ chối, nhận %v", err)
	}
	// Chỉ ghi thêm: vai ứng dụng không có quyền; owner bị trigger chặn.
	for _, q := range []string{
		"UPDATE pf_masterdata.admin_unit_succession SET legal_basis = 'sửa'",
		"DELETE FROM pf_masterdata.admin_unit_succession",
	} {
		err := tenant.WithTenantTx(ctx, app, ta, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, q); return e })
		if c := pgCode(err); c != "42501" {
			t.Errorf("masterdata_app %q phải bị từ chối quyền, nhận %v", q, err)
		}
		err = tenant.WithTenantTx(ctx, owner, ta, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, q); return e })
		if err == nil {
			t.Errorf("owner %q phải bị trigger chặn", q)
		}
	}
	// Cổng RLS, policy tạm đã bỏ.
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM shared.v_missing_rls
	    WHERE schema_name = 'pf_masterdata' AND table_name IN ('admin_unit', 'admin_unit_succession')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("v_missing_rls: %d (%v)", n, err)
	}
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM pg_policy WHERE polname = 'p_migrate_000089'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("policy tạm còn: %d (%v)", n, err)
	}
}
