-- W4.2 (kiến trúc v4.1 §17.3, R-DB-08, ADR-0030 mục 2 + "Hiện thực"): đọc tại chỗ có cấp phép. Áp cho schema
-- shared của MỌI database. Chạy bằng vai ag_migrator. Chỉ tiến (R-24), không UPDATE/DELETE dòng nào.
--
-- Vấn đề (W3.4, shared 000012): hàng thấy được = p_tenant AND p_scope, p_tenant là chính sách PERMISSIVE DUY NHẤT.
-- Nhánh chia sẻ đọc dòng của tenant KHÁC (đơn vị chủ) nên không thể nằm trong p_scope (RESTRICTIVE chỉ thu hẹp)
-- và không thể nằm trong p_tenant mà giữ WITH CHECK không có nhánh chia sẻ (p_tenant FOR ALL, một biểu thức
-- USING cho cả SELECT/UPDATE/DELETE — nhánh chia sẻ trong USING của p_tenant thì đơn vị xem UPDATE/DELETE được
-- dòng của đơn vị chủ). Cách đúng: chính sách PERMISSIVE thứ hai CHỈ cho SELECT.
--
-- Bất biến mới của shared.v_missing_rls (thay "p_tenant là chính sách cho phép duy nhất"):
--   * p_tenant có mặt, PERMISSIVE (như cũ);
--   * p_scope (tùy chọn) phải RESTRICTIVE (như cũ);
--   * p_share (tùy chọn, W4.2) phải PERMISSIVE, FOR SELECT, cho PUBLIC, KHÔNG có WITH CHECK, và USING phải tra
--     k_ref.share_scope — nên p_share chỉ mở thêm ĐỌC dòng mà k_ref.share_scope (worker pf.kref dựng từ cấp phép
--     intra_platform đang hiệu lực × cây Nút của đơn vị chủ) cho phép. Ghi (INSERT/UPDATE/DELETE) vẫn chỉ qua
--     p_tenant: cấp phép là quyền đọc, không phải quyền ghi (R-DB-08, phép thử T6e);
--   * mọi chính sách khác → báo.
-- Bảng nào được có p_share (chỉ bảng khai trong `shares` của module.yaml) do kiểm thử Go đối chiếu modreg
-- (migrations/share_rls_integration_test.go) — cổng SQL không biết danh mục module.
--
-- shared.apply_share_rls(bảng, biểu thức USING): mẫu duy nhất để thêm nhánh chia sẻ (như apply_tenant_rls cho
-- p_tenant). Nó (1) kiểm bảng đã đúng mẫu RLS và biểu thức tra k_ref.share_scope; (2) tạo p_share; (3) nếu bảng
-- có p_scope RESTRICTIVE (W3.4), nới USING của p_scope cho dòng KHÁC tenant phiên — p_scope là phạm vi Nút
-- TRONG tenant (Nút người gọi thuộc cây của đơn vị xem, không so được với Nút của đơn vị chủ); dòng khác tenant
-- chỉ thấy được qua p_share, và phạm vi Nút của nó là Nút phạm vi của cấp phép (share_scope.node_id = các hậu
-- duệ của scope_node_ref). WITH CHECK của p_scope giữ nguyên (không có nhánh chia sẻ). Lệnh UPDATE/DELETE: p_share
-- là FOR SELECT nên không thuộc chính sách áp cho UPDATE/DELETE — dòng khác tenant không qua p_tenant → 0 dòng.

CREATE FUNCTION shared.apply_share_rls(p_table regclass, p_using text) RETURNS void
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
DECLARE
  rel   text;
  qual  text;
BEGIN
  IF p_using IS NULL OR position('k_ref.share_scope' IN p_using) = 0 THEN
    RAISE EXCEPTION 'apply_share_rls(%): biểu thức USING phải tra k_ref.share_scope', p_table;
  END IF;
  IF EXISTS (SELECT 1 FROM shared.v_missing_rls v
              WHERE format('%I.%I', v.schema_name, v.table_name)::regclass = p_table) THEN
    RAISE EXCEPTION 'apply_share_rls(%): bảng chưa đúng mẫu RLS (shared.v_missing_rls)', p_table;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = p_table AND p.polname = 'p_tenant') THEN
    RAISE EXCEPTION 'apply_share_rls(%): thiếu p_tenant', p_table;
  END IF;
  SELECT c.relname INTO rel FROM pg_class c WHERE c.oid = p_table;
  EXECUTE format('CREATE POLICY p_share ON %s AS PERMISSIVE FOR SELECT TO PUBLIC USING (%s)', p_table, p_using);
  SELECT pg_get_expr(p.polqual, p.polrelid) INTO qual FROM pg_policy p
   WHERE p.polrelid = p_table AND p.polname = 'p_scope' AND NOT p.polpermissive;
  IF qual IS NOT NULL THEN
    EXECUTE format('ALTER POLICY p_scope ON %s USING (%I.tenant_id <> shared.current_tenant() OR (%s))', p_table, rel, qual);
  END IF;
END
$fn$;
REVOKE ALL ON FUNCTION shared.apply_share_rls(regclass, text) FROM PUBLIC;
COMMENT ON FUNCTION shared.apply_share_rls(regclass, text) IS
  'W4.2: thêm p_share (PERMISSIVE, FOR SELECT, không WITH CHECK, tra k_ref.share_scope) và nới p_scope cho dòng khác tenant (ADR-0030)';

CREATE OR REPLACE VIEW shared.v_missing_rls AS
SELECT n.nspname AS schema_name, c.relname AS table_name,
       concat_ws(', ',
         CASE WHEN NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped)
              THEN 'thiếu cột tenant_id' END,
         CASE WHEN NOT c.relrowsecurity THEN 'chưa ENABLE RLS' END,
         CASE WHEN NOT c.relforcerowsecurity THEN 'chưa FORCE RLS' END,
         CASE WHEN EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname NOT IN ('p_tenant', 'p_scope', 'p_share'))
              THEN 'có policy ngoài p_tenant/p_scope/p_share' END,
         CASE WHEN NOT EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_tenant') THEN 'thiếu policy p_tenant' END,
         CASE WHEN EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_tenant' AND NOT p.polpermissive)
              THEN 'p_tenant phải PERMISSIVE' END,
         CASE WHEN EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_scope' AND p.polpermissive)
              THEN 'p_scope phải RESTRICTIVE' END,
         CASE WHEN EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_share'
                             AND NOT (p.polpermissive AND p.polcmd = 'r' AND p.polwithcheck IS NULL AND p.polroles = '{0}'::oid[]
                                      AND EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_policy'::regclass AND d.objid = p.oid
                                                  AND d.refclassid = 'pg_class'::regclass AND d.refobjid = to_regclass('k_ref.share_scope'))))
              THEN 'p_share phải PERMISSIVE FOR SELECT cho PUBLIC, không WITH CHECK, tra k_ref.share_scope' END
       ) AS problem
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p')
  AND (n.nspname = 'shared' OR n.nspname ~ '^(pf_|mkg_|vault|ts_|sync_)')
  AND coalesce(obj_description(c.oid, 'pg_class'), '') NOT LIKE 'ag:no-rls%'
  AND NOT (
        EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped)
    AND c.relrowsecurity AND c.relforcerowsecurity
    AND NOT EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname NOT IN ('p_tenant', 'p_scope', 'p_share'))
    AND EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_tenant' AND p.polpermissive)
    AND NOT EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_scope' AND p.polpermissive)
    AND NOT EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_share'
                      AND NOT (p.polpermissive AND p.polcmd = 'r' AND p.polwithcheck IS NULL AND p.polroles = '{0}'::oid[]
                               AND EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_policy'::regclass AND d.objid = p.oid
                                                  AND d.refclassid = 'pg_class'::regclass AND d.refobjid = to_regclass('k_ref.share_scope')))));

COMMENT ON VIEW shared.v_missing_rls IS
  'Bảng thiếu RLS đúng mẫu: cột tenant_id, ENABLE + FORCE RLS, p_tenant PERMISSIVE, tùy chọn p_scope RESTRICTIVE (W3.4) và p_share PERMISSIVE FOR SELECT tra k_ref.share_scope (W4.2), không policy khác';
