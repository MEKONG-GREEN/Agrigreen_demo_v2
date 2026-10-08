-- View công bố `_pub` phiên bản 1 (W6.3, R-DB-10, ADR-0031 §2, kiến trúc v4.1 §13 R-DB-01, §14, §22.1).
--
-- `cap/analytics` (= pf/metrics, aka trong module.yaml) chỉ đọc view trong schema pf_<module>_pub, khai ở
-- provides.publications của module chủ (chỉ thị //ag:pubview trong port/ của module đó). View là HỢP ĐỒNG có
-- phiên bản như port: tên view mang tiền tố phiên bản (v1_<tên>); đổi bảng nội bộ phải giữ nguyên cột/ý nghĩa
-- của view đã công bố, đổi hợp đồng = view v2_<tên> mới (view cũ giữ tới khi không còn bên đọc).
--
-- Chỉ VIEW, không chép dữ liệu. Chủ view là ag_migrator (chủ bảng gốc, NOBYPASSRLS): mọi bảng gốc có FORCE ROW
-- LEVEL SECURITY (shared.apply_tenant_rls) nên policy p_tenant áp qua view như đọc thẳng — dashboard Cấp Tỉnh
-- chỉ thấy tenant của mình (§22.1). Không dữ liệu cá nhân (R-14): chỉ mã đơn vị, mã tham chiếu, số liệu, mốc
-- thời gian; actor_ref của chỉ thị KHÔNG công bố (v1_directive_target_ack chỉ có mốc xác nhận).
--
-- Quyền (§14, ADR-0031 §2): vai r_<module>_pub (NOLOGIN, do `cmd/migrate bootstrap` tạo — ag_migrator không có
-- CREATEROLE) có USAGE trên schema _pub và SELECT trên view; r_metrics là thành viên (bootstrap cấp). Không vai
-- nào có quyền trên schema _pub ngoài r_<module>_pub. r_metrics KHÔNG có quyền trên schema nội bộ của module
-- khác — các REVOKE cuối tệp chốt điều đó (không có gì để thu hồi ở bản hiện tại: pf/metrics trước W6.3 đọc qua
-- port.Source chạy bằng vai của module chủ).
--
-- Tên tệp <cụm>_pub_*: migration công bố được tạo view trong schema _pub của nhiều module (agmodgraph, quy tắc
-- schema) nhưng không đụng schema nội bộ nào.

DO $$
DECLARE
  r text;
BEGIN
  FOREACH r IN ARRAY ARRAY['r_cropcycle_pub', 'r_extension_pub', 'r_geo_pub', 'r_mechanization_pub', 'r_metrics'] LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r AND NOT rolcanlogin) THEN
      RAISE EXCEPTION 'thiếu vai nhóm % (NOLOGIN) — chạy cmd/migrate bootstrap bản W6.3 trước migrate up (ADR-0031)', r;
    END IF;
  END LOOP;
END
$$;

-- ---------------------------------------------------------------------------
-- pf/cropcycle: sổ vụ thửa và khai sản lượng (cc.season_area_ha, cc.yield_t_per_ha)
-- ---------------------------------------------------------------------------
CREATE SCHEMA pf_cropcycle_pub;
REVOKE ALL ON SCHEMA pf_cropcycle_pub FROM PUBLIC;

CREATE VIEW pf_cropcycle_pub.v1_season_event AS
  SELECT tenant_id, id, stream_key, seq, recorded_at, action, area_ha, commune_code, season_code, season_year
    FROM pf_cropcycle.season_event;

CREATE VIEW pf_cropcycle_pub.v1_harvest_declaration AS
  SELECT tenant_id, id, stream_key, seq, recorded_at, mass_kg, area_ha, commune_code, season_code, season_year
    FROM pf_cropcycle.harvest_declaration;

GRANT USAGE ON SCHEMA pf_cropcycle_pub TO r_cropcycle_pub;
GRANT SELECT ON pf_cropcycle_pub.v1_season_event, pf_cropcycle_pub.v1_harvest_declaration TO r_cropcycle_pub;

-- ---------------------------------------------------------------------------
-- pf/extension: báo cáo đồng ruộng, nhiệm vụ, chỉ thị (kn.*)
-- ---------------------------------------------------------------------------
CREATE SCHEMA pf_extension_pub;
REVOKE ALL ON SCHEMA pf_extension_pub FROM PUBLIC;

CREATE VIEW pf_extension_pub.v1_field_report AS
  SELECT tenant_id, id, stream_key, seq, recorded_at, report_kind, period, sown_area_ha, cooperative_ref,
         province_code, commune_code
    FROM pf_extension.field_report;

CREATE VIEW pf_extension_pub.v1_task_event AS
  SELECT tenant_id, id, task_id, seq, recorded_at, status_after
    FROM pf_extension.task_event;

CREATE VIEW pf_extension_pub.v1_task AS
  SELECT tenant_id, task_id, province_code, commune_code, created_at
    FROM pf_extension.task_current;

-- Mỗi dòng: một đơn vị nhận của một chỉ thị (khóa (tenant_id, directive_id, actor_ref) duy nhất ở bảng gốc, nên
-- LEFT JOIN xác nhận không nhân dòng). acked_at NULL = chưa xác nhận.
CREATE VIEW pf_extension_pub.v1_directive_target_ack AS
  SELECT dt.tenant_id, dt.directive_id, d.recorded_at AS directive_recorded_at, d.issued_at,
         dt.province_code, dt.unit_code, dt.issued_at AS target_issued_at, a.recorded_at AS acked_at
    FROM pf_extension.directive_target dt
    JOIN pf_extension.directive d ON d.tenant_id = dt.tenant_id AND d.directive_id = dt.directive_id
    LEFT JOIN pf_extension.directive_ack a ON a.tenant_id = dt.tenant_id AND a.directive_id = dt.directive_id
         AND a.actor_ref = dt.actor_ref;

GRANT USAGE ON SCHEMA pf_extension_pub TO r_extension_pub;
GRANT SELECT ON pf_extension_pub.v1_field_report, pf_extension_pub.v1_task_event, pf_extension_pub.v1_task,
  pf_extension_pub.v1_directive_target_ack TO r_extension_pub;

-- ---------------------------------------------------------------------------
-- pf/geo: sổ thửa, ranh giới (geo.plots_digitised) và hình học as-of cho phát lại lớp GIS
-- ---------------------------------------------------------------------------
CREATE SCHEMA pf_geo_pub;
REVOKE ALL ON SCHEMA pf_geo_pub FROM PUBLIC;

CREATE VIEW pf_geo_pub.v1_plot_event AS
  SELECT tenant_id, id, plot_ref, seq, recorded_at, status, commune_code
    FROM pf_geo.plot_event;

CREATE VIEW pf_geo_pub.v1_plot_boundary AS
  SELECT tenant_id, id, plot_ref, recorded_at, became_current
    FROM pf_geo.plot_boundary;

-- Hình học GeoJSON (7 chữ số thập phân) theo id dòng sổ bất biến; kind = loại dòng nguồn (GeometryRef.Kind).
CREATE VIEW pf_geo_pub.v1_layer_geometry AS
  SELECT tenant_id, 'plot_boundary'::text AS kind, id, ST_AsGeoJSON(geom, 7) AS geojson
    FROM pf_geo.plot_boundary WHERE geom IS NOT NULL
  UNION ALL
  SELECT tenant_id, 'cooperative_boundary'::text, id, ST_AsGeoJSON(geom, 7)
    FROM pf_geo.cooperative_boundary WHERE geom IS NOT NULL
  UNION ALL
  SELECT tenant_id, 'facility_event'::text, id, ST_AsGeoJSON(geom, 7)
    FROM pf_geo.facility_event WHERE geom IS NOT NULL;

GRANT USAGE ON SCHEMA pf_geo_pub TO r_geo_pub;
GRANT SELECT ON pf_geo_pub.v1_plot_event, pf_geo_pub.v1_plot_boundary, pf_geo_pub.v1_layer_geometry TO r_geo_pub;

-- ---------------------------------------------------------------------------
-- pf/mechanization: lần cân đối và mức đáp ứng tổng thể của mỗi HTX (cgh.*)
-- ---------------------------------------------------------------------------
CREATE SCHEMA pf_mechanization_pub;
REVOKE ALL ON SCHEMA pf_mechanization_pub FROM PUBLIC;

CREATE VIEW pf_mechanization_pub.v1_balance_run AS
  SELECT tenant_id, id, seq, recorded_at, season_code, season_year
    FROM pf_mechanization.balance_run;

-- Mức đáp ứng TỔNG THỂ của mỗi HTX trong một lần cân đối — quy tắc của module chủ (domain.Summarize, QT-02):
-- theo khâu: nhu cầu = tổng required (bỏ dòng NULL), năng lực = tổng capacity của dòng có required, mức =
-- năng lực × 100 ÷ nhu cầu làm tròn 4 chữ số, bão hòa 99999.9999; tổng thể = khâu THẤP NHẤT ("nút thắt");
-- không khâu nào có mức → no_norm nếu có khâu thiếu định mức, không thì no_data; phân loại theo ngưỡng đã lưu
-- của lần cân đối (thiếu một mốc → unclassified; mốc đúng bằng ngưỡng thuộc mức cao hơn). Kiểm thử đối chứng:
-- internal/platform/mechanization/handler/metrics_integration_test.go (so với domain.Summarize).
CREATE VIEW pf_mechanization_pub.v1_coop_coverage AS
  WITH stage AS (
    SELECT x.tenant_id, x.run_id, x.cooperative_ref, x.province_code, x.commune_code, x.farm_stage,
           CASE WHEN sum(x.required) > 0 THEN
             least(round((coalesce(sum(x.capacity) FILTER (WHERE x.required IS NOT NULL), 0) * 100)::numeric
                         / sum(x.required), 4), 99999.9999)
           END AS pct,
           bool_or(x.status = 'no_norm') AS no_norm
      FROM pf_mechanization.balance_result x
     GROUP BY x.tenant_id, x.run_id, x.cooperative_ref, x.province_code, x.commune_code, x.farm_stage),
  coop AS (
    SELECT tenant_id, run_id, cooperative_ref, province_code, commune_code,
           min(pct) AS worst, bool_or(pct IS NULL AND no_norm) AS no_norm
      FROM stage
     GROUP BY tenant_id, run_id, cooperative_ref, province_code, commune_code)
  SELECT c.tenant_id, c.run_id, c.cooperative_ref, c.province_code, c.commune_code, c.worst AS coverage_pct,
         CASE
           WHEN c.worst IS NULL THEN CASE WHEN c.no_norm THEN 'no_norm' ELSE 'no_data' END
           WHEN t.surplus IS NULL OR t.sufficient IS NULL OR t.attention IS NULL THEN 'unclassified'
           WHEN c.worst >= t.surplus THEN 'surplus'
           WHEN c.worst >= t.sufficient THEN 'sufficient'
           WHEN c.worst >= t.attention THEN 'attention'
           ELSE 'shortage'
         END AS status
    FROM coop c
    JOIN pf_mechanization.balance_run r ON r.tenant_id = c.tenant_id AND r.id = c.run_id
    CROSS JOIN LATERAL (
      SELECT (r.thresholds -> 'surplus' ->> 'value')::numeric    AS surplus,
             (r.thresholds -> 'sufficient' ->> 'value')::numeric AS sufficient,
             (r.thresholds -> 'attention' ->> 'value')::numeric  AS attention) t;

GRANT USAGE ON SCHEMA pf_mechanization_pub TO r_mechanization_pub;
GRANT SELECT ON pf_mechanization_pub.v1_balance_run, pf_mechanization_pub.v1_coop_coverage TO r_mechanization_pub;

-- ---------------------------------------------------------------------------
-- pf/metrics (cap/analytics) chỉ đọc _pub: chốt không có quyền đọc thẳng schema nội bộ của bốn module nguồn.
-- ---------------------------------------------------------------------------
REVOKE ALL ON ALL TABLES IN SCHEMA pf_cropcycle, pf_extension, pf_geo, pf_mechanization FROM r_metrics;
REVOKE ALL ON SCHEMA pf_cropcycle, pf_extension, pf_geo, pf_mechanization FROM r_metrics;
