-- mkg_costing (1/2): sổ giá thành — event sourcing MỨC B, SỔ LÀ NGUỒN (ADR-0003; tài liệu
-- kiến trúc mục 16; docs/design/mkg.md §5.6; docs/dev/g4-conventions.md §2).
--
--   param_version / param_decision  SỔ tham số có phiên bản (cost_accountant đặt ≠ chief_accountant duyệt, R-56)
--                                  stream 'param:<khóa>'; áp theo THỜI ĐIỂM SỰ VIỆC (effective_from ≤ event_time)
--                                  và, khi tính lại, theo quyết định có recorded_at ≤ as_of (R-13)
--   input_event        SỔ đầu vào từ sự kiện nội bộ (phiếu cân, đếm cuộn). stream 'lot:<lot_ref>'
--   cost_entry         SỔ bút toán chi phí / doanh thu theo lô. stream 'lot:<lot_ref>'. Mỗi dòng mang
--                      delta_amount = amount − amount của dòng bị thay (supersedes) → tổng theo kỳ là
--                      tổng delta, điều chỉnh sau đóng kỳ nằm ở kỳ đang mở
--   conversion_result  SỔ quy đổi cuộn → tấn (yield_kg_per_roll = mass_kg(HUB_IN) / rolls, mục 13.3) và
--                      ẩm độ (C-4: mặc định không quy đổi). stream 'order:<collection_order_ref>'
--   allocation_result  SỔ kết quả phân bổ (chi phí sơ chế hub, cước chuyến, doanh thu giao nhận, quy kết
--                      hao hụt về lô nguồn). stream 'period:<period_id>'. Tính lại = dòng MỚI supersedes_id
--   *_current          bảng chiếu cập nhật cùng giao dịch (R-21), dựng lại từ sổ
--                      (POST /v1/costing/projections/rebuild, R-22)
--
-- Mọi kết quả ghim rule_version + param_pins. Tiền numeric(18,2), đơn giá numeric(18,4), khối lượng
-- numeric(14,3) (R-10). Không dữ liệu cá nhân: HTX, khách, lô, chuyến là *_ref UUID (R-06).

-- ---------------------------------------------------------------------------
-- 1. Tham số có phiên bản
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_costing.param_version (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'param:<param_key>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  param_key         text        NOT NULL CHECK (param_key IN (
                      'costing.allocation.key', 'costing.moisture.base_pct', 'costing.loss.expected_pct',
                      'costing.loss.hub_processing_pct', 'costing.rate.hub_processing_vnd_per_t',
                      'costing.settlement.payment_term_days', 'costing.carbon.retained_pct',
                      'costing.carbon.split.farmers_pct', 'costing.carbon.split.authority_pct',
                      'costing.carbon.split.verifier_pct')),
  value             text        NOT NULL CHECK (length(value) BETWEEN 1 AND 40),
  reason            text        NOT NULL CHECK (length(reason) BETWEEN 10 AND 500),
  effective_from    timestamptz NOT NULL,
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người đặt (cost_accountant)
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL CHECK (rule_version = 'costing.params.v1'),
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX param_version_idem   ON mkg_costing.param_version (tenant_id, idem_key);
CREATE INDEX param_version_stream ON mkg_costing.param_version (tenant_id, stream_key, seq);
CREATE INDEX param_version_id     ON mkg_costing.param_version (id);
SELECT shared.apply_ledger('mkg_costing.param_version', 'mkg_costing.param_version');

CREATE TABLE mkg_costing.param_decision (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'param:<param_key>'
  seq               bigint      NOT NULL,
  param_key         text        NOT NULL,
  version_id        uuid        NOT NULL,
  version_seq       bigint      NOT NULL CHECK (version_seq >= 1),
  decision          text        NOT NULL CHECK (decision IN ('approved', 'rejected')),
  reason            text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  proposer_ref      uuid        NOT NULL,
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người duyệt (chief_accountant)
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (actor_ref <> proposer_ref),                     -- R-56 lớp DB
  CHECK (decision = 'approved' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX param_decision_idem    ON mkg_costing.param_decision (tenant_id, idem_key);
CREATE INDEX param_decision_version ON mkg_costing.param_decision (tenant_id, version_id);
SELECT shared.apply_ledger('mkg_costing.param_decision', 'mkg_costing.param_decision');

-- ---------------------------------------------------------------------------
-- 2. Đầu vào từ sự kiện nội bộ (phiếu cân, đếm cuộn)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_costing.input_event (
  id                   uuid          NOT NULL,
  tenant_id            uuid          NOT NULL,
  stream_key           text          NOT NULL,            -- 'lot:<lot_ref>'
  seq                  bigint        NOT NULL,
  lot_ref              uuid          NOT NULL,            -- lô cân (weigh) / lệnh gom (rolls)
  kind                 text          NOT NULL CHECK (kind IN ('weigh', 'rolls')),
  source_ref           uuid          NOT NULL,            -- ticket_id (measurement) / roll_count_id (fieldops)
  collection_order_ref uuid          NOT NULL,            -- lệnh gom (weigh: liên kết tường minh hoặc quy ước đồng nhất)
  htx_ref              uuid          NULL,
  point                text          NULL CHECK (point IS NULL OR point IN ('HUB_IN', 'HUB_OUT', 'CUSTOMER_IN')),
  route                text          NULL CHECK (route IS NULL OR route IN ('HUB', 'DIRECT')),
  mass_kg              numeric(14,3) NULL CHECK (mass_kg IS NULL OR mass_kg >= 0),
  moisture_pct         numeric(5,2)  NULL CHECK (moisture_pct IS NULL OR (moisture_pct >= 0 AND moisture_pct <= 100)),
  rolls                integer       NULL CHECK (rolls IS NULL OR rolls >= 0),
  plot_ref             uuid          NULL,
  stage                text          NULL CHECK (stage IS NULL OR stage IN ('bale', 'gather', 'boat_load')),
  event_time           timestamptz   NOT NULL,
  recorded_at          timestamptz   NOT NULL DEFAULT now(),
  supersedes_id        uuid          NULL,                -- đầu vào bị thay (phiếu bù / đếm bù)
  correction_reason    text          NULL,
  actor_ref            uuid          NOT NULL,
  device_id            text          NULL,
  idem_key             text          NOT NULL,
  rule_version         text          NOT NULL,
  prev_hash            bytea         NOT NULL,
  row_hash             bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (kind <> 'weigh' OR (mass_kg IS NOT NULL AND point IS NOT NULL AND route IS NOT NULL AND rolls IS NULL)),
  CHECK (kind <> 'rolls' OR (rolls IS NOT NULL AND mass_kg IS NULL AND point IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX input_event_idem   ON mkg_costing.input_event (tenant_id, idem_key);
CREATE INDEX input_event_stream ON mkg_costing.input_event (tenant_id, stream_key, seq);
CREATE INDEX input_event_source ON mkg_costing.input_event (tenant_id, source_ref);
CREATE INDEX input_event_order  ON mkg_costing.input_event (tenant_id, collection_order_ref);
SELECT shared.apply_ledger('mkg_costing.input_event', 'mkg_costing.input_event');

-- Bảng chiếu theo lệnh gom: số cuộn hiệu lực (mỗi thửa lấy công đoạn cuối), khối lượng cân
-- (Σ HUB_IN của lô cân liên kết; tuyến DIRECT: Σ CUSTOMER_IN).
CREATE TABLE mkg_costing.collection_current (
  tenant_id            uuid          NOT NULL,
  collection_order_ref uuid          NOT NULL,
  htx_ref              uuid          NULL,
  rolls                integer       NULL,
  weighed_kg           numeric(14,3) NULL,
  weigh_point          text          NULL,
  weighed_lots         integer       NOT NULL DEFAULT 0,
  last_event_time      timestamptz   NOT NULL,
  updated_at           timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, collection_order_ref)
);
SELECT shared.apply_tenant_rls('mkg_costing.collection_current');

-- ---------------------------------------------------------------------------
-- 3. Bút toán chi phí / doanh thu theo lô
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_costing.cost_entry (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'lot:<lot_ref>'
  seq               bigint        NOT NULL,
  lot_ref           uuid          NOT NULL,
  period_code       text          NOT NULL CHECK (period_code ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),  -- kỳ kế toán nhận bút toán
  late_from         text          NULL CHECK (late_from IS NULL OR late_from ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'), -- kỳ của sự việc khi kỳ đó đã đóng
  category          text          NOT NULL CHECK (category IN ('straw_purchase', 'collection_labour', 'consumables',
                      'transport_road', 'transport_waterway', 'hub_processing', 'storage', 'handling', 'loss',
                      'sales_revenue', 'carbon_revenue', 'carbon_share')),
  amount            numeric(18,2) NOT NULL,
  delta_amount      numeric(18,2) NOT NULL,
  mass_kg           numeric(14,3) NULL,
  delta_mass_kg     numeric(14,3) NULL,
  dimension_kind    text          NOT NULL CHECK (dimension_kind IN ('facility', 'htx', 'customer', 'mode', 'none')),
  dimension_ref     uuid          NULL,
  source_kind       text          NOT NULL CHECK (source_kind IN ('hub_processing', 'trip', 'delivery', 'manual', 'adjusting')),
  source_ref        uuid          NULL,
  allocation_ref    uuid          NULL,                   -- allocation_result sinh ra dòng này
  entry_kind        text          NOT NULL CHECK (entry_kind IN ('normal', 'adjusting')),
  note              text          NULL CHECK (note IS NULL OR length(note) <= 500),
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK ((dimension_kind = 'none') = (dimension_ref IS NULL)),
  CHECK (entry_kind <> 'adjusting' OR (supersedes_id IS NOT NULL AND source_kind = 'adjusting')),
  CHECK (supersedes_id IS NOT NULL OR delta_amount = amount),
  CHECK ((mass_kg IS NULL) = (delta_mass_kg IS NULL) OR supersedes_id IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cost_entry_idem       ON mkg_costing.cost_entry (tenant_id, idem_key);
CREATE INDEX cost_entry_stream     ON mkg_costing.cost_entry (tenant_id, stream_key, seq);
CREATE INDEX cost_entry_id         ON mkg_costing.cost_entry (id);
CREATE INDEX cost_entry_period     ON mkg_costing.cost_entry (tenant_id, period_code);
CREATE INDEX cost_entry_alloc      ON mkg_costing.cost_entry (tenant_id, allocation_ref) WHERE allocation_ref IS NOT NULL;
CREATE INDEX cost_entry_supersedes ON mkg_costing.cost_entry (tenant_id, supersedes_id) WHERE supersedes_id IS NOT NULL;
SELECT shared.apply_ledger('mkg_costing.cost_entry', 'mkg_costing.cost_entry');

-- Bảng chiếu giá thành theo lô × nhóm (Σ delta của mọi bút toán).
CREATE TABLE mkg_costing.lot_cost_current (
  tenant_id  uuid          NOT NULL,
  lot_ref    uuid          NOT NULL,
  category   text          NOT NULL,
  amount     numeric(18,2) NOT NULL,
  mass_kg    numeric(14,3) NULL,
  entries    integer       NOT NULL,
  updated_at timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, lot_ref, category)
);
SELECT shared.apply_tenant_rls('mkg_costing.lot_cost_current');

-- ---------------------------------------------------------------------------
-- 4. Quy đổi cuộn → tấn, ẩm độ
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_costing.conversion_result (
  id                   uuid          NOT NULL,
  tenant_id            uuid          NOT NULL,
  stream_key           text          NOT NULL,            -- 'order:<collection_order_ref>'
  seq                  bigint        NOT NULL,
  collection_order_ref uuid          NOT NULL,
  rolls                integer       NOT NULL CHECK (rolls > 0),
  weighed_kg           numeric(14,3) NOT NULL CHECK (weighed_kg >= 0),
  weigh_point          text          NOT NULL CHECK (weigh_point IN ('HUB_IN', 'CUSTOMER_IN')),
  yield_kg_per_roll    numeric(14,3) NOT NULL,
  moisture_rule        text          NOT NULL,            -- 'none' (C-4: chưa có công thức quy đổi — tạm, chờ khách)
  converted_kg         numeric(14,3) NOT NULL,
  inputs_sha256        text          NOT NULL CHECK (inputs_sha256 ~ '^[0-9a-f]{64}$'),
  param_pins           jsonb         NOT NULL CHECK (jsonb_typeof(param_pins) = 'object'),
  event_time           timestamptz   NOT NULL,
  recorded_at          timestamptz   NOT NULL DEFAULT now(),
  supersedes_id        uuid          NULL,
  correction_reason    text          NULL,
  actor_ref            uuid          NOT NULL,
  device_id            text          NULL,
  idem_key             text          NOT NULL,
  rule_version         text          NOT NULL,
  prev_hash            bytea         NOT NULL,
  row_hash             bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX conversion_result_idem   ON mkg_costing.conversion_result (tenant_id, idem_key);
CREATE INDEX conversion_result_stream ON mkg_costing.conversion_result (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_costing.conversion_result', 'mkg_costing.conversion_result');

CREATE TABLE mkg_costing.conversion_current (
  tenant_id            uuid          NOT NULL,
  collection_order_ref uuid          NOT NULL,
  result_id            uuid          NOT NULL,
  rolls                integer       NOT NULL,
  weighed_kg           numeric(14,3) NOT NULL,
  yield_kg_per_roll    numeric(14,3) NOT NULL,
  converted_kg         numeric(14,3) NOT NULL,
  inputs_sha256        text          NOT NULL,
  rule_version         text          NOT NULL,
  updated_at           timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, collection_order_ref)
);
SELECT shared.apply_tenant_rls('mkg_costing.conversion_current');

-- ---------------------------------------------------------------------------
-- 5. Kết quả phân bổ
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_costing.allocation_result (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'period:<period_id>'
  seq               bigint        NOT NULL,
  period_ref        uuid          NOT NULL,
  kind              text          NOT NULL CHECK (kind IN ('hub_processing', 'trip_cost', 'delivery_revenue', 'loss_attribution')),
  source_ref        uuid          NOT NULL,               -- ticket / trip / delivery / reconciliation
  source_event_time timestamptz   NOT NULL,               -- thời điểm sự việc: tham số áp theo mốc này
  basis             text          NOT NULL,               -- khóa phân bổ đã dùng (mass | equal | self | origins)
  total_amount      numeric(18,2) NULL,
  total_kg          numeric(14,3) NULL,
  lines             jsonb         NOT NULL CHECK (jsonb_typeof(lines) = 'array'),
  inputs            jsonb         NOT NULL CHECK (jsonb_typeof(inputs) = 'object'),
  inputs_sha256     text          NOT NULL CHECK (inputs_sha256 ~ '^[0-9a-f]{64}$'),
  result_sha256     text          NOT NULL CHECK (result_sha256 ~ '^[0-9a-f]{64}$'),
  param_pins        jsonb         NOT NULL CHECK (jsonb_typeof(param_pins) = 'object'),
  params_as_of      timestamptz   NOT NULL,               -- quyết định tham số có recorded_at ≤ mốc này (R-13)
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,                   -- kết quả cũ bị tính lại (không bao giờ sửa)
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,
  device_id         text          NULL,
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (kind = 'loss_attribution' OR total_amount IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX allocation_result_idem   ON mkg_costing.allocation_result (tenant_id, idem_key);
CREATE INDEX allocation_result_stream ON mkg_costing.allocation_result (tenant_id, stream_key, seq);
CREATE INDEX allocation_result_id     ON mkg_costing.allocation_result (id);
CREATE INDEX allocation_result_source ON mkg_costing.allocation_result (tenant_id, source_ref);
SELECT shared.apply_ledger('mkg_costing.allocation_result', 'mkg_costing.allocation_result');

CREATE TABLE mkg_costing.allocation_current (
  tenant_id      uuid          NOT NULL,
  allocation_id  uuid          NOT NULL,
  kind           text          NOT NULL,
  source_ref     uuid          NOT NULL,
  period_ref     uuid          NOT NULL,
  total_amount   numeric(18,2) NULL,
  total_kg       numeric(14,3) NULL,
  result_sha256  text          NOT NULL,
  rule_version   text          NOT NULL,
  superseded_by  uuid          NULL,
  updated_at     timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, allocation_id)
);
-- Mỗi nguồn (phiếu, chuyến, giao nhận, đối soát) tối đa một kết quả hiệu lực mỗi loại.
CREATE UNIQUE INDEX allocation_current_live ON mkg_costing.allocation_current (tenant_id, kind, source_ref) WHERE superseded_by IS NULL;
SELECT shared.apply_tenant_rls('mkg_costing.allocation_current');

-- ---------------------------------------------------------------------------
-- 6. Quyền (vai module costing_app)
-- ---------------------------------------------------------------------------
-- Sổ: chỉ SELECT + INSERT. Bảng chiếu: UPDATE (R-21), DELETE chỉ để dựng lại (R-22).
GRANT SELECT, INSERT ON mkg_costing.param_version, mkg_costing.param_decision, mkg_costing.input_event,
                        mkg_costing.cost_entry, mkg_costing.conversion_result, mkg_costing.allocation_result TO costing_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_costing.param_version, mkg_costing.param_decision, mkg_costing.input_event,
                                   mkg_costing.cost_entry, mkg_costing.conversion_result, mkg_costing.allocation_result FROM costing_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_costing.collection_current, mkg_costing.lot_cost_current,
                                        mkg_costing.conversion_current, mkg_costing.allocation_current TO costing_app;
