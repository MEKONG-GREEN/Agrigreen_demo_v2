-- pf_reports KN v7.7 P3: lần nạp mẫu từ Excel (FN-AD-04.06 "nạp từ Excel"; kn-v7.7-plan
-- P3 mục 3). Mỗi lần nạp một dòng bất biến: tệp (tên, sha256, cỡ, khóa kho đối tượng nếu
-- lưu), sheet, kết quả phân tích (số cột, dòng tiêu đề, vùng gộp, công thức, cảnh báo) và
-- phiên bản nháp đã tạo. Tệp gốc của khách (Q-01) nạp qua đây hoặc cmd/formimport.

CREATE TABLE pf_reports.form_import (
  tenant_id     uuid        NOT NULL,
  id            uuid        NOT NULL,                        -- UUIDv7
  file_name     text        NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 255),
  file_sha256   text        NOT NULL CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
  size_bytes    bigint      NOT NULL CHECK (size_bytes > 0),
  object_key    text        NULL,                            -- reports/<tenant>/form-imports/<id>.xlsx khi có kho
  sheet_name    text        NOT NULL,
  status        text        NOT NULL CHECK (status IN ('parsed', 'failed')),
  error_code    text        NULL,
  report        jsonb       NOT NULL,                        -- formimport.Report (không PII)
  template_id   uuid        NULL,
  version_id    uuid        NULL,
  created_by    uuid        NOT NULL,
  created_at    timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, id),
  CHECK ((status = 'failed') = (error_code IS NOT NULL)),
  CHECK (status <> 'parsed' OR (template_id IS NOT NULL AND version_id IS NOT NULL))
);
CREATE INDEX form_import_template ON pf_reports.form_import (tenant_id, template_id, created_at DESC);
SELECT shared.apply_tenant_rls('pf_reports.form_import');
CREATE TRIGGER trg_no_update BEFORE UPDATE OR DELETE ON pf_reports.form_import
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_reports.form_import
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

GRANT SELECT, INSERT ON pf_reports.form_import TO reports_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_reports.form_import FROM reports_app;
