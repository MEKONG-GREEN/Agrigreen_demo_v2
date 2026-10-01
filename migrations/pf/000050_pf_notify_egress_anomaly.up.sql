-- G5 tích hợp (02/10/2026, số dự phòng tích hợp DLP của g5-conventions §2): consumer
-- pf.notify.egress_anomaly (pf/notify) ghi thông báo loại egress_anomaly cho người nhận DLP của tenant
-- CHỦ DỮ LIỆU khi pf/metering phát pf.metering.egress.anomaly_detected (ADR-0024, mục 18).
--
-- pf_notify.notification.kind thêm egress_anomaly (mẫu pf 000041 / 000045: đọc CHECK hiện hành rồi
-- cộng thêm, giữ nguyên các loại đã có). Không bảng mới, không dữ liệu.

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
  EXECUTE format('ALTER TABLE pf_notify.notification ADD CONSTRAINT notification_kind_check CHECK ((%s) OR kind IN (%L))',
                 substring(def FROM '^CHECK \((.*)\)$'), 'egress_anomaly');
END
$$;
