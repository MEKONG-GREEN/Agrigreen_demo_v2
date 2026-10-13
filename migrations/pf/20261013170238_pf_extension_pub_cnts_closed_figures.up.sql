-- pf_extension_pub: số đã chốt của bảng tổng hợp CNTS (BM-02 tuần KSGM, BM-18 Phụ lục 2 tuần CNTS) cho KHTC — W11.44.
-- Chỉ đụng schema pf_extension_pub. Hợp đồng: contracts/pubview/pf_extension_pub.cnts_closed_figures.v1.json — CỘT VÀ THỨ
-- TỰ GIỐNG HỆT họ closed_figures/v1 (ADR-0053 §1: pf_forms_pub.v1_closed_figures, pf_extension_pub.v1_closed_figures của
-- gói PFPUB) để bộ đọc KHTC (extension/port.KHTCSources) thêm MỘT nhánh UNION ALL mà không đổi mã quy đổi (ADR-0053 §3
-- "thêm nguồn mới = thêm view cùng family").
--
-- Một dòng = một kỳ tổng hợp (tỉnh × biểu × khóa kỳ) ĐÃ CHỐT ÍT NHẤT MỘT LẦN (pf_extension.cnts_agg_close). Số liệu = lần
-- chốt mới nhất (đóng băng: rows, totals, băm do app tính — lũy kế quý/năm là lũy kế CỦA NGUỒN, D18). Mở lại → cờ về
-- false, số của lần chốt trước vẫn trả (như PFPUB); chốt lại → số mới, snapshot_no tăng. App chỉ cho chốt khi MỌI kỳ nguồn
-- của tuần (BM-01 các ngày / BM-17) đã chốt (D17).
--   form_closed = period_closed  sự kiện cuối của kỳ tổng hợp là closed;
--   key_closed  mọi bảng tổng hợp cùng (phòng, tỉnh, loại kỳ, khóa kỳ) TRONG VIEW NÀY đã chốt.
-- Lịch nguồn (D8): (calendar_id, calendar_version) đọc từ ";cal=<phòng>:v<n>" của period_rule (lịch CNTS Thứ Ba – Thứ Hai);
-- period_key = khóa gốc ("2026-W41", "2026-10-K2"). Không dữ liệu cá nhân (R-14): không actor_ref, không lý do mở lại.
-- Trạng thái xác nhận (D2): bảng tổng hợp CNTS không có dòng HTX → mọi dòng 'confirmed'.

CREATE VIEW pf_extension_pub.v1_cnts_closed_figures AS
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
    JOIN last l ON l.tenant_id = s.tenant_id AND l.stream_key = s.stream_key;

GRANT SELECT ON pf_extension_pub.v1_cnts_closed_figures TO r_extension_pub;
