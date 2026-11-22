-- pf/extension — CNTS-V4-CHECKOUT-PHOTO (#249), BRD CNTS v4.0 FN-12 (CV-07, CV-08, BR-06, BR-07, BR-08, AC-09, AC-11..13):
-- check-in BẮT BUỘC ảnh; check-out (sau check-in, kèm ảnh, GPS, thời điểm, độ chính xác; không sửa được).
--
-- 1. work_schedule_checkin (sổ) thêm 4 cột ảnh, NULL được (ADR-0012: đăng ký register_ledger_columns để verify_chain
--    băm lại dòng cũ). Từ bản này mọi check-in mới có ảnh (dịch vụ chặn khi thiếu). Dòng có photo_key NULL là
--    DỮ LIỆU CŨ (check-in trước BRD v4.0, không ảnh): giữ nguyên, API trả "legacy_no_photo": true, không bắt nhập bù.
--    Bốn cột cùng NULL hoặc cùng có giá trị.
-- 2. work_schedule_checkout — sổ chỉ ghi thêm: SỰ VIỆC check-out của một mục công tác (chuỗi 'wcheckout:<item_id>',
--    tối đa một check-out mỗi mục, tham chiếu check-in của mục). Như check-in: KHÔNG có tọa độ trong sổ (QĐ-8) —
--    tọa độ (cờ extension.work_schedule_checkin_store_point) ghi vào bảng có hạn lưu work_schedule_checkin_point với
--    checkin_id = id của check-out (UUIDv7 không trùng), job extension_work_schedule xóa khi quá hạn như tọa độ
--    check-in. Ảnh bắt buộc. recorded_at là thời điểm đồng bộ; captured_at là giờ thiết bị lúc bấm (BR-08: ngày
--    ghi nhận theo giờ Việt Nam sau ngày công tác → "Đồng bộ trễ", suy ra ở dịch vụ, không lưu cờ thừa).
-- Ảnh: bản rõ ở kho đối tượng (shared/objstore, cùng kho tệp của module — như ảnh thực địa KNQG), sổ chỉ giữ khóa,
-- kiểu, cỡ, SHA-256. Ảnh nén ≤ 500 KB (BRD Mục VIII "Định vị, ảnh").
-- Vai r_extension: sổ check-out SELECT, INSERT.

SET LOCAL lock_timeout = '5s';

ALTER TABLE pf_extension.work_schedule_checkin
  ADD COLUMN photo_key          text    NULL CHECK (photo_key IS NULL OR char_length(photo_key) BETWEEN 1 AND 400),
  ADD COLUMN photo_content_type text    NULL CHECK (photo_content_type IS NULL OR photo_content_type IN ('image/jpeg', 'image/png', 'image/webp')),
  ADD COLUMN photo_size_bytes   integer NULL CHECK (photo_size_bytes IS NULL OR photo_size_bytes BETWEEN 1 AND 512000),
  ADD COLUMN photo_sha256       text    NULL CHECK (photo_sha256 IS NULL OR photo_sha256 ~ '^[0-9a-f]{64}$');
SELECT shared.register_ledger_columns('pf_extension.work_schedule_checkin',
  ARRAY['photo_key', 'photo_content_type', 'photo_size_bytes', 'photo_sha256']);
ALTER TABLE pf_extension.work_schedule_checkin ADD CONSTRAINT work_schedule_checkin_photo_all
  CHECK ((photo_key IS NULL) = (photo_content_type IS NULL) AND (photo_key IS NULL) = (photo_size_bytes IS NULL)
     AND (photo_key IS NULL) = (photo_sha256 IS NULL)) NOT VALID;
ALTER TABLE pf_extension.work_schedule_checkin VALIDATE CONSTRAINT work_schedule_checkin_photo_all;

CREATE TABLE pf_extension.work_schedule_checkout (
  id                  uuid          NOT NULL,             -- UUIDv7 — mã check-out
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'wcheckout:<item_id>'
  seq                 bigint        NOT NULL,
  schedule_id         uuid          NOT NULL,
  item_id             uuid          NOT NULL,
  item_version        integer       NOT NULL CHECK (item_version >= 1),
  checkin_id          uuid          NOT NULL,             -- = work_schedule_checkin.id của mục
  owner_ref           uuid          NOT NULL,
  department_code     text          NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  day                 date          NOT NULL,             -- ngày công tác của mục
  captured_at         timestamptz   NOT NULL,             -- giờ thiết bị lúc bấm "Check-out" (AC-11: giữ khi gửi muộn)
  accuracy_m          numeric(8,2)  NULL CHECK (accuracy_m IS NULL OR accuracy_m >= 0),
  distance_m          numeric(12,1) NULL CHECK (distance_m IS NULL OR distance_m >= 0),   -- tới địa điểm đã đăng ký (CV-06)
  far                 boolean       NOT NULL,             -- distance_m > ngưỡng (mặc định 500 m)
  weak_accuracy       boolean       NOT NULL,
  mock_location       boolean       NOT NULL,
  point_stored        boolean       NOT NULL,             -- có dòng work_schedule_checkin_point (checkin_id = id)
  device_id           text          NULL CHECK (device_id IS NULL OR char_length(device_id) <= 200),
  photo_key           text          NOT NULL CHECK (char_length(photo_key) BETWEEN 1 AND 400),
  photo_content_type  text          NOT NULL CHECK (photo_content_type IN ('image/jpeg', 'image/png', 'image/webp')),
  photo_size_bytes    integer       NOT NULL CHECK (photo_size_bytes BETWEEN 1 AND 512000),
  photo_sha256        text          NOT NULL CHECK (photo_sha256 ~ '^[0-9a-f]{64}$'),
  event_time          timestamptz   NOT NULL,
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  idem_key            text          NOT NULL,
  rule_version        text          NOT NULL,
  prev_hash           bytea         NOT NULL,
  row_hash            bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT work_schedule_checkout_stream CHECK (stream_key = 'wcheckout:' || item_id::text),
  CONSTRAINT work_schedule_checkout_far CHECK (NOT far OR distance_m IS NOT NULL),
  CONSTRAINT work_schedule_checkout_owner CHECK (actor_ref = owner_ref)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX work_schedule_checkout_schedule ON pf_extension.work_schedule_checkout (tenant_id, schedule_id);
CREATE INDEX work_schedule_checkout_item ON pf_extension.work_schedule_checkout (tenant_id, item_id);
SELECT shared.apply_ledger('pf_extension.work_schedule_checkout', 'pf_extension.work_schedule_checkout');

REVOKE ALL ON pf_extension.work_schedule_checkout FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.work_schedule_checkout TO r_extension;
