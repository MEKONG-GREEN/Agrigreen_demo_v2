-- pf/cooperative — hợp đồng đọc có phiên bản cho lần chép MỘT LẦN thửa ruộng sang kho KN (KN-STANDALONE-5-TT-DATA #271,
-- migration 20261110013755_pf_extension_kn_plot_copy). Chỉ VIEW, không chép dữ liệu, không đổi bảng gốc pf_coop.
-- Chủ view là ag_migrator (NOBYPASSRLS), pf_coop.plot FORCE RLS → p_tenant áp qua view: người đọc phải đặt
-- app.tenant_id. Chỉ cấp cho r_cooperative_pub (không cấp vai ứng dụng KN). Không tên người (farmer_ref là mã kho định danh).
SET LOCAL lock_timeout = '5s';

CREATE VIEW pf_cooperative_pub.v1_plot_export WITH (security_barrier=true) AS
  SELECT tenant_id, ref AS plot_ref, code, commune_code, cooperative_ref, status, current_boundary_id, geom, area_ha,
    farmer_ref, lot_name, note, declared_area_ha, shape, department_code, created_by, created_at, updated_at
  FROM pf_coop.plot;

REVOKE ALL ON pf_cooperative_pub.v1_plot_export FROM PUBLIC;
GRANT SELECT ON pf_cooperative_pub.v1_plot_export TO r_cooperative_pub;
