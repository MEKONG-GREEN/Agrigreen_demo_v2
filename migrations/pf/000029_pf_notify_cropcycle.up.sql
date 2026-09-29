-- pf_notify G2 đợt 2: thông báo liên thông App HTX (pf/cropcycle → pf/notify).
--
-- Loại thông báo mới:
--   - farm_log_pending : nông dân ghi nhật ký canh tác → quản lý HTX "nhật ký chờ duyệt"
--                        (HTX US-LOG-05, thông báo chủ đề theo HTX + vai coop_manager);
--   - farm_log_reviewed: quản lý HTX duyệt / yêu cầu bổ sung → người ghi nhật ký
--                        (HTX US-LOG-05 AC-3);
--   - season_opened    : nông dân tự mở vụ → quản lý HTX (HTX US-SEASON-01).
-- Cảnh báo khẩn tới nông dân theo địa bàn (HTX US-NEWS-03) dùng lại kind 'alert'
-- (thông báo cá nhân theo tài khoản nông dân) — không cần cột mới.

ALTER TABLE pf_notify.notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE pf_notify.notification ADD CONSTRAINT notification_kind_check CHECK (kind IN (
  'directive', 'alert', 'document', 'task_escalated', 'sync_conflict', 'org_deactivated',
  'household_registered', 'household_membership_changed', 'task_assigned',
  'farm_log_pending', 'farm_log_reviewed', 'season_opened'));
