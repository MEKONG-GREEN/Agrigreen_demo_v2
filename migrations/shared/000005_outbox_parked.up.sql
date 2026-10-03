-- Review Go 03/10/2026 (M6): sự kiện outbox bị broker TỪ CHỐI (nack) lặp lại không còn chặn mọi sự kiện sau nó
-- (publisher dừng ở lỗi đầu để giữ thứ tự). Sau outbox.ParkAfterRejections lần, publisher GÁC sự kiện (parked_at)
-- và đi tiếp; lỗi kết nối broker KHÔNG bao giờ gác (giữ thứ tự, thử lại mãi). Gác ≠ đã phát: metric
-- ag.outbox.parked > 0 phải cảnh báo; mở lại bằng tay (docs/runbooks):
--   UPDATE shared.outbox SET parked_at = NULL, attempts = 0 WHERE id = '<id>';
-- Chạy bằng vai ag_migrator (R-25). Chỉ tiến (R-24).
ALTER TABLE shared.outbox ADD COLUMN parked_at timestamptz NULL;
CREATE INDEX outbox_parked ON shared.outbox (id) WHERE parked_at IS NOT NULL AND published_at IS NULL;
GRANT UPDATE (parked_at) ON shared.outbox TO ag_outbox_publisher;
