package migrations_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Cổng W6.4 (ADR-0031 §3, §6, R-DB-11, R-SEC-03), cụm pf sau khi migrate tới head:
//
//  1. r_agg: NOLOGIN, BYPASSRLS; quyền hiệu lực CHỈ trên schema công bố *_pub (USAGE + SELECT view, qua membership
//     r_<module>_pub) và đúng cột tenant_id của k_ref.org (+ USAGE k_ref) — không gì trên schema nội bộ, không quyền ghi;
//  2. thành viên r_agg chỉ là vai đăng nhập riêng app_agg (gói FC, ADR-0031 §3), NOINHERIT, không thuộc vai nào khác;
//  3. danh mục vai BYPASSRLS (ngoài superuser) đúng bảng ADR-0031 §6 — mọi cụm.
func TestSchemaIsolationAggRole_W64(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	admin := env.AdminPool(t, migrations.PF)

	var login, bypass, inherit bool
	if err := admin.QueryRow(ctx, `SELECT rolcanlogin, rolbypassrls, rolinherit FROM pg_roles WHERE rolname = $1`, dbadmin.AggRole).
		Scan(&login, &bypass, &inherit); err != nil {
		t.Fatal(err)
	}
	if login || !bypass || !inherit {
		t.Fatalf("r_agg: login=%v bypassrls=%v inherit=%v", login, bypass, inherit)
	}

	rows, err := admin.Query(ctx, `
		WITH rel AS (SELECT c.oid, n.nspname, c.oid::regclass::text AS name, c.relkind FROM pg_class c
		               JOIN pg_namespace n ON n.oid = c.relnamespace
		              WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S') AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		                AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e'))
		SELECT 'table ' || name || ' ' || p FROM rel, unnest(ARRAY['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER']) p
		 WHERE relkind <> 'S' AND has_table_privilege($1, oid, p)
		   AND NOT (p = 'SELECT' AND relkind = 'v' AND nspname LIKE '%\_pub')
		UNION ALL
		SELECT 'sequence ' || name FROM rel WHERE relkind = 'S' AND has_sequence_privilege($1, oid, 'USAGE, SELECT, UPDATE')
		UNION ALL
		SELECT DISTINCT 'column ' || name || '.' || a.attname || ' ' || p FROM rel JOIN pg_attribute a ON a.attrelid = rel.oid,
		       unnest(ARRAY['SELECT', 'INSERT', 'UPDATE', 'REFERENCES']) p
		 WHERE relkind <> 'S' AND a.attnum > 0 AND NOT a.attisdropped AND has_column_privilege($1, rel.oid, a.attnum, p)
		   AND NOT has_table_privilege($1, rel.oid, p)
		   AND NOT (rel.name = 'k_ref.org' AND a.attname = 'tenant_id' AND p = 'SELECT')
		UNION ALL
		SELECT 'schema ' || nspname || ' ' || p FROM pg_namespace s, unnest(ARRAY['USAGE', 'CREATE']) p
		 WHERE nspname NOT IN ('pg_catalog', 'information_schema') AND nspname NOT LIKE 'pg\_%'
		   AND has_schema_privilege($1, s.oid, p) AND NOT has_schema_privilege('public', s.oid, p)
		   AND NOT (p = 'USAGE' AND (nspname LIKE '%\_pub' OR nspname = 'k_ref'))
		ORDER BY 1`, dbadmin.AggRole)
	if err != nil {
		t.Fatal(err)
	}
	var extra []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		extra = append(extra, s)
	}
	rows.Close()
	if len(extra) > 0 {
		t.Errorf("r_agg có quyền ngoài view _pub + k_ref.org(tenant_id): %v", extra)
	}
	var pubViews, tenantCol int
	if err := admin.QueryRow(ctx, `SELECT
	    (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	      WHERE n.nspname LIKE '%\_pub' AND c.relkind = 'v' AND has_table_privilege($1, c.oid, 'SELECT')),
	    (SELECT count(*) FROM pg_attribute a WHERE a.attrelid = 'k_ref.org'::regclass AND a.attname = 'tenant_id'
	      AND has_column_privilege($1, a.attrelid, a.attnum, 'SELECT'))`, dbadmin.AggRole).Scan(&pubViews, &tenantCol); err != nil {
		t.Fatal(err)
	}
	if pubViews == 0 || tenantCol != 1 {
		t.Errorf("r_agg: SELECT %d view _pub, k_ref.org.tenant_id %d", pubViews, tenantCol)
	}
	// pf 000137 (gói FC): chính sách RLS dưới view _pub gọi k_ref.ctx_* — quyền EXECUTE kiểm theo current_user (r_agg).
	for _, fn := range []string{"k_ref.ctx_uuid(text)", "k_ref.ctx_text(text)", "k_ref.ctx_reject(text)"} {
		var ok bool
		if err := admin.QueryRow(ctx, `SELECT has_function_privilege($1, $2, 'EXECUTE')`, dbadmin.AggRole, fn).Scan(&ok); err != nil || !ok {
			t.Errorf("r_agg EXECUTE %s: %v %v", fn, ok, err)
		}
	}

	// Gói FC: grant cũ app_worker → r_agg (W6.4) bị bootstrap thu hồi (dbadmin.ExclusiveMembers). Dựng lại trạng thái
	// W6.4 rồi bootstrap — cũng thu hẹp cửa sổ với tiến trình khác đang chạy bootstrap bản cũ trên cùng máy chủ (vai là
	// đối tượng toàn cụm PostgreSQL).
	if _, err := admin.Exec(ctx, `GRANT r_agg TO app_worker WITH INHERIT FALSE, SET TRUE`); err != nil {
		t.Fatal(err)
	}
	if err := dbadmin.Bootstrap(ctx, env.AdminURL, env.Suffix); err != nil {
		t.Fatal(err)
	}
	var members []string
	mrows, err := admin.Query(ctx, `SELECT m.rolname || ':' || g.inherit_option::text FROM pg_auth_members g
	    JOIN pg_roles r ON r.oid = g.roleid JOIN pg_roles m ON m.oid = g.member WHERE r.rolname = $1 ORDER BY 1`, dbadmin.AggRole)
	if err != nil {
		t.Fatal(err)
	}
	for mrows.Next() {
		var s string
		if err := mrows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		members = append(members, s)
	}
	mrows.Close()
	if !slices.Equal(members, []string{dbadmin.AppAgg + ":false"}) {
		t.Errorf("thành viên r_agg: %v, muốn [app_agg:false]", members)
	}
	// app_agg (gói FC): chỉ thuộc r_agg, NOINHERIT, không BYPASSRLS, mặc định chỉ đọc + statement_timeout.
	var groups []string
	var aggInherit, aggBypass bool
	var cfg []string
	if err := admin.QueryRow(ctx, `SELECT r.rolinherit, r.rolbypassrls, coalesce(r.rolconfig, '{}'),
	    coalesce((SELECT array_agg(g.rolname::text ORDER BY g.rolname) FROM pg_auth_members a JOIN pg_roles g ON g.oid = a.roleid
	               WHERE a.member = r.oid), '{}')
	    FROM pg_roles r WHERE r.rolname = $1`, dbadmin.AppAgg).Scan(&aggInherit, &aggBypass, &cfg, &groups); err != nil {
		t.Fatal(err)
	}
	if aggInherit || aggBypass || !slices.Equal(groups, []string{dbadmin.AggRole}) ||
		!slices.Contains(cfg, "default_transaction_read_only=on") || !slices.ContainsFunc(cfg, func(s string) bool { return strings.HasPrefix(s, "statement_timeout=") }) {
		t.Errorf("app_agg: inherit=%v bypass=%v config=%v thành viên=%v", aggInherit, aggBypass, cfg, groups)
	}

	allowed := []string{dbadmin.AggRole, dbadmin.MKGMonitorOwner, dbadmin.OpsJobs, dbadmin.OutboxPublisher, dbadmin.TSMonitorOwner}
	for _, c := range migrations.Clusters {
		var got []string
		brows, err := env.AdminPool(t, c).Query(ctx, `SELECT rolname FROM pg_roles WHERE rolbypassrls AND NOT rolsuper ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		for brows.Next() {
			var s string
			if err := brows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(allowed, s) {
				got = append(got, s)
			}
		}
		brows.Close()
		if len(got) > 0 {
			t.Errorf("%s: vai BYPASSRLS ngoài danh mục ADR-0031 §6: %v", c, got)
		}
	}
}
