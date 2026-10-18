-- W11.46: thêm thông báo đổi ca KSGM; giữ toàn bộ loại thông báo hiện có.
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
                 substring(def FROM '^CHECK \((.*)\)$'), 'cnts_duty_changed');
END
$$;

ALTER TABLE pf_notify.notification VALIDATE CONSTRAINT notification_kind_check;
