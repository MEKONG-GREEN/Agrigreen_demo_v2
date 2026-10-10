-- pf/price (`cap/price`, W11.74 — ADR-0049 mục 4, BRD Báo giá nông sản v1.3 FN-AD-04.04…06, FN-XA-03.04; đối chiếu
-- Phụ lục D BG4, Q-086): lịch báo giá + ngoại lệ, giờ khóa, hạn phản hồi xác minh, nhắc nhập giá, ngưỡng cảnh báo.
--
-- 1. Bốn sổ chỉ ghi thêm (mẫu 8.1, shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo tenant). Mỗi
--    dòng là MỘT phiên bản; cột profile là tên bên dùng của port.Profile (KN = 'kn'). Chưa có phiên bản → giá trị mặc
--    định của Profile (cmd/api-platform/assembly/price.go), không seed dữ liệu.
--      - calendar_rule       — lịch báo giá của bên dùng: ngày trong tuần (bit 1<<weekday, CN = bit 0), giờ khóa, hạn phản
--                              hồi, hai giờ nhắc, kênh, nội dung mẫu; hiệu lực từ valid_from (date giờ VN). Một chuỗi mỗi
--                              bên dùng ('calendar:<profile>'). CHECK: nhắc 1 < nhắc 2 < giờ khóa < hạn phản hồi
--                              (FN-AD-04.04 AC-05, FN-AD-04.05 AC-01).
--      - calendar_exception  — ngày nghỉ / ngày báo giá bổ sung, kèm lý do; xóa = phiên bản 'retired'. Chuỗi mỗi ngày
--                              ('exception:<profile>:<ngày>').
--      - alert_threshold     — ngưỡng ±% theo phạm vi default | group | item (BR-02 mặt hàng → nhóm → mặc định); phạm vi
--                              default mang tham số chung N (lookback_days), ngưỡng đi ngang, ngưỡng phiếu điều tra
--                              (CT-02, CT-04, CT-06). numeric(5,2), 0 < % ≤ 100. Ngưỡng mặc định không 'retired' được
--                              (AC-04). Áp dụng từ lần gửi kế tiếp (BR-03) → hiệu lực theo thứ tự ghi, không valid_from.
--      - calendar_run        — nhật ký job price_calendar (cmd/worker, mỗi phút ở leader): mốc khóa 12:00 thực tế (±1
--                              phút) và lần nhắc 1, 2 ("Lịch sử gửi nhắc", cột "Hệ thống đã nhắc"). Mỗi (bên dùng, ngày,
--                              loại) đúng một dòng: CHECK seq = 1 + idem_key 'price:run:<profile>:<ngày>:<loại>'.
-- 2. Không dữ liệu cá nhân (R-04): mã tham chiếu người ghi, nội dung mẫu do Cấp Tỉnh soạn. Không cột số thực (R-CODE-01).
-- 3. Vai: r_price SELECT, INSERT. ag_ops_jobs (BYPASSRLS) chỉ đọc cột tenant_id để job tìm tenant đã bật Báo giá
--    (đã có danh mục nền hoặc cấu hình lịch) — mẫu pf 000114.

CREATE TABLE pf_price.calendar_rule (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'calendar:<profile>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  profile           text        NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,29}$'),
  weekdays          smallint    NOT NULL CHECK (weekdays BETWEEN 1 AND 127),
  lock_at           time        NOT NULL,
  respond_by        time        NOT NULL,
  remind1_at        time        NOT NULL,
  remind2_at        time        NOT NULL,
  notify_in_app     boolean     NOT NULL,
  notify_email      boolean     NOT NULL,
  template          text        NOT NULL CHECK (char_length(template) BETWEEN 1 AND 500),
  valid_from        date        NOT NULL,                 -- ngày báo giá đầu tiên phiên bản có hiệu lực (giờ VN)
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (remind1_at < remind2_at AND remind2_at < lock_at AND lock_at < respond_by),
  CHECK (notify_in_app OR notify_email),
  CONSTRAINT calendar_rule_stream CHECK (stream_key = 'calendar:' || profile)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX calendar_rule_profile ON pf_price.calendar_rule (tenant_id, profile, valid_from, seq);
SELECT shared.apply_ledger('pf_price.calendar_rule', 'pf_price.calendar_rule');

CREATE TABLE pf_price.calendar_exception (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'exception:<profile>:<YYYY-MM-DD>'
  seq               bigint      NOT NULL,
  profile           text        NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,29}$'),
  day               date        NOT NULL,
  kind              text        NOT NULL CHECK (kind IN ('holiday', 'extra')),
  reason            text        NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 300),
  status            text        NOT NULL CHECK (status IN ('active', 'retired')),
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT calendar_exception_stream CHECK (stream_key = 'exception:' || profile || ':' || to_char(day, 'YYYY-MM-DD'))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX calendar_exception_day ON pf_price.calendar_exception (tenant_id, profile, day, seq);
SELECT shared.apply_ledger('pf_price.calendar_exception', 'pf_price.calendar_exception');

CREATE TABLE pf_price.alert_threshold (
  id                uuid         NOT NULL,
  tenant_id         uuid         NOT NULL,
  stream_key        text         NOT NULL,                -- 'threshold:<profile>:<scope>:<code>'
  seq               bigint       NOT NULL,
  profile           text         NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,29}$'),
  scope             text         NOT NULL CHECK (scope IN ('default', 'group', 'item')),
  scope_code        text         NOT NULL CHECK (scope_code = '' OR scope_code ~ '^[a-z0-9][a-z0-9_.-]{0,39}$'),
  pct               numeric(5,2) NOT NULL CHECK (pct > 0 AND pct <= 100),
  lookback_days     integer      NULL CHECK (lookback_days BETWEEN 1 AND 60),
  flat_pct          numeric(5,2) NULL CHECK (flat_pct > 0 AND flat_pct <= 100),
  survey_pct        numeric(5,2) NULL CHECK (survey_pct > 0 AND survey_pct <= 100),
  status            text         NOT NULL CHECK (status IN ('active', 'retired')),
  event_time        timestamptz  NOT NULL,
  recorded_at       timestamptz  NOT NULL DEFAULT now(),
  supersedes_id     uuid         NULL,
  correction_reason text         NULL,
  actor_ref         uuid         NOT NULL,
  device_id         text         NULL,
  idem_key          text         NOT NULL,
  rule_version      text         NOT NULL,
  prev_hash         bytea        NOT NULL,
  row_hash          bytea        NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  -- default: không mã, có đủ tham số chung, không ngưng; group/item: có mã, không tham số chung.
  CHECK ((scope = 'default') = (scope_code = '')),
  CHECK ((scope = 'default') = (lookback_days IS NOT NULL AND flat_pct IS NOT NULL AND survey_pct IS NOT NULL)),
  CHECK (scope = 'default' OR (lookback_days IS NULL AND flat_pct IS NULL AND survey_pct IS NULL)),
  CHECK (scope <> 'default' OR status = 'active'),
  CONSTRAINT alert_threshold_stream CHECK (stream_key = 'threshold:' || profile || ':' || scope || ':' || scope_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX alert_threshold_scope ON pf_price.alert_threshold (tenant_id, profile, scope, scope_code, seq);
SELECT shared.apply_ledger('pf_price.alert_threshold', 'pf_price.alert_threshold');

CREATE TABLE pf_price.calendar_run (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'run:<profile>:<YYYY-MM-DD>:<kind>'
  seq               bigint      NOT NULL CHECK (seq = 1), -- mỗi (bên dùng, ngày, loại) một lần
  profile           text        NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,29}$'),
  day               date        NOT NULL,
  kind              text        NOT NULL CHECK (kind IN ('lock', 'remind1', 'remind2')),
  scheduled_at      timestamptz NOT NULL,                 -- mốc theo cấu hình; event_time − scheduled_at = độ trễ
  recipients        integer     NOT NULL CHECK (recipients >= 0),
  targets           integer     NOT NULL CHECK (targets >= 0), -- số xã (0 = thông báo chủ đề toàn tenant)
  note              text        NULL CHECK (note IS NULL OR char_length(note) <= 300),
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT calendar_run_stream CHECK (stream_key = 'run:' || profile || ':' || to_char(day, 'YYYY-MM-DD') || ':' || kind)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX calendar_run_day ON pf_price.calendar_run (tenant_id, profile, day);
SELECT shared.apply_ledger('pf_price.calendar_run', 'pf_price.calendar_run');

REVOKE ALL ON pf_price.calendar_rule, pf_price.calendar_exception, pf_price.alert_threshold, pf_price.calendar_run FROM PUBLIC;

GRANT SELECT, INSERT ON pf_price.calendar_rule, pf_price.calendar_exception, pf_price.alert_threshold, pf_price.calendar_run TO r_price;

-- Job price_calendar tìm tenant (worker, ag_ops_jobs BYPASSRLS): chỉ cột tenant_id.
GRANT USAGE ON SCHEMA pf_price TO ag_ops_jobs;
GRANT SELECT (tenant_id) ON pf_price.unit, pf_price.calendar_rule TO ag_ops_jobs;
