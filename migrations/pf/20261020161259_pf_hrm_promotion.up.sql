-- pf/hrm (`dom/hrm`, W11.25 — ADR-0050, BRD Hành chính – Tổng hợp v3.0 FN-09, FN-10): NÂNG BẬC LƯƠNG.
--
-- Năm sổ chỉ ghi thêm (shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo tenant). KHÔNG có dữ liệu cá
-- nhân: họ tên, năm sinh, giới tính đọc ở kho định danh khi dựng màn/tệp (RB-3); sổ chỉ giữ mã hồ sơ, mã danh mục, số.
--   - annual_rating      — XẾP LOẠI NĂM dùng kiểm điều kiện nâng bậc (FN-09 BR-02): nguồn 'conversion' (dữ liệu chuyển
--                          đổi ban đầu), 'manual' (HCTH nhập), 'evaluation' (W11.26 FN-18 ghi sau). Sửa = dòng mới
--                          supersedes_id + lý do; hủy = action 'void'. Chuỗi 'rating:<staff_id>'.
--   - promotion_event    — TRẠNG THÁI XỬ LÝ của từng kỳ nâng bậc (case_key = '<staff_id>:<loại>:<khóa kỳ>'): đưa vào /
--                          bỏ khỏi danh sách (kèm lý do khi "Chưa đủ"), đề xuất / gỡ NLTH từ thành tích, đã xuất, đã có quyết
--                          định, hủy quyết định (bù), trả về bảng theo dõi (người không có tên trong quyết định — FN-10
--                          BR-02). Trạng thái hiện hành = dòng mới nhất của case_key. Chuỗi 'promo:<staff_id>'.
--   - promotion_export   — LẦN XUẤT Mẫu số 1/2/4 (FN-09 B4, AC-04): tháng xét, người lập (mã hồ sơ), chức danh người ký,
--                          các dòng (mã, số — KHÔNG họ tên), tên tệp, SHA-256, thời điểm biết; 'download' = tải lại (dựng
--                          lại, so SHA-256 — drift khi định danh ở kho đã đổi). Tệp không lưu. Chuỗi 'promo-export:<form>'.
--   - promotion_decision — SỔ QUYẾT ĐỊNH (FN-10): 'issue' (số, ngày, cơ quan, loại, danh sách gốc, người có tên + bậc/hệ
--                          số/% TNVK cũ→mới, ngày hưởng, dòng quá trình bậc đã ghi); 'attach' (tệp ở Kho tài liệu FN-14);
--                          'cancel' = SỰ KIỆN BÙ (supersedes_id = dòng issue + lý do ≥ 10 ký tự) — cùng giao dịch ghi dòng
--                          'void' cho các dòng bậc đã ghi vào staff_event (khôi phục bậc, hệ số trước đó). Quyết định không
--                          xóa (BR-03). Chuỗi 'decision:<decision_id>'.
--   - promotion_scan     — KẾT QUẢ JOB HẰNG NGÀY hrm_promotion_scan: số người đến kỳ trong 1/3/6 tháng theo loại, số
--                          "Chưa đủ"/"Cần xem xét", số thiếu mốc xếp bậc (không PII). Chuỗi 'scan'.
-- Bậc, hệ số trong hồ sơ là phép chiếu của staff_event (W11.22): áp quyết định ghi sự kiện 'step' CÙNG GIAO DỊCH với dòng
-- 'issue'. % TNVK không lưu trong hồ sơ (NL04-03) — lấy từ sổ quyết định.
-- Vai: r_hrm SELECT, INSERT. ag_ops_jobs (BYPASSRLS) chỉ đọc cột tenant_id của pf_hrm.staff để tìm tenant cho job.

CREATE TABLE pf_hrm.annual_rating (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'rating:<staff_id>'
  seq               bigint      NOT NULL,
  staff_id          uuid        NOT NULL,
  year              smallint    NOT NULL CHECK (year BETWEEN 1990 AND 2100),
  rating            text        NULL CHECK (rating IS NULL OR rating IN ('HTXSNV', 'HTTNV', 'HTNV', 'KHTNV')),
  source            text        NOT NULL CHECK (source IN ('conversion', 'manual', 'evaluation')),
  action            text        NOT NULL CHECK (action IN ('add', 'void')),
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
  CHECK ((action = 'add') = (rating IS NOT NULL)),
  CHECK (action = 'add' OR supersedes_id IS NOT NULL),
  CONSTRAINT annual_rating_stream CHECK (stream_key = 'rating:' || staff_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX annual_rating_staff ON pf_hrm.annual_rating (tenant_id, staff_id, year, seq);
SELECT shared.apply_ledger('pf_hrm.annual_rating', 'pf_hrm.annual_rating');

CREATE TABLE pf_hrm.promotion_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'promo:<staff_id>'
  seq               bigint      NOT NULL,
  staff_id          uuid        NOT NULL,
  case_key          text        NOT NULL CHECK (case_key ~ '^[0-9a-f-]{36}:(nltx|nlth|tnvk):[0-9a-z-]{1,40}$'),
  kind              text        NOT NULL CHECK (kind IN ('nltx', 'nlth', 'tnvk')),
  action            text        NOT NULL CHECK (action IN ('include', 'exclude', 'propose', 'withdraw', 'export', 'decide',
                                  'undecide', 'return')),
  due_on            date        NULL,                     -- ngày đến kỳ khi ghi (thông tin; khóa kỳ ở case_key)
  award_event_id    uuid        NULL,                     -- NLTH: dòng khen thưởng (staff_event kind 'award') dùng quy đổi
  months            smallint    NULL CHECK (months IS NULL OR months BETWEEN 0 AND 36),
  export_id         uuid        NULL,
  decision_id       uuid        NULL,
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 1000),
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
  CHECK (action <> 'propose' OR (kind = 'nlth' AND award_event_id IS NOT NULL)),
  CHECK (action <> 'export' OR export_id IS NOT NULL),
  CHECK (action NOT IN ('decide', 'undecide', 'return') OR decision_id IS NOT NULL),
  CONSTRAINT promotion_event_case CHECK (starts_with(case_key, staff_id::text || ':' || kind || ':')),
  CONSTRAINT promotion_event_stream CHECK (stream_key = 'promo:' || staff_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX promotion_event_case ON pf_hrm.promotion_event (tenant_id, case_key, seq);
CREATE INDEX promotion_event_staff ON pf_hrm.promotion_event (tenant_id, staff_id, seq);
SELECT shared.apply_ledger('pf_hrm.promotion_event', 'pf_hrm.promotion_event');

CREATE TABLE pf_hrm.promotion_export (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'promo-export:<form>'
  seq               bigint      NOT NULL,
  action            text        NOT NULL CHECK (action IN ('export', 'download')),
  form              text        NOT NULL CHECK (form IN ('mau1', 'mau2', 'mau4')),
  review_month      date        NOT NULL CHECK (extract(day FROM review_month) = 1),
  as_of             date        NOT NULL,
  export_id         uuid        NULL,                     -- download: lần xuất gốc
  known_at          timestamptz NULL,                     -- export: thời điểm biết của dữ liệu đã dựng
  params            jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(params) = 'object'),
  items             jsonb       NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(items) = 'array'),  -- dòng: mã, số (không PII)
  staff_ids         uuid[]      NOT NULL CHECK (cardinality(staff_ids) BETWEEN 1 AND 2000),
  file_name         text        NOT NULL CHECK (file_name ~ '^[A-Za-z0-9._-]{1,120}$'),
  file_sha256       text        NOT NULL CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
  byte_size         bigint      NOT NULL CHECK (byte_size > 0),
  drift             boolean     NULL,
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
  CHECK ((action = 'export') = (known_at IS NOT NULL)),
  CHECK ((action = 'download') = (export_id IS NOT NULL AND drift IS NOT NULL)),
  CONSTRAINT promotion_export_stream CHECK (stream_key = 'promo-export:' || form)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX promotion_export_form ON pf_hrm.promotion_export (tenant_id, form, recorded_at);
CREATE INDEX promotion_export_export ON pf_hrm.promotion_export (tenant_id, export_id);
CREATE INDEX promotion_export_staff ON pf_hrm.promotion_export USING gin (staff_ids);
SELECT shared.apply_ledger('pf_hrm.promotion_export', 'pf_hrm.promotion_export');

CREATE TABLE pf_hrm.promotion_decision (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'decision:<decision_id>'
  seq               bigint      NOT NULL,
  decision_id       uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('issue', 'attach', 'cancel')),
  kind              text        NOT NULL CHECK (kind IN ('nltx', 'nlth', 'tnvk')),
  decision_no       text        NOT NULL CHECK (char_length(decision_no) BETWEEN 1 AND 60),
  decided_on        date        NOT NULL,
  issuer            text        NOT NULL CHECK (char_length(issuer) BETWEEN 1 AND 200),
  export_id         uuid        NULL,                     -- danh sách gốc (NULL = nhập quyết định đã ban hành)
  doc_id            uuid        NULL,                     -- attach: tài liệu ở Kho tài liệu nhân sự (FN-14)
  people            jsonb       NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(people) = 'array'),
  staff_ids         uuid[]      NOT NULL CHECK (cardinality(staff_ids) <= 2000),
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
  CHECK ((action = 'cancel') = (supersedes_id IS NOT NULL)),
  CHECK ((action = 'attach') = (doc_id IS NOT NULL)),
  CHECK (action <> 'issue' OR cardinality(staff_ids) >= 1),
  CONSTRAINT promotion_decision_stream CHECK (stream_key = 'decision:' || decision_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX promotion_decision_id ON pf_hrm.promotion_decision (tenant_id, decision_id, seq);
CREATE INDEX promotion_decision_no ON pf_hrm.promotion_decision (tenant_id, lower(decision_no));
CREATE INDEX promotion_decision_staff ON pf_hrm.promotion_decision USING gin (staff_ids);
SELECT shared.apply_ledger('pf_hrm.promotion_decision', 'pf_hrm.promotion_decision');

CREATE TABLE pf_hrm.promotion_scan (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL CHECK (stream_key = 'scan'),
  seq               bigint      NOT NULL,
  scan_day          date        NOT NULL,
  counts            jsonb       NOT NULL CHECK (jsonb_typeof(counts) = 'object'),
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX promotion_scan_day ON pf_hrm.promotion_scan (tenant_id, scan_day);
SELECT shared.apply_ledger('pf_hrm.promotion_scan', 'pf_hrm.promotion_scan');

REVOKE ALL ON pf_hrm.annual_rating, pf_hrm.promotion_event, pf_hrm.promotion_export, pf_hrm.promotion_decision,
  pf_hrm.promotion_scan FROM PUBLIC;
GRANT SELECT, INSERT ON pf_hrm.annual_rating, pf_hrm.promotion_event, pf_hrm.promotion_export, pf_hrm.promotion_decision,
  pf_hrm.promotion_scan TO r_hrm;

-- Job hrm_promotion_scan (cmd/worker): tìm tenant có hồ sơ nhân sự; việc tính chạy bằng r_hrm theo từng tenant.
GRANT USAGE ON SCHEMA pf_hrm TO ag_ops_jobs;
GRANT SELECT (tenant_id) ON pf_hrm.staff TO ag_ops_jobs;
