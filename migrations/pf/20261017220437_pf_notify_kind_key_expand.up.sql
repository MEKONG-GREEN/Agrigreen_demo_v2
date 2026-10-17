-- Expand storage without rewriting historical notifications.
-- Registration remains enforced by the application before any SQL write.
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_notify.notification
  ADD CONSTRAINT notification_kind_key_shape
  CHECK ((kind COLLATE "C") ~ '^[a-z][a-z_]{0,39}$') NOT VALID;
