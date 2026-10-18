-- pf_extension: Tổng hợp CNTS Cấp Tỉnh — BM-02 KSGM tuần/tháng, BM-18 Phụ lục 2 thủy sản, lũy kế (W11.44 — BRD CNTS v3.7
-- UR-TINH-14, UR-TINH-15, FN-16, Mục III.5 quy tắc 2, 3, 5, 6; D4 CN4). Chỉ đụng schema pf_extension.
--
-- Số TỰ CỘNG (tổng BM-01 theo cơ sở × tuần KSGM, tổng BM-17 theo xã × tuần CNTS, lũy kế quý/năm) KHÔNG lưu: tính khi đọc
-- từ kỳ native cap/forms (forms/port.FiguresReader) — tuần cũ đổi thì lũy kế các tuần sau tự tính lại (quy tắc 3). Bốn sổ
-- dưới đây chỉ giữ phần do người nhập và phần cần đóng băng (append-only, shared.apply_ledger):
--
--   1. cnts_bm02_adjustment — dòng điều chỉnh BM-02 (BR-01 FN-16): một cơ sở × một tuần KSGM, số cộng/trừ từng loài, lý do
--      (10–500 ký tự), số tự cộng LÚC điều chỉnh (base — BM-01 đổi sau đó → "cần xem lại"). Không ghi đè số tự cộng; muốn
--      bỏ thì thêm dòng ngược dấu. Tuần KSGM của BM-02 đã chốt → chặn (phải mở lại — UR-TINH-14).
--   2. cnts_aqua_entry — số nuôi trồng của Phụ lục 2 (UR-TINH-15) theo tỉnh × tuần CNTS × đối tượng: diện tích (ha hoặc
--      lồng), sản lượng (tấn hoặc 1.000 viên) TRONG TUẦN; dòng sau thay dòng trước cùng (tỉnh, tuần, đối tượng). Tuần
--      BM-18 đã chốt → chặn.
--   3. cnts_agg_close — chốt / mở lại bảng tổng hợp (BM-02 tuần KSGM, BM-18 tuần CNTS). Lần chốt ĐÓNG BĂNG số công bố
--      theo hợp đồng closed_figures/v1 (ADR-0053: rows, totals, băm) — chỉ chốt được khi MỌI kỳ nguồn của tuần đã chốt
--      (D17, app kiểm); lũy kế trong ô là lũy kế của nguồn (D18). View pf_extension_pub.v1_closed_figures (nhánh cnts_aggregate) (migration
--      kế tiếp) công bố lần chốt mới nhất cho KHTC.
--   4. cnts_agg_share — phiên bản tệp chia sẻ Chi cục Chăn nuôi và Thú y (BM-02 tuần/tháng, BM-18): nội dung bảng lúc
--      chia sẻ + băm; phiên bản = seq của chuỗi (tỉnh × biểu × loại kỳ × khóa). Tệp Excel dựng lại từ nội dung đã lưu.
--      Đây cũng là sổ "báo cáo đã gửi cơ quan ngoài" (Q-079 đề xuất của dev).

SET LOCAL lock_timeout = '5s';

-- ---------------------------------------------------------------------------
-- 1. Dòng điều chỉnh BM-02
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.cnts_bm02_adjustment (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'cnts_bm02_adj:<tỉnh>:<tuần KSGM>'
  seq               bigint      NOT NULL,
  province_code     text        NOT NULL CHECK (province_code ~ '^[0-9A-Za-z_-]{1,20}$'),
  week_key          text        NOT NULL CHECK (week_key ~ '^[0-9]{4}-(0[1-9]|1[0-2])-K[1-4]$'),
  facility_ref      uuid        NOT NULL,                 -- cơ sở giết mổ (đơn vị lá BM-01, danh mục CNTS W11.41)
  commune_code      text        NULL CHECK (commune_code IS NULL OR char_length(commune_code) BETWEEN 1 AND 20),
  deltas            jsonb       NOT NULL CHECK (jsonb_typeof(deltas) = 'object'),  -- {"heo": "-2", …} chuỗi thập phân
  base              jsonb       NOT NULL CHECK (jsonb_typeof(base) = 'object'),    -- số tự cộng lúc điều chỉnh
  reason            text        NOT NULL CHECK (char_length(reason) BETWEEN 10 AND 500),
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
  CONSTRAINT cnts_bm02_adjustment_stream CHECK (stream_key = 'cnts_bm02_adj:' || province_code || ':' || week_key)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_bm02_adjustment_week ON pf_extension.cnts_bm02_adjustment (tenant_id, province_code, week_key, seq);

-- ---------------------------------------------------------------------------
-- 2. Số nuôi trồng (Phụ lục 2)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.cnts_aqua_entry (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'cnts_aqua:<tỉnh>:<tuần CNTS>'
  seq               bigint      NOT NULL,
  province_code     text        NOT NULL CHECK (province_code ~ '^[0-9A-Za-z_-]{1,20}$'),
  week_key          text        NOT NULL CHECK (week_key ~ '^[0-9]{4}-W[0-9]{2}$'),
  week_from         date        NOT NULL,
  week_to           date        NOT NULL,
  object_code       text        NOT NULL CHECK (object_code ~ '^[a-z][a-z0-9_]{0,39}$'),
  area              numeric     NULL CHECK (area IS NULL OR area >= 0),
  output            numeric     NULL CHECK (output IS NULL OR output >= 0),
  note              text        NULL CHECK (note IS NULL OR char_length(note) <= 500),
  catalog_version   text        NOT NULL CHECK (char_length(catalog_version) BETWEEN 1 AND 80),
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
  CONSTRAINT cnts_aqua_entry_stream CHECK (stream_key = 'cnts_aqua:' || province_code || ':' || week_key),
  CONSTRAINT cnts_aqua_entry_range CHECK (week_to = week_from + 6)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_aqua_entry_week ON pf_extension.cnts_aqua_entry (tenant_id, province_code, week_to, object_code, seq);

-- ---------------------------------------------------------------------------
-- 3. Chốt / mở lại bảng tổng hợp (số công bố đóng băng)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.cnts_agg_close (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'cnts_agg:<tỉnh>:<biểu>:<loại kỳ>:<khóa>'
  seq               bigint      NOT NULL,
  agg_period_id     uuid        NOT NULL,                 -- mã ổn định của kỳ tổng hợp (domain.AggPeriodID)
  province_code     text        NOT NULL CHECK (province_code ~ '^[0-9A-Za-z_-]{1,20}$'),
  form_code         text        NOT NULL CHECK (form_code IN ('CNTS-BM02', 'CNTS-BM18')),
  def_version_id    uuid        NOT NULL,                 -- phiên bản định nghĩa bảng (domain.AggDefinitionID)
  period_type       text        NOT NULL CHECK (period_type IN ('ksgm_week', 'week')),
  period_key        text        NOT NULL CHECK (period_key ~ '^[0-9]{4}-(W[0-9]{2}|(0[1-9]|1[0-2])-K[1-4])$'),
  period_from       date        NOT NULL,
  period_to         date        NOT NULL,
  period_rule       text        NOT NULL CHECK (char_length(period_rule) BETWEEN 1 AND 200),
  period_label      text        NOT NULL CHECK (char_length(period_label) BETWEEN 1 AND 200),
  action            text        NOT NULL CHECK (action IN ('closed', 'reopened')),
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 10 AND 500),
  -- Số công bố (chỉ dòng closed): phần tử rows/totals theo hợp đồng closed_figures/v1 (không gồm trạng thái xác nhận).
  rows              jsonb       NULL CHECK (rows IS NULL OR jsonb_typeof(rows) = 'array'),
  totals            jsonb       NULL CHECK (totals IS NULL OR jsonb_typeof(totals) = 'array'),
  units_total       integer     NULL CHECK (units_total IS NULL OR units_total >= 0),
  units_with_data   integer     NULL CHECK (units_with_data IS NULL OR units_with_data >= 0),
  missing_units     text[]      NULL,
  provisional       boolean     NULL,
  figures_hash      text        NULL CHECK (figures_hash IS NULL OR figures_hash ~ '^[0-9a-f]{64}$'),
  sources           jsonb       NULL CHECK (sources IS NULL OR jsonb_typeof(sources) = 'array'), -- kỳ nguồn + số ảnh chụp
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
  CONSTRAINT cnts_agg_close_stream CHECK (stream_key = 'cnts_agg:' || province_code || ':' || form_code || ':' || period_type || ':' || period_key),
  CONSTRAINT cnts_agg_close_form_type CHECK ((form_code = 'CNTS-BM02') = (period_type = 'ksgm_week')),
  CONSTRAINT cnts_agg_close_range CHECK (period_to >= period_from),
  CONSTRAINT cnts_agg_close_payload CHECK (CASE action
    WHEN 'closed' THEN rows IS NOT NULL AND totals IS NOT NULL AND units_total IS NOT NULL AND units_with_data IS NOT NULL
                       AND missing_units IS NOT NULL AND provisional IS NOT NULL AND figures_hash IS NOT NULL AND sources IS NOT NULL
    ELSE reason IS NOT NULL AND rows IS NULL AND figures_hash IS NULL END)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_agg_close_key ON pf_extension.cnts_agg_close (tenant_id, province_code, form_code, period_type, period_key, seq);

-- Khóa tư vấn chung của một kỳ tổng hợp (chốt/mở lại và ghi dòng điều chỉnh/nuôi trồng xếp hàng tại đây).
CREATE FUNCTION pf_extension.fn_cnts_agg_lock(p_tenant uuid, p_province text, p_form text, p_type text, p_key text) RETURNS boolean
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_last text;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('cnts_agg:' || p_tenant::text || ':' || p_province || ':' || p_form || ':' || p_type || ':' || p_key, 0));
  SELECT c.action INTO v_last FROM pf_extension.cnts_agg_close c
   WHERE c.tenant_id = p_tenant AND c.stream_key = 'cnts_agg:' || p_province || ':' || p_form || ':' || p_type || ':' || p_key
   ORDER BY c.seq DESC LIMIT 1;
  RETURN v_last = 'closed';
END $$;

-- Máy trạng thái: chốt khi đang mở (chưa chốt lần nào hoặc vừa mở lại); mở lại khi đang chốt.
CREATE FUNCTION pf_extension.fn_cnts_agg_close_transition() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_closed boolean;
BEGIN
  v_closed := pf_extension.fn_cnts_agg_lock(NEW.tenant_id, NEW.province_code, NEW.form_code, NEW.period_type, NEW.period_key);
  IF NEW.action = 'closed' AND v_closed THEN
    RAISE EXCEPTION 'pf_extension: bảng tổng hợp % % đã chốt', NEW.form_code, NEW.period_key
      USING ERRCODE = 'check_violation', CONSTRAINT = 'cnts_agg_close_state';
  END IF;
  IF NEW.action = 'reopened' AND NOT coalesce(v_closed, false) THEN
    RAISE EXCEPTION 'pf_extension: bảng tổng hợp % % chưa chốt', NEW.form_code, NEW.period_key
      USING ERRCODE = 'check_violation', CONSTRAINT = 'cnts_agg_close_state';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_cnts_agg_close_transition BEFORE INSERT ON pf_extension.cnts_agg_close
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_cnts_agg_close_transition();

-- Dòng điều chỉnh BM-02 / số nuôi trồng: bảng của tuần đã chốt → chặn (mở lại kỳ trước — UR-TINH-14, III.3.0). Tham số
-- trigger = (mã biểu, loại kỳ) của bảng tổng hợp (TG_TABLE_NAME là tên phân vùng nên không dùng để rẽ nhánh).
CREATE FUNCTION pf_extension.fn_cnts_agg_input_open() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF coalesce(pf_extension.fn_cnts_agg_lock(NEW.tenant_id, NEW.province_code, TG_ARGV[0], TG_ARGV[1], NEW.week_key), false) THEN
    RAISE EXCEPTION 'pf_extension: % tuần % đã chốt — mở lại trước khi ghi', TG_ARGV[0], NEW.week_key
      USING ERRCODE = 'check_violation', CONSTRAINT = 'cnts_agg_input_closed';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_cnts_bm02_adjustment_open BEFORE INSERT ON pf_extension.cnts_bm02_adjustment
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_cnts_agg_input_open('CNTS-BM02', 'ksgm_week');
CREATE TRIGGER trg_cnts_aqua_entry_open BEFORE INSERT ON pf_extension.cnts_aqua_entry
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_cnts_agg_input_open('CNTS-BM18', 'week');

-- ---------------------------------------------------------------------------
-- 4. Phiên bản tệp chia sẻ Chi cục
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.cnts_agg_share (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'cnts_share:<tỉnh>:<biểu>:<loại kỳ>:<khóa>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  province_code     text        NOT NULL CHECK (province_code ~ '^[0-9A-Za-z_-]{1,20}$'),
  form_code         text        NOT NULL CHECK (form_code IN ('CNTS-BM02', 'CNTS-BM18')),
  period_type       text        NOT NULL CHECK (period_type IN ('ksgm_week', 'month', 'week')),
  period_key        text        NOT NULL CHECK (period_key ~ '^[0-9]{4}-(W[0-9]{2}|(0[1-9]|1[0-2])(-K[1-4])?)$'),
  basis             text        NOT NULL CHECK (basis IN ('submitted', 'closed')),
  content           jsonb       NOT NULL CHECK (jsonb_typeof(content) = 'object'),
  content_hash      text        NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
  recipient         text        NOT NULL CHECK (char_length(recipient) BETWEEN 1 AND 200),
  channel           text        NOT NULL CHECK (channel IN ('download')),
  note              text        NULL CHECK (note IS NULL OR char_length(note) <= 500),
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
  CONSTRAINT cnts_agg_share_stream CHECK (stream_key = 'cnts_share:' || province_code || ':' || form_code || ':' || period_type || ':' || period_key),
  CONSTRAINT cnts_agg_share_form_type CHECK ((form_code = 'CNTS-BM18') = (period_type = 'week'))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_agg_share_key ON pf_extension.cnts_agg_share (tenant_id, province_code, form_code, period_type, period_key, seq);
CREATE INDEX cnts_agg_share_id ON pf_extension.cnts_agg_share (tenant_id, id);

SELECT shared.apply_ledger('pf_extension.cnts_bm02_adjustment', 'pf_extension.cnts_bm02_adjustment');
SELECT shared.apply_ledger('pf_extension.cnts_aqua_entry', 'pf_extension.cnts_aqua_entry');
SELECT shared.apply_ledger('pf_extension.cnts_agg_close', 'pf_extension.cnts_agg_close');
SELECT shared.apply_ledger('pf_extension.cnts_agg_share', 'pf_extension.cnts_agg_share');

GRANT SELECT, INSERT ON pf_extension.cnts_bm02_adjustment, pf_extension.cnts_aqua_entry, pf_extension.cnts_agg_close,
  pf_extension.cnts_agg_share TO r_extension;
