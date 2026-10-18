-- pf_notify D24 (CEO 13/10/2026 — W10.05): loại thông báo account_dormant — báo trước khóa tài khoản không hoạt động
-- (pf.accounts.user.dormant_warning của job accounts_dormant, mặc định ngày 38/45) gửi cho CHÍNH tài khoản, trong ứng
-- dụng + email theo người nhận (pf_notify.email_delivery). Mẫu pf 20261013070025: đọc CHECK hiện hành rồi cộng thêm,
-- giữ nguyên các loại đã có. Không UPDATE/DELETE dữ liệu (R-25), không PII.

SET LOCAL lock_timeout = '5s';

DO $$
DECLARE
  def text;
BEGIN
  SELECT pg_get_constraintdef(c.oid) INTO def
    FROM pg_constraint c
   WHERE c.conrelid = 'pf_notify.notification'::regclass AND c.conname = 'notification_kind_check';
  IF def IS NULL THEN
    RAISE EXCEPTION 'thiếu ràng buộc pf_notify.notification.notification_kind_check';
  END IF;
  EXECUTE 'ALTER TABLE pf_notify.notification DROP CONSTRAINT notification_kind_check';
  EXECUTE format('ALTER TABLE pf_notify.notification ADD CONSTRAINT notification_kind_check CHECK ((%s) OR kind IN (%L)) NOT VALID',
                 substring(def FROM '^CHECK \((.*)\)$'), 'account_dormant');
  EXECUTE 'ALTER TABLE pf_notify.notification VALIDATE CONSTRAINT notification_kind_check';
END
$$;
