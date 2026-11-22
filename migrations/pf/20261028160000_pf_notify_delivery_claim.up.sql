-- BF-NOTIFY-POOL (#155): consumer pf.notify.push / pf.notify.email KHÔNG giữ giao dịch inbox trong lúc gửi FCM/SMTP hay
-- tra danh bạ thư (cùng lớp lỗi với P0-POOL-SHARED-2 #143, mẫu pf 20261025135114_pf_reports_delivery_claim).
--
-- Trước: handler chạy trong giao dịch inbox (giữ một kết nối) rồi mở giao dịch RIÊNG trên cùng pool để đọc lô và ghi
-- kết quả từng lần gửi — pool cạn thì chờ kết nối thứ hai tới hết hạn, không gửi được. Giao lặp đồng thời bị chặn ở dòng
-- inbox. Sau: các bước ngắn, không giữ kết nối nào trong lúc gọi ra ngoài —
--   1. giao dịch NHẬN một lần gửi (ngoài giao dịch inbox): ghi claim_id + claim_until khi lần gửi còn mở và chưa ai giữ
--      còn hạn, commit — giao lặp đồng thời không gửi song song;
--   2. gửi FCM / tra email + gửi SMTP NGOÀI giao dịch;
--   3. giao dịch KẾT: ghi kết quả (status, attempts, …) + xóa nhận, chỉ khi nhận còn là của mình.
-- Tiến trình chết giữa 1 và 3 → hết claim_until thì nhận lại (ít nhất một lần, như trước).
-- push_delivery / email_delivery là bảng trạng thái (đã có UPDATE), cột mới NULL được; GRANT UPDATE mức bảng của
-- notify_app / r_notify đã bao cột mới. Không UPDATE/DELETE dữ liệu, không đổi RLS, không PII.

SET LOCAL lock_timeout = '5s';

ALTER TABLE pf_notify.push_delivery
  ADD COLUMN claim_id    uuid        NULL,
  ADD COLUMN claim_until timestamptz NULL;
ALTER TABLE pf_notify.push_delivery
  ADD CONSTRAINT push_delivery_claim_check CHECK ((claim_id IS NULL) = (claim_until IS NULL)) NOT VALID;
ALTER TABLE pf_notify.push_delivery VALIDATE CONSTRAINT push_delivery_claim_check;

ALTER TABLE pf_notify.email_delivery
  ADD COLUMN claim_id    uuid        NULL,
  ADD COLUMN claim_until timestamptz NULL;
ALTER TABLE pf_notify.email_delivery
  ADD CONSTRAINT email_delivery_claim_check CHECK ((claim_id IS NULL) = (claim_until IS NULL)) NOT VALID;
ALTER TABLE pf_notify.email_delivery VALIDATE CONSTRAINT email_delivery_claim_check;
