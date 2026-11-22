-- vault/identity — CNTS-V4-HOUSEHOLD-LIST (#251), ADR-0056 mục 4 (BRD CNTS v4.0 FN-10 HCN-09 "giá trị cũ → mới"): BẢN SỬA
-- ĐỊNH DANH.
--
-- 1. vault.identity_revision — mỗi lần sửa họ tên/SĐT (Amend) một dòng: bản mã họ tên, SĐT TRƯỚC và SAU lần sửa (chép
--    nguyên bản mã của vault.identity — cùng khóa dữ liệu, AAD theo chủ thể). Bên gọi chỉ nhận mã bản sửa (revision_ref);
--    đọc lại chỉ ra NHÃN CHE BỚT (Revisions, op 'revision_read'), không bao giờ ra họ tên/SĐT đầy đủ. KHÔNG phải sổ: xóa
--    hẳn khi xóa định danh (RB-3), giống vault.identity, vault.section.
-- 2. vault.access_log.op thêm 'revision_read'. Sổ có chuỗi băm: chỉ thay ràng buộc CHECK (tập cũ là tập con; CHECK mới
--    access_log_op_v4), KHÔNG thêm cột (bài học W4.1).
-- 3. Quyền: r_identity SELECT, INSERT, DELETE trên vault.identity_revision.

CREATE TABLE vault.identity_revision (
  tenant_id      uuid        NOT NULL,
  ref            uuid        NOT NULL,                 -- revision_ref (UUIDv7)
  subject_ref    uuid        NOT NULL,
  prev_name_ct   bytea       NULL,                     -- bản mã họ tên trước lần sửa (như vault.identity.full_name_ct)
  prev_phone_ct  bytea       NULL,
  next_name_ct   bytea       NULL,                     -- bản mã họ tên sau lần sửa
  next_phone_ct  bytea       NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  created_by     uuid        NOT NULL,
  PRIMARY KEY (tenant_id, ref),
  FOREIGN KEY (tenant_id, subject_ref) REFERENCES vault.subject (tenant_id, ref)
);
CREATE INDEX identity_revision_subject ON vault.identity_revision (tenant_id, subject_ref);
COMMENT ON TABLE vault.identity_revision IS 'Bản sửa họ tên/SĐT (ADR-0056). Không phải sổ: xóa hẳn khi xóa định danh (RB-3).';
SELECT shared.apply_tenant_rls('vault.identity_revision');

-- Mẫu khóa nhẹ (pf 000143): thêm CHECK mới NOT VALID → VALIDATE → bỏ CHECK cũ. Dòng cũ đều thỏa CHECK mới.
SET LOCAL lock_timeout = '5s';
ALTER TABLE vault.access_log ADD CONSTRAINT access_log_op_v4
  CHECK (op IN ('create', 'display', 'resolve', 'lookup', 'unwrap', 'erase', 'amend', 'section_write', 'section_read',
                'card', 'revision_read')) NOT VALID;
ALTER TABLE vault.access_log VALIDATE CONSTRAINT access_log_op_v4;
ALTER TABLE vault.access_log DROP CONSTRAINT access_log_op_v3;

REVOKE ALL ON vault.identity_revision FROM PUBLIC;
GRANT SELECT, INSERT, DELETE ON vault.identity_revision TO r_identity;
