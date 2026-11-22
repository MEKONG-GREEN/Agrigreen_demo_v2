SET LOCAL lock_timeout = '5s';

-- BF-CNTS-CAMPAIGN-CAS-CONSUMER (#201, nối port #159 BF-FORMS-PERIOD-CAS vào đợt điều tra CNTS).
--
-- Trước (#156): yêu cầu đã MẤT ý định thao tác vẫn gọi được ChangeDue/ClosePeriod/ReopenPeriod vô điều kiện và đè kỳ native
-- sau khi yêu cầu kế tiếp đã ghi xong; chỉ có đối soát sau ghi (stale_gen/synced_gen).
--
-- Sau: mọi lời gọi ghi kỳ native của đợt dùng cổng có điều kiện formsport.ConditionalPeriodControl. Revision kỳ native (head
-- seq của hai sổ period_event, period_due — đọc bằng ControlSnapshot NGOÀI giao dịch pf_extension) được GHI BỀN vào ý định
-- của token trong giao dịch ngắn xác minh token còn giữ ý định còn hạn (giờ DB), TRƯỚC khi gọi cap/forms; giá trị truyền vào
-- lời gọi CAS là giá trị đọc lại từ dòng ý định. Mất ý định khi ghi revision → dừng, không gọi cap/forms. Chuỗi Reopen →
-- ChangeDue ghi revision áp dụng của bước trước vào ý định (xác minh lại quyền giữ) rồi mới gọi bước sau.
-- Revision gắn với token giữ ý định: Claim và Release luôn xóa revision (ràng buộc dưới đây).
-- stale_gen/synced_gen giữ làm lớp phòng thủ.

ALTER TABLE pf_extension.cnts_campaign_intent
  ADD COLUMN rev_period_id uuid   NULL,
  ADD COLUMN rev_profile   text   NULL,
  ADD COLUMN rev_event_seq bigint NULL,
  ADD COLUMN rev_due_seq   bigint NULL;

ALTER TABLE pf_extension.cnts_campaign_intent
  ADD CONSTRAINT cnts_campaign_intent_revision CHECK (
    (rev_period_id IS NULL) = (rev_profile IS NULL)
    AND (rev_period_id IS NULL) = (rev_event_seq IS NULL)
    AND (rev_period_id IS NULL) = (rev_due_seq IS NULL)
    AND (rev_period_id IS NULL OR claim_id IS NOT NULL)
    AND (rev_event_seq IS NULL OR rev_event_seq >= 1)
    AND (rev_due_seq IS NULL OR rev_due_seq >= 0)
  ) NOT VALID;

ALTER TABLE pf_extension.cnts_campaign_intent VALIDATE CONSTRAINT cnts_campaign_intent_revision;
