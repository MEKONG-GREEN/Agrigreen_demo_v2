-- pf_forms: sửa hạn nộp của kỳ native (W11.62 — KHTC "Sửa hạn nộp", BRD KHTC v2.2 FN-TI-02.03). Chỉ đụng schema pf_forms.
--
--   1. period_due — SỔ PHỤ (append-only, shared.apply_ledger) các lần đổi hạn nộp của một kỳ native: luồng 'due:<kỳ>',
--      hạn mới, lý do (tùy chọn). Hạn HIỆU LỰC của kỳ = dòng cuối (seq lớn nhất), không có dòng → pf_forms.period.due_at
--      (dòng kỳ không UPDATE — ADR-0012). Chỉ đổi khi kỳ chưa chốt và chưa hủy (sự kiện cuối published/reopened).
--   2. pf_forms.effective_due(): hạn hiệu lực. fn_bind_meta GỘP hai quy tắc: D8 (pf 20261013091503 — trễ theo GIỜ MÁY
--      CHỦ nhận bài, submission.event_time; captured_at của bản offline chỉ để kiểm toán) và W11.62 (so với hạn HIỆU LỰC
--      thay cho period.due_at gốc): late = event_time > effective_due(kỳ). Bài nhận sau hạn gốc nhưng trước hạn mới
--      (kể cả bản offline đồng bộ muộn) không bị đánh dấu trễ.
--   3. Nguồn đồng bộ: mỗi lần đổi hạn ghi sync_feed cả kỳ ('*') để thiết bị nhận hạn mới.

SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_forms.period_due (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'due:<period_id>'
  seq               bigint      NOT NULL,
  period_id         uuid        NOT NULL,
  profile           text        NOT NULL,                 -- chép từ kỳ (trigger)
  due_at            timestamptz NOT NULL,
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
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
  CONSTRAINT period_due_stream CHECK (stream_key = 'due:' || period_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX period_due_period ON pf_forms.period_due (tenant_id, period_id, seq DESC);

-- Kỳ native có thật trong tenant, đang nhận bài (sự kiện cuối published/reopened); profile chép từ kỳ.
CREATE FUNCTION pf_forms.fn_bind_due() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_profile text;
  v_last    text;
BEGIN
  SELECT p.profile INTO v_profile FROM pf_forms.period p WHERE p.tenant_id = NEW.tenant_id AND p.id = NEW.period_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'pf_forms: không có kỳ native % trong tenant', NEW.period_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'period_due_period_fkey';
  END IF;
  SELECT e.action INTO v_last FROM pf_forms.period_event e
   WHERE e.tenant_id = NEW.tenant_id AND e.stream_key = 'period:' || NEW.period_id::text
   ORDER BY e.seq DESC LIMIT 1;
  IF v_last IS NULL OR v_last NOT IN ('published', 'reopened') THEN
    RAISE EXCEPTION 'pf_forms: kỳ % đã chốt hoặc đã hủy — không đổi hạn nộp', NEW.period_id USING ERRCODE = 'AGF02';
  END IF;
  NEW.profile := v_profile;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_bind_due BEFORE INSERT ON pf_forms.period_due
  FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_bind_due();
SELECT shared.apply_ledger('pf_forms.period_due', 'pf_forms.period_due');

-- Hạn hiệu lực của kỳ (lần đổi cuối, hoặc hạn lúc phát hành).
CREATE FUNCTION pf_forms.effective_due(p_tenant uuid, p_period uuid, p_base timestamptz) RETURNS timestamptz
LANGUAGE sql STABLE
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT coalesce((SELECT d.due_at FROM pf_forms.period_due d
                    WHERE d.tenant_id = p_tenant AND d.period_id = p_period ORDER BY d.seq DESC LIMIT 1), p_base)
$$;

-- fn_bind_meta: thân của D8 (pf 20261013091503) + hạn hiệu lực (W11.62).
CREATE OR REPLACE FUNCTION pf_forms.fn_bind_meta() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  s record;
  v_due timestamptz;
BEGIN
  SELECT x.period_id, x.unit_code, x.profile, x.seq, x.event_time, x.recorded_at INTO s
    FROM pf_forms.submission x WHERE x.tenant_id = NEW.tenant_id AND x.id = NEW.submission_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'pf_forms: không có bài nộp % trong tenant', NEW.submission_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'submission_meta_submission_fkey';
  END IF;
  IF s.recorded_at <> now() THEN
    RAISE EXCEPTION 'pf_forms: thông tin bài nộp % phải ghi cùng giao dịch nộp', NEW.submission_id USING ERRCODE = 'AGF01';
  END IF;
  IF NEW.base_version <> s.seq - 1 THEN
    RAISE EXCEPTION 'pf_forms: bài nộp dựa trên phiên bản % nhưng đơn vị % đã có phiên bản %', NEW.base_version, s.unit_code, s.seq - 1
      USING ERRCODE = 'AGF04';
  END IF;
  SELECT pf_forms.effective_due(p.tenant_id, p.id, p.due_at) INTO v_due
    FROM pf_forms.period p WHERE p.tenant_id = NEW.tenant_id AND p.id = s.period_id;
  NEW.period_id := s.period_id;
  NEW.unit_code := s.unit_code;
  NEW.profile   := s.profile;
  -- D8: trễ hạn theo giờ MÁY CHỦ nhận bài (event_time của phiên bản), không theo captured_at (giờ thiết bị —
  -- có thể sai giờ máy hoặc bị chỉnh); captured_at vẫn lưu để kiểm toán. W11.62: so với hạn hiệu lực (sau "Sửa hạn nộp").
  NEW.late      := s.event_time > v_due;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION pf_forms.fn_feed() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF TG_ARGV[0] = 'period' THEN
    INSERT INTO pf_forms.sync_feed (tenant_id, profile, period_id, unit_code) VALUES (NEW.tenant_id, NEW.profile, NEW.id, '*');
  ELSIF TG_ARGV[0] = 'event' THEN
    IF NEW.origin = 'native' AND NEW.action <> 'published' THEN
      INSERT INTO pf_forms.sync_feed (tenant_id, profile, period_id, unit_code) VALUES (NEW.tenant_id, NEW.profile, NEW.period_ref, '*');
    END IF;
  ELSIF TG_ARGV[0] = 'due' THEN
    INSERT INTO pf_forms.sync_feed (tenant_id, profile, period_id, unit_code) VALUES (NEW.tenant_id, NEW.profile, NEW.period_id, '*');
  ELSE
    INSERT INTO pf_forms.sync_feed (tenant_id, profile, period_id, unit_code)
    SELECT NEW.tenant_id, p.profile, NEW.period_id, NEW.unit_code FROM pf_forms.period p
     WHERE p.tenant_id = NEW.tenant_id AND p.id = NEW.period_id;
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER trg_feed AFTER INSERT ON pf_forms.period_due FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_feed('due');

REVOKE ALL ON pf_forms.period_due FROM PUBLIC;
REVOKE ALL ON FUNCTION pf_forms.fn_bind_due(), pf_forms.effective_due(uuid, uuid, timestamptz) FROM PUBLIC;
GRANT SELECT, INSERT ON pf_forms.period_due TO r_forms;
GRANT EXECUTE ON FUNCTION pf_forms.effective_due(uuid, uuid, timestamptz) TO r_forms;
