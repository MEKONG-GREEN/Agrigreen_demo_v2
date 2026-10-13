-- pf_extension: Kỳ KHTC (W11.62 — BRD KHTC v2.2 FN-TI-02.03, Mục II.4, II.5; D4 KH2). Chỉ đụng schema pf_extension.
--
-- Phiếu của các phòng là MỘT kỳ native cap/forms (profile kn_khtc, đơn vị lá = phòng ban, tối đa 3 lần gửi — W11.05):
-- nộp, yêu cầu bổ sung/xác nhận, chốt kèm ảnh chụp, hủy ở pf_forms. Ba sổ dưới đây chỉ giữ phần riêng của KHTC:
--
--   1. khtc_period — SỔ (append-only, shared.apply_ledger), MỘT dòng một kỳ KHTC: loại báo cáo, năm, số tuần/tháng/quý,
--      phạm vi, khóa chống mở trùng, tên kỳ và khoảng thời gian do hệ thống sinh (Mục II.4), ngày chốt, hạn nộp, hạn xuất
--      lúc mở, số lần gửi tối đa, phòng tham gia, kỳ native cap/forms (forms_period_ref — không khóa ngoại liên schema).
--      Không mở trùng kỳ cùng loại + khóa (tuần/tháng/quý/năm) khi kỳ cũ chưa hủy (trigger, khóa tư vấn theo khóa kỳ).
--   2. khtc_period_event — SỔ sự kiện của kỳ: sửa hạn nộp/hạn xuất, đóng kỳ (thường hoặc ngoại lệ có lý do — BR-DL-15),
--      hủy kỳ (lý do bắt buộc — BR-DL-18). Đã đóng / Đã hủy là trạng thái cuối (trigger). Trạng thái Đã mở, Đang thu thập,
--      Trễ hạn, Đủ dữ liệu là PHÉP CHIẾU (tiến độ phiếu ở pf_forms + trạng thái nguồn) — không ghi sổ.
--   3. khtc_period_source — SỔ tham chiếu kỳ nguồn của module các phòng, gắn CÙNG GIAO DỊCH mở kỳ: bộ ba (calendar_id,
--      calendar_version, native_key) theo lịch CỦA NGUỒN (QĐ D8) do bộ quy đổi của gói PFPUB trả — Kỳ KHTC không tự đổi
--      lịch, không lưu số liệu nguồn.

SET LOCAL lock_timeout = '5s';

-- ---------------------------------------------------------------------------
-- 1. Kỳ KHTC
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.khtc_period (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'khtc_period:<id>'
  seq               bigint      NOT NULL,
  report_type       text        NOT NULL CHECK (report_type IN ('week', 'month', 'quarter', 'year', 'program', 'adhoc')),
  plan_year         integer     NOT NULL CHECK (plan_year BETWEEN 2000 AND 2100),
  period_no         integer     NULL CHECK (period_no IS NULL OR period_no BETWEEN 1 AND 53),
  scope             text        NULL CHECK (scope IS NULL OR scope IN ('quarter', 'half', 'nine_months')),
  -- Khóa chống mở trùng (Mục II.4): "2026-W41", "2026-09", "2026-Q3", "2026-H1", "2026-9M", "2026"; NULL với Mẫu 5 và
  -- báo cáo phát sinh (không thuộc quy tắc trùng).
  period_key        text        NULL CHECK (period_key IS NULL OR period_key ~ '^[0-9]{4}(-(W[0-9]{2}|[0-9]{2}|Q[1-4]|H1|9M))?$'),
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
  period_from       date        NOT NULL,
  period_to         date        NOT NULL,
  cutoff_date       date        NOT NULL,
  due_at            timestamptz NOT NULL,
  export_due_at     timestamptz NOT NULL,
  max_submissions   integer     NOT NULL CHECK (max_submissions BETWEEN 0 AND 20), -- 0 = không giới hạn (Q-081: 3)
  departments       text[]      NOT NULL CHECK (cardinality(departments) BETWEEN 1 AND 50),
  forms_period_ref  uuid        NOT NULL,                 -- kỳ native pf_forms.period (profile kn_khtc)
  template_code     text        NOT NULL CHECK (template_code ~ '^[A-Z0-9][A-Z0-9._-]{0,63}$'),
  period_rule       text        NOT NULL CHECK (char_length(period_rule) BETWEEN 1 AND 200), -- thẻ quy tắc chia kỳ (W11.03)
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
  -- Một dòng một kỳ (UNIQUE không đặt được trên sổ phân vùng — ADR-0012).
  CONSTRAINT khtc_period_one CHECK (stream_key = 'khtc_period:' || id::text AND seq = 1),
  CONSTRAINT khtc_period_range CHECK (period_to >= period_from),
  CONSTRAINT khtc_period_export CHECK (export_due_at >= due_at),
  -- Ngày chốt không trước ngày kết thúc khoảng thời gian, trừ báo cáo các CT (Mục II.4).
  CONSTRAINT khtc_period_cutoff CHECK (report_type = 'program' OR cutoff_date >= period_to),
  CONSTRAINT khtc_period_key_type CHECK ((period_key IS NULL) = (report_type IN ('program', 'adhoc')))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_period_id    ON pf_extension.khtc_period (tenant_id, id);
CREATE INDEX khtc_period_key   ON pf_extension.khtc_period (tenant_id, report_type, period_key);
CREATE INDEX khtc_period_year  ON pf_extension.khtc_period (tenant_id, plan_year, id DESC);
CREATE INDEX khtc_period_forms ON pf_extension.khtc_period (tenant_id, forms_period_ref);

-- ---------------------------------------------------------------------------
-- 2. Sự kiện kỳ KHTC
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.khtc_period_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'khtc_period_event:<period_id>'
  seq               bigint      NOT NULL,
  period_id         uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('deadline_changed', 'closed', 'cancelled')),
  forced            boolean     NOT NULL DEFAULT false,   -- đóng ngoại lệ khi còn thiếu dữ liệu (BR-DL-15)
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 10 AND 500),
  due_at            timestamptz NULL,                     -- deadline_changed: hạn nộp mới
  export_due_at     timestamptz NULL,                     -- deadline_changed: hạn xuất mới
  period_state      text        NULL CHECK (period_state IS NULL OR period_state IN ('opened', 'collecting', 'late', 'complete')),
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
  CONSTRAINT khtc_period_event_stream CHECK (stream_key = 'khtc_period_event:' || period_id::text),
  CONSTRAINT khtc_period_event_deadline CHECK (action <> 'deadline_changed'
    OR (due_at IS NOT NULL AND export_due_at IS NOT NULL AND export_due_at >= due_at)),
  CONSTRAINT khtc_period_event_cancel CHECK (action <> 'cancelled' OR reason IS NOT NULL),
  CONSTRAINT khtc_period_event_forced CHECK (NOT forced OR (action = 'closed' AND reason IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_period_event_period ON pf_extension.khtc_period_event (tenant_id, period_id, seq);

-- Không mở trùng: cùng loại + khóa kỳ khi kỳ cũ chưa hủy (Đã hủy không tính — Mục II.4, BR-DL-18).
CREATE FUNCTION pf_extension.fn_khtc_period_unique() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF NEW.period_key IS NULL THEN
    RETURN NEW;
  END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended('khtc_period:' || NEW.tenant_id::text || ':' || NEW.report_type || ':' || NEW.period_key, 0));
  IF EXISTS (SELECT 1 FROM pf_extension.khtc_period p
              WHERE p.tenant_id = NEW.tenant_id AND p.report_type = NEW.report_type AND p.period_key = NEW.period_key
                AND p.id <> NEW.id
                AND NOT EXISTS (SELECT 1 FROM pf_extension.khtc_period_event e
                                 WHERE e.tenant_id = p.tenant_id AND e.period_id = p.id AND e.action = 'cancelled')) THEN
    RAISE EXCEPTION 'pf_extension: kỳ KHTC % % đã mở', NEW.report_type, NEW.period_key
      USING ERRCODE = 'unique_violation', CONSTRAINT = 'khtc_period_duplicate';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_khtc_period_unique BEFORE INSERT ON pf_extension.khtc_period
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_period_unique();

-- Máy trạng thái kỳ (Mục II.5.1): kỳ có thật trong tenant; sau Đã đóng / Đã hủy không ghi sự kiện nào nữa. Khóa tư vấn
-- theo kỳ để hai thao tác đóng/hủy đồng thời không cùng qua.
CREATE FUNCTION pf_extension.fn_khtc_period_transition() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('khtc_period_event:' || NEW.tenant_id::text || ':' || NEW.period_id::text, 0));
  IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_period p WHERE p.tenant_id = NEW.tenant_id AND p.id = NEW.period_id) THEN
    RAISE EXCEPTION 'pf_extension: không có kỳ KHTC % trong tenant', NEW.period_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'khtc_period_event_period_fkey';
  END IF;
  IF EXISTS (SELECT 1 FROM pf_extension.khtc_period_event e
              WHERE e.tenant_id = NEW.tenant_id AND e.period_id = NEW.period_id AND e.action IN ('closed', 'cancelled')) THEN
    RAISE EXCEPTION 'pf_extension: kỳ KHTC % đã đóng hoặc đã hủy', NEW.period_id
      USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_period_event_terminal';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_khtc_period_transition BEFORE INSERT ON pf_extension.khtc_period_event
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_period_transition();

-- ---------------------------------------------------------------------------
-- 3. Kỳ nguồn của kỳ KHTC
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.khtc_period_source (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'khtc_period_source:<period_id>'
  seq               bigint      NOT NULL,
  period_id         uuid        NOT NULL,
  department        text        NOT NULL CHECK (department ~ '^[a-z][a-z0-9_]{0,39}$'),
  source            text        NOT NULL CHECK (source ~ '^[a-z][a-z0-9_.-]{0,79}$'),
  calendar_id       text        NOT NULL CHECK (char_length(calendar_id) BETWEEN 1 AND 80),
  calendar_version  text        NOT NULL CHECK (char_length(calendar_version) BETWEEN 1 AND 80),
  native_key        text        NOT NULL CHECK (char_length(native_key) BETWEEN 1 AND 40),
  label             text        NULL CHECK (label IS NULL OR char_length(label) <= 200),
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
  CONSTRAINT khtc_period_source_stream CHECK (stream_key = 'khtc_period_source:' || period_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_period_source_period ON pf_extension.khtc_period_source (tenant_id, period_id, seq);

-- Kỳ nguồn gắn CÙNG GIAO DỊCH mở kỳ (dòng khtc_period ghi trong giao dịch hiện tại).
CREATE FUNCTION pf_extension.fn_khtc_period_source_bind() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_period p
                  WHERE p.tenant_id = NEW.tenant_id AND p.id = NEW.period_id AND p.recorded_at = now()) THEN
    RAISE EXCEPTION 'pf_extension: kỳ nguồn của kỳ KHTC % phải ghi cùng giao dịch mở kỳ', NEW.period_id
      USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_period_source_open_tx';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_khtc_period_source_bind BEFORE INSERT ON pf_extension.khtc_period_source
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_period_source_bind();

SELECT shared.apply_ledger('pf_extension.khtc_period', 'pf_extension.khtc_period');
SELECT shared.apply_ledger('pf_extension.khtc_period_event', 'pf_extension.khtc_period_event');
SELECT shared.apply_ledger('pf_extension.khtc_period_source', 'pf_extension.khtc_period_source');

GRANT SELECT, INSERT ON pf_extension.khtc_period, pf_extension.khtc_period_event, pf_extension.khtc_period_source TO r_extension;
