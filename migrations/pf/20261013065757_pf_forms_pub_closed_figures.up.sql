-- pf_forms_pub: hợp đồng "số liệu đã chốt" thống nhất cho KHTC — nguồn kỳ native `cap/forms` (gói PFPUB, ADR-0052;
-- BRD KN 10/2026 — D4 X6, QĐ-6, Q-047, Q-048). Họ hợp đồng closed_figures/v1: pf_forms_pub.v1_closed_figures (tệp này)
-- và pf_extension_pub.v1_closed_figures (pf 20261013065758, nguồn vòng đời báo cáo cũ) có CỘT VÀ THỨ TỰ GIỐNG HỆT —
-- contracts/pubview/pf_forms_pub.closed_figures.v1.json. Chỉ đụng schema pf_forms_pub (module pf/forms).
--
-- Một dòng = một kỳ native có khóa kỳ đã lưu (period_spec.period_key, W11.05) của profile có phòng nguồn (kn_cnts →
-- chan_nuoi_thuy_san; profile khác — kn chung, kn_khtc là phiếu của chính KHTC — không có dòng; thêm profile =
-- CREATE OR REPLACE VIEW giữ nguyên cột, ADR-0052 §3). Kỳ đã hủy không có dòng. Mỗi kỳ native có đúng một biểu.
-- Tỉnh = tỉnh của Nút phát hành (owner_ref → k_ref.org, cùng quy tắc với pf/extension knActor): TTKN tỉnh → mã đơn vị
-- hành chính của Nút; Nút khác → tỉnh cha của đơn vị hành chính có hiệu lực. Không xác định được tỉnh → không có dòng.
--
-- "Nguồn đã chốt" (QĐ-6 mặc định — ảnh chụp đã chốt): số liệu là ẢNH CHỤP ĐÓNG BĂNG mới nhất của kỳ (close_snapshot,
-- một ảnh mỗi lần chốt; chốt lại → ảnh mới, ảnh cũ không đổi). Mở lại kỳ (cả kỳ hay một phần đơn vị) → cờ chốt về
-- false nhưng số của lần chốt trước vẫn trả (KHTC hiện "Nguồn đã thay đổi"); bài nộp lại khi mở là số tạm, không lọt vào
-- view cho tới lần chốt kế tiếp. Chưa chốt lần nào → cột số liệu NULL.
--   form_closed = period_closed  sự kiện cuối của kỳ là closed (kỳ native một biểu — hai cờ trùng nhau);
--   key_closed  mọi kỳ cùng (phòng, tỉnh, loại kỳ, khóa kỳ) TRONG VIEW NÀY đã chốt (Q-048 đề xuất "mọi biểu nguồn
--               của tuần đã chốt"); bộ đọc của KHTC (extension/port.ClosedFigures) tính lại trên cả hai nguồn.
--
-- Nguồn gốc cho ảnh chụp "Cập nhật số liệu" của KHTC (W11.64): source_module, (calendar_id, calendar_version,
-- period_key), closed_at, figures_hash (sha256 của dòng lõi + totals, KHÔNG gồm trạng thái xác nhận), status_hash (băm
-- riêng của trạng thái xác nhận từng đơn vị — D2), confirmation_status ('unconfirmed' nếu có đơn vị chưa xác nhận).
-- rows: [{key, unit, unit_kind, parent, region, indicator, nil_report, cells, confirmation}] — mỗi đơn vị CÓ bài trong ảnh chụp; cells
-- CHỈ các trường số (integer, decimal) của phiên bản biểu, chuỗi thập phân như đã nộp (gồm trường lũy kế của nguồn —
-- Q-047: KHTC đọc lũy kế của nguồn). totals: '[]' (kỳ native không tổng hợp; bên đọc cộng rows theo decimal).
-- Lịch nguồn (D8 — mỗi phòng một lịch tuần, reportcycle/domain.CalendarOf): (calendar_id, calendar_version) đọc từ
-- ";cal=<phòng>:v<n>" của period_rule (vd chan_nuoi_thuy_san:v3); tag cũ → (phòng, 1|2); period_key = khóa GỐC. Không quy
-- đổi sang tuần KHTC ở đây: bảng chuyển đổi (crosswalk) có phiên bản nằm sau port đọc của KHTC (ADR-0052 §6).
-- Không dữ liệu cá nhân (R-14): KHÔNG submitted_by/reviewed_by/submission_id của ảnh chụp, không trường văn bản tự do
-- (ghi chú, kế hoạch công tác, địa điểm), không actor_ref.
-- Chủ view ag_migrator (NOBYPASSRLS), mọi bảng gốc FORCE RLS (pf_forms.* sổ apply_ledger, k_ref.* apply_tenant_rls) →
-- p_tenant áp qua view (W6.3). Vai r_forms_pub do `cmd/migrate bootstrap` tạo (module sở hữu schema _pub).

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_forms_pub' AND NOT rolcanlogin) THEN
    RAISE EXCEPTION 'thiếu vai nhóm r_forms_pub (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0031, ADR-0052)';
  END IF;
END
$$;

CREATE SCHEMA pf_forms_pub;
REVOKE ALL ON SCHEMA pf_forms_pub FROM PUBLIC;

CREATE VIEW pf_forms_pub.v1_closed_figures AS
  WITH base AS (
    SELECT p.tenant_id, p.id AS period_id, p.template_id, p.template_code, p.label, p.units, p.owner_ref,
           s.unit_kind, s.unit_parents, s.period_kind, s.period_key, s.period_from, s.period_to, s.period_rule,
           CASE p.profile WHEN 'kn_cnts' THEN 'chan_nuoi_thuy_san' END AS department_code,
           (SELECT e.action FROM pf_forms.period_event e
             WHERE e.tenant_id = p.tenant_id AND e.period_ref = p.id ORDER BY e.seq DESC LIMIT 1) AS last_action,
           (SELECT max(e.recorded_at) FROM pf_forms.period_event e
             WHERE e.tenant_id = p.tenant_id AND e.period_ref = p.id AND e.action = 'closed') AS closed_at
      FROM pf_forms.period p
      JOIN pf_forms.period_spec s ON s.tenant_id = p.tenant_id AND s.period_id = p.id AND s.period_key IS NOT NULL
     WHERE p.profile IN ('kn_cnts')),
  prov AS (
    SELECT b.*,
           CASE WHEN o.kind = 'ttkn_province' OR a.level = 'province' OR a.code IS NULL THEN o.admin_unit_code
                ELSE a.parent_code END AS province_code
      FROM base b
      LEFT JOIN k_ref.org o ON o.tenant_id = b.tenant_id AND o.ref = b.owner_ref
      LEFT JOIN LATERAL (
        SELECT u.code, u.level, u.parent_code FROM k_ref.admin_unit u
         WHERE u.tenant_id = b.tenant_id AND u.code = o.admin_unit_code
           AND u.valid_from <= coalesce(b.period_to, b.period_from, CURRENT_DATE)
         ORDER BY u.valid_from DESC LIMIT 1) a ON true
     WHERE b.last_action IS DISTINCT FROM 'cancelled'),
  state AS (
    SELECT x.*, (x.last_action = 'closed') AS closed_now
      FROM prov x
     WHERE x.department_code IS NOT NULL AND x.province_code IS NOT NULL)
  SELECT x.tenant_id, x.department_code, x.province_code,
         -- Danh tính lịch của nguồn (D8: mỗi phòng một lịch): (calendar_id, calendar_version, period_key = khóa gốc).
         coalesce(substring(x.period_rule from ';cal=([a-z][a-z0-9_]{1,39}):v[0-9]{1,4}(;|$)'), x.department_code) AS calendar_id,
         -- calendar_version theo D8 (reportcycle/domain.CalendarOf): tag mang ";cal=<phòng>:v<n>" → n; tag cũ v1-tam → 1,
         -- v2 → 2; tag không phải lịch kỳ (vd tuần làm việc BM-05 "cnts.work_week/v1") → NULL.
         CASE WHEN x.period_rule ~ ';cal=[a-z][a-z0-9_]{1,39}:v[0-9]{1,4}(;|$)'
                THEN substring(x.period_rule from ';cal=[a-z][a-z0-9_]{1,39}:v([0-9]{1,4})(;|$)')::integer
              WHEN x.period_rule LIKE 'kn.period_rule/v1%' THEN 1
              WHEN x.period_rule LIKE 'kn.period_rule/v2%' THEN 2 END AS calendar_version,
         x.period_kind AS period_type, x.period_key,
         CASE WHEN x.period_kind = 'week' AND x.period_key ~ '^[0-9]{4}-W[0-9]{2}$' THEN left(x.period_key, 4)::integer END
           AS week_year,
         x.period_rule, x.period_from, x.period_to, x.label AS period_label,
         'forms'::text AS source, 'pf/forms'::text AS source_module, x.period_id AS source_period_id, x.template_code AS form_code, x.template_id AS form_version_id,
         x.closed_now AS form_closed, x.closed_now AS period_closed,
         -- Cửa sổ chỉ chia theo cột khóa tra cứu → điều kiện WHERE của bên đọc trên các cột này đẩy xuống được.
         bool_and(x.closed_now) OVER (PARTITION BY x.tenant_id, x.department_code, x.province_code, x.period_kind, x.period_key)
           AS key_closed,
         x.closed_at,
         sn.id AS snapshot_id, sn.seq AS snapshot_no, sn.recorded_at AS data_at,
         f.missing > 0 AS provisional, f.total AS units_total, f.with_data AS units_with_data, f.missing_units,
         -- rows công bố = dòng lõi + trạng thái xác nhận (D2: HTX do TTKN tạo "Chưa xác nhận"; hiện mọi đơn vị
         -- 'confirmed' — D2 thay bằng trạng thái thật bằng CREATE OR REPLACE VIEW, cột giữ nguyên).
         CASE WHEN f.rows IS NOT NULL THEN (
           SELECT coalesce(jsonb_agg(e.v || jsonb_build_object('confirmation', 'confirmed') ORDER BY e.n), '[]'::jsonb)
             FROM jsonb_array_elements(f.rows) WITH ORDINALITY e(v, n)) END AS rows,
         CASE WHEN sn.id IS NOT NULL THEN '[]'::jsonb END AS totals,
         'confirmed'::text AS confirmation_status,
         -- Băm số đã chốt (KHÔNG gồm trạng thái xác nhận): đổi khi và chỉ khi số/ô đã chốt đổi — W11.64 so với ảnh chụp
         -- "Cập nhật số liệu" để báo "Nguồn đã thay đổi". Chốt lại không đổi số → băm giữ nguyên.
         CASE WHEN sn.id IS NOT NULL THEN encode(sha256(convert_to(jsonb_build_array(f.rows, '[]'::jsonb)::text, 'UTF8')), 'hex') END
           AS figures_hash,
         -- Băm trạng thái xác nhận riêng: nhận HTX (unconfirmed → confirmed) chỉ đổi băm này.
         CASE WHEN f.rows IS NOT NULL THEN encode(sha256(convert_to((
           SELECT coalesce(jsonb_agg(jsonb_build_array(e.v -> 'key', 'confirmed') ORDER BY e.n), '[]'::jsonb)
             FROM jsonb_array_elements(f.rows) WITH ORDINALITY e(v, n))::text, 'UTF8')), 'hex') END AS status_hash
    FROM state x
    LEFT JOIN LATERAL (
      SELECT c.id, c.seq, c.recorded_at, c.content FROM pf_forms.close_snapshot c
       WHERE c.tenant_id = x.tenant_id AND c.period_ref = x.period_id
       ORDER BY c.seq DESC LIMIT 1) sn ON true
    LEFT JOIN LATERAL (
      SELECT coalesce(array_agg(fl.e ->> 'key'), '{}') AS keys
        FROM pf_forms.template t CROSS JOIN LATERAL jsonb_array_elements(t.fields) fl(e)
       WHERE t.tenant_id = x.tenant_id AND t.id = x.template_id AND fl.e ->> 'type' IN ('integer', 'decimal')) nk ON true
    LEFT JOIN LATERAL (
      SELECT count(*)::integer AS total,
             count(*) FILTER (WHERE u.e ->> 'submission_id' IS NOT NULL)::integer AS with_data,
             count(*) FILTER (WHERE u.e ->> 'submission_id' IS NULL)::integer AS missing,
             coalesce(array_agg(u.e ->> 'unit' ORDER BY u.e ->> 'unit') FILTER (WHERE u.e ->> 'submission_id' IS NULL), '{}')
               AS missing_units,
             coalesce(jsonb_agg(jsonb_build_object(
                 'key', u.e -> 'unit',
                 'unit', u.e -> 'unit',
                 'unit_kind', x.unit_kind,
                 'parent', (SELECT to_jsonb(pp.parent) FROM unnest(x.units, x.unit_parents) pp(unit, parent)
                             WHERE pp.unit = u.e ->> 'unit' LIMIT 1),
                 'region', 'null'::jsonb,
                 'indicator', 'null'::jsonb,
                 'nil_report', coalesce((u.e ->> 'nil_report')::boolean, false),
                 'cells', (SELECT coalesce(jsonb_object_agg(v.key, v.value), '{}'::jsonb)
                             FROM jsonb_each(coalesce(u.e -> 'values', '{}'::jsonb)) v WHERE v.key = ANY (nk.keys)))
               ORDER BY u.e ->> 'unit') FILTER (WHERE u.e ->> 'submission_id' IS NOT NULL), '[]'::jsonb) AS rows
        FROM jsonb_array_elements(sn.content -> 'units') u(e)) f ON sn.id IS NOT NULL;

GRANT USAGE ON SCHEMA pf_forms_pub TO r_forms_pub;
GRANT SELECT ON pf_forms_pub.v1_closed_figures TO r_forms_pub;
