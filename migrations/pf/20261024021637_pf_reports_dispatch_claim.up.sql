-- pf_reports P0-WORKER-REPORTS (#128): NHẬN VIỆC gửi báo cáo Chi cục có thời hạn (lease) — một lần gửi chỉ một worker
-- gửi email tại một thời điểm.
--
-- pf_reports.dispatch là BẢNG CHIẾU (không phải sổ) — thêm cột được; sổ dispatch_event giữ nguyên (chỉ ghi thêm).
--   claim_token  mã nhận việc của worker đang gửi (NULL = chưa ai nhận / đã xong)
--   claim_until  hạn nhận việc; worker gia hạn sau mỗi người nhận. Quá hạn khi còn 'sending' → worker khác (giao lại
--                sự kiện) hoặc người dùng "Gửi lại" tiếp tục CÙNG lượt gửi: người đã có dấu recipient_mailed không bị gửi lại.
-- Nhận việc = một câu UPDATE … WHERE status = 'sending' AND attempts = $n AND (claim_until IS NULL OR claim_until <= now)
-- (một câu, đếm dòng bị đổi) — giao dịch riêng, commit trước khi gửi.

SET LOCAL lock_timeout = '5s';

ALTER TABLE pf_reports.dispatch
  ADD COLUMN claim_token uuid NULL,
  ADD COLUMN claim_until timestamptz NULL;

COMMENT ON COLUMN pf_reports.dispatch.claim_token IS 'P0-WORKER-REPORTS: mã nhận việc của worker đang gửi email (NULL = không ai giữ)';
COMMENT ON COLUMN pf_reports.dispatch.claim_until IS 'P0-WORKER-REPORTS: hạn nhận việc (lease) — quá hạn khi còn sending thì được nhận lại';
