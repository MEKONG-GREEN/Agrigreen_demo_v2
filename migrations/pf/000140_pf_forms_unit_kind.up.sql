-- pf_forms: `cap/forms` tổng quát đơn vị lá (W11.05, BRD KN 10/2026 — D4 X4, D3 §2.5; Q-045, Q-047, Q-081).
--
-- Sổ có dữ liệu không ADD COLUMN (ADR-0012) → thêm SỔ PHỤ và sự kiện mới, không đổi dòng cũ:
--   - period_spec     — đặc tả kỳ native, một dòng/kỳ, ghi CÙNG GIAO DỊCH phát hành (kiểm recorded_at = now()):
--                       loại đơn vị lá unit_kind (commune = xã | facility = cơ sở GM | household = hộ/điểm |
--                       department = phòng ban), đơn vị cha để lọc phạm vi (cơ sở/hộ → mã xã), giới hạn số lần gửi,
--                       luồng rà soát (chỉ 'single' — chừa chỗ cho nhiều bước HCTH), khóa kỳ theo lịch kỳ hệ thống
--                       (W11.03: period_kind, period_key, from/to, period_rule — Period.Key được LƯU tại đây).
--                       Kỳ cũ không có dòng → mặc định: xã, không giới hạn, 'single'. Đơn vị = người (person) và
--                       nhiều bước KHÔNG có trong CHECK: thêm bằng migration sau khi HCTH chọn phương án (a) ở D3 §2.5.
--   - held_submission — bài nộp đến SAU khi kỳ đã chốt với đơn vị (bản offline — CNTS III.3.0 dòng 13): giữ lại ở
--                       trạng thái "Chờ mở kỳ", không vào submission, không đổi ảnh chụp. Mở lại kỳ rồi nộp lại.
--   - period_event    — thêm action 'cancelled' (hủy kỳ, KHTC BR-DL-18): lý do bắt buộc; native chỉ hủy được kỳ đang
--                       mở (published/reopened), là trạng thái cuối (không chốt/mở lại/nộp sau hủy). Mirror ghi được
--                       'cancelled' (KN thu hồi đợt), bên dùng vẫn giữ máy trạng thái của mình.
--   - Giới hạn số lần gửi (trg_submission_limit, sau trg_chain): phiên bản thứ n > max_submissions → AGF03.
--
-- Trạng thái hiển thị mới (Không nộp, Đã chốt – chưa điều chỉnh, Chờ mở kỳ) là PHÉP CHIẾU ở app/domain — không cột.

SET LOCAL lock_timeout = '5s';

-- ---------------------------------------------------------------------------
-- 1. Sổ phụ đặc tả kỳ
-- ---------------------------------------------------------------------------
CREATE TABLE pf_forms.period_spec (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'spec:<period_id>'
  seq               bigint      NOT NULL,
  period_id         uuid        NOT NULL,
  profile           text        NOT NULL,                 -- chép từ kỳ (trg_bind_spec)
  unit_kind         text        NOT NULL CHECK (unit_kind IN ('commune', 'facility', 'household', 'department')),
  unit_parents      text[]      NOT NULL CHECK (cardinality(unit_parents) <= 5000), -- song song với period.units ('{}' = không có)
  max_submissions   integer     NULL CHECK (max_submissions IS NULL OR max_submissions BETWEEN 1 AND 20),
  review_flow       text        NOT NULL CHECK (review_flow IN ('single')),
  period_kind       text        NULL CHECK (period_kind IS NULL OR period_kind IN
                                  ('day', 'week', 'ksgm_week', 'month', 'quarter', 'season', 'year', 'adhoc')),
  period_key        text        NULL CHECK (period_key IS NULL OR period_key ~ '^[0-9A-Za-z-]{1,20}$'),
  period_from       date        NULL,
  period_to         date        NULL,
  period_rule       text        NULL CHECK (period_rule IS NULL OR char_length(period_rule) BETWEEN 1 AND 200),
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
  CONSTRAINT period_spec_one CHECK (stream_key = 'spec:' || period_id::text AND seq = 1),
  CONSTRAINT period_spec_range CHECK ((period_from IS NULL) = (period_to IS NULL) AND (period_to IS NULL OR period_to >= period_from)),
  CONSTRAINT period_spec_key_kind CHECK (period_key IS NULL OR period_kind IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX period_spec_period ON pf_forms.period_spec (tenant_id, period_id);
CREATE INDEX period_spec_key    ON pf_forms.period_spec (tenant_id, profile, period_kind, period_key);

-- Đặc tả gắn đúng một kỳ cùng tenant, ghi cùng giao dịch phát hành (không đổi giới hạn/loại đơn vị của kỳ đã chạy);
-- đơn vị cha song song với danh sách đơn vị: bắt buộc với cơ sở/hộ, rỗng với xã/phòng ban.
CREATE FUNCTION pf_forms.fn_bind_spec() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  p record;
BEGIN
  SELECT x.profile, x.units, x.recorded_at INTO p FROM pf_forms.period x WHERE x.tenant_id = NEW.tenant_id AND x.id = NEW.period_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'pf_forms: không có kỳ % trong tenant', NEW.period_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'period_spec_period_fkey';
  END IF;
  IF p.recorded_at <> now() THEN
    RAISE EXCEPTION 'pf_forms: đặc tả kỳ % phải ghi cùng giao dịch phát hành', NEW.period_id USING ERRCODE = 'AGF01';
  END IF;
  IF NEW.unit_kind IN ('facility', 'household') THEN
    IF cardinality(NEW.unit_parents) <> cardinality(p.units) OR EXISTS (SELECT 1 FROM unnest(NEW.unit_parents) u WHERE u !~ '^[A-Za-z0-9_.-]{1,40}$') THEN
      RAISE EXCEPTION 'pf_forms: đơn vị % cần mã xã cha cho từng đơn vị', NEW.unit_kind USING ERRCODE = 'check_violation',
        CONSTRAINT = 'period_spec_parents';
    END IF;
  ELSIF cardinality(NEW.unit_parents) <> 0 THEN
    RAISE EXCEPTION 'pf_forms: đơn vị % không có đơn vị cha', NEW.unit_kind USING ERRCODE = 'check_violation', CONSTRAINT = 'period_spec_parents';
  END IF;
  NEW.profile := p.profile;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_bind_spec BEFORE INSERT ON pf_forms.period_spec
  FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_bind_spec();
SELECT shared.apply_ledger('pf_forms.period_spec', 'pf_forms.period_spec');

-- ---------------------------------------------------------------------------
-- 2. Hủy kỳ: sự kiện 'cancelled'
-- ---------------------------------------------------------------------------
ALTER TABLE pf_forms.period_event DROP CONSTRAINT period_event_action_check;
ALTER TABLE pf_forms.period_event ADD CONSTRAINT period_event_action_check
  CHECK (action IN ('published', 'closed', 'reopened', 'cancelled')) NOT VALID;
ALTER TABLE pf_forms.period_event VALIDATE CONSTRAINT period_event_action_check;
ALTER TABLE pf_forms.period_event ADD CONSTRAINT period_event_cancel_reason CHECK (action <> 'cancelled' OR reason IS NOT NULL) NOT VALID;
ALTER TABLE pf_forms.period_event VALIDATE CONSTRAINT period_event_cancel_reason;

-- Máy trạng thái native: thêm published|reopened → cancelled; cancelled là trạng thái cuối. Mirror giữ nguyên.
CREATE OR REPLACE FUNCTION pf_forms.fn_transition() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_prev text;
BEGIN
  SELECT e.action INTO v_prev FROM pf_forms.period_event e
   WHERE e.tenant_id = NEW.tenant_id AND e.stream_key = NEW.stream_key AND e.seq = NEW.seq - 1;
  IF NEW.action = 'published' AND v_prev IS NULL AND NEW.seq = 1 THEN
    RETURN NEW;
  END IF;
  IF NEW.origin = 'mirror' THEN
    IF EXISTS (SELECT 1 FROM pf_forms.period_event x WHERE x.tenant_id = NEW.tenant_id AND x.stream_key = NEW.stream_key
                AND x.origin <> 'mirror') THEN
      RAISE EXCEPTION 'pf_forms: kỳ % là kỳ native', NEW.period_ref USING ERRCODE = 'AGF01';
    END IF;
    RETURN NEW;
  END IF;
  IF (NEW.action = 'closed' AND v_prev IN ('published', 'reopened')) OR (NEW.action = 'reopened' AND v_prev = 'closed')
     OR (NEW.action = 'cancelled' AND v_prev IN ('published', 'reopened')) THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'pf_forms: kỳ % không chuyển được % → %', NEW.period_ref, coalesce(v_prev, '∅'), NEW.action USING ERRCODE = 'AGF01';
END $$;

-- Bài nộp: thêm kỳ đã hủy vào điều kiện từ chối.
CREATE OR REPLACE FUNCTION pf_forms.fn_bind_period() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  p record;
  e record;
BEGIN
  SELECT x.profile, x.template_id, x.units INTO p FROM pf_forms.period x WHERE x.tenant_id = NEW.tenant_id AND x.id = NEW.period_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'pf_forms: không có kỳ % trong tenant', NEW.period_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'submission_period_fkey';
  END IF;
  IF NOT (NEW.unit_code = ANY (p.units)) THEN
    RAISE EXCEPTION 'pf_forms: đơn vị % không thuộc kỳ %', NEW.unit_code, NEW.period_id USING ERRCODE = 'AGF02';
  END IF;
  SELECT v.action, v.units INTO e FROM pf_forms.period_event v
   WHERE v.tenant_id = NEW.tenant_id AND v.stream_key = 'period:' || NEW.period_id::text ORDER BY v.seq DESC LIMIT 1;
  IF NOT FOUND OR e.action IN ('closed', 'cancelled')
     OR (e.action = 'reopened' AND cardinality(e.units) > 0 AND NOT (NEW.unit_code = ANY (e.units))) THEN
    RAISE EXCEPTION 'pf_forms: kỳ % không mở cho đơn vị %', NEW.period_id, NEW.unit_code USING ERRCODE = 'AGF02';
  END IF;
  NEW.profile     := p.profile;
  NEW.template_id := p.template_id;
  RETURN NEW;
END $$;

-- Lối ghi song song: thêm 'cancelled' (lý do bắt buộc, không ảnh chụp).
CREATE OR REPLACE FUNCTION pf_forms.mirror(p_event_id uuid, p_snapshot_id uuid, p_profile text, p_period_ref uuid, p_label text,
                                           p_action text, p_mode text, p_units text[], p_reason text, p_content jsonb, p_actor uuid,
                                           p_event_time timestamptz, p_idem text, p_rule_version text)
RETURNS TABLE (o_event_id uuid, o_seq bigint, o_snapshot_id uuid, o_snapshot_no bigint, o_content_hash bytea, o_replayed boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_tenant uuid := shared.current_tenant();
  e pf_forms.period_event%ROWTYPE;
  s pf_forms.close_snapshot%ROWTYPE;
BEGIN
  IF v_tenant IS NULL THEN
    RAISE EXCEPTION 'pf_forms.mirror: chưa đặt app.tenant_id' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF p_action NOT IN ('closed', 'reopened', 'cancelled') THEN
    RAISE EXCEPTION 'pf_forms.mirror: action % không hợp lệ', p_action USING ERRCODE = 'invalid_parameter_value';
  END IF;
  IF p_action = 'closed' AND p_content IS NULL THEN
    RAISE EXCEPTION 'pf_forms.mirror: chốt kỳ cần nội dung ảnh chụp' USING ERRCODE = 'invalid_parameter_value';
  END IF;
  SELECT * INTO e FROM pf_forms.period_event x WHERE x.tenant_id = v_tenant AND x.idem_key = p_idem;
  IF FOUND THEN
    SELECT * INTO s FROM pf_forms.close_snapshot c WHERE c.tenant_id = v_tenant AND c.period_event_id = e.id;
    RETURN QUERY SELECT e.id, e.seq, s.id, s.seq, s.content_hash, true;
    RETURN;
  END IF;
  INSERT INTO pf_forms.period_event (id, tenant_id, stream_key, period_ref, profile, origin, action, mode, units, reason, label,
                                     event_time, actor_ref, idem_key, rule_version)
  VALUES (p_event_id, v_tenant, 'period:' || p_period_ref::text, p_period_ref, p_profile, 'mirror', p_action, p_mode,
          coalesce(p_units, '{}'), p_reason, p_label, p_event_time, p_actor, p_idem, p_rule_version)
  RETURNING * INTO e;
  IF p_action = 'closed' THEN
    INSERT INTO pf_forms.close_snapshot (id, tenant_id, stream_key, period_ref, profile, period_event_id, content, content_hash,
                                         event_time, actor_ref, idem_key, rule_version)
    VALUES (p_snapshot_id, v_tenant, 'snapshot:' || p_period_ref::text, p_period_ref, p_profile, e.id, p_content, '\x'::bytea,
            p_event_time, p_actor, 'snapshot:' || e.id::text, p_rule_version)
    RETURNING * INTO s;
  END IF;
  RETURN QUERY SELECT e.id, e.seq, s.id, s.seq, s.content_hash, false;
END $$;

-- ---------------------------------------------------------------------------
-- 3. Giới hạn số lần gửi (KHTC FN-TI-02.03: tối đa 3, gửi lần 3 tự khóa — Q-081). Tên xếp SAU trg_chain: seq đã gán
--    và đầu chuỗi đã khóa nên seq = số phiên bản thật.
-- ---------------------------------------------------------------------------
CREATE FUNCTION pf_forms.fn_submission_limit() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_max integer;
BEGIN
  SELECT s.max_submissions INTO v_max FROM pf_forms.period_spec s WHERE s.tenant_id = NEW.tenant_id AND s.period_id = NEW.period_id;
  IF v_max IS NOT NULL AND NEW.seq > v_max THEN
    RAISE EXCEPTION 'pf_forms: đơn vị % đã gửi đủ % lần cho kỳ %', NEW.unit_code, v_max, NEW.period_id USING ERRCODE = 'AGF03';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_submission_limit BEFORE INSERT ON pf_forms.submission
  FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_submission_limit();

-- ---------------------------------------------------------------------------
-- 4. Bài đến sau chốt ("Chờ mở kỳ")
-- ---------------------------------------------------------------------------
CREATE TABLE pf_forms.held_submission (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'held:<period_id>:<unit_code>'
  seq               bigint      NOT NULL,
  period_id         uuid        NOT NULL,
  unit_code         text        NOT NULL CHECK (unit_code ~ '^[A-Za-z0-9_.-]{1,40}$'),
  profile           text        NOT NULL,                 -- chép từ kỳ (trg_bind_held)
  template_id       uuid        NOT NULL,                 -- chép từ kỳ
  "values"          jsonb       NOT NULL CHECK (jsonb_typeof("values") = 'object'),
  submitter_ref     uuid        NOT NULL,
  captured_at       timestamptz NOT NULL,                 -- lúc ghi trên thiết bị (offline)
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
  CHECK (actor_ref = submitter_ref),
  CONSTRAINT held_submission_stream CHECK (stream_key = 'held:' || period_id::text || ':' || unit_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX held_submission_period ON pf_forms.held_submission (tenant_id, period_id, unit_code, seq);

-- Chỉ giữ bài khi kỳ KHÔNG nhận bài của đơn vị vì đã chốt (đã chốt, hoặc mở lại một phần không gồm đơn vị); kỳ đang
-- nhận bài → nộp bình thường; kỳ đã hủy → từ chối.
CREATE FUNCTION pf_forms.fn_bind_held() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  p record;
  e record;
BEGIN
  SELECT x.profile, x.template_id, x.units INTO p FROM pf_forms.period x WHERE x.tenant_id = NEW.tenant_id AND x.id = NEW.period_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'pf_forms: không có kỳ % trong tenant', NEW.period_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'held_submission_period_fkey';
  END IF;
  IF NOT (NEW.unit_code = ANY (p.units)) THEN
    RAISE EXCEPTION 'pf_forms: đơn vị % không thuộc kỳ %', NEW.unit_code, NEW.period_id USING ERRCODE = 'AGF02';
  END IF;
  SELECT v.action, v.units INTO e FROM pf_forms.period_event v
   WHERE v.tenant_id = NEW.tenant_id AND v.stream_key = 'period:' || NEW.period_id::text ORDER BY v.seq DESC LIMIT 1;
  IF NOT FOUND OR NOT (e.action = 'closed'
                       OR (e.action = 'reopened' AND cardinality(e.units) > 0 AND NOT (NEW.unit_code = ANY (e.units)))) THEN
    RAISE EXCEPTION 'pf_forms: kỳ % không ở trạng thái chốt với đơn vị %', NEW.period_id, NEW.unit_code USING ERRCODE = 'AGF02';
  END IF;
  NEW.profile     := p.profile;
  NEW.template_id := p.template_id;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_bind_held BEFORE INSERT ON pf_forms.held_submission
  FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_bind_held();
SELECT shared.apply_ledger('pf_forms.held_submission', 'pf_forms.held_submission');

-- ---------------------------------------------------------------------------
-- 5. Quyền (khớp pf 000122): r_forms chỉ SELECT, INSERT.
-- ---------------------------------------------------------------------------
REVOKE ALL ON pf_forms.period_spec, pf_forms.held_submission FROM PUBLIC;
REVOKE ALL ON FUNCTION pf_forms.fn_bind_spec(), pf_forms.fn_submission_limit(), pf_forms.fn_bind_held() FROM PUBLIC;
GRANT SELECT, INSERT ON pf_forms.period_spec, pf_forms.held_submission TO r_forms;
