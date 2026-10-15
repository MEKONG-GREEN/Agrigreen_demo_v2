-- pf/hrm (`dom/hrm`, W11.24 — ADR-0050, BRD Hành chính – Tổng hợp v3.0 FN-14, UR-HC-25, UR-ADM-10): KHO TÀI LIỆU NHÂN SỰ.
--
-- Nội dung tệp KHÔNG ở schema này: tệp mã hóa phong bì ở pf/evidence (pf_files.file, bộ ba chủ thể
-- 'pf/hrm' / 'hr_folder' / <folder_id>, mục đích 'hr_records'); pf_hrm chỉ giữ siêu dữ liệu (BR-03): thư mục, phân loại
-- (loại quyết định, năm, phòng ban, nhân viên = mã hồ sơ), mã tệp, người tải, ngày.
--
-- Ba sổ chỉ ghi thêm (mẫu 8.1, shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo tenant):
--   - doc_folder  — mỗi dòng là PHIÊN BẢN ĐẦY ĐỦ của một thư mục, chuỗi 'folder:<folder_id>': tạo, đổi tên, đổi quyền
--                   thư mục (acl — ghi đè ma trận FN-20 cho nhóm quyền tài liệu ở thư mục và thư mục con), ẩn/hiện lại.
--                   system_key: thư mục hệ thống để nghiệp vụ khác tự lưu tệp (vd nâng bậc W11.25, đánh giá W11.26).
--   - doc         — phiên bản đầy đủ của một tài liệu, chuỗi 'doc:<doc_id>': tải lên, sửa phân loại, ẩn ("xóa = ẩn",
--                   lý do bắt buộc, lưu vết), hiện lại. Tệp của tài liệu không đổi (tải bản mới = tài liệu mới).
--   - doc_access  — NHẬT KÝ XEM TRƯỚC/TẢI XUỐNG (BR-02, AC-03), chuỗi 'actor:<actor_ref>'. Tải lên, sửa, ẩn đã là
--                   dòng của sổ doc (người, lúc).
-- Vai: r_hrm SELECT, INSERT.

CREATE TABLE pf_hrm.doc_folder (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'folder:<folder_id>'
  seq               bigint      NOT NULL,
  folder_id         uuid        NOT NULL,
  parent_id         uuid        NULL,                     -- NULL = thư mục gốc (chỉ Admin tạo)
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
  system_key        text        NULL CHECK (system_key IS NULL OR system_key ~ '^[a-z][a-z0-9_]{1,39}$'),
  doc_type          text        NULL,                     -- loại quyết định gợi ý khi tải lên trong thư mục
  sort_order        integer     NOT NULL DEFAULT 0,
  acl               jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(acl) = 'object'),
  status            text        NOT NULL CHECK (status IN ('active', 'hidden')),
  change            text        NOT NULL CHECK (change IN ('created', 'renamed', 'acl', 'hidden', 'restored')),
  note              text        NULL CHECK (note IS NULL OR char_length(note) <= 500),
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
  CHECK (parent_id IS NULL OR parent_id <> folder_id),
  CONSTRAINT doc_folder_stream CHECK (stream_key = 'folder:' || folder_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX doc_folder_id_seq ON pf_hrm.doc_folder (tenant_id, folder_id, seq);
CREATE INDEX doc_folder_system ON pf_hrm.doc_folder (tenant_id, system_key) WHERE system_key IS NOT NULL;
SELECT shared.apply_ledger('pf_hrm.doc_folder', 'pf_hrm.doc_folder');

CREATE TABLE pf_hrm.doc (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'doc:<doc_id>'
  seq               bigint      NOT NULL,
  doc_id            uuid        NOT NULL,
  folder_id         uuid        NOT NULL,
  title             text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
  doc_type          text        NOT NULL CHECK (doc_type IN ('nang_bac', 'dieu_dong', 'bo_nhiem', 'khen_thuong', 'ky_luat',
                                  'danh_gia', 'khac')),
  doc_year          integer     NULL CHECK (doc_year IS NULL OR doc_year BETWEEN 1975 AND 2100),
  unit_code         text        NULL CHECK (unit_code IS NULL OR unit_code ~ '^[a-z0-9][a-z0-9_]{0,63}$'),  -- phòng ban (W11.07)
  staff_ids         uuid[]      NOT NULL DEFAULT '{}' CHECK (cardinality(staff_ids) <= 500),  -- nhân viên (pf_hrm.staff)
  doc_no            text        NULL CHECK (doc_no IS NULL OR char_length(doc_no) <= 60),     -- số quyết định
  issued_on         date        NULL,
  note              text        NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  file_ref          uuid        NOT NULL,                 -- pf_files.file (pf/evidence) — bản mã
  file_name         text        NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 200),
  content_type      text        NOT NULL,
  size_bytes        bigint      NOT NULL CHECK (size_bytes > 0),
  status            text        NOT NULL CHECK (status IN ('active', 'hidden')),
  change            text        NOT NULL CHECK (change IN ('created', 'updated', 'hidden', 'restored')),
  changed_fields    text[]      NOT NULL DEFAULT '{}',
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
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (change NOT IN ('hidden', 'restored') OR reason IS NOT NULL),            -- "xóa = ẩn" có lý do, lưu vết
  CHECK (array_position(staff_ids, NULL) IS NULL),
  CHECK (array_position(changed_fields, NULL) IS NULL AND cardinality(changed_fields) <= 20),
  CONSTRAINT doc_stream CHECK (stream_key = 'doc:' || doc_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX doc_id_seq ON pf_hrm.doc (tenant_id, doc_id, seq);
CREATE INDEX doc_folder_idx ON pf_hrm.doc (tenant_id, folder_id);
CREATE INDEX doc_staff ON pf_hrm.doc USING gin (staff_ids);
SELECT shared.apply_ledger('pf_hrm.doc', 'pf_hrm.doc');

CREATE TABLE pf_hrm.doc_access (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'actor:<actor_ref>'
  seq               bigint      NOT NULL,
  action            text        NOT NULL CHECK (action IN ('preview', 'download')),
  doc_id            uuid        NOT NULL,
  file_ref          uuid        NOT NULL,
  scope             text        NOT NULL CHECK (scope IN ('self', 'unit', 'all')),  -- phạm vi đã cho phép
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
  CONSTRAINT doc_access_stream CHECK (stream_key = 'actor:' || actor_ref::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX doc_access_doc ON pf_hrm.doc_access (tenant_id, doc_id, recorded_at);
CREATE INDEX doc_access_actor ON pf_hrm.doc_access (tenant_id, actor_ref, recorded_at);
SELECT shared.apply_ledger('pf_hrm.doc_access', 'pf_hrm.doc_access');

REVOKE ALL ON pf_hrm.doc_folder, pf_hrm.doc, pf_hrm.doc_access FROM PUBLIC;
GRANT SELECT, INSERT ON pf_hrm.doc_folder, pf_hrm.doc, pf_hrm.doc_access TO r_hrm;
