-- pf/extension (`dom/extension`) — W11.54, D4 CN14: TRANG CHỦ PHÒNG + KHO VĂN BẢN PHÒNG (BRD CNTS v3.7 FN-17 UR-TINH-06,
-- FN-20 UR-TINH-25; trang chủ tương ứng của Trồng trọt, KHTC, HCTH, Thông tin – Truyền thông). Ba sổ chỉ ghi thêm
-- (shared.apply_ledger: DB tính seq, prev_hash, row_hash; chặn UPDATE/DELETE; RLS tenant):
--
-- 1. dept_profile — giới thiệu + chức năng nhiệm vụ của phòng (chuỗi 'dept_profile:<mã phòng>'); mỗi lần Lãnh đạo phòng
--    lưu là một phiên bản đủ hai trường (FN-17 luồng phụ). Bản mới nhất là hiện hành.
-- 2. dept_doc_folder — thư mục của kho văn bản phòng (chuỗi 'dept_folder:<folder_ref>'): created / renamed / hidden /
--    restored. Thư mục cha cùng phòng (kiểm ở ứng dụng; không khóa ngoại trên sổ phân vùng — ADR-0012).
-- 3. dept_doc — văn bản (chuỗi 'dept_doc:<doc_ref>'): mỗi dòng là MỘT phiên bản đủ thuộc tính + tệp hiện hành. Tệp ở kho
--    đối tượng (object_key theo băm SHA-256, ghi một lần) — KHÔNG lưu nội dung trong DB (g1-1031 §4: không base64 trong
--    DB). Văn bản công khai trong ngành (Nghị định, quy trình kỹ thuật) không phải dữ liệu cá nhân → bản rõ (ngoại lệ
--    R-40 như pf/content). file_version tăng khi tải tệp mới; tệp cũ vẫn tải được theo phiên bản. "Xóa" = ẩn (hidden,
--    có lý do); hết hiệu lực vẫn lưu, tìm được (BR-01, AC-03). Tệp ≤ 20 MB (AC-02).
-- Không dữ liệu cá nhân ngoài actor_ref (mã tài khoản). Vai r_extension: SELECT, INSERT.

CREATE TABLE pf_extension.dept_profile (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'dept_profile:<department_code>'
  seq               bigint      NOT NULL,
  department_code   text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,40}$'),
  event             text        NOT NULL CHECK (event = 'saved'),
  intro             text        NOT NULL CHECK (char_length(intro) <= 4000),
  duties            text        NOT NULL CHECK (char_length(duties) <= 8000),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  actor_ref         uuid        NOT NULL,
  on_behalf_of      uuid        NULL,                     -- chế độ hỗ trợ của Admin
  support_reason    text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT dept_profile_stream CHECK (stream_key = 'dept_profile:' || department_code),
  CONSTRAINT dept_profile_text CHECK (char_length(intro) + char_length(duties) > 0)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX dept_profile_dept ON pf_extension.dept_profile (tenant_id, department_code, seq);
SELECT shared.apply_ledger('pf_extension.dept_profile', 'pf_extension.dept_profile');

CREATE TABLE pf_extension.dept_doc_folder (
  id                uuid        NOT NULL,                 -- mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'dept_folder:<folder_ref>'
  seq               bigint      NOT NULL,
  folder_ref        uuid        NOT NULL,
  department_code   text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,40}$'),
  parent_ref        uuid        NULL,                     -- NULL = thư mục gốc của phòng
  event             text        NOT NULL CHECK (event IN ('created', 'renamed', 'hidden', 'restored')),
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
  hidden            boolean     NOT NULL,
  status_reason     text        NULL CHECK (status_reason IS NULL OR char_length(status_reason) BETWEEN 1 AND 1000),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  actor_ref         uuid        NOT NULL,
  on_behalf_of      uuid        NULL,
  support_reason    text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT dept_doc_folder_stream CHECK (stream_key = 'dept_folder:' || folder_ref::text),
  CONSTRAINT dept_doc_folder_parent CHECK (parent_ref IS NULL OR parent_ref <> folder_ref),
  CONSTRAINT dept_doc_folder_hidden CHECK (event <> 'hidden' OR hidden),
  CONSTRAINT dept_doc_folder_restored CHECK (event NOT IN ('created', 'restored') OR NOT hidden),
  CONSTRAINT dept_doc_folder_reason CHECK (event NOT IN ('hidden', 'restored') OR status_reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX dept_doc_folder_ref ON pf_extension.dept_doc_folder (tenant_id, folder_ref, seq);
CREATE INDEX dept_doc_folder_dept ON pf_extension.dept_doc_folder (tenant_id, department_code);
SELECT shared.apply_ledger('pf_extension.dept_doc_folder', 'pf_extension.dept_doc_folder');

CREATE TABLE pf_extension.dept_doc (
  id                uuid        NOT NULL,                 -- mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'dept_doc:<doc_ref>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  doc_ref           uuid        NOT NULL,
  department_code   text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,40}$'),
  folder_ref        uuid        NULL,                     -- NULL = gốc kho của phòng
  event             text        NOT NULL CHECK (event IN ('created', 'updated', 'file_replaced', 'hidden', 'restored')),
  title             text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
  kind              text        NOT NULL CHECK (kind IN ('quy_trinh_ky_thuat', 'van_ban_phap_quy', 'van_ban_chi_dao', 'ke_hoach',
                                                         'mau_bieu', 'tai_lieu_tap_huan', 'khac')),
  field             text        NULL CHECK (field IS NULL OR char_length(field) BETWEEN 1 AND 100),      -- lĩnh vực
  doc_number        text        NULL CHECK (doc_number IS NULL OR char_length(doc_number) BETWEEN 1 AND 100),
  issuer            text        NULL CHECK (issuer IS NULL OR char_length(issuer) BETWEEN 1 AND 200),
  summary           text        NULL CHECK (summary IS NULL OR char_length(summary) BETWEEN 1 AND 2000),
  issued_on         date        NULL,
  effective_from    date        NULL,
  effective_to      date        NULL,
  audience          text[]      NOT NULL DEFAULT '{}' CHECK (audience <@ ARRAY['xa', 'nong_ho']::text[]),
  file_version      integer     NOT NULL CHECK (file_version >= 1),
  object_key        text        NOT NULL CHECK (char_length(object_key) BETWEEN 1 AND 400),
  file_name         text        NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 200),
  content_type      text        NOT NULL CHECK (content_type IN ('application/pdf',
                                  'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
                                  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet')),
  size_bytes        bigint      NOT NULL CHECK (size_bytes BETWEEN 1 AND 20971520),
  sha256            text        NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  hidden            boolean     NOT NULL,
  status_reason     text        NULL CHECK (status_reason IS NULL OR char_length(status_reason) BETWEEN 1 AND 1000),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  actor_ref         uuid        NOT NULL,
  on_behalf_of      uuid        NULL,
  support_reason    text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT dept_doc_stream CHECK (stream_key = 'dept_doc:' || doc_ref::text),
  CONSTRAINT dept_doc_effective CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to >= effective_from),
  CONSTRAINT dept_doc_hidden CHECK (event <> 'hidden' OR hidden),
  CONSTRAINT dept_doc_restored CHECK (event <> 'restored' OR NOT hidden),
  CONSTRAINT dept_doc_reason CHECK (event NOT IN ('hidden', 'restored') OR status_reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX dept_doc_ref ON pf_extension.dept_doc (tenant_id, doc_ref, seq);
CREATE INDEX dept_doc_dept ON pf_extension.dept_doc (tenant_id, department_code, recorded_at DESC);
SELECT shared.apply_ledger('pf_extension.dept_doc', 'pf_extension.dept_doc');

REVOKE ALL ON pf_extension.dept_profile, pf_extension.dept_doc_folder, pf_extension.dept_doc FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.dept_profile, pf_extension.dept_doc_folder, pf_extension.dept_doc TO r_extension;
