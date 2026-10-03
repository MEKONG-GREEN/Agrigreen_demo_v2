-- pf_metering (G5, ADR-0023 + ADR-0024; g5-conventions §2): sổ đo lường sử dụng Public API, bộ đếm
-- hạn mức thẩm quyền, điểm kiểm soát xuất dữ liệu (DLP) và bảng kê theo kỳ tháng. Vai metering_app
-- (R-05); schema + USAGE đã có ở pf 000080. Không tham chiếu schema khác (R-07), không dữ liệu cá nhân.
--
--   usage_event              SỔ (mẫu §8.1): một dòng mỗi lời gọi Public API ĐÃ XÁC THỰC, tenant = đơn vị
--                            vận hành, stream 'client:<client_id>:<YYYY-MM>' (tháng giờ VN của event_time),
--                            idem_key = request_id. Đơn vị theo quy tắc ghim rule_version (metering.units.v1).
--   usage_counter_current    bảng chiếu bộ đếm (khách hàng × sản phẩm × cửa sổ ngày/tháng giờ VN); dòng
--                            product = '*' là tổng mọi sản phẩm — nguồn THẨM QUYỀN của hạn mức gói, cập nhật
--                            cùng giao dịch với sổ, khóa dòng. Dựng lại được từ sổ (R-22).
--   egress_event             SỔ điểm kiểm soát xuất: tenant = CHỦ DỮ LIỆU rời đi, stream
--                            'egress:<kênh>:<YYYY-MM-DD>', idem_key = '<kênh>:<source_id>:<counterparty_ref>'.
--   egress_counter_current   bảng chiếu khối lượng ngày (kênh × bên nhận × ngày giờ VN) của dòng được cho qua.
--   egress_anomaly           chỉ ghi thêm: một dòng mỗi (ngày × kênh × bên nhận) lần ĐẦU vượt ngưỡng bất thường.
--   statement_job            bảng vận hành (mẫu §12): yêu cầu lập bảng kê → worker.
--   statement_snapshot       chỉ ghi thêm: ảnh chụp bất biến (first/last seq, row_hash cuối chuỗi, SHA-256).
--   statement                SỔ bảng kê: 'drafted' → 'approved' (người duyệt ≠ người yêu cầu, R-56 CHECK);
--                            sai sót = bảng kê mới supersedes_id + correction_reason, không sửa dòng cũ.
--   statement_line           chỉ ghi thêm: dòng đơn vị (+ đơn giá numeric(18,4), thành tiền numeric(18,2)).
--   statement_current        bảng chiếu hiện hành của bảng kê.

-- ---------------------------------------------------------------------------
-- 1. Sổ sử dụng
-- ---------------------------------------------------------------------------
CREATE TABLE pf_metering.usage_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,                 -- đơn vị vận hành
  stream_key        text        NOT NULL,                 -- 'client:<client_id>:<YYYY-MM>'
  seq               bigint      NOT NULL,
  client_id         uuid        NOT NULL,
  client_tenant_id  uuid        NOT NULL,
  key_prefix        text        NOT NULL CHECK (length(key_prefix) <= 64),
  auth_method       text        NOT NULL CHECK (length(auth_method) BETWEEN 1 AND 64),
  call_kind         text        NOT NULL CHECK (call_kind IN ('read', 'export_request', 'job_status', 'file_download', 'meta')),
  product           text        NOT NULL CHECK (product = '' OR product ~ '^[a-z][a-z0-9_]{1,62}$'),
  contract_version  integer     NOT NULL CHECK (contract_version >= 0),
  scope             text        NOT NULL CHECK (length(scope) <= 200),
  route_template    text        NOT NULL CHECK (length(route_template) BETWEEN 1 AND 200 AND position('?' IN route_template) = 0),
  status            integer     NOT NULL CHECK (status BETWEEN 100 AND 599),
  problem_code      text        NULL CHECK (problem_code IS NULL OR length(problem_code) <= 100),
  data_tenant_id    uuid        NULL,
  units             bigint      NOT NULL CHECK (units >= 0),
  rows_out          bigint      NOT NULL CHECK (rows_out >= 0),
  bytes_out         bigint      NOT NULL CHECK (bytes_out >= 0),
  latency_ms        integer     NOT NULL CHECK (latency_ms >= 0),
  replayed          boolean     NOT NULL,
  job_id            uuid        NULL,
  trace_id          text        NULL,
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- = client_id (mẫu)
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- = request_id
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (NOT replayed OR units = 0),                       -- R-51: gọi lặp 0 đơn vị
  CHECK (status BETWEEN 200 AND 299 OR units = 0)          -- lỗi 0 đơn vị
) PARTITION BY RANGE (recorded_at);
CREATE INDEX usage_event_idem ON pf_metering.usage_event (tenant_id, idem_key);
CREATE INDEX usage_event_stream ON pf_metering.usage_event (tenant_id, stream_key, seq);
CREATE INDEX usage_event_client ON pf_metering.usage_event (tenant_id, client_id, id);
SELECT shared.apply_ledger('pf_metering.usage_event', 'pf_metering.usage_event');

CREATE TABLE pf_metering.usage_counter_current (
  tenant_id    uuid        NOT NULL,
  client_id    uuid        NOT NULL,
  product      text        NOT NULL,                      -- '*' = tổng mọi sản phẩm (hạn mức gói)
  window_kind  text        NOT NULL CHECK (window_kind IN ('day', 'month')),
  window_start date        NOT NULL,                      -- giờ VN; tháng = ngày 1
  units        bigint      NOT NULL CHECK (units >= 0),
  calls        bigint      NOT NULL CHECK (calls >= 0),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, client_id, product, window_kind, window_start),
  CHECK (window_kind <> 'month' OR extract(day FROM window_start) = 1)
);
SELECT shared.apply_tenant_rls('pf_metering.usage_counter_current');

-- ---------------------------------------------------------------------------
-- 2. Điểm kiểm soát xuất dữ liệu (tenant = chủ dữ liệu)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_metering.egress_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,                 -- chủ dữ liệu rời đi
  stream_key        text        NOT NULL,                 -- 'egress:<kênh>:<YYYY-MM-DD>'
  seq               bigint      NOT NULL,
  channel           text        NOT NULL CHECK (channel IN ('public_api', 'mrv_package', 'report_export', 'valve', 'partner_share')),
  source_id         uuid        NOT NULL,
  counterparty_kind text        NOT NULL CHECK (counterparty_kind IN ('api_client', 'tenant', 'actor')),
  counterparty_ref  uuid        NOT NULL,
  product           text        NOT NULL CHECK (length(product) <= 200),
  contract_version  integer     NOT NULL CHECK (contract_version >= 0),
  records           bigint      NOT NULL CHECK (records >= 0),
  bytes             bigint      NOT NULL CHECK (bytes >= 0),
  business_date     date        NOT NULL,
  decision          text        NOT NULL CHECK (decision IN ('allowed', 'denied')),
  reason            text        NOT NULL CHECK (reason IN ('', 'quota_exceeded')),
  mode              text        NOT NULL CHECK (mode IN ('observe', 'enforce')),
  daily_records     bigint      NOT NULL CHECK (daily_records >= 0),  -- khối lượng ngày sau lần này
  daily_quota       bigint      NOT NULL CHECK (daily_quota >= 0),    -- 0 = không giới hạn
  anomaly           boolean     NOT NULL,
  trace_id          text        NULL,
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người/khách hàng gây ra; van: bên nhận
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- '<kênh>:<source_id>:<counterparty_ref>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (decision = 'allowed' OR (mode = 'enforce' AND reason = 'quota_exceeded'))  -- observe không bao giờ từ chối
) PARTITION BY RANGE (recorded_at);
CREATE INDEX egress_event_idem ON pf_metering.egress_event (tenant_id, idem_key);
CREATE INDEX egress_event_list ON pf_metering.egress_event (tenant_id, id);
CREATE INDEX egress_event_channel ON pf_metering.egress_event (tenant_id, channel, business_date);
SELECT shared.apply_ledger('pf_metering.egress_event', 'pf_metering.egress_event');

CREATE TABLE pf_metering.egress_counter_current (
  tenant_id         uuid        NOT NULL,
  channel           text        NOT NULL,
  counterparty_kind text        NOT NULL,
  counterparty_ref  uuid        NOT NULL,
  business_date     date        NOT NULL,
  records           bigint      NOT NULL CHECK (records >= 0),
  bytes             bigint      NOT NULL CHECK (bytes >= 0),
  events            bigint      NOT NULL CHECK (events >= 0),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, channel, counterparty_kind, counterparty_ref, business_date)
);
SELECT shared.apply_tenant_rls('pf_metering.egress_counter_current');

CREATE TABLE pf_metering.egress_anomaly (
  id                uuid        NOT NULL PRIMARY KEY,
  tenant_id         uuid        NOT NULL,
  business_date     date        NOT NULL,
  channel           text        NOT NULL,
  counterparty_kind text        NOT NULL,
  counterparty_ref  uuid        NOT NULL,
  product           text        NOT NULL,
  egress_event_id   uuid        NOT NULL,
  daily_records     bigint      NOT NULL CHECK (daily_records > 0),
  threshold         bigint      NOT NULL CHECK (threshold >= 0),
  basis             text        NOT NULL CHECK (basis IN ('median', 'absolute')),
  mode              text        NOT NULL CHECK (mode IN ('observe', 'enforce')),
  detected_at       timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, business_date, channel, counterparty_kind, counterparty_ref)
);
CREATE INDEX egress_anomaly_list ON pf_metering.egress_anomaly (tenant_id, id);
SELECT shared.apply_append_only('pf_metering.egress_anomaly');

-- ---------------------------------------------------------------------------
-- 3. Bảng kê theo kỳ (mẫu §12; tenant = đơn vị vận hành)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_metering.statement_job (
  id                uuid        NOT NULL PRIMARY KEY,
  tenant_id         uuid        NOT NULL,
  period            text        NOT NULL CHECK (period ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
  client_id         uuid        NULL,                     -- NULL = mọi khách hàng có sử dụng trong kỳ
  supersedes_id     uuid        NULL,                     -- bảng kê bù
  correction_reason text        NULL CHECK (correction_reason IS NULL OR length(correction_reason) BETWEEN 10 AND 500),
  requested_by      uuid        NOT NULL,
  requested_at      timestamptz NOT NULL,
  idem_key          text        NOT NULL,
  status            text        NOT NULL CHECK (status IN ('queued', 'completed', 'failed')),
  statement_ids     uuid[]      NOT NULL DEFAULT '{}',
  error             text        NULL,
  completed_at      timestamptz NULL,
  UNIQUE (tenant_id, idem_key),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (supersedes_id IS NULL OR client_id IS NOT NULL)
);
SELECT shared.apply_tenant_rls('pf_metering.statement_job');

CREATE TABLE pf_metering.statement_snapshot (
  statement_id     uuid        NOT NULL PRIMARY KEY,
  tenant_id        uuid        NOT NULL,
  job_id           uuid        NOT NULL,
  client_id        uuid        NOT NULL,
  period           text        NOT NULL,
  stream_key       text        NOT NULL,
  first_seq        bigint      NOT NULL CHECK (first_seq >= 1),
  last_seq         bigint      NOT NULL CHECK (last_seq >= first_seq),
  chain_head_hash  text        NOT NULL CHECK (chain_head_hash ~ '^[0-9a-f]{64}$'),
  body             bytea       NOT NULL,                  -- JSON chuẩn tắc của ảnh chụp
  snapshot_sha256  text        NOT NULL CHECK (snapshot_sha256 ~ '^[0-9a-f]{64}$'),
  created_at       timestamptz NOT NULL DEFAULT now()
);
SELECT shared.apply_append_only('pf_metering.statement_snapshot');

CREATE TABLE pf_metering.statement (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'statement:<client_id>:<YYYY-MM>'
  seq               bigint      NOT NULL,
  statement_id      uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('drafted', 'approved')),
  client_id         uuid        NOT NULL,
  client_tenant_id  uuid        NOT NULL,
  period            text        NOT NULL CHECK (period ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
  job_id            uuid        NOT NULL,
  snapshot_sha256   text        NOT NULL CHECK (snapshot_sha256 ~ '^[0-9a-f]{64}$'),
  first_seq         bigint      NOT NULL,
  last_seq          bigint      NOT NULL,
  chain_head_hash   text        NOT NULL,
  requested_by      uuid        NOT NULL,
  subscription_vnd  numeric(18,2) NULL,
  total_vnd         numeric(18,2) NULL,
  price_version     text        NULL CHECK (price_version IS NULL OR length(price_version) <= 2000),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,                     -- statement_id của bảng kê bị thay
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- drafted: người yêu cầu; approved: người duyệt
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- '<action>:<statement_id>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (action <> 'approved' OR actor_ref <> requested_by),   -- R-56: không tự duyệt
  CHECK (action <> 'approved' OR supersedes_id IS NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX statement_by_id ON pf_metering.statement (tenant_id, statement_id, seq);
SELECT shared.apply_ledger('pf_metering.statement', 'pf_metering.statement');

CREATE TABLE pf_metering.statement_line (
  tenant_id      uuid          NOT NULL,
  statement_id   uuid          NOT NULL,
  product        text          NOT NULL,
  status_class   text          NOT NULL CHECK (status_class ~ '^[1-5]xx$'),
  calls          bigint        NOT NULL CHECK (calls >= 0),
  units          bigint        NOT NULL CHECK (units >= 0),
  unit_price_vnd numeric(18,4) NULL,
  amount_vnd     numeric(18,2) NULL,
  price_version  text          NULL,
  PRIMARY KEY (tenant_id, statement_id, product, status_class),
  CHECK ((unit_price_vnd IS NULL) = (amount_vnd IS NULL))
);
SELECT shared.apply_append_only('pf_metering.statement_line');

CREATE TABLE pf_metering.statement_current (
  tenant_id         uuid          NOT NULL,
  statement_id      uuid          NOT NULL,
  client_id         uuid          NOT NULL,
  client_tenant_id  uuid          NOT NULL,
  period            text          NOT NULL,
  status            text          NOT NULL CHECK (status IN ('draft', 'approved')),
  job_id            uuid          NOT NULL,
  snapshot_sha256   text          NOT NULL,
  first_seq         bigint        NOT NULL,
  last_seq          bigint        NOT NULL,
  chain_head_hash   text          NOT NULL,
  rule_version      text          NOT NULL,
  subscription_vnd  numeric(18,2) NULL,
  total_vnd         numeric(18,2) NULL,
  price_version     text          NULL,
  requested_by      uuid          NOT NULL,
  requested_at      timestamptz   NOT NULL,
  approved_by       uuid          NULL,
  approved_at       timestamptz   NULL,
  supersedes_id     uuid          NULL,
  correction_reason text          NULL,
  superseded_by     uuid          NULL,
  updated_at        timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, statement_id),
  CHECK (approved_by IS NULL OR approved_by <> requested_by),  -- R-56
  CHECK ((status = 'approved') = (approved_by IS NOT NULL))
);
CREATE INDEX statement_current_period ON pf_metering.statement_current (tenant_id, period, client_id);
SELECT shared.apply_tenant_rls('pf_metering.statement_current');

-- ---------------------------------------------------------------------------
-- 4. Quyền của metering_app (R-05). Sổ và bảng chỉ ghi thêm: SELECT, INSERT.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_metering.usage_event, pf_metering.egress_event, pf_metering.statement,
  pf_metering.egress_anomaly, pf_metering.statement_snapshot, pf_metering.statement_line TO metering_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_metering.usage_event, pf_metering.egress_event, pf_metering.statement,
  pf_metering.egress_anomaly, pf_metering.statement_snapshot, pf_metering.statement_line FROM metering_app;
-- Bảng chiếu (DELETE chỉ cho RebuildTenant — R-22) và bảng vận hành job.
GRANT SELECT, INSERT, UPDATE, DELETE ON pf_metering.usage_counter_current, pf_metering.egress_counter_current,
  pf_metering.statement_current TO metering_app;
GRANT SELECT, INSERT, UPDATE ON pf_metering.statement_job TO metering_app;
