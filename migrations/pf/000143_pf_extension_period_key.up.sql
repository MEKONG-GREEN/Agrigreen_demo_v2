-- pf_extension: loại kỳ ngày / quý / tuần KSGM ở "Phát hành biểu" + khóa kỳ đã lưu (W11.05b, tiếp W11.05 — BRD KN
-- 10/2026; QĐ-6, Q-045). Chỉ đụng schema pf_extension.
--
--   1. report_batch.period_type: CHECK cũ (week, month, season, year, adhoc — 000068) thay bằng CHECK mới nhận thêm
--      'day', 'quarter', 'ksgm_week' (đặt tên như domain.Period* của reportcycle, cùng dạng danh từ với giá trị cũ).
--      Theo mẫu khóa nhẹ: thêm NOT VALID → VALIDATE (SHARE UPDATE EXCLUSIVE) → bỏ CHECK cũ. Dòng cũ đều thỏa CHECK mới.
--   2. report_period_key — SỔ PHỤ (append-only, shared.apply_ledger) khóa kỳ theo lịch kỳ hệ thống W11.03, một dòng
--      một kỳ, ghi CÙNG GIAO DỊCH phát hành đợt: period_key (domain.Span.Key — "2026-11-05" ngày, "2026-W45" tuần,
--      "2026-11-K2" tuần KSGM, "2026-11", "2026-Q4", "2026"), week_year (năm của số tuần — chỉ kỳ tuần), loại kỳ và
--      quy tắc chia kỳ CHÉP từ đợt. Không ADD COLUMN vào report_period (bảng chiếu) / sổ (ADR-0012): khóa kỳ là sự kiện
--      bất biến lúc phát hành, không đổi khi quy tắc/tham số KSGM đổi về sau. Vụ/đột xuất (và năm của quy tắc v1)
--      không có khóa → không có dòng. Kỳ phát hành trước migration này không có dòng → bên đọc tính lại từ period_rule
--      của đợt (forms_mirror.periodKey).

SET LOCAL lock_timeout = '5s';

-- ---------------------------------------------------------------------------
-- 1. Mở loại kỳ
-- ---------------------------------------------------------------------------
ALTER TABLE pf_extension.report_batch
  ADD CONSTRAINT report_batch_period_type_v2
  CHECK (period_type IN ('day', 'week', 'ksgm_week', 'month', 'quarter', 'season', 'year', 'adhoc')) NOT VALID;
ALTER TABLE pf_extension.report_batch VALIDATE CONSTRAINT report_batch_period_type_v2;
ALTER TABLE pf_extension.report_batch DROP CONSTRAINT report_batch_period_type_check;

-- ---------------------------------------------------------------------------
-- 2. Sổ phụ khóa kỳ
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.report_period_key (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'period:<period_id>'
  seq               bigint      NOT NULL,
  period_id         uuid        NOT NULL,
  batch_id          uuid        NOT NULL,
  period_type       text        NOT NULL CHECK (period_type IN ('day', 'week', 'ksgm_week', 'month', 'quarter', 'year')),
  period_key        text        NOT NULL CHECK (period_key ~ '^[0-9]{4}(-[0-9A-Z]{2,3}(-[0-9]{2}|-K[1-4])?)?$'),
  week_year         integer     NULL CHECK (week_year IS NULL OR week_year BETWEEN 2000 AND 2100),
  period_rule       text        NOT NULL CHECK (char_length(period_rule) BETWEEN 1 AND 200),
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
  -- Một dòng một kỳ: luồng riêng của kỳ, chỉ seq 1 (UNIQUE không đặt được trên sổ phân vùng — ADR-0012).
  CONSTRAINT report_period_key_one CHECK (stream_key = 'period:' || period_id::text AND seq = 1),
  -- Năm của số tuần chỉ có (và bắt buộc) ở kỳ tuần.
  CONSTRAINT report_period_key_week CHECK ((week_year IS NOT NULL) = (period_type = 'week'))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX report_period_key_period ON pf_extension.report_period_key (tenant_id, period_id);
CREATE INDEX report_period_key_key    ON pf_extension.report_period_key (tenant_id, period_type, period_key);

-- Khóa gắn đúng một kỳ cùng tenant của đợt đang phát hành: kỳ thuộc batch_id, loại kỳ và quy tắc trùng đợt, ghi CÙNG
-- GIAO DỊCH sự kiện 'issued' của đợt (không gắn khóa cho kỳ đã chạy về sau).
CREATE FUNCTION pf_extension.fn_bind_period_key() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  b record;
BEGIN
  SELECT x.period_type, x.period_rule INTO b
    FROM pf_extension.report_period p
    JOIN pf_extension.report_batch x ON x.tenant_id = p.tenant_id AND x.id = p.batch_id
   WHERE p.tenant_id = NEW.tenant_id AND p.id = NEW.period_id AND p.batch_id = NEW.batch_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'pf_extension: không có kỳ % của đợt % trong tenant', NEW.period_id, NEW.batch_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'report_period_key_period_fkey';
  END IF;
  IF b.period_type <> NEW.period_type OR b.period_rule <> NEW.period_rule THEN
    RAISE EXCEPTION 'pf_extension: loại kỳ/quy tắc của khóa kỳ % khác đợt (%, %)', NEW.period_id, b.period_type, b.period_rule
      USING ERRCODE = 'check_violation', CONSTRAINT = 'report_period_key_batch';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pf_extension.report_batch_event e
                  WHERE e.tenant_id = NEW.tenant_id AND e.stream_key = 'batch:' || NEW.batch_id::text
                    AND e.action = 'issued' AND e.recorded_at = now()) THEN
    RAISE EXCEPTION 'pf_extension: khóa kỳ % phải ghi cùng giao dịch phát hành đợt', NEW.period_id
      USING ERRCODE = 'check_violation', CONSTRAINT = 'report_period_key_issue_tx';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_bind_period_key BEFORE INSERT ON pf_extension.report_period_key
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_bind_period_key();

SELECT shared.apply_ledger('pf_extension.report_period_key', 'pf_extension.report_period_key');

GRANT SELECT, INSERT ON pf_extension.report_period_key TO r_extension;
