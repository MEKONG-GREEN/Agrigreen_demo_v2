-- pf_apiclient G5-1 (đợt A): khách hàng Public API, khóa API lưu băm, đăng ký sản phẩm dữ liệu, gói
-- hạn mức có phiên bản, việc xuất bất đồng bộ (ADR-0022; docs/dev/g5-conventions.md §2).
--
-- Mọi bảng thuộc tenant ĐƠN VỊ VẬN HÀNH (AG_PUBLIC_OPERATOR_TENANT); client_tenant_id = tenant bên
-- thứ ba. Không có dữ liệu cá nhân (R-04): đầu mối liên hệ chỉ là contact_ref. Không có bí mật khóa:
-- chỉ key_prefix + key_hmac = HMAC-SHA-256(pepper theo phiên bản, khóa đầy đủ) (ADR-0022 mục 4).
--
--   client_event          SỔ vòng đời khách hàng (proposed → approved → suspended ↔ reactivated → revoked)
--   client_current        bảng chiếu
--   api_key_event         SỔ vòng đời khóa (issued / rotated / revoked / expired — RB-1)
--   api_key_current       bảng chiếu (key_prefix UNIQUE — tra cứu khi xác thực)
--   subscription_event    SỔ đăng ký sản phẩm + scope (proposed → approved → revoked)
--   subscription_current  bảng chiếu
--   quota_plan            SỔ gói hạn mức có phiên bản (proposed → approved)
--   quota_plan_current    bảng chiếu theo phiên bản
--   export_job            việc xuất (mẫu mục 12): đầu vào bất biến, chỉ đổi trạng thái
--   export_file           tệp kết quả (SHA-256, khóa đối tượng) — bất biến
--
-- R-56: người duyệt ≠ người đề xuất kiểm ở app/ VÀ bằng CHECK dưới đây.

-- ---------------------------------------------------------------------------
-- 1. Khách hàng API
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.client_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,                 -- đơn vị vận hành
  stream_key        text        NOT NULL,                 -- 'client:<client_id>'
  seq               bigint      NOT NULL,
  client_id         uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('proposed', 'approved', 'suspended', 'reactivated', 'revoked')),
  client_tenant_id  uuid        NOT NULL,
  display_name      text        NOT NULL CHECK (length(display_name) BETWEEN 2 AND 200),
  purpose           text        NOT NULL CHECK (length(purpose) BETWEEN 10 AND 500),
  contact_ref       uuid        NOT NULL,
  auth_methods      text[]      NOT NULL CHECK (cardinality(auth_methods) BETWEEN 1 AND 3
                      AND 'api_key' = ANY (auth_methods)
                      AND auth_methods <@ ARRAY['api_key', 'oauth_client_credentials', 'mtls']::text[]),
  oauth_client_id   text        NULL CHECK (oauth_client_id IS NULL OR oauth_client_id ~ '^[A-Za-z0-9._-]{3,100}$'),
  proposed_by       uuid        NOT NULL,
  reason            text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
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
  CHECK (client_tenant_id <> tenant_id),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (action <> 'approved' OR actor_ref <> proposed_by),        -- R-56
  CHECK (action NOT IN ('suspended', 'revoked') OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX client_event_client ON pf_apiclient.client_event (tenant_id, client_id, seq);
SELECT shared.apply_ledger('pf_apiclient.client_event', 'pf_apiclient.client_event');

CREATE TABLE pf_apiclient.client_current (
  tenant_id         uuid        NOT NULL,
  client_id         uuid        NOT NULL,
  client_tenant_id  uuid        NOT NULL,
  display_name      text        NOT NULL,
  purpose           text        NOT NULL,
  contact_ref       uuid        NOT NULL,
  auth_methods      text[]      NOT NULL,
  oauth_client_id   text        NULL,
  status            text        NOT NULL CHECK (status IN ('proposed', 'active', 'suspended', 'revoked')),
  proposed_by       uuid        NOT NULL,
  proposed_at       timestamptz NOT NULL,
  approved_by       uuid        NULL,
  approved_at       timestamptz NULL,
  status_reason     text        NULL,
  last_event_id     uuid        NOT NULL,
  last_seq          bigint      NOT NULL,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, client_id),
  CHECK (client_tenant_id <> tenant_id),
  CHECK (approved_by IS NULL OR approved_by <> proposed_by),       -- R-56
  CHECK (status IN ('proposed', 'revoked') OR approved_by IS NOT NULL)
);
CREATE UNIQUE INDEX client_current_oauth ON pf_apiclient.client_current (tenant_id, oauth_client_id) WHERE oauth_client_id IS NOT NULL;
SELECT shared.apply_tenant_rls('pf_apiclient.client_current');

-- ---------------------------------------------------------------------------
-- 2. Khóa API (chỉ prefix + HMAC; KHÔNG có bí mật)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.api_key_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'client:<client_id>'
  seq               bigint      NOT NULL,
  key_id            uuid        NOT NULL,
  client_id         uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('issued', 'rotated', 'revoked', 'expired')),
  key_prefix        text        NOT NULL CHECK (key_prefix ~ '^[a-z2-7]{12}$'),
  key_hmac          bytea       NULL CHECK (key_hmac IS NULL OR length(key_hmac) = 32),
  pepper_version    integer     NOT NULL CHECK (pepper_version >= 1),
  expires_at        timestamptz NOT NULL,
  grace_until       timestamptz NULL,
  replaced_by       uuid        NULL,                     -- rotated: khóa mới
  rotated_from      uuid        NULL,                     -- issued khi xoay: khóa cũ
  reason            text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
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
  CHECK ((action = 'issued') = (key_hmac IS NOT NULL)),
  CHECK (action <> 'rotated' OR (grace_until IS NOT NULL AND replaced_by IS NOT NULL)),
  CHECK (action <> 'revoked' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX api_key_event_key ON pf_apiclient.api_key_event (tenant_id, key_id, seq);
SELECT shared.apply_ledger('pf_apiclient.api_key_event', 'pf_apiclient.api_key_event');

CREATE TABLE pf_apiclient.api_key_current (
  tenant_id         uuid        NOT NULL,
  key_id            uuid        NOT NULL,
  client_id         uuid        NOT NULL,
  key_prefix        text        NOT NULL,
  key_hmac          bytea       NOT NULL CHECK (length(key_hmac) = 32),
  pepper_version    integer     NOT NULL,
  status            text        NOT NULL CHECK (status IN ('active', 'rotated', 'revoked', 'expired')),
  issued_at         timestamptz NOT NULL,
  issued_by         uuid        NOT NULL,
  expires_at        timestamptz NOT NULL,
  grace_until       timestamptz NULL,
  revoked_at        timestamptz NULL,
  revoke_reason     text        NULL,
  last_event_id     uuid        NOT NULL,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, key_id),
  CHECK (status <> 'rotated' OR grace_until IS NOT NULL),
  CHECK ((status = 'revoked') = (revoked_at IS NOT NULL))
);
-- Prefix duy nhất toàn bảng (sinh ngẫu nhiên 60 bit) — tra khóa khi xác thực.
CREATE UNIQUE INDEX api_key_current_prefix ON pf_apiclient.api_key_current (key_prefix);
CREATE INDEX api_key_current_client ON pf_apiclient.api_key_current (tenant_id, client_id, status);
SELECT shared.apply_tenant_rls('pf_apiclient.api_key_current');

-- ---------------------------------------------------------------------------
-- 3. Đăng ký sản phẩm dữ liệu (scope cấp qua đây)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.subscription_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'client:<client_id>'
  seq               bigint      NOT NULL,
  subscription_id   uuid        NOT NULL,
  client_id         uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('proposed', 'approved', 'revoked')),
  product           text        NOT NULL CHECK (product ~ '^[a-z][a-z0-9_]{2,47}$'),
  scopes            text[]      NOT NULL CHECK (cardinality(scopes) BETWEEN 1 AND 3),
  proposed_by       uuid        NOT NULL,
  reason            text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
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
  CHECK (action <> 'approved' OR actor_ref <> proposed_by),        -- R-56
  CHECK (action <> 'revoked' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX subscription_event_sub ON pf_apiclient.subscription_event (tenant_id, subscription_id, seq);
SELECT shared.apply_ledger('pf_apiclient.subscription_event', 'pf_apiclient.subscription_event');

CREATE TABLE pf_apiclient.subscription_current (
  tenant_id         uuid        NOT NULL,
  subscription_id   uuid        NOT NULL,
  client_id         uuid        NOT NULL,
  product           text        NOT NULL,
  scopes            text[]      NOT NULL,
  status            text        NOT NULL CHECK (status IN ('proposed', 'active', 'revoked')),
  proposed_by       uuid        NOT NULL,
  proposed_at       timestamptz NOT NULL,
  approved_by       uuid        NULL,
  approved_at       timestamptz NULL,
  revoked_at        timestamptz NULL,
  revoke_reason     text        NULL,
  last_event_id     uuid        NOT NULL,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, subscription_id),
  CHECK (approved_by IS NULL OR approved_by <> proposed_by),       -- R-56
  CHECK (status <> 'active' OR approved_by IS NOT NULL)
);
-- Một đăng ký chưa thu hồi cho mỗi (khách hàng, sản phẩm).
CREATE UNIQUE INDEX subscription_current_open ON pf_apiclient.subscription_current (tenant_id, client_id, product) WHERE status <> 'revoked';
SELECT shared.apply_tenant_rls('pf_apiclient.subscription_current');

-- ---------------------------------------------------------------------------
-- 4. Gói hạn mức có phiên bản (tạm, chờ khách — g5-conventions §1.2)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.quota_plan (
  id                  uuid        NOT NULL,
  tenant_id           uuid        NOT NULL,
  stream_key          text        NOT NULL,               -- 'plan:<client_id>'
  seq                 bigint      NOT NULL,
  client_id           uuid        NOT NULL,
  version             integer     NOT NULL CHECK (version >= 1),
  action              text        NOT NULL CHECK (action IN ('proposed', 'approved')),
  requests_per_minute bigint      NOT NULL CHECK (requests_per_minute BETWEEN 1 AND 100000),
  daily_units         bigint      NOT NULL CHECK (daily_units BETWEEN 1 AND 1000000000),
  monthly_units       bigint      NOT NULL CHECK (monthly_units BETWEEN 1 AND 100000000000),
  max_export_rows     bigint      NOT NULL CHECK (max_export_rows BETWEEN 1 AND 100000000),
  effective_from      timestamptz NOT NULL,
  proposed_by         uuid        NOT NULL,
  event_time          timestamptz NOT NULL,
  recorded_at         timestamptz NOT NULL DEFAULT now(),
  supersedes_id       uuid        NULL,
  correction_reason   text        NULL,
  actor_ref           uuid        NOT NULL,
  device_id           text        NULL,
  idem_key            text        NOT NULL,
  rule_version        text        NOT NULL,
  prev_hash           bytea       NOT NULL,
  row_hash            bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (daily_units <= monthly_units),
  CHECK (action <> 'approved' OR actor_ref <> proposed_by)         -- R-56
) PARTITION BY RANGE (recorded_at);
CREATE INDEX quota_plan_client ON pf_apiclient.quota_plan (tenant_id, client_id, version);
SELECT shared.apply_ledger('pf_apiclient.quota_plan', 'pf_apiclient.quota_plan');

CREATE TABLE pf_apiclient.quota_plan_current (
  tenant_id           uuid        NOT NULL,
  client_id           uuid        NOT NULL,
  version             integer     NOT NULL,
  requests_per_minute bigint      NOT NULL,
  daily_units         bigint      NOT NULL,
  monthly_units       bigint      NOT NULL,
  max_export_rows     bigint      NOT NULL,
  effective_from      timestamptz NOT NULL,
  status              text        NOT NULL CHECK (status IN ('proposed', 'approved')),
  proposed_by         uuid        NOT NULL,
  proposed_at         timestamptz NOT NULL,
  approved_by         uuid        NULL,
  approved_at         timestamptz NULL,
  updated_at          timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, client_id, version),
  CHECK (approved_by IS NULL OR approved_by <> proposed_by),       -- R-56
  CHECK ((status = 'approved') = (approved_by IS NOT NULL))
);
SELECT shared.apply_tenant_rls('pf_apiclient.quota_plan_current');

-- ---------------------------------------------------------------------------
-- 5. Việc xuất bất đồng bộ (mẫu mục 12, R-52)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.export_job (
  id                uuid        PRIMARY KEY,              -- UUIDv7 = job_id
  tenant_id         uuid        NOT NULL,                 -- đơn vị vận hành
  client_id         uuid        NOT NULL,
  client_tenant_id  uuid        NOT NULL,
  data_tenant_id    uuid        NOT NULL,                 -- bên cấp đã kiểm cấp quyền lúc yêu cầu
  product           text        NOT NULL,
  contract_version  integer     NOT NULL CHECK (contract_version >= 1),
  format            text        NOT NULL CHECK (format IN ('csv', 'json')),
  filters           jsonb       NOT NULL CHECK (jsonb_typeof(filters) = 'object'),
  k_applied         integer     NOT NULL CHECK (k_applied >= 0),
  max_rows          bigint      NOT NULL CHECK (max_rows >= 1),
  request_id        uuid        NOT NULL,
  request_hash      text        NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
  idem_key          text        NOT NULL,
  requested_at      timestamptz NOT NULL,                 -- mốc ảnh chụp dữ liệu (as_of)
  status            text        NOT NULL CHECK (status IN ('queued', 'completed', 'failed')),
  rows_count        bigint      NULL,
  sha256            text        NULL CHECK (sha256 IS NULL OR sha256 ~ '^[0-9a-f]{64}$'),
  error_code        text        NULL,
  completed_at      timestamptz NULL,
  UNIQUE (tenant_id, idem_key),
  CHECK ((status = 'completed') = (sha256 IS NOT NULL AND completed_at IS NOT NULL AND rows_count IS NOT NULL)),
  CHECK (status <> 'failed' OR (error_code IS NOT NULL AND completed_at IS NOT NULL))
);
CREATE INDEX export_job_client ON pf_apiclient.export_job (tenant_id, client_id, id);
SELECT shared.apply_tenant_rls('pf_apiclient.export_job');

CREATE FUNCTION pf_apiclient.fn_export_job_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status IN ('completed', 'failed') THEN
    RAISE EXCEPTION 'export_job %: job đã % là bất biến', OLD.id, OLD.status
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id OR NEW.client_id <> OLD.client_id
     OR NEW.client_tenant_id <> OLD.client_tenant_id OR NEW.data_tenant_id <> OLD.data_tenant_id
     OR NEW.product <> OLD.product OR NEW.contract_version <> OLD.contract_version OR NEW.format <> OLD.format
     OR NEW.filters <> OLD.filters OR NEW.k_applied <> OLD.k_applied OR NEW.max_rows <> OLD.max_rows
     OR NEW.request_id <> OLD.request_id OR NEW.request_hash <> OLD.request_hash OR NEW.idem_key <> OLD.idem_key
     OR NEW.requested_at <> OLD.requested_at THEN
    RAISE EXCEPTION 'export_job %: ảnh chụp đầu vào bất biến, chỉ đổi được trạng thái', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_export_job_guard BEFORE UPDATE ON pf_apiclient.export_job
  FOR EACH ROW EXECUTE FUNCTION pf_apiclient.fn_export_job_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_apiclient.export_job
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_apiclient.export_job
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

CREATE TABLE pf_apiclient.export_file (
  tenant_id         uuid        NOT NULL,
  job_id            uuid        NOT NULL,
  object_key        text        NOT NULL,
  sha256            text        NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  size_bytes        bigint      NOT NULL CHECK (size_bytes >= 0),
  rows_count        bigint      NOT NULL CHECK (rows_count >= 0),
  content_type      text        NOT NULL,
  created_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, job_id)
);
SELECT shared.apply_tenant_rls('pf_apiclient.export_file');
CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON pf_apiclient.export_file
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON pf_apiclient.export_file
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 6. Quyền của apiclient_app (R-05). Sổ: chỉ SELECT, INSERT.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_apiclient.client_event, pf_apiclient.api_key_event, pf_apiclient.subscription_event,
  pf_apiclient.quota_plan, pf_apiclient.export_file TO apiclient_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_apiclient.client_event, pf_apiclient.api_key_event, pf_apiclient.subscription_event,
  pf_apiclient.quota_plan, pf_apiclient.export_file FROM apiclient_app;
GRANT SELECT, INSERT, UPDATE ON pf_apiclient.client_current, pf_apiclient.api_key_current, pf_apiclient.subscription_current,
  pf_apiclient.quota_plan_current, pf_apiclient.export_job TO apiclient_app;
