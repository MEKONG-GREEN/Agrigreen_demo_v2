-- W3.4 (kiến trúc v4.1 §17.2, DB-13, DB-14, ADR-0005, ADR-0029, ADR-0030; docs/design/cooperative.md mục 9):
-- RLS phạm vi theo Nút trên pf_coop. Chạy bằng vai ag_migrator. Chỉ tiến (R-24); không UPDATE/DELETE dòng nào.
--
-- Cấu trúc (quyết định W3.4, ghi ở thiết kế mục 9):
--   * p_tenant GIỮ NGUYÊN (PERMISSIVE, apply_tenant_rls) — chính sách cho phép duy nhất; migratecheck cấm DROP.
--   * p_scope mới là RESTRICTIVE, FOR ALL → hàng thấy/ghi được = p_tenant AND p_scope. Phạm vi Nút chỉ THU HẸP
--     trong tenant, không mở rộng ra tenant khác (shared.v_missing_rls — shared 000012 — đòi p_scope RESTRICTIVE).
--   * Viết thẳng EXISTS trên k_ref.node_closure (không hàm do repo định nghĩa, ngoài k_ref.ctx_uuid/ctx_text
--     là SQL STABLE inline được) — cổng EXPLAIN W9.1 (DB-14).
--   * WITH CHECK = cùng biểu thức phạm vi: người gọi không ghi được dòng mà mình không đọc được (UPDATE: USING
--     trên dòng cũ, WITH CHECK trên dòng mới). Ngoại lệ: household — WITH CHECK (true), vì dòng household được
--     ghi TRƯỚC household_membership trong cùng giao dịch (Nút của hộ suy ra từ thành viên) — ghi vẫn bị
--     p_tenant kiểm tenant, đọc lại thì p_scope kiểm.
--
-- Biểu thức phạm vi của bảng T có cột Nút N (HTX = Nút kind=cooperative trong k_ref.org):
--   (a) k_ref.ctx_uuid('app.node_id') IS NULL        — consumer, job, worker, Admin không gắn Nút: chỉ lọc tenant
--       (giữ kiểm ở ứng dụng, R-5 thiết kế mục 12);
--   (b) tenant CHƯA có bản chiếu k_ref (không dòng node_closure nào) — InitPlan một lần/câu: trước Rebuild đầu
--       tiên của W2.5 hoặc tenant chưa có tổ chức nào, chính sách lùi về p_tenant như trước W3.4 thay vì trả
--       rỗng cho mọi người dùng. Tenant đã chiếu mà Nút của người gọi chưa có (độ trễ worker) → ĐÓNG. Lọc
--       tenant viết đúng như p_tenant (shared.current_tenant(), SQL inline) để planner dùng chỉ mục khóa chính
--       (dạng CASE của ctx_uuid làm ước lượng sai). Viết dạng truy vấn con vô hướng ORDER BY ancestor LIMIT 1
--       thay vì NOT EXISTS: EXISTS bỏ ORDER BY và planner chọn quét tuần tự "tới dòng khớp đầu tiên" (giả định
--       phân bố đều) — với tenant nhỏ nằm cuối bảng là quét cả bảng; ORDER BY theo chỉ mục khóa chính ép quét
--       chỉ mục một dòng (TestCoopScopeExplainVolume_W34);
--   (c) EXISTS node_closure (ancestor = Nút người gọi, descendant = T.N) — Nút người gọi và mọi hậu duệ;
--   (d) xã phụ trách: T.commune_code = ANY(app.xa_ids); bảng không có commune_code dùng xã của Nút
--       (k_ref.org.admin_unit_code của T.N);
--   (e) dòng CHƯA gắn Nút (T.N IS NULL: thửa nháp, xung đột giữa HTX, lô nhật ký không HTX) — thấy được khi
--       người gọi không bị giới hạn theo xã (app.xa_ids rỗng); có xã thì chỉ qua (d).
-- household (không cột Nút): (c) qua household_membership đang mở JOIN node_closure; (d) qua commune_code.
-- kn_profile_event: subject_kind = cooperative → (c) trên subject_ref; household → như household.
-- sop_template, sop_version, variety_sop_binding: danh mục cấp tenant — chỉ p_tenant (thiết kế mục 9).
-- Phân vùng giữ p_tenant (secure_partition); r_cooperative không có quyền trên phân vùng (000133 kiểm).

DO $$
DECLARE
  -- bảng, cột Nút, cột Nút NULL được, có commune_code
  spec constant text[][] := ARRAY[
    ['cooperative',          'org_ref',         'f', 'f'],
    ['household_membership', 'org_ref',         'f', 'f'],
    ['plot',                 'cooperative_ref', 't', 't'],
    ['plot_boundary',        'cooperative_ref', 't', 't'],
    ['plot_event',           'cooperative_ref', 't', 't'],
    ['boundary_conflict',    'owner_ref',       't', 't'],
    ['cooperative_boundary', 'cooperative_ref', 'f', 't'],
    ['cooperative_area',     'cooperative_ref', 'f', 't'],
    ['season_event',         'cooperative_ref', 'f', 't'],
    ['season_current',       'cooperative_ref', 'f', 't'],
    ['farm_log_batch',       'cooperative_ref', 't', 'f'],
    ['farm_log',             'cooperative_ref', 'f', 't'],
    ['farm_log_review',      'cooperative_ref', 'f', 'f'],
    ['farm_log_current',     'cooperative_ref', 'f', 't'],
    ['harvest_declaration',  'cooperative_ref', 'f', 't'],
    ['yield_summary',        'cooperative_ref', 'f', 't'],
    ['coop_season_area',     'cooperative_ref', 'f', 'f']];
  -- (a) + (b): không phụ thuộc dòng
  free constant text := $f$k_ref.ctx_uuid('app.node_id') IS NULL
         OR (SELECT 1 FROM k_ref.node_closure u WHERE u.tenant_id = shared.current_tenant() ORDER BY u.ancestor LIMIT 1) IS NULL$f$;
  t text;
  n text;
  expr text;
BEGIN
  FOR i IN 1 .. array_length(spec, 1) LOOP
    t := spec[i][1];
    n := spec[i][2];
    expr := free || format($f$
         OR EXISTS (SELECT 1 FROM k_ref.node_closure nc
                     WHERE nc.tenant_id = %1$I.tenant_id
                       AND nc.ancestor = k_ref.ctx_uuid('app.node_id')
                       AND nc.descendant = %1$I.%2$I)$f$, t, n);
    IF spec[i][4] = 't' THEN
      expr := expr || format($f$
         OR %1$I.commune_code = ANY (k_ref.ctx_text('app.xa_ids')::text[])$f$, t);
    ELSE
      expr := expr || format($f$
         OR EXISTS (SELECT 1 FROM k_ref.org o
                     WHERE o.tenant_id = %1$I.tenant_id AND o.ref = %1$I.%2$I
                       AND o.admin_unit_code = ANY (k_ref.ctx_text('app.xa_ids')::text[]))$f$, t, n);
    END IF;
    IF spec[i][3] = 't' THEN
      expr := expr || format($f$
         OR (%1$I.%2$I IS NULL AND k_ref.ctx_text('app.xa_ids') IS NULL)$f$, t, n);
    END IF;
    EXECUTE format('CREATE POLICY p_scope ON pf_coop.%I AS RESTRICTIVE FOR ALL USING (%s) WITH CHECK (%s)', t, expr, expr);
  END LOOP;
END
$$;

-- household: Nút suy từ thành viên HTX đang mở (household_membership có p_scope riêng, không tham chiếu household
-- → không đệ quy chính sách).
CREATE POLICY p_scope ON pf_coop.household AS RESTRICTIVE FOR ALL
  USING (
       k_ref.ctx_uuid('app.node_id') IS NULL
    OR (SELECT 1 FROM k_ref.node_closure u WHERE u.tenant_id = shared.current_tenant() ORDER BY u.ancestor LIMIT 1) IS NULL
    OR EXISTS (SELECT 1 FROM pf_coop.household_membership m
                 JOIN k_ref.node_closure nc ON nc.tenant_id = m.tenant_id AND nc.descendant = m.org_ref
                WHERE m.tenant_id = household.tenant_id AND m.household_ref = household.household_ref
                  AND (m.until_date IS NULL OR m.until_date > current_date)
                  AND nc.ancestor = k_ref.ctx_uuid('app.node_id'))
    OR household.commune_code = ANY (k_ref.ctx_text('app.xa_ids')::text[]))
  WITH CHECK (true);

CREATE POLICY p_scope ON pf_coop.kn_profile_event AS RESTRICTIVE FOR ALL
  USING (
       k_ref.ctx_uuid('app.node_id') IS NULL
    OR (SELECT 1 FROM k_ref.node_closure u WHERE u.tenant_id = shared.current_tenant() ORDER BY u.ancestor LIMIT 1) IS NULL
    OR (kn_profile_event.subject_kind = 'cooperative'
        AND EXISTS (SELECT 1 FROM k_ref.node_closure nc
                     WHERE nc.tenant_id = kn_profile_event.tenant_id
                       AND nc.ancestor = k_ref.ctx_uuid('app.node_id')
                       AND nc.descendant = kn_profile_event.subject_ref))
    OR (kn_profile_event.subject_kind = 'household'
        AND EXISTS (SELECT 1 FROM pf_coop.household_membership m
                      JOIN k_ref.node_closure nc ON nc.tenant_id = m.tenant_id AND nc.descendant = m.org_ref
                     WHERE m.tenant_id = kn_profile_event.tenant_id AND m.household_ref = kn_profile_event.subject_ref
                       AND (m.until_date IS NULL OR m.until_date > current_date)
                       AND nc.ancestor = k_ref.ctx_uuid('app.node_id')))
    OR kn_profile_event.commune_code = ANY (k_ref.ctx_text('app.xa_ids')::text[]))
  WITH CHECK (
       k_ref.ctx_uuid('app.node_id') IS NULL
    OR (SELECT 1 FROM k_ref.node_closure u WHERE u.tenant_id = shared.current_tenant() ORDER BY u.ancestor LIMIT 1) IS NULL
    OR (kn_profile_event.subject_kind = 'cooperative'
        AND EXISTS (SELECT 1 FROM k_ref.node_closure nc
                     WHERE nc.tenant_id = kn_profile_event.tenant_id
                       AND nc.ancestor = k_ref.ctx_uuid('app.node_id')
                       AND nc.descendant = kn_profile_event.subject_ref))
    OR (kn_profile_event.subject_kind = 'household'
        AND EXISTS (SELECT 1 FROM pf_coop.household_membership m
                      JOIN k_ref.node_closure nc ON nc.tenant_id = m.tenant_id AND nc.descendant = m.org_ref
                     WHERE m.tenant_id = kn_profile_event.tenant_id AND m.household_ref = kn_profile_event.subject_ref
                       AND (m.until_date IS NULL OR m.until_date > current_date)
                       AND nc.ancestor = k_ref.ctx_uuid('app.node_id')))
    OR kn_profile_event.commune_code = ANY (k_ref.ctx_text('app.xa_ids')::text[]));

-- Kiểm cuối (lỗi → giao dịch lùi): 19 bảng có p_scope RESTRICTIVE, 3 bảng danh mục không có; cổng chung rỗng.
DO $$
DECLARE
  bad text;
BEGIN
  SELECT string_agg(c.relname, ', ') INTO bad FROM pg_class c
   WHERE c.relnamespace = 'pf_coop'::regnamespace AND c.relkind IN ('r', 'p') AND NOT c.relispartition
     AND (c.relname IN ('sop_template', 'sop_version', 'variety_sop_binding'))
       = EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'p_scope' AND NOT p.polpermissive);
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'W3.4: p_scope sai trên pf_coop: %', bad;
  END IF;
  IF (SELECT count(*) FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid
       WHERE c.relnamespace = 'pf_coop'::regnamespace AND p.polname = 'p_scope') <> 19 THEN
    RAISE EXCEPTION 'W3.4: số p_scope trên pf_coop khác 19';
  END IF;
  IF EXISTS (SELECT 1 FROM shared.v_missing_rls) THEN
    RAISE EXCEPTION 'W3.4: shared.v_missing_rls không rỗng';
  END IF;
END
$$;
