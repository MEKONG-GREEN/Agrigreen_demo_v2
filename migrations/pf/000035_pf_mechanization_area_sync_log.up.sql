-- pf_mechanization G2: nhật ký đồng bộ diện tích vụ App HTX và cảnh báo độ tươi (CGH FN-07
-- B5, BR-04, AC-02, AC-03).
--
-- 1. area_sync_log: mỗi lần nhận hoặc nhập diện tích vụ một dòng — thời điểm, nguồn
--    (app_htx | manual), HTX × vụ, kết quả áp QT-03 (applied | kept_locked | stale_ignored),
--    diện tích, thời điểm dữ liệu ở nguồn, event_id của sự kiện đồng bộ. Lần xử lý THẤT BẠI
--    của consumer (rollback) nằm ở pf_sync.integration_failure (migration 000034). Chỉ ghi thêm.
-- 2. area_feed_alert: mỗi tenant tối đa một cảnh báo "dữ liệu App HTX quá ngưỡng độ tươi"
--    mỗi ngày nghiệp vụ (khóa chính chặn trùng khi job chạy lại). Cùng giao dịch phát
--    pf.mechanization.area_feed.stale → pf/notify báo Admin CGH.
-- Job kiểm độ tươi chạy ở worker (leader) cho MỌI tenant: vai ag_outbox_publisher chỉ đọc
-- hai cột của area_sync_log để tìm tenant; ghi bằng vai mechanization_app.

CREATE TABLE pf_mechanization.area_sync_log (
  tenant_id       uuid          NOT NULL,
  id              uuid          NOT NULL,                  -- UUIDv7
  cooperative_ref uuid          NOT NULL,
  season_code     text          NOT NULL,
  season_year     integer       NOT NULL CHECK (season_year BETWEEN 2000 AND 2100),
  source          text          NOT NULL CHECK (source IN ('app_htx', 'manual')),
  outcome         text          NOT NULL CHECK (outcome IN ('applied', 'kept_locked', 'stale_ignored')),
  area_ha         numeric(14,4) NOT NULL CHECK (area_ha >= 0),
  source_at       timestamptz   NOT NULL,                  -- thời điểm cập nhật ở nguồn
  event_id        uuid          NULL,                      -- sự kiện đồng bộ (NULL với nhập tay)
  actor_ref       uuid          NOT NULL,
  recorded_at     timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id)
);
CREATE UNIQUE INDEX area_sync_log_event ON pf_mechanization.area_sync_log (tenant_id, event_id) WHERE event_id IS NOT NULL;
CREATE INDEX area_sync_log_recent ON pf_mechanization.area_sync_log (tenant_id, recorded_at DESC);
CREATE INDEX area_sync_log_coop ON pf_mechanization.area_sync_log (tenant_id, cooperative_ref, season_code, season_year, recorded_at DESC);
SELECT shared.apply_tenant_rls('pf_mechanization.area_sync_log');
CREATE TRIGGER trg_no_update BEFORE UPDATE ON pf_mechanization.area_sync_log
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_mechanization.area_sync_log
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_mechanization.area_sync_log
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

CREATE TABLE pf_mechanization.area_feed_alert (
  tenant_id       uuid        NOT NULL,
  alert_date      date        NOT NULL,                    -- ngày nghiệp vụ (giờ Việt Nam)
  last_sync_at    timestamptz NOT NULL,
  threshold_hours numeric(14,4) NOT NULL CHECK (threshold_hours > 0),
  recorded_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, alert_date)
);
SELECT shared.apply_tenant_rls('pf_mechanization.area_feed_alert');
CREATE TRIGGER trg_no_update BEFORE UPDATE ON pf_mechanization.area_feed_alert
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_mechanization.area_feed_alert
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_mechanization.area_feed_alert
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

GRANT SELECT, INSERT ON pf_mechanization.area_sync_log, pf_mechanization.area_feed_alert TO mechanization_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_mechanization.area_sync_log, pf_mechanization.area_feed_alert FROM mechanization_app;

GRANT USAGE ON SCHEMA pf_mechanization TO ag_outbox_publisher;
GRANT SELECT (tenant_id, source) ON pf_mechanization.area_sync_log TO ag_outbox_publisher;
