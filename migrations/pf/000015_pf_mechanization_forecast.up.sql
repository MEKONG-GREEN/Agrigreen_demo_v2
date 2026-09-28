-- pf_mechanization: dự báo nhu cầu cơ giới hóa (CGH FN-09 "dự báo").
--
-- BRD không nêu phương pháp → phương pháp TẠM (rule_version cgh.forecast/v1, cần
-- DCRD xác nhận, docs/dev/cgh-forecast.md):
--   diện tích dự báo vụ S năm Y = diện tích vụ S năm Y−1 × (1 + g),
--   g = trung bình tăng trưởng cùng vụ của tối đa 3 năm gần nhất, chặn ±20 %;
--   số máy cần = diện tích dự báo ÷ định mức hiệu lực tại ngày bắt đầu vụ dự báo.
--
-- Mỗi lần chạy là một forecast_run (stream 'forecast:<vụ>-<năm>') và các dòng
-- forecast_result (stream 'frun:<run_id>') — SỔ BẤT BIẾN, kèm đầu vào đã dùng
-- (diện tích lịch sử, tỷ lệ tăng, định mức + phiên bản). Chạy lại = lần chạy mới.

CREATE TABLE pf_mechanization.forecast_run (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  season_code       text        NOT NULL,
  season_year       integer     NOT NULL CHECK (season_year BETWEEN 2000 AND 2100),
  reference_date    date        NOT NULL,             -- ngày bắt đầu vụ dự báo: định mức tra tại đây
  pilot_stages      text[]      NOT NULL,
  method            jsonb       NOT NULL,             -- tham số phương pháp: số vụ lịch sử tối đa, mức chặn
  result_count      integer     NOT NULL CHECK (result_count >= 0),
  missing_norms     text[]      NOT NULL DEFAULT '{}',
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX forecast_run_idem   ON pf_mechanization.forecast_run (tenant_id, idem_key);
CREATE INDEX forecast_run_stream ON pf_mechanization.forecast_run (tenant_id, stream_key, seq);
CREATE INDEX forecast_run_id     ON pf_mechanization.forecast_run (tenant_id, id);
SELECT shared.apply_ledger('pf_mechanization.forecast_run', 'pf_mechanization.forecast_run');

CREATE TABLE pf_mechanization.forecast_result (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,
  seq               bigint        NOT NULL,
  run_id            uuid          NOT NULL,
  season_code       text          NOT NULL,
  season_year       integer       NOT NULL,
  cooperative_ref   uuid          NOT NULL,
  commune_code      text          NULL,
  province_code     text          NULL,
  farm_stage        text          NOT NULL,
  machine_type      text          NOT NULL,
  base_area_ha      numeric(14,4) NULL,               -- cùng vụ năm trước; NULL = chưa có dữ liệu
  growth_pct        numeric(9,4)  NULL,               -- tăng trưởng đã áp (đã chặn), %
  forecast_area_ha  numeric(14,4) NULL,
  norm_key          text          NOT NULL,
  norm_value        numeric(14,4) NULL,
  norm_version      integer       NULL,
  required          numeric(14,4) NULL,
  required_whole    integer       NULL,
  capacity_now      integer       NOT NULL CHECK (capacity_now >= 0),  -- máy "Hoạt động" lúc chạy dự báo
  gap               integer       NULL,
  status            text          NOT NULL CHECK (status IN ('ok', 'no_data', 'no_norm')),
  inputs            jsonb         NOT NULL,           -- diện tích lịch sử theo năm, tỷ lệ tăng từng năm, cờ chặn
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,
  device_id         text          NULL,
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX forecast_result_idem ON pf_mechanization.forecast_result (tenant_id, idem_key);
CREATE INDEX forecast_result_run  ON pf_mechanization.forecast_result (tenant_id, run_id, seq);
SELECT shared.apply_ledger('pf_mechanization.forecast_result', 'pf_mechanization.forecast_result');

GRANT SELECT, INSERT ON pf_mechanization.forecast_run, pf_mechanization.forecast_result TO mechanization_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_mechanization.forecast_run, pf_mechanization.forecast_result FROM mechanization_app;
