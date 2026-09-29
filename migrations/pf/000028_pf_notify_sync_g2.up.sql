-- pf_notify G2: bổ sung cho liên thông App HTX ↔ App Khuyến nông.
--
-- 1. Loại thông báo mới:
--    - household_registered, household_membership_changed: nông hộ mới đăng ký /
--      đổi HTX từ App HTX → báo cán bộ khuyến nông của xã (KN US-HTX-05);
--    - task_assigned: nhiệm vụ tự sinh từ yêu cầu hỗ trợ → báo cán bộ được giao
--      (hoặc quản trị tỉnh khi không có ai để giao) (KN US-TASK-01).
-- 2. Đồng bộ ngay theo nhóm dữ liệu (KN US-SYNC-02 AC): nudge.entities là danh
--    sách entity pf/sync được yêu cầu (rỗng = tất cả). Thông điệp đẩy sync_now mang
--    thêm khóa "entities" (tên entity, không phải dữ liệu cá nhân — RB-7, ADR-0015).
--
-- ADD COLUMN ... DEFAULT '{}' điền sẵn cho dòng cũ (không cần UPDATE, R-25).

ALTER TABLE pf_notify.notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE pf_notify.notification ADD CONSTRAINT notification_kind_check CHECK (kind IN (
  'directive', 'alert', 'document', 'task_escalated', 'sync_conflict', 'org_deactivated',
  'household_registered', 'household_membership_changed', 'task_assigned'));

ALTER TABLE pf_notify.nudge
  ADD COLUMN entities text[] NOT NULL DEFAULT '{}'
    CHECK (cardinality(entities) <= 30 AND array_to_string(entities, ',') ~ '^([a-z_]{1,40}(,[a-z_]{1,40})*)?$');
