package migrations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Cổng W5.6 (ADR-0034 §1, §3, §5 — "không bao giờ tổng hợp"), cụm pf sau khi migrate tới head, với hai sổ song phương
// pf_coop.bilateral_pair_event, pf_coop.bilateral_record:
//
//  1. chỉ vai nhóm của module chủ (r_cooperative) có quyền, và chỉ SELECT + INSERT — không r_agg, không vai module khác,
//     không vai đăng nhập trực tiếp (quyền hiệu lực qua membership r_cooperative được phép);
//  2. không view / view vật chất hóa nào (kể cả *_pub) đọc hai bảng;
//  3. đúng một chính sách, PERMISSIVE, mang biểu thức hai bên (counterparty_tenant_id + app.node_id), WITH CHECK ràng
//     phía tác giả; FORCE RLS.
func TestBilateralLedgersIsolated_W56(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	admin := env.AdminPool(t, migrations.PF)
	tables := []string{"pf_coop.bilateral_pair_event", "pf_coop.bilateral_record"}
	for _, tbl := range tables {
		rows, err := admin.Query(ctx, `SELECT grantee, privilege_type FROM information_schema.role_table_grants
		  WHERE table_schema || '.' || table_name = $1 AND grantee <> (SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid = $1::regclass)`, tbl)
		if err != nil {
			t.Fatal(err)
		}
		var bad []string
		for rows.Next() {
			var g, p string
			if err := rows.Scan(&g, &p); err != nil {
				t.Fatal(err)
			}
			if g != "r_cooperative" || (p != "SELECT" && p != "INSERT") {
				bad = append(bad, g+" "+p)
			}
		}
		rows.Close()
		if len(bad) > 0 {
			t.Errorf("%s: quyền ngoài r_cooperative SELECT/INSERT: %v", tbl, bad)
		}
		var aggSel bool
		if err := admin.QueryRow(ctx, `SELECT has_table_privilege('r_agg', $1::regclass, 'SELECT')`, tbl).Scan(&aggSel); err != nil {
			t.Fatal(err)
		}
		if aggSel {
			t.Errorf("%s: r_agg không được đọc (ADR-0034 §5)", tbl)
		}
		var views []string
		vr, err := admin.Query(ctx, `SELECT DISTINCT v.oid::regclass::text FROM pg_depend d JOIN pg_rewrite rw ON rw.oid = d.objid
		  JOIN pg_class v ON v.oid = rw.ev_class WHERE d.refobjid = $1::regclass AND v.oid <> $1::regclass`, tbl)
		if err != nil {
			t.Fatal(err)
		}
		for vr.Next() {
			var v string
			if err := vr.Scan(&v); err != nil {
				t.Fatal(err)
			}
			views = append(views, v)
		}
		vr.Close()
		if len(views) > 0 {
			t.Errorf("%s: không view nào được đọc sổ song phương: %v", tbl, views)
		}
		var n int
		var permissive, force bool
		var qual, check string
		if err := admin.QueryRow(ctx, `SELECT count(*) OVER (), p.polpermissive, c.relforcerowsecurity,
		    pg_get_expr(p.polqual, p.polrelid), pg_get_expr(p.polwithcheck, p.polrelid)
		  FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid WHERE p.polrelid = $1::regclass AND p.polname = 'p_tenant'`, tbl).
			Scan(&n, &permissive, &force, &qual, &check); err != nil {
			t.Fatalf("%s: chính sách p_tenant: %v", tbl, err)
		}
		var total int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_policy WHERE polrelid = $1::regclass AND polpermissive`, tbl).Scan(&total); err != nil {
			t.Fatal(err)
		}
		if total != 1 || !permissive || !force || !strings.Contains(qual, "counterparty_tenant_id") || !strings.Contains(qual, "app.node_id") ||
			!strings.Contains(check, "author_side") {
			t.Errorf("%s: một chính sách hai bên (permissive=%v, force=%v, tổng permissive=%d): USING %s WITH CHECK %s", tbl, permissive, force, total, qual, check)
		}
	}
}
