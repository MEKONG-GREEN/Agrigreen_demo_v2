-- pf_extension KN v7.7 P7 (Cán bộ xã trên field-app): sổ vật tư trên địa bàn xã (FN-XA-03.06) và con trỏ đồng bộ
-- ngoại tuyến cho công việc báo cáo (FN-XA-03.01/03.02; docs/brd/kn-v7.7-plan.md §2 pf 000077, sync-contract.md).
--
--   1. material_record  SỔ ghi nhận vật tư: xã × thời điểm ghi nhận × vật tư (danh mục masterdata "material") ×
--                       số lượng numeric(14,4) × ĐVT (chép từ danh mục lúc ghi) × ghi chú; mang department_code.
--                       "Sửa" = bản ghi bù (supersedes_id + correction_reason, RB-1) — bản hiện hành là bản không bị
--                       bản nào khác thay. Không PII (R-04): ghi chú kiểm không chứa SĐT/CCCD ở app/.
--   2. report_task.sync_seq     tăng mỗi lần công việc đổi (trigger BEFORE UPDATE) — entity pull "report_task" và
--                               "report_submission" (bản nộp mới nhất của công việc) đọc theo cột này.
--      report_task.created_seq  cấp một lần lúc sinh công việc — entity pull "form_layout" gom phiên bản mẫu của
--                               công việc mới (phiên bản mẫu bất biến nên không gửi lại khi công việc đổi).
--
-- ADD COLUMN ... DEFAULT nextval() cấp số cho dòng đã có ngay khi thêm cột (không UPDATE, R-25).

-- ---------------------------------------------------------------------------
-- 1. Vật tư
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.material_record (
  id                uuid          NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,                 -- 'material:<commune_code>'
  seq               bigint        NOT NULL,
  commune_code      text          NOT NULL,
  province_code     text          NOT NULL DEFAULT '',
  department_code   text          NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  recorded_on       date          NOT NULL,                 -- thời điểm ghi nhận (ngày, giờ Việt Nam)
  material_code     text          NOT NULL CHECK (char_length(material_code) BETWEEN 1 AND 60),
  material_name     text          NOT NULL CHECK (char_length(material_name) BETWEEN 1 AND 200),
  quantity          numeric(14,4) NOT NULL CHECK (quantity >= 0),
  unit              text          NOT NULL CHECK (char_length(unit) BETWEEN 1 AND 40),
  note              text          NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  source            text          NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'import', 'field_app')),
  on_behalf_of      uuid          NULL,                     -- chế độ hỗ trợ Admin (FN-AD-04.07)
  support_reason    text          NULL,
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
  CHECK (correction_reason IS NULL OR char_length(correction_reason) BETWEEN 10 AND 500)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX material_record_commune ON pf_extension.material_record (tenant_id, department_code, commune_code, recorded_on DESC, id);
CREATE INDEX material_record_province ON pf_extension.material_record (tenant_id, department_code, province_code, recorded_on);
CREATE INDEX material_record_supersedes ON pf_extension.material_record (tenant_id, supersedes_id) WHERE supersedes_id IS NOT NULL;
SELECT shared.apply_ledger('pf_extension.material_record', 'pf_extension.material_record');

GRANT SELECT, INSERT ON pf_extension.material_record TO extension_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_extension.material_record FROM extension_app;

-- ---------------------------------------------------------------------------
-- 2. Con trỏ đồng bộ công việc báo cáo
-- ---------------------------------------------------------------------------
ALTER TABLE pf_extension.report_task
  ADD COLUMN created_seq bigint NOT NULL DEFAULT nextval('pf_extension.sync_seq'),
  ADD COLUMN sync_seq    bigint NOT NULL DEFAULT nextval('pf_extension.sync_seq');
CREATE INDEX report_task_sync ON pf_extension.report_task (tenant_id, sync_seq);
CREATE INDEX report_task_created_sync ON pf_extension.report_task (tenant_id, created_seq);

CREATE FUNCTION pf_extension.fn_report_task_sync_seq() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.sync_seq := nextval('pf_extension.sync_seq');
  NEW.created_seq := OLD.created_seq;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_report_task_sync_seq BEFORE UPDATE ON pf_extension.report_task
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_report_task_sync_seq();
