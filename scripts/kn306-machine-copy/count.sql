-- KN-STANDALONE-5-TT-DATA-MAY (#306): đếm và so số máy trước/sau khi chép từ pf_mechanization.coop_machine_count (module
-- CGH, chỉ đọc) sang kho KN. Chạy bằng vai quản trị (psql -f), chỉ SELECT. Mỗi dòng là một (tenant, xã, loại máy): số dòng
-- khai báo và tổng số máy ở nguồn (dòng đang khai báo) và ở phần đã chép vào kho KN (dòng sổ 'migrated' — ảnh chụp lúc
-- chép, không đổi khi KN sửa tiếp). Kết quả rỗng = khớp hoàn toàn.
WITH src AS (
  SELECT tenant_id, commune_code, machine_type, count(*) AS n, sum(quantity) AS qty
  FROM pf_mechanization.coop_machine_count WHERE active
  GROUP BY tenant_id, commune_code, machine_type
), dst AS (
  SELECT tenant_id, commune_code, machine_type, count(*) AS n, sum(quantity) AS qty
  FROM pf_extension.kn_machine_count_event WHERE action = 'migrated'
  GROUP BY tenant_id, commune_code, machine_type
)
SELECT coalesce(s.tenant_id, d.tenant_id) AS tenant_id, coalesce(s.commune_code, d.commune_code) AS commune_code,
       coalesce(s.machine_type, d.machine_type) AS machine_type,
       coalesce(s.n, 0) AS src_lines, coalesce(d.n, 0) AS kn_lines, coalesce(s.qty, 0) AS src_quantity, coalesce(d.qty, 0) AS kn_quantity
FROM src s FULL JOIN dst d ON d.tenant_id = s.tenant_id AND d.commune_code = s.commune_code AND d.machine_type = s.machine_type
WHERE coalesce(s.n, 0) <> coalesce(d.n, 0) OR coalesce(s.qty, 0) <> coalesce(d.qty, 0)
ORDER BY 1, 2, 3;
