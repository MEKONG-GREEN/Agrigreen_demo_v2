-- Hợp đồng nguồn module `_pub` cho KHTC (W11.10 — BRD KN 10/2026, đối chiếu Phụ lục D X6, BG10 phần dữ liệu; QĐ-6,
-- Q-048): view "số liệu đã chốt theo kỳ báo cáo" của các phòng KN chạy vòng đời báo cáo pf/extension (Trồng trọt
-- bây giờ; CNTS sau, cùng view — mọi dòng mang department_code). Hợp đồng: contracts/pubview/
-- pf_extension_pub.consolidation_closed.v1.json. Chỉ đụng schema pf_extension_pub (migration công bố <cụm>_pub_*).
--
-- Một dòng = một kỳ × một biểu của đợt đã phát hành (không thu hồi) CÓ khóa kỳ đã lưu (sổ phụ report_period_key,
-- pf 000143 — kỳ ngày/tuần/tuần KSGM/tháng/quý/năm; vụ/đột xuất và kỳ phát hành trước pf 000143 không có khóa → không
-- có dòng). Khóa tra cứu: (department_code, province_code, period_type, period_key).
--
-- "Nguồn đã chốt" (QĐ-6 mặc định — ảnh chụp đã chốt): số liệu là LẦN TỔNG HỢP CÁC XÃ (consolidation_run +
-- consolidation_snapshot, bất biến) mới nhất của kỳ × biểu ghi KHÔNG MUỘN HƠN lần chốt gần nhất chạm biểu đó (closed,
-- auto_closed: cả kỳ; reclosed: biểu trong template_ids hoặc mọi biểu). Lúc chốt hệ thống tổng hợp cùng giao dịch
-- (recorded_at bằng nhau); không có thay đổi thì không ghi lần mới — lần trước đó chính là số lúc chốt. Lần "Cập nhật"
-- tay SAU khi mở lại (số tạm của kỳ đang mở lại) không lọt vào view cho tới khi chốt lại. Chưa chốt lần nào → cột số
-- liệu NULL. Đọc lại bất kỳ lúc nào cho cùng kết quả cho tới lần chốt kế tiếp (nguồn chỉ ghi thêm).
--
-- Cờ chốt của kỳ nguồn (nguồn sửa được tới khi chốt — QĐ-6):
--   form_closed   kỳ đã chốt VÀ không xã nào của biểu đang mở lại (report_task.reopened_until còn giá trị);
--   period_closed kỳ đã chốt VÀ không xã × biểu nào đang mở lại;
--   key_closed    MỌI biểu của MỌI kỳ cùng (phòng, tỉnh, loại kỳ, khóa kỳ) đã chốt — Q-048 đề xuất "mọi biểu nguồn
--                 của tuần đã chốt" (giả định ở docs/kn/gia-dinh-brd-2026-10.md mục W11.10).
--
-- Không dữ liệu cá nhân (R-14): không actor_ref/người gửi (consolidation_snapshot.sources KHÔNG công bố), không tên xã/
-- khu vực (region_snapshot không công bố); rows/totals chỉ có mã xã, mã khu vực, nhãn chỉ tiêu, ô số (chuỗi thập phân).
-- Chủ view ag_migrator (NOBYPASSRLS), mọi bảng gốc FORCE RLS → p_tenant áp qua view (W6.3).

CREATE VIEW pf_extension_pub.v1_consolidation_closed AS
  WITH form AS (
    SELECT p.tenant_id, p.id AS period_id, p.batch_id, p.department_code, p.province_code,
           k.period_type, k.period_key, k.week_year, k.period_rule, p.from_date, p.to_date, p.label, p.status,
           (f.e ->> 'template_id')::uuid AS template_id, f.e ->> 'code' AS form_code,
           (f.e ->> 'version_id')::uuid AS form_version_id
      FROM pf_extension.report_period p
      JOIN pf_extension.report_batch b ON b.tenant_id = p.tenant_id AND b.id = p.batch_id AND b.status = 'issued'
      JOIN pf_extension.report_period_key k ON k.tenant_id = p.tenant_id AND k.period_id = p.id
      CROSS JOIN LATERAL jsonb_array_elements(b.forms) AS f(e)),
  state AS (
    SELECT x.*,
           (x.status = 'closed' AND NOT EXISTS (
              SELECT 1 FROM pf_extension.report_task t
               WHERE t.tenant_id = x.tenant_id AND t.period_id = x.period_id AND t.template_id = x.template_id
                 AND NOT t.withdrawn AND t.reopened_until IS NOT NULL)) AS form_closed,
           (x.status = 'closed' AND NOT EXISTS (
              SELECT 1 FROM pf_extension.report_task t
               WHERE t.tenant_id = x.tenant_id AND t.period_id = x.period_id
                 AND NOT t.withdrawn AND t.reopened_until IS NOT NULL)) AS period_closed,
           (SELECT max(e.recorded_at) FROM pf_extension.period_event e
             WHERE e.tenant_id = x.tenant_id AND e.period_id = x.period_id
               AND (e.action IN ('closed', 'auto_closed')
                    OR (e.action = 'reclosed' AND (cardinality(e.template_ids) = 0 OR x.template_id = ANY (e.template_ids))))
           ) AS closed_at
      FROM form x)
  SELECT s.tenant_id, s.department_code, s.province_code, s.period_type, s.period_key, s.week_year, s.period_rule,
         s.from_date AS period_from, s.to_date AS period_to, s.label AS period_label, s.period_id, s.batch_id,
         s.template_id, s.form_code, s.form_version_id,
         s.form_closed, s.period_closed,
         -- Cửa sổ chỉ chia theo cột khóa tra cứu → điều kiện WHERE của bên đọc trên các cột này đẩy xuống được.
         bool_and(s.form_closed) OVER (PARTITION BY s.tenant_id, s.department_code, s.province_code, s.period_type, s.period_key)
           AS key_closed,
         s.closed_at,
         r.id AS run_id, r.run_no, r.recorded_at AS run_at, r.provisional, r.communes_total, r.communes_with_data,
         r.missing_communes, c.rows, c.totals
    FROM state s
    LEFT JOIN LATERAL (
      SELECT x.id, x.run_no, x.recorded_at, x.provisional, x.communes_total, x.communes_with_data, x.missing_communes
        FROM pf_extension.consolidation_run x
       WHERE x.tenant_id = s.tenant_id AND x.period_id = s.period_id AND x.template_id = s.template_id
         AND s.closed_at IS NOT NULL AND x.recorded_at <= s.closed_at
       ORDER BY x.run_no DESC
       LIMIT 1) r ON true
    LEFT JOIN pf_extension.consolidation_snapshot c ON c.tenant_id = s.tenant_id AND c.run_id = r.id;

GRANT SELECT ON pf_extension_pub.v1_consolidation_closed TO r_extension_pub;
