-- mkg G4 — đóng khoảng trống mã (docs/dev/g4-conventions.md §2: số dự phòng 000021, dùng bởi agent G4-F):
--
--   mkg_hub.arrival_event          SỔ thông báo hàng đến (proto FM-05 "thông báo hàng đến cho_den → da_den mang mã chuyến"):
--                                  stream 'arrival:<trip_id>'. Ghi bởi consumer mkg.hub.trip_milestone khi mkg/logistics
--                                  phát mkg.logistics.trip.milestone_recorded v1 cho chuyến có đích là cơ sở (hub).
--                                  expected (= proto cho_den) → arrived (= proto da_den); cancelled khi chuyến bị hủy.
--   mkg_hub.arrival_current        bảng chiếu hàng sắp đến / đã đến theo chuyến (dựng lại từ arrival_event, R-22).
--   mkg_orders.file_object         bảng CHỈ GHI THÊM: siêu dữ liệu tệp chứng từ vùng mkg (biên bản chênh lệch của phiếu nhận,
--                                  ePOD / biên bản / e-bill của giao nhận) — nội dung ở kho đối tượng trong lãnh thổ
--                                  (shared/objstore, ghi một lần, khóa theo băm), kiểm SHA-256, loại + cỡ tệp giới hạn.
--   mkg_logistics.file_object      như trên cho chứng từ chuyến (ePOD / e-bill / biên bản, L-11).
--   mkg_logistics.trip_document    SỔ chứng từ chuyến có người ký + thời điểm ký (L-11, proto ePOD / e-bill): stream
--                                  'tripdoc:<trip_id>'; người ký là *_ref (vault tenant MKG, RB-3), không dữ liệu cá nhân.
--
-- Không có dữ liệu cá nhân (R-04). Tệp không mã hóa phong bì ở G4 (chứng từ nghiệp vụ không có trường định danh; chữ ký
-- trên ảnh quét → mã hóa phía kho (SSE) + khóa đối tượng; mã hóa phong bì cần dịch vụ khóa vùng mkg — mục mở).

-- ---------------------------------------------------------------------------
-- 1. Thông báo hàng đến (hub)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_hub.arrival_event (
  id                   uuid          NOT NULL,
  tenant_id            uuid          NOT NULL,
  stream_key           text          NOT NULL,              -- 'arrival:<trip_id>'
  seq                  bigint        NOT NULL,
  trip_id              uuid          NOT NULL,
  trip_code            text          NOT NULL CHECK (trip_code ~ '^CH[0-9]{6,}$'),
  facility_ref         uuid          NOT NULL,              -- cơ sở đích (chặng cuối của chuyến)
  kind                 text          NOT NULL CHECK (kind IN ('expected', 'arrived', 'cancelled')),
  milestone            text          NOT NULL CHECK (milestone IN ('departed', 'rerouted', 'arrived', 'completed', 'cancelled')),
  mode                 text          NOT NULL CHECK (mode IN ('road', 'waterway')),
  vessel_code          text          NULL CHECK (vessel_code IS NULL OR char_length(vessel_code) BETWEEN 1 AND 32),
  collection_order_ref uuid          NULL,
  lot_refs             uuid[]        NOT NULL CHECK (cardinality(lot_refs) <= 200),
  planned_kg           numeric(14,3) NULL CHECK (planned_kg IS NULL OR planned_kg >= 0),
  source_event_ref     uuid          NOT NULL,              -- dòng trip_event / leg_event của logistics
  event_time           timestamptz   NOT NULL,              -- thời điểm mốc (xuất phát / đến / hủy)
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
  CHECK (kind <> 'expected' OR milestone IN ('departed', 'rerouted')),
  CHECK (kind <> 'arrived' OR milestone IN ('arrived', 'completed')),
  CHECK (kind <> 'cancelled' OR milestone = 'cancelled')
) PARTITION BY RANGE (recorded_at);
CREATE INDEX arrival_event_idem   ON mkg_hub.arrival_event (tenant_id, idem_key);
CREATE INDEX arrival_event_stream ON mkg_hub.arrival_event (tenant_id, stream_key, seq);
CREATE INDEX arrival_event_trip   ON mkg_hub.arrival_event (tenant_id, trip_id, seq);
SELECT shared.apply_ledger('mkg_hub.arrival_event', 'mkg_hub.arrival_event');

CREATE TABLE mkg_hub.arrival_current (
  tenant_id            uuid          NOT NULL,
  trip_id              uuid          NOT NULL,
  trip_code            text          NOT NULL,
  facility_ref         uuid          NOT NULL,
  status               text          NOT NULL CHECK (status IN ('expected', 'arrived', 'cancelled')),
  mode                 text          NOT NULL,
  vessel_code          text          NULL,
  collection_order_ref uuid          NULL,
  lot_refs             uuid[]        NOT NULL,
  planned_kg           numeric(14,3) NULL,
  expected_since       timestamptz   NULL,                 -- mốc xuất phát (cho_den)
  arrived_at           timestamptz   NULL,                 -- mốc đến / hoàn tất (da_den)
  cancelled_at         timestamptz   NULL,
  last_event_id        uuid          NOT NULL,
  last_seq             bigint        NOT NULL,
  updated_at           timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, trip_id),
  CHECK (status <> 'arrived' OR arrived_at IS NOT NULL),
  CHECK (status <> 'cancelled' OR cancelled_at IS NOT NULL)
);
CREATE INDEX arrival_current_facility ON mkg_hub.arrival_current (tenant_id, facility_ref, status, trip_id DESC);
CREATE INDEX arrival_current_status   ON mkg_hub.arrival_current (tenant_id, status, trip_id DESC);
SELECT shared.apply_tenant_rls('mkg_hub.arrival_current');

GRANT SELECT, INSERT ON mkg_hub.arrival_event TO hub_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_hub.arrival_event FROM hub_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_hub.arrival_current TO hub_app;

-- ---------------------------------------------------------------------------
-- 2. Tệp chứng từ vùng mkg (orders, logistics) — chỉ ghi thêm
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_orders.file_object (
  tenant_id     uuid        NOT NULL,
  id            uuid        NOT NULL,                      -- file_ref
  purpose       text        NOT NULL CHECK (purpose IN ('receipt_variance_report', 'delivery_evidence')),
  object_key    text        NOT NULL CHECK (object_key ~ '^mkg/orders/'),
  sha256        text        NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  size_bytes    bigint      NOT NULL CHECK (size_bytes BETWEEN 1 AND 15728640),
  content_type  text        NOT NULL CHECK (content_type IN ('application/pdf', 'image/jpeg', 'image/png', 'image/webp')),
  file_name     text        NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 200),
  uploaded_by   uuid        NOT NULL,
  idem_key      text        NOT NULL,
  rule_version  text        NOT NULL,
  recorded_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, idem_key)
);
CREATE INDEX file_object_recent ON mkg_orders.file_object (tenant_id, recorded_at DESC, id DESC);
SELECT shared.apply_append_only('mkg_orders.file_object');
GRANT SELECT, INSERT ON mkg_orders.file_object TO orders_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_orders.file_object FROM orders_app;

CREATE TABLE mkg_logistics.file_object (
  tenant_id     uuid        NOT NULL,
  id            uuid        NOT NULL,                      -- file_ref
  purpose       text        NOT NULL CHECK (purpose IN ('trip_document')),
  object_key    text        NOT NULL CHECK (object_key ~ '^mkg/logistics/'),
  sha256        text        NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  size_bytes    bigint      NOT NULL CHECK (size_bytes BETWEEN 1 AND 15728640),
  content_type  text        NOT NULL CHECK (content_type IN ('application/pdf', 'image/jpeg', 'image/png', 'image/webp')),
  file_name     text        NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 200),
  uploaded_by   uuid        NOT NULL,
  idem_key      text        NOT NULL,
  rule_version  text        NOT NULL,
  recorded_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, idem_key)
);
CREATE INDEX file_object_recent ON mkg_logistics.file_object (tenant_id, recorded_at DESC, id DESC);
SELECT shared.apply_append_only('mkg_logistics.file_object');
GRANT SELECT, INSERT ON mkg_logistics.file_object TO logistics_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_logistics.file_object FROM logistics_app;

-- ---------------------------------------------------------------------------
-- 3. Chứng từ chuyến có người ký (logistics, L-11)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_logistics.trip_document (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                  -- 'tripdoc:<trip_id>'
  seq               bigint      NOT NULL,
  trip_id           uuid        NOT NULL,
  kind              text        NOT NULL CHECK (kind IN ('epod', 'ebill', 'minutes')),
  file_ref          uuid        NOT NULL,                  -- mkg_logistics.file_object.id
  file_sha256       text        NOT NULL CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
  signer_role       text        NOT NULL CHECK (signer_role IN ('driver', 'consignee', 'hub_operator', 'dispatcher')),
  signer_ref        uuid        NULL,                      -- người ký (sub / ref vault) — không tên, không số điện thoại
  signed_at         timestamptz NOT NULL,
  note              text        NULL CHECK (note IS NULL OR length(note) BETWEEN 1 AND 500),
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
CREATE INDEX trip_document_idem   ON mkg_logistics.trip_document (tenant_id, idem_key);
CREATE INDEX trip_document_stream ON mkg_logistics.trip_document (tenant_id, stream_key, seq);
CREATE INDEX trip_document_trip   ON mkg_logistics.trip_document (tenant_id, trip_id, seq);
SELECT shared.apply_ledger('mkg_logistics.trip_document', 'mkg_logistics.trip_document');
GRANT SELECT, INSERT ON mkg_logistics.trip_document TO logistics_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_logistics.trip_document FROM logistics_app;
