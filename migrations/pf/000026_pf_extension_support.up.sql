-- pf_extension G2: yêu cầu hỗ trợ từ App HTX (HTX US-SUPPORT-01) tự sinh nhiệm vụ
-- cho cán bộ khuyến nông trong vòng 1 phút (KN US-TASK-01).
--
-- Nông dân gửi yêu cầu (trực tuyến hoặc ngoại tuyến qua pf/sync, entity
-- support_request). Khi máy chủ NHẬN (received_at, giờ máy chủ), trong CÙNG giao
-- dịch: ghi yêu cầu, tạo nhiệm vụ (task_event + task_current, source = htx_app)
-- giao cho cán bộ xã đang trực (roster DutyAt), không có → cán bộ cấp tỉnh, không
-- có → để trống và báo quản trị tỉnh gán lại; phát pf.extension.support_request.submitted
-- và pf.extension.task.created.
--
-- Không có dữ liệu cá nhân (R-04): nông hộ, HTX, thửa, ảnh là *_ref. Mô tả tự do
-- bị chặn nếu chứa SĐT/CCCD (domain.ContainsPII). Yêu cầu là sự việc: chỉ ghi thêm,
-- trạng thái xử lý nằm ở nhiệm vụ.

CREATE TABLE pf_extension.support_request (
  tenant_id        uuid          NOT NULL,
  id               uuid          NOT NULL,                  -- UUIDv7 sinh trên máy (R-11)
  household_ref    uuid          NOT NULL,
  cooperative_ref  uuid          NOT NULL,
  commune_code     text          NOT NULL,
  province_code    text          NOT NULL,
  plot_ref         uuid          NULL,
  category         text          NOT NULL CHECK (category IN ('dich_hai', 'benh_lua', 'thoi_tiet', 'ky_thuat', 'may_moc', 'khac')),
  urgency          text          NOT NULL CHECK (urgency IN ('binh_thuong', 'khan')),
  description      text          NOT NULL CHECK (char_length(description) BETWEEN 10 AND 2000),
  photo_refs       uuid[]        NOT NULL DEFAULT '{}' CHECK (cardinality(photo_refs) <= 5),  -- tệp pf_files (HTX US-SUPPORT-01 AC-3)
  location         geometry(Point, 4326) NULL,             -- GPS lô ruộng; NULL = "Thiếu vị trí GPS" (AC-1)
  gps_accuracy_m   numeric(14,4) NULL CHECK (gps_accuracy_m IS NULL OR gps_accuracy_m >= 0),
  event_time       timestamptz   NOT NULL,                  -- R-13: lúc nông dân bấm Gửi (có thể ngoại tuyến, AC-2)
  received_at      timestamptz   NOT NULL,                  -- giờ máy chủ nhận — mốc của SLA 1 phút (KN US-TASK-01)
  recorded_at      timestamptz   NOT NULL DEFAULT now(),
  task_id          uuid          NOT NULL,                  -- nhiệm vụ sinh cùng giao dịch
  task_created_at  timestamptz   NOT NULL,
  assignee_ref     uuid          NULL,
  assignment_rule  text          NOT NULL CHECK (assignment_rule IN ('commune_on_duty', 'province_on_duty', 'province_officer', 'admin_queue')),
  submitted_by     uuid          NOT NULL,                  -- actor_ref của nông dân
  device_id        text          NULL,
  idem_key         text          NOT NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, idem_key),
  UNIQUE (tenant_id, task_id),
  CHECK (task_created_at >= received_at),
  CHECK (gps_accuracy_m IS NULL OR location IS NOT NULL),
  CHECK ((assignment_rule = 'admin_queue') = (assignee_ref IS NULL))
);
CREATE INDEX support_request_household ON pf_extension.support_request (tenant_id, household_ref, received_at DESC);
CREATE INDEX support_request_commune   ON pf_extension.support_request (tenant_id, commune_code, received_at DESC);
SELECT shared.apply_tenant_rls('pf_extension.support_request');

-- Chỉ ghi thêm: yêu cầu là sự việc đã xảy ra (RB-1).
CREATE TRIGGER trg_no_update BEFORE UPDATE ON pf_extension.support_request
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_extension.support_request
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_extension.support_request
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

GRANT SELECT, INSERT ON pf_extension.support_request TO extension_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_extension.support_request FROM extension_app;
