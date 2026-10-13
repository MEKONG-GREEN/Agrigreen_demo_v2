-- pf_extension_pub.v1_closed_figures (họ closed_figures/v1 — ADR-0053): thêm nguồn BẢNG TỔNG HỢP CNTS Cấp Tỉnh (W11.44 —
-- BM-02 KSGM tuần KSGM theo cơ sở = tổng BM-01 + dòng điều chỉnh; BM-18 Phụ lục 2 tuần CNTS = khai thác BM-17 theo xã + nuôi
-- trồng theo đối tượng, kèm LŨY KẾ quý/năm của nguồn — D18). Theo ADR-0053 §3/§5: CREATE OR REPLACE VIEW, GIỮ NGUYÊN cột,
-- thứ tự, kiểu và ý nghĩa; nhánh Trồng trọt chép NGUYÊN VĂN từ pf 20261013150110 (không đổi); nhánh mới `UNION ALL`.
-- Bộ đọc KHTC (extension/closedfig, KHTCSources) đọc view này sẵn → không đổi mã bên đọc, không thêm hợp đồng/vai.
-- Chỉ đụng schema pf_extension_pub.
--
-- Nhánh CNTS: một dòng = một kỳ tổng hợp (tỉnh × biểu × khóa kỳ) ĐÃ CHỐT ÍT NHẤT MỘT LẦN (pf_extension.cnts_agg_close,
-- pf 20261013170237). Số = lần chốt mới nhất (đóng băng; app chỉ cho chốt khi MỌI kỳ nguồn của tuần đã chốt — D17).
-- Mở lại → cờ về false, số của lần chốt trước vẫn trả; chốt lại → số mới, snapshot_no tăng (như nguồn kỳ native).
--   source = 'cnts_aggregate', source_module = 'pf/extension', form_code = CNTS-BM02 (period_type ksgm_week) | CNTS-BM18 (week);
--   source_period_id = mã ổn định của kỳ tổng hợp (domain.AggPeriodID), form_version_id = phiên bản định nghĩa bảng;
--   key_closed = mọi bảng tổng hợp CNTS cùng (tỉnh, loại kỳ, khóa kỳ) đã chốt (phòng duy nhất của nhánh này);
--   rows: BM-02 mỗi cơ sở {trau_bo, heo, gia_cam, de, <loài>_dieu_chinh, so_ngay_bao_cao}; BM-18 mỗi xã {khai_thac_<nhóm>,
--   khai_thac_tong, tong_san_luong} + mỗi đối tượng nuôi trồng (unit_kind aquaculture_object) {nuoi_trong_*, tong_san_luong};
--   mọi ô BM-18 có bản _luy_ke_quy, _luy_ke_nam (KHTC dùng Last). Σ tong_san_luong của rows = tổng sản lượng thủy sản.
--   totals: dòng Khu vực/Toàn tỉnh do nguồn tính. figures_hash do app tính lúc chốt (sha256 [rows, totals]).
-- Lịch nguồn (D8): ";cal=chan_nuoi_thuy_san:v3" của period_rule (Thứ Ba – Thứ Hai); period_key = khóa gốc.
-- Không dữ liệu cá nhân (R-14): không actor_ref, không lý do mở lại, không sources.

CREATE OR REPLACE VIEW pf_extension_pub.v1_closed_figures AS
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
                 FROM jsonb_array_elements(c.rows) WITH ORDINALITY r(e, n)) END AS rows) rc ON true
  UNION ALL
  SELECT * FROM (
  WITH ev AS (
    SELECT c.*,
           row_number() OVER (PARTITION BY c.tenant_id, c.stream_key ORDER BY c.seq DESC) AS rn,
           count(*) FILTER (WHERE c.action = 'closed') OVER (PARTITION BY c.tenant_id, c.stream_key ORDER BY c.seq) AS close_no
      FROM pf_extension.cnts_agg_close c),
  last AS (SELECT tenant_id, stream_key, action FROM ev WHERE rn = 1),
  snap AS (
    SELECT DISTINCT ON (e.tenant_id, e.stream_key) e.*
      FROM ev e WHERE e.action = 'closed'
     ORDER BY e.tenant_id, e.stream_key, e.seq DESC)
  SELECT s.tenant_id, 'chan_nuoi_thuy_san'::text AS department_code, s.province_code,
         coalesce(substring(s.period_rule from ';cal=([a-z][a-z0-9_]{1,39}):v[0-9]{1,4}(;|$)'), 'chan_nuoi_thuy_san') AS calendar_id,
         CASE WHEN s.period_rule ~ ';cal=[a-z][a-z0-9_]{1,39}:v[0-9]{1,4}(;|$)'
                THEN substring(s.period_rule from ';cal=[a-z][a-z0-9_]{1,39}:v([0-9]{1,4})(;|$)')::integer
              WHEN s.period_rule LIKE 'kn.period_rule/v1%' THEN 1
              WHEN s.period_rule LIKE 'kn.period_rule/v2%' THEN 2 END AS calendar_version,
         s.period_type, s.period_key,
         CASE WHEN s.period_type = 'week' AND s.period_key ~ '^[0-9]{4}-W[0-9]{2}$' THEN left(s.period_key, 4)::integer END AS week_year,
         s.period_rule, s.period_from, s.period_to, s.period_label,
         'cnts_aggregate'::text AS source, 'pf/extension'::text AS source_module, s.agg_period_id AS source_period_id,
         s.form_code, s.def_version_id AS form_version_id,
         (l.action = 'closed') AS form_closed, (l.action = 'closed') AS period_closed,
         bool_and(l.action = 'closed') OVER (PARTITION BY s.tenant_id, s.province_code, s.period_type, s.period_key) AS key_closed,
         s.event_time AS closed_at,
         s.id AS snapshot_id, s.close_no AS snapshot_no, s.recorded_at AS data_at,
         s.provisional, s.units_total, s.units_with_data, s.missing_units,
         (SELECT coalesce(jsonb_agg(e.v || jsonb_build_object('confirmation', 'confirmed') ORDER BY e.n), '[]'::jsonb)
            FROM jsonb_array_elements(s.rows) WITH ORDINALITY e(v, n)) AS rows,
         s.totals,
         'confirmed'::text AS confirmation_status,
         s.figures_hash,
         encode(sha256(convert_to((
           SELECT coalesce(jsonb_agg(jsonb_build_array(e.v -> 'key', 'confirmed') ORDER BY e.n), '[]'::jsonb)
             FROM jsonb_array_elements(s.rows) WITH ORDINALITY e(v, n))::text, 'UTF8')), 'hex') AS status_hash
    FROM snap s
    JOIN last l ON l.tenant_id = s.tenant_id AND l.stream_key = s.stream_key
  ) cnts;
