-- pf_accounts AUTH-V2-RESET-NOPIN (#255, ADR-0056; BRD 2.0 Báo giá v2.0 FN-CH-00.01 luồng phụ "Quên mật khẩu?"):
-- người dùng tự đặt lại mật khẩu qua email.
--
--   password_reset  một yêu cầu đã được xếp gửi cho một tài khoản (user_ref = id Keycloak). Không lưu email, IP hay mã
--                   thô: token_hash là SHA-256 của 32 byte bí mật trong liên kết (consumer ghi khi gửi). Liên kết hiệu
--                   lực 30 phút từ lúc sinh mã (expires_at), dùng một lần (used_at); claimed_until là lease ngắn khi
--                   đang gọi Keycloak để hai lần xác nhận song song không cùng dùng một mã. Tối đa 5 dòng/giờ/tài khoản
--                   (đếm theo requested_at, khóa advisory theo tài khoản ở tầng ứng dụng).
--
-- Không UPDATE/DELETE dữ liệu (R-25), không PII.

SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_accounts.password_reset (
  tenant_id     uuid        NOT NULL,
  id            uuid        NOT NULL,                    -- UUIDv7
  user_ref      uuid        NOT NULL,
  requested_at  timestamptz NOT NULL,
  token_hash    bytea       NULL CHECK (token_hash IS NULL OR octet_length(token_hash) = 32),
  sent_at       timestamptz NULL,
  expires_at    timestamptz NULL,
  claimed_until timestamptz NULL,
  used_at       timestamptz NULL,
  recorded_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  CHECK ((token_hash IS NULL) = (expires_at IS NULL)),
  CHECK (sent_at IS NULL OR token_hash IS NOT NULL),
  CHECK (expires_at IS NULL OR expires_at > requested_at),
  CHECK (used_at IS NULL OR token_hash IS NOT NULL)
);
CREATE INDEX password_reset_user ON pf_accounts.password_reset (tenant_id, user_ref, requested_at DESC);
SELECT shared.apply_tenant_rls('pf_accounts.password_reset');

-- Quyền của r_accounts (vai nhóm của module, ADR-0028): không DELETE; UPDATE chỉ các cột trạng thái.
GRANT SELECT, INSERT ON pf_accounts.password_reset TO r_accounts;
GRANT UPDATE (token_hash, sent_at, expires_at, claimed_until, used_at) ON pf_accounts.password_reset TO r_accounts;
