-- pf_extension_pub: hợp đồng "số liệu đã chốt" thống nhất cho KHTC — nguồn vòng đời báo cáo extension/reportcycle
-- (Trồng trọt — nguồn chính thức, lâu dài) (gói PFPUB, ADR-0052; BRD KN 10/2026 — D4 X6, QĐ-6, Q-047, Q-048). Họ hợp đồng
-- closed_figures/v1: CỘT VÀ THỨ TỰ GIỐNG HỆT pf_forms_pub.v1_closed_figures (pf 20261013065757, nguồn kỳ native) —
-- contracts/pubview/pf_extension_pub.closed_figures.v1.json. Chỉ đụng schema pf_extension_pub (module pf/extension).
--
-- Chiếu lại pf_extension_pub.v1_consolidation_closed (pf 000144 — ngữ nghĩa "nguồn đã chốt" giữ một chỗ): một dòng mỗi
-- kỳ × biểu có khóa kỳ đã lưu; số liệu = lần tổng hợp các xã mới nhất ghi không muộn hơn lần chốt gần nhất chạm biểu;
-- cờ form_closed/period_closed/key_closed như pf 000144. Đổi tên cột về hợp đồng chung: run_* → snapshot_*,
-- communes_* → units_*, period_id → source_period_id; bỏ batch_id, template_id (riêng của nguồn này).
-- rows chuẩn hóa về [{key, unit, unit_kind = commune, parent = null, region, indicator, nil_report = false, cells}];
-- totals giữ nguyên dạng [{key, kind, region, indicator, members, cells}] (dòng Khu vực/Toàn tỉnh do nguồn tổng hợp).
-- Lịch nguồn (D8, reportcycle/domain.CalendarOf): (calendar_id, calendar_version) từ ";cal=<phòng>:v<n>" của
-- period_rule; tag cũ → (phòng, 1 nếu v1-tam, 2 nếu v2); period_key = khóa gốc.
-- Nguồn gốc (W11.64), trạng thái xác nhận (D2), băm số/băm trạng thái: như pf 20261013065757.
-- Nguồn LÂU DÀI (quyết định 13/10/2026: Trồng trọt giữ vòng đời báo cáo riêng, không cắt sang cap/forms — W11.04 hủy).
-- Không dữ liệu cá nhân (R-14): view gốc không công bố sources/actor_ref.

CREATE VIEW pf_extension_pub.v1_closed_figures AS
  SELECT c.tenant_id, c.department_code, c.province_code,
         coalesce(substring(c.period_rule from ';cal=([a-z][a-z0-9_]{1,39}):v[0-9]{1,4}(;|$)'), c.department_code) AS calendar_id,
         -- calendar_version theo D8 (reportcycle/domain.CalendarOf): tag mang ";cal=<phòng>:v<n>" → n; tag cũ v1-tam → 1,
         -- v2 → 2; tag không phải lịch kỳ (vd tuần làm việc BM-05 "cnts.work_week/v1") → NULL.
         CASE WHEN c.period_rule ~ ';cal=[a-z][a-z0-9_]{1,39}:v[0-9]{1,4}(;|$)'
                THEN substring(c.period_rule from ';cal=[a-z][a-z0-9_]{1,39}:v([0-9]{1,4})(;|$)')::integer
              WHEN c.period_rule LIKE 'kn.period_rule/v1%' THEN 1
              WHEN c.period_rule LIKE 'kn.period_rule/v2%' THEN 2 END AS calendar_version,
         c.period_type, c.period_key, c.week_year, c.period_rule,
         c.period_from, c.period_to, c.period_label,
         'reportcycle'::text AS source, 'pf/extension'::text AS source_module, c.period_id AS source_period_id,
         c.form_code, c.form_version_id,
         c.form_closed, c.period_closed, c.key_closed, c.closed_at,
         c.run_id AS snapshot_id, c.run_no::bigint AS snapshot_no, c.run_at AS data_at, c.provisional,
         c.communes_total AS units_total, c.communes_with_data AS units_with_data, c.missing_communes AS missing_units,
         CASE WHEN rc.rows IS NOT NULL THEN (
           SELECT coalesce(jsonb_agg(e.v || jsonb_build_object('confirmation', 'confirmed') ORDER BY e.n), '[]'::jsonb)
             FROM jsonb_array_elements(rc.rows) WITH ORDINALITY e(v, n)) END AS rows,
         c.totals,
         'confirmed'::text AS confirmation_status,
         CASE WHEN c.run_id IS NOT NULL THEN encode(sha256(convert_to(jsonb_build_array(rc.rows, c.totals)::text, 'UTF8')), 'hex') END
           AS figures_hash,
         CASE WHEN rc.rows IS NOT NULL THEN encode(sha256(convert_to((
           SELECT coalesce(jsonb_agg(jsonb_build_array(e.v -> 'key', 'confirmed') ORDER BY e.n), '[]'::jsonb)
             FROM jsonb_array_elements(rc.rows) WITH ORDINALITY e(v, n))::text, 'UTF8')), 'hex') END AS status_hash
    FROM pf_extension_pub.v1_consolidation_closed c
    LEFT JOIN LATERAL (
      SELECT CASE WHEN c.rows IS NOT NULL THEN (
               SELECT coalesce(jsonb_agg(jsonb_build_object(
                        'key', r.e -> 'key',
                        'unit', r.e -> 'commune',
                        'unit_kind', 'commune',
                        'parent', 'null'::jsonb,
                        'region', coalesce(r.e -> 'region', 'null'::jsonb),
                        'indicator', coalesce(r.e -> 'indicator', 'null'::jsonb),
                        'nil_report', false,
                        'cells', coalesce(r.e -> 'cells', '{}'::jsonb)) ORDER BY r.n), '[]'::jsonb)
                 FROM jsonb_array_elements(c.rows) WITH ORDINALITY r(e, n)) END AS rows) rc ON true;

GRANT SELECT ON pf_extension_pub.v1_closed_figures TO r_extension_pub;
