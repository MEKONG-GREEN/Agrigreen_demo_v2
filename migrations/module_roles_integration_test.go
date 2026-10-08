package migrations_test

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// pf 000090 (W2.3, ADR-0028 §4 bước 1): r_<module> có ĐÚNG quyền của vai cũ <module>_app trên mọi bảng,
// chuỗi số, cột, schema và hàm của database pf — không thiếu (mã chuyển sang r_* chạy như cũ), không thừa
// (giữ "không quyền chéo").
func TestModuleGroupRolesMirrorLegacy_ADR0028(t *testing.T) {
	ctx := context.Background()
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
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
	for _, g := range dbadmin.ModuleGroups() {
		if g.Legacy == "" {
			continue // vai nhóm không có vai cũ (pf/kref, W2.5): không có gì để so
		}
		rows, err := admin.Query(ctx, diff, g.Legacy, g.Role)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				t.Fatal(err)
			}
			t.Errorf("%s ≠ %s: %s", g.Legacy, g.Role, d)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
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
