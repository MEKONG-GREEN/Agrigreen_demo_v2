-- Validate existing rows in a separate transaction.
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_notify.notification
  VALIDATE CONSTRAINT notification_kind_key_shape;
