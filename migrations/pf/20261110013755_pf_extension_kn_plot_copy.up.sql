-- pf/extension — KN-STANDALONE-5-TT-DATA (#271): chép MỘT LẦN thửa đang dùng (status <> 'retired') của module HTX vào kho
-- KN (pf_extension.kn_plot_version, kn_plot; migration 20261110013754). Nguồn đọc qua hợp đồng công bố
-- pf_cooperative_pub.v1_plot_export (migration 20261110013753) — KN không nhắc bảng riêng của HTX, không sửa/xóa gì ở HTX.
-- Giữ nguyên ref, mã, hình, diện tích, thuộc tính v7.7 để hồ sơ cây trồng (plot-profiles) và tham chiếu cũ vẫn khớp.
-- HTX nền tảng → hồ sơ KN loại HTX đang liên kết (htx_ref của phiên bản mới nhất; nhiều hồ sơ thì mã nhỏ nhất);
-- không có → NULL (thửa chưa gán HTX của KN), HTX cũ giữ ở legacy_cooperative_ref để đối chiếu.
-- Vai chạy migration (ag_migrator) NOBYPASSRLS, mọi bảng FORCE RLS: chép theo từng tenant (đặt app.tenant_id); danh sách
-- tenant lấy từ đầu chuỗi băm của mọi sổ (shared.chain_head — siêu dữ liệu không RLS; tenant có thửa luôn có sổ ranh giới),
-- view công bố tự lọc thửa của tenant đang đặt.
-- Chạy lại được (bỏ qua thửa đã có ở kho KN). Đếm/so trước–sau và quay lui: scripts/kn271-plot-copy/{count,rollback}.sql.
SET LOCAL lock_timeout = '5s';

DO $$
DECLARE
  t uuid;
  prev text := current_setting('app.tenant_id', true);
BEGIN
  FOR t IN SELECT DISTINCT tenant_id FROM shared.chain_head ORDER BY tenant_id LOOP
    PERFORM set_config('app.tenant_id', t::text, true);

    INSERT INTO pf_extension.kn_plot_version (id, tenant_id, stream_key, plot_ref, event, code, commune_code, boundary_id, geom,
        area_ha, cooperative_ref, farmer_ref, lot_name, note, declared_area_ha, shape, department_code, source, event_time,
        actor_ref, idem_key, rule_version)
      SELECT s.current_boundary_id, s.tenant_id, 'kn_plot:' || s.plot_ref::text, s.plot_ref, 'migrated', s.code, s.commune_code,
        s.current_boundary_id, s.geom, s.area_ha, m.producer_ref, s.farmer_ref, s.lot_name, s.note, s.declared_area_ha, s.shape,
        s.department_code, 'migrated', s.updated_at, s.created_by, 'kn271:migrate:' || s.plot_ref::text, 'kn-plot-1'
      FROM pf_cooperative_pub.v1_plot_export s
      LEFT JOIN LATERAL (
        SELECT min(cur.producer_ref::text)::uuid AS producer_ref
        FROM (SELECT DISTINCT ON (k.producer_ref) k.producer_ref, k.htx_ref
              FROM pf_extension.kn_producer k WHERE k.kind = 'cooperative'
              ORDER BY k.producer_ref, k.seq DESC) cur
        WHERE cur.htx_ref = s.cooperative_ref
      ) m ON s.cooperative_ref IS NOT NULL
      WHERE s.tenant_id = t AND s.status <> 'retired'
        AND NOT EXISTS (SELECT 1 FROM pf_extension.kn_plot k WHERE k.tenant_id = s.tenant_id AND k.ref = s.plot_ref)
      ORDER BY s.plot_ref;

    INSERT INTO pf_extension.kn_plot (tenant_id, ref, code, commune_code, cooperative_ref, farmer_ref, lot_name, note,
        declared_area_ha, shape, department_code, version, boundary_id, geom, area_ha, legacy_cooperative_ref, created_by,
        created_at, updated_at)
      SELECT v.tenant_id, v.plot_ref, v.code, v.commune_code, v.cooperative_ref, v.farmer_ref, v.lot_name, v.note,
        v.declared_area_ha, v.shape, v.department_code, v.seq, v.boundary_id, v.geom, v.area_ha, s.cooperative_ref,
        s.created_by, s.created_at, s.updated_at
      FROM pf_extension.kn_plot_version v
      JOIN pf_cooperative_pub.v1_plot_export s ON s.tenant_id = v.tenant_id AND s.plot_ref = v.plot_ref
      WHERE v.tenant_id = t AND v.event = 'migrated'
        AND NOT EXISTS (SELECT 1 FROM pf_extension.kn_plot k WHERE k.tenant_id = v.tenant_id AND k.ref = v.plot_ref);
  END LOOP;
  PERFORM set_config('app.tenant_id', coalesce(prev, ''), true);
END $$;
