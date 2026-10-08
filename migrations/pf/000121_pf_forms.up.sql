-- pf_forms: năng lực biểu thu thập theo kỳ dùng chung `cap/forms` (W6.2, kiến trúc v4.1 §26, ADR-0027).
-- Tách từ pf/reports (mẫu có phiên bản) + extension/reportcycle (phát hành, nộp, rà soát, chốt/mở lại kỳ).
--
-- Sáu sổ chỉ ghi thêm (mẫu 8.1, shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo tenant).
-- Không có bảng chiếu sửa được: trạng thái là phép chiếu của sổ (sự kiện cuối của kỳ, phiên bản cuối của bài nộp).
--   - template       — định nghĩa biểu theo bên dùng (profile) + mã; mỗi dòng là MỘT phiên bản (version = seq).
--                      fixture = biểu mẫu thử/seed (14 biểu thật chờ khách — Q-01).
--   - period         — kỳ đã phát hành: biểu (phiên bản cố định), nhãn, hạn nộp, đơn vị nhận (cấp lá), Nút chủ.
--   - period_event   — vòng đời kỳ: published | closed | reopened. origin = native (kỳ của pf/forms) hoặc mirror
--                      (ghi song song từ bên dùng có vòng đời riêng — KN extension/reportcycle, nguyên tắc 3).
--   - submission     — bài nộp của một đơn vị cho một kỳ; mỗi lần nộp là một phiên bản (version = seq).
--   - review         — rà soát DUY NHẤT của một phiên bản bài nộp: accepted | returned; người rà soát ≠ người nộp.
--   - close_snapshot — ẢNH CHỤP ĐÓNG BĂNG lúc chốt kỳ (§26): nội dung jsonb + băm SHA-256 do DB tính + rule_version.
--                      Mở lại kỳ là sự kiện mới; chốt lại sinh ảnh chụp mới (snapshot_no = seq), ảnh cũ không đổi.
--
-- Bất biến ở DB (không chỉ ở app/):
--   - Chuyển trạng thái kỳ native (trg_transition, chạy SAU trg_chain — lúc đó đầu chuỗi đã khóa nên đọc sự kiện
--     trước an toàn): published chỉ là sự kiện đầu; closed sau published/reopened; reopened sau closed. Kỳ mirror:
--     bên dùng giữ máy trạng thái (KN mở lại/chốt lại từng xã × biểu, kỳ có trước W6.2) — mọi closed/reopened được
--     ghi, không trộn với kỳ native. Sai → SQLSTATE AGF01.
--   - Bài nộp chỉ vào kỳ native đang mở, đơn vị thuộc kỳ, và thuộc phạm vi mở lại nếu kỳ mở lại một phần
--     (trg_bind_period chép profile, template_id từ kỳ). Sai → AGF02; không có kỳ → 23503.
--   - Rà soát: trg_bind_submission chép kỳ, đơn vị, người nộp từ bài nộp (không giả được); CHECK review_four_eyes.
--   - Ảnh chụp: trg_bind_close kiểm sự kiện chốt cùng kỳ, chép profile, TÍNH content_hash = sha256(content::text).
--
-- Không dữ liệu cá nhân (R-04): chỉ mã tham chiếu người, mã đơn vị hành chính, số liệu biểu (chuỗi thập phân, R-10).

CREATE SCHEMA pf_forms;

CREATE TABLE pf_forms.template (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'template:<profile>:<code>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  profile           text        NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,39}$'),
  code              text        NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9._-]{0,63}$'),
  title             text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
  fields            jsonb       NOT NULL CHECK (jsonb_typeof(fields) = 'array' AND jsonb_array_length(fields) BETWEEN 1 AND 500),
  fixture           boolean     NOT NULL,                 -- biểu mẫu thử/seed (Q-01: 14 biểu thật chưa có)
  event_time        timestamptz NOT NULL,                 -- R-13
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
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
  CONSTRAINT template_stream CHECK (stream_key = 'template:' || profile || ':' || code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX template_code ON pf_forms.template (tenant_id, profile, code, seq);
CREATE INDEX template_id   ON pf_forms.template (tenant_id, id);
SELECT shared.apply_ledger('pf_forms.template', 'pf_forms.template');

CREATE TABLE pf_forms.period (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'period:<id>'
  seq               bigint      NOT NULL,
  profile           text        NOT NULL,                 -- chép từ template (trg_bind_template)
  template_id       uuid        NOT NULL,                 -- phiên bản biểu cố định của kỳ
  template_code     text        NOT NULL,                 -- chép từ template
  template_version  bigint      NOT NULL,                 -- chép từ template
  label             text        NOT NULL CHECK (char_length(label) BETWEEN 1 AND 200),
  due_at            timestamptz NOT NULL,
  units             text[]      NOT NULL CHECK (cardinality(units) BETWEEN 1 AND 5000), -- đơn vị nhận (cấp lá)
  owner_ref         uuid        NULL,                     -- Nút của người phát hành (chốt/mở lại theo Nút này)
  publisher_level   text        NOT NULL CHECK (publisher_level ~ '^[A-Z][A-Z0-9_]{0,19}$'),
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
  CONSTRAINT period_one CHECK (stream_key = 'period:' || id::text AND seq = 1)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX period_id      ON pf_forms.period (tenant_id, id);
CREATE INDEX period_profile ON pf_forms.period (tenant_id, profile, id DESC);

-- Kỳ gắn đúng một phiên bản biểu cùng tenant: chép profile, mã, số phiên bản (không tin bên ghi).
CREATE FUNCTION pf_forms.fn_bind_template() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  r record;
BEGIN
  SELECT t.profile, t.code, t.seq INTO r FROM pf_forms.template t WHERE t.tenant_id = NEW.tenant_id AND t.id = NEW.template_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'pf_forms: không có phiên bản biểu % trong tenant', NEW.template_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'period_template_fkey';
  END IF;
  NEW.profile          := r.profile;
  NEW.template_code    := r.code;
  NEW.template_version := r.seq;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_bind_template BEFORE INSERT ON pf_forms.period
  FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_bind_template();
SELECT shared.apply_ledger('pf_forms.period', 'pf_forms.period');

CREATE TABLE pf_forms.period_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'period:<period_ref>'
  seq               bigint      NOT NULL,
  period_ref        uuid        NOT NULL,                 -- native: pf_forms.period.id; mirror: mã kỳ của bên dùng
  profile           text        NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,39}$'),
  origin            text        NOT NULL CHECK (origin IN ('native', 'mirror')),
  action            text        NOT NULL CHECK (action IN ('published', 'closed', 'reopened')),
  mode              text        NOT NULL CHECK (mode IN ('manual', 'auto')),
  units             text[]      NOT NULL DEFAULT '{}',    -- phạm vi: reopened = mở lại, mirror closed = chốt lại ('{}' = cả kỳ)
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 10 AND 500),
  label             text        NULL CHECK (label IS NULL OR char_length(label) BETWEEN 1 AND 200), -- mirror: nhãn kỳ
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
  CONSTRAINT period_event_stream CHECK (stream_key = 'period:' || period_ref::text),
  CONSTRAINT period_event_reopen_reason CHECK (action <> 'reopened' OR reason IS NOT NULL),
  CONSTRAINT period_event_mirror_no_publish CHECK (origin = 'native' OR action <> 'published')
) PARTITION BY RANGE (recorded_at);
CREATE INDEX period_event_period ON pf_forms.period_event (tenant_id, period_ref, seq);
CREATE INDEX period_event_idem   ON pf_forms.period_event (tenant_id, idem_key);

-- Máy trạng thái kỳ ở DB. Tên xếp SAU trg_chain: fn_chain đã khóa đầu chuỗi (FOR UPDATE) và gán NEW.seq, nên sự kiện
-- trước (seq - 1) đã commit và không thể chen.
CREATE FUNCTION pf_forms.fn_transition() RETURNS trigger
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
    -- Bên dùng giữ máy trạng thái của mình (KN: mở lại/chốt lại từng xã × biểu, kỳ có trước W6.2 bắt đầu giữa vòng
    -- đời): sổ ghi lại mọi chốt/mở lại; chỉ cấm trộn với kỳ native cùng mã.
    IF EXISTS (SELECT 1 FROM pf_forms.period_event x WHERE x.tenant_id = NEW.tenant_id AND x.stream_key = NEW.stream_key
                AND x.origin <> 'mirror') THEN
      RAISE EXCEPTION 'pf_forms: kỳ % là kỳ native', NEW.period_ref USING ERRCODE = 'AGF01';
    END IF;
    RETURN NEW;
  END IF;
  IF (NEW.action = 'closed' AND v_prev IN ('published', 'reopened')) OR (NEW.action = 'reopened' AND v_prev = 'closed') THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'pf_forms: kỳ % không chuyển được % → %', NEW.period_ref, coalesce(v_prev, '∅'), NEW.action USING ERRCODE = 'AGF01';
END $$;
SELECT shared.apply_ledger('pf_forms.period_event', 'pf_forms.period_event');
CREATE TRIGGER trg_transition BEFORE INSERT ON pf_forms.period_event
  FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_transition();

CREATE TABLE pf_forms.submission (
  id                uuid        NOT NULL,                 -- mã của PHIÊN BẢN bài nộp
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'sub:<period_id>:<unit_code>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  period_id         uuid        NOT NULL,
  unit_code         text        NOT NULL CHECK (unit_code ~ '^[A-Za-z0-9_.-]{1,40}$'),
  profile           text        NOT NULL,                 -- chép từ kỳ (trg_bind_period)
  template_id       uuid        NOT NULL,                 -- chép từ kỳ
  "values"          jsonb       NOT NULL CHECK (jsonb_typeof("values") = 'object'),
  submitter_ref     uuid        NOT NULL,
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
  CONSTRAINT submission_stream CHECK (stream_key = 'sub:' || period_id::text || ':' || unit_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX submission_period ON pf_forms.submission (tenant_id, period_id, unit_code, seq);
CREATE INDEX submission_id     ON pf_forms.submission (tenant_id, id);

-- Bài nộp chỉ vào kỳ native đang mở (sự kiện cuối published/reopened), đơn vị thuộc kỳ, và thuộc phạm vi mở lại.
CREATE FUNCTION pf_forms.fn_bind_period() RETURNS trigger
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
  IF NOT FOUND OR e.action = 'closed' OR (e.action = 'reopened' AND cardinality(e.units) > 0 AND NOT (NEW.unit_code = ANY (e.units))) THEN
    RAISE EXCEPTION 'pf_forms: kỳ % không mở cho đơn vị %', NEW.period_id, NEW.unit_code USING ERRCODE = 'AGF02';
  END IF;
  NEW.profile     := p.profile;
  NEW.template_id := p.template_id;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_bind_period BEFORE INSERT ON pf_forms.submission
  FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_bind_period();
SELECT shared.apply_ledger('pf_forms.submission', 'pf_forms.submission');

CREATE TABLE pf_forms.review (
  id                  uuid        NOT NULL,
  tenant_id           uuid        NOT NULL,
  stream_key          text        NOT NULL,               -- 'review:<submission_id>'
  seq                 bigint      NOT NULL,
  submission_id       uuid        NOT NULL,
  period_id           uuid        NOT NULL,               -- chép từ bài nộp (trg_bind_submission)
  unit_code           text        NOT NULL,               -- chép từ bài nộp
  submitter_ref       uuid        NOT NULL,               -- chép từ bài nộp
  reviewer_ref        uuid        NOT NULL,
  decision            text        NOT NULL CHECK (decision IN ('accepted', 'returned')),
  note                text        NULL CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 1000),
  workflow_request_id uuid        NULL,                   -- yêu cầu của cap/workflow (bốn mắt)
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
  CHECK (actor_ref = reviewer_ref),
  CONSTRAINT review_one_per_submission CHECK (stream_key = 'review:' || submission_id::text AND seq = 1),
  CONSTRAINT review_four_eyes CHECK (reviewer_ref <> submitter_ref),
  CONSTRAINT review_note_required CHECK (decision = 'accepted' OR char_length(btrim(coalesce(note, ''))) >= 1)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX review_submission ON pf_forms.review (tenant_id, submission_id);
CREATE INDEX review_period     ON pf_forms.review (tenant_id, period_id);

CREATE FUNCTION pf_forms.fn_bind_submission() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  s record;
BEGIN
  SELECT x.period_id, x.unit_code, x.submitter_ref INTO s FROM pf_forms.submission x WHERE x.tenant_id = NEW.tenant_id AND x.id = NEW.submission_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'pf_forms: không có bài nộp % trong tenant', NEW.submission_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'review_submission_fkey';
  END IF;
  NEW.period_id     := s.period_id;
  NEW.unit_code     := s.unit_code;
  NEW.submitter_ref := s.submitter_ref;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_bind_submission BEFORE INSERT ON pf_forms.review
  FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_bind_submission();
SELECT shared.apply_ledger('pf_forms.review', 'pf_forms.review');

CREATE TABLE pf_forms.close_snapshot (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'snapshot:<period_ref>'
  seq               bigint      NOT NULL,                 -- = số ảnh chụp của kỳ (chốt lại → ảnh mới)
  period_ref        uuid        NOT NULL,
  profile           text        NOT NULL,                 -- chép từ sự kiện chốt (trg_bind_close)
  period_event_id   uuid        NOT NULL,                 -- sự kiện closed sinh ảnh chụp
  content           jsonb       NOT NULL CHECK (jsonb_typeof(content) = 'object'),
  content_hash      bytea       NOT NULL,                 -- = sha256(content::text), DB tính (trg_bind_close)
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- 'snapshot:<period_event_id>' — một ảnh mỗi lần chốt
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT close_snapshot_stream CHECK (stream_key = 'snapshot:' || period_ref::text),
  CONSTRAINT close_snapshot_one_per_close CHECK (idem_key = 'snapshot:' || period_event_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX close_snapshot_period ON pf_forms.close_snapshot (tenant_id, period_ref, seq);
CREATE INDEX close_snapshot_id     ON pf_forms.close_snapshot (tenant_id, id);

CREATE FUNCTION pf_forms.fn_bind_close() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  e record;
BEGIN
  SELECT x.period_ref, x.profile, x.action INTO e FROM pf_forms.period_event x WHERE x.tenant_id = NEW.tenant_id AND x.id = NEW.period_event_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'pf_forms: không có sự kiện kỳ % trong tenant', NEW.period_event_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'close_snapshot_event_fkey';
  END IF;
  IF e.action <> 'closed' OR e.period_ref <> NEW.period_ref THEN
    RAISE EXCEPTION 'pf_forms: ảnh chụp chỉ gắn sự kiện chốt của cùng kỳ' USING ERRCODE = 'AGF01';
  END IF;
  NEW.profile      := e.profile;
  NEW.content_hash := sha256(convert_to(NEW.content::text, 'UTF8'));
  RETURN NEW;
END $$;
CREATE TRIGGER trg_bind_close BEFORE INSERT ON pf_forms.close_snapshot
  FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_bind_close();
SELECT shared.apply_ledger('pf_forms.close_snapshot', 'pf_forms.close_snapshot');

-- Lối ghi song song cho bên dùng có vòng đời kỳ riêng (KN extension/reportcycle — nguyên tắc 3): chạy TRONG GIAO
-- DỊCH CỦA BÊN DÙNG (vai r_extension chỉ có EXECUTE, pf 000122), ghi sự kiện origin = mirror và — với closed — ảnh chụp.
-- Idempotent theo idem_key của sự kiện (gọi lặp → o_replayed). Tenant = app.tenant_id; thiếu → 42501.
CREATE FUNCTION pf_forms.mirror(p_event_id uuid, p_snapshot_id uuid, p_profile text, p_period_ref uuid, p_label text,
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
  IF p_action NOT IN ('closed', 'reopened') THEN
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
