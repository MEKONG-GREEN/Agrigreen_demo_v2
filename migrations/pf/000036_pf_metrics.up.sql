-- pf_metrics (G3-A5): tầng chỉ số CÓ LỊCH SỬ (tài liệu kiến trúc mục 16, Phụ lục A
-- cổng G3; KN US-DASH-01/02, CGH FN-09/10/11).
--
--   1. definition_pin  ghim băm tệp định nghĩa (internal/platform/metrics/defs/*.yaml)
--                      lần đầu một tenant dùng (key, version). Sửa tệp của phiên bản đã
--                      dùng → job từ chối (định nghĩa có phiên bản, không sửa tại chỗ).
--   2. daily_value     SỔ giá trị theo ngày nghiệp vụ × phạm vi × chiều. Giá trị của D
--                      chỉ dựng từ bản ghi nguồn có recorded_at < cutoff (đầu ngày D+1
--                      giờ VN). Tính lại (định nghĩa mới có hiệu lực lùi, sửa lỗi công
--                      thức) = dòng mới supersedes dòng trước kèm lý do; kỳ đã chốt
--                      không bao giờ bị UPDATE (RB-1).
--
-- Không có dữ liệu cá nhân (R-14): phạm vi là mã đơn vị hành chính; chiều là mã vụ.

CREATE SCHEMA pf_metrics;
REVOKE ALL ON SCHEMA pf_metrics FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_metrics TO metrics_app;

-- ---------------------------------------------------------------------------
-- 1. Ghim định nghĩa (chỉ ghi thêm, không chuỗi băm)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_metrics.definition_pin (
  tenant_id      uuid        NOT NULL,
  metric_key     text        NOT NULL CHECK (metric_key ~ '^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+$'),
  def_version    integer     NOT NULL CHECK (def_version >= 1),
  file_sha256    bytea       NOT NULL CHECK (length(file_sha256) = 32),
  unit           text        NOT NULL,
  grain          text        NOT NULL CHECK (grain IN ('day', 'season')),
  effective_from date        NOT NULL,
  proposed_by    text        NOT NULL,
  approved_by    text        NOT NULL,
  recorded_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, metric_key, def_version),
  CHECK (lower(btrim(proposed_by)) <> lower(btrim(approved_by)))  -- R-56
);
SELECT shared.apply_append_only('pf_metrics.definition_pin');

-- ---------------------------------------------------------------------------
-- 2. Sổ giá trị theo ngày (mẫu mục 8.1)
-- stream_key = 'value:<key>:<scope_unit>:<dimension>:<business_date>' — mỗi ô một chuỗi.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_metrics.daily_value (
  id                    uuid          NOT NULL,
  tenant_id             uuid          NOT NULL,
  stream_key            text          NOT NULL,
  seq                   bigint        NOT NULL,
  metric_key            text          NOT NULL,
  def_version           integer       NOT NULL CHECK (def_version >= 1),
  scope_unit            text          NOT NULL DEFAULT '',   -- '' = toàn tenant
  dimension             text          NOT NULL DEFAULT '',   -- vd 'DX-2026' (grain season)
  business_date         date          NOT NULL,
  value                 numeric(20,6) NOT NULL,              -- R-10: không float
  unit                  text          NOT NULL,
  cutoff                timestamptz   NOT NULL,              -- đầu ngày D+1 giờ VN
  watermark_event_id    uuid          NULL,
  watermark_recorded_at timestamptz   NULL,
  inputs_hash           bytea         NOT NULL CHECK (length(inputs_hash) = 32),
  event_time            timestamptz   NOT NULL,              -- = cutoff: giá trị "như hệ biết lúc hết ngày D"
  recorded_at           timestamptz   NOT NULL DEFAULT now(),
  supersedes_id         uuid          NULL,                  -- tính lại: dòng bị thay
  correction_reason     text          NULL,
  actor_ref             uuid          NOT NULL,
  device_id             text          NULL,
  idem_key              text          NOT NULL,              -- stream_key + ':' + hex(inputs_hash)
  rule_version          text          NOT NULL,
  prev_hash             bytea         NOT NULL,
  row_hash              bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(btrim(correction_reason)) >= 10),
  CHECK (event_time = cutoff),
  CHECK (recorded_at >= cutoff)                                -- chỉ ghi ngày đã kết thúc
) PARTITION BY RANGE (recorded_at);

CREATE INDEX daily_value_series ON pf_metrics.daily_value (tenant_id, metric_key, scope_unit, dimension, business_date, seq DESC);
CREATE INDEX daily_value_stream ON pf_metrics.daily_value (tenant_id, stream_key, seq);
CREATE INDEX daily_value_idem   ON pf_metrics.daily_value (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_metrics.daily_value', 'pf_metrics.daily_value');

-- ---------------------------------------------------------------------------
-- Quyền (R-05): chỉ SELECT, INSERT.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_metrics.definition_pin, pf_metrics.daily_value TO metrics_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_metrics.definition_pin, pf_metrics.daily_value FROM metrics_app;
