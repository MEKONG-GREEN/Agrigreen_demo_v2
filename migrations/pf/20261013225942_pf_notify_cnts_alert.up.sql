-- W11.43 (D4 CN3; BRD CNTS v3.7 FN-13, UR-TINH-08, UR-TINH-13, Mục III.5): thông báo cảnh báo và nhắc nộp biểu của phòng
-- Chăn nuôi – Thủy sản (pf/extension → pf/notify, sự kiện pf.extension.cnts_alert.changed / pf.extension.cnts_form.reminded).
--
-- pf_notify.notification.kind thêm:
--   cnts_alert           cảnh báo mới (Khẩn / Cần chú ý) → người nhận theo bảng Mục III.5 (người phụ trách biểu, người
--                        nhập, cán bộ xã, Lãnh đạo phòng…)
--   cnts_alert_assigned  Cấp Tỉnh giao xử lý cảnh báo → người được giao
--   cnts_alert_overdue   cảnh báo quá hạn xử lý → Lãnh đạo phòng (+ người được giao)
--   cnts_form_reminder   nhắc nộp biểu kỳ CNTS (trước hạn, khi trễ, nhắc lặp) theo cấu hình nhắc của biểu (UR-TINH-08)
-- Mẫu pf 000041 / 000073 / 000083 / 20261013070025: đọc CHECK hiện hành rồi cộng thêm, giữ nguyên các loại đã có.

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
  EXECUTE format('ALTER TABLE pf_notify.notification ADD CONSTRAINT notification_kind_check CHECK ((%s) OR kind IN (%L, %L, %L, %L))',
                 substring(def FROM '^CHECK \((.*)\)$'), 'cnts_alert', 'cnts_alert_assigned', 'cnts_alert_overdue', 'cnts_form_reminder');
END
$$;
