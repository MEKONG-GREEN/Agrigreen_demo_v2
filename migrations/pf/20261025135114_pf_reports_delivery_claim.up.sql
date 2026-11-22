SET LOCAL lock_timeout = '5s';

-- P0-POOL-SHARED-2 (#143): email lịch báo cáo KHÔNG gửi SMTP trong giao dịch inbox.
--
-- Trước: consumer pf.reports.export.completed giữ giao dịch inbox (khóa dòng schedule_run FOR UPDATE) trong lúc ký URL,
-- tra email (pf/accounts) và gửi SMTP; giao lặp đồng thời bị chặn ở dòng inbox rồi bỏ qua. Sau: các bước ngắn —
--   1. giao dịch NHẬN (ngoài giao dịch inbox): ghi delivery_claim_id + delivery_claim_until khi lần chạy chưa có kết quả
--      gửi và chưa ai giữ còn hạn, commit — giao lặp đồng thời không gửi song song;
--   2. ký URL, tra email, gửi SMTP NGOÀI giao dịch;
--   3. giao dịch inbox chạy lại: ghi kết quả (delivery_status …) + xóa nhận, cùng commit với inbox (R-61); gửi lỗi →
--      trả nhận (xóa), giao lại gửi lại.
-- Tiến trình chết giữa 2 và 3 → hết delivery_claim_until thì nhận lại (ít nhất một lần, như trước).
-- schedule_run là bảng trạng thái (không phải sổ — có UPDATE), cột mới NULL được; quyền UPDATE mức bảng của r_reports
-- đã bao cột mới.

ALTER TABLE pf_reports.schedule_run
  ADD COLUMN delivery_claim_id    uuid        NULL,
  ADD COLUMN delivery_claim_until timestamptz NULL;
ALTER TABLE pf_reports.schedule_run
  ADD CONSTRAINT schedule_run_delivery_claim_check CHECK ((delivery_claim_id IS NULL) = (delivery_claim_until IS NULL)) NOT VALID;
ALTER TABLE pf_reports.schedule_run VALIDATE CONSTRAINT schedule_run_delivery_claim_check;
