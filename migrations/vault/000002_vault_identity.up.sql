-- vault/identity: kho định danh (mục 10, ADR-0004). Cụm ag_vault riêng, vai vault_app.
--
-- HAI LOẠI BẢNG, CÓ CHỦ Ý:
--   * subject, identity, subject_key, blind_index KHÔNG phải sổ. Chúng chứa dữ
--     liệu cá nhân hoặc khóa, và PHẢI xóa được theo yêu cầu (RB-3, mục 10.1:
--     "xóa bình thường"). Vì vậy không có cột row_hash, không gắn fn_immutable,
--     không nằm trong shared.v_missing_immutability. Vẫn có tenant_id + RLS.
--   * access_log LÀ sổ chỉ ghi thêm (mẫu mục 8.1 + shared.apply_ledger): ghi mọi
--     create/display/resolve/lookup/unwrap/erase kèm mục đích và người yêu cầu
--     (R-42). Sổ CHỈ chứa mã tham chiếu, mục đích, kết quả — KHÔNG BAO GIỜ chứa
--     giá trị định danh, nên không phải xóa khi xóa định danh.
--
-- Mã hóa lưu trữ: mọi trường định danh là AES-256-GCM bằng khóa dữ liệu riêng
-- của chủ thể; khóa đó và khóa bí mật X25519 được bọc bằng KEK của KMS
-- (subject_key). Xóa định danh = xóa identity + subject_key + blind_index.

-- ---------------------------------------------------------------------------
-- Chủ thể: mã tham chiếu ngẫu nhiên (R-41) + dấu mộ sau khi xóa (erased_at).
-- ---------------------------------------------------------------------------
CREATE TABLE vault.subject (
  tenant_id      uuid        NOT NULL,
  ref            uuid        NOT NULL,          -- UUIDv7 ngẫu nhiên, KHÔNG suy ra từ định danh (R-41)
  kind           text        NOT NULL CHECK (kind IN ('household', 'person')),
  area_code      text        NOT NULL CHECK (area_code ~ '^[0-9A-Za-z._-]{1,32}$'),
  created_at     timestamptz NOT NULL DEFAULT now(),
  created_by     uuid        NOT NULL,
  erased_at      timestamptz NULL,
  erased_by      uuid        NULL,
  erasure_reason text        NULL CHECK (erasure_reason IS NULL OR length(erasure_reason) BETWEEN 10 AND 500),
  PRIMARY KEY (tenant_id, ref),
  CHECK ((erased_at IS NULL) = (erased_by IS NULL))
);
CREATE INDEX subject_area ON vault.subject (tenant_id, area_code, ref) WHERE erased_at IS NULL;
COMMENT ON TABLE vault.subject IS 'Chủ thể dữ liệu. Không phải sổ: dòng được UPDATE erased_at khi xóa định danh (RB-3); dấu mộ giữ lại.';
SELECT shared.apply_tenant_rls('vault.subject');

-- ---------------------------------------------------------------------------
-- Định danh đã mã hóa. Mỗi cột *_ct = nonce 12 byte ‖ AES-256-GCM(khóa dữ liệu
-- của chủ thể, giá trị, aad = tenant/ref/tên trường).
-- ---------------------------------------------------------------------------
CREATE TABLE vault.identity (
  tenant_id      uuid        NOT NULL,
  subject_ref    uuid        NOT NULL,
  full_name_ct   bytea       NOT NULL,
  phone_ct       bytea       NULL,
  national_id_ct bytea       NULL,
  address_ct     bytea       NULL,
  updated_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, subject_ref),
  FOREIGN KEY (tenant_id, subject_ref) REFERENCES vault.subject (tenant_id, ref)
);
COMMENT ON TABLE vault.identity IS 'Định danh đã mã hóa. Không phải sổ: xóa hẳn khi xóa định danh (RB-3).';
SELECT shared.apply_tenant_rls('vault.identity');

-- ---------------------------------------------------------------------------
-- Khóa của chủ thể: khóa công khai X25519 rõ (đồng bộ xuống thiết bị), khóa bí
-- mật và khóa dữ liệu định danh đều bọc bằng KEK. Hủy dòng = hủy khóa (RB-3).
-- ---------------------------------------------------------------------------
CREATE TABLE vault.subject_key (
  tenant_id            uuid        NOT NULL,
  subject_ref          uuid        NOT NULL,
  public_key           bytea       NOT NULL CHECK (length(public_key) = 32),
  wrapped_private_key  bytea       NOT NULL,
  wrapped_data_key     bytea       NOT NULL,
  kek_id               text        NOT NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, subject_ref),
  FOREIGN KEY (tenant_id, subject_ref) REFERENCES vault.subject (tenant_id, ref)
);
COMMENT ON TABLE vault.subject_key IS 'Cặp khóa X25519 và khóa dữ liệu của chủ thể, bọc bằng KEK. Không phải sổ: xóa hẳn khi xóa định danh (RB-3).';
SELECT shared.apply_tenant_rls('vault.subject_key');

-- ---------------------------------------------------------------------------
-- Chỉ mục mù: HMAC-SHA256(khóa riêng, tenant ‖ loại ‖ giá trị chuẩn hóa).
-- Duy nhất theo (tenant, loại, giá trị) → chống trùng chủ thể trong tenant.
-- ---------------------------------------------------------------------------
CREATE TABLE vault.blind_index (
  tenant_id   uuid  NOT NULL,
  kind        text  NOT NULL CHECK (kind IN ('phone', 'national_id')),
  value       bytea NOT NULL CHECK (length(value) = 32),
  subject_ref uuid  NOT NULL,
  PRIMARY KEY (tenant_id, kind, value),
  FOREIGN KEY (tenant_id, subject_ref) REFERENCES vault.subject (tenant_id, ref)
);
CREATE INDEX blind_index_subject ON vault.blind_index (tenant_id, subject_ref);
COMMENT ON TABLE vault.blind_index IS 'Chỉ mục mù để tra/chống trùng. Không phải sổ: xóa hẳn khi xóa định danh (RB-3).';
SELECT shared.apply_tenant_rls('vault.blind_index');

-- ---------------------------------------------------------------------------
-- Sổ truy cập (mẫu mục 8.1). Chuỗi băm theo chủ thể: 'subject:<ref>';
-- tra cứu không thấy ghi vào chuỗi 'lookup'.
-- ---------------------------------------------------------------------------
CREATE TABLE vault.access_log (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,                 -- RB-2
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  subject_ref       uuid        NULL,                     -- NULL khi tra cứu không thấy
  op                text        NOT NULL CHECK (op IN ('create', 'display', 'resolve', 'lookup', 'unwrap', 'erase')),
  purpose           text        NOT NULL CHECK (purpose ~ '^[a-z][a-z0-9_.]{2,63}$'),  -- R-42
  outcome           text        NOT NULL CHECK (outcome IN ('ok', 'not_found', 'erased', 'duplicate', 'rejected')),
  trace_id          text        NULL,
  event_time        timestamptz NOT NULL,                 -- R-13
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người yêu cầu = sub của token người dùng cuối
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10)
) PARTITION BY RANGE (recorded_at);

-- Tính duy nhất của idem_key và (stream_key, seq) do shared.fn_chain bảo đảm (ADR-0012).
CREATE INDEX access_log_stream  ON vault.access_log (tenant_id, stream_key, seq);
CREATE INDEX access_log_subject ON vault.access_log (tenant_id, subject_ref, recorded_at);
CREATE INDEX access_log_actor   ON vault.access_log (tenant_id, actor_ref, recorded_at);

SELECT shared.apply_ledger('vault.access_log', 'vault.access_log');

-- ---------------------------------------------------------------------------
-- Quyền của vai vault_app (R-05). Không có DELETE trên subject (dấu mộ giữ lại);
-- sổ không có UPDATE/DELETE.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT, UPDATE ON vault.subject TO vault_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON vault.identity, vault.subject_key, vault.blind_index TO vault_app;
GRANT SELECT, INSERT ON vault.access_log TO vault_app;
REVOKE UPDATE, DELETE, TRUNCATE ON vault.access_log FROM vault_app;
