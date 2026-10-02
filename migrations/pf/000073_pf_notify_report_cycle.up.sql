-- pf_notify KN v7.7 (gói P4, docs/brd/kn-v7.7-plan.md §2 pf 000073): kiểu thông báo mới của vòng đời báo
-- cáo (pf/extension → pf/notify) và của lịch công tác tuần (P5, consumer pf.notify.work_schedule).
--
--   report_task_assigned        công việc nộp biểu mới sau phát hành → Cán bộ xã phụ trách (FN-TI-02.02 AC)
--   report_task_due             tự nhắc trước hạn / khi trễ hạn theo cấu hình (FN-TI-02.03, Q-15)
--   report_reminder             Cấp Tỉnh "Gửi nhắc nhở" kèm nội dung (FN-TI-02.03 AC: nhận trong 1 phút)
--   report_adjustment_requested yêu cầu điều chỉnh kèm lý do (FN-TI-02.04 AC)
--   report_period_reopened      mở lại kỳ cho xã/biểu, kèm hạn mở (FN-TI-02.04)
--   work_schedule_submitted     lịch công tác gửi duyệt / điều chỉnh chờ duyệt lại → người duyệt (P5)
--   work_schedule_reviewed      kết quả duyệt lịch kèm lý do (P5, FN-XA-03.08 AC)
--   work_schedule_due           nhắc hạn đăng ký / lịch chờ duyệt quá hạn (P5)
--
-- Giữ nguyên các loại đã có: đọc định nghĩa CHECK hiện hành rồi cộng thêm (mẫu pf 000041).

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
  EXECUTE format('ALTER TABLE pf_notify.notification ADD CONSTRAINT notification_kind_check CHECK ((%s) OR kind IN (%L, %L, %L, %L, %L, %L, %L, %L))',
                 substring(def FROM '^CHECK \((.*)\)$'),
                 'report_task_assigned', 'report_task_due', 'report_reminder', 'report_adjustment_requested', 'report_period_reopened',
                 'work_schedule_submitted', 'work_schedule_reviewed', 'work_schedule_due');
END
$$;
