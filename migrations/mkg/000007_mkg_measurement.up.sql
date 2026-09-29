-- mkg_measurement (1/2): sổ thiết bị cân, phiên cân, phiếu cân, khối lượng lô
-- (tài liệu kiến trúc mục 13.1–13.2; docs/design/mkg.md §5.2; ADR-0019).
--
-- Mức event sourcing B (mục 8.3): ở measurement SỔ LÀ NGUỒN. Mọi bảng *_current
-- là bảng chiếu cập nhật trong CÙNG giao dịch với sổ (R-21) và dựng lại được từ
-- sổ (R-22, POST /v1/measurement/projections/rebuild).
--
--   device_directory  thư mục device_id → tenant (KHÔNG RLS, chỉ ghi thêm): ingest suy ra tenant từ thiết bị (R-16)
--   device_version    SỔ đề xuất đăng ký / thu hồi kỳ đăng ký thiết bị. stream 'device:<device_id>'
--   device_decision   SỔ phê duyệt / từ chối đề xuất đăng ký (người duyệt ≠ người đề xuất, R-56)
--   device_current    bảng chiếu kỳ đăng ký + sức khỏe (last seen, bộ đệm, độ lệch đồng hồ)
--   weighing_session  SỔ phiên cân của lô ở một điểm cân: thiết bị + cửa sổ BỘ ĐẾM. stream 'session:<id>'
--   weighing_session_current bảng chiếu phiên cân
--   weigh_ticket      SỔ phiếu cân (dẫn xuất từ luồng thô bởi ingest — R-70 — hoặc nhập tay có bằng chứng). stream 'lot:<lot_ref>'
--   lot_mass_current  bảng chiếu phiếu HIỆU LỰC của ba điểm cân mỗi lô (đã tính phiếu bù)
--   lot_terms         SỔ thuộc tính thương mại của lô dùng cho đối soát (cách tính giá — tạm tới G4 orders)
--   lot_terms_current bảng chiếu
--
-- Khối lượng kg numeric(14,3), độ ẩm numeric(5,2) (R-10). Không dữ liệu cá nhân
-- (R-04): khách, hợp đồng, bằng chứng chỉ là *_ref UUID thuần (R-06).

CREATE SCHEMA mkg_measurement;
REVOKE ALL ON SCHEMA mkg_measurement FROM PUBLIC;
GRANT USAGE ON SCHEMA mkg_measurement TO measurement_app, ingest_mkg;

-- ---------------------------------------------------------------------------
-- 1. Thư mục thiết bị (xuyên tenant)
-- ---------------------------------------------------------------------------
-- Ingest nhận lô qua mTLS, CHƯA biết tenant: tenant phải suy ra từ thiết bị,
-- không bao giờ từ lô (R-16). Vai ứng dụng là NOBYPASSRLS và owner bị FORCE RLS,
-- nên cần một bảng nhỏ không RLS chỉ gồm (device_id, tenant_id) — không khóa,
-- không chứng thư, không trạng thái. Chỉ ghi thêm: device_id gắn với MỘT tenant
-- mãi mãi (thiết bị chuyển tenant = device_id mới).
CREATE TABLE mkg_measurement.device_directory (
  device_id     text        PRIMARY KEY CHECK (device_id ~ '^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$'),
  tenant_id     uuid        NOT NULL,
  registered_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE mkg_measurement.device_directory IS
  'ag:no-rls thư mục device_id → tenant_id cho ingest suy ra tenant từ chứng thư thiết bị (R-16); không chứa khóa/chứng thư/trạng thái; chỉ ghi thêm';
CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON mkg_measurement.device_directory
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON mkg_measurement.device_directory
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 2. Sổ thiết bị cân
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_measurement.device_version (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'device:<device_id>'
  seq               bigint      NOT NULL,
  scale_device_id   text        NOT NULL,                 -- = CN/SAN chứng thư mTLS client
  epoch             integer     NOT NULL CHECK (epoch >= 1),
  action            text        NOT NULL CHECK (action IN ('register', 'revoke')),
  facility_ref      uuid        NULL,                     -- hub/kho (mkg/hub)
  point             text        NULL CHECK (point IS NULL OR point IN ('HUB_IN', 'HUB_OUT')),
  public_key        bytea       NULL CHECK (public_key IS NULL OR length(public_key) = 32),  -- Ed25519
  key_id            text        NULL CHECK (key_id IS NULL OR key_id ~ '^[0-9a-f]{16}$'),
  cert_sha256       text        NULL CHECK (cert_sha256 IS NULL OR cert_sha256 ~ '^[0-9a-f]{64}$'),
  scale_model       text        NULL CHECK (scale_model IS NULL OR length(scale_model) BETWEEN 1 AND 100),  -- C-6
  reason            text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người đề xuất (register) / người thu hồi (revoke)
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (action <> 'register' OR (facility_ref IS NOT NULL AND point IS NOT NULL AND public_key IS NOT NULL
                                  AND key_id IS NOT NULL AND cert_sha256 IS NOT NULL AND scale_model IS NOT NULL)),
  CHECK (action <> 'revoke' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX device_version_idem   ON mkg_measurement.device_version (tenant_id, idem_key);
CREATE INDEX device_version_stream ON mkg_measurement.device_version (tenant_id, stream_key, seq);
CREATE INDEX device_version_id     ON mkg_measurement.device_version (id);
SELECT shared.apply_ledger('mkg_measurement.device_version', 'mkg_measurement.device_version');

CREATE TABLE mkg_measurement.device_decision (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'device:<device_id>'
  seq               bigint      NOT NULL,
  scale_device_id   text        NOT NULL,
  epoch             integer     NOT NULL CHECK (epoch >= 1),
  version_id        uuid        NOT NULL,
  decision          text        NOT NULL CHECK (decision IN ('approved', 'rejected')),
  reason            text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  proposer_ref      uuid        NOT NULL,
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người duyệt (device_approver)
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (actor_ref <> proposer_ref),                     -- R-56 ở tầng DB
  CHECK (decision = 'approved' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX device_decision_idem   ON mkg_measurement.device_decision (tenant_id, idem_key);
CREATE INDEX device_decision_stream ON mkg_measurement.device_decision (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_measurement.device_decision', 'mkg_measurement.device_decision');

CREATE TABLE mkg_measurement.device_current (
  tenant_id         uuid          NOT NULL,
  device_id         text          NOT NULL,
  epoch             integer       NOT NULL,
  facility_ref      uuid          NOT NULL,
  point             text          NOT NULL,
  public_key        bytea         NOT NULL,
  key_id            text          NOT NULL,
  cert_sha256       text          NOT NULL,
  scale_model       text          NOT NULL,
  status            text          NOT NULL CHECK (status IN ('pending', 'active', 'rejected', 'revoked', 'superseded')),
  version_id        uuid          NOT NULL,
  proposed_by       uuid          NOT NULL,
  proposed_at       timestamptz   NOT NULL,
  decided_by        uuid          NULL,
  decided_at        timestamptz   NULL,
  revoked_at        timestamptz   NULL,
  revoke_reason     text          NULL,
  -- Sức khỏe do ingest cập nhật (ADR-0019): không phải sổ, không dùng làm thứ tự.
  last_seen_at      timestamptz   NULL,
  buffer_pct        numeric(5,2)  NULL CHECK (buffer_pct IS NULL OR (buffer_pct >= 0 AND buffer_pct <= 100)),
  clock_skew_ms     bigint        NULL,
  last_counter      bigint        NULL,
  health_updated_at timestamptz   NULL,
  updated_at        timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, device_id, epoch)
);
-- Một thiết bị có tối đa một kỳ đang hiệu lực và một đề xuất chờ duyệt.
CREATE UNIQUE INDEX device_current_one_active  ON mkg_measurement.device_current (tenant_id, device_id) WHERE status = 'active';
CREATE UNIQUE INDEX device_current_one_pending ON mkg_measurement.device_current (tenant_id, device_id) WHERE status = 'pending';
SELECT shared.apply_tenant_rls('mkg_measurement.device_current');

-- ---------------------------------------------------------------------------
-- 3. Phiên cân (cửa sổ lô theo bộ đếm)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_measurement.weighing_session (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'session:<session_id>'
  seq               bigint      NOT NULL,
  session_id        uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('open', 'close')),
  lot_ref           uuid        NOT NULL,
  point             text        NOT NULL CHECK (point IN ('HUB_IN', 'HUB_OUT')),
  route             text        NOT NULL CHECK (route = 'HUB'),
  facility_ref      uuid        NOT NULL,
  scale_device_id   text        NOT NULL,
  epoch             integer     NOT NULL CHECK (epoch >= 1),
  counter_from      bigint      NOT NULL CHECK (counter_from >= 0),
  counter_to        bigint      NULL,
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
  CHECK ((action = 'close') = (counter_to IS NOT NULL)),
  CHECK (counter_to IS NULL OR counter_to >= counter_from)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX weighing_session_idem   ON mkg_measurement.weighing_session (tenant_id, idem_key);
CREATE INDEX weighing_session_stream ON mkg_measurement.weighing_session (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_measurement.weighing_session', 'mkg_measurement.weighing_session');

CREATE TABLE mkg_measurement.weighing_session_current (
  tenant_id       uuid        NOT NULL,
  session_id      uuid        NOT NULL,
  lot_ref         uuid        NOT NULL,
  point           text        NOT NULL,
  route           text        NOT NULL,
  facility_ref    uuid        NOT NULL,
  device_id       text        NOT NULL,
  epoch           integer     NOT NULL,
  counter_from    bigint      NOT NULL,
  counter_to      bigint      NULL,
  status          text        NOT NULL CHECK (status IN ('open', 'closed')),
  opened_at       timestamptz NOT NULL,
  opened_by       uuid        NOT NULL,
  closed_at       timestamptz NULL,
  closed_by       uuid        NULL,
  last_seq        bigint      NOT NULL,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, session_id),
  CHECK ((status = 'closed') = (counter_to IS NOT NULL))
);
-- Một thiết bị (kỳ) chỉ có một phiên mở — cửa sổ bộ đếm không chồng nhau.
CREATE UNIQUE INDEX weighing_session_current_one_open ON mkg_measurement.weighing_session_current (tenant_id, device_id, epoch) WHERE status = 'open';
CREATE INDEX weighing_session_current_lot ON mkg_measurement.weighing_session_current (tenant_id, lot_ref);
SELECT shared.apply_tenant_rls('mkg_measurement.weighing_session_current');

-- ---------------------------------------------------------------------------
-- 4. Phiếu cân — sổ là nguồn
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_measurement.weigh_ticket (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'lot:<lot_ref>'
  seq               bigint        NOT NULL,
  lot_ref           uuid          NOT NULL,
  point             text          NOT NULL CHECK (point IN ('HUB_IN', 'HUB_OUT', 'CUSTOMER_IN')),
  route             text          NOT NULL CHECK (route IN ('HUB', 'DIRECT')),
  source            text          NOT NULL CHECK (source IN ('edge', 'customer_integration', 'manual_evidence')),
  mass_kg           numeric(14,3) NOT NULL CHECK (mass_kg >= 0),
  moisture_pct      numeric(5,2)  NULL CHECK (moisture_pct IS NULL OR (moisture_pct >= 0 AND moisture_pct <= 100)),
  moisture_state    text          NOT NULL CHECK (moisture_state IN ('not_measured', 'as_received', 'after_drying')),  -- C-4 tạm
  session_ref       uuid          NULL,
  epoch             integer       NULL CHECK (epoch IS NULL OR epoch >= 1),
  counter_from      bigint        NULL,
  counter_to        bigint        NULL,
  readings_used     integer       NULL CHECK (readings_used IS NULL OR readings_used >= 0),
  readings_total    integer       NULL CHECK (readings_total IS NULL OR readings_total >= 0),
  batch_ids         jsonb         NULL CHECK (batch_ids IS NULL OR jsonb_typeof(batch_ids) = 'array'),  -- ts_weigh.reading_batch đã dùng (R-70)
  readings_sha256   text          NULL CHECK (readings_sha256 IS NULL OR readings_sha256 ~ '^[0-9a-f]{64}$'),
  evidence_ref      uuid          NULL,                   -- tệp bằng chứng (ảnh phiếu cân, biên bản)
  customer_ref      uuid          NULL,
  import_ref        uuid          NULL,                   -- customer_weight_import (C-5)
  reason            text          NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  event_time        timestamptz   NOT NULL,               -- lúc cân tại hiện trường (R-13)
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,                   -- phiếu bù: phiếu bị thay (cùng lô, cùng điểm)
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,
  device_id         text          NULL,                   -- thiết bị cân (nguồn edge)
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,               -- edge: thuật toán tích phân (weigh.integrate.v1…)
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  -- R-70: phiếu edge có đủ thiết bị, kỳ, cửa sổ bộ đếm, lô đọc; không bao giờ ở CUSTOMER_IN.
  CHECK (source <> 'edge' OR (point <> 'CUSTOMER_IN' AND device_id IS NOT NULL AND epoch IS NOT NULL
                              AND counter_from IS NOT NULL AND counter_to IS NOT NULL AND counter_to >= counter_from
                              AND batch_ids IS NOT NULL)),
  CHECK (source <> 'manual_evidence' OR (evidence_ref IS NOT NULL AND reason IS NOT NULL)),
  CHECK (source <> 'customer_integration' OR point = 'CUSTOMER_IN'),
  CHECK (route <> 'DIRECT' OR point = 'CUSTOMER_IN'),
  CHECK (moisture_state <> 'not_measured' OR moisture_pct IS NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX weigh_ticket_idem       ON mkg_measurement.weigh_ticket (tenant_id, idem_key);
CREATE INDEX weigh_ticket_stream     ON mkg_measurement.weigh_ticket (tenant_id, stream_key, seq);
CREATE INDEX weigh_ticket_id         ON mkg_measurement.weigh_ticket (id);
CREATE INDEX weigh_ticket_supersedes ON mkg_measurement.weigh_ticket (tenant_id, supersedes_id) WHERE supersedes_id IS NOT NULL;
CREATE INDEX weigh_ticket_window     ON mkg_measurement.weigh_ticket (tenant_id, device_id, epoch, counter_from) WHERE device_id IS NOT NULL;
SELECT shared.apply_ledger('mkg_measurement.weigh_ticket', 'mkg_measurement.weigh_ticket');

-- Bảng chiếu: phiếu HIỆU LỰC của ba điểm cân (RB-6, Q-MKG-04).
CREATE TABLE mkg_measurement.lot_mass_current (
  tenant_id            uuid          NOT NULL,
  lot_ref              uuid          NOT NULL,
  route                text          NOT NULL CHECK (route IN ('HUB', 'DIRECT')),
  hub_in_kg            numeric(14,3) NULL,
  hub_in_ticket        uuid          NULL,
  hub_in_at            timestamptz   NULL,
  hub_in_moisture_pct  numeric(5,2)  NULL,
  hub_out_kg           numeric(14,3) NULL,
  hub_out_ticket       uuid          NULL,
  hub_out_at           timestamptz   NULL,
  customer_in_kg       numeric(14,3) NULL,
  customer_in_ticket   uuid          NULL,
  customer_in_at       timestamptz   NULL,
  last_seq             bigint        NOT NULL,
  as_of                timestamptz   NOT NULL,          -- recorded_at của phiếu mới nhất đã tính
  updated_at           timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, lot_ref),
  CHECK ((hub_in_kg IS NULL) = (hub_in_ticket IS NULL)),
  CHECK ((hub_out_kg IS NULL) = (hub_out_ticket IS NULL)),
  CHECK ((customer_in_kg IS NULL) = (customer_in_ticket IS NULL)),
  CHECK (route <> 'DIRECT' OR (hub_in_kg IS NULL AND hub_out_kg IS NULL))
);
SELECT shared.apply_tenant_rls('mkg_measurement.lot_mass_current');

-- ---------------------------------------------------------------------------
-- 5. Thuộc tính thương mại của lô (tạm tới G4 orders)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_measurement.lot_terms (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'terms:<lot_ref>'
  seq               bigint      NOT NULL,
  lot_ref           uuid        NOT NULL,
  pricing_basis     text        NOT NULL CHECK (pricing_basis IN ('per_tonne', 'per_bale')),
  contract_ref      uuid        NULL,                     -- hợp đồng (mkg/orders G4)
  customer_ref      uuid        NULL,
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
CREATE INDEX lot_terms_idem   ON mkg_measurement.lot_terms (tenant_id, idem_key);
CREATE INDEX lot_terms_stream ON mkg_measurement.lot_terms (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_measurement.lot_terms', 'mkg_measurement.lot_terms');

CREATE TABLE mkg_measurement.lot_terms_current (
  tenant_id     uuid        NOT NULL,
  lot_ref       uuid        NOT NULL,
  terms_id      uuid        NOT NULL,
  pricing_basis text        NOT NULL,
  contract_ref  uuid        NULL,
  customer_ref  uuid        NULL,
  last_seq      bigint      NOT NULL,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, lot_ref)
);
SELECT shared.apply_tenant_rls('mkg_measurement.lot_terms_current');

-- ---------------------------------------------------------------------------
-- 6. Quyền
-- ---------------------------------------------------------------------------
-- Vai module: sổ chỉ SELECT + INSERT; bảng chiếu được UPDATE (R-21), DELETE chỉ để dựng lại (R-22).
GRANT SELECT, INSERT ON mkg_measurement.device_directory TO measurement_app;
GRANT SELECT, INSERT ON mkg_measurement.device_version, mkg_measurement.device_decision, mkg_measurement.weighing_session,
                        mkg_measurement.weigh_ticket, mkg_measurement.lot_terms TO measurement_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_measurement.device_directory, mkg_measurement.device_version,
                                   mkg_measurement.device_decision, mkg_measurement.weighing_session,
                                   mkg_measurement.weigh_ticket, mkg_measurement.lot_terms FROM measurement_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_measurement.device_current, mkg_measurement.weighing_session_current,
                                        mkg_measurement.lot_mass_current, mkg_measurement.lot_terms_current TO measurement_app;

-- Vai ingest (cmd/ingest, cụm mkg) — ĐÚNG những gì TicketWriter / DeviceRegistry /
-- DeviceHealth / WeighingSessions cần, không hơn:
--   * DeviceRegistry.Resolve: đọc thư mục + kỳ đăng ký;
--   * DeviceHealth: UPDATE chỉ các cột sức khỏe của device_current;
--   * WeighingSessions + kiểm SessionRef: đọc phiên cân;
--   * TicketWriter.Record: SELECT + INSERT sổ phiếu, SELECT/INSERT/UPDATE bảng
--     chiếu lot_mass_current cùng giao dịch (R-21); outbox qua ag_shared_user.
-- Không có quyền nào trên thiết bị (đăng ký/duyệt), phiên (mở/đóng), dung sai,
-- đối soát, tranh chấp, van.
GRANT SELECT ON mkg_measurement.device_directory, mkg_measurement.device_current,
                mkg_measurement.weighing_session_current TO ingest_mkg;
GRANT UPDATE (last_seen_at, buffer_pct, clock_skew_ms, last_counter, health_updated_at) ON mkg_measurement.device_current TO ingest_mkg;
GRANT SELECT, INSERT ON mkg_measurement.weigh_ticket TO ingest_mkg;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_measurement.weigh_ticket FROM ingest_mkg;
GRANT SELECT, INSERT, UPDATE ON mkg_measurement.lot_mass_current TO ingest_mkg;
