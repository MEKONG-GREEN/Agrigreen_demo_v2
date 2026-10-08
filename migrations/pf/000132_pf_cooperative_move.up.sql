-- W3.3 (docs/design/cooperative.md mục 2, 8.2–8.4): chuyển dữ liệu HTX/hộ/thửa/vụ sang schema pf_coop của
-- pf/cooperative bằng ALTER … SET SCHEMA — không sao chép, không UPDATE/DELETE (R-25). OID, dữ liệu, chỉ mục,
-- ràng buộc, trigger (trg_chain, trg_immutable*, trg_no_delete, guard), chính sách p_tenant và FORCE RLS đi theo
-- quan hệ. Bước 2/3 (000131 tạo schema, 000133 chỉ kiểm). Chạy bằng ag_migrator, một giao dịch.
--
-- Tên sổ GIỮ NGUYÊN (D-3, quyết định PO 09/10/2026 — phương án A của mục 8.3): tham số TG_ARGV[0] của trg_chain
-- vẫn là tên cũ ('pf_geo.plot_boundary', 'pf_cropcycle.farm_log', …). shared.chain_head, ledger_idem,
-- chain_anchor KHÔNG đổi — chuỗi băm, chống trùng và mỏ neo liên tục; `migrate verify-chains` không đổi mã.
-- Riêng shared.ledger_added_column (shared 000011) khóa theo tên bảng THẬT (format('%I.%I', schema, bảng)) chứ
-- không theo tên sổ, nên đăng ký bù cho tên mới bằng INSERT (dòng cũ giữ nguyên) — thiếu bước này dòng ghi trước
-- pf 000079 trên plot_boundary/plot_event sẽ báo đứt chuỗi giả.
--
-- Không tương thích ngược ở mức SQL: nhị phân cũ (SQL pf_geo.plot …) hỏng sau migration này — chạy trong cửa sổ
-- bảo trì (docs/runbooks/cooperative-schema-move.md, H-17).

SET LOCAL lock_timeout = '5s';

-- 0. Khóa theo thứ tự cố định (party → geo → cropcycle; tránh deadlock với job maintain/anchor). LOCK trên bảng
--    phân vùng khóa cả phân vùng.
LOCK TABLE pf_party.cooperative, pf_party.household, pf_party.household_membership, pf_party.kn_profile_event,
           pf_geo.plot, pf_geo.plot_boundary, pf_geo.plot_event, pf_geo.boundary_conflict,
           pf_geo.cooperative_boundary, pf_geo.cooperative_area,
           pf_cropcycle.sop_template, pf_cropcycle.sop_version, pf_cropcycle.variety_sop_binding,
           pf_cropcycle.season_event, pf_cropcycle.farm_log_batch, pf_cropcycle.farm_log,
           pf_cropcycle.farm_log_review, pf_cropcycle.harvest_declaration, pf_cropcycle.season_current,
           pf_cropcycle.farm_log_current, pf_cropcycle.coop_season_area, pf_cropcycle.yield_summary
  IN ACCESS EXCLUSIVE MODE;

-- 1. Chuỗi số độc lập (không OWNED BY). DEFAULT nextval(regclass) bám OID nên tự theo; giá trị giữ nguyên
--    (con trỏ sync_seq của client không phải đồng bộ lại).
ALTER SEQUENCE pf_geo.plot_code_seq SET SCHEMA pf_coop;
ALTER SEQUENCE pf_geo.plot_sync_seq SET SCHEMA pf_coop;
ALTER SEQUENCE pf_cropcycle.sop_sync_seq SET SCHEMA pf_coop;
ALTER SEQUENCE pf_cropcycle.season_sync_seq SET SCHEMA pf_coop;
ALTER SEQUENCE pf_cropcycle.farm_log_sync_seq SET SCHEMA pf_coop;

-- 2. Hàm trigger của schema module (trigger bám OID hàm).
ALTER FUNCTION pf_geo.fn_boundary_conflict_guard() SET SCHEMA pf_coop;
ALTER FUNCTION pf_cropcycle.fn_sop_template_guard() SET SCHEMA pf_coop;
ALTER FUNCTION pf_cropcycle.fn_sop_version_guard() SET SCHEMA pf_coop;
ALTER FUNCTION pf_cropcycle.fn_variety_binding_guard() SET SCHEMA pf_coop;

-- 3. 22 bảng cha (mục 2.4). FK chéo còn lại: pf_coop.{cooperative,household_membership}.org_ref → pf_party.org (D-7).
ALTER TABLE pf_party.cooperative SET SCHEMA pf_coop;
ALTER TABLE pf_party.household SET SCHEMA pf_coop;
ALTER TABLE pf_party.household_membership SET SCHEMA pf_coop;
ALTER TABLE pf_party.kn_profile_event SET SCHEMA pf_coop;
ALTER TABLE pf_geo.plot SET SCHEMA pf_coop;
ALTER TABLE pf_geo.plot_boundary SET SCHEMA pf_coop;
ALTER TABLE pf_geo.plot_event SET SCHEMA pf_coop;
ALTER TABLE pf_geo.boundary_conflict SET SCHEMA pf_coop;
ALTER TABLE pf_geo.cooperative_boundary SET SCHEMA pf_coop;
ALTER TABLE pf_geo.cooperative_area SET SCHEMA pf_coop;
ALTER TABLE pf_cropcycle.sop_template SET SCHEMA pf_coop;
ALTER TABLE pf_cropcycle.sop_version SET SCHEMA pf_coop;
ALTER TABLE pf_cropcycle.variety_sop_binding SET SCHEMA pf_coop;
ALTER TABLE pf_cropcycle.season_event SET SCHEMA pf_coop;
ALTER TABLE pf_cropcycle.farm_log_batch SET SCHEMA pf_coop;
ALTER TABLE pf_cropcycle.farm_log SET SCHEMA pf_coop;
ALTER TABLE pf_cropcycle.farm_log_review SET SCHEMA pf_coop;
ALTER TABLE pf_cropcycle.harvest_declaration SET SCHEMA pf_coop;
ALTER TABLE pf_cropcycle.season_current SET SCHEMA pf_coop;
ALTER TABLE pf_cropcycle.farm_log_current SET SCHEMA pf_coop;
ALTER TABLE pf_cropcycle.coop_season_area SET SCHEMA pf_coop;
ALTER TABLE pf_cropcycle.yield_summary SET SCHEMA pf_coop;

-- 4. Phân vùng: SET SCHEMA của bảng cha KHÔNG chuyển phân vùng (mỗi phân vùng là quan hệ riêng; INSERT qua bảng
--    cha vẫn định tuyến đúng nên lỗi không lộ ngay — rủi ro R-1). Tên lấy từ pg_inherits (analyzer agmodgraph
--    không thấy SQL động; 000133 kiểm không còn phân vùng nào ở schema cũ). shared.create_monthly_partitions lấy
--    schema của bảng cha, nên phân vùng job maintain tạo sau này nằm ở pf_coop.
DO $$
DECLARE
  r record;
BEGIN
  FOR r IN
    SELECT format('%I.%I', n.nspname, c.relname) AS part
      FROM pg_inherits i
      JOIN pg_class c ON c.oid = i.inhrelid
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_class p ON p.oid = i.inhparent
      JOIN pg_namespace pn ON pn.oid = p.relnamespace
     WHERE pn.nspname = 'pf_coop' AND n.nspname <> 'pf_coop'
       AND c.relkind IN ('r', 'p') -- pg_inherits có cả chỉ mục phân vùng; chỉ mục đi theo bảng của nó
     ORDER BY 1
  LOOP
    EXECUTE format('ALTER TABLE %s SET SCHEMA pf_coop', r.part);
  END LOOP;
END
$$;

-- 5. Cột thêm vào sổ (shared 000011) — đăng ký bù dưới tên bảng mới, cùng đợt; dòng tên cũ giữ (vô hại: chỉ dùng
--    khi tên bảng khớp).
INSERT INTO shared.ledger_added_column (ledger_table, column_name, epoch, registered_at)
SELECT 'pf_coop.' || split_part(a.ledger_table, '.', 2), a.column_name, a.epoch, a.registered_at
  FROM shared.ledger_added_column a
 WHERE a.ledger_table IN ('pf_party.kn_profile_event', 'pf_geo.plot_boundary', 'pf_geo.plot_event',
                          'pf_geo.cooperative_boundary', 'pf_cropcycle.season_event', 'pf_cropcycle.farm_log_batch',
                          'pf_cropcycle.farm_log', 'pf_cropcycle.farm_log_review', 'pf_cropcycle.harvest_declaration')
ON CONFLICT (ledger_table, column_name) DO NOTHING;

-- 6. Quyền (mục 8.4). ACL đi theo quan hệ khi SET SCHEMA → thu hồi MỌI quyền cấp cho vai khác chủ (r_party,
--    r_geo, r_cropcycle; vai cũ party_app/geo_app/cropcycle_app nếu còn sót) trên bảng, cột, chuỗi số của pf_coop,
--    rồi cấp lại cho r_cooperative. Vòng lặp đọc ACL thật nên không phụ thuộc danh sách vai.
DO $$
DECLARE
  r record;
BEGIN
  FOR r IN
    SELECT DISTINCT c.oid::regclass::text AS rel, c.relkind, g.rolname
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      CROSS JOIN LATERAL aclexplode(c.relacl) e
      JOIN pg_roles g ON g.oid = e.grantee
     WHERE n.nspname = 'pf_coop' AND e.grantee <> c.relowner
    UNION
    SELECT DISTINCT c.oid::regclass::text, c.relkind, g.rolname
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
      CROSS JOIN LATERAL aclexplode(a.attacl) e
      JOIN pg_roles g ON g.oid = e.grantee
     WHERE n.nspname = 'pf_coop' AND e.grantee <> c.relowner
  LOOP
    IF r.relkind = 'S' THEN
      EXECUTE format('REVOKE ALL ON SEQUENCE %s FROM %I', r.rel, r.rolname);
    ELSE
      EXECUTE format('REVOKE ALL ON TABLE %s FROM %I', r.rel, r.rolname); -- mức bảng thu hồi cả quyền theo cột
    END IF;
  END LOOP;
END
$$;

-- 9 sổ: chỉ SELECT, INSERT (R-05, R-20).
GRANT SELECT, INSERT ON pf_coop.kn_profile_event, pf_coop.plot_boundary, pf_coop.plot_event,
  pf_coop.cooperative_boundary, pf_coop.season_event, pf_coop.farm_log_batch, pf_coop.farm_log,
  pf_coop.farm_log_review, pf_coop.harvest_declaration TO r_cooperative;
-- Bảng hiện tại / quy trình: không DELETE (trg_no_delete, guard).
GRANT SELECT, INSERT, UPDATE ON pf_coop.cooperative, pf_coop.household, pf_coop.household_membership,
  pf_coop.plot, pf_coop.boundary_conflict, pf_coop.cooperative_area, pf_coop.sop_template, pf_coop.sop_version,
  pf_coop.variety_sop_binding TO r_cooperative;
-- Bảng chiếu dựng lại từ sổ (R-21, R-22; như pf 000021).
GRANT SELECT, INSERT, UPDATE, DELETE ON pf_coop.season_current, pf_coop.farm_log_current, pf_coop.coop_season_area,
  pf_coop.yield_summary TO r_cooperative;
GRANT USAGE ON SEQUENCE pf_coop.plot_code_seq, pf_coop.plot_sync_seq, pf_coop.sop_sync_seq, pf_coop.season_sync_seq,
  pf_coop.farm_log_sync_seq TO r_cooperative;

-- Tên sổ giữ nguyên (D-3).
COMMENT ON TABLE pf_coop.kn_profile_event IS 'sổ — tên sổ (TG_ARGV, chain_head) giữ ''pf_party.kn_profile_event'' sau W3.3 (D-3)';
COMMENT ON TABLE pf_coop.plot_boundary IS 'sổ — tên sổ (TG_ARGV, chain_head) giữ ''pf_geo.plot_boundary'' sau W3.3 (D-3)';
COMMENT ON TABLE pf_coop.plot_event IS 'sổ — tên sổ (TG_ARGV, chain_head) giữ ''pf_geo.plot_event'' sau W3.3 (D-3)';
COMMENT ON TABLE pf_coop.cooperative_boundary IS 'sổ — tên sổ (TG_ARGV, chain_head) giữ ''pf_geo.cooperative_boundary'' sau W3.3 (D-3)';
COMMENT ON TABLE pf_coop.season_event IS 'sổ — tên sổ (TG_ARGV, chain_head) giữ ''pf_cropcycle.season_event'' sau W3.3 (D-3)';
COMMENT ON TABLE pf_coop.farm_log_batch IS 'sổ — tên sổ (TG_ARGV, chain_head) giữ ''pf_cropcycle.farm_log_batch'' sau W3.3 (D-3)';
COMMENT ON TABLE pf_coop.farm_log IS 'sổ — tên sổ (TG_ARGV, chain_head) giữ ''pf_cropcycle.farm_log'' sau W3.3 (D-3)';
COMMENT ON TABLE pf_coop.farm_log_review IS 'sổ — tên sổ (TG_ARGV, chain_head) giữ ''pf_cropcycle.farm_log_review'' sau W3.3 (D-3)';
COMMENT ON TABLE pf_coop.harvest_declaration IS 'sổ — tên sổ (TG_ARGV, chain_head) giữ ''pf_cropcycle.harvest_declaration'' sau W3.3 (D-3)';
