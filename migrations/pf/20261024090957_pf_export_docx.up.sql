-- pf/export (`cap/export`, W10.17, ADR-0051): mẫu văn bản có phiên bản, lần xuất .docx có băm, số văn bản tự cấp.
--
-- Ba sổ chỉ ghi thêm (shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo tenant):
--   template_version  một dòng = một phiên bản mẫu đã dùng trong tenant (định nghĩa JSON + băm). Cùng khóa + phiên bản chỉ
--                     một dòng (idem_key 'template:<khóa>:v<n>'); nội dung đổi mà không tăng phiên bản bị ứng dụng từ chối.
--   export_run        một dòng = một lần xuất: byte tệp bất biến + SHA-256 (CHECK tính lại ở DB), băm mẫu, băm dữ liệu vào,
--                     số văn bản đã dùng. Lần xuất phải trỏ tới phiên bản mẫu có thật cùng băm (trigger).
--   doc_number        số văn bản tự cấp "{n}/<ký hiệu>" theo năm: n = seq của luồng 'docno:<ký hiệu>:<năm>' (chuỗi băm của
--                     shared.fn_chain khóa đầu luồng → tuần tự, không trùng, không hở khi giao dịch hủy). Mỗi đối tượng (subject)
--                     nhận một số cho mỗi ký hiệu (idem_key 'docno:<ký hiệu>:<subject>').
-- Vai: r_export SELECT, INSERT; k_ref như mọi module pf sinh sau 000092 (R-DB-05).

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_export' AND NOT rolcanlogin) THEN
    RAISE EXCEPTION 'thiếu vai nhóm r_export (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)';
  END IF;
END
$$;

SET LOCAL lock_timeout = '5s';

CREATE SCHEMA pf_export;
REVOKE ALL ON SCHEMA pf_export FROM PUBLIC;

CREATE TABLE pf_export.template_version (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'template:<khóa>'
  seq               bigint      NOT NULL,
  template_key      text        NOT NULL CHECK (template_key ~ '^[a-z][a-z0-9_]*(\.[a-z0-9_]+)*$' AND char_length(template_key) <= 80),
  version           integer     NOT NULL CHECK (version >= 1),
  title             text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
  owner             text        NOT NULL CHECK (char_length(owner) BETWEEN 1 AND 80),
  definition        jsonb       NOT NULL CHECK (jsonb_typeof(definition) = 'object' AND octet_length(definition::text) <= 1048576),
  definition_hash   text        NOT NULL CHECK (definition_hash ~ '^[0-9a-f]{64}$'),
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
  CONSTRAINT template_version_stream CHECK (stream_key = 'template:' || template_key),
  CONSTRAINT template_version_idem CHECK (idem_key = 'template:' || template_key || ':v' || version::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX template_version_key ON pf_export.template_version (tenant_id, template_key, version);
SELECT shared.apply_ledger('pf_export.template_version', 'pf_export.template_version');

CREATE TABLE pf_export.doc_number (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'docno:<ký hiệu>:<năm>'
  seq               bigint      NOT NULL,                 -- = số thứ tự n trong năm
  series            text        NOT NULL CHECK (series ~ '^[A-Z0-9Đ][A-Z0-9Đ-]{0,29}$'),
  year              integer     NOT NULL CHECK (year BETWEEN 2000 AND 2999),
  subject           text        NOT NULL CHECK (subject ~ '^[a-z][a-z0-9_]*:[A-Za-z0-9._:-]{1,150}$'),
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
  CONSTRAINT doc_number_stream CHECK (stream_key = 'docno:' || series || ':' || year::text),
  CONSTRAINT doc_number_idem CHECK (idem_key = 'docno:' || series || ':' || subject)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX doc_number_subject ON pf_export.doc_number (tenant_id, series, subject);
SELECT shared.apply_ledger('pf_export.doc_number', 'pf_export.doc_number');

CREATE TABLE pf_export.export_run (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'run:<subject>'
  seq               bigint      NOT NULL,                 -- lần xuất thứ n của đối tượng
  subject           text        NOT NULL CHECK (subject ~ '^[a-z][a-z0-9_]*:[A-Za-z0-9._:-]{1,150}$'),
  template_key      text        NOT NULL,
  template_version  integer     NOT NULL,
  template_hash     text        NOT NULL CHECK (template_hash ~ '^[0-9a-f]{64}$'),
  input_hash        text        NOT NULL CHECK (input_hash ~ '^[0-9a-f]{64}$'),
  doc_number        text        NULL CHECK (doc_number IS NULL OR char_length(doc_number) BETWEEN 1 AND 100),
  doc_number_auto   boolean     NOT NULL DEFAULT false,
  file_name         text        NOT NULL CHECK (file_name ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,150}\.docx$'),
  content_type      text        NOT NULL CHECK (content_type = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'),
  body              bytea       NOT NULL CHECK (octet_length(body) BETWEEN 1 AND 33554432),
  body_hash         text        NOT NULL,
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
  CONSTRAINT export_run_stream CHECK (stream_key = 'run:' || subject),
  -- Băm do DB tính lại: byte lưu và băm công bố không thể lệch nhau.
  CONSTRAINT export_run_body_hash CHECK (body_hash = encode(sha256(body), 'hex')),
  CONSTRAINT export_run_auto_number CHECK (NOT doc_number_auto OR doc_number IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX export_run_subject ON pf_export.export_run (tenant_id, subject, seq DESC);
CREATE INDEX export_run_recent ON pf_export.export_run (tenant_id, event_time DESC);
CREATE INDEX export_run_id ON pf_export.export_run (tenant_id, id);

-- Lần xuất phải dùng đúng một phiên bản mẫu đã ghi sổ trong tenant (cùng băm định nghĩa).
CREATE FUNCTION pf_export.fn_export_run_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pf_export.template_version t
                  WHERE t.tenant_id = NEW.tenant_id AND t.template_key = NEW.template_key
                    AND t.version = NEW.template_version AND t.definition_hash = NEW.template_hash) THEN
    RAISE EXCEPTION 'export run must reference a recorded template version' USING ERRCODE = 'check_violation', CONSTRAINT = 'export_run_template';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_export_run_guard BEFORE INSERT ON pf_export.export_run
  FOR EACH ROW EXECUTE FUNCTION pf_export.fn_export_run_guard();
SELECT shared.apply_ledger('pf_export.export_run', 'pf_export.export_run');

REVOKE ALL ON pf_export.template_version, pf_export.doc_number, pf_export.export_run FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_export TO r_export;
GRANT SELECT, INSERT ON pf_export.template_version, pf_export.doc_number, pf_export.export_run TO r_export;

-- k_ref như mọi module pf sinh sau 000092 (R-DB-05).
GRANT USAGE ON SCHEMA k_ref TO r_export;
GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) TO r_export;
GRANT SELECT ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope TO r_export;
