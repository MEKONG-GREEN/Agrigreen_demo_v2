SET LOCAL lock_timeout = '5s';

-- BF-CNTS-CAMPAIGN-POOL (#156, hậu kiểm #27 W11.50): thao tác chuyển trạng thái đợt điều tra CNTS không giữ giao dịch DB quanh
-- lời gọi cap/forms.
--
-- Trước: change() mở giao dịch pf_extension giữ khóa tư vấn cnts_campaign:<tenant>:<đợt> rồi — TRONG giao dịch đó — gọi
-- cap/forms (LatestTemplate, PublishBatch, ChangeDue, ClosePeriod, ReopenPeriod, Progress), mỗi lời gọi mở giao dịch riêng trên
-- CÙNG pool app_platform: một yêu cầu giữ một kết nối trong khi chờ kết nối thứ hai → pool bận thì treo tới hạn rồi 500.
--
-- Sau (cùng mẫu ý định hủy kỳ KHTC 20261025140626): thay khóa giữ suốt bằng Ý ĐỊNH THAO TÁC bền (bảng trạng thái, một dòng/đợt):
--   1. giao dịch ngắn (khóa cnts_campaign): kiểm idempotency/trạng thái, ghi claim_id + claim_until (hạn, giờ DB), commit;
--   2. gọi cap/forms NGOÀI giao dịch (hạn lời gọi trước hạn ý định);
--   3. giao dịch ngắn (khóa): bỏ ý định của token (phải còn hạn) rồi ghi sự kiện đợt (+ outbox nhắc nộp). Lỗi ở 2 → bỏ ý định;
--      lỗi ở 3 → bù ở cap/forms (phát hành: hủy kỳ native) rồi bỏ ý định.
-- Trong lúc ý định còn hạn: trigger chặn sửa nháp (phiên bản mới) và chặn mọi sự kiện đợt — đúng những gì khóa cũ chặn (cả
-- hai trigger lấy cùng khóa tư vấn, nên kiểm ý định ở đây tuần tự với bước 1/3). Tiến trình chết giữa chừng → hết hạn ý định
-- thì thao tác khác chạy được; gọi lại cùng Idempotency-Key dùng cùng khóa idempotent ở cap/forms.

CREATE TABLE pf_extension.cnts_campaign_intent (
  tenant_id   uuid        NOT NULL,
  campaign_id uuid        NOT NULL,
  claim_id    uuid        NULL,
  action      text        NULL,
  claim_until timestamptz NULL,
  -- Đối soát kỳ native (review Codex #156 [P2]): lời gọi cap/forms của yêu cầu đã MẤT ý định có thể ghi đè kỳ native sau khi
  -- yêu cầu kế tiếp đã ghi xong. Yêu cầu phát hiện mình mất ý định (hoặc không rõ kết quả lời gọi cap/forms) tăng stale_gen SAU
  -- khi lời gọi đã trả về; người giữ ý định đưa kỳ native về đúng sổ đợt đã cam kết rồi đặt synced_gen = stale_gen đọc TRƯỚC
  -- khi đối soát. stale_gen > synced_gen = kỳ native có thể lệch sổ đợt, còn phải đối soát.
  stale_gen   bigint      NOT NULL DEFAULT 0,
  synced_gen  bigint      NOT NULL DEFAULT 0,
  updated_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, campaign_id),
  CONSTRAINT cnts_campaign_intent_claim CHECK ((claim_id IS NULL) = (claim_until IS NULL) AND (claim_id IS NULL) = (action IS NULL)),
  CONSTRAINT cnts_campaign_intent_gen CHECK (synced_gen >= 0 AND synced_gen <= stale_gen)
);
SELECT shared.apply_tenant_rls('pf_extension.cnts_campaign_intent');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_extension.cnts_campaign_intent
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_extension.cnts_campaign_intent
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();
GRANT SELECT, INSERT, UPDATE ON pf_extension.cnts_campaign_intent TO r_extension;

-- Ý định thao tác còn hạn của đợt (gọi từ trigger, đã giữ khóa cnts_campaign).
CREATE FUNCTION pf_extension.fn_cnts_campaign_busy(p_tenant uuid, p_campaign uuid) RETURNS boolean
LANGUAGE sql STABLE SET search_path = pg_catalog, pg_temp AS $$
  SELECT EXISTS (SELECT 1 FROM pf_extension.cnts_campaign_intent i
                  WHERE i.tenant_id = p_tenant AND i.campaign_id = p_campaign AND i.claim_until > now())
$$;
REVOKE ALL ON FUNCTION pf_extension.fn_cnts_campaign_busy(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_extension.fn_cnts_campaign_busy(uuid, uuid) TO r_extension;

-- Phiên bản nội dung: như 20261024091058, thêm chặn sửa nháp khi đang có thao tác (phát hành) dở dang.
CREATE OR REPLACE FUNCTION pf_extension.fn_cnts_campaign_draft() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('cnts_campaign:' || NEW.tenant_id::text || ':' || NEW.campaign_id::text, 0));
  IF EXISTS (SELECT 1 FROM pf_extension.cnts_campaign_event e
              WHERE e.tenant_id = NEW.tenant_id AND e.campaign_id = NEW.campaign_id AND e.action = 'published') THEN
    RAISE EXCEPTION 'pf_extension: đợt điều tra % đã phát hành — không sửa nội dung', NEW.campaign_id
      USING ERRCODE = 'check_violation', CONSTRAINT = 'cnts_campaign_not_draft';
  END IF;
  IF pf_extension.fn_cnts_campaign_busy(NEW.tenant_id, NEW.campaign_id) THEN
    RAISE EXCEPTION 'pf_extension: đợt điều tra % đang có thao tác dở dang', NEW.campaign_id
      USING ERRCODE = 'check_violation', CONSTRAINT = 'cnts_campaign_busy';
  END IF;
  RETURN NEW;
END $$;

-- Máy trạng thái: như 20261024091058, thêm chặn sự kiện khi còn ý định thao tác (bước 3 bỏ ý định của mình TRƯỚC khi ghi
-- sự kiện trong cùng giao dịch, nên chỉ đường ghi khác — không qua ý định — bị chặn).
CREATE OR REPLACE FUNCTION pf_extension.fn_cnts_campaign_transition() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  last_state text;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('cnts_campaign:' || NEW.tenant_id::text || ':' || NEW.campaign_id::text, 0));
  IF NOT EXISTS (SELECT 1 FROM pf_extension.cnts_campaign c WHERE c.tenant_id = NEW.tenant_id AND c.campaign_id = NEW.campaign_id) THEN
    RAISE EXCEPTION 'pf_extension: không có đợt điều tra % trong tenant', NEW.campaign_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'cnts_campaign_event_campaign_fkey';
  END IF;
  IF pf_extension.fn_cnts_campaign_busy(NEW.tenant_id, NEW.campaign_id) THEN
    RAISE EXCEPTION 'pf_extension: đợt điều tra % đang có thao tác dở dang', NEW.campaign_id
      USING ERRCODE = 'check_violation', CONSTRAINT = 'cnts_campaign_busy';
  END IF;
  SELECT e.action INTO last_state FROM pf_extension.cnts_campaign_event e
   WHERE e.tenant_id = NEW.tenant_id AND e.campaign_id = NEW.campaign_id AND e.action IN ('published', 'closed', 'reopened')
   ORDER BY e.seq DESC LIMIT 1;
  IF (NEW.action = 'published' AND last_state IS NOT NULL)
     OR (NEW.action IN ('extended', 'closed', 'reminded') AND last_state IS DISTINCT FROM 'published' AND last_state IS DISTINCT FROM 'reopened')
     OR (NEW.action = 'reopened' AND last_state IS DISTINCT FROM 'closed') THEN
    RAISE EXCEPTION 'pf_extension: đợt điều tra % không chuyển được % từ trạng thái %', NEW.campaign_id, NEW.action, coalesce(last_state, 'draft')
      USING ERRCODE = 'check_violation', CONSTRAINT = 'cnts_campaign_event_state';
  END IF;
  RETURN NEW;
END $$;
