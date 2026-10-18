-- vault/identity — W11.22 (hồ sơ nhân sự HCTH, BRD HCTH v3.0 FN-07; RB-3, R-42): NGĂN HỒ SƠ mã hóa riêng.
--
-- 1. vault.section — mỗi (chủ thể, ngăn) một dòng: tập trường định danh/nhạy cảm (JSON) mã hóa AES-256-GCM bằng KHÓA
--    DỮ LIỆU RIÊNG CỦA NGĂN (sinh mới mỗi lần ghi, bọc bằng KEK của KMS, AAD = ngăn/tenant/chủ thể) — không dùng chung
--    khóa dữ liệu của định danh. Ngăn và mục đích gắn cứng ở domain (domain.SectionPurpose): 'hrm.profile' (ngày sinh,
--    giới tính, email, ghi chú) chỉ đọc/ghi với mục đích hrm.profile; 'hrm.sensitive' (thẻ Thống kê BC Sở TK-01..06:
--    dân tộc, tôn giáo, ngày vào Đảng, quê quán, nơi sinh, ngày TGCM) chỉ với mục đích hrm.sensitive (đọc bắt buộc MFA
--    khi bật AG_VAULT_REQUIRE_MFA). KHÔNG phải sổ: xóa hẳn khi xóa định danh (RB-3), giống vault.identity.
-- 2. vault.access_log.op thêm 'amend' (sửa họ tên/SĐT/địa chỉ), 'section_write', 'section_read'. Sổ có chuỗi băm: chỉ
--    thay ràng buộc CHECK (tập cũ là tập con; CHECK mới access_log_op_v2), KHÔNG thêm cột (bài học W4.1).
-- 3. Quyền: r_identity (vai nhóm của module, vault 000003) SELECT/INSERT/UPDATE/DELETE trên vault.section.

CREATE TABLE vault.section (
  tenant_id    uuid        NOT NULL,
  subject_ref  uuid        NOT NULL,
  section      text        NOT NULL CHECK (section IN ('hrm.profile', 'hrm.sensitive')),
  payload_ct   bytea       NOT NULL,                     -- nonce 12 byte ‖ AES-256-GCM(khóa của ngăn, JSON, aad)
  wrapped_key  bytea       NOT NULL,                     -- khóa của ngăn bọc bằng KEK
  kek_id       text        NOT NULL,
  updated_at   timestamptz NOT NULL DEFAULT now(),
  updated_by   uuid        NOT NULL,
  PRIMARY KEY (tenant_id, subject_ref, section),
  FOREIGN KEY (tenant_id, subject_ref) REFERENCES vault.subject (tenant_id, ref)
);
COMMENT ON TABLE vault.section IS 'Ngăn hồ sơ mã hóa bằng khóa riêng của ngăn (W11.22). Không phải sổ: xóa hẳn khi xóa định danh (RB-3).';
SELECT shared.apply_tenant_rls('vault.section');

-- Mẫu khóa nhẹ (pf 000143): thêm CHECK mới NOT VALID → VALIDATE → bỏ CHECK cũ. Dòng cũ đều thỏa CHECK mới.
SET LOCAL lock_timeout = '5s';
ALTER TABLE vault.access_log ADD CONSTRAINT access_log_op_v2
  CHECK (op IN ('create', 'display', 'resolve', 'lookup', 'unwrap', 'erase', 'amend', 'section_write', 'section_read')) NOT VALID;
ALTER TABLE vault.access_log VALIDATE CONSTRAINT access_log_op_v2;
ALTER TABLE vault.access_log DROP CONSTRAINT access_log_op_check;

REVOKE ALL ON vault.section FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE, DELETE ON vault.section TO r_identity;
