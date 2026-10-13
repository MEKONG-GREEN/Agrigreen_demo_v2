-- Gói NOTIF (G1, quyết định CEO D5; BRD Báo giá nông sản v1.3 FN-XA-03.04, FN-XA-03.05, FN-TI-02.03, FN-TI-02.06, NFR
-- "Thông báo"): thông báo chuông + email của Báo giá nông sản (pf/price → pf/notify).
--
-- 1. pf_notify.notification.kind thêm các loại riêng của Báo giá (W11.74 tạm dùng report_task_due cho nhắc nhập giá):
--      price_reminder         nhắc nhập giá lần 1 / lần 2 → cán bộ Xã (FN-XA-03.04)
--      price_alert            cảnh báo biến động Tạm tính mới khi Xã gửi → Cấp Tỉnh phụ trách (FN-TI-02.03 B1)
--      price_verify_request   yêu cầu xác minh → cán bộ Xã (FN-TI-02.03 B3, FN-XA-03.05)
--      price_verify_reply     Xã phản hồi xác minh → Cấp Tỉnh (FN-XA-03.05 B4)
--      price_verify_decision  kết quả xử lý cảnh báo / hiệu chỉnh → cán bộ Xã (FN-TI-02.03 B6, FN-TI-02.04)
--      price_lock_summary     tổng hợp lúc khóa nhập 12:00 → Cấp Tỉnh (FN-TI-02.03 B4)
--      price_board_approved   bảng giá ngày đã duyệt (phiên bản mới) → Cấp Tỉnh (FN-TI-02.05)
--      price_board_reopened   bảng giá ngày đang mở duyệt → Cấp Tỉnh (FN-TI-02.06)
--    Mẫu pf 000041 / 000073 / 000083: đọc CHECK hiện hành rồi cộng thêm, giữ nguyên các loại đã có.
--
-- 2. pf_notify.email_delivery: email THEO TỪNG NGƯỜI NHẬN của một thông báo (consumer pf.notify.email). Bảng trạng thái
--    như push_delivery (status/attempts/sent_at được UPDATE). KHÔNG lưu địa chỉ email: địa chỉ tra lúc gửi qua pf/accounts
--    (Keycloak, R-04) và không ghi log đầy đủ. Một thông báo người nhận có tối đa một lần gửi email (UNIQUE) — sự kiện
--    nguồn giao lặp không tạo email trùng; lần gửi đã 'sent' không gửi lại khi hàng đợi giao lại lô.

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
                 substring(def FROM '^CHECK \((.*)\)$'), 'price_reminder', 'price_alert', 'price_verify_request',
                 'price_verify_reply', 'price_verify_decision', 'price_lock_summary', 'price_board_approved',
                 'price_board_reopened');
END
$$;

CREATE TABLE pf_notify.email_delivery (
  tenant_id       uuid        NOT NULL,
  id              uuid        NOT NULL,                    -- UUIDv7 (R-11)
  batch_id        uuid        NOT NULL,                    -- = batch_id của pf.notify.email.requested
  notification_id uuid        NOT NULL,
  recipient_ref   uuid        NOT NULL,                    -- actor_ref; địa chỉ tra lúc gửi, không lưu
  kind            text        NOT NULL CHECK (kind ~ '^[a-z_]{1,40}$'),
  status          text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed', 'dropped')),
  attempts        integer     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  last_error      text        NULL CHECK (char_length(last_error) <= 200),   -- chỉ mã lỗi, không địa chỉ
  created_at      timestamptz NOT NULL,
  sent_at         timestamptz NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, notification_id)
);
CREATE INDEX email_delivery_batch ON pf_notify.email_delivery (tenant_id, batch_id);
SELECT shared.apply_tenant_rls('pf_notify.email_delivery');
GRANT SELECT, INSERT, UPDATE ON pf_notify.email_delivery TO r_notify;
