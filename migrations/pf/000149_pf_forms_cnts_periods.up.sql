-- pf_forms: biểu kỳ Cấp Xã CNTS trên kỳ native `cap/forms` (W11.42, BRD CNTS v3.7 UR-XA-06…09, 13 — D4 CN2; Q-045).
--
-- Sổ có dữ liệu không ADD COLUMN (ADR-0012) → thêm SỔ PHỤ và bảng nguồn đồng bộ, không đổi dòng cũ:
--   - submission_meta — một dòng/phiên bản bài nộp, ghi CÙNG GIAO DỊCH với bài nộp (kiểm recorded_at = now()):
--                       base_version = phiên bản người nhập đã thấy khi mở bài (KHÓA LẠC QUAN — CNTS III.3.0 "Hai người
--                       cùng sửa một báo cáo: người lưu sau được báo … không ghi đè"): DB đòi base_version = seq − 1
--                       của phiên bản vừa ghi, sai → AGF04; nil_report = "Báo không phát sinh" (UR-XA-13; BM-01 cơ sở
--                       "Không hoạt động", BM-17 "Không phát sinh"); note = ghi chú chung (H08); explanation = giải
--                       trình / lý do trễ (H09); captured_at = lúc ghi trên thiết bị (bản offline); late = gửi sau hạn
--                       nộp của kỳ (DB tính từ captured_at hoặc event_time so với period.due_at — "Nộp trễ").
--   - sync_feed       — nguồn con trỏ đồng bộ ngoại tuyến (docs/dev/sync-contract.md): mỗi thay đổi của kỳ native
--                       (phát hành, chốt, mở lại, hủy → unit_code '*'; nộp, rà soát, bài giữ → mã đơn vị) thêm một dòng
--                       với feed_seq tăng dần (sequence). Trigger AFTER INSERT trên sổ ghi — không phụ thuộc mã app.
--                       Không phải sổ băm (không row_hash): chỉ ghi thêm (chặn UPDATE/DELETE/TRUNCATE), RLS theo tenant.
--
-- Trạng thái hiển thị mới (Trễ hạn khi chưa nộp và quá hạn) là PHÉP CHIẾU ở app/domain — không cột.

SET LOCAL lock_timeout = '5s';

-- ---------------------------------------------------------------------------
-- 1. Sổ phụ thông tin phiên bản bài nộp
-- ---------------------------------------------------------------------------
CREATE TABLE pf_forms.submission_meta (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'meta:<submission_id>'
  seq               bigint      NOT NULL,
  submission_id     uuid        NOT NULL,
  period_id         uuid        NOT NULL,                 -- chép từ bài nộp (trg_bind_meta)
  unit_code         text        NOT NULL,                 -- chép từ bài nộp
  profile           text        NOT NULL,                 -- chép từ bài nộp
  base_version      bigint      NOT NULL CHECK (base_version >= 0),
  nil_report        boolean     NOT NULL,
  note              text        NULL CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 1000),
  explanation       text        NULL CHECK (explanation IS NULL OR char_length(explanation) BETWEEN 1 AND 1000),
  captured_at       timestamptz NULL,
  late              boolean     NOT NULL,                 -- DB tính (trg_bind_meta)
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
  CONSTRAINT submission_meta_one CHECK (stream_key = 'meta:' || submission_id::text AND seq = 1)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX submission_meta_submission ON pf_forms.submission_meta (tenant_id, submission_id);
CREATE INDEX submission_meta_period     ON pf_forms.submission_meta (tenant_id, period_id, unit_code);

-- Thông tin gắn đúng một phiên bản bài nộp cùng tenant, ghi cùng giao dịch; khóa lạc quan: base_version = seq − 1.
CREATE FUNCTION pf_forms.fn_bind_meta() RETURNS trigger
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
  SELECT p.due_at INTO v_due FROM pf_forms.period p WHERE p.tenant_id = NEW.tenant_id AND p.id = s.period_id;
  NEW.period_id := s.period_id;
  NEW.unit_code := s.unit_code;
  NEW.profile   := s.profile;
  NEW.late      := coalesce(NEW.captured_at, s.event_time) > v_due;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_bind_meta BEFORE INSERT ON pf_forms.submission_meta
  FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_bind_meta();
SELECT shared.apply_ledger('pf_forms.submission_meta', 'pf_forms.submission_meta');

-- ---------------------------------------------------------------------------
-- 2. Nguồn con trỏ đồng bộ ngoại tuyến
-- ---------------------------------------------------------------------------
CREATE SEQUENCE pf_forms.sync_feed_seq;

CREATE TABLE pf_forms.sync_feed (
  feed_seq    bigint      NOT NULL DEFAULT nextval('pf_forms.sync_feed_seq'),
  tenant_id   uuid        NOT NULL,
  profile     text        NOT NULL,
  period_id   uuid        NOT NULL,
  unit_code   text        NOT NULL CHECK (unit_code = '*' OR unit_code ~ '^[A-Za-z0-9_.-]{1,40}$'), -- '*' = mọi đơn vị của kỳ
  recorded_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (feed_seq)
);
CREATE INDEX sync_feed_profile ON pf_forms.sync_feed (tenant_id, profile, feed_seq);
SELECT shared.apply_tenant_rls('pf_forms.sync_feed');
CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON pf_forms.sync_feed
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON pf_forms.sync_feed
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- Ghi nguồn đồng bộ sau mỗi thay đổi của kỳ native. TG_ARGV[0] = 'period' (dòng period: id, cả kỳ), 'event' (sự kiện
-- native: period_ref, cả kỳ; mirror bỏ qua — không phải kỳ native) hoặc 'unit' (period_id, unit_code).
CREATE FUNCTION pf_forms.fn_feed() RETURNS trigger
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
  ELSE
    INSERT INTO pf_forms.sync_feed (tenant_id, profile, period_id, unit_code)
    SELECT NEW.tenant_id, p.profile, NEW.period_id, NEW.unit_code FROM pf_forms.period p
     WHERE p.tenant_id = NEW.tenant_id AND p.id = NEW.period_id;
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER trg_feed AFTER INSERT ON pf_forms.period          FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_feed('period');
CREATE TRIGGER trg_feed AFTER INSERT ON pf_forms.period_event    FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_feed('event');
CREATE TRIGGER trg_feed AFTER INSERT ON pf_forms.submission      FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_feed('unit');
CREATE TRIGGER trg_feed AFTER INSERT ON pf_forms.review          FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_feed('unit');
CREATE TRIGGER trg_feed AFTER INSERT ON pf_forms.held_submission FOR EACH ROW EXECUTE FUNCTION pf_forms.fn_feed('unit');

-- ---------------------------------------------------------------------------
-- 3. Quyền (khớp pf 000122): r_forms chỉ SELECT, INSERT; dùng sequence của nguồn đồng bộ.
--    Lối ghi song song (pf_forms.mirror, SECURITY DEFINER) chạy trigger nguồn dưới quyền chủ hàm — mirror bỏ qua.
-- ---------------------------------------------------------------------------
REVOKE ALL ON pf_forms.submission_meta, pf_forms.sync_feed FROM PUBLIC;
REVOKE ALL ON SEQUENCE pf_forms.sync_feed_seq FROM PUBLIC;
REVOKE ALL ON FUNCTION pf_forms.fn_bind_meta(), pf_forms.fn_feed() FROM PUBLIC;
GRANT SELECT, INSERT ON pf_forms.submission_meta, pf_forms.sync_feed TO r_forms;
GRANT USAGE ON SEQUENCE pf_forms.sync_feed_seq TO r_forms;
