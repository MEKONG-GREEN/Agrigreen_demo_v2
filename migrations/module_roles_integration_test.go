package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// pf 000090 (W2.3) + pf 000119, mkg 000035, vault 000003 (W2.6, ADR-0028 §4): r_<module> có ĐÚNG quyền hiệu lực
// của vai cũ <module>_app trên mọi bảng, chuỗi số, cột, schema và hàm của database mỗi cụm — không thiếu (mã chuyển
// sang r_* chạy như cũ), không thừa (giữ "không quyền chéo"). Sau W2.6 vai cũ chỉ còn quyền qua membership r_*
// (lùi bản), nên phép so là tautology có chủ đích: hai vai PHẢI tương đương.
func TestModuleGroupRolesMirrorLegacy_ADR0028(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	const diff = `
		WITH rel AS (
		  SELECT c.oid, c.relkind, c.oid::regclass::text AS name
		    FROM pg_class c JOIN pg_namespace ns ON ns.oid = c.relnamespace
		   WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')
		     AND ns.nspname NOT IN ('pg_catalog', 'information_schema') AND ns.nspname NOT LIKE 'pg\_toast%'
		)
		SELECT 'relation ' || name || ' ' || p FROM rel, unnest(ARRAY['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER']) p
		 WHERE relkind <> 'S' AND has_table_privilege($1, oid, p) <> has_table_privilege($2, oid, p)
		UNION ALL
		SELECT 'sequence ' || name || ' ' || p FROM rel, unnest(ARRAY['USAGE', 'SELECT', 'UPDATE']) p
		 WHERE relkind = 'S' AND has_sequence_privilege($1, oid, p) <> has_sequence_privilege($2, oid, p)
		UNION ALL
		SELECT 'column ' || name || '.' || a.attname || ' ' || p FROM rel JOIN pg_attribute a ON a.attrelid = rel.oid,
		       unnest(ARRAY['SELECT', 'INSERT', 'UPDATE', 'REFERENCES']) p
		 WHERE relkind <> 'S' AND a.attnum > 0 AND NOT a.attisdropped
		   AND has_column_privilege($1, rel.oid, a.attnum, p) <> has_column_privilege($2, rel.oid, a.attnum, p)
		UNION ALL
		SELECT 'schema ' || nspname FROM pg_namespace
		 WHERE nspname NOT LIKE 'pg\_%' AND has_schema_privilege($1, oid, 'USAGE') <> has_schema_privilege($2, oid, 'USAGE')
		UNION ALL
		SELECT 'function ' || f.oid::regprocedure::text FROM pg_proc f JOIN pg_namespace ns ON ns.oid = f.pronamespace
		 WHERE ns.nspname NOT IN ('pg_catalog', 'information_schema')
		   AND has_function_privilege($1, f.oid, 'EXECUTE') <> has_function_privilege($2, f.oid, 'EXECUTE')
		LIMIT 10`
	admin := env.AdminPool(t, migrations.PF)
	for _, g := range dbadmin.ModuleGroups() {
		if g.Legacy == "" {
			continue
		}
		for _, c := range g.Clusters {
			mirror(ctx, t, env.AdminPool(t, c), diff, g.Legacy, g.Role)
		}
	}
	// Kiểm có nghĩa: r_content thật sự có quyền trên bảng của mình, không có trên bảng pf_notify.
	var own, other bool
	if err := admin.QueryRow(ctx, `SELECT has_table_privilege('r_content', 'pf_content.document', 'SELECT'),
	        has_table_privilege('r_content', 'pf_notify.notification', 'SELECT')`).Scan(&own, &other); err != nil {
		t.Fatal(err)
	}
	if !own || other {
		t.Fatalf("r_content: SELECT pf_content.document=%v pf_notify.notification=%v", own, other)
	}
}

func mirror(ctx context.Context, t *testing.T, admin *pgxpool.Pool, diff, legacy, group string) {
	t.Helper()
	rows, err := admin.Query(ctx, diff, legacy, group)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		t.Errorf("%s ≠ %s: %s", legacy, group, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
