-- Forward-only: retain the validated storage contract on application rollback.
SET LOCAL lock_timeout = '5s';
DO $migration$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conrelid = 'pf_notify.notification'::regclass
       AND conname = 'notification_kind_key_shape'
       AND convalidated
  ) THEN
    RAISE EXCEPTION 'notification kind shape must be validated before removing legacy check';
  END IF;
END
$migration$;
ALTER TABLE pf_notify.notification
  DROP CONSTRAINT notification_kind_check;
