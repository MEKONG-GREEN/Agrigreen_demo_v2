package migrations_test

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Cổng CI W2.6 (ADR-0028 §2 "truy vấn ngoài WithContextTx thất bại", R-DB-02), phần động — mọi cụm:
//
//  1. mọi vai đăng nhập đơn vị app_* KHÔNG có quyền hiệu lực nào trên bất kỳ bảng/khung nhìn/chuỗi số/cột nào
//     ngoài schema hệ thống và đối tượng của extension (PostGIS cấp PUBLIC) — NOINHERIT, không thuộc ag_shared_user;
//  2. truy vấn thẳng trên pool app_* (ngoài giao dịch của module) vào bảng của MỌI schema module bị 42501;
//     cùng truy vấn trong giao dịch db.ForModule của module chủ chạy được;
//  3. vai cũ <module>_app không còn quyền TRỰC TIẾP nào (pf 000119, mkg 000035, vault 000003) — chỉ qua
//     membership r_* (lùi bản một kỳ).
func TestLoginRolesQueryOutsideModuleTxFails_ADR0028(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	for _, c := range migrations.Clusters {
		admin := env.AdminPool(t, c)
		for _, u := range dbadmin.LoginUnits {
			if !slices.Contains(u.Clusters, c) {
				continue
			}
			var n int
			var sample *string
			if err := admin.QueryRow(ctx, `
				SELECT count(*), min(c.oid::regclass::text) FROM pg_class c JOIN pg_namespace ns ON ns.oid = c.relnamespace
				 WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')
				   AND ns.nspname NOT IN ('pg_catalog', 'information_schema') AND ns.nspname NOT LIKE 'pg\_%'
				   AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e')
				   AND (CASE WHEN c.relkind = 'S' THEN has_sequence_privilege($1, c.oid, 'USAGE, SELECT, UPDATE')
				             ELSE has_table_privilege($1, c.oid, 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER')
				                  OR has_any_column_privilege($1, c.oid, 'SELECT, INSERT, UPDATE, REFERENCES') END)`,
				u.Role).Scan(&n, &sample); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("%s/%s: %s có quyền trên %d quan hệ (vd %v) — muốn 0", c, u.Role, u.Role, n, *sample)
			}
		}

		// 2. Mỗi schema module của cụm: một bảng mẫu, đọc bằng vai đăng nhập của đơn vị ghép module đó.
		for _, g := range dbadmin.ClusterGroups(c) {
			unit := unitFor(g.Role, c)
			if unit == "" {
				continue // module chưa đơn vị nào ở cụm này ghép (vd r_kref ở mkg)
			}
			var table *string
			if err := admin.QueryRow(ctx, `
				SELECT min(c.oid::regclass::text) FROM pg_class c JOIN pg_namespace ns ON ns.oid = c.relnamespace
				 WHERE c.relkind IN ('r', 'p') AND NOT c.relispartition AND has_table_privilege($1, c.oid, 'SELECT')
				   AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e')
				   AND ns.nspname NOT IN ('pg_catalog', 'information_schema', 'shared')
				   AND (ns.nspname <> 'k_ref' OR $1 = 'r_kref')`, g.Role).Scan(&table); err != nil {
				t.Fatal(err)
			}
			if table == nil {
				t.Errorf("%s/%s: không có bảng nào đọc được", c, g.Role)
				continue
			}
			p := env.Pool(t, c, unit)
			q := "SELECT count(*) FROM " + *table
			var cnt int64
			if err := p.QueryRow(ctx, q).Scan(&cnt); pgCode(err) != "42501" {
				t.Errorf("%s/%s: %s ngoài giao dịch của module phải 42501, nhận %v", c, unit, q, err)
			}
			m := db.Module(g.Module)
			err := db.WithModuleTenantTx(ctx, p, m, ids.New(), func(tx pgx.Tx) error { return tx.QueryRow(ctx, q).Scan(&cnt) })
			if err != nil {
				t.Errorf("%s/%s: %s trong giao dịch %s: %v", c, unit, q, g.Role, err)
			}
			// Cả shared.* (outbox) ngoài giao dịch module cũng bị từ chối.
			if err := p.QueryRow(ctx, "SELECT count(*) FROM shared.outbox").Scan(&cnt); pgCode(err) != "42501" {
				t.Errorf("%s/%s: shared.outbox ngoài giao dịch phải 42501, nhận %v", c, unit, err)
			}
		}

		// 3. Vai cũ <module>_app: không ACL trực tiếp nào do ag_migrator cấp.
		for _, g := range dbadmin.ClusterGroups(c) {
			if g.Legacy == "" {
				continue
			}
			var n int
			if err := admin.QueryRow(ctx, `
				WITH r AS (SELECT oid FROM pg_roles WHERE rolname = $1)
				SELECT (SELECT count(*) FROM pg_class c, aclexplode(c.relacl) a, r WHERE a.grantee = r.oid)
				     + (SELECT count(*) FROM pg_attribute att, aclexplode(att.attacl) a, r WHERE a.grantee = r.oid)
				     + (SELECT count(*) FROM pg_proc f, aclexplode(f.proacl) a, r WHERE a.grantee = r.oid)
				     + (SELECT count(*) FROM pg_namespace n, aclexplode(n.nspacl) a, r WHERE a.grantee = r.oid)`,
				g.Legacy).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("%s: vai cũ %s còn %d quyền trực tiếp (muốn 0 sau W2.6)", c, g.Legacy, n)
			}
		}
	}
}

// unitFor trả vai đăng nhập (ở cụm c) là thành viên vai nhóm grp.
func unitFor(grp string, c migrations.Cluster) string {
	for _, r := range dbadmin.Roles {
		if r.Login && r.NoInherit && slices.Contains(r.Clusters, c) && slices.Contains(r.MemberOf, grp) {
			return r.Name
		}
	}
	return ""
}
