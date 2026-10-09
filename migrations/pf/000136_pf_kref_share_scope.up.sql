-- W4.2 (kiến trúc v4.1 §17.3, R-DB-08, DB-14; ADR-0029, ADR-0030; docs/design/cooperative.md §7.3, §19):
-- đọc tại chỗ có cấp phép. Chạy bằng vai ag_migrator. Chỉ tiến (R-24); không UPDATE/DELETE dòng nào.
-- Thay số pf 000099 của bản kế hoạch (đã bị gói khác dùng).
--
-- 1. k_ref.share_grant_active (chủ pf/kref) thêm năm trường v2 của W4.1 (NULL được — dòng cũ = partner kế thừa;
--    sự kiện approved/revoked đã mang chúng từ pf 000120). Bảng chiếu, KHÔNG phải sổ (không apply_ledger) nên
--    không cần shared.register_ledger_columns. Sau triển khai chạy `worker -kref-rebuild` một lần để dòng của
--    cấp phép intra_platform đã duyệt trước W4.2 nhận đủ trường và share_scope được dựng.
-- 2. k_ref.share_scope (tạo rỗng ở pf 000092, chưa từng có bên ghi): thêm grant_id (dựng lại/thu hồi theo cấp
--    phép — hai cấp phép phủ cùng Nút không xóa dòng của nhau), valid_from/valid_to (cấp phép hết hạn tự đóng
--    trong chính sách, không chờ sự kiện). ADD COLUMN … NOT NULL không DEFAULT: bảng có dòng → migration lỗi (bảo
--    đảm "chưa từng có bên ghi"). Khóa chính thêm grant_id ở CUỐI — tiền tố (viewer_tenant, purpose, module_table)
--    vẫn là đường chỉ mục của chính sách. Chính sách của chính share_scope: đơn vị xem ĐỌC dòng của mình, worker
--    ghi trong giao dịch tenant ĐƠN VỊ CHỦ (sự kiện cấp phép có tenant phong bì = bên cấp) → WITH CHECK owner.
-- 3. Nhánh chia sẻ p_share (PERMISSIVE, FOR SELECT, không WITH CHECK) trên bảng của `shares` — qua
--    shared.apply_share_rls (shared 000014, mẫu chung như shared.apply_tenant_rls): pf_coop household,
--    household_membership (share household), plot, season_current, coop_season_area, yield_summary (season),
--    farm_log_current (diary_entry); pf_mechanization.machine (machine). Sổ (*_event, farm_log, …) KHÔNG có
--    nhánh. apply_share_rls nới p_scope (W3.4) cho dòng khác tenant phiên — xem shared 000014.
--    Ghi chú cổng agmodgraph: migration gắn pf/kref; chính sách trên bảng pf_coop / pf_mechanization do hàm thư
--    viện shared tạo (cùng cơ chế apply_tenant_rls), không phải câu DDL trực tiếp trên schema module khác. Danh
--    sách bảng được kiểm bằng migrations/share_rls_integration_test.go đối chiếu `shares` của modreg.
--
-- Biểu thức (kiến trúc §17.3, viết thẳng — không hàm có truy vấn con, DB-14; ctx đọc qua k_ref.ctx_*, DB-13):
--   EXISTS (SELECT 1 FROM k_ref.share_scope s
--            WHERE s.viewer_tenant = ctx('app.tenant_id') AND s.purpose = ctx_text('app.purpose')
--              AND s.module_table = '<khóa share>' AND s.owner_tenant = T.tenant_id AND s.node_id = T.<cột Nút>
--              AND s.valid_from <= now() AND s.valid_to > now())
-- → planner dựng hashed SubPlan một lần/câu (T9). household (không cột Nút): Nút qua xã viên đang mở, như p_scope
-- của W3.4 — household_membership JOIN share_scope (household_membership tự có p_share cùng khóa).

SET LOCAL lock_timeout = '5s';

-- ---------------------------------------------------------------------------
-- 1. share_grant_active: điều khoản v2
-- ---------------------------------------------------------------------------
ALTER TABLE k_ref.share_grant_active
  ADD COLUMN grant_class    text NULL CHECK (grant_class IS NULL OR grant_class IN ('intra_platform', 'partner')),
  ADD COLUMN module_table   text NULL,     -- tên logic '<module>:<share>' (khóa share_scope: modreg.ShareTable)
  ADD COLUMN scope_node_ref uuid NULL,     -- Nút phạm vi của đơn vị chủ
  ADD COLUMN purpose_code   text NULL,
  ADD COLUMN basis_ref      text NULL;
CREATE INDEX share_grant_active_scope ON k_ref.share_grant_active (tenant_id, scope_node_ref) WHERE grant_class = 'intra_platform';
COMMENT ON TABLE k_ref.share_grant_active IS
  'Cấp phép chia sẻ đang hiệu lực (bên cấp = tenant_id) + điều khoản v2 W4.1 (grant_class NULL = partner kế thừa) — ADR-0029, ADR-0030';

-- ---------------------------------------------------------------------------
-- 2. share_scope: dựng được theo cấp phép
-- ---------------------------------------------------------------------------
ALTER TABLE k_ref.share_scope
  ADD COLUMN grant_id   uuid        NOT NULL,
  ADD COLUMN valid_from timestamptz NOT NULL,
  ADD COLUMN valid_to   timestamptz NOT NULL;
ALTER TABLE k_ref.share_scope DROP CONSTRAINT share_scope_pkey;
ALTER TABLE k_ref.share_scope ADD PRIMARY KEY (viewer_tenant, purpose, module_table, owner_tenant, node_id, grant_id);
ALTER TABLE k_ref.share_scope ADD CONSTRAINT share_scope_shape
  CHECK (viewer_tenant <> owner_tenant AND valid_to > valid_from) NOT VALID;
ALTER TABLE k_ref.share_scope VALIDATE CONSTRAINT share_scope_shape;
CREATE INDEX share_scope_grant ON k_ref.share_scope (owner_tenant, grant_id);
ALTER POLICY p_tenant ON k_ref.share_scope
  USING (viewer_tenant = k_ref.ctx_uuid('app.tenant_id') OR owner_tenant = k_ref.ctx_uuid('app.tenant_id'))
  WITH CHECK (owner_tenant = k_ref.ctx_uuid('app.tenant_id'));
COMMENT ON TABLE k_ref.share_scope IS
  'Phạm vi chia sẻ (cấp phép intra_platform đang hiệu lực × hậu duệ của scope_node_ref) — worker pf.kref dựng mỗi sự kiện cấp phép/cây Nút trong một giao dịch (W4.2, ADR-0029, ADR-0030)';

-- ---------------------------------------------------------------------------
-- 3. Nhánh chia sẻ p_share trên bảng của `shares`
-- ---------------------------------------------------------------------------
SELECT shared.apply_share_rls('pf_coop.plot', $q$EXISTS (SELECT 1 FROM k_ref.share_scope s
   WHERE s.viewer_tenant = k_ref.ctx_uuid('app.tenant_id') AND s.purpose = k_ref.ctx_text('app.purpose')
     AND s.module_table = 'pf_coop.plot' AND s.owner_tenant = plot.tenant_id AND s.node_id = plot.cooperative_ref
     AND s.valid_from <= now() AND s.valid_to > now())$q$);

SELECT shared.apply_share_rls('pf_coop.season_current', $q$EXISTS (SELECT 1 FROM k_ref.share_scope s
   WHERE s.viewer_tenant = k_ref.ctx_uuid('app.tenant_id') AND s.purpose = k_ref.ctx_text('app.purpose')
     AND s.module_table = 'pf_coop.season' AND s.owner_tenant = season_current.tenant_id
     AND s.node_id = season_current.cooperative_ref
     AND s.valid_from <= now() AND s.valid_to > now())$q$);

SELECT shared.apply_share_rls('pf_coop.coop_season_area', $q$EXISTS (SELECT 1 FROM k_ref.share_scope s
   WHERE s.viewer_tenant = k_ref.ctx_uuid('app.tenant_id') AND s.purpose = k_ref.ctx_text('app.purpose')
     AND s.module_table = 'pf_coop.season' AND s.owner_tenant = coop_season_area.tenant_id
     AND s.node_id = coop_season_area.cooperative_ref
     AND s.valid_from <= now() AND s.valid_to > now())$q$);

SELECT shared.apply_share_rls('pf_coop.yield_summary', $q$EXISTS (SELECT 1 FROM k_ref.share_scope s
   WHERE s.viewer_tenant = k_ref.ctx_uuid('app.tenant_id') AND s.purpose = k_ref.ctx_text('app.purpose')
     AND s.module_table = 'pf_coop.season' AND s.owner_tenant = yield_summary.tenant_id
     AND s.node_id = yield_summary.cooperative_ref
     AND s.valid_from <= now() AND s.valid_to > now())$q$);

SELECT shared.apply_share_rls('pf_coop.farm_log_current', $q$EXISTS (SELECT 1 FROM k_ref.share_scope s
   WHERE s.viewer_tenant = k_ref.ctx_uuid('app.tenant_id') AND s.purpose = k_ref.ctx_text('app.purpose')
     AND s.module_table = 'pf_coop.diary_entry' AND s.owner_tenant = farm_log_current.tenant_id
     AND s.node_id = farm_log_current.cooperative_ref
     AND s.valid_from <= now() AND s.valid_to > now())$q$);

SELECT shared.apply_share_rls('pf_coop.household_membership', $q$EXISTS (SELECT 1 FROM k_ref.share_scope s
   WHERE s.viewer_tenant = k_ref.ctx_uuid('app.tenant_id') AND s.purpose = k_ref.ctx_text('app.purpose')
     AND s.module_table = 'pf_coop.household' AND s.owner_tenant = household_membership.tenant_id
     AND s.node_id = household_membership.org_ref
     AND s.valid_from <= now() AND s.valid_to > now())$q$);

-- household: hộ thấy được khi có xã viên ĐANG MỞ ở một HTX trong phạm vi cấp phép (household_membership đọc dưới
-- chính sách của nó — dòng khác tenant chỉ qua p_share của household_membership, cùng khóa).
SELECT shared.apply_share_rls('pf_coop.household', $q$EXISTS (SELECT 1 FROM pf_coop.household_membership m
     JOIN k_ref.share_scope s ON s.owner_tenant = m.tenant_id AND s.node_id = m.org_ref
   WHERE m.tenant_id = household.tenant_id AND m.household_ref = household.household_ref
     AND (m.until_date IS NULL OR m.until_date > current_date)
     AND s.viewer_tenant = k_ref.ctx_uuid('app.tenant_id') AND s.purpose = k_ref.ctx_text('app.purpose')
     AND s.module_table = 'pf_coop.household'
     AND s.valid_from <= now() AND s.valid_to > now())$q$);

SELECT shared.apply_share_rls('pf_mechanization.machine', $q$EXISTS (SELECT 1 FROM k_ref.share_scope s
   WHERE s.viewer_tenant = k_ref.ctx_uuid('app.tenant_id') AND s.purpose = k_ref.ctx_text('app.purpose')
     AND s.module_table = 'pf_mechanization.machine' AND s.owner_tenant = machine.tenant_id
     AND s.node_id = machine.cooperative_ref
     AND s.valid_from <= now() AND s.valid_to > now())$q$);

-- ---------------------------------------------------------------------------
-- 4. Kiểm cuối (lỗi → giao dịch lùi): đúng 8 p_share, p_scope của pf_coop vẫn 19 RESTRICTIVE, cổng chung rỗng.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  bad text;
BEGIN
  SELECT string_agg(c.oid::regclass::text, ', ' ORDER BY 1) INTO bad
    FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid
   WHERE p.polname = 'p_share'
     AND c.oid::regclass::text NOT IN ('pf_coop.household', 'pf_coop.household_membership', 'pf_coop.plot',
       'pf_coop.season_current', 'pf_coop.coop_season_area', 'pf_coop.yield_summary', 'pf_coop.farm_log_current',
       'pf_mechanization.machine');
  IF bad IS NOT NULL OR (SELECT count(*) FROM pg_policy WHERE polname = 'p_share') <> 8 THEN
    RAISE EXCEPTION 'W4.2: p_share sai: %', coalesce(bad, 'số chính sách khác 8');
  END IF;
  IF (SELECT count(*) FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid
       WHERE c.relnamespace = 'pf_coop'::regnamespace AND p.polname = 'p_scope' AND NOT p.polpermissive) <> 19 THEN
    RAISE EXCEPTION 'W4.2: số p_scope RESTRICTIVE trên pf_coop khác 19';
  END IF;
  SELECT string_agg(schema_name || '.' || table_name || ': ' || problem, '; ') INTO bad FROM shared.v_missing_rls;
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'W4.2: shared.v_missing_rls: %', bad;
  END IF;
END
$$;
