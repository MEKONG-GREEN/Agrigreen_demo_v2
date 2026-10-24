-- Expand-only: existing/native notifications retain their inbox visibility.
-- The row is also the durable deduplication anchor for push/email-only notices.
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_notify.notification
    ADD COLUMN in_app boolean NOT NULL DEFAULT true;
