-- Review Go 03/10/2026 (M2): gửi email Chi cục không còn phụ thuộc vào việc giao dịch inbox commit.
-- HandleDispatchRequested ghi liên kết tải và dấu "đã gửi tới người nhận" ở GIAO DỊCH RIÊNG, commit ngay quanh mỗi
-- lần gửi: giao dịch inbox hủy rồi giao lại → không gửi trùng người đã nhận (idem_key
-- 'dispatch-mailed:<dispatch>:<attempt>:<recipient>'), liên kết trong email đã gửi vẫn mở được.
--
-- dispatch_event.action thêm 'recipient_mailed' (đọc CHECK hiện hành rồi cộng thêm, mẫu 000085).

DO $$
DECLARE
  def text;
BEGIN
  SELECT pg_get_constraintdef(c.oid) INTO def
    FROM pg_constraint c
   WHERE c.conrelid = 'pf_reports.dispatch_event'::regclass AND c.conname = 'dispatch_event_action_check';
  IF def IS NULL THEN
    RAISE EXCEPTION 'thiếu ràng buộc pf_reports.dispatch_event.dispatch_event_action_check';
  END IF;
  EXECUTE 'ALTER TABLE pf_reports.dispatch_event DROP CONSTRAINT dispatch_event_action_check';
  EXECUTE format('ALTER TABLE pf_reports.dispatch_event ADD CONSTRAINT dispatch_event_action_check CHECK ((%s) OR action = %L)',
                 substring(def FROM '^CHECK \((.*)\)$'), 'recipient_mailed');
END
$$;
