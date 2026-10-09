package migrator

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// fingerprintSQL mô tả lược đồ của database thành tập dòng văn bản so sánh được (W7.1: bộ chạy v1 và v2 trên
// database mới phải cho lược đồ giống hệt): schema, quan hệ (loại, chủ, ACL, RLS, chú thích, biên phân vùng),
// cột, ràng buộc, chỉ mục, view, hàm (băm định nghĩa), trigger, policy, kiểu, quyền mặc định, extension. Bỏ đối
// tượng của extension và bảng sổ của v2 (<schema>.schema_migrations) — chỉ v2 có. ACL sắp theo mục: v2 có thể xếp
// tệp của hai module không liên quan khác thứ tự số, nên thứ tự GRANT (thứ tự mục trong aclitem[]) khác mà quyền
// như nhau.
const fingerprintSQL = `
WITH ns AS (
  SELECT oid, nspname, nspowner, nspacl FROM pg_namespace
   WHERE nspname NOT IN ('pg_catalog', 'information_schema') AND nspname !~ '^pg_(toast|temp)'
), ext AS (
  SELECT objid FROM pg_depend WHERE deptype = 'e'
), rel AS (
  SELECT c.*, n.nspname FROM pg_class c JOIN ns n ON n.oid = c.relnamespace
   WHERE c.oid NOT IN (SELECT objid FROM ext) AND c.relname NOT IN ('schema_migrations', 'schema_migrations_pkey')
)
SELECT 'schema ' || nspname || ' owner=' || pg_get_userbyid(nspowner) || ' acl=' || (SELECT coalesce(string_agg(x::text, ',' ORDER BY x::text), '') FROM unnest(nspacl) x) FROM ns
UNION ALL
SELECT 'rel ' || nspname || '.' || relname || ' kind=' || relkind::text || ' owner=' || pg_get_userbyid(relowner)
       || ' acl=' || (SELECT coalesce(string_agg(x::text, ',' ORDER BY x::text), '') FROM unnest(relacl) x) || ' rls=' || relrowsecurity || '/' || relforcerowsecurity
       || ' persist=' || relpersistence::text || ' comment=' || coalesce(obj_description(oid, 'pg_class'), '')
       || ' bound=' || coalesce(pg_get_expr(relpartbound, oid), '')
  FROM rel WHERE relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
UNION ALL
SELECT 'col ' || r.nspname || '.' || r.relname || '.' || a.attname || ' ' || format_type(a.atttypid, a.atttypmod)
       || ' nn=' || a.attnotnull || ' def=' || coalesce(pg_get_expr(d.adbin, d.adrelid), '') || ' acl=' || (SELECT coalesce(string_agg(x::text, ',' ORDER BY x::text), '') FROM unnest(a.attacl) x)
       || ' gen=' || a.attgenerated::text || ' ident=' || a.attidentity::text || ' comment=' || coalesce(col_description(r.oid, a.attnum), '')
  FROM rel r JOIN pg_attribute a ON a.attrelid = r.oid
  LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
 WHERE a.attnum > 0 AND NOT a.attisdropped AND r.relkind IN ('r', 'p', 'v', 'm', 'f')
UNION ALL
SELECT 'con ' || n.nspname || '.' || coalesce(r.relname, t.typname, '') || ' ' || k.conname || ' ' || pg_get_constraintdef(k.oid)
       || ' valid=' || k.convalidated
  FROM pg_constraint k JOIN ns n ON n.oid = k.connamespace
  LEFT JOIN pg_class r ON r.oid = k.conrelid
  LEFT JOIN pg_type t ON t.oid = k.contypid
 WHERE coalesce(r.relname, '') <> 'schema_migrations' AND k.oid NOT IN (SELECT objid FROM ext)
UNION ALL
SELECT 'idx ' || r.nspname || '.' || r.relname || ' ' || pg_get_indexdef(r.oid) FROM rel r WHERE r.relkind IN ('i', 'I')
UNION ALL
SELECT 'view ' || nspname || '.' || relname || ' ' || pg_get_viewdef(oid) FROM rel WHERE relkind IN ('v', 'm')
UNION ALL
SELECT 'fn ' || n.nspname || '.' || p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ') kind=' || p.prokind::text
       || ' owner=' || pg_get_userbyid(p.proowner) || ' acl=' || (SELECT coalesce(string_agg(x::text, ',' ORDER BY x::text), '') FROM unnest(p.proacl) x) || ' secdef=' || p.prosecdef
       || ' def=' || CASE WHEN p.prokind IN ('f', 'p', 'w') THEN md5(pg_get_functiondef(p.oid)) ELSE '' END
  FROM pg_proc p JOIN ns n ON n.oid = p.pronamespace WHERE p.oid NOT IN (SELECT objid FROM ext)
UNION ALL
SELECT 'trg ' || r.nspname || '.' || r.relname || ' ' || pg_get_triggerdef(t.oid) || ' en=' || t.tgenabled::text
  FROM pg_trigger t JOIN rel r ON r.oid = t.tgrelid WHERE NOT t.tgisinternal
UNION ALL
SELECT 'pol ' || schemaname || '.' || tablename || ' ' || policyname || ' ' || permissive || ' ' || roles::text || ' ' || cmd
       || ' using=' || coalesce(qual, '') || ' check=' || coalesce(with_check, '')
  FROM pg_policies WHERE tablename <> 'schema_migrations'
UNION ALL
SELECT 'type ' || n.nspname || '.' || t.typname || ' ' || t.typtype::text || ' owner=' || pg_get_userbyid(t.typowner)
       || ' acl=' || (SELECT coalesce(string_agg(x::text, ',' ORDER BY x::text), '') FROM unnest(t.typacl) x)
       || ' labels=' || coalesce((SELECT string_agg(e.enumlabel, ',' ORDER BY e.enumsortorder) FROM pg_enum e WHERE e.enumtypid = t.oid), '')
       || ' base=' || CASE WHEN t.typtype = 'd' THEN format_type(t.typbasetype, t.typtypmod) ELSE '' END
  FROM pg_type t JOIN ns n ON n.oid = t.typnamespace
 WHERE (t.typtype IN ('e', 'd', 'r', 'm') OR (t.typtype = 'c' AND (SELECT relkind FROM pg_class WHERE oid = t.typrelid) = 'c'))
   AND t.oid NOT IN (SELECT objid FROM ext)
UNION ALL
SELECT 'defacl ' || pg_get_userbyid(d.defaclrole) || ' ' || coalesce(n.nspname, '') || ' ' || d.defaclobjtype::text || ' ' || (SELECT string_agg(x::text, ',' ORDER BY x::text) FROM unnest(d.defaclacl) x)
  FROM pg_default_acl d LEFT JOIN pg_namespace n ON n.oid = d.defaclnamespace
UNION ALL
SELECT 'ext ' || extname || ' ' || extversion || ' ' || extnamespace::regnamespace::text FROM pg_extension
ORDER BY 1`

// Fingerprint trả mô tả lược đồ của database conn đang nối (đã sắp) — xem fingerprintSQL.
func Fingerprint(ctx context.Context, conn *pgx.Conn) ([]string, error) {
	rows, err := conn.Query(ctx, fingerprintSQL)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
