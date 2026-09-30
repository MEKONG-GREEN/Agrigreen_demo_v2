-- mkg G4 tích hợp (docs/dev/g4-conventions.md §2 — số dự phòng 000020 do người tích hợp dùng):
--
--   mkg_fieldops.roll_count_vessel     SỔ số hiệu ghe/sà lan tùy chọn của bản ghi xếp ghe (boat_load, proto FM-05) —
--                                      stream 'vessel:<roll_count_id>', cùng giao dịch với roll_count. mkg/logistics
--                                      nhận số hiệu qua trường tùy chọn vessel_code của roll_count.recorded v1.
--   mkg_logistics.trip_milestone_fix   SỔ độ chính xác GPS (accuracy_m) của mốc chuyến do app tài xế gửi — stream
--                                      'fix:<trip_event_id>'. Tọa độ vẫn ở trip_event.lat/lng (NULL khi máy không có
--                                      GPS, không bao giờ 0,0).
--   mkg_logistics.alert_notice         bảng vận hành CHỈ GHI THÊM: thông báo cảnh báo L-08 (ghe chưa cân > N giờ,
--                                      đăng kiểm hết hạn / sắp hết hạn) do job logistics_alerts của api-internal
--                                      ghi, một dòng mỗi (loại, đối tượng, mốc) — chống gửi lặp; không dữ liệu cá nhân.
--   mkg_logistics.trip_event           NỚI hai CHECK: mốc departed / arrived không còn bắt buộc lat/lng (app tài xế
--                                      không có GPS → NULL thay vì 0,0); thêm CHECK lat/lng cùng có hoặc cùng vắng.
--                                      Chỉ đổi ràng buộc, không đổi dữ liệu / cột (chuỗi băm không ảnh hưởng).
--
-- Sổ riêng thay vì thêm cột vào sổ đã có: chuỗi băm tính trên to_jsonb(dòng) nên cột mới làm hỏng verify_chain của
-- các dòng đã ghi (như roll_count_location, mkg 000010).

-- ---------------------------------------------------------------------------
-- 1. Số hiệu ghe của bản ghi xếp ghe (fieldops)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_fieldops.roll_count_vessel (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'vessel:<roll_count_id>'
  seq               bigint      NOT NULL,
  roll_count_id     uuid        NOT NULL,
  order_id          uuid        NOT NULL,
  vessel_code       text        NOT NULL CHECK (vessel_code = upper(vessel_code) AND vessel_code !~ '\s'
                                                AND char_length(vessel_code) BETWEEN 1 AND 32),
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
CREATE INDEX roll_count_vessel_idem   ON mkg_fieldops.roll_count_vessel (tenant_id, idem_key);
CREATE INDEX roll_count_vessel_stream ON mkg_fieldops.roll_count_vessel (tenant_id, stream_key, seq);
CREATE INDEX roll_count_vessel_order  ON mkg_fieldops.roll_count_vessel (tenant_id, order_id);
CREATE INDEX roll_count_vessel_count  ON mkg_fieldops.roll_count_vessel (tenant_id, roll_count_id);
SELECT shared.apply_ledger('mkg_fieldops.roll_count_vessel', 'mkg_fieldops.roll_count_vessel');

GRANT SELECT, INSERT ON mkg_fieldops.roll_count_vessel TO fieldops_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_fieldops.roll_count_vessel FROM fieldops_app;

-- ---------------------------------------------------------------------------
-- 2. Độ chính xác GPS của mốc chuyến (logistics)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_logistics.trip_milestone_fix (
  id                uuid         NOT NULL,
  tenant_id         uuid         NOT NULL,
  stream_key        text         NOT NULL,                -- 'fix:<trip_event_id>'
  seq               bigint       NOT NULL,
  trip_id           uuid         NOT NULL,
  trip_event_id     uuid         NOT NULL,                -- dòng departed / arrived của trip_event
  accuracy_m        numeric(9,2) NOT NULL CHECK (accuracy_m >= 0),
  event_time        timestamptz  NOT NULL,
  recorded_at       timestamptz  NOT NULL DEFAULT now(),
  supersedes_id     uuid         NULL,
  correction_reason text         NULL,
  actor_ref         uuid         NOT NULL,
  device_id         text         NULL,
  idem_key          text         NOT NULL,
  rule_version      text         NOT NULL,
  prev_hash         bytea        NOT NULL,
  row_hash          bytea        NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX trip_milestone_fix_idem   ON mkg_logistics.trip_milestone_fix (tenant_id, idem_key);
CREATE INDEX trip_milestone_fix_stream ON mkg_logistics.trip_milestone_fix (tenant_id, stream_key, seq);
CREATE INDEX trip_milestone_fix_trip   ON mkg_logistics.trip_milestone_fix (tenant_id, trip_id);
SELECT shared.apply_ledger('mkg_logistics.trip_milestone_fix', 'mkg_logistics.trip_milestone_fix');

GRANT SELECT, INSERT ON mkg_logistics.trip_milestone_fix TO logistics_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_logistics.trip_milestone_fix FROM logistics_app;

-- ---------------------------------------------------------------------------
-- 3. Thông báo cảnh báo L-08 (logistics, bảng vận hành chỉ ghi thêm)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_logistics.alert_notice (
  tenant_id     uuid        NOT NULL,
  id            uuid        NOT NULL,
  kind          text        NOT NULL CHECK (kind IN ('trip_unweighed', 'registration_expired', 'registration_expiring')),
  subject_ref   uuid        NOT NULL,                     -- trip_id | vessel_id
  subject_code  text        NOT NULL,                     -- mã chuyến CH… | số hiệu phương tiện
  dedupe_key    text        NOT NULL,                     -- '<kind>:<subject>' (chuyến) | '<kind>:<vessel>:<hạn đăng kiểm>'
  audience      text[]      NOT NULL CHECK (cardinality(audience) BETWEEN 1 AND 10),   -- vai nhận (dispatcher, hub_operator…)
  detail        jsonb       NOT NULL CHECK (jsonb_typeof(detail) = 'object'),
  param_version text        NOT NULL,                     -- tham số ngưỡng đã áp (unweighed_alert_hours / expiry_warn_days)
  rule_version  text        NOT NULL,
  noticed_at    timestamptz NOT NULL,
  recorded_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, dedupe_key)
);
CREATE INDEX alert_notice_recent ON mkg_logistics.alert_notice (tenant_id, noticed_at DESC, id DESC);
SELECT shared.apply_append_only('mkg_logistics.alert_notice');

GRANT SELECT, INSERT ON mkg_logistics.alert_notice TO logistics_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_logistics.alert_notice FROM logistics_app;

-- Job logistics_alerts (api-internal, leader) tìm tenant có chuyến mở / phương tiện đang dùng — vai
-- ag_outbox_publisher chỉ đọc hai cột (như mkg_hub.facility_current, mkg 000005; mkg_acctbridge 000019).
GRANT USAGE ON SCHEMA mkg_logistics TO ag_outbox_publisher;
GRANT SELECT (tenant_id, status) ON mkg_logistics.trip_current TO ag_outbox_publisher;
GRANT SELECT (tenant_id, status) ON mkg_logistics.vessel_current TO ag_outbox_publisher;

-- ---------------------------------------------------------------------------
-- 4. Mốc chuyến không GPS (logistics): nới CHECK departed / arrived của trip_event
-- ---------------------------------------------------------------------------
-- CHECK của 000015 không đặt tên → tìm theo định nghĩa (trên bảng cha; phân vùng kế thừa và bị bỏ theo).
DO $$
DECLARE
  r record;
BEGIN
  FOR r IN SELECT conname FROM pg_constraint
           WHERE conrelid = 'mkg_logistics.trip_event'::regclass AND contype = 'c'
             AND pg_get_constraintdef(oid) ~ '''(departed|arrived)''::text'
             AND pg_get_constraintdef(oid) ~ 'lat IS NOT NULL'
  LOOP
    EXECUTE format('ALTER TABLE mkg_logistics.trip_event DROP CONSTRAINT %I', r.conname);
  END LOOP;
END $$;
ALTER TABLE mkg_logistics.trip_event
  ADD CONSTRAINT trip_event_departed_status CHECK (kind <> 'departed' OR status = 'departed'),
  ADD CONSTRAINT trip_event_arrived_status  CHECK (kind <> 'arrived' OR status = 'arrived'),
  ADD CONSTRAINT trip_event_latlng_pair     CHECK ((lat IS NULL) = (lng IS NULL));
