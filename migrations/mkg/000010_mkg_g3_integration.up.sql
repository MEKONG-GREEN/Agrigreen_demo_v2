-- mkg G3-2 tích hợp (docs/dev/g3-conventions.md §2, docs/design/mkg.md §5.2):
-- nối số cuộn của mkg/fieldops với phiếu cân của mkg/measurement.
--
--   mkg_measurement.lot_collection          SỔ liên kết TƯỜNG MINH lô cân ↔ lệnh gom (lô gom của fieldops)
--                                           — stream 'collection:<lot_ref>'. Ghi khi mở phiên cân có
--                                           collection_order_ref hoặc qua POST …/lots/{id}/collection. Đổi
--                                           liên kết = bản ghi bù (supersedes_id + lý do, RB-1).
--   mkg_measurement.lot_collection_current  bảng chiếu: một lô ↔ một lệnh gom (và ngược lại) trong tenant.
--                                           Không có dòng = quy ước đồng nhất (lot_ref = mã lệnh gom).
--   mkg_fieldops.weighed_loading_current    THÊM cột nullable ticket_ref / supersedes_ticket_ref: bảng đệm
--                                           học kg/cuộn được consumer mkg.fieldops.weigh_ticket cập nhật
--                                           theo phiếu; phiếu gốc tới sau phiếu bù bị bỏ (stale).
--   mkg_fieldops.roll_count_location        SỔ dấu GPS tùy chọn của bản ghi đếm cuộn (app hiện trường).
--
-- Không thêm cột vào sổ đã có (chuỗi băm tính trên to_jsonb(dòng)). weighed_loading_current
-- là bảng chiếu (không băm) nên thêm cột nullable được. Không có dữ liệu cá nhân (R-04).

-- ---------------------------------------------------------------------------
-- 1. Liên kết lô cân ↔ lệnh gom
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_measurement.lot_collection (
  id                   uuid        NOT NULL,
  tenant_id            uuid        NOT NULL,
  stream_key           text        NOT NULL,                 -- 'collection:<lot_ref>'
  seq                  bigint      NOT NULL,
  lot_ref              uuid        NOT NULL,                 -- lô cân (weigh_ticket.lot_ref; lô hub khi qua hub)
  collection_order_ref uuid        NOT NULL,                 -- lệnh gom = lô gom của mkg/fieldops (R-06, UUID thuần)
  origin               text        NOT NULL CHECK (origin IN ('session', 'manual')),
  session_ref          uuid        NULL,                     -- phiên cân mang liên kết (origin = session)
  event_time           timestamptz NOT NULL,
  recorded_at          timestamptz NOT NULL DEFAULT now(),
  supersedes_id        uuid        NULL,
  correction_reason    text        NULL,
  actor_ref            uuid        NOT NULL,
  device_id            text        NULL,
  idem_key             text        NOT NULL,
  rule_version         text        NOT NULL,
  prev_hash            bytea       NOT NULL,
  row_hash             bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) BETWEEN 10 AND 500),
  CHECK ((origin = 'session') = (session_ref IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX lot_collection_idem   ON mkg_measurement.lot_collection (tenant_id, idem_key);
CREATE INDEX lot_collection_stream ON mkg_measurement.lot_collection (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('mkg_measurement.lot_collection', 'mkg_measurement.lot_collection');

CREATE TABLE mkg_measurement.lot_collection_current (
  tenant_id            uuid        NOT NULL,
  lot_ref              uuid        NOT NULL,
  link_id              uuid        NOT NULL,
  collection_order_ref uuid        NOT NULL,
  last_seq             bigint      NOT NULL,
  updated_at           timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, lot_ref)
);
-- Một lệnh gom thuộc tối đa một lô cân (G3: một lệnh gom = một lần xếp hàng / một lô).
CREATE UNIQUE INDEX lot_collection_current_order ON mkg_measurement.lot_collection_current (tenant_id, collection_order_ref);
SELECT shared.apply_tenant_rls('mkg_measurement.lot_collection_current');

GRANT SELECT, INSERT ON mkg_measurement.lot_collection TO measurement_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_measurement.lot_collection FROM measurement_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_measurement.lot_collection_current TO measurement_app;
-- Ingest (TicketWriter.Record) chỉ ĐỌC liên kết để ghi collection_order_ref vào sự kiện
-- nội bộ mkg.measurement.weigh_ticket.recorded.
GRANT SELECT ON mkg_measurement.lot_collection_current TO ingest_mkg;

-- ---------------------------------------------------------------------------
-- 2. Bảng đệm học kg/cuộn của fieldops: phiếu nguồn
-- ---------------------------------------------------------------------------
ALTER TABLE mkg_fieldops.weighed_loading_current
  ADD COLUMN ticket_ref            uuid NULL,               -- phiếu cân (HUB_IN, hoặc CUSTOMER_IN tuyến DIRECT)
  ADD COLUMN supersedes_ticket_ref uuid NULL;               -- phiếu mà ticket_ref thay (phiếu bù)

-- ---------------------------------------------------------------------------
-- 3. Dấu GPS của bản ghi đếm cuộn (app hiện trường MKG, tùy chọn, không bao giờ chặn lưu)
-- ---------------------------------------------------------------------------
-- SỔ riêng thay vì thêm cột vào roll_count: chuỗi băm của sổ tính trên to_jsonb(dòng) nên cột
-- mới làm hỏng verify_chain của các dòng đã ghi. Một dòng mỗi bản ghi đếm có gửi gps/gps_status,
-- cùng giao dịch với roll_count. stream 'location:<roll_count_id>'.
CREATE TABLE mkg_fieldops.roll_count_location (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,
  seq               bigint        NOT NULL,
  roll_count_id     uuid          NOT NULL,
  order_id          uuid          NOT NULL,
  gps_status        text          NOT NULL CHECK (gps_status IN ('ok', 'weak', 'denied', 'unavailable', 'timeout', 'no_device',
                                                                  'invalid', 'unknown')),
  lat               numeric(10,7) NULL CHECK (lat BETWEEN -90 AND 90),
  lng               numeric(10,7) NULL CHECK (lng BETWEEN -180 AND 180),
  accuracy_m        numeric(9,2)  NULL CHECK (accuracy_m >= 0),
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
  CHECK ((lat IS NULL) = (lng IS NULL)),
  CHECK (accuracy_m IS NULL OR lat IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX roll_count_location_idem   ON mkg_fieldops.roll_count_location (tenant_id, idem_key);
CREATE INDEX roll_count_location_stream ON mkg_fieldops.roll_count_location (tenant_id, stream_key, seq);
CREATE INDEX roll_count_location_order  ON mkg_fieldops.roll_count_location (tenant_id, order_id);
SELECT shared.apply_ledger('mkg_fieldops.roll_count_location', 'mkg_fieldops.roll_count_location');

GRANT SELECT, INSERT ON mkg_fieldops.roll_count_location TO fieldops_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_fieldops.roll_count_location FROM fieldops_app;
