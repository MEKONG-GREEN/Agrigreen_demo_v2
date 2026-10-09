-- W3.4 (kiến trúc v4.1 §17.2, ADR-0030, docs/design/cooperative.md mục 9): cổng chung shared.v_missing_rls chấp
-- nhận chính sách phạm vi p_scope bên cạnh p_tenant. Áp cho schema shared của MỌI database. Chạy bằng vai
-- ag_migrator. Chỉ tiến (R-24), chỉ thay định nghĩa view.
--
-- Bất biến mới (thay "đúng một policy p_tenant"):
--   * có p_tenant và p_tenant là PERMISSIVE — chính sách cho phép DUY NHẤT của bảng;
--   * ngoài p_tenant chỉ được thêm p_scope, và p_scope phải RESTRICTIVE.
-- Như vậy mọi bảng có hàng thấy được = p_tenant AND p_scope: phạm vi Nút chỉ THU HẸP trong tenant, không bao giờ mở
-- rộng ra tenant khác (RB-2). Một p_scope PERMISSIVE (OR với p_tenant) hay một chính sách thứ ba bất kỳ → cổng báo.

CREATE OR REPLACE VIEW shared.v_missing_rls AS
SELECT n.nspname AS schema_name, c.relname AS table_name,
       concat_ws(', ',
         CASE WHEN NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped)
              THEN 'thiếu cột tenant_id' END,
         CASE WHEN NOT c.relrowsecurity THEN 'chưa ENABLE RLS' END,
         CASE WHEN NOT c.relforcerowsecurity THEN 'chưa FORCE RLS' END,
         CASE WHEN EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname NOT IN ('p_tenant', 'p_scope'))
              THEN 'có policy ngoài p_tenant/p_scope' END,
         CASE WHEN NOT EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_tenant') THEN 'thiếu policy p_tenant' END,
         CASE WHEN EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_tenant' AND NOT p.polpermissive)
              THEN 'p_tenant phải PERMISSIVE' END,
         CASE WHEN EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_scope' AND p.polpermissive)
              THEN 'p_scope phải RESTRICTIVE' END
       ) AS problem
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p')
  AND (n.nspname = 'shared' OR n.nspname ~ '^(pf_|mkg_|vault|ts_|sync_)')
  AND coalesce(obj_description(c.oid, 'pg_class'), '') NOT LIKE 'ag:no-rls%'
  AND NOT (
        EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped)
    AND c.relrowsecurity AND c.relforcerowsecurity
    AND NOT EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname NOT IN ('p_tenant', 'p_scope'))
    AND EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_tenant' AND p.polpermissive)
    AND NOT EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_scope' AND p.polpermissive));

COMMENT ON VIEW shared.v_missing_rls IS
  'Bảng thiếu RLS đúng mẫu: cột tenant_id, ENABLE + FORCE RLS, p_tenant PERMISSIVE (duy nhất cho phép), tùy chọn p_scope RESTRICTIVE (W3.4), không policy khác';
