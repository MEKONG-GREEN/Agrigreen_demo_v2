-- pf_reports: mẫu biểu có phiên bản, việc xuất báo cáo bất đồng bộ (mục 12,
-- R-52), nhật ký điểm kiểm soát xuất dữ liệu (DLP, mục 18) và lịch gửi báo cáo
-- định kỳ (KN US-DASH-03).
--
-- Nguồn: BRD App Khuyến nông v1.0 FR-01..FR-05, FR-27; Backlog KN US-DASH-02,
-- US-DASH-03; BRD Bản đồ CGH v1.3 FN-10 (3 tab, xuất từ kết quả ĐÃ LƯU).
--
-- KHÔNG có dữ liệu cá nhân ở đây (R-04): người nhận lịch gửi là actor_ref;
-- địa chỉ email chỉ tra lúc gửi qua cổng MailDirectory, không lưu.
--
--   1. template_version  mẫu biểu có phiên bản (chỉ ghi thêm); mẫu hệ thống nằm trong mã (phiên bản 0)
--   2. export_job        việc xuất: bộ lọc + phạm vi ĐÃ GIẢI + ảnh chụp mẫu lúc yêu cầu
--   3. export_audit      nhật ký điểm kiểm soát xuất (chỉ ghi thêm) — nguồn đếm hạn mức ngày
--   4. schedule          lịch gửi báo cáo định kỳ (ngừng dùng bằng trạng thái, không xóa)
--   5. schedule_run      mỗi mốc lịch chạy đúng một lần (khóa chính = lịch × mốc)

CREATE SCHEMA pf_reports;
REVOKE ALL ON SCHEMA pf_reports FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_reports TO reports_app;

-- ---------------------------------------------------------------------------
-- 1. Mẫu biểu. Mỗi lần Admin sửa là một phiên bản mới; việc xuất ghim phiên bản
-- (ảnh chụp định nghĩa nằm trên export_job). status = retired: ngừng dùng kèm lý do.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_reports.template_version (
  tenant_id     uuid        NOT NULL,
  template_key  text        NOT NULL CHECK (template_key ~ '^[a-z][a-z0-9_.]{2,63}$'),
  version       integer     NOT NULL CHECK (version >= 1),
  status        text        NOT NULL CHECK (status IN ('active', 'retired')),
  definition    jsonb       NOT NULL,                     -- tiêu đề, nguồn, cấp tổng hợp, cột, định dạng
  reason        text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 10 AND 500),
  created_by    uuid        NOT NULL,
  created_at    timestamptz NOT NULL,
  recorded_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, template_key, version),
  CHECK (status <> 'retired' OR reason IS NOT NULL)
);
SELECT shared.apply_tenant_rls('pf_reports.template_version');
CREATE TRIGGER trg_no_update BEFORE UPDATE OR DELETE ON pf_reports.template_version
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_reports.template_version
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 2. Việc xuất (mẫu mục 12). params và scope_* là ảnh chụp ĐÃ GIẢI lúc yêu cầu:
-- worker không cần token của người gọi và dựng lại được đúng tệp.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_reports.export_job (
  id               uuid        PRIMARY KEY,                -- UUIDv7 = job_id
  tenant_id        uuid        NOT NULL,
  template_key     text        NOT NULL,
  template_version integer     NOT NULL CHECK (template_version >= 0),  -- 0 = mẫu hệ thống
  template_def     jsonb       NOT NULL,
  format           text        NOT NULL CHECK (format IN ('xlsx', 'pdf')),
  params           jsonb       NOT NULL,
  scope_all        boolean     NOT NULL,
  scope_units      text[]      NOT NULL,
  requested_by     uuid        NOT NULL,
  requested_at     timestamptz NOT NULL DEFAULT now(),     -- "Ngày xuất" in trên tệp
  trigger_kind     text        NOT NULL CHECK (trigger_kind IN ('api', 'schedule')),
  schedule_id      uuid        NULL,
  schedule_slot    text        NULL,
  idem_key         text        NOT NULL,
  status           text        NOT NULL CHECK (status IN ('queued', 'completed', 'failed')),
  object_key       text        NULL,
  sha256           text        NULL,
  size_bytes       bigint      NULL,
  row_count        integer     NULL,
  provisional      boolean     NULL,                       -- "Số liệu tạm tính"
  error_code       text        NULL,
  completed_at     timestamptz NULL,
  attempts         integer     NOT NULL DEFAULT 0,
  UNIQUE (tenant_id, idem_key),
  CHECK ((trigger_kind = 'schedule') = (schedule_id IS NOT NULL AND schedule_slot IS NOT NULL)),
  CHECK ((status = 'completed') = (sha256 IS NOT NULL AND object_key IS NOT NULL AND completed_at IS NOT NULL)),
  CHECK (status <> 'failed' OR error_code IS NOT NULL)
);
CREATE INDEX export_job_requester ON pf_reports.export_job (tenant_id, requested_by, requested_at DESC);
SELECT shared.apply_tenant_rls('pf_reports.export_job');

CREATE FUNCTION pf_reports.fn_export_job_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status IN ('completed', 'failed') THEN
    RAISE EXCEPTION 'export_job %: việc xuất đã % là bất biến', OLD.id, OLD.status
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id OR NEW.template_key <> OLD.template_key
     OR NEW.template_version <> OLD.template_version OR NEW.template_def <> OLD.template_def
     OR NEW.format <> OLD.format OR NEW.params <> OLD.params OR NEW.scope_all <> OLD.scope_all
     OR NEW.scope_units <> OLD.scope_units OR NEW.requested_by <> OLD.requested_by
     OR NEW.requested_at <> OLD.requested_at OR NEW.idem_key <> OLD.idem_key
     OR NEW.trigger_kind <> OLD.trigger_kind THEN
    RAISE EXCEPTION 'export_job %: yêu cầu xuất bất biến, chỉ đổi được trạng thái', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_export_job_guard BEFORE UPDATE ON pf_reports.export_job
  FOR EACH ROW EXECUTE FUNCTION pf_reports.fn_export_job_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_reports.export_job
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_reports.export_job
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 3. Nhật ký điểm kiểm soát xuất dữ liệu (mục 18 "Phòng chống thất thoát dữ
-- liệu"): mọi yêu cầu xuất, xuất theo lịch và lần cấp URL tải đi qua MỘT hàm
-- (app.Service.egress) ghi một dòng ở đây. Không PII: chỉ actor_ref và mã mẫu.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_reports.export_audit (
  tenant_id     uuid        NOT NULL,
  id            uuid        NOT NULL,
  kind          text        NOT NULL CHECK (kind IN ('export', 'scheduled', 'download')),
  actor_ref     uuid        NOT NULL,
  template_key  text        NOT NULL,
  format        text        NOT NULL,
  job_id        uuid        NOT NULL,
  business_date date        NOT NULL,                      -- ngày giờ Việt Nam — đơn vị của hạn mức ngày
  daily_count   integer     NOT NULL CHECK (daily_count >= 0),  -- số lần xuất của người này trong ngày, gồm lần này
  anomaly       boolean     NOT NULL DEFAULT false,        -- lần này vượt ngưỡng bất thường
  event_time    timestamptz NOT NULL,
  recorded_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id)
);
CREATE INDEX export_audit_quota ON pf_reports.export_audit (tenant_id, actor_ref, business_date, kind);
SELECT shared.apply_tenant_rls('pf_reports.export_audit');
CREATE TRIGGER trg_no_update BEFORE UPDATE OR DELETE ON pf_reports.export_audit
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_reports.export_audit
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 4. Lịch gửi báo cáo định kỳ (US-DASH-03). Người nhận là actor_ref của tài
-- khoản nền tảng (không lưu email). scope_* là phạm vi của người lập lịch lúc
-- lập, đã giải; job chỉ xuất trong unit_code ⊂ phạm vi đó.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_reports.schedule (
  tenant_id     uuid        NOT NULL,
  id            uuid        NOT NULL,
  template_key  text        NOT NULL,
  format        text        NOT NULL CHECK (format IN ('xlsx', 'pdf')),
  unit_code     text        NULL,                          -- NULL = toàn bộ phạm vi của người lập
  params        jsonb       NOT NULL,
  frequency     text        NOT NULL CHECK (frequency IN ('weekly', 'monthly')),
  weekday       smallint    NULL CHECK (weekday BETWEEN 1 AND 7),       -- 1 = thứ Hai (ISO)
  day_of_month  smallint    NULL CHECK (day_of_month BETWEEN 1 AND 28),
  hour          smallint    NOT NULL CHECK (hour BETWEEN 0 AND 23),     -- giờ Việt Nam
  recipients    uuid[]      NOT NULL CHECK (cardinality(recipients) BETWEEN 1 AND 50),
  owner_ref     uuid        NOT NULL,
  owner_roles   text[]      NOT NULL,
  scope_all     boolean     NOT NULL,
  scope_units   text[]      NOT NULL,
  active        boolean     NOT NULL DEFAULT true,
  deactivated_reason text   NULL CHECK (deactivated_reason IS NULL OR char_length(deactivated_reason) BETWEEN 10 AND 500),
  created_at    timestamptz NOT NULL,
  updated_at    timestamptz NOT NULL,
  idem_key      text        NOT NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, idem_key),
  CHECK ((frequency = 'weekly') = (weekday IS NOT NULL)),
  CHECK ((frequency = 'monthly') = (day_of_month IS NOT NULL)),
  CHECK (active OR deactivated_reason IS NOT NULL)
);
CREATE INDEX schedule_active ON pf_reports.schedule (tenant_id) WHERE active;
SELECT shared.apply_tenant_rls('pf_reports.schedule');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_reports.schedule
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_reports.schedule
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- 5. Một mốc lịch chạy đúng một lần: khóa chính (lịch, mốc) — chạy lặp job lịch
-- (leader đổi, khởi động lại) không sinh việc xuất thứ hai.
CREATE TABLE pf_reports.schedule_run (
  tenant_id       uuid        NOT NULL,
  schedule_id     uuid        NOT NULL,
  slot_key        text        NOT NULL,                    -- vd 2026-W40, 2026-10
  slot_at         timestamptz NOT NULL,
  status          text        NOT NULL CHECK (status IN ('queued', 'skipped')),
  skip_reason     text        NULL,
  job_id          uuid        NULL,
  fired_at        timestamptz NOT NULL,
  delivery_status text        NULL CHECK (delivery_status IN ('sent', 'skipped', 'failed')),
  delivery_note   text        NULL,
  sent_count      integer     NULL CHECK (sent_count >= 0),
  delivered_at    timestamptz NULL,
  PRIMARY KEY (tenant_id, schedule_id, slot_key),
  FOREIGN KEY (tenant_id, schedule_id) REFERENCES pf_reports.schedule (tenant_id, id),
  CHECK ((status = 'queued') = (job_id IS NOT NULL)),
  CHECK (status <> 'skipped' OR skip_reason IS NOT NULL)
);
CREATE INDEX schedule_run_job ON pf_reports.schedule_run (tenant_id, job_id);
SELECT shared.apply_tenant_rls('pf_reports.schedule_run');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_reports.schedule_run
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_reports.schedule_run
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- Quyền của reports_app. Bảng chỉ ghi thêm: SELECT, INSERT. Bảng trạng thái
-- (export_job, schedule, schedule_run): thêm UPDATE. Không DELETE ở đâu cả.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_reports.template_version, pf_reports.export_audit TO reports_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_reports.template_version, pf_reports.export_audit FROM reports_app;
GRANT SELECT, INSERT, UPDATE ON pf_reports.export_job, pf_reports.schedule, pf_reports.schedule_run TO reports_app;
REVOKE DELETE, TRUNCATE ON pf_reports.export_job, pf_reports.schedule, pf_reports.schedule_run FROM reports_app;

-- Job lịch gửi chạy ở worker (leader) cho MỌI tenant: vai ag_outbox_publisher
-- (BYPASSRLS) chỉ được đọc hai cột để biết tenant nào có lịch đang dùng; việc
-- chạy lịch thật chạy bằng reports_app trong ngữ cảnh từng tenant (R-05).
GRANT USAGE ON SCHEMA pf_reports TO ag_outbox_publisher;
GRANT SELECT (tenant_id, active) ON pf_reports.schedule TO ag_outbox_publisher;
