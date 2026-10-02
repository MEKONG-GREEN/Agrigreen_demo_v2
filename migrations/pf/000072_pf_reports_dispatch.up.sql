-- pf_reports KN v7.7 P4: "Báo cáo Chi cục" — xuất tệp toàn tỉnh từ lần tổng hợp gần nhất rồi gửi email từ
-- hệ thống tới người nhận ngoài hệ thống (FN-TI-02.06; docs/brd/kn-v7.7-plan.md §2 pf 000072, gap §4.2–4.3).
--
--   1. dispatch_recipient_event  SỔ danh bạ người nhận Chi cục (thêm, đổi, ngừng dùng) — mặc định lần trước
--      dispatch_recipient        bảng chiếu. Địa chỉ là HỘP THƯ ĐƠN VỊ (dữ liệu tổ chức, Q-09 tạm); nếu khách
--                                xác nhận là địa chỉ cá nhân → chuyển sang chủ thể vault (contact_ref), cột
--                                address để NULL (đã có CHECK cho phép)
--   2. dispatch_event            SỔ lần xuất/gửi: exported, send_requested, sent, failed (gửi lỗi → gửi lại)
--      dispatch                  bảng chiếu lần gửi
--   3. dispatch_file             tệp đã dựng (Excel theo mẫu, PDF) trong kho đối tượng trong lãnh thổ — bất biến
--   4. gsheet_link               link Google Sheet đã tạo — bộ chuyển TẮT mặc định (ADR-0026 "Đề xuất", RB-7)
--
-- Điểm kiểm soát xuất (mục 18 DLP): mỗi lần xuất ghi export_audit kind 'dispatch' (mở rộng CHECK bên dưới).
-- Tệp/email chỉ chứa số tổng hợp theo xã, không PII (gap §4.2).

CREATE TABLE pf_reports.dispatch_recipient_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'recipient:<recipient_id>'
  seq               bigint      NOT NULL,
  recipient_id      uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('added', 'updated', 'deactivated', 'used')),
  department_code   text        NOT NULL,
  province_code     text        NOT NULL,
  before            jsonb       NULL,
  after             jsonb       NOT NULL,
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 10 AND 500),
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
CREATE INDEX dispatch_recipient_event_rec ON pf_reports.dispatch_recipient_event (tenant_id, recipient_id, id);
SELECT shared.apply_ledger('pf_reports.dispatch_recipient_event', 'pf_reports.dispatch_recipient_event');

CREATE TABLE pf_reports.dispatch_recipient (
  tenant_id       uuid        NOT NULL,
  id              uuid        NOT NULL,
  department_code text        NOT NULL,
  province_code   text        NOT NULL,
  label           text        NOT NULL CHECK (char_length(label) BETWEEN 1 AND 200),  -- "Chi cục Trồng trọt và BVTV"
  address         text        NULL CHECK (address IS NULL OR (char_length(address) <= 254 AND address ~ '^[^@\s<>]+@[^@\s<>]+\.[^@\s<>]+$')),
  contact_ref     uuid        NULL,                       -- chủ thể vault nếu là địa chỉ cá nhân (Q-09)
  kind            text        NOT NULL CHECK (kind IN ('to', 'cc')),
  is_default      boolean     NOT NULL DEFAULT true,
  active          boolean     NOT NULL DEFAULT true,
  last_used_at    timestamptz NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, id),
  CHECK ((address IS NULL) <> (contact_ref IS NULL))
);
CREATE UNIQUE INDEX dispatch_recipient_address ON pf_reports.dispatch_recipient (tenant_id, province_code, department_code, lower(address))
  WHERE active AND address IS NOT NULL;
SELECT shared.apply_tenant_rls('pf_reports.dispatch_recipient');

CREATE TABLE pf_reports.dispatch_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'dispatch:<dispatch_id>'
  seq               bigint      NOT NULL,
  dispatch_id       uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('exported', 'send_requested', 'sent', 'failed', 'gsheet_created')),
  payload           jsonb       NOT NULL,                 -- người nhận (mã danh bạ), kỳ, tệp/link, lỗi (không địa chỉ email)
  department_code   text        NOT NULL,
  province_code     text        NOT NULL,
  on_behalf_of      uuid        NULL,
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
CREATE INDEX dispatch_event_dispatch ON pf_reports.dispatch_event (tenant_id, dispatch_id, id);
SELECT shared.apply_ledger('pf_reports.dispatch_event', 'pf_reports.dispatch_event');

CREATE TABLE pf_reports.dispatch (
  tenant_id       uuid        NOT NULL,
  id              uuid        NOT NULL,
  department_code text        NOT NULL,
  province_code   text        NOT NULL,
  period_id       uuid        NOT NULL,                   -- kỳ của pf_extension (tham chiếu thuần, R-06)
  period_label    text        NOT NULL,
  run_ids         uuid[]      NOT NULL,                   -- lần tổng hợp đã dùng (nguồn số trong tệp)
  forms           text[]      NOT NULL,
  formats         text[]      NOT NULL,
  provisional     boolean     NOT NULL,
  missing_communes text[]     NOT NULL DEFAULT '{}',
  status          text        NOT NULL CHECK (status IN ('exported', 'sending', 'sent', 'failed')),
  recipient_ids   uuid[]      NOT NULL DEFAULT '{}',
  cc_ids          uuid[]      NOT NULL DEFAULT '{}',
  subject         text        NULL CHECK (subject IS NULL OR char_length(subject) <= 300),
  body            text        NULL CHECK (body IS NULL OR char_length(body) <= 5000),
  link_only       boolean     NOT NULL DEFAULT false,     -- vượt giới hạn đính kèm → chỉ gửi liên kết
  attempts        integer     NOT NULL DEFAULT 0,
  last_error      text        NULL,
  sent_at         timestamptz NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, id)
);
CREATE INDEX dispatch_scope ON pf_reports.dispatch (tenant_id, province_code, department_code, created_at DESC);
CREATE INDEX dispatch_period ON pf_reports.dispatch (tenant_id, period_id);
SELECT shared.apply_tenant_rls('pf_reports.dispatch');

CREATE TABLE pf_reports.dispatch_file (
  tenant_id    uuid        NOT NULL,
  id           uuid        NOT NULL,
  dispatch_id  uuid        NOT NULL,
  format       text        NOT NULL CHECK (format IN ('xlsx', 'pdf')),
  form_code    text        NOT NULL,
  file_name    text        NOT NULL,
  object_key   text        NOT NULL,
  sha256       text        NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  size_bytes   bigint      NOT NULL CHECK (size_bytes > 0),
  created_at   timestamptz NOT NULL,
  recorded_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, dispatch_id) REFERENCES pf_reports.dispatch (tenant_id, id)
);
SELECT shared.apply_tenant_rls('pf_reports.dispatch_file');
CREATE TRIGGER trg_no_update BEFORE UPDATE OR DELETE ON pf_reports.dispatch_file
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_reports.dispatch_file
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

CREATE TABLE pf_reports.gsheet_link (
  tenant_id    uuid        NOT NULL,
  id           uuid        NOT NULL,
  dispatch_id  uuid        NOT NULL,
  form_code    text        NOT NULL,
  url          text        NOT NULL,
  share_mode   text        NOT NULL CHECK (share_mode IN ('recipients', 'anyone_with_link')),
  created_at   timestamptz NOT NULL,
  recorded_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, dispatch_id) REFERENCES pf_reports.dispatch (tenant_id, id)
);
SELECT shared.apply_tenant_rls('pf_reports.gsheet_link');
CREATE TRIGGER trg_no_update BEFORE UPDATE OR DELETE ON pf_reports.gsheet_link
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_reports.gsheet_link
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- Điểm kiểm soát xuất: thêm loại 'dispatch' (giữ nguyên các loại đã có — đọc định nghĩa CHECK hiện hành).
DO $$
DECLARE
  def text;
BEGIN
  SELECT pg_get_constraintdef(c.oid) INTO def
    FROM pg_constraint c
   WHERE c.conrelid = 'pf_reports.export_audit'::regclass AND c.conname = 'export_audit_kind_check';
  IF def IS NULL THEN
    RAISE EXCEPTION 'thiếu ràng buộc pf_reports.export_audit.export_audit_kind_check';
  END IF;
  EXECUTE 'ALTER TABLE pf_reports.export_audit DROP CONSTRAINT export_audit_kind_check';
  EXECUTE format('ALTER TABLE pf_reports.export_audit ADD CONSTRAINT export_audit_kind_check CHECK ((%s) OR kind = %L)',
                 substring(def FROM '^CHECK \((.*)\)$'), 'dispatch');
END
$$;

GRANT SELECT, INSERT ON pf_reports.dispatch_recipient_event, pf_reports.dispatch_event, pf_reports.dispatch_file, pf_reports.gsheet_link TO reports_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_reports.dispatch_recipient_event, pf_reports.dispatch_event, pf_reports.dispatch_file, pf_reports.gsheet_link FROM reports_app;
GRANT SELECT, INSERT, UPDATE ON pf_reports.dispatch_recipient, pf_reports.dispatch TO reports_app;
REVOKE DELETE, TRUNCATE ON pf_reports.dispatch_recipient, pf_reports.dispatch FROM reports_app;
