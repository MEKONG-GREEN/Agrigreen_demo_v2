-- pf/evidence (`cap/evidence`, W6.5 — kiến trúc v4.1 §18 R-DB-07, §26; ADR-0027 mục 2): schema pf_files chuyển
-- từ pf/sync sang module năng lực pf/evidence (mã SQL nay chỉ ở internal/platform/evidence/store).
--
-- 1. Bộ ba chủ thể (subject_module, subject_type, subject_id) trên pf_files.file — tham chiếu chéo module KHÔNG
--    có khóa ngoại xuyên schema; module chủ phân giải qua port Resolver khi ghi và khi mở. Cột NULL được: tệp
--    cũ giữ NULL (forward-only, KHÔNG UPDATE điền ngược), tệp tải trước khi biết thực thể cũng NULL. Đủ cả ba
--    hoặc không có gì (CHECK). Tệp vẫn ghi một lần: vai của module chỉ có SELECT, INSERT.
-- 2. Sổ truy cập pf_files.access_log (mẫu 8.1, shared.apply_ledger — chuỗi băm theo tệp 'file:<ref>', chặn
--    UPDATE/DELETE/TRUNCATE, RLS theo tenant): MỌI quyết định mở tệp (Open(purpose)) — cấp hay từ chối — kèm
--    mục đích, thao tác, kết quả. Chỉ mã tham chiếu (R-14): không toạ độ, không thời điểm chụp.
-- 3. Vai: r_evidence (vai nhóm không có vai cũ — dbadmin suy từ owns_schema; app_platform là thành viên) có
--    SELECT, INSERT trên hai bảng. pf/sync không còn đọc/ghi pf_files → thu quyền của sync_app, r_sync (R-05).

-- pf_files.file là bảng đã có: khóa ngắn, không treo hàng đợi khóa.
SET LOCAL lock_timeout = '5s';

ALTER TABLE pf_files.file
  ADD COLUMN subject_module text NULL,   -- tên module.yaml của module chủ, vd 'pf/cooperative'
  ADD COLUMN subject_type   text NULL,   -- loại thực thể module chủ cho tham chiếu, vd 'plot'
  ADD COLUMN subject_id     uuid NULL;   -- mã thực thể (không khóa ngoại — R-DB-07)

ALTER TABLE pf_files.file ADD CONSTRAINT file_subject_triple CHECK (
  (subject_module IS NULL AND subject_type IS NULL AND subject_id IS NULL)
  OR (subject_module ~ '^(pf|mkg)/[a-z][a-z0-9]{1,39}$'
      AND subject_type ~ '^[a-z][a-z0-9_]{1,39}$'
      AND subject_id IS NOT NULL)) NOT VALID;
ALTER TABLE pf_files.file VALIDATE CONSTRAINT file_subject_triple;  -- dòng cũ: cả ba NULL

CREATE INDEX file_subject_triple ON pf_files.file (tenant_id, subject_module, subject_type, subject_id)
  WHERE subject_id IS NOT NULL;

CREATE TABLE pf_files.access_log (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,                 -- RB-2
  stream_key        text        NOT NULL,                 -- 'file:<file_ref>'
  seq               bigint      NOT NULL,
  file_ref          uuid        NOT NULL,
  op                text        NOT NULL CHECK (op IN ('metadata', 'content')),
  purpose           text        NOT NULL CHECK (purpose ~ '^[a-z][a-z0-9_.]{2,63}$'),  -- R-42
  outcome           text        NOT NULL CHECK (outcome IN ('granted', 'denied_subject', 'denied_scope', 'denied_consent')),
  subject_module    text        NULL,                     -- chép từ tệp lúc mở
  subject_type      text        NULL,
  subject_id        uuid        NULL,
  trace_id          text        NULL,
  event_time        timestamptz NOT NULL,                 -- R-13
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người mở (sub của token)
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- 'open:<id>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK ((subject_module IS NULL) = (subject_id IS NULL) AND (subject_type IS NULL) = (subject_id IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX access_log_file  ON pf_files.access_log (tenant_id, file_ref, seq);
CREATE INDEX access_log_actor ON pf_files.access_log (tenant_id, actor_ref, recorded_at);
SELECT shared.apply_ledger('pf_files.access_log', 'pf_files.access_log');

REVOKE ALL ON pf_files.access_log FROM PUBLIC;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_evidence' AND NOT rolcanlogin) THEN
    RAISE EXCEPTION 'thiếu vai nhóm r_evidence (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)';
  END IF;
END
$$;

-- Vai của module: tệp ghi một lần, sổ chỉ ghi thêm (R-05, R-20).
GRANT USAGE ON SCHEMA pf_files TO r_evidence;
GRANT SELECT, INSERT ON pf_files.file, pf_files.access_log TO r_evidence;

-- Module pf mới có r_* (docs/dev/modules.md, W2.3 bước 5): đọc k_ref + biến ngữ cảnh (R-DB-05).
GRANT USAGE ON SCHEMA k_ref TO r_evidence;
GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) TO r_evidence;
GRANT SELECT ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope TO r_evidence;

-- pf/sync không còn chạm pf_files (route /v1/files gọi port của pf/evidence).
REVOKE ALL ON pf_files.file FROM sync_app, r_sync;
REVOKE USAGE ON SCHEMA pf_files FROM sync_app, r_sync;
