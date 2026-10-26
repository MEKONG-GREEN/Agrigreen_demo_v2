-- pf_notify P0-NOTIFY-FAILCLOSED (#127, Codex P2): thời điểm của lần thử/kết quả gần nhất cho bản gửi đẩy và email.
-- GET /v1/notify/delivery-health so lần bỏ vì kênh TẮT với lần gửi thành công gần nhất; created_at là lúc vào hàng đợi,
-- không phải lúc thử — một lô cũ xử lý muộn (sau khi kênh tắt) bị so nhầm và cảnh báo bị che. finished_at được ghi
-- cùng UPDATE của FinishDelivery/FinishEmailDelivery (cùng giao dịch với status/last_error/sent_at).
-- Chỉ thêm cột NULL (dòng cũ giữ NULL; truy vấn dùng coalesce(finished_at, created_at)). Không UPDATE/DELETE dữ liệu,
-- không đổi RLS/quyền (GRANT UPDATE cấp cho cả bảng), không PII.

SET LOCAL lock_timeout = '5s';

ALTER TABLE pf_notify.push_delivery ADD COLUMN finished_at timestamptz NULL;
ALTER TABLE pf_notify.email_delivery ADD COLUMN finished_at timestamptz NULL;
