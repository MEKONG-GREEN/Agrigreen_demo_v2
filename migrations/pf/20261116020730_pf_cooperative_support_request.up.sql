-- pf/cooperative — PA1-HTX-SUPPORT-GPSLOG (#313, kế hoạch PA1 bước 2, Q7): YÊU CẦU HỖ TRỢ của nông dân App HTX
-- (HTX US-SUPPORT-01) về module chủ HTX. Trước đây ở pf_extension.support_request (pf 000026, module KN) và tự sinh
-- nhiệm vụ cho cán bộ khuyến nông; từ đây HTX tự giữ, không còn cột nhiệm vụ KN (KN chạy độc lập, quyết định 07/11).
--
-- Nông dân gửi trực tuyến (POST /v1/support-requests) hoặc ngoại tuyến (pf/sync entity support_request). Cùng giao
-- dịch ghi phát pf.cooperative.support_request.submitted; consumer pf.cropcycle.gps_log ghi log GPS qua pf/geo sau
-- commit (HTX US-GPS-03, mục đích support). Quản lý HTX xem danh sách HTX mình (GET /v1/cropcycle/support-requests).
--
-- Yêu cầu là sự việc: CHỈ GHI THÊM (chặn UPDATE/DELETE/TRUNCATE). sync_seq lấy lúc ghi là con trỏ chiều xuống.
-- legacy_task_id, legacy_task_status: chỉ dòng chép một lần từ bảng KN (migration 20261116020731) — nhiệm vụ KN và
-- trạng thái của nó lúc chép, để App HTX giữ nhãn cũ; dòng mới để NULL.
-- Không dữ liệu cá nhân (R-04): nông hộ, HTX, thửa, ảnh là *_ref; mô tả chặn SĐT/CCCD ở tầng nghiệp vụ.
SET LOCAL lock_timeout = '5s';

CREATE SEQUENCE pf_coop.support_request_sync_seq;

CREATE TABLE pf_coop.support_request (
  tenant_id          uuid                  NOT NULL,
  id                 uuid                  NOT NULL,                 -- UUIDv7 sinh trên máy (R-11)
  household_ref      uuid                  NOT NULL,
  cooperative_ref    uuid                  NOT NULL,
  commune_code       text                  NOT NULL,
  province_code      text                  NOT NULL,
  plot_ref           uuid                  NULL,
  category           text                  NOT NULL CHECK (category IN ('dich_hai', 'benh_lua', 'thoi_tiet', 'ky_thuat', 'may_moc', 'khac')),
  urgency            text                  NOT NULL CHECK (urgency IN ('binh_thuong', 'khan')),
  description        text                  NOT NULL CHECK (char_length(description) BETWEEN 10 AND 2000),
  photo_refs         uuid[]                NOT NULL DEFAULT '{}' CHECK (cardinality(photo_refs) <= 5),
  location           geometry(Point, 4326) NULL,                     -- NULL = "Thiếu vị trí GPS" (AC-1)
  gps_accuracy_m     numeric(14,4)         NULL CHECK (gps_accuracy_m IS NULL OR gps_accuracy_m >= 0),
  event_time         timestamptz           NOT NULL,                 -- R-13: lúc nông dân bấm Gửi (có thể ngoại tuyến)
  received_at        timestamptz           NOT NULL,                 -- giờ máy chủ nhận
  recorded_at        timestamptz           NOT NULL DEFAULT now(),
  submitted_by       uuid                  NOT NULL,                 -- actor_ref của nông dân
  device_id          text                  NULL,
  idem_key           text                  NOT NULL,
  sync_seq           bigint                NOT NULL DEFAULT nextval('pf_coop.support_request_sync_seq'),
  legacy_task_id     uuid                  NULL,
  legacy_task_status text                  NULL CHECK (legacy_task_status IS NULL OR legacy_task_status IN ('new', 'assigned', 'accepted', 'in_progress', 'resolved')),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, idem_key),
  CHECK (gps_accuracy_m IS NULL OR location IS NOT NULL),
  CHECK (legacy_task_status IS NULL OR legacy_task_id IS NOT NULL)
);
CREATE INDEX support_request_household ON pf_coop.support_request (tenant_id, household_ref, sync_seq);
CREATE INDEX support_request_cooperative ON pf_coop.support_request (tenant_id, cooperative_ref, id DESC);
SELECT shared.apply_tenant_rls('pf_coop.support_request');

CREATE TRIGGER trg_no_update BEFORE UPDATE ON pf_coop.support_request
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_coop.support_request
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_coop.support_request
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

REVOKE ALL ON pf_coop.support_request FROM PUBLIC;
GRANT SELECT, INSERT ON pf_coop.support_request TO r_cooperative;
GRANT USAGE ON SEQUENCE pf_coop.support_request_sync_seq TO r_cooperative;
