-- mkg_fieldops G3: lệnh gom, ca tổ gom, đếm cuộn theo công đoạn, xác nhận của
-- HTX, đối soát chiều mua, học kg/cuộn theo HTX, sổ van (docs/design/mkg.md §5.1,
-- docs/dev/g3-conventions.md §1.1, §2).
--
--   roll_count             SỔ đã có (mkg 000001). G3 THÊM cột nullable: lệnh gom, công đoạn,
--                          khối lượng rơm rời, lý do chênh công đoạn (FM-04). Cột nullable để
--                          bản ghi mẫu G0 và kiểm thử sổ dùng chung vẫn hợp lệ; app G3 luôn điền.
--                          stream 'lot:<order_id>' — lô gom = lệnh gom.
--   collection_order_event SỔ lệnh gom (stream 'order:<id>'): planned/approved/assigned/closed/
--                          cancelled; người duyệt ≠ người lập (R-56, CHECK DB).
--   crew_shift             SỔ ca tổ gom (stream 'crew:<crew_ref>'): mở/đóng ca trên một lệnh gom.
--   htx_confirmation       SỔ xác nhận của HTX (stream 'order:<id>'): confirmed/disputed kèm số
--                          cuộn và lý do. G3: nhân viên MKG ghi thay; G3-2: HTX xác nhận qua nền tảng.
--   valve_export           SỔ xuất của van collection_schedule / htx_collection_statement.
--
--   collection_order_current       bảng chiếu lệnh gom (+ sync_seq cho pull của app hiện trường MKG).
--   roll_total_current             bảng chiếu số cuộn hiệu lực theo (lệnh gom, thửa, công đoạn).
--   buyside_reconciliation_current đối soát chiều mua MKG ↔ HTX (RB-6): pending/match/mismatch.
--   weighed_loading_current        bảng đệm khối lượng cân của lô gom (lấy qua port từ
--                                  mkg/measurement) để học kg/cuộn — yield_kg_per_roll KHÔNG BAO GIỜ qua van.
--
-- Không có dữ liệu cá nhân (R-04): HTX, thửa, tổ gom, người ghi chỉ là *_ref.
-- Khối lượng kg numeric(14,3) (R-10).

-- ---------------------------------------------------------------------------
-- 1. Mở rộng sổ roll_count (cộng thêm, không đổi cột cũ)
-- ---------------------------------------------------------------------------
ALTER TABLE mkg_fieldops.roll_count
  ADD COLUMN order_id           uuid          NULL,
  ADD COLUMN stage              text          NULL,
  ADD COLUMN loose_kg           numeric(14,3) NULL,
  ADD COLUMN discrepancy_reason text          NULL;

ALTER TABLE mkg_fieldops.roll_count
  ADD CONSTRAINT roll_count_stage_chk CHECK (stage IS NULL OR stage IN ('bale', 'gather', 'boat_load')),
  ADD CONSTRAINT roll_count_order_stage_chk CHECK ((order_id IS NULL) = (stage IS NULL)),
  -- Bắt buộc có số cuộn; chỉ ghi khối lượng rơm rời khi KHÔNG có cuộn (proto FM-09).
  ADD CONSTRAINT roll_count_loose_chk CHECK (loose_kg IS NULL OR (rolls = 0 AND loose_kg > 0)),
  ADD CONSTRAINT roll_count_nonempty_chk CHECK (stage IS NULL OR rolls > 0 OR loose_kg IS NOT NULL OR supersedes_id IS NOT NULL),
  ADD CONSTRAINT roll_count_discrepancy_chk CHECK (discrepancy_reason IS NULL OR length(discrepancy_reason) BETWEEN 10 AND 500),
  ADD CONSTRAINT roll_count_correction_len_chk CHECK (correction_reason IS NULL OR length(correction_reason) BETWEEN 10 AND 500);

CREATE INDEX roll_count_order      ON mkg_fieldops.roll_count (tenant_id, order_id, plot_ref, stage) WHERE order_id IS NOT NULL;
CREATE INDEX roll_count_id         ON mkg_fieldops.roll_count (id);
CREATE INDEX roll_count_supersedes ON mkg_fieldops.roll_count (tenant_id, supersedes_id) WHERE supersedes_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 2. Lệnh gom
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_fieldops.collection_order_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'order:<order_id>'
  seq               bigint      NOT NULL,
  order_id          uuid        NOT NULL,
  kind              text        NOT NULL CHECK (kind IN ('planned', 'approved', 'assigned', 'closed', 'cancelled')),
  revision          integer     NOT NULL CHECK (revision >= 1),      -- phiên bản kế hoạch (planned lần n)
  htx_ref           uuid        NOT NULL,
  commune_code      text        NOT NULL CHECK (commune_code ~ '^[0-9]{5}$'),
  window_from       date        NOT NULL,
  window_to         date        NOT NULL,
  plot_refs         uuid[]      NOT NULL CHECK (cardinality(plot_refs) BETWEEN 1 AND 200),
  crew_ref          uuid        NULL,
  reason            text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  creator_ref       uuid        NOT NULL,                 -- người lập kế hoạch (revision hiện hành), chép để kiểm R-56
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
  CHECK (window_to >= window_from),
  CHECK (kind <> 'approved' OR actor_ref <> creator_ref),  -- R-56 ở tầng DB (lớp thứ hai sau app/)
  CHECK (kind <> 'assigned' OR crew_ref IS NOT NULL),
  CHECK (kind <> 'cancelled' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX collection_order_event_idem   ON mkg_fieldops.collection_order_event (tenant_id, idem_key);
CREATE INDEX collection_order_event_stream ON mkg_fieldops.collection_order_event (tenant_id, stream_key, seq);
CREATE INDEX collection_order_event_order  ON mkg_fieldops.collection_order_event (tenant_id, order_id, seq);
SELECT shared.apply_ledger('mkg_fieldops.collection_order_event', 'mkg_fieldops.collection_order_event');

CREATE SEQUENCE mkg_fieldops.sync_seq;

CREATE TABLE mkg_fieldops.collection_order_current (
  tenant_id     uuid        NOT NULL,
  order_id      uuid        NOT NULL,
  status        text        NOT NULL CHECK (status IN ('planned', 'approved', 'assigned', 'closed', 'cancelled')),
  revision      integer     NOT NULL,
  htx_ref       uuid        NOT NULL,
  commune_code  text        NOT NULL,
  window_from   date        NOT NULL,
  window_to     date        NOT NULL,
  plot_refs     uuid[]      NOT NULL,
  crew_ref      uuid        NULL,
  reason        text        NULL,
  creator_ref   uuid        NOT NULL,
  approver_ref  uuid        NULL,
  approved_at   timestamptz NULL,
  published     boolean     NOT NULL DEFAULT false,     -- đã từng xuất collection_schedule
  last_seq      bigint      NOT NULL,
  sync_seq      bigint      NOT NULL DEFAULT nextval('mkg_fieldops.sync_seq'),
  created_at    timestamptz NOT NULL,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, order_id),
  CHECK (approver_ref IS NULL OR approver_ref <> creator_ref)
);
CREATE INDEX collection_order_current_status ON mkg_fieldops.collection_order_current (tenant_id, status, order_id);
CREATE INDEX collection_order_current_sync   ON mkg_fieldops.collection_order_current (tenant_id, sync_seq);
SELECT shared.apply_tenant_rls('mkg_fieldops.collection_order_current');

-- ---------------------------------------------------------------------------
-- 3. Ca tổ gom
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_fieldops.crew_shift (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'crew:<crew_ref>'
  seq               bigint      NOT NULL,
  crew_ref          uuid        NOT NULL,
  order_id          uuid        NOT NULL,
  kind              text        NOT NULL CHECK (kind IN ('opened', 'closed')),
  headcount         integer     NULL CHECK (headcount IS NULL OR headcount BETWEEN 1 AND 200),
  equipment_refs    text[]      NOT NULL DEFAULT '{}' CHECK (cardinality(equipment_refs) <= 50),
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
  CHECK (kind <> 'opened' OR headcount IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX crew_shift_idem   ON mkg_fieldops.crew_shift (tenant_id, idem_key);
CREATE INDEX crew_shift_stream ON mkg_fieldops.crew_shift (tenant_id, stream_key, seq);
CREATE INDEX crew_shift_order  ON mkg_fieldops.crew_shift (tenant_id, order_id, event_time);
SELECT shared.apply_ledger('mkg_fieldops.crew_shift', 'mkg_fieldops.crew_shift');

-- ---------------------------------------------------------------------------
-- 4. Số cuộn hiệu lực theo công đoạn (bảng chiếu của roll_count)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_fieldops.roll_total_current (
  tenant_id          uuid          NOT NULL,
  order_id           uuid          NOT NULL,
  plot_ref           uuid          NOT NULL,
  stage              text          NOT NULL CHECK (stage IN ('bale', 'gather', 'boat_load')),
  htx_ref            uuid          NOT NULL,
  rolls              integer       NOT NULL CHECK (rolls >= 0),
  loose_kg           numeric(14,3) NULL,
  roll_count_id      uuid          NOT NULL,               -- bản ghi hiệu lực (đã tính bản bù)
  discrepancy_reason text          NULL,
  event_time         timestamptz   NOT NULL,
  last_seq           bigint        NOT NULL,
  updated_at         timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, order_id, plot_ref, stage)
);
SELECT shared.apply_tenant_rls('mkg_fieldops.roll_total_current');

-- ---------------------------------------------------------------------------
-- 5. Xác nhận của HTX và đối soát chiều mua (RB-6)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_fieldops.htx_confirmation (
  id                 uuid        NOT NULL,
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,                -- 'order:<order_id>'
  seq                bigint      NOT NULL,
  order_id           uuid        NOT NULL,
  plot_ref           uuid        NOT NULL,
  htx_ref            uuid        NOT NULL,
  statement_ref      uuid        NOT NULL,
  statement_revision integer     NOT NULL CHECK (statement_revision >= 1),
  roll_count_id      uuid        NOT NULL,                -- số MKG được xác nhận / tranh chấp
  mkg_rolls          integer     NOT NULL CHECK (mkg_rolls >= 0),
  outcome            text        NOT NULL CHECK (outcome IN ('confirmed', 'disputed')),
  htx_rolls          integer     NOT NULL CHECK (htx_rolls >= 0),
  reason             text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  channel            text        NOT NULL CHECK (channel IN ('mkg_on_behalf', 'platform')),
  evidence_ref       uuid        NULL,
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
  CHECK (outcome <> 'confirmed' OR htx_rolls = mkg_rolls),
  CHECK (outcome <> 'disputed' OR (reason IS NOT NULL AND htx_rolls <> mkg_rolls))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX htx_confirmation_idem   ON mkg_fieldops.htx_confirmation (tenant_id, idem_key);
CREATE INDEX htx_confirmation_stream ON mkg_fieldops.htx_confirmation (tenant_id, stream_key, seq);
CREATE INDEX htx_confirmation_plot   ON mkg_fieldops.htx_confirmation (tenant_id, order_id, plot_ref, seq);
SELECT shared.apply_ledger('mkg_fieldops.htx_confirmation', 'mkg_fieldops.htx_confirmation');

CREATE TABLE mkg_fieldops.buyside_reconciliation_current (
  tenant_id          uuid        NOT NULL,
  order_id           uuid        NOT NULL,
  plot_ref           uuid        NOT NULL,
  htx_ref            uuid        NOT NULL,
  statement_ref      uuid        NOT NULL,
  statement_revision integer     NOT NULL CHECK (statement_revision >= 1),
  mkg_rolls          integer     NOT NULL CHECK (mkg_rolls >= 0),
  mkg_roll_count_id  uuid        NOT NULL,
  htx_rolls          integer     NULL,
  outcome            text        NULL CHECK (outcome IS NULL OR outcome IN ('confirmed', 'disputed')),
  confirmation_id    uuid        NULL,
  confirmed_revision integer     NULL,
  status             text        NOT NULL CHECK (status IN ('pending', 'match', 'mismatch')),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, order_id, plot_ref),
  CHECK ((confirmation_id IS NULL) = (status = 'pending'))
);
CREATE INDEX buyside_reconciliation_status ON mkg_fieldops.buyside_reconciliation_current (tenant_id, status, order_id, plot_ref);
SELECT shared.apply_tenant_rls('mkg_fieldops.buyside_reconciliation_current');

-- ---------------------------------------------------------------------------
-- 6. Học kg/cuộn: khối lượng cân của lô gom (bảng đệm, dựng lại được từ measurement)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_fieldops.weighed_loading_current (
  tenant_id   uuid          NOT NULL,
  order_id    uuid          NOT NULL,                     -- lô gom
  htx_ref     uuid          NOT NULL,
  rolls       integer       NOT NULL CHECK (rolls > 0),   -- Σ cuộn xếp ghe/xe của lô lúc cân
  weighed_kg  numeric(14,3) NOT NULL CHECK (weighed_kg > 0),
  weighed_at  timestamptz   NOT NULL,
  updated_at  timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, order_id)
);
CREATE INDEX weighed_loading_htx ON mkg_fieldops.weighed_loading_current (tenant_id, htx_ref, weighed_at DESC);
CREATE INDEX weighed_loading_at  ON mkg_fieldops.weighed_loading_current (tenant_id, weighed_at DESC);
SELECT shared.apply_tenant_rls('mkg_fieldops.weighed_loading_current');

-- ---------------------------------------------------------------------------
-- 7. Sổ van công bố (cùng mẫu mkg_hub.valve_export)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_fieldops.valve_export (
  id                uuid        NOT NULL,                 -- = event_id trên shared.outbox
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'valve:<contract>:<subject_ref>'
  seq               bigint      NOT NULL,
  contract          text        NOT NULL CHECK (contract IN ('collection_schedule', 'htx_collection_statement')),
  contract_version  integer     NOT NULL CHECK (contract_version >= 1),
  event_type        text        NOT NULL CHECK (event_type ~ '^mkg\.[a-z_]+\.[a-z_]+\.[a-z_]+$'),
  schema_version    integer     NOT NULL CHECK (schema_version >= 1),
  subject_ref       uuid        NOT NULL,                 -- schedule_ref (= order_id) / statement_ref
  order_id          uuid        NOT NULL,
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
CREATE INDEX valve_export_idem    ON mkg_fieldops.valve_export (tenant_id, idem_key);
CREATE INDEX valve_export_stream  ON mkg_fieldops.valve_export (tenant_id, stream_key, seq);
CREATE INDEX valve_export_order   ON mkg_fieldops.valve_export (tenant_id, order_id, event_time);
CREATE INDEX valve_export_id      ON mkg_fieldops.valve_export (id);
SELECT shared.apply_ledger('mkg_fieldops.valve_export', 'mkg_fieldops.valve_export');

-- ---------------------------------------------------------------------------
-- Quyền
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON mkg_fieldops.collection_order_event, mkg_fieldops.crew_shift,
                        mkg_fieldops.htx_confirmation, mkg_fieldops.valve_export TO fieldops_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_fieldops.collection_order_event, mkg_fieldops.crew_shift,
                                   mkg_fieldops.htx_confirmation, mkg_fieldops.valve_export FROM fieldops_app;
-- Bảng chiếu: được UPDATE (R-21); DELETE chỉ để dựng lại (R-22).
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_fieldops.collection_order_current, mkg_fieldops.roll_total_current,
                                        mkg_fieldops.buyside_reconciliation_current,
                                        mkg_fieldops.weighed_loading_current TO fieldops_app;
GRANT USAGE ON SEQUENCE mkg_fieldops.sync_seq TO fieldops_app;
