-- pf/hrm (`dom/hrm`, W11.28 — ADR-0050, BRD Hành chính – Tổng hợp v3.0 FN-12, FN-06): SỔ BÁO CÁO SỞ NS01–NS06.
--
-- report_log — sổ chỉ ghi thêm (shared.apply_ledger, chuỗi 'report:<NSxx>', RLS theo tenant) vừa là LỊCH SỬ XUẤT vừa
-- là NHẬT KÝ TRƯỜNG NHẠY CẢM của báo cáo Sở (export_audit):
--   - export   — một lần xuất Excel: ngày chốt, nhãn kỳ, tham số (người lập/người ký là mã hồ sơ, không tên), mã các hồ
--                sơ có trong biểu, TÊN các trường thẻ Thống kê BC Sở đã đưa vào tệp, kết quả kiểm tra (chỉ mã quy tắc,
--                mức, đạt/không, số người — không PII), tên tệp, SHA-256, dung lượng, và known_at = thời điểm biết dùng để
--                dựng lại đúng tệp (bitemporal: sự kiện hồ sơ, danh mục ghi sổ sau known_at bị bỏ qua).
--   - download — tải lại một lần xuất (export_id): tệp được DỰNG LẠI tại (as_of, known_at) và so SHA-256 với bản gốc;
--                drift = true khi khác (định danh ở kho đã sửa/xóa sau lần xuất).
--   - preview  — xem trước biểu có họ tên / trường nhạy cảm.
--   - drill    — nhấn ô số của biểu thống kê (NS05, NS06) để xem người được đếm (params.cell).
-- KHÔNG lưu nội dung tệp: tệp chứa họ tên, ngày sinh, dân tộc, tôn giáo … — dữ liệu cá nhân chỉ ở kho định danh (RB-3);
-- kho đối tượng chỉ nhận bản mã phong bì theo chủ thể (R-40), không hợp với tệp nhiều người. Bộ ghi tệp mã hóa dùng
-- chung thuộc W10.17 (`cap/export`).
-- Vai: r_hrm SELECT, INSERT.

CREATE TABLE pf_hrm.report_log (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'report:<NSxx>'
  seq               bigint      NOT NULL,
  action            text        NOT NULL CHECK (action IN ('export', 'download', 'preview', 'drill')),
  report_code       text        NOT NULL CHECK (report_code ~ '^NS0[1-6]$'),
  as_of             date        NOT NULL,
  period_label      text        NOT NULL CHECK (char_length(period_label) BETWEEN 1 AND 40),
  export_id         uuid        NULL,                     -- download: lần xuất gốc
  known_at          timestamptz NULL,                     -- export: thời điểm biết của dữ liệu đã dựng
  params            jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(params) = 'object'),
  staff_ids         uuid[]      NOT NULL CHECK (cardinality(staff_ids) <= 5000),
  sensitive_fields  text[]      NOT NULL DEFAULT '{}' CHECK (array_position(sensitive_fields, NULL) IS NULL AND cardinality(sensitive_fields) <= 10),
  item_count        integer     NOT NULL CHECK (item_count >= 0),
  file_name         text        NULL CHECK (file_name IS NULL OR file_name ~ '^[A-Za-z0-9._-]{1,120}$'),
  file_sha256       text        NULL CHECK (file_sha256 IS NULL OR file_sha256 ~ '^[0-9a-f]{64}$'),
  byte_size         bigint      NULL CHECK (byte_size IS NULL OR byte_size > 0),
  check_status      text        NULL CHECK (check_status IS NULL OR check_status IN ('ready', 'warning', 'blocked')),
  checks            jsonb       NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(checks) = 'array'),
  drift             boolean     NULL,
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
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK ((action IN ('export', 'download')) = (file_sha256 IS NOT NULL)),
  CHECK ((action = 'export') = (known_at IS NOT NULL)),
  CHECK ((action = 'download') = (export_id IS NOT NULL AND drift IS NOT NULL)),
  CONSTRAINT report_log_stream CHECK (stream_key = 'report:' || report_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX report_log_report ON pf_hrm.report_log (tenant_id, report_code, recorded_at);
CREATE INDEX report_log_export ON pf_hrm.report_log (tenant_id, export_id);
CREATE INDEX report_log_staff ON pf_hrm.report_log USING gin (staff_ids);
SELECT shared.apply_ledger('pf_hrm.report_log', 'pf_hrm.report_log');

REVOKE ALL ON pf_hrm.report_log FROM PUBLIC;
GRANT SELECT, INSERT ON pf_hrm.report_log TO r_hrm;
