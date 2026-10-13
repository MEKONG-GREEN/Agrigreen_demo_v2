-- pf_extension: ảnh chụp "Cập nhật số liệu" của Kỳ KHTC (W11.64 — BRD KHTC v2.2 BR-DL-02…04, BR-DL-09, BR-DL-17; D4 KH4;
-- ADR-0053 §8). Chỉ đụng schema pf_extension.
--
-- KHTC KHÔNG đọc trực tiếp số của các phòng khi soạn: cán bộ KHTC phụ trách kỳ bấm "Cập nhật số liệu" → hệ thống chụp
-- BẤT BIẾN kết quả port KHTCSources (họ closed_figures/v1 + Báo giá, đã quy đổi sang tuần KHTC) vào bốn sổ chỉ ghi thêm:
--
--   1. khtc_snapshot — một dòng mỗi lần chụp của một kỳ KHTC (seq = số thứ tự ảnh chụp của kỳ): người bấm (actor_ref),
--      lúc bấm (event_time), lịch đích, phiên bản bảng chuyển đổi, phiên bản Kế hoạch năm dùng, băm gộp số/trạng thái.
--   2. khtc_snapshot_row — một dòng mỗi kỳ nguồn × biểu × phòng quy vào một tuần KHTC của kỳ (hoặc dòng mức phòng khi
--      Missing/Unavailable): nguồn gốc (source_module, calendar_id, calendar_version, native_key, closed_at, snapshot_no),
--      figures_hash, status_hash (băm riêng trạng thái xác nhận — D2), ô số của kỳ nguồn.
--   3. khtc_snapshot_value — số của từng khóa nguồn module của Kế hoạch năm (W11.61 source_key): lũy kế của nguồn (D18),
--      gốc, trong kỳ, lũy kế năm, số phát sinh, giá min/max toàn tỉnh (D22), phần chưa xác nhận (D2), cờ Tạm tính.
--   4. khtc_snapshot_change — lịch sử thay đổi so với ảnh chụp trước của cùng kỳ (giá trị cũ → mới); người và thời điểm
--      là của ảnh chụp.
--
-- "Nguồn đã thay đổi" là PHÉP CHIẾU: so figures_hash hiện tại của port với khtc_snapshot_row (không so status_hash) —
-- chỉ cảnh báo, không ghi đè ảnh chụp.

SET LOCAL lock_timeout = '5s';

-- ---------------------------------------------------------------------------
-- 1. Ảnh chụp
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.khtc_snapshot (
  id                      uuid        NOT NULL,         -- UUIDv7 (R-11)
  tenant_id               uuid        NOT NULL,
  stream_key              text        NOT NULL,         -- 'khtc_snapshot:<period_id>'
  seq                     bigint      NOT NULL,         -- số thứ tự ảnh chụp của kỳ (1, 2, …)
  period_id               uuid        NOT NULL,         -- pf_extension.khtc_period
  period_from             date        NOT NULL,
  period_to               date        NOT NULL,
  window_from             date        NOT NULL,         -- tuần đầu đọc (gốc, lũy kế năm)
  target_calendar_id      text        NOT NULL CHECK (char_length(target_calendar_id) BETWEEN 1 AND 80),
  target_calendar_version integer     NOT NULL CHECK (target_calendar_version BETWEEN 1 AND 9999),
  target_rule             text        NOT NULL CHECK (char_length(target_rule) BETWEEN 1 AND 200),
  crosswalk_version       text        NOT NULL CHECK (char_length(crosswalk_version) BETWEEN 1 AND 120),
  plan_version_id         uuid        NULL,             -- phiên bản Kế hoạch năm đã dùng (NULL = chưa có kế hoạch)
  closed                  boolean     NOT NULL,         -- mọi phòng nguồn đã chốt cho mọi tuần của kỳ (D17)
  figures_hash            text        NOT NULL CHECK (figures_hash ~ '^[0-9a-f]{64}$'),
  status_hash             text        NOT NULL CHECK (status_hash ~ '^[0-9a-f]{64}$'),
  row_count               integer     NOT NULL CHECK (row_count >= 0),
  value_count             integer     NOT NULL CHECK (value_count >= 0),
  change_count            integer     NOT NULL CHECK (change_count >= 0),
  event_time              timestamptz NOT NULL,
  recorded_at             timestamptz NOT NULL DEFAULT now(),
  supersedes_id           uuid        NULL,
  correction_reason       text        NULL,
  actor_ref               uuid        NOT NULL,         -- cán bộ KHTC phụ trách kỳ đã bấm
  device_id               text        NULL,
  idem_key                text        NOT NULL,
  rule_version            text        NOT NULL,
  prev_hash               bytea       NOT NULL,
  row_hash                bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT khtc_snapshot_stream CHECK (stream_key = 'khtc_snapshot:' || period_id::text),
  CONSTRAINT khtc_snapshot_range CHECK (period_to >= period_from AND window_from <= period_from)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_snapshot_period ON pf_extension.khtc_snapshot (tenant_id, period_id, seq DESC);
CREATE INDEX khtc_snapshot_id     ON pf_extension.khtc_snapshot (tenant_id, id);

-- ---------------------------------------------------------------------------
-- 2. Dòng nguồn của ảnh chụp
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.khtc_snapshot_row (
  id                  uuid        NOT NULL,
  tenant_id           uuid        NOT NULL,
  stream_key          text        NOT NULL,             -- 'khtc_snapshot_row:<snapshot_id>'
  seq                 bigint      NOT NULL,
  snapshot_id         uuid        NOT NULL,
  period_id           uuid        NOT NULL,
  target_key          text        NOT NULL CHECK (char_length(target_key) BETWEEN 1 AND 40), -- tuần KHTC (2026-W45)
  department          text        NOT NULL CHECK (department ~ '^[a-z][a-z0-9_]{0,39}$'),
  source_module       text        NOT NULL CHECK (char_length(source_module) <= 120),
  form_code           text        NULL CHECK (form_code IS NULL OR char_length(form_code) BETWEEN 1 AND 64),
  calendar_id         text        NULL CHECK (calendar_id IS NULL OR char_length(calendar_id) BETWEEN 1 AND 80),
  calendar_version    integer     NULL,                 -- D8; NULL = kỳ không theo lịch tuần (BM-05) hoặc dòng mức phòng
  native_key          text        NULL CHECK (native_key IS NULL OR char_length(native_key) BETWEEN 1 AND 40),
  source_period_type  text        NULL CHECK (source_period_type IS NULL OR char_length(source_period_type) <= 20),
  source_from         date        NULL,
  source_to           date        NULL,
  closed              boolean     NOT NULL,
  converted           boolean     NOT NULL DEFAULT false,
  missing             boolean     NOT NULL DEFAULT false,
  unavailable         boolean     NOT NULL DEFAULT false,
  fallback            boolean     NOT NULL DEFAULT false, -- bảng giá ngày duyệt gần nhất trước tuần (D22)
  source_closed_at    timestamptz NULL,                 -- mốc dữ liệu của kỳ nguồn (lần chốt gần nhất)
  source_snapshot_no  bigint      NULL,
  figures_hash        text        NULL CHECK (figures_hash IS NULL OR figures_hash ~ '^[0-9a-f]{64}$'),
  status_hash         text        NULL CHECK (status_hash IS NULL OR status_hash ~ '^[0-9a-f]{64}$'),
  confirmation_status text        NULL CHECK (confirmation_status IS NULL OR confirmation_status IN ('confirmed', 'unconfirmed')),
  cells               jsonb       NULL CHECK (cells IS NULL OR jsonb_typeof(cells) = 'object'),
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
  CONSTRAINT khtc_snapshot_row_stream CHECK (stream_key = 'khtc_snapshot_row:' || snapshot_id::text),
  CONSTRAINT khtc_snapshot_row_source CHECK ((form_code IS NULL) = (native_key IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_snapshot_row_snapshot ON pf_extension.khtc_snapshot_row (tenant_id, snapshot_id, seq);

-- ---------------------------------------------------------------------------
-- 3. Số của khóa nguồn module (hạng mục Kế hoạch năm)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.khtc_snapshot_value (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,               -- 'khtc_snapshot_value:<snapshot_id>'
  seq               bigint      NOT NULL,
  snapshot_id       uuid        NOT NULL,
  period_id         uuid        NOT NULL,
  source_key        text        NOT NULL CHECK (source_key ~ '^[a-z][a-z0-9_.:-]{1,99}$'),
  entry_refs        uuid[]      NOT NULL,               -- hạng mục Kế hoạch năm dùng khóa (W11.61 entry_ref)
  mode              text        NOT NULL CHECK (mode IN ('cum', 'season', 'sum', 'price', 'invalid')),
  cumulative        numeric     NULL,
  base              numeric     NULL,
  base_ref          text        NULL CHECK (base_ref IS NULL OR char_length(base_ref) <= 60),
  increment         numeric     NULL,
  year_to_date      numeric     NULL,
  sum_value         numeric     NULL,
  price_min         numeric     NULL,
  price_max         numeric     NULL,
  price_day         date        NULL,
  unconfirmed       numeric     NULL,
  closed            boolean     NOT NULL,
  no_source         boolean     NOT NULL DEFAULT false,
  frozen            boolean     NOT NULL DEFAULT false,
  source_changed    boolean     NOT NULL DEFAULT false,
  fallback          boolean     NOT NULL DEFAULT false,
  unavailable       boolean     NOT NULL DEFAULT false,
  detail            jsonb       NOT NULL CHECK (jsonb_typeof(detail) = 'object'), -- kỳ nguồn, hai ngày giá, số từng tuần
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
  CONSTRAINT khtc_snapshot_value_stream CHECK (stream_key = 'khtc_snapshot_value:' || snapshot_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_snapshot_value_snapshot ON pf_extension.khtc_snapshot_value (tenant_id, snapshot_id, seq);

-- ---------------------------------------------------------------------------
-- 4. Lịch sử thay đổi so với ảnh chụp trước
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.khtc_snapshot_change (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,               -- 'khtc_snapshot_change:<snapshot_id>'
  seq               bigint      NOT NULL,
  snapshot_id       uuid        NOT NULL,
  prev_snapshot_id  uuid        NOT NULL,
  period_id         uuid        NOT NULL,
  source_key        text        NOT NULL CHECK (source_key ~ '^[a-z][a-z0-9_.:-]{1,99}$'),
  entry_refs        uuid[]      NOT NULL,
  field             text        NOT NULL CHECK (field IN ('cumulative', 'increment', 'sum', 'price_min', 'price_max')),
  old_value         numeric     NULL,
  new_value         numeric     NULL,
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
  CONSTRAINT khtc_snapshot_change_stream CHECK (stream_key = 'khtc_snapshot_change:' || snapshot_id::text),
  CONSTRAINT khtc_snapshot_change_values CHECK (old_value IS NOT NULL OR new_value IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_snapshot_change_period ON pf_extension.khtc_snapshot_change (tenant_id, period_id, snapshot_id);

-- Ảnh chụp chỉ ghi khi kỳ có thật trong tenant và chưa đóng/hủy (kỳ đã đóng giữ số — Q7); khóa tư vấn theo kỳ để hai
-- lần bấm đồng thời không cùng tính lịch sử từ một ảnh chụp trước.
CREATE FUNCTION pf_extension.fn_khtc_snapshot_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:' || NEW.tenant_id::text || ':' || NEW.period_id::text, 0));
  IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_period p WHERE p.tenant_id = NEW.tenant_id AND p.id = NEW.period_id) THEN
    RAISE EXCEPTION 'pf_extension: không có kỳ KHTC % trong tenant', NEW.period_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'khtc_snapshot_period_fkey';
  END IF;
  IF EXISTS (SELECT 1 FROM pf_extension.khtc_period_event e
              WHERE e.tenant_id = NEW.tenant_id AND e.period_id = NEW.period_id AND e.action IN ('closed', 'cancelled')) THEN
    RAISE EXCEPTION 'pf_extension: kỳ KHTC % đã đóng hoặc đã hủy', NEW.period_id
      USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_snapshot_terminal';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_khtc_snapshot_guard BEFORE INSERT ON pf_extension.khtc_snapshot
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_snapshot_guard();

-- Dòng, số, lịch sử ghi CÙNG GIAO DỊCH với ảnh chụp (ảnh chụp bất biến, không thêm dòng về sau).
CREATE FUNCTION pf_extension.fn_khtc_snapshot_child() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_snapshot s
                  WHERE s.tenant_id = NEW.tenant_id AND s.id = NEW.snapshot_id AND s.period_id = NEW.period_id
                    AND s.recorded_at = now()) THEN
    RAISE EXCEPTION 'pf_extension: dòng của ảnh chụp % phải ghi cùng giao dịch ảnh chụp', NEW.snapshot_id
      USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_snapshot_child_tx';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_khtc_snapshot_row_child BEFORE INSERT ON pf_extension.khtc_snapshot_row
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_snapshot_child();
CREATE TRIGGER trg_khtc_snapshot_value_child BEFORE INSERT ON pf_extension.khtc_snapshot_value
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_snapshot_child();
CREATE TRIGGER trg_khtc_snapshot_change_child BEFORE INSERT ON pf_extension.khtc_snapshot_change
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_snapshot_child();

SELECT shared.apply_ledger('pf_extension.khtc_snapshot', 'pf_extension.khtc_snapshot');
SELECT shared.apply_ledger('pf_extension.khtc_snapshot_row', 'pf_extension.khtc_snapshot_row');
SELECT shared.apply_ledger('pf_extension.khtc_snapshot_value', 'pf_extension.khtc_snapshot_value');
SELECT shared.apply_ledger('pf_extension.khtc_snapshot_change', 'pf_extension.khtc_snapshot_change');

GRANT SELECT, INSERT ON pf_extension.khtc_snapshot, pf_extension.khtc_snapshot_row, pf_extension.khtc_snapshot_value,
  pf_extension.khtc_snapshot_change TO r_extension;
