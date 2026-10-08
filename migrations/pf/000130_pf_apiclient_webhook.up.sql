-- pf_apiclient W5.7 (R-INT-07, R-INT-09, ADR-0033 §4): WEBHOOK MỎNG + NGẮT MẠCH của cổng tích hợp.
--
-- Webhook chỉ là CHỈ BÁO cursor: thân {feed, cursor, occurred_at} ký HMAC-SHA256 (dấu thời gian + thân), không bao giờ
-- có dữ liệu nghiệp vụ — đối tác đọc tiếp bằng GET /v1/feeds/{feed}?cursor= (giao lại không mất dữ liệu).
--
--   webhook_event     SỔ vòng đời đăng ký (registered → secret_rotated* → disabled ↔ enabled; breaker_reset):
--                     URL, feed đăng ký, ngưỡng ngắt N, phiên bản bí mật + khóa chủ + nonce. Bí mật KHÔNG lưu: dẫn
--                     xuất HMAC-SHA256(AG_WEBHOOK_KEY_V<k>, 'agrigreen.webhook.v1:<webhook_id>:<phiên bản>:<nonce>')
--                     — nonce một mình không đủ để ký (khóa chủ chỉ ở trình quản lý bí mật).
--   webhook_current   bảng chiếu (dựng lại được từ sổ): trạng thái, bí mật hiện hành + bí mật cũ còn hiệu lực tới
--                     prev_valid_until (xoay có chồng lấn — ký cả hai trong khoảng đó).
--   webhook_breaker   trạng thái ngắt mạch mỗi webhook (closed | open | half_open), số lỗi liên tiếp, mốc thử lại.
--   webhook_cursor    mỗi webhook × feed: seq đã báo thành công, số lần thử của chỉ báo đang chờ, mốc thử kế tiếp
--                     (lùi theo hàm mũ có nhiễu).
--   webhook_delivery  SỔ từng lần giao (delivered | failed | blocked): mã HTTP, lớp lỗi, độ trễ, trạng thái ngắt sau
--                     lần giao — KHÔNG có thân phản hồi, không URL đầy đủ (chỉ host ở bảng chiếu).
--
-- Mọi bản ghi thuộc tenant ĐƠN VỊ VẬN HÀNH (như feed_head, client_current): Admin API (api-platform) ghi sổ đăng ký,
-- job apiclient_webhook_dispatch (cmd/worker, leader) giao. Không dữ liệu cá nhân (R-04). Chỉ vai nhóm r_apiclient.

SET LOCAL lock_timeout = '5s';

-- ---------------------------------------------------------------------------
-- 1. Sổ đăng ký
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.webhook_event (
  id                 uuid        NOT NULL,
  tenant_id          uuid        NOT NULL,                 -- đơn vị vận hành
  stream_key         text        NOT NULL,                 -- 'webhook:<webhook_id>'
  seq                bigint      NOT NULL,
  webhook_id         uuid        NOT NULL,
  client_id          uuid        NOT NULL,
  action             text        NOT NULL CHECK (action IN ('registered', 'secret_rotated', 'disabled', 'enabled', 'breaker_reset')),
  url                text        NULL CHECK (url IS NULL OR (url ~ '^https://' AND length(url) <= 2048)),
  feeds              text[]      NULL CHECK (feeds IS NULL OR cardinality(feeds) BETWEEN 1 AND 20),
  failure_threshold  integer     NULL CHECK (failure_threshold IS NULL OR failure_threshold BETWEEN 1 AND 100),
  secret_version     integer     NULL CHECK (secret_version IS NULL OR secret_version >= 1),
  key_version        integer     NULL CHECK (key_version IS NULL OR key_version >= 1),
  secret_nonce       text        NULL CHECK (secret_nonce IS NULL OR secret_nonce ~ '^[0-9a-f]{32}$'),
  prev_valid_until   timestamptz NULL,
  reason             text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  event_time         timestamptz NOT NULL,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  supersedes_id      uuid        NULL,
  correction_reason  text        NULL,
  actor_ref          uuid        NOT NULL,
  device_id          text        NULL,
  idem_key           text        NOT NULL,
  rule_version       text        NOT NULL,
  prev_hash          bytea       NOT NULL,
  row_hash           bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (action <> 'registered' OR (url IS NOT NULL AND feeds IS NOT NULL AND failure_threshold IS NOT NULL)),
  CHECK (action NOT IN ('registered', 'secret_rotated') OR (secret_version IS NOT NULL AND key_version IS NOT NULL AND secret_nonce IS NOT NULL)),
  CHECK (action NOT IN ('disabled', 'breaker_reset') OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX webhook_event_webhook ON pf_apiclient.webhook_event (tenant_id, webhook_id, seq);
SELECT shared.apply_ledger('pf_apiclient.webhook_event', 'pf_apiclient.webhook_event');

-- ---------------------------------------------------------------------------
-- 2. Bảng chiếu đăng ký
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.webhook_current (
  tenant_id          uuid        NOT NULL,
  webhook_id         uuid        NOT NULL,
  client_id          uuid        NOT NULL,
  url                text        NOT NULL CHECK (url ~ '^https://' AND length(url) <= 2048),
  host               text        NOT NULL CHECK (length(host) BETWEEN 1 AND 253),
  feeds              text[]      NOT NULL CHECK (cardinality(feeds) BETWEEN 1 AND 20),
  status             text        NOT NULL CHECK (status IN ('active', 'disabled')),
  failure_threshold  integer     NOT NULL CHECK (failure_threshold BETWEEN 1 AND 100),
  secret_version     integer     NOT NULL CHECK (secret_version >= 1),
  key_version        integer     NOT NULL CHECK (key_version >= 1),
  secret_nonce       text        NOT NULL CHECK (secret_nonce ~ '^[0-9a-f]{32}$'),
  prev_secret_version integer    NULL,
  prev_key_version   integer     NULL,
  prev_secret_nonce  text        NULL CHECK (prev_secret_nonce IS NULL OR prev_secret_nonce ~ '^[0-9a-f]{32}$'),
  prev_valid_until   timestamptz NULL,
  status_reason      text        NULL,
  created_by         uuid        NOT NULL,
  created_at         timestamptz NOT NULL,
  last_event_id      uuid        NOT NULL,
  last_seq           bigint      NOT NULL,
  updated_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, webhook_id),
  CHECK ((prev_secret_version IS NULL) = (prev_secret_nonce IS NULL) AND (prev_secret_nonce IS NULL) = (prev_valid_until IS NULL)
    AND (prev_secret_nonce IS NULL) = (prev_key_version IS NULL))
);
CREATE INDEX webhook_current_client ON pf_apiclient.webhook_current (tenant_id, client_id);
SELECT shared.apply_tenant_rls('pf_apiclient.webhook_current');

-- ---------------------------------------------------------------------------
-- 3. Ngắt mạch
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.webhook_breaker (
  tenant_id            uuid        NOT NULL,
  webhook_id           uuid        NOT NULL,
  state                text        NOT NULL CHECK (state IN ('closed', 'open', 'half_open')),
  consecutive_failures integer     NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
  opened_count         integer     NOT NULL DEFAULT 0 CHECK (opened_count >= 0), -- số lần mở liên tiếp (lùi thời gian thử)
  opened_at            timestamptz NULL,
  next_probe_at        timestamptz NULL,
  last_success_at      timestamptz NULL,
  last_failure_at      timestamptz NULL,
  last_error           text        NULL CHECK (last_error IS NULL OR last_error ~ '^[a-z][a-z0-9_]{1,40}$'),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, webhook_id),
  FOREIGN KEY (tenant_id, webhook_id) REFERENCES pf_apiclient.webhook_current (tenant_id, webhook_id),
  CHECK (state = 'closed' OR (opened_at IS NOT NULL AND next_probe_at IS NOT NULL))
);
SELECT shared.apply_tenant_rls('pf_apiclient.webhook_breaker');

-- ---------------------------------------------------------------------------
-- 4. Cursor đã báo mỗi webhook × feed
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.webhook_cursor (
  tenant_id          uuid        NOT NULL,
  webhook_id         uuid        NOT NULL,
  feed               text        NOT NULL CHECK (feed ~ '^[a-z][a-z0-9_]{2,47}$'),
  notified_seq       bigint      NOT NULL DEFAULT 0 CHECK (notified_seq >= 0),
  attempts           integer     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_attempt_at    timestamptz NULL,
  last_attempt_at    timestamptz NULL,
  updated_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, webhook_id, feed),
  FOREIGN KEY (tenant_id, webhook_id) REFERENCES pf_apiclient.webhook_current (tenant_id, webhook_id)
);
SELECT shared.apply_tenant_rls('pf_apiclient.webhook_cursor');

-- ---------------------------------------------------------------------------
-- 5. Sổ giao
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.webhook_delivery (
  id                 uuid        NOT NULL,                 -- = AG-Webhook-Id gửi đối tác (UUIDv7)
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,                 -- 'webhook:<webhook_id>'
  seq                bigint      NOT NULL,
  webhook_id         uuid        NOT NULL,
  client_id          uuid        NOT NULL,
  feed               text        NOT NULL CHECK (feed ~ '^[a-z][a-z0-9_]{2,47}$'),
  cursor_seq         bigint      NOT NULL CHECK (cursor_seq >= 1),
  attempt            integer     NOT NULL CHECK (attempt >= 1),
  probe              boolean     NOT NULL,                 -- lần thử half-open
  outcome            text        NOT NULL CHECK (outcome IN ('delivered', 'failed', 'blocked')),
  http_status        integer     NULL CHECK (http_status IS NULL OR http_status BETWEEN 100 AND 599),
  error_class        text        NULL CHECK (error_class IS NULL OR error_class ~ '^[a-z][a-z0-9_]{1,40}$'),
  latency_ms         integer     NOT NULL CHECK (latency_ms >= 0),
  breaker_state      text        NOT NULL CHECK (breaker_state IN ('closed', 'open', 'half_open')),
  event_time         timestamptz NOT NULL,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  supersedes_id      uuid        NULL,
  correction_reason  text        NULL,
  actor_ref          uuid        NOT NULL,                 -- = client_id (giao thay mặt hệ thống cho khách hàng)
  device_id          text        NULL,
  idem_key           text        NOT NULL,                 -- 'delivery:<id>'
  rule_version       text        NOT NULL,
  prev_hash          bytea       NOT NULL,
  row_hash           bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK ((outcome = 'delivered') = (error_class IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX webhook_delivery_webhook ON pf_apiclient.webhook_delivery (tenant_id, webhook_id, event_time);
SELECT shared.apply_ledger('pf_apiclient.webhook_delivery', 'pf_apiclient.webhook_delivery');

-- ---------------------------------------------------------------------------
-- 6. Đối soát theo ngày của luồng song phương (R-INT-10): GET /v1/feeds/{hợp đồng}/checksums quét sổ đệm
--    ingest_staging theo (tenant đối tác, hợp đồng, ngày nhận) — tạm tới W5.6.
-- ---------------------------------------------------------------------------
CREATE INDEX ingest_staging_day ON pf_apiclient.ingest_staging (tenant_id, contract, event_time);

-- ---------------------------------------------------------------------------
-- 7. Quyền: chỉ vai nhóm của module (ADR-0028). Sổ: chỉ SELECT, INSERT (R-05, R-20).
-- ---------------------------------------------------------------------------
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_apiclient' AND NOT rolcanlogin) THEN
    RAISE EXCEPTION 'thiếu vai nhóm r_apiclient (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)';
  END IF;
END
$$;
REVOKE ALL ON pf_apiclient.webhook_event, pf_apiclient.webhook_current, pf_apiclient.webhook_breaker, pf_apiclient.webhook_cursor,
  pf_apiclient.webhook_delivery FROM PUBLIC;
GRANT SELECT, INSERT ON pf_apiclient.webhook_event, pf_apiclient.webhook_delivery TO r_apiclient;
GRANT SELECT, INSERT, UPDATE ON pf_apiclient.webhook_current, pf_apiclient.webhook_breaker, pf_apiclient.webhook_cursor TO r_apiclient;
