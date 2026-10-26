-- P0-POOL-SHARED (#126): email tóm tắt KHÔNG gửi SMTP trong giao dịch DB.
--
-- Trước: RunEmailDigest khóa mục chờ (FOR UPDATE SKIP LOCKED), gửi email rồi đánh dấu đã gửi — cùng một giao dịch,
-- SMTP chậm giữ kết nối pool. Sau: ba bước ngắn —
--   1. giao dịch NHẬN: khóa mục chờ chưa có người nhận còn hạn, ghi claim_id + claimed_until (hạn nhận), commit;
--   2. gửi email NGOÀI giao dịch;
--   3. giao dịch KẾT: thành công → digest_id = claim_id, sent_at; lỗi → xóa claim_id/claimed_until (lần sau gửi lại).
-- Tiến trình chết giữa 2 và 3 → hết claimed_until thì mục được nhận lại (ít nhất một lần, như trước: trước đây chết
-- sau khi gửi mà chưa commit cũng gửi lại). email_digest_item là bảng trạng thái (không phải sổ), cột mới NULL được.

SET LOCAL lock_timeout = '5s';

ALTER TABLE pf_notify.email_digest_item
  ADD COLUMN claim_id      uuid        NULL,
  ADD COLUMN claimed_until timestamptz NULL;
ALTER TABLE pf_notify.email_digest_item
  ADD CONSTRAINT email_digest_item_claim_check CHECK ((claim_id IS NULL) = (claimed_until IS NULL)) NOT VALID;
ALTER TABLE pf_notify.email_digest_item VALIDATE CONSTRAINT email_digest_item_claim_check;

GRANT UPDATE (claim_id, claimed_until) ON pf_notify.email_digest_item TO r_notify;
