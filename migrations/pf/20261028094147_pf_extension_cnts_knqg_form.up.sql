-- pf/extension (`dom/extension`, phòng ban chan_nuoi_thuy_san) — W11.53, D4 CN13, BRD CNTS v3.7 FN-19, UR-TINH-20…24,
-- UR-QG-01: MẪU BIỂU DỰ ÁN KHUYẾN NÔNG QUỐC GIA — Mẫu 01/TĐ-DA tiến độ tháng (BM-12), Mẫu 03 kiểm tra đánh giá (BM-13),
-- Mẫu 04/DA-NTTH nghiệm thu lớp tập huấn (BM-14), Mẫu 05/DA-NTMH nghiệm thu điểm trình diễn (BM-15), Mẫu 07/NTKQ-DA nghiệm
-- thu dự án năm (BM-16) — gắn dự án/điểm của KN (pf_extension.cnts_project, cnts_demo_site — W11.47). KN không ghi dữ liệu
-- HTX (ADR-0055, Q-KNS-04).
--
-- 1. cnts_knqg_doc — sổ chỉ ghi thêm (shared.apply_ledger), chuỗi 'knqg:<doc_ref>': mỗi dòng là MỘT phiên bản đủ trường của
--    một biên bản/báo cáo: mẫu, dự án, điểm (Mẫu 05), kỳ (tháng Mẫu 01, năm Mẫu 07), số bản điều chỉnh (revision), trạng
--    thái, nội dung (jsonb theo đặc tả mẫu — ô và bảng), tệp scan đã ký (khóa kho đối tượng + băm). Sự việc: created,
--    updated, submitted, approved, returned (ý kiến bắt buộc), sign_requested, unrequested, signed (kèm scan), adjusted (bản
--    điều chỉnh — revision + 1, lý do bắt buộc), sent (gửi TTKN Quốc gia), confirmed (Cấp TW xác nhận — khóa hẳn).
--    Tệp scan ở kho đối tượng (cùng kho của kho văn bản phòng, W11.54); sổ chỉ giữ khóa và SHA-256.
-- Vai r_extension: SELECT, INSERT.

CREATE TABLE pf_extension.cnts_knqg_doc (
  id                  uuid          NOT NULL,             -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'knqg:<doc_ref>'
  seq                 bigint        NOT NULL,
  doc_ref             uuid          NOT NULL,             -- định danh bền của biên bản/báo cáo
  form                text          NOT NULL CHECK (form IN ('01', '03', '04', '05', '07')),
  project_ref         uuid          NOT NULL,             -- dự án của KN (W11.47)
  site_ref            uuid          NULL,                 -- điểm trình diễn (chỉ Mẫu 05)
  period              text          NULL,                 -- 'YYYY-MM' (Mẫu 01) | 'YYYY' (Mẫu 07)
  revision            integer       NOT NULL CHECK (revision BETWEEN 1 AND 1000), -- bản điều chỉnh thứ n
  event               text          NOT NULL CHECK (event IN ('created', 'updated', 'submitted', 'approved', 'returned',
                        'sign_requested', 'unrequested', 'signed', 'adjusted', 'sent', 'confirmed')),
  status              text          NOT NULL CHECK (status IN ('draft', 'submitted', 'approved', 'returned', 'awaiting_sign',
                        'signed', 'sent', 'confirmed')),
  content             jsonb         NOT NULL CHECK (jsonb_typeof(content) = 'object' AND octet_length(content::text) <= 262144),
  signed_key          text          NULL CHECK (signed_key IS NULL OR char_length(signed_key) BETWEEN 1 AND 300),
  signed_name         text          NULL CHECK (signed_name IS NULL OR char_length(signed_name) BETWEEN 1 AND 200),
  signed_type         text          NULL CHECK (signed_type IS NULL OR signed_type IN ('application/pdf', 'image/jpeg', 'image/png')),
  signed_size         bigint        NULL CHECK (signed_size IS NULL OR signed_size BETWEEN 1 AND 10485760),
  signed_sha256       text          NULL CHECK (signed_sha256 IS NULL OR signed_sha256 ~ '^[0-9a-f]{64}$'),
  status_reason       text          NULL CHECK (status_reason IS NULL OR char_length(status_reason) BETWEEN 1 AND 1000),
  event_time          timestamptz   NOT NULL,
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  on_behalf_of        uuid          NULL,                 -- chế độ hỗ trợ của Admin
  support_reason      text          NULL,
  idem_key            text          NOT NULL,
  rule_version        text          NOT NULL,
  prev_hash           bytea         NOT NULL,
  row_hash            bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT cnts_knqg_doc_stream CHECK (stream_key = 'knqg:' || doc_ref::text),
  CONSTRAINT cnts_knqg_doc_site CHECK ((form = '05') = (site_ref IS NOT NULL)),
  CONSTRAINT cnts_knqg_doc_period CHECK (
    (form = '01' AND period ~ '^[0-9]{4}-(0[1-9]|1[0-2])$') OR (form = '07' AND period ~ '^[0-9]{4}$') OR
    (form IN ('03', '04', '05') AND period IS NULL)),
  CONSTRAINT cnts_knqg_doc_lifecycle CHECK (
    (form = '01' AND status IN ('draft', 'submitted', 'approved', 'returned')) OR
    (form IN ('03', '04', '05') AND status IN ('draft', 'awaiting_sign', 'signed')) OR
    (form = '07' AND status IN ('draft', 'returned', 'sent', 'confirmed'))),
  CONSTRAINT cnts_knqg_doc_reason CHECK (event NOT IN ('returned', 'adjusted') OR status_reason IS NOT NULL),
  CONSTRAINT cnts_knqg_doc_signed CHECK (status <> 'signed' OR (signed_key IS NOT NULL AND signed_sha256 IS NOT NULL)),
  CONSTRAINT cnts_knqg_doc_scan CHECK ((signed_key IS NULL) = (signed_sha256 IS NULL) AND (signed_key IS NULL) = (signed_size IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_knqg_doc_ref ON pf_extension.cnts_knqg_doc (tenant_id, doc_ref, seq);
CREATE INDEX cnts_knqg_doc_project ON pf_extension.cnts_knqg_doc (tenant_id, project_ref, form);
CREATE INDEX cnts_knqg_doc_idem ON pf_extension.cnts_knqg_doc (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_extension.cnts_knqg_doc', 'pf_extension.cnts_knqg_doc');

REVOKE ALL ON pf_extension.cnts_knqg_doc FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_knqg_doc TO r_extension;
