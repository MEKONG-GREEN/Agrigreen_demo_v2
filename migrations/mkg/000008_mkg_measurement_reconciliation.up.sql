-- mkg_measurement (2/2): tham số có phiên bản (dung sai C-4, định mức hao hụt
-- sấy), đối soát theo lô, sản lượng quy đổi, tranh chấp, nhập số cân của khách
-- (C-5), sổ van mrv_collection_evidence (tài liệu kiến trúc mục 13.1, 13.3, 16).
--
--   tolerance_version       SỔ phiên bản bảng dung sai (người đặt ≠ người duyệt). stream 'tolerance:commercial'
--   loss_norm_version       SỔ phiên bản định mức hao hụt sấy theo độ ẩm. stream 'loss_norm:drying'
--   param_decision          SỔ duyệt / từ chối phiên bản tham số (R-56, CHECK DB)
--   param_current           bảng chiếu trạng thái phiên bản tham số
--   reconciliation          SỔ đối soát của lô (lần sau thay lần trước bằng supersedes_id). stream 'lot:<lot_ref>'
--   lot_yield_current       bảng chiếu yield_kg_per_roll (KHÔNG BAO GIỜ qua van)
--   dispute_event           SỔ hồ sơ tranh chấp thương mại. stream 'dispute:<id>'
--   dispute_current         bảng chiếu tranh chấp
--   customer_weight_import  SỔ lần nhập số cân của khách (tệp / API / tay có bằng chứng — C-5 tạm). stream 'import:<id>'
--   valve_export            SỔ xuất van công bố (chỉ mrv_collection_evidence, khối lượng THEO BẬC)
--
-- Mọi tham số mặc định là TẠM, chờ khách (C-4, định mức sấy): dung sai cờ 5 %,
-- nghiêm trọng 15 % (prototype plant-weighing). Tỷ lệ phần trăm numeric (R-10).

-- ---------------------------------------------------------------------------
-- 1. Tham số có phiên bản
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_measurement.tolerance_version (
  id                  uuid         NOT NULL,
  tenant_id           uuid         NOT NULL,
  stream_key          text         NOT NULL,              -- 'tolerance:commercial'
  seq                 bigint       NOT NULL,              -- = số phiên bản
  commercial_flag_pct     numeric(5,2) NOT NULL CHECK (commercial_flag_pct >= 0),
  commercial_critical_pct numeric(5,2) NOT NULL,
  roll_flag_pct       numeric(5,2) NOT NULL CHECK (roll_flag_pct >= 0),
  roll_critical_pct   numeric(5,2) NOT NULL,
  effective_from      timestamptz  NOT NULL,              -- áp theo event_time của lần cân (mục 16)
  provisional         boolean      NOT NULL,              -- tạm, chờ khách (C-4)
  reason              text         NOT NULL CHECK (length(reason) BETWEEN 10 AND 500),
  event_time          timestamptz  NOT NULL,
  recorded_at         timestamptz  NOT NULL DEFAULT now(),
  supersedes_id       uuid         NULL,
  correction_reason   text         NULL,
  actor_ref           uuid         NOT NULL,              -- người đặt
  device_id           text         NULL,
  idem_key            text         NOT NULL,
  rule_version        text         NOT NULL,
  prev_hash           bytea        NOT NULL,
  row_hash            bytea        NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (commercial_critical_pct >= commercial_flag_pct),
  CHECK (roll_critical_pct >= roll_flag_pct)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX tolerance_version_idem   ON mkg_measurement.tolerance_version (tenant_id, idem_key);
CREATE INDEX tolerance_version_stream ON mkg_measurement.tolerance_version (tenant_id, stream_key, seq);
CREATE INDEX tolerance_version_id     ON mkg_measurement.tolerance_version (id);
SELECT shared.apply_ledger('mkg_measurement.tolerance_version', 'mkg_measurement.tolerance_version');

CREATE TABLE mkg_measurement.loss_norm_version (
  id                  uuid         NOT NULL,
  tenant_id           uuid         NOT NULL,
  stream_key          text         NOT NULL,              -- 'loss_norm:drying'
  seq                 bigint       NOT NULL,
  -- [{"moisture_from":"0.00","moisture_to":"15.00","loss_pct":"1.00"}, …] — khoảng [from, to), liền nhau (app kiểm)
  bands               jsonb        NOT NULL CHECK (jsonb_typeof(bands) = 'array' AND jsonb_array_length(bands) >= 1),
  default_loss_pct    numeric(5,2) NOT NULL CHECK (default_loss_pct >= 0 AND default_loss_pct <= 100),  -- khi không đo ẩm
  abnormal_alert_pct  numeric(5,2) NOT NULL CHECK (abnormal_alert_pct >= 0 AND abnormal_alert_pct <= 100),
  effective_from      timestamptz  NOT NULL,
  provisional         boolean      NOT NULL,
  reason              text         NOT NULL CHECK (length(reason) BETWEEN 10 AND 500),
  event_time          timestamptz  NOT NULL,
  recorded_at         timestamptz  NOT NULL DEFAULT now(),
  supersedes_id       uuid         NULL,
  correction_reason   text         NULL,
  actor_ref           uuid         NOT NULL,
  device_id           text         NULL,
  idem_key            text         NOT NULL,
  rule_version        text         NOT NULL,
  prev_hash           bytea        NOT NULL,
  row_hash            bytea        NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX loss_norm_version_idem   ON mkg_measurement.loss_norm_version (tenant_id, idem_key);
CREATE INDEX loss_norm_version_stream ON mkg_measurement.loss_norm_version (tenant_id, stream_key, seq);
CREATE INDEX loss_norm_version_id     ON mkg_measurement.loss_norm_version (id);
SELECT shared.apply_ledger('mkg_measurement.loss_norm_version', 'mkg_measurement.loss_norm_version');

CREATE TABLE mkg_measurement.param_decision (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- cùng stream với phiên bản
  seq               bigint      NOT NULL,
  param_kind        text        NOT NULL CHECK (param_kind IN ('tolerance', 'loss_norm')),
  version_id        uuid        NOT NULL,
  version_seq       bigint      NOT NULL CHECK (version_seq >= 1),
  decision          text        NOT NULL CHECK (decision IN ('approved', 'rejected')),
  reason            text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  proposer_ref      uuid        NOT NULL,
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- tolerance_approver
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
CREATE INDEX param_decision_idem   ON mkg_measurement.param_decision (tenant_id, idem_key);
CREATE INDEX param_decision_stream ON mkg_measurement.param_decision (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_measurement.param_decision', 'mkg_measurement.param_decision');

CREATE TABLE mkg_measurement.param_current (
  tenant_id      uuid        NOT NULL,
  param_kind     text        NOT NULL CHECK (param_kind IN ('tolerance', 'loss_norm')),
  version_id     uuid        NOT NULL,
  version_seq    bigint      NOT NULL,
  status         text        NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
  effective_from timestamptz NOT NULL,
  proposed_by    uuid        NOT NULL,
  decided_by     uuid        NULL,
  decided_at     timestamptz NULL,
  updated_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, param_kind, version_id),
  UNIQUE (tenant_id, param_kind, version_seq)
);
CREATE INDEX param_current_effective ON mkg_measurement.param_current (tenant_id, param_kind, status, effective_from);
SELECT shared.apply_tenant_rls('mkg_measurement.param_current');

-- ---------------------------------------------------------------------------
-- 2. Đối soát theo lô (RB-6, mục 13.1)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_measurement.reconciliation (
  id                    uuid          NOT NULL,           -- reconciliation_id
  tenant_id             uuid          NOT NULL,
  stream_key            text          NOT NULL,           -- 'lot:<lot_ref>'
  seq                   bigint        NOT NULL,
  lot_ref               uuid          NOT NULL,
  route                 text          NOT NULL CHECK (route IN ('HUB', 'DIRECT')),
  pricing_basis         text          NOT NULL CHECK (pricing_basis IN ('per_tonne', 'per_bale')),
  hub_in_kg             numeric(14,3) NULL,
  hub_out_kg            numeric(14,3) NULL,
  customer_in_kg        numeric(14,3) NULL,
  hub_in_ticket         uuid          NULL,
  hub_out_ticket        uuid          NULL,
  customer_in_ticket    uuid          NULL,
  hub_in_moisture_pct   numeric(5,2)  NULL,
  -- Hao hụt nội bộ HUB_IN → HUB_OUT: chỉ phần BẤT THƯỜNG sinh cảnh báo.
  internal_loss_kg      numeric(14,3) NULL,
  loss_norm_pct         numeric(5,2)  NULL,
  expected_loss_kg      numeric(14,3) NULL,
  abnormal_loss_kg      numeric(14,3) NULL,
  internal_status       text          NOT NULL CHECK (internal_status IN ('not_applicable', 'normal', 'abnormal', 'gain')),
  -- Chênh thương mại HUB_OUT → CUSTOMER_IN so dung sai.
  commercial_diff_kg    numeric(14,3) NULL,               -- CUSTOMER_IN − HUB_OUT
  commercial_diff_pct   numeric(9,2)  NULL,
  commercial_status     text          NOT NULL CHECK (commercial_status IN ('not_applicable', 'within', 'flag', 'critical')),
  -- Kiểm chéo cuộn ↔ khối lượng (RB-6; chính thức khi hợp đồng tính theo tấn).
  rolls                 bigint        NULL CHECK (rolls IS NULL OR rolls >= 0),
  kg_per_roll_estimate  numeric(14,3) NULL,
  roll_estimate_kg      numeric(14,3) NULL,
  roll_variance_pct     numeric(9,2)  NULL,
  roll_check            text          NOT NULL CHECK (roll_check IN ('pending_rolls', 'within', 'flag', 'critical', 'informational')),
  yield_kg_per_roll     numeric(14,3) NULL,
  yield_aggregated      boolean       NOT NULL,           -- route DIRECT: chỉ số sản lượng gộp (mục 13.1)
  outcome               text          NOT NULL CHECK (outcome IN ('within_tolerance', 'loss_alert', 'dispute_opened', 'direct_aggregate')),
  tolerance_version_id  uuid          NULL,
  tolerance_label       text          NOT NULL,           -- 'tolerance:v<n>' hoặc 'provisional-default'
  loss_norm_version_id  uuid          NULL,
  loss_norm_label       text          NULL,
  dispute_id            uuid          NULL,
  inputs_sha256         text          NOT NULL CHECK (inputs_sha256 ~ '^[0-9a-f]{64}$'),  -- dấu vân đầu vào: chạy lại không đổi → không ghi
  event_time            timestamptz   NOT NULL,
  recorded_at           timestamptz   NOT NULL DEFAULT now(),
  supersedes_id         uuid          NULL,               -- lần đối soát trước của lô
  correction_reason     text          NULL,
  actor_ref             uuid          NOT NULL,
  device_id             text          NULL,
  idem_key              text          NOT NULL,
  rule_version          text          NOT NULL,           -- measurement.reconcile.v1
  prev_hash             bytea         NOT NULL,
  row_hash              bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (route <> 'DIRECT' OR (internal_status = 'not_applicable' AND commercial_status = 'not_applicable'
                               AND yield_aggregated AND outcome = 'direct_aggregate')),
  CHECK (abnormal_loss_kg IS NULL OR abnormal_loss_kg >= 0)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX reconciliation_idem   ON mkg_measurement.reconciliation (tenant_id, idem_key);
CREATE INDEX reconciliation_stream ON mkg_measurement.reconciliation (tenant_id, stream_key, seq);
CREATE INDEX reconciliation_id     ON mkg_measurement.reconciliation (id);
SELECT shared.apply_ledger('mkg_measurement.reconciliation', 'mkg_measurement.reconciliation');

CREATE TABLE mkg_measurement.lot_yield_current (
  tenant_id          uuid          NOT NULL,
  lot_ref            uuid          NOT NULL,
  route              text          NOT NULL,
  basis_point        text          NOT NULL CHECK (basis_point IN ('HUB_IN', 'CUSTOMER_IN')),
  basis_kg           numeric(14,3) NOT NULL,
  rolls              bigint        NULL,
  yield_kg_per_roll  numeric(14,3) NULL,
  aggregated         boolean       NOT NULL,
  reconciliation_id  uuid          NOT NULL,
  updated_at         timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, lot_ref)
);
SELECT shared.apply_tenant_rls('mkg_measurement.lot_yield_current');

-- ---------------------------------------------------------------------------
-- 3. Tranh chấp thương mại
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_measurement.dispute_event (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'dispute:<dispute_id>'
  seq               bigint        NOT NULL,
  dispute_id        uuid          NOT NULL,
  lot_ref           uuid          NOT NULL,
  action            text          NOT NULL CHECK (action IN ('open', 'add_evidence', 'start_review', 'resolve', 'cancel')),
  status_after      text          NOT NULL CHECK (status_after IN ('open', 'under_review', 'resolved', 'cancelled')),
  reconciliation_id uuid          NULL,
  severity          text          NULL CHECK (severity IS NULL OR severity IN ('flag', 'critical')),
  hub_out_kg        numeric(14,3) NULL,
  customer_in_kg    numeric(14,3) NULL,
  diff_kg           numeric(14,3) NULL,
  diff_pct          numeric(9,2)  NULL,
  evidence_refs     jsonb         NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(evidence_refs) = 'array'),
  resolution        text          NULL CHECK (resolution IS NULL OR resolution IN ('customer_weight', 'mkg_weight', 'agreed_weight')),
  agreed_kg         numeric(14,3) NULL CHECK (agreed_kg IS NULL OR agreed_kg >= 0),
  note              text          NULL CHECK (note IS NULL OR length(note) BETWEEN 10 AND 1000),
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
  CHECK (action <> 'open' OR (reconciliation_id IS NOT NULL AND severity IS NOT NULL AND diff_kg IS NOT NULL)),
  CHECK (action <> 'resolve' OR (resolution IS NOT NULL AND note IS NOT NULL)),
  CHECK (action <> 'cancel' OR note IS NOT NULL),
  CHECK (resolution IS DISTINCT FROM 'agreed_weight' OR agreed_kg IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX dispute_event_idem   ON mkg_measurement.dispute_event (tenant_id, idem_key);
CREATE INDEX dispute_event_stream ON mkg_measurement.dispute_event (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_measurement.dispute_event', 'mkg_measurement.dispute_event');

CREATE TABLE mkg_measurement.dispute_current (
  tenant_id         uuid          NOT NULL,
  dispute_id        uuid          NOT NULL,
  lot_ref           uuid          NOT NULL,
  status            text          NOT NULL CHECK (status IN ('open', 'under_review', 'resolved', 'cancelled')),
  severity          text          NOT NULL,
  reconciliation_id uuid          NOT NULL,
  hub_out_kg        numeric(14,3) NULL,
  customer_in_kg    numeric(14,3) NULL,
  diff_kg           numeric(14,3) NOT NULL,
  diff_pct          numeric(9,2)  NULL,
  evidence_refs     jsonb         NOT NULL,
  resolution        text          NULL,
  agreed_kg         numeric(14,3) NULL,
  opened_at         timestamptz   NOT NULL,
  opened_by         uuid          NOT NULL,
  last_seq          bigint        NOT NULL,
  updated_at        timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, dispute_id)
);
-- Một lô có tối đa một hồ sơ đang mở.
CREATE UNIQUE INDEX dispute_current_one_active ON mkg_measurement.dispute_current (tenant_id, lot_ref) WHERE status IN ('open', 'under_review');
CREATE INDEX dispute_current_status ON mkg_measurement.dispute_current (tenant_id, status, dispute_id);
SELECT shared.apply_tenant_rls('mkg_measurement.dispute_current');

-- ---------------------------------------------------------------------------
-- 4. Nhập số cân của khách (C-5 — tạm, chờ khách)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_measurement.customer_weight_import (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'import:<id>'
  seq               bigint      NOT NULL,
  channel           text        NOT NULL CHECK (channel IN ('file', 'api', 'manual')),
  customer_ref      uuid        NULL,
  evidence_ref      uuid        NULL,                     -- tệp gốc / ảnh phiếu
  file_sha256       text        NULL CHECK (file_sha256 IS NULL OR file_sha256 ~ '^[0-9a-f]{64}$'),
  -- [{lot_ref, mass_kg, moisture_pct?, weighed_at, customer_ticket_no?}] — đúng dữ liệu đã nhận
  rows              jsonb       NOT NULL CHECK (jsonb_typeof(rows) = 'array' AND jsonb_array_length(rows) >= 1),
  ticket_ids        jsonb       NOT NULL CHECK (jsonb_typeof(ticket_ids) = 'array'),
  provisional       boolean     NOT NULL,
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
  CHECK (channel = 'api' OR evidence_ref IS NOT NULL),
  CHECK (channel <> 'manual' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX customer_weight_import_idem   ON mkg_measurement.customer_weight_import (tenant_id, idem_key);
CREATE INDEX customer_weight_import_stream ON mkg_measurement.customer_weight_import (tenant_id, stream_key, seq);
CREATE INDEX customer_weight_import_id     ON mkg_measurement.customer_weight_import (id);
SELECT shared.apply_ledger('mkg_measurement.customer_weight_import', 'mkg_measurement.customer_weight_import');

-- ---------------------------------------------------------------------------
-- 5. Sổ xuất van công bố (mẫu mkg_hub.valve_export)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_measurement.valve_export (
  id                uuid        NOT NULL,                 -- = event_id trên shared.outbox
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'valve:<contract>:<subject_ref>'
  seq               bigint      NOT NULL,
  contract          text        NOT NULL CHECK (contract IN ('mrv_collection_evidence')),
  contract_version  integer     NOT NULL CHECK (contract_version >= 1),
  event_type        text        NOT NULL CHECK (event_type ~ '^mkg\.[a-z_]+\.[a-z_]+\.[a-z_]+$'),
  schema_version    integer     NOT NULL CHECK (schema_version >= 1),
  subject_ref       uuid        NOT NULL,                 -- evidence_ref
  source_ref        uuid        NOT NULL,                 -- lot_ref (ở lại vùng mkg, KHÔNG có trong payload)
  plot_ref          uuid        NOT NULL,
  revision          integer     NOT NULL CHECK (revision >= 1),
  payload           jsonb       NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
  payload_sha256    bytea       NOT NULL CHECK (length(payload_sha256) = 32),
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
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX valve_export_idem   ON mkg_measurement.valve_export (tenant_id, idem_key);
CREATE INDEX valve_export_stream ON mkg_measurement.valve_export (tenant_id, stream_key, seq);
CREATE INDEX valve_export_source ON mkg_measurement.valve_export (tenant_id, source_ref, plot_ref, revision);
SELECT shared.apply_ledger('mkg_measurement.valve_export', 'mkg_measurement.valve_export');

-- ---------------------------------------------------------------------------
-- 6. Quyền (chỉ vai module; ingest_mkg KHÔNG có quyền nào ở đây)
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON mkg_measurement.tolerance_version, mkg_measurement.loss_norm_version, mkg_measurement.param_decision,
                        mkg_measurement.reconciliation, mkg_measurement.dispute_event, mkg_measurement.customer_weight_import,
                        mkg_measurement.valve_export TO measurement_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_measurement.tolerance_version, mkg_measurement.loss_norm_version,
                                   mkg_measurement.param_decision, mkg_measurement.reconciliation,
                                   mkg_measurement.dispute_event, mkg_measurement.customer_weight_import,
                                   mkg_measurement.valve_export FROM measurement_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_measurement.param_current, mkg_measurement.lot_yield_current,
                                        mkg_measurement.dispute_current TO measurement_app;
