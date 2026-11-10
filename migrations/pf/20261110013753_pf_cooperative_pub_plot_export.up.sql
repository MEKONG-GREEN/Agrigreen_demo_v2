-- pf/cooperative — hợp đồng đọc có phiên bản cho lần chép MỘT LẦN thửa ruộng sang kho KN (KN-STANDALONE-5-TT-DATA #271,
-- migration 20261110013755_pf_extension_kn_plot_copy). Chỉ VIEW, không chép dữ liệu, không đổi bảng gốc pf_coop.
-- Chủ view là ag_migrator (NOBYPASSRLS), pf_coop.plot FORCE RLS → p_tenant áp qua view: người đọc phải đặt
-- app.tenant_id. Chỉ cấp cho r_cooperative_pub (không cấp vai ứng dụng KN). Không tên người (farmer_ref là mã kho định danh).
SET LOCAL lock_timeout = '5s';

CREATE VIEW pf_cooperative_pub.v1_plot_export WITH (security_barrier=true) AS
  SELECT tenant_id, ref AS plot_ref, code, commune_code, cooperative_ref, status, current_boundary_id, geom, area_ha,
    farmer_ref, lot_name, note, declared_area_ha, shape, department_code, created_by, created_at, updated_at
  FROM pf_coop.plot;

-- Tenant có thửa (để bên đọc lần lượt đặt app.tenant_id — vai migration không bỏ qua RLS): đầu chuỗi băm của sổ phiên bản
-- ranh giới thửa (tên sổ giữ 'pf_geo.plot_boundary' từ trước khi dời schema; mỗi thửa có ít nhất một phiên bản ranh giới).
-- shared.chain_head không RLS (chỉ siêu dữ liệu chuỗi băm); view chỉ trả mã tenant.
CREATE VIEW pf_cooperative_pub.v1_plot_export_tenants WITH (security_barrier=true) AS
  SELECT DISTINCT tenant_id FROM shared.chain_head WHERE ledger = 'pf_geo.plot_boundary';

REVOKE ALL ON pf_cooperative_pub.v1_plot_export, pf_cooperative_pub.v1_plot_export_tenants FROM PUBLIC;
GRANT SELECT ON pf_cooperative_pub.v1_plot_export, pf_cooperative_pub.v1_plot_export_tenants TO r_cooperative_pub;
