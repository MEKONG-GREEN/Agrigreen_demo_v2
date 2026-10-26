-- pf_extension: Chiến dịch điều tra mở rộng của Phòng Chăn nuôi – Thủy sản (W11.50 — BRD CNTS v3.7 FN-21, UR-TINH-26,
-- UR-XA-14; D4 CN10). Chỉ đụng schema pf_extension.
--
-- Mỗi đợt điều tra phát hành thành MỘT kỳ native cap/forms profile kn_cnts loại kỳ `adhoc` (khóa "DT-<ngày đầu>-<6 hex>",
-- đơn vị lá = xã): nhập, gửi lại, yêu cầu điều chỉnh, chốt kèm ảnh chụp, "Không nộp" khi chốt — ở pf_forms. Kỳ `adhoc`
-- KHÔNG vào lũy kế báo cáo định kỳ (BM-02, BM-18 — extension/reportcycle) và không vào tổng quy đổi của KHTC
-- (extension/closedfig) — FN-21 BR-02, AC-04. Hai sổ dưới đây chỉ giữ phần riêng của đợt:
--
--   1. cnts_campaign — SỔ (append-only, shared.apply_ledger) phiên bản nội dung đợt khi còn Nháp: tên, mục tiêu, lĩnh vực,
--      biểu điều tra (mã biểu kn_cnts — thiết kế ở FN-07), phạm vi (toàn tỉnh / khu vực / danh sách xã → danh sách xã đã
--      giải), thời gian thu thập, hạn nộp, người phụ trách, giờ nhắc trước hạn. Một dòng một phiên bản (seq tăng theo
--      đợt); đã phát hành thì không ghi phiên bản mới (trigger).
--   2. cnts_campaign_event — SỔ sự kiện của đợt: phát hành (kỳ native, khóa kỳ, phiên bản biểu, hạn), gia hạn (lý do bắt
--      buộc), đóng đợt, mở lại (lý do + thời hạn mới; Lãnh đạo phòng — kiểm ở ứng dụng), nhắc. Máy trạng thái Nháp →
--      Đang thu thập → Đã đóng (→ Đang thu thập khi mở lại) kiểm ở trigger. Tiến độ, trạng thái từng xã là PHÉP CHIẾU của
--      kỳ native — không ghi sổ.

SET LOCAL lock_timeout = '5s';

-- ---------------------------------------------------------------------------
-- 1. Phiên bản nội dung đợt
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.cnts_campaign (
  id                  uuid        NOT NULL,                 -- UUIDv7 của phiên bản (R-11)
  tenant_id           uuid        NOT NULL,
  stream_key          text        NOT NULL,                 -- 'cnts_campaign:<campaign_id>'
  seq                 bigint      NOT NULL,                 -- = số phiên bản nội dung
  campaign_id         uuid        NOT NULL,
  org_ref             uuid        NOT NULL,                 -- Nút Cấp Tỉnh tạo đợt (phạm vi xem/ghi)
  province_code       text        NULL CHECK (province_code IS NULL OR province_code ~ '^[0-9A-Za-z_-]{1,20}$'),
  name                text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
  goal                text        NULL CHECK (goal IS NULL OR char_length(goal) <= 2000),
  field               text        NOT NULL CHECK (field IN ('chan_nuoi', 'thu_y', 'thuy_san', 'khac')),
  template_code       text        NOT NULL CHECK (template_code ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$'),
  scope_mode          text        NOT NULL CHECK (scope_mode IN ('province', 'regions', 'communes')),
  regions             text[]      NOT NULL DEFAULT '{}' CHECK (cardinality(regions) <= 100),
  units               text[]      NOT NULL CHECK (cardinality(units) BETWEEN 1 AND 2000),
  period_from         date        NOT NULL,
  period_to           date        NOT NULL,
  due_at              timestamptz NOT NULL,
  owner_ref           uuid        NULL,                     -- người phụ trách đợt (tham chiếu tài khoản)
  remind_before_hours integer     NOT NULL DEFAULT 0 CHECK (remind_before_hours BETWEEN 0 AND 168),
  event_time          timestamptz NOT NULL,
  recorded_at         timestamptz NOT NULL DEFAULT now(),
  supersedes_id       uuid        NULL,
  correction_reason   text        NULL,
  actor_ref           uuid        NOT NULL,
  device_id           text        NULL,
  idem_key            text        NOT NULL,
  rule_version        text        NOT NULL,
  prev_hash           bytea       NOT NULL,
  row_hash            bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT cnts_campaign_stream CHECK (stream_key = 'cnts_campaign:' || campaign_id::text),
  CONSTRAINT cnts_campaign_range CHECK (period_to >= period_from),
  CONSTRAINT cnts_campaign_regions CHECK (scope_mode = 'regions' OR cardinality(regions) = 0)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_campaign_id   ON pf_extension.cnts_campaign (tenant_id, campaign_id, seq DESC);
CREATE INDEX cnts_campaign_org  ON pf_extension.cnts_campaign (tenant_id, org_ref, campaign_id DESC);
CREATE INDEX cnts_campaign_idem ON pf_extension.cnts_campaign (tenant_id, idem_key);

-- ---------------------------------------------------------------------------
-- 2. Sự kiện của đợt
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.cnts_campaign_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'cnts_campaign_event:<campaign_id>'
  seq               bigint      NOT NULL,
  campaign_id       uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('published', 'extended', 'closed', 'reopened', 'reminded')),
  version           bigint      NULL,                     -- published: phiên bản nội dung đã phát hành
  forms_period_ref  uuid        NULL,                     -- published: kỳ native pf_forms.period (không khóa ngoại liên schema)
  period_key        text        NULL CHECK (period_key IS NULL OR period_key ~ '^DT-[0-9]{8}-[0-9a-f]{6}$'),
  template_version  bigint      NULL,                     -- published: phiên bản biểu cap/forms
  due_at            timestamptz NULL,                     -- published / extended / reopened: hạn nộp (mới)
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 10 AND 500),
  reminded          integer     NULL CHECK (reminded IS NULL OR reminded >= 0), -- reminded: số xã được nhắc
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT cnts_campaign_event_stream CHECK (stream_key = 'cnts_campaign_event:' || campaign_id::text),
  CONSTRAINT cnts_campaign_event_published CHECK (action <> 'published'
    OR (version IS NOT NULL AND forms_period_ref IS NOT NULL AND period_key IS NOT NULL AND template_version IS NOT NULL AND due_at IS NOT NULL)),
  CONSTRAINT cnts_campaign_event_due CHECK (action NOT IN ('extended', 'reopened') OR (due_at IS NOT NULL AND reason IS NOT NULL)),
  CONSTRAINT cnts_campaign_event_reminded CHECK ((action = 'reminded') = (reminded IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_campaign_event_campaign ON pf_extension.cnts_campaign_event (tenant_id, campaign_id, seq);
CREATE INDEX cnts_campaign_event_forms    ON pf_extension.cnts_campaign_event (tenant_id, forms_period_ref);

-- Phiên bản nội dung chỉ ghi khi đợt còn Nháp (chưa có sự kiện phát hành).
CREATE FUNCTION pf_extension.fn_cnts_campaign_draft() RETURNS trigger
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
  RETURN NEW;
END $$;
CREATE TRIGGER trg_cnts_campaign_draft BEFORE INSERT ON pf_extension.cnts_campaign
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_cnts_campaign_draft();

-- Máy trạng thái (FN-21 BR-01): Nháp → (published) Đang thu thập → (closed) Đã đóng → (reopened) Đang thu thập…; gia hạn
-- và nhắc chỉ khi Đang thu thập. Trạng thái = sự kiện chuyển trạng thái cuối (published/closed/reopened). Khóa tư vấn
-- cùng khóa của trigger phiên bản để phát hành và sửa nháp đồng thời không cùng qua.
CREATE FUNCTION pf_extension.fn_cnts_campaign_transition() RETURNS trigger
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
CREATE TRIGGER trg_cnts_campaign_transition BEFORE INSERT ON pf_extension.cnts_campaign_event
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_cnts_campaign_transition();

SELECT shared.apply_ledger('pf_extension.cnts_campaign', 'pf_extension.cnts_campaign');
SELECT shared.apply_ledger('pf_extension.cnts_campaign_event', 'pf_extension.cnts_campaign_event');

GRANT SELECT, INSERT ON pf_extension.cnts_campaign, pf_extension.cnts_campaign_event TO r_extension;
