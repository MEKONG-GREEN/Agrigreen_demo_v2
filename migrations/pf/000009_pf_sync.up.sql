-- pf_sync: đồng bộ ngoại tuyến (mục 15, ADR-0006, docs/dev/sync-contract.md).
--
--   device    : thiết bị hiện trường đã đăng ký, thu hồi → máy xóa dữ liệu cục bộ.
--   inbound   : sổ chỉ ghi thêm — MỌI bản ghi máy gửi lên, kèm kết quả (kiểm toán).
--               KHÔNG lưu payload (có thể chứa dữ liệu cá nhân), chỉ lưu SHA-256.
--   conflict  : sổ đăng ký xung đột chờ xử lý, có người phụ trách và SLA (C-8).
--
-- Dữ liệu nghiệp vụ của bản ghi nằm ở module sở hữu entity (Sink), không ở đây.

CREATE SCHEMA pf_sync;
REVOKE ALL ON SCHEMA pf_sync FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_sync TO sync_app;

-- ---------------------------------------------------------------------------
-- Thiết bị. Bảng trạng thái (không phải sổ): last_seen_at và thu hồi được UPDATE.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_sync.device (
  tenant_id     uuid        NOT NULL,
  device_id     uuid        NOT NULL,                 -- UUIDv7 sinh trên máy (R-11)
  actor_ref     uuid        NOT NULL,                 -- người đăng ký (sub của token)
  org_ref       uuid        NULL,                     -- đơn vị công tác lúc đăng ký (claim org_ref)
  platform      text        NOT NULL CHECK (platform IN ('android', 'ios')),
  app_version   text        NOT NULL CHECK (app_version ~ '^[0-9A-Za-z.+_-]{1,40}$'),
  registered_at timestamptz NOT NULL,
  last_seen_at  timestamptz NULL,
  revoked_at    timestamptz NULL,
  revoked_by    uuid        NULL,
  revoke_reason text        NULL,
  PRIMARY KEY (tenant_id, device_id),
  CHECK ((revoked_at IS NULL) = (revoked_by IS NULL)),
  CHECK (revoked_at IS NULL OR char_length(btrim(revoke_reason)) >= 10)
);
CREATE INDEX device_actor ON pf_sync.device (tenant_id, actor_ref, device_id);
SELECT shared.apply_tenant_rls('pf_sync.device');

-- ---------------------------------------------------------------------------
-- Sổ nhận bản ghi chiều lên (mẫu mục 8.1). stream_key = 'device:<device_id>'.
-- idem_key của SỔ = 'in:<mã lô>:<vị trí>' — mỗi lần nhận một dòng, kể cả bản ghi
-- trùng hoặc bị từ chối; record_idem_key là idem_key do máy sinh.
-- supersedes_id/correction_reason là cột bù của CHÍNH dòng sổ (luôn NULL: dòng
-- nhận không bao giờ bị sửa); bản ghi bù của máy lưu ở record_supersedes_id.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_sync.inbound (
  id                   uuid        NOT NULL,                 -- UUIDv7 (R-11), do server sinh
  tenant_id            uuid        NOT NULL,
  stream_key           text        NOT NULL,
  seq                  bigint      NOT NULL,
  batch_key            text        NOT NULL,                 -- Idempotency-Key của lô
  record_index         integer     NOT NULL CHECK (record_index >= 0),
  record_id            uuid        NULL,                     -- NULL khi máy gửi id sai dạng
  record_idem_key      text        NOT NULL CHECK (length(record_idem_key) <= 200),
  entity               text        NOT NULL CHECK (length(entity) <= 40),
  op                   text        NOT NULL CHECK (length(op) <= 20),
  status               text        NOT NULL CHECK (status IN ('accepted', 'duplicate', 'conflict', 'rejected')),
  code                 text        NULL,
  conflict_id          uuid        NULL,
  record_supersedes_id uuid        NULL,
  payload_sha256       text        NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  event_time           timestamptz NOT NULL,                 -- R-13: lúc xảy ra trên máy (sai dạng → lúc nhận)
  recorded_at          timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
  supersedes_id        uuid        NULL,
  correction_reason    text        NULL,
  actor_ref            uuid        NOT NULL,
  device_id            text        NOT NULL,
  idem_key             text        NOT NULL,
  rule_version         text        NOT NULL,
  prev_hash            bytea       NOT NULL,
  row_hash             bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK ((status = 'conflict') = (conflict_id IS NOT NULL))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX inbound_idem       ON pf_sync.inbound (tenant_id, idem_key);
CREATE INDEX inbound_stream     ON pf_sync.inbound (tenant_id, stream_key, seq);
CREATE INDEX inbound_record_key ON pf_sync.inbound (tenant_id, record_idem_key);
CREATE INDEX inbound_conflict   ON pf_sync.inbound (tenant_id, conflict_id) WHERE conflict_id IS NOT NULL;

SELECT shared.apply_ledger('pf_sync.inbound', 'pf_sync.inbound');

-- ---------------------------------------------------------------------------
-- Sổ đăng ký xung đột. Bảng trạng thái: status open → resolved (UPDATE), không xóa.
-- due_at = opened_at + tham số masterdata sync.conflict_sla_hours (mặc định 72, C-8).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_sync.conflict (
  tenant_id          uuid        NOT NULL,
  id                 uuid        NOT NULL,                 -- conflict_id do Sink trả
  entity             text        NOT NULL,
  first_record_id    uuid        NOT NULL,
  first_idem_key     text        NOT NULL,
  device_id          uuid        NOT NULL,
  reported_by        uuid        NOT NULL,                 -- actor_ref của người gửi bản ghi
  owner_org_ref      uuid        NULL,                     -- đơn vị phụ trách xử lý (đơn vị của người gửi)
  opened_at          timestamptz NOT NULL,
  due_at             timestamptz NOT NULL,
  status             text        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
  resolved_at        timestamptz NULL,
  resolved_by        uuid        NULL,
  resolution_note    text        NULL,
  resolved_record_id uuid        NULL,
  PRIMARY KEY (tenant_id, id),
  CHECK (due_at > opened_at),
  CHECK ((status = 'resolved') = (resolved_at IS NOT NULL)),
  CHECK (status = 'open' OR (resolved_by IS NOT NULL AND char_length(btrim(resolution_note)) >= 10))
);
CREATE INDEX conflict_open_due ON pf_sync.conflict (tenant_id, due_at) WHERE status = 'open';
CREATE INDEX conflict_status   ON pf_sync.conflict (tenant_id, status, id);
SELECT shared.apply_tenant_rls('pf_sync.conflict');

-- ---------------------------------------------------------------------------
-- Quyền của sync_app. Sổ: chỉ SELECT, INSERT. Bảng trạng thái: không DELETE.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_sync.inbound TO sync_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_sync.inbound FROM sync_app;
GRANT SELECT, INSERT, UPDATE ON pf_sync.device, pf_sync.conflict TO sync_app;
