-- PA1-HTX-SUPPORT-GPSLOG (#313): đếm yêu cầu hỗ trợ trước/sau khi chép một lần từ bảng KN pf_extension.support_request
-- (chỉ đọc sau migration 20261116020729) sang bảng HTX pf_coop.support_request (migration 20261116020731).
-- Chạy bằng vai quản trị (psql -f), chỉ SELECT.
--   1. TRƯỚC khi chép (chạy được bất cứ lúc nào): số dòng nguồn KN theo tenant — lưu lại kết quả.
--   2. SAU khi chép: so từng (tenant, HTX) — kn_rows = htx_copied, thiếu GPS và số ảnh khớp, cột khop = true; dòng cuối là
--      tổng. Dòng HTX tạo mới sau migration (legacy_task_id NULL) không tính vào htx_copied.

-- 1. Trước
SELECT tenant_id, count(*) AS kn_rows FROM pf_extension.support_request GROUP BY tenant_id ORDER BY tenant_id;

-- 2. Sau
WITH src AS (
  SELECT tenant_id, cooperative_ref, count(*) AS n, count(*) FILTER (WHERE location IS NULL) AS no_gps,
         sum(cardinality(photo_refs)) AS photos
  FROM pf_extension.support_request
  GROUP BY tenant_id, cooperative_ref
), dst AS (
  SELECT tenant_id, cooperative_ref, count(*) AS n, count(*) FILTER (WHERE location IS NULL) AS no_gps,
         sum(cardinality(photo_refs)) AS photos
  FROM pf_coop.support_request WHERE legacy_task_id IS NOT NULL
  GROUP BY tenant_id, cooperative_ref
), cmp AS (
  SELECT coalesce(s.tenant_id, d.tenant_id)::text AS tenant_id, coalesce(s.cooperative_ref, d.cooperative_ref)::text AS cooperative_ref,
         coalesce(s.n, 0) AS kn_rows, coalesce(d.n, 0) AS htx_copied,
         coalesce(s.no_gps, 0) AS kn_no_gps, coalesce(d.no_gps, 0) AS htx_no_gps,
         coalesce(s.photos, 0) AS kn_photos, coalesce(d.photos, 0) AS htx_photos
  FROM src s FULL JOIN dst d ON d.tenant_id = s.tenant_id AND d.cooperative_ref = s.cooperative_ref
)
SELECT * FROM (
SELECT tenant_id, cooperative_ref, kn_rows, htx_copied, kn_no_gps, htx_no_gps, kn_photos, htx_photos,
       (kn_rows = htx_copied AND kn_no_gps = htx_no_gps AND kn_photos = htx_photos) AS khop
FROM cmp
UNION ALL
SELECT 'TONG', '', sum(kn_rows), sum(htx_copied), sum(kn_no_gps), sum(htx_no_gps), sum(kn_photos), sum(htx_photos),
       bool_and(kn_rows = htx_copied AND kn_no_gps = htx_no_gps AND kn_photos = htx_photos)
FROM cmp
) r
ORDER BY (tenant_id = 'TONG'), tenant_id, cooperative_ref;
