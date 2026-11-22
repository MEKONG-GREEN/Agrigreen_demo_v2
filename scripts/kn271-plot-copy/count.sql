-- KN-STANDALONE-5-TT-DATA (#271): đếm và so thửa trước/sau khi chép từ pf_coop.plot (module HTX, chỉ đọc) sang kho KN
-- pf_extension.kn_plot. Chạy bằng vai quản trị (psql -f), chỉ SELECT. Mỗi dòng là một (tenant, xã): số thửa và tổng diện
-- tích (ha) ở nguồn (thửa chưa ngừng dùng) và ở kho KN (thửa đã chép, event migrated). Kết quả rỗng = khớp hoàn toàn.
WITH src AS (
  SELECT tenant_id, commune_code, count(*) AS n, sum(area_ha) AS ha
  FROM pf_coop.plot WHERE status <> 'retired'
  GROUP BY tenant_id, commune_code
), dst AS (
  SELECT k.tenant_id, k.commune_code, count(*) AS n, sum(k.area_ha) AS ha
  FROM pf_extension.kn_plot k
  WHERE EXISTS (SELECT 1 FROM pf_extension.kn_plot_version v
                WHERE v.tenant_id = k.tenant_id AND v.plot_ref = k.ref AND v.event = 'migrated')
  GROUP BY k.tenant_id, k.commune_code
)
SELECT coalesce(s.tenant_id, d.tenant_id) AS tenant_id, coalesce(s.commune_code, d.commune_code) AS commune_code,
       coalesce(s.n, 0) AS src_plots, coalesce(d.n, 0) AS kn_plots, coalesce(s.ha, 0) AS src_area_ha, coalesce(d.ha, 0) AS kn_area_ha
FROM src s FULL JOIN dst d ON d.tenant_id = s.tenant_id AND d.commune_code = s.commune_code
WHERE coalesce(s.n, 0) <> coalesce(d.n, 0) OR coalesce(s.ha, 0) <> coalesce(d.ha, 0)
ORDER BY 1, 2;
