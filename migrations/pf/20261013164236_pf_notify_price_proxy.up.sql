-- W11.77 (BRD Báo giá nông sản v1.3 FN-TI-02.09 hậu điều kiện "cán bộ Xã nhận thông báo"): loại thông báo
-- price_proxy_entry — Cấp Tỉnh nhập thay báo giá Nộp muộn cho xã → cán bộ Xã phụ trách chính của xã tại ngày báo giá
-- (sự kiện pf.price.ticket.proxied của pf/price). Mẫu pf 20261013070025: đọc CHECK hiện hành rồi cộng thêm, giữ nguyên
-- các loại đã có.

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
                 substring(def FROM '^CHECK \((.*)\)$'), 'price_proxy_entry');
END
$$;
