-- mkg_hub G3 (docs/design/mkg.md §4.7; g3-conventions §2): khối lượng lô từ phiếu
-- cân (Q-MKG-04), kiểm kê có phê duyệt (Q-MKG-06), tham số có phiên bản cho quy
-- tắc rủi ro hub.risk.v2 và dung sai cân (Q-MKG-09), chuyển kho giữa hai cơ sở
-- (Q-MKG-10), quy kết hao hụt từ đối soát của mkg/measurement.
--
--   param_version / param_decision / param_current  tham số có phiên bản (người đặt ≠ người duyệt, R-56)
--   weigh_link / lot_weigh_current                  phiếu cân (sự kiện nội bộ) ↔ lô / phiếu xuất
--   stocktake / stocktake_decision / stocktake_current  phiên kiểm kê → dịch chuyển điều chỉnh có duyệt
--   transfer_event / transfer_current               chuyển kho A → B, trạng thái đang đi đường
--   lot_loss / lot_loss_current                     quy kết hao hụt HUB_IN → HUB_OUT theo cơ sở
--   expected_inbound                                số cuộn dự kiến về (fieldops, tùy chọn)
--
-- Không thêm cột vào sổ đã có: chuỗi băm tính trên to_jsonb(dòng) nên cột mới
-- làm hỏng verify_chain của dòng cũ. Chỉ nới CHECK loại dịch chuyển / nguồn gốc lô.
-- Không có dữ liệu cá nhân (R-04). Khối lượng numeric(14,3) có dấu khi là chênh.

-- ---------------------------------------------------------------------------
-- 0. Nới CHECK của sổ G2 (không đổi dòng cũ, không đổi chuỗi băm)
-- ---------------------------------------------------------------------------
ALTER TABLE mkg_hub.stock_movement DROP CONSTRAINT stock_movement_kind_check;
ALTER TABLE mkg_hub.stock_movement ADD CONSTRAINT stock_movement_kind_check CHECK (kind IN (
  'receive', 'transform_in', 'transform_out', 'dispatch', 'correction',
  'adjustment',                     -- kiểm kê đã duyệt / phiếu cân HUB_IN khác số nhập tay (có dấu)
  'transfer_out', 'transfer_in'));  -- chuyển kho giữa hai cơ sở
ALTER TABLE mkg_hub.stock_movement DROP CONSTRAINT stock_movement_ref_type_check;
ALTER TABLE mkg_hub.stock_movement ADD CONSTRAINT stock_movement_ref_type_check CHECK (
  ref_type IS NULL OR ref_type IN ('batch', 'dispatch', 'movement', 'stocktake', 'weigh_ticket', 'transfer'));
ALTER TABLE mkg_hub.stock_movement ADD CONSTRAINT stock_movement_g3_sign_check CHECK (
  (kind <> 'transfer_in' OR qty_kg > 0) AND (kind <> 'transfer_out' OR qty_kg < 0));
-- Điều chỉnh luôn trỏ về căn cứ (phiên kiểm kê đã duyệt hoặc phiếu cân); chuyển kho trỏ về phiếu chuyển.
ALTER TABLE mkg_hub.stock_movement ADD CONSTRAINT stock_movement_g3_ref_check CHECK (
  (kind <> 'adjustment' OR (ref_type IN ('stocktake', 'weigh_ticket') AND ref_id IS NOT NULL))
  AND (kind NOT IN ('transfer_out', 'transfer_in') OR (ref_type = 'transfer' AND ref_id IS NOT NULL)));

ALTER TABLE mkg_hub.lot DROP CONSTRAINT lot_origin_check;
ALTER TABLE mkg_hub.lot ADD CONSTRAINT lot_origin_check CHECK (origin IN (
  'receipt', 'transform',
  'stocktake',   -- lô điều chỉnh khi kiểm kê thừa (nguồn kind = stocktake, tỷ lệ 1)
  'transfer'));  -- lô nhận ở cơ sở đích (nguồn kind = lot tỷ lệ 1 — giữ chuỗi nguồn gốc)

-- ---------------------------------------------------------------------------
-- 1. Tham số có phiên bản (Q-MKG-09): hub.risk.v2, hub.weigh.v1
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_hub.param_version (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'param:<param_set>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  param_set         text        NOT NULL CHECK (param_set IN ('risk', 'weigh')),
  params            jsonb       NOT NULL CHECK (jsonb_typeof(params) = 'object'),
  reason            text        NOT NULL CHECK (length(reason) BETWEEN 10 AND 500),
  effective_from    timestamptz NOT NULL,
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người đặt
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,                 -- quy tắc mà bộ tham số phục vụ: hub.risk.v2 | hub.weigh.v1
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK ((param_set = 'risk') = (rule_version = 'hub.risk.v2')),
  CHECK ((param_set = 'weigh') = (rule_version = 'hub.weigh.v1'))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX param_version_idem   ON mkg_hub.param_version (tenant_id, idem_key);
CREATE INDEX param_version_stream ON mkg_hub.param_version (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_hub.param_version', 'mkg_hub.param_version');

CREATE TABLE mkg_hub.param_decision (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'param:<param_set>'
  seq               bigint      NOT NULL,
  param_set         text        NOT NULL CHECK (param_set IN ('risk', 'weigh')),
  version_id        uuid        NOT NULL,
  version_seq       bigint      NOT NULL CHECK (version_seq >= 1),
  decision          text        NOT NULL CHECK (decision IN ('approved', 'rejected')),
  reason            text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  proposer_ref      uuid        NOT NULL,
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người duyệt
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
CREATE INDEX param_decision_idem    ON mkg_hub.param_decision (tenant_id, idem_key);
CREATE INDEX param_decision_version ON mkg_hub.param_decision (tenant_id, version_id);
SELECT shared.apply_ledger('mkg_hub.param_decision', 'mkg_hub.param_decision');

CREATE TABLE mkg_hub.param_current (
  tenant_id      uuid        NOT NULL,
  param_set      text        NOT NULL,
  version_id     uuid        NOT NULL,
  version_seq    bigint      NOT NULL,
  rule_version   text        NOT NULL,
  params         jsonb       NOT NULL,
  effective_from timestamptz NOT NULL,
  proposed_by    uuid        NOT NULL,
  approved_by    uuid        NOT NULL,
  approved_at    timestamptz NOT NULL,
  updated_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, param_set),
  CHECK (approved_by <> proposed_by)
);
SELECT shared.apply_tenant_rls('mkg_hub.param_current');

-- ---------------------------------------------------------------------------
-- 2. Phiếu cân ↔ lô (Q-MKG-04) — nguồn: sự kiện nội bộ mkg.measurement.weigh_ticket.recorded
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_hub.weigh_link (
  id                   uuid          NOT NULL,
  tenant_id            uuid          NOT NULL,
  stream_key           text          NOT NULL,            -- 'lot:<lot_ref>'
  seq                  bigint        NOT NULL,
  lot_ref              uuid          NOT NULL,            -- lot_ref của phiếu = id lô hub (UUID thuần, R-06)
  point                text          NOT NULL CHECK (point IN ('HUB_IN', 'HUB_OUT')),
  ticket_id            uuid          NULL,                -- NULL chỉ với nhận tay có lý do
  ticket_source        text          NULL CHECK (ticket_source IS NULL OR ticket_source IN ('edge', 'customer_integration', 'manual_evidence')),
  supersedes_ticket_id uuid          NULL,                -- phiếu bù của measurement: phiếu bị thay
  measured_kg          numeric(14,3) NULL CHECK (measured_kg IS NULL OR measured_kg >= 0),
  moisture_pct         numeric(5,2)  NULL,
  book_kg              numeric(14,3) NULL,                -- khối lượng sổ hub trước khi áp phiếu
  diff_kg              numeric(14,3) NULL,                -- measured − book (có dấu)
  tolerance_kg         numeric(14,3) NULL,
  outcome              text          NOT NULL CHECK (outcome IN (
                         'pending',             -- phiếu HUB_IN về trước khi hub nhận lô
                         'receipt',             -- lô nhận bằng phiếu (khối lượng = phiếu)
                         'manual_receipt',      -- nhận tay có lý do (cân hỏng / chưa có cân)
                         'within_tolerance',    -- lô nhập tay, phiếu lệch trong dung sai → giữ số nhập tay
                         'adjusted',            -- lệch ngoài dung sai (hoặc lô đã theo phiếu) → dịch chuyển adjustment
                         'variance_unresolved', -- cần giảm nhưng hàng của lô đã đi → chờ kiểm kê
                         'transfer_arrival',    -- phiếu HUB_IN của lô đang chuyển kho → dùng khi nhận ở đích
                         'dispatch_linked',     -- phiếu HUB_OUT gắn với phiếu xuất
                         'dispatch_variance',   -- phiếu HUB_OUT gắn được nhưng lệch phân bổ ngoài dung sai
                         'dispatch_unmatched',  -- phiếu HUB_OUT chưa có phiếu xuất chứa lô
                         'stale')),             -- phiếu đã bị phiếu bù thay trước khi tới (giao lệch thứ tự)
  dispatch_id          uuid          NULL,
  movement_id          uuid          NULL,                -- dịch chuyển adjustment đã ghi
  reason               text          NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  param_version        text          NULL,                -- 'hub.weigh.v1#<seq>' (0 = mặc định tạm)
  event_time           timestamptz   NOT NULL,            -- event_time của phiếu (lúc cân) / lúc nhận tay
  recorded_at          timestamptz   NOT NULL DEFAULT now(),
  supersedes_id        uuid          NULL,
  correction_reason    text          NULL,
  actor_ref            uuid          NOT NULL,
  device_id            text          NULL,
  idem_key             text          NOT NULL,            -- 'weigh:<ticket_id>' | 'manual:<lot_id>'
  rule_version         text          NOT NULL,
  prev_hash            bytea         NOT NULL,
  row_hash             bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK ((outcome = 'manual_receipt') = (ticket_id IS NULL)),
  CHECK (outcome <> 'manual_receipt' OR reason IS NOT NULL),
  CHECK (outcome <> 'adjusted' OR movement_id IS NOT NULL),
  CHECK (outcome NOT IN ('dispatch_linked', 'dispatch_variance') OR dispatch_id IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX weigh_link_idem   ON mkg_hub.weigh_link (tenant_id, idem_key);
CREATE INDEX weigh_link_stream ON mkg_hub.weigh_link (tenant_id, stream_key, seq);
CREATE INDEX weigh_link_ticket ON mkg_hub.weigh_link (tenant_id, ticket_id) WHERE ticket_id IS NOT NULL;
SELECT shared.apply_ledger('mkg_hub.weigh_link', 'mkg_hub.weigh_link');

-- Phiếu hiệu lực theo (lô, điểm cân) và căn cứ khối lượng của lô.
CREATE TABLE mkg_hub.lot_weigh_current (
  tenant_id            uuid          NOT NULL,
  lot_id               uuid          NOT NULL,
  point                text          NOT NULL CHECK (point IN ('HUB_IN', 'HUB_OUT')),
  ticket_id            uuid          NULL,
  supersedes_ticket_id uuid          NULL,
  measured_kg          numeric(14,3) NULL,
  moisture_pct         numeric(5,2)  NULL,
  status               text          NOT NULL CHECK (status IN ('pending', 'applied', 'manual', 'transfer_arrival', 'linked', 'unmatched')),
  basis                text          NOT NULL CHECK (basis IN ('manual', 'ticket')),
  book_kg              numeric(14,3) NULL,                -- khối lượng nhận theo sổ hub sau khi áp phiếu
  dispatch_id          uuid          NULL,
  last_link_id         uuid          NOT NULL,
  updated_at           timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, lot_id, point)
);
CREATE INDEX lot_weigh_current_ticket ON mkg_hub.lot_weigh_current (tenant_id, ticket_id);
SELECT shared.apply_tenant_rls('mkg_hub.lot_weigh_current');

-- ---------------------------------------------------------------------------
-- 3. Kiểm kê (Q-MKG-06; prototype WH FN-20..23)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_hub.stocktake (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'facility:<facility_id>'
  seq               bigint        NOT NULL,
  facility_id       uuid          NOT NULL,
  -- [{product, book_kg, counted_kg, variance_kg}] — sổ chụp LÚC ĐẾM
  lines             jsonb         NOT NULL CHECK (jsonb_typeof(lines) = 'array' AND jsonb_array_length(lines) >= 1),
  book_kg           numeric(14,3) NOT NULL CHECK (book_kg >= 0),
  counted_kg        numeric(14,3) NOT NULL CHECK (counted_kg >= 0),
  variance_kg       numeric(14,3) NOT NULL,
  note              text          NULL CHECK (note IS NULL OR length(note) <= 500),
  event_time        timestamptz   NOT NULL,               -- lúc đếm
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,                   -- đếm lại thay phiên chờ duyệt
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,               -- người đếm
  device_id         text          NULL,
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (variance_kg = counted_kg - book_kg)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX stocktake_idem   ON mkg_hub.stocktake (tenant_id, idem_key);
CREATE INDEX stocktake_stream ON mkg_hub.stocktake (tenant_id, stream_key, seq);
CREATE INDEX stocktake_id     ON mkg_hub.stocktake (id);
SELECT shared.apply_ledger('mkg_hub.stocktake', 'mkg_hub.stocktake');

CREATE TABLE mkg_hub.stocktake_decision (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'facility:<facility_id>'
  seq               bigint      NOT NULL,
  facility_id       uuid        NOT NULL,
  stocktake_id      uuid        NOT NULL,
  decision          text        NOT NULL CHECK (decision IN ('approved', 'rejected')),
  reason            text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  proposer_ref      uuid        NOT NULL,                 -- người đếm (chép để kiểm toán R-56)
  -- [{lot_id, movement_id, qty_kg}] — dịch chuyển adjustment đã ghi khi duyệt
  adjustments       jsonb       NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(adjustments) = 'array'),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người duyệt
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (actor_ref <> proposer_ref),                     -- R-56 lớp DB
  CHECK (decision = 'approved' OR reason IS NOT NULL),
  CHECK (decision = 'approved' OR adjustments = '[]'::jsonb)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX stocktake_decision_idem      ON mkg_hub.stocktake_decision (tenant_id, idem_key);
CREATE INDEX stocktake_decision_stocktake ON mkg_hub.stocktake_decision (tenant_id, stocktake_id);
SELECT shared.apply_ledger('mkg_hub.stocktake_decision', 'mkg_hub.stocktake_decision');

CREATE TABLE mkg_hub.stocktake_current (
  tenant_id    uuid          NOT NULL,
  stocktake_id uuid          NOT NULL,
  facility_id  uuid          NOT NULL,
  status       text          NOT NULL CHECK (status IN ('pending', 'approved', 'rejected', 'superseded')),
  variance_kg  numeric(14,3) NOT NULL,
  counted_by   uuid          NOT NULL,
  counted_at   timestamptz   NOT NULL,
  decided_by   uuid          NULL,
  decided_at   timestamptz   NULL,
  updated_at   timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, stocktake_id),
  CHECK ((status IN ('approved', 'rejected')) = (decided_by IS NOT NULL))
);
-- Một phiên chờ duyệt mỗi cơ sở.
CREATE UNIQUE INDEX stocktake_current_one_pending ON mkg_hub.stocktake_current (tenant_id, facility_id) WHERE status = 'pending';
SELECT shared.apply_tenant_rls('mkg_hub.stocktake_current');

-- ---------------------------------------------------------------------------
-- 4. Chuyển kho giữa hai cơ sở MKG (Q-MKG-10)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_hub.transfer_event (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'transfer:<transfer_id>'
  seq               bigint        NOT NULL,
  transfer_id       uuid          NOT NULL,
  kind              text          NOT NULL CHECK (kind IN ('dispatched', 'received')),
  from_facility_id  uuid          NOT NULL,
  to_facility_id    uuid          NOT NULL,
  dispatch_id       uuid          NOT NULL,               -- phiếu xuất ở A (mkg_hub.dispatch, destination_ref = B)
  mass_kg           numeric(14,3) NOT NULL CHECK (mass_kg > 0),   -- xuất (dispatched) hoặc nhận (received)
  loss_kg           numeric(14,3) NULL,                   -- received: xuất − nhận (có dấu: âm = tăng khối lượng)
  -- dispatched: [{lot_id, mass_kg}] lô nguồn ở A; received: [{source_lot_id, lot_id, mass_kg}] lô mới ở B
  lots              jsonb         NOT NULL CHECK (jsonb_typeof(lots) = 'array' AND jsonb_array_length(lots) >= 1),
  weigh_ticket_refs jsonb         NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(weigh_ticket_refs) = 'array'),
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
  CHECK (from_facility_id <> to_facility_id),
  CHECK ((kind = 'received') = (loss_kg IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX transfer_event_idem     ON mkg_hub.transfer_event (tenant_id, idem_key);
CREATE INDEX transfer_event_stream   ON mkg_hub.transfer_event (tenant_id, stream_key, seq);
CREATE INDEX transfer_event_transfer ON mkg_hub.transfer_event (tenant_id, transfer_id);
CREATE INDEX transfer_event_lots     ON mkg_hub.transfer_event USING gin (lots jsonb_path_ops);
SELECT shared.apply_ledger('mkg_hub.transfer_event', 'mkg_hub.transfer_event');

CREATE TABLE mkg_hub.transfer_current (
  tenant_id        uuid          NOT NULL,
  transfer_id      uuid          NOT NULL,
  from_facility_id uuid          NOT NULL,
  to_facility_id   uuid          NOT NULL,
  dispatch_id      uuid          NOT NULL,
  status           text          NOT NULL CHECK (status IN ('in_transit', 'received')),
  dispatched_kg    numeric(14,3) NOT NULL,
  received_kg      numeric(14,3) NULL,
  loss_kg          numeric(14,3) NULL,
  source_lots      jsonb         NOT NULL,                -- [{lot_id, mass_kg}]
  received_lots    jsonb         NULL,
  dispatched_at    timestamptz   NOT NULL,
  received_at      timestamptz   NULL,
  updated_at       timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, transfer_id),
  CHECK ((status = 'received') = (received_kg IS NOT NULL))
);
CREATE INDEX transfer_current_status ON mkg_hub.transfer_current (tenant_id, status);
CREATE INDEX transfer_current_lots   ON mkg_hub.transfer_current USING gin (source_lots jsonb_path_ops);
SELECT shared.apply_tenant_rls('mkg_hub.transfer_current');

-- ---------------------------------------------------------------------------
-- 5. Quy kết hao hụt — nguồn: sự kiện nội bộ mkg.measurement.reconciliation.completed
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_hub.lot_loss (
  id                 uuid          NOT NULL,
  tenant_id          uuid          NOT NULL,
  stream_key         text          NOT NULL,              -- 'lot:<lot_ref>'
  seq                bigint        NOT NULL,
  reconciliation_id  uuid          NOT NULL,
  lot_ref            uuid          NOT NULL,
  facility_id        uuid          NULL,                  -- cơ sở giữ lô (NULL: lô không có ở hub, vd DIRECT)
  route              text          NOT NULL CHECK (route IN ('HUB', 'DIRECT')),
  hub_in_kg          numeric(14,3) NULL,
  hub_out_kg         numeric(14,3) NULL,
  customer_in_kg     numeric(14,3) NULL,
  expected_loss_kg   numeric(14,3) NULL,
  abnormal_loss_kg   numeric(14,3) NULL,
  commercial_diff_kg numeric(14,3) NULL,
  outcome            text          NOT NULL CHECK (outcome IN ('within_tolerance', 'loss_alert', 'dispute_opened', 'direct_aggregate')),
  tolerance_version  text          NULL,
  loss_norm_version  text          NULL,
  dispute_id         uuid          NULL,
  source_rule        text          NOT NULL,              -- rule_version của đối soát bên measurement
  event_time         timestamptz   NOT NULL,
  recorded_at        timestamptz   NOT NULL DEFAULT now(),
  supersedes_id      uuid          NULL,
  correction_reason  text          NULL,
  actor_ref          uuid          NOT NULL,
  device_id          text          NULL,
  idem_key           text          NOT NULL,              -- 'recon:<reconciliation_id>'
  rule_version       text          NOT NULL,
  prev_hash          bytea         NOT NULL,
  row_hash           bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX lot_loss_idem   ON mkg_hub.lot_loss (tenant_id, idem_key);
CREATE INDEX lot_loss_stream ON mkg_hub.lot_loss (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_hub.lot_loss', 'mkg_hub.lot_loss');

-- Đối soát mới nhất (theo event_time) của mỗi lô — tổng hợp hao hụt theo cơ sở.
CREATE TABLE mkg_hub.lot_loss_current (
  tenant_id         uuid          NOT NULL,
  lot_ref           uuid          NOT NULL,
  facility_id       uuid          NULL,
  reconciliation_id uuid          NOT NULL,
  route             text          NOT NULL,
  hub_in_kg         numeric(14,3) NULL,
  hub_out_kg        numeric(14,3) NULL,
  expected_loss_kg  numeric(14,3) NULL,
  abnormal_loss_kg  numeric(14,3) NULL,
  outcome           text          NOT NULL,
  event_time        timestamptz   NOT NULL,
  updated_at        timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, lot_ref)
);
CREATE INDEX lot_loss_current_facility ON mkg_hub.lot_loss_current (tenant_id, facility_id);
SELECT shared.apply_tenant_rls('mkg_hub.lot_loss_current');

-- ---------------------------------------------------------------------------
-- 6. Hàng dự kiến về (tùy chọn) — nguồn: mkg.fieldops.roll_count.recorded.
-- Bảng chiếu của sổ mkg_fieldops.roll_count (nguồn sự thật ở fieldops), dựng
-- lại bằng phát lại sự kiện. Chỉ số cuộn; kg ước tính = cuộn × kg/cuộn mặc định
-- của tham số hub.weigh.v1 (KHÔNG phải yield_kg_per_roll học theo HTX).
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_hub.expected_inbound (
  tenant_id     uuid        NOT NULL,
  roll_count_id uuid        NOT NULL,
  lot_ref       uuid        NOT NULL,
  plot_ref      uuid        NOT NULL,
  htx_ref       uuid        NOT NULL,
  stage         text        NOT NULL CHECK (stage IN ('bale', 'gather', 'boat_load')),
  rolls         integer     NOT NULL CHECK (rolls >= 0),
  supersedes_id uuid        NULL,
  status        text        NOT NULL CHECK (status IN ('active', 'superseded')),
  event_time    timestamptz NOT NULL,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, roll_count_id)
);
CREATE INDEX expected_inbound_lot ON mkg_hub.expected_inbound (tenant_id, lot_ref, stage);
SELECT shared.apply_tenant_rls('mkg_hub.expected_inbound');

-- ---------------------------------------------------------------------------
-- Quyền
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON mkg_hub.param_version, mkg_hub.param_decision, mkg_hub.weigh_link, mkg_hub.stocktake,
                        mkg_hub.stocktake_decision, mkg_hub.transfer_event, mkg_hub.lot_loss TO hub_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_hub.param_version, mkg_hub.param_decision, mkg_hub.weigh_link, mkg_hub.stocktake,
                                   mkg_hub.stocktake_decision, mkg_hub.transfer_event, mkg_hub.lot_loss FROM hub_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_hub.param_current, mkg_hub.lot_weigh_current, mkg_hub.stocktake_current,
                                        mkg_hub.transfer_current, mkg_hub.lot_loss_current, mkg_hub.expected_inbound TO hub_app;
