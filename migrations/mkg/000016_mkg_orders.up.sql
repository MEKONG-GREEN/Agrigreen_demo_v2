-- mkg_orders (G4, docs/design/mkg.md §5.5, docs/dev/g4-conventions.md §1–2): chứng từ thương mại của
-- MKG — chiều mua (hợp đồng mua rơm với HTX, đơn mua, phiếu nhận hàng ↔ đơn mua) và chiều bán (đơn
-- bán, lịch giao, biên bản giao nhận liên kết phiếu cân CUSTOMER_IN). Schema tạo ở mkg 000014.
--
-- Mỗi dòng sổ là ẢNH CHỤP ĐẦY ĐỦ của aggregate sau sự kiện (kind) → bảng chiếu = dòng mới nhất của
-- mỗi thực thể (theo seq của chuỗi), dựng lại được bằng một câu SELECT DISTINCT ON (R-21, R-22).
--
--   contract_event        SỔ hợp đồng mua rơm có phiên bản (stream 'contract:<id>'): proposed / approved /
--                         rejected / superseded / terminated. Người lập ≠ người duyệt (R-56, CHECK). proto CT-01..03.
--   contract_current      bảng chiếu theo (hợp đồng, phiên bản).
--   po_event              SỔ đơn mua (stream 'po:<id>'): created / approved / rejected / cancelled.
--   po_current            bảng chiếu đơn mua.
--   receipt_event         SỔ phiếu nhận hàng (stream 'receipt:<id>'): created / weighed / report_filed / corrected /
--                         approved / rejected. Kiểm ↔ đơn mua (> ngưỡng tạm 3 % → cần biên bản, KHÔNG chặn).
--   receipt_current       bảng chiếu phiếu nhận hàng.
--   payable_event         SỔ khoản phải trả đơn mua KHÁC (không phải rơm): đúng một dòng mỗi phiếu nhận đã
--                         duyệt (idem 'payable:<receipt_id>'). Phải trả mua rơm chỉ từ quyết toán costing.
--   order_event           SỔ đơn bán (stream 'order:<id>'): created / confirmed / delivering / delivered / completed /
--                         cancelled. Giá/chiết khấu vượt ngưỡng → xác nhận bởi sales_approver ≠ người lập (CHECK).
--   order_current         bảng chiếu đơn bán.
--   delivery_event        SỔ giao nhận (stream 'delivery:<id>'): planned / dispatched / ticket_linked / accepted /
--                         rejected / cancelled. Chấp nhận bù = dòng accepted mới với supersedes_id (RB-1).
--   delivery_current      bảng chiếu giao nhận (lịch giao + biên bản).
--   ticket_event          SỔ phiếu cân nhận từ mkg.measurement.weigh_ticket.recorded (HUB_IN, CUSTOMER_IN) —
--                         stream 'lot:<lot_ref>', idem 'ticket:<ticket_id>'. Khối lượng chuẩn KHÔNG nhập tay.
--   lot_ticket_current    phiếu hiệu lực theo (lô, điểm cân) — dựng lại bằng phát lại ticket_event.
--   param_version         SỔ tham số có phiên bản (stream 'param:<key>'), người đặt ≠ người duyệt (R-56).
--   param_current         bảng chiếu tham số.
--
-- Không có gì của schema này qua van: giá mua/bán, chiết khấu, khách, HTX, điều khoản ở lại vùng mkg
-- (mkg.md §3.1). Khách / HTX / nhà cung cấp là *_ref (R-06); tên hiển thị qua vault.
-- Tiền numeric(18,2), đơn giá numeric(18,4) (shared/money), khối lượng numeric(14,3) (shared/mass), R-10.

-- ---------------------------------------------------------------------------
-- 1. Hợp đồng mua rơm (proto CT-01..03)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_orders.contract_event (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,                 -- 'contract:<contract_id>'
  seq               bigint        NOT NULL,
  contract_id       uuid          NOT NULL,
  version           integer       NOT NULL CHECK (version >= 1),
  kind              text          NOT NULL CHECK (kind IN ('proposed', 'approved', 'rejected', 'superseded', 'terminated')),
  status            text          NOT NULL CHECK (status IN ('pending', 'active', 'rejected', 'superseded', 'terminated')),
  code              text          NOT NULL CHECK (code ~ '^HDR[0-9]{5,}$'),
  htx_ref           uuid          NOT NULL,
  basis             text          NOT NULL CHECK (basis IN ('per_tonne', 'per_bale')),
  unit_price        numeric(18,4) NOT NULL CHECK (unit_price > 0),            -- CT-02
  committed_kg      numeric(14,3) NOT NULL CHECK (committed_kg > 0),          -- CT-02
  max_moisture_pct  numeric(5,2)  NULL CHECK (max_moisture_pct IS NULL OR max_moisture_pct BETWEEN 0 AND 100),  -- lưu, chưa cưỡng chế (C-4)
  valid_from        date          NOT NULL,
  valid_to          date          NOT NULL,
  note              text          NULL CHECK (note IS NULL OR length(note) <= 1000),
  proposer_ref      uuid          NOT NULL,
  approver_ref      uuid          NULL,
  reason            text          NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
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
  CHECK (valid_to >= valid_from),                                          -- CT-02
  CHECK (kind <> 'proposed' OR (status = 'pending' AND actor_ref = proposer_ref AND approver_ref IS NULL)),
  CHECK (kind NOT IN ('approved', 'rejected') OR actor_ref <> proposer_ref),  -- R-56 ở tầng DB
  CHECK (kind <> 'approved' OR (status = 'active' AND approver_ref = actor_ref)),
  CHECK (kind <> 'rejected' OR (status = 'rejected' AND reason IS NOT NULL)),
  CHECK (kind <> 'superseded' OR status = 'superseded'),
  CHECK (kind <> 'terminated' OR (status = 'terminated' AND reason IS NOT NULL)),
  CHECK (approver_ref IS NULL OR approver_ref <> proposer_ref)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX contract_event_idem   ON mkg_orders.contract_event (tenant_id, idem_key);
CREATE INDEX contract_event_stream ON mkg_orders.contract_event (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_orders.contract_event', 'mkg_orders.contract_event');

CREATE TABLE mkg_orders.contract_current (
  tenant_id        uuid          NOT NULL,
  contract_id      uuid          NOT NULL,
  version          integer       NOT NULL,
  status           text          NOT NULL CHECK (status IN ('pending', 'active', 'rejected', 'superseded', 'terminated')),
  code             text          NOT NULL,
  htx_ref          uuid          NOT NULL,
  basis            text          NOT NULL,
  unit_price       numeric(18,4) NOT NULL CHECK (unit_price > 0),
  committed_kg     numeric(14,3) NOT NULL CHECK (committed_kg > 0),
  max_moisture_pct numeric(5,2)  NULL,
  valid_from       date          NOT NULL,
  valid_to         date          NOT NULL,
  note             text          NULL,
  proposer_ref     uuid          NOT NULL,
  approver_ref     uuid          NULL,
  reason           text          NULL,
  last_event_id    uuid          NOT NULL,
  last_seq         bigint        NOT NULL,
  event_time       timestamptz   NOT NULL,
  PRIMARY KEY (tenant_id, contract_id, version),
  CHECK (approver_ref IS NULL OR approver_ref <> proposer_ref)
);
CREATE INDEX contract_current_htx ON mkg_orders.contract_current (tenant_id, htx_ref, status, valid_from);
SELECT shared.apply_tenant_rls('mkg_orders.contract_current');

-- ---------------------------------------------------------------------------
-- 2. Đơn mua (proto createPurchaseOrder)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_orders.po_event (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,                 -- 'po:<po_id>'
  seq               bigint        NOT NULL,
  po_id             uuid          NOT NULL,
  kind              text          NOT NULL CHECK (kind IN ('created', 'approved', 'rejected', 'cancelled')),
  status            text          NOT NULL CHECK (status IN ('draft', 'approved', 'rejected', 'cancelled')),
  code              text          NOT NULL CHECK (code ~ '^PO[0-9]{6,}$'),
  category          text          NOT NULL CHECK (category IN ('straw', 'other')),
  supplier_ref      uuid          NOT NULL,                 -- HTX (rơm) hoặc nhà cung cấp — *_ref, không định danh
  contract_ref      uuid          NULL,
  contract_version  integer       NULL,
  facility_ref      uuid          NULL,
  unit              text          NOT NULL CHECK (unit IN ('kg', 'bale')),
  ordered_qty       numeric(14,3) NOT NULL CHECK (ordered_qty > 0),
  unit_price        numeric(18,4) NOT NULL CHECK (unit_price > 0),           -- VND/tấn (kg) hoặc VND/cuộn (bale)
  price_source      text          NOT NULL CHECK (price_source IN ('contract', 'entered')),
  expected_date     date          NULL,
  description       text          NULL CHECK (description IS NULL OR length(description) <= 1000),
  creator_ref       uuid          NOT NULL,
  approver_ref      uuid          NULL,
  reason            text          NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
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
  CHECK (unit <> 'bale' OR ordered_qty = trunc(ordered_qty)),
  CHECK ((price_source = 'contract') = (contract_ref IS NOT NULL)),
  CHECK (contract_ref IS NULL OR category = 'straw'),
  CHECK (kind <> 'created' OR (status = 'draft' AND actor_ref = creator_ref)),
  CHECK (kind NOT IN ('approved', 'rejected') OR actor_ref <> creator_ref),  -- R-56 ở tầng DB
  CHECK (kind <> 'approved' OR (status = 'approved' AND approver_ref = actor_ref)),
  CHECK (kind <> 'rejected' OR (status = 'rejected' AND reason IS NOT NULL)),
  CHECK (kind <> 'cancelled' OR (status = 'cancelled' AND reason IS NOT NULL)),
  CHECK (approver_ref IS NULL OR approver_ref <> creator_ref)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX po_event_idem   ON mkg_orders.po_event (tenant_id, idem_key);
CREATE INDEX po_event_stream ON mkg_orders.po_event (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_orders.po_event', 'mkg_orders.po_event');

CREATE TABLE mkg_orders.po_current (
  tenant_id        uuid          NOT NULL,
  po_id            uuid          NOT NULL,
  status           text          NOT NULL CHECK (status IN ('draft', 'approved', 'rejected', 'cancelled')),
  code             text          NOT NULL,
  category         text          NOT NULL,
  supplier_ref     uuid          NOT NULL,
  contract_ref     uuid          NULL,
  contract_version integer       NULL,
  facility_ref     uuid          NULL,
  unit             text          NOT NULL,
  ordered_qty      numeric(14,3) NOT NULL,
  unit_price       numeric(18,4) NOT NULL,
  price_source     text          NOT NULL,
  expected_date    date          NULL,
  description      text          NULL,
  creator_ref      uuid          NOT NULL,
  approver_ref     uuid          NULL,
  reason           text          NULL,
  last_event_id    uuid          NOT NULL,
  last_seq         bigint        NOT NULL,
  event_time       timestamptz   NOT NULL,
  PRIMARY KEY (tenant_id, po_id),
  UNIQUE (tenant_id, code),
  CHECK (approver_ref IS NULL OR approver_ref <> creator_ref)
);
CREATE INDEX po_current_supplier ON mkg_orders.po_current (tenant_id, supplier_ref, status);
SELECT shared.apply_tenant_rls('mkg_orders.po_current');

-- ---------------------------------------------------------------------------
-- 3. Phiếu nhận hàng ↔ đơn mua (proto createGoodsReceipt, g3-acceptance "ngoài G3 → G4")
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_orders.receipt_event (
  id                    uuid          NOT NULL,
  tenant_id             uuid          NOT NULL,
  stream_key            text          NOT NULL,             -- 'receipt:<receipt_id>'
  seq                   bigint        NOT NULL,
  receipt_id            uuid          NOT NULL,
  po_id                 uuid          NOT NULL,
  kind                  text          NOT NULL CHECK (kind IN ('created', 'weighed', 'report_filed', 'corrected', 'approved', 'rejected')),
  status                text          NOT NULL CHECK (status IN ('awaiting_weigh', 'pending_approval', 'approved', 'rejected')),
  code                  text          NOT NULL CHECK (code ~ '^GRN[0-9]{6,}$'),
  category              text          NOT NULL CHECK (category IN ('straw', 'other')),
  supplier_ref          uuid          NOT NULL,
  lot_ref               uuid          NULL,
  unit                  text          NOT NULL CHECK (unit IN ('kg', 'bale')),
  ordered_qty           numeric(14,3) NOT NULL CHECK (ordered_qty > 0),
  unit_price            numeric(18,4) NOT NULL CHECK (unit_price > 0),
  ticket_ref            uuid          NULL,                 -- phiếu cân HUB_IN (measurement)
  ticket_kg             numeric(14,3) NULL CHECK (ticket_kg IS NULL OR ticket_kg >= 0),
  received_qty          numeric(14,3) NULL CHECK (received_qty IS NULL OR received_qty >= 0),  -- theo unit
  qty_source            text          NULL CHECK (qty_source IN ('ticket', 'count', 'entered')),
  variance_pct          numeric(9,2)  NULL,
  threshold_pct         numeric(5,2)  NULL CHECK (threshold_pct IS NULL OR threshold_pct >= 0),
  param_version         text          NULL,
  needs_variance_report boolean       NOT NULL DEFAULT false,
  audit_note            text          NULL,
  report_note           text          NULL CHECK (report_note IS NULL OR length(report_note) BETWEEN 10 AND 2000),
  report_file_ref       uuid          NULL,
  report_by             uuid          NULL,
  creator_ref           uuid          NOT NULL,
  approver_ref          uuid          NULL,
  reason                text          NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  event_time            timestamptz   NOT NULL,
  recorded_at           timestamptz   NOT NULL DEFAULT now(),
  supersedes_id         uuid          NULL,
  correction_reason     text          NULL,
  actor_ref             uuid          NOT NULL,
  device_id             text          NULL,
  idem_key              text          NOT NULL,
  rule_version          text          NOT NULL,
  prev_hash             bytea         NOT NULL,
  row_hash              bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (kind <> 'corrected' OR supersedes_id IS NOT NULL),
  CHECK ((status = 'awaiting_weigh') = (variance_pct IS NULL)),
  CHECK (NOT needs_variance_report OR audit_note IS NOT NULL),             -- proto: ghi chú kiểm toán khi vượt ngưỡng
  CHECK ((report_note IS NULL) = (report_by IS NULL)),
  CHECK (unit <> 'bale' OR received_qty IS NULL OR received_qty = trunc(received_qty)),
  CHECK (kind <> 'created' OR actor_ref = creator_ref),
  CHECK (kind NOT IN ('approved', 'rejected') OR actor_ref <> creator_ref),  -- R-56 ở tầng DB
  CHECK (kind <> 'approved' OR (status = 'approved' AND approver_ref = actor_ref)),
  CHECK (kind <> 'rejected' OR (status = 'rejected' AND reason IS NOT NULL)),
  CHECK (approver_ref IS NULL OR approver_ref <> creator_ref)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX receipt_event_idem   ON mkg_orders.receipt_event (tenant_id, idem_key);
CREATE INDEX receipt_event_stream ON mkg_orders.receipt_event (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_orders.receipt_event', 'mkg_orders.receipt_event');

CREATE TABLE mkg_orders.receipt_current (
  tenant_id             uuid          NOT NULL,
  receipt_id            uuid          NOT NULL,
  po_id                 uuid          NOT NULL,
  status                text          NOT NULL CHECK (status IN ('awaiting_weigh', 'pending_approval', 'approved', 'rejected')),
  code                  text          NOT NULL,
  category              text          NOT NULL,
  supplier_ref          uuid          NOT NULL,
  lot_ref               uuid          NULL,
  unit                  text          NOT NULL,
  ordered_qty           numeric(14,3) NOT NULL,
  unit_price            numeric(18,4) NOT NULL,
  ticket_ref            uuid          NULL,
  ticket_kg             numeric(14,3) NULL,
  received_qty          numeric(14,3) NULL,
  qty_source            text          NULL,
  variance_pct          numeric(9,2)  NULL,
  threshold_pct         numeric(5,2)  NULL,
  param_version         text          NULL,
  needs_variance_report boolean       NOT NULL,
  audit_note            text          NULL,
  report_note           text          NULL,
  report_file_ref       uuid          NULL,
  report_by             uuid          NULL,
  creator_ref           uuid          NOT NULL,
  approver_ref          uuid          NULL,
  reason                text          NULL,
  last_event_id         uuid          NOT NULL,
  last_seq              bigint        NOT NULL,
  event_time            timestamptz   NOT NULL,
  PRIMARY KEY (tenant_id, receipt_id),
  UNIQUE (tenant_id, code),
  CHECK (approver_ref IS NULL OR approver_ref <> creator_ref)
);
CREATE INDEX receipt_current_po  ON mkg_orders.receipt_current (tenant_id, po_id);
CREATE INDEX receipt_current_lot ON mkg_orders.receipt_current (tenant_id, lot_ref, status);
SELECT shared.apply_tenant_rls('mkg_orders.receipt_current');

-- Khoản phải trả đơn mua KHÁC — một nguồn (g4-conventions §1: không ghi trùng như prototype).
CREATE TABLE mkg_orders.payable_event (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,                 -- 'payable:<receipt_id>'
  seq               bigint        NOT NULL,
  receipt_id        uuid          NOT NULL,
  receipt_event_id  uuid          NOT NULL,                 -- dòng 'approved' của receipt_event
  po_id             uuid          NOT NULL,
  category          text          NOT NULL CHECK (category = 'other'),   -- rơm: chỉ từ quyết toán costing
  supplier_ref      uuid          NOT NULL,
  unit              text          NOT NULL CHECK (unit IN ('kg', 'bale')),
  qty               numeric(14,3) NOT NULL CHECK (qty >= 0),
  unit_price        numeric(18,4) NOT NULL CHECK (unit_price > 0),
  amount            numeric(18,2) NOT NULL CHECK (amount >= 0),
  due_date          date          NOT NULL,
  payment_term_days integer       NOT NULL CHECK (payment_term_days BETWEEN 0 AND 365),
  param_version     text          NOT NULL,
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
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX payable_event_idem    ON mkg_orders.payable_event (tenant_id, idem_key);
CREATE INDEX payable_event_stream  ON mkg_orders.payable_event (tenant_id, stream_key, seq);
CREATE INDEX payable_event_receipt ON mkg_orders.payable_event (tenant_id, receipt_id);
SELECT shared.apply_ledger('mkg_orders.payable_event', 'mkg_orders.payable_event');

-- ---------------------------------------------------------------------------
-- 4. Đơn bán (proto createSalesOrder / advanceSalesOrder)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_orders.order_event (
  id                   uuid          NOT NULL,
  tenant_id            uuid          NOT NULL,
  stream_key           text          NOT NULL,              -- 'order:<order_id>'
  seq                  bigint        NOT NULL,
  order_id             uuid          NOT NULL,
  kind                 text          NOT NULL CHECK (kind IN ('created', 'confirmed', 'delivering', 'delivered', 'completed', 'cancelled')),
  status               text          NOT NULL CHECK (status IN ('draft', 'confirmed', 'delivering', 'delivered', 'completed', 'cancelled')),
  code                 text          NOT NULL CHECK (code ~ '^SO[0-9]{6,}$'),
  customer_ref         uuid          NOT NULL,              -- khách: *_ref (định danh ở vault), không PII
  product              text          NOT NULL CHECK (product IN ('straw_bale', 'straw_loose', 'dried_straw', 'compressed_bale')),
  ordered_kg           numeric(14,3) NOT NULL CHECK (ordered_kg > 0),
  list_price           numeric(18,4) NULL CHECK (list_price IS NULL OR list_price > 0),   -- giá bảng giá, VND/tấn
  unit_price           numeric(18,4) NOT NULL CHECK (unit_price > 0),                     -- VND/tấn trước chiết khấu
  discount_pct         numeric(5,2)  NOT NULL DEFAULT 0 CHECK (discount_pct >= 0 AND discount_pct < 100),
  net_price            numeric(18,4) NOT NULL CHECK (net_price > 0),                      -- VND/tấn sau chiết khấu
  amount               numeric(18,2) NOT NULL CHECK (amount > 0),                         -- ordered_kg/1000 × net_price
  needs_price_approval boolean       NOT NULL,
  price_rule           text          NOT NULL,              -- phiên bản ngưỡng giá / chiết khấu đã áp
  payment_term_days    integer       NOT NULL CHECK (payment_term_days BETWEEN 0 AND 365),
  delivery_from        date          NULL,
  delivery_to          date          NULL,
  creator_ref          uuid          NOT NULL,
  approver_ref         uuid          NULL,
  reason               text          NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
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
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (delivery_to IS NULL OR delivery_from IS NULL OR delivery_to >= delivery_from),
  CHECK (kind <> 'created' OR (status = 'draft' AND actor_ref = creator_ref)),
  -- R-56 ở tầng DB: giá / chiết khấu vượt ngưỡng → người xác nhận là sales_approver KHÁC người lập.
  CHECK (kind <> 'confirmed' OR NOT needs_price_approval OR (actor_ref <> creator_ref AND approver_ref = actor_ref)),
  CHECK (NOT needs_price_approval OR status = 'draft' OR status = 'cancelled' OR approver_ref IS NOT NULL),
  CHECK (approver_ref IS NULL OR approver_ref <> creator_ref),
  CHECK (kind <> 'cancelled' OR (status = 'cancelled' AND reason IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX order_event_idem   ON mkg_orders.order_event (tenant_id, idem_key);
CREATE INDEX order_event_stream ON mkg_orders.order_event (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_orders.order_event', 'mkg_orders.order_event');

CREATE TABLE mkg_orders.order_current (
  tenant_id            uuid          NOT NULL,
  order_id             uuid          NOT NULL,
  status               text          NOT NULL CHECK (status IN ('draft', 'confirmed', 'delivering', 'delivered', 'completed', 'cancelled')),
  code                 text          NOT NULL,
  customer_ref         uuid          NOT NULL,
  product              text          NOT NULL,
  ordered_kg           numeric(14,3) NOT NULL,
  list_price           numeric(18,4) NULL,
  unit_price           numeric(18,4) NOT NULL,
  discount_pct         numeric(5,2)  NOT NULL,
  net_price            numeric(18,4) NOT NULL,
  amount               numeric(18,2) NOT NULL,
  needs_price_approval boolean       NOT NULL,
  price_rule           text          NOT NULL,
  payment_term_days    integer       NOT NULL,
  delivery_from        date          NULL,
  delivery_to          date          NULL,
  creator_ref          uuid          NOT NULL,
  approver_ref         uuid          NULL,
  reason               text          NULL,
  last_event_id        uuid          NOT NULL,
  last_seq             bigint        NOT NULL,
  event_time           timestamptz   NOT NULL,
  PRIMARY KEY (tenant_id, order_id),
  UNIQUE (tenant_id, code),
  CHECK (approver_ref IS NULL OR approver_ref <> creator_ref)
);
CREATE INDEX order_current_customer ON mkg_orders.order_current (tenant_id, customer_ref, status);
SELECT shared.apply_tenant_rls('mkg_orders.order_current');

-- ---------------------------------------------------------------------------
-- 5. Giao nhận: lịch giao + biên bản giao nhận liên kết phiếu CUSTOMER_IN
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_orders.delivery_event (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,                 -- 'delivery:<delivery_id>'
  seq               bigint        NOT NULL,
  delivery_id       uuid          NOT NULL,
  order_id          uuid          NOT NULL,
  kind              text          NOT NULL CHECK (kind IN ('planned', 'dispatched', 'ticket_linked', 'accepted', 'rejected', 'cancelled')),
  status            text          NOT NULL CHECK (status IN ('planned', 'dispatched', 'accepted', 'rejected', 'cancelled')),
  code              text          NOT NULL CHECK (code ~ '^GN[0-9]{6,}$'),
  customer_ref      uuid          NOT NULL,
  route             text          NOT NULL CHECK (route IN ('HUB', 'DIRECT')),
  lot_ref           uuid          NOT NULL,
  trip_ref          uuid          NULL,
  planned_date      date          NOT NULL,
  planned_kg        numeric(14,3) NOT NULL CHECK (planned_kg > 0),
  unit_price        numeric(18,4) NOT NULL CHECK (unit_price > 0),          -- đơn giá sau chiết khấu của đơn bán, VND/tấn
  ticket_ref        uuid          NULL,                     -- phiếu CUSTOMER_IN (measurement)
  ticket_kg         numeric(14,3) NULL CHECK (ticket_kg IS NULL OR ticket_kg >= 0),
  accepted_kg       numeric(14,3) NULL CHECK (accepted_kg IS NULL OR accepted_kg >= 0),
  amount            numeric(18,2) NULL,
  accepted_at       timestamptz   NULL,
  acceptance_id     uuid          NULL,                     -- id dòng 'accepted' hiệu lực (= event_id của delivery.accepted)
  dispute_ref       uuid          NULL,                     -- hồ sơ tranh chấp của measurement
  evidence_file_ref uuid          NULL,
  evidence_kind     text          NULL CHECK (evidence_kind IS NULL OR evidence_kind IN ('epod', 'minutes', 'ebill')),
  reason            text          NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,                     -- dòng 'accepted' trước (chấp nhận bù)
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,
  device_id         text          NULL,
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (supersedes_id IS NULL OR kind = 'accepted'),
  CHECK ((evidence_file_ref IS NULL) = (evidence_kind IS NULL)),
  -- Khối lượng chấp nhận = khối lượng phiếu CUSTOMER_IN (measurement), không nhập tay.
  CHECK (kind <> 'accepted' OR (status = 'accepted' AND ticket_ref IS NOT NULL AND accepted_kg IS NOT NULL
                                AND accepted_kg = ticket_kg AND amount IS NOT NULL AND accepted_at IS NOT NULL AND acceptance_id = id)),
  CHECK (status <> 'accepted' OR acceptance_id IS NOT NULL),
  CHECK (kind NOT IN ('rejected', 'cancelled') OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX delivery_event_idem   ON mkg_orders.delivery_event (tenant_id, idem_key);
CREATE INDEX delivery_event_stream ON mkg_orders.delivery_event (tenant_id, stream_key, seq);
CREATE INDEX delivery_event_accept ON mkg_orders.delivery_event (tenant_id, kind, accepted_at);
SELECT shared.apply_ledger('mkg_orders.delivery_event', 'mkg_orders.delivery_event');

CREATE TABLE mkg_orders.delivery_current (
  tenant_id         uuid          NOT NULL,
  delivery_id       uuid          NOT NULL,
  order_id          uuid          NOT NULL,
  status            text          NOT NULL CHECK (status IN ('planned', 'dispatched', 'accepted', 'rejected', 'cancelled')),
  code              text          NOT NULL,
  customer_ref      uuid          NOT NULL,
  route             text          NOT NULL,
  lot_ref           uuid          NOT NULL,
  trip_ref          uuid          NULL,
  planned_date      date          NOT NULL,
  planned_kg        numeric(14,3) NOT NULL,
  unit_price        numeric(18,4) NOT NULL,
  ticket_ref        uuid          NULL,
  ticket_kg         numeric(14,3) NULL,
  accepted_kg       numeric(14,3) NULL,
  amount            numeric(18,2) NULL,
  accepted_at       timestamptz   NULL,
  acceptance_id     uuid          NULL,
  dispute_ref       uuid          NULL,
  evidence_file_ref uuid          NULL,
  evidence_kind     text          NULL,
  reason            text          NULL,
  rule_version      text          NOT NULL,
  last_event_id     uuid          NOT NULL,
  last_seq          bigint        NOT NULL,
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL,
  PRIMARY KEY (tenant_id, delivery_id),
  UNIQUE (tenant_id, code)
);
CREATE INDEX delivery_current_order ON mkg_orders.delivery_current (tenant_id, order_id, planned_date);
CREATE INDEX delivery_current_lot   ON mkg_orders.delivery_current (tenant_id, lot_ref, status);
SELECT shared.apply_tenant_rls('mkg_orders.delivery_current');

-- ---------------------------------------------------------------------------
-- 6. Phiếu cân nhận từ measurement (HUB_IN → phiếu nhận hàng; CUSTOMER_IN → giao nhận)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_orders.ticket_event (
  id                   uuid          NOT NULL,
  tenant_id            uuid          NOT NULL,
  stream_key           text          NOT NULL,              -- 'lot:<lot_ref>'
  seq                  bigint        NOT NULL,
  ticket_id            uuid          NOT NULL,
  lot_ref              uuid          NOT NULL,
  point                text          NOT NULL CHECK (point IN ('HUB_IN', 'CUSTOMER_IN')),
  route                text          NOT NULL CHECK (route IN ('HUB', 'DIRECT')),
  source               text          NOT NULL,
  mass_kg              numeric(14,3) NOT NULL CHECK (mass_kg >= 0),
  supersedes_ticket_id uuid          NULL,                  -- phiếu bù của measurement
  ticket_time          timestamptz   NOT NULL,
  source_rule          text          NULL,
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
CREATE INDEX ticket_event_idem   ON mkg_orders.ticket_event (tenant_id, idem_key);
CREATE INDEX ticket_event_stream ON mkg_orders.ticket_event (tenant_id, stream_key, seq);
CREATE INDEX ticket_event_ticket ON mkg_orders.ticket_event (tenant_id, ticket_id);
SELECT shared.apply_ledger('mkg_orders.ticket_event', 'mkg_orders.ticket_event');

CREATE TABLE mkg_orders.lot_ticket_current (
  tenant_id            uuid          NOT NULL,
  lot_ref              uuid          NOT NULL,
  point                text          NOT NULL CHECK (point IN ('HUB_IN', 'CUSTOMER_IN')),
  ticket_id            uuid          NOT NULL,
  route                text          NOT NULL,
  mass_kg              numeric(14,3) NOT NULL,
  supersedes_ticket_id uuid          NULL,
  ticket_time          timestamptz   NOT NULL,
  last_seq             bigint        NOT NULL,
  PRIMARY KEY (tenant_id, lot_ref, point)
);
SELECT shared.apply_tenant_rls('mkg_orders.lot_ticket_current');

-- ---------------------------------------------------------------------------
-- 7. Tham số có phiên bản (g4-conventions §1.2 — mặc định TẠM, CHỜ KHÁCH)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_orders.param_version (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,                 -- 'param:<param_key>'
  seq               bigint        NOT NULL,
  param_key         text          NOT NULL CHECK (param_key IN ('orders.receipt.po_variance_warn_pct', 'orders.sales.price_approval_delta_pct',
                                                              'orders.sales.discount_approval_pct', 'orders.payment_term_days',
                                                              'orders.sales.stock_shortage_blocking')),
  version_id        uuid          NOT NULL,
  version_seq       integer       NOT NULL CHECK (version_seq >= 1),
  kind              text          NOT NULL CHECK (kind IN ('proposed', 'approved', 'rejected')),
  value             numeric(12,3) NOT NULL CHECK (value >= 0),
  effective_from    date          NOT NULL,
  provisional       boolean       NOT NULL,
  proposer_ref      uuid          NOT NULL,
  reason            text          NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
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
  CHECK (kind <> 'proposed' OR (actor_ref = proposer_ref AND reason IS NOT NULL)),
  CHECK (kind = 'proposed' OR actor_ref <> proposer_ref),             -- R-56 ở tầng DB
  CHECK (kind <> 'rejected' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX param_version_idem   ON mkg_orders.param_version (tenant_id, idem_key);
CREATE INDEX param_version_stream ON mkg_orders.param_version (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_orders.param_version', 'mkg_orders.param_version');

CREATE TABLE mkg_orders.param_current (
  tenant_id       uuid          NOT NULL,
  param_key       text          NOT NULL,
  version_id      uuid          NOT NULL,
  version_seq     integer       NOT NULL,
  value           numeric(12,3) NOT NULL,
  effective_from  date          NOT NULL,
  provisional     boolean       NOT NULL,
  status          text          NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
  proposed_by     uuid          NOT NULL,
  proposal_reason text          NOT NULL,
  decided_by      uuid          NULL,
  decided_at      timestamptz   NULL,
  decision_reason text          NULL,
  last_seq        bigint        NOT NULL,
  PRIMARY KEY (tenant_id, param_key, version_id),
  UNIQUE (tenant_id, param_key, version_seq),
  CHECK (decided_by IS NULL OR decided_by <> proposed_by)
);
CREATE INDEX param_current_effective ON mkg_orders.param_current (tenant_id, param_key, status, effective_from);
SELECT shared.apply_tenant_rls('mkg_orders.param_current');

-- ---------------------------------------------------------------------------
-- Quyền
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON mkg_orders.contract_event, mkg_orders.po_event, mkg_orders.receipt_event, mkg_orders.payable_event,
                        mkg_orders.order_event, mkg_orders.delivery_event, mkg_orders.ticket_event, mkg_orders.param_version TO orders_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_orders.contract_event, mkg_orders.po_event, mkg_orders.receipt_event, mkg_orders.payable_event,
                                   mkg_orders.order_event, mkg_orders.delivery_event, mkg_orders.ticket_event,
                                   mkg_orders.param_version FROM orders_app;
-- Bảng chiếu: được UPDATE (R-21); DELETE chỉ để dựng lại (R-22).
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_orders.contract_current, mkg_orders.po_current, mkg_orders.receipt_current,
                                        mkg_orders.order_current, mkg_orders.delivery_current, mkg_orders.lot_ticket_current,
                                        mkg_orders.param_current TO orders_app;
