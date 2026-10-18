-- pf_accounts D24 (CEO 13/10/2026 — QĐ-4 c, W10.03/W10.05): vô hiệu tài khoản không hoạt động và đặt lại TOTP.
--
--   account_event.action  + 'dormant_warned'  job accounts_dormant báo trước (mặc định ngày 38): tài khoản vẫn hoạt
--                                             động; details = last_activity_at, disable_at, idle_days. Mỗi mốc hoạt
--                                             động chỉ báo một lần (idem_key 'dormant_warn:<user>:<mốc>').
--                         + 'totp_reset'      quản trị viên gỡ TOTP (mất điện thoại…): Keycloak xóa thông tin xác thực
--                                             otp, thêm CONFIGURE_TOTP, thu hồi phiên; lý do ≥ 20 ký tự như khóa/mở.
--   lock_cause            + 'dormant_45d'     job khóa (action 'locked', actor = tác nhân hệ thống) khi không hoạt
--                                             động quá accounts.dormant_disable_days (mặc định 45). Chỉ Admin mở khóa;
--                                             nông hộ tự kích hoạt lại bằng OTP chờ kênh SMS (W10.06).
--
-- Job ở worker tìm tenant bằng ag_ops_jobs (chỉ cột tenant_id, enabled của bảng hiện tại — mẫu pf 000114); việc
-- thật chạy bằng accounts_app trong ngữ cảnh từng tenant (R-05). Không UPDATE/DELETE dữ liệu (R-25), không PII.

SET LOCAL lock_timeout = '5s';

ALTER TABLE pf_accounts.account_event DROP CONSTRAINT account_event_action_check;
ALTER TABLE pf_accounts.account_event ADD CONSTRAINT account_event_action_check
  CHECK (action IN ('created', 'locked', 'unlocked', 'assignment_changed', 'actions_sent', 'dormant_warned', 'totp_reset')) NOT VALID;
ALTER TABLE pf_accounts.account_event VALIDATE CONSTRAINT account_event_action_check;

ALTER TABLE pf_accounts.account_event DROP CONSTRAINT account_event_lock_cause_check;
ALTER TABLE pf_accounts.account_event ADD CONSTRAINT account_event_lock_cause_check
  CHECK (lock_cause IN ('manual', 'org_deactivated', 'dormant_45d')) NOT VALID;
ALTER TABLE pf_accounts.account_event VALIDATE CONSTRAINT account_event_lock_cause_check;

ALTER TABLE pf_accounts.account DROP CONSTRAINT account_lock_cause_check;
ALTER TABLE pf_accounts.account ADD CONSTRAINT account_lock_cause_check
  CHECK (lock_cause IN ('manual', 'org_deactivated', 'dormant_45d')) NOT VALID;
ALTER TABLE pf_accounts.account VALIDATE CONSTRAINT account_lock_cause_check;

-- Đặt lại TOTP bắt buộc lý do như khóa/mở khóa; báo trước không đổi trạng thái (tài khoản đang hoạt động).
ALTER TABLE pf_accounts.account_event ADD CONSTRAINT account_event_totp_reset_reason
  CHECK (action <> 'totp_reset' OR char_length(btrim(reason)) >= 20) NOT VALID;
ALTER TABLE pf_accounts.account_event VALIDATE CONSTRAINT account_event_totp_reset_reason;
ALTER TABLE pf_accounts.account_event ADD CONSTRAINT account_event_dormant_warned_enabled
  CHECK (action <> 'dormant_warned' OR enabled) NOT VALID;
ALTER TABLE pf_accounts.account_event VALIDATE CONSTRAINT account_event_dormant_warned_enabled;

-- Job accounts_dormant: tìm tenant có tài khoản đang hoạt động.
GRANT USAGE ON SCHEMA pf_accounts TO ag_ops_jobs;
GRANT SELECT (tenant_id, enabled) ON pf_accounts.account TO ag_ops_jobs;
