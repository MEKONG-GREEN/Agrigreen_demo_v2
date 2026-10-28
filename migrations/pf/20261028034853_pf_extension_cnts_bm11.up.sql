-- pf_extension: Báo cáo phòng CNTS BM-11 tuần/tháng/quý/năm (W11.45 — BRD CNTS v3.7 UR-TINH-16, FN-16 BR-01…BR-06, Mục III.5
-- quy tắc 13, vòng đời "Báo cáo phòng (BM-11)"; D4 CN5; Q-046 giả định: giữ BM-11 gửi để LƯU, số liệu cho KHTC chỉ qua phiếu +
-- nguồn đã chốt). Chỉ đụng schema pf_extension.
--
--   1. cnts_bm11_report — đầu báo cáo (chỉ INSERT, không UPDATE/DELETE): tỉnh × loại × kỳ là duy nhất (một báo cáo mỗi kỳ),
--      danh sách nơi nhận chốt lúc tạo (mặc định tuần: Ban Giám đốc, HCTH; tháng/quý/năm: Ban Giám đốc, KHTC).
--   2. cnts_bm11_event — SỔ (append-only, shared.apply_ledger) mọi bước của báo cáo: tạo / làm mới số liệu / sửa phần viết
--      (mỗi bước mang NỘI DUNG ĐẦY ĐỦ đóng băng + băm = một phiên bản), trình, trả lại (bắt buộc ý kiến — AC-04), duyệt (duyệt
--      xong TỰ GỬI theo nơi nhận — BR-03, trạng thái sau = sent), phòng nhận bấm "Đã nhận" (mỗi nơi nhận một lần). Báo cáo đã
--      gửi không sửa, không xóa (BR-02). Trigger giữ máy trạng thái dưới khóa tư vấn của báo cáo (đồng thời an toàn).

SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_extension.cnts_bm11_report (
  tenant_id      uuid        NOT NULL,
  id             uuid        NOT NULL,                 -- UUIDv7 (R-11)
  province_code  text        NOT NULL CHECK (province_code ~ '^[0-9A-Za-z_-]{1,20}$'),
  report_type    text        NOT NULL CHECK (report_type IN ('week', 'month', 'quarter', 'year')),
  period_key     text        NOT NULL CHECK (period_key ~ '^[0-9]{4}(-W[0-9]{2}|-(0[1-9]|1[0-2])|-Q[1-4])?$'),
  period_from    date        NOT NULL,
  period_to      date        NOT NULL,
  period_label   text        NOT NULL CHECK (char_length(period_label) BETWEEN 1 AND 200),
  recipients     text[]      NOT NULL CHECK (cardinality(recipients) BETWEEN 1 AND 3 AND recipients <@ ARRAY['bgd', 'hcth', 'khtc']::text[]),
  created_by     uuid        NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  idem_key       text        NOT NULL CHECK (char_length(idem_key) BETWEEN 1 AND 300),
  PRIMARY KEY (tenant_id, id),
  CONSTRAINT cnts_bm11_report_period UNIQUE (tenant_id, province_code, report_type, period_key),
  CONSTRAINT cnts_bm11_report_idem UNIQUE (tenant_id, idem_key),
  CONSTRAINT cnts_bm11_report_range CHECK (period_to >= period_from),
  CONSTRAINT cnts_bm11_report_key CHECK (CASE report_type
    WHEN 'week' THEN period_key ~ '-W[0-9]{2}$'
    WHEN 'month' THEN period_key ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'
    WHEN 'quarter' THEN period_key ~ '-Q[1-4]$'
    ELSE period_key ~ '^[0-9]{4}$' END)
);
CREATE INDEX cnts_bm11_report_list ON pf_extension.cnts_bm11_report (tenant_id, province_code, period_to DESC);
SELECT shared.apply_tenant_rls('pf_extension.cnts_bm11_report');

CREATE TABLE pf_extension.cnts_bm11_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'cnts_bm11:<mã báo cáo>'
  seq               bigint      NOT NULL,                 -- = số phiên bản/bước của báo cáo (khóa lạc quan expected_seq)
  report_id         uuid        NOT NULL,
  province_code     text        NOT NULL CHECK (province_code ~ '^[0-9A-Za-z_-]{1,20}$'),
  event             text        NOT NULL CHECK (event IN ('created', 'refreshed', 'edited', 'submitted', 'returned', 'approved', 'received')),
  status            text        NOT NULL CHECK (status IN ('draft', 'pending', 'returned', 'sent')),
  content           jsonb       NULL CHECK (content IS NULL OR jsonb_typeof(content) = 'object'),
  content_hash      text        NULL CHECK (content_hash IS NULL OR content_hash ~ '^[0-9a-f]{64}$'),
  comment           text        NULL CHECK (comment IS NULL OR char_length(comment) BETWEEN 1 AND 2000),
  recipient         text        NULL CHECK (recipient IS NULL OR recipient IN ('bgd', 'hcth', 'khtc')),
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
  CONSTRAINT cnts_bm11_event_stream CHECK (stream_key = 'cnts_bm11:' || report_id::text),
  CONSTRAINT cnts_bm11_event_content CHECK ((event IN ('created', 'refreshed', 'edited')) = (content IS NOT NULL AND content_hash IS NOT NULL)),
  CONSTRAINT cnts_bm11_event_comment CHECK (event <> 'returned' OR comment IS NOT NULL),
  CONSTRAINT cnts_bm11_event_recipient CHECK ((event = 'received') = (recipient IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_bm11_event_report ON pf_extension.cnts_bm11_event (tenant_id, report_id, seq);
CREATE INDEX cnts_bm11_event_idem ON pf_extension.cnts_bm11_event (tenant_id, idem_key);

-- Khóa tư vấn của một báo cáo (ứng dụng giữ cùng khóa khi đọc expected_seq rồi ghi).
CREATE FUNCTION pf_extension.fn_cnts_bm11_lock(p_tenant uuid, p_report uuid) RETURNS void
LANGUAGE sql
SET search_path = pg_catalog, pg_temp
AS $$ SELECT pg_advisory_xact_lock(hashtextextended('cnts_bm11:' || p_tenant::text || ':' || p_report::text, 0)) $$;

-- Máy trạng thái (Mục III.5): Đang soạn/Trả lại → (làm mới, sửa) Đang soạn → (trình) Chờ duyệt → (trả lại, có ý kiến) Trả lại
-- | (duyệt = tự gửi) Đã gửi → (mỗi nơi nhận bấm Đã nhận một lần, chỉ nơi nhận của báo cáo). Đã gửi không sửa.
CREATE FUNCTION pf_extension.fn_cnts_bm11_transition() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_last  text;
  v_next  text;
  v_recip text[];
BEGIN
  PERFORM pf_extension.fn_cnts_bm11_lock(NEW.tenant_id, NEW.report_id);
  SELECT e.status INTO v_last FROM pf_extension.cnts_bm11_event e
   WHERE e.tenant_id = NEW.tenant_id AND e.stream_key = NEW.stream_key ORDER BY e.seq DESC LIMIT 1;
  v_next := CASE
    WHEN NEW.event = 'created' AND v_last IS NULL THEN 'draft'
    WHEN NEW.event IN ('refreshed', 'edited', 'submitted') AND v_last IN ('draft', 'returned') THEN
      CASE NEW.event WHEN 'submitted' THEN 'pending' ELSE 'draft' END
    WHEN NEW.event = 'returned' AND v_last = 'pending' THEN 'returned'
    WHEN NEW.event = 'approved' AND v_last = 'pending' THEN 'sent'
    WHEN NEW.event = 'received' AND v_last = 'sent' THEN 'sent'
    ELSE NULL END;
  IF v_next IS NULL OR v_next <> NEW.status THEN
    RAISE EXCEPTION 'pf_extension: báo cáo BM-11 % không chuyển được % từ trạng thái %', NEW.report_id, NEW.event, coalesce(v_last, '(chưa có)')
      USING ERRCODE = 'check_violation', CONSTRAINT = 'cnts_bm11_state';
  END IF;
  IF NEW.event = 'received' THEN
    SELECT r.recipients INTO v_recip FROM pf_extension.cnts_bm11_report r WHERE r.tenant_id = NEW.tenant_id AND r.id = NEW.report_id;
    IF NOT (NEW.recipient = ANY (coalesce(v_recip, '{}'::text[]))) OR EXISTS (SELECT 1 FROM pf_extension.cnts_bm11_event e
        WHERE e.tenant_id = NEW.tenant_id AND e.stream_key = NEW.stream_key AND e.event = 'received' AND e.recipient = NEW.recipient) THEN
      RAISE EXCEPTION 'pf_extension: nơi nhận % không thuộc báo cáo hoặc đã xác nhận', NEW.recipient
        USING ERRCODE = 'check_violation', CONSTRAINT = 'cnts_bm11_received';
    END IF;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_cnts_bm11_transition BEFORE INSERT ON pf_extension.cnts_bm11_event
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_cnts_bm11_transition();

SELECT shared.apply_ledger('pf_extension.cnts_bm11_event', 'pf_extension.cnts_bm11_event');

REVOKE ALL ON pf_extension.cnts_bm11_report, pf_extension.cnts_bm11_event FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_bm11_report, pf_extension.cnts_bm11_event TO r_extension;
GRANT EXECUTE ON FUNCTION pf_extension.fn_cnts_bm11_lock(uuid, uuid) TO r_extension;
