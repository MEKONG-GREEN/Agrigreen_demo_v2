-- pf_files: siêu dữ liệu tệp minh chứng đã mã hóa phong bì (mục 10.2, ADR-0004,
-- docs/dev/envelope-format.md). Nội dung chỉ nằm ở kho đối tượng dưới dạng BẢN MÃ
-- (R-40), khóa đối tượng files/<tenant>/<file_ref>. Máy chủ không có DEK: chỉ kho
-- định danh mở được wrapped_dek, bắt buộc có mục đích.
--
-- gps_* và captured_at là dữ liệu cá nhân khi gắn với hộ: chỉ nằm ở đây, KHÔNG
-- vào log hay sự kiện.

CREATE EXTENSION IF NOT EXISTS postgis;

CREATE SCHEMA pf_files;
REVOKE ALL ON SCHEMA pf_files FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_files TO sync_app;

-- Tệp ghi một lần: không UPDATE, không DELETE (vòng đời theo khóa hộ, mục 22).
CREATE TABLE pf_files.file (
  tenant_id         uuid                  NOT NULL,
  file_ref          uuid                  NOT NULL,                -- UUIDv7, máy sinh hoặc server sinh
  subject_ref       uuid                  NOT NULL,                -- chủ thể có khóa công khai bọc DEK
  v                 smallint              NOT NULL CHECK (v = 1),
  alg               text                  NOT NULL CHECK (alg = 'A256GCM+X25519-SEALEDBOX'),
  nonce             bytea                 NOT NULL CHECK (length(nonce) = 12),
  wrapped_dek       bytea                 NOT NULL CHECK (length(wrapped_dek) = 80),
  sha256_ciphertext text                  NOT NULL CHECK (sha256_ciphertext ~ '^[0-9a-f]{64}$'),
  size_bytes        bigint                NOT NULL CHECK (size_bytes BETWEEN 16 AND 15728640),
  content_type      text                  NOT NULL,                -- kiểu của BẢN RÕ
  captured_at       timestamptz           NULL,                    -- dữ liệu cá nhân khi gắn với hộ
  gps               geometry(Point, 4326) NULL,                    -- dữ liệu cá nhân khi gắn với hộ
  gps_accuracy_m    numeric(10,2)         NULL CHECK (gps_accuracy_m >= 0),
  object_key        text                  NOT NULL,
  uploaded_by       uuid                  NOT NULL,
  device_id         uuid                  NULL,
  created_at        timestamptz           NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, file_ref),
  UNIQUE (tenant_id, object_key)
);
CREATE INDEX file_subject ON pf_files.file (tenant_id, subject_ref, file_ref);
SELECT shared.apply_tenant_rls('pf_files.file');

GRANT SELECT, INSERT ON pf_files.file TO sync_app;
