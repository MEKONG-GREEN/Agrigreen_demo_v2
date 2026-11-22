SET LOCAL lock_timeout = '5s';

-- BF-CN-FORM-DESIGN-EDIT (#192, BRD CNTS v4.0 FN-21 B1 "biểu và phiên bản", UR-TINH-26 "biểu điều tra (thiết kế tại FN-07,
-- phiên bản)"; FN-07 BR-01): đợt điều tra ghi phiên bản biểu đã chọn. Phát hành dùng đúng template_id của phiên bản này,
-- không tự lấy phiên bản mới nhất. NULL = phiên bản Nháp ghi trước gói này (giữ hành vi cũ: mới nhất lúc phát hành).
-- Sổ chỉ ghi thêm: chỉ thêm cột cho phiên bản Nháp mới, không sửa dòng cũ.

ALTER TABLE pf_extension.cnts_campaign
  ADD COLUMN template_version bigint NULL;
-- ADR-0012: cột thêm vào sổ đăng ký để verify_chain xác minh được các dòng ghi trước migration này.
SELECT shared.register_ledger_columns('pf_extension.cnts_campaign', ARRAY['template_version']);

ALTER TABLE pf_extension.cnts_campaign
  ADD CONSTRAINT cnts_campaign_template_version CHECK (template_version IS NULL OR template_version >= 1) NOT VALID;

ALTER TABLE pf_extension.cnts_campaign VALIDATE CONSTRAINT cnts_campaign_template_version;
