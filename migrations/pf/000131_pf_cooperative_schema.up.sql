-- W3.3 (docs/design/cooperative.md mục 8, ADR-0027, ADR-0028): schema pf_coop của module pf/cooperative
-- (dom/cooperative). Bước 1/3 — chỉ tạo schema và quyền nền của vai r_cooperative; 000132 chuyển bảng, 000133 kiểm.
-- Chạy bằng vai ag_migrator. Chỉ tiến (R-24).
--
-- Vai: r_cooperative (W2.6: r_<module> suy từ modreg.Catalog — module có owns_schema; KHÔNG ghi đè thành r_coop
-- như bản thiết kế mục 7.2: giữ một quy tắc tên duy nhất, không bảng ngoại lệ trong modreg.DBRole). Vai nhóm
-- NOLOGIN do `cmd/migrate bootstrap` tạo (ag_migrator không có CREATEROLE) — thiếu vai → lỗi rõ ràng, không bỏ qua.
-- Module không có vai đăng nhập cũ cooperative_app (sinh sau ADR-0028).

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_cooperative' AND NOT rolcanlogin) THEN
    RAISE EXCEPTION 'thiếu vai nhóm r_cooperative (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028, W3.3)';
  END IF;
END
$$;

CREATE SCHEMA pf_coop;
COMMENT ON SCHEMA pf_coop IS
  'pf/cooperative (dom/cooperative, W3.3): hồ sơ HTX, nông hộ/xã viên, thửa, vụ, nhật ký canh tác, SOP, sản lượng. Chủ: ag_migrator; vai chạy: r_cooperative. Tên sổ (TG_ARGV của trg_chain) giữ tên cũ pf_party./pf_geo./pf_cropcycle. — định danh logic bất biến (D-3, quyết định PO 09/10/2026)';
REVOKE ALL ON SCHEMA pf_coop FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_coop TO r_cooperative;

-- Module pf mới có r_* (docs/dev/modules.md, W2.3 bước 5): đọc k_ref + biến ngữ cảnh (R-DB-05).
GRANT USAGE ON SCHEMA k_ref TO r_cooperative;
GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) TO r_cooperative;
GRANT SELECT ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope TO r_cooperative;
