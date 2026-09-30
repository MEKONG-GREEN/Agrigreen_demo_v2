-- pf_notify G3-2: thông báo thu gom rơm MKG cho quản lý HTX (pf/cropcycle → pf/notify).
--
-- Loại thông báo mới (chủ đề theo HTX + vai coop_manager, không có dữ liệu cá nhân):
--   - collection_schedule : MKG công bố / đổi lịch gom của HTX (van collection_schedule);
--   - collection_statement: MKG gửi bảng kê số cuộn đã gom trên một thửa, hoặc bản sửa
--                           (van htx_collection_statement) — HTX cần xác nhận hoặc tranh chấp.
-- Giữ nguyên các loại đã có: đọc định nghĩa CHECK hiện hành rồi cộng thêm (mẫu pf 000034).

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
  EXECUTE format('ALTER TABLE pf_notify.notification ADD CONSTRAINT notification_kind_check CHECK ((%s) OR kind IN (%L, %L))',
                 substring(def FROM '^CHECK \((.*)\)$'), 'collection_schedule', 'collection_statement');
END
$$;
