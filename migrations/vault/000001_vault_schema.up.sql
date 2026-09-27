-- Cụm kho định danh (mục 10). G0 chỉ dựng schema và quyền; bảng
-- household_identity, khóa X25519 theo hộ và blind index làm ở G1 (ADR-0004).
-- R-04: không module nào ngoài vault/identity có vai trên database này.

CREATE SCHEMA vault;
REVOKE ALL ON SCHEMA vault FROM PUBLIC;
GRANT USAGE ON SCHEMA vault TO vault_app;
