-- pf/hrm (`dom/hrm`, W11.21 — ADR-0050, BRD Hành chính – Tổng hợp v3.0 FN-19, UR-ADM-08): khung module nhân sự +
-- DANH MỤC NHÂN SỰ. Không dữ liệu cá nhân ở gói này (hồ sơ, PII qua vault: W11.22, pf 000148).
--
-- 1. Schema pf_hrm (nội bộ). Không có schema _pub (báo cáo Sở NS01–06 ở W11.28 đọc trong module).
-- 2. Ba sổ chỉ ghi thêm (mẫu 8.1, shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo tenant):
--      - catalog_item  — mọi danh mục mã → tên của FN-19: chức vụ, ngạch/CDNN, trình độ chuyên môn, nhóm chuyên ngành
--                        (theo trình độ — NS05), chuyên ngành, và các danh sách khai báo sẵn (dân tộc, tôn giáo, LLCT,
--                        QLNN, ngoại ngữ, trình độ ngoại ngữ, tin học, hình thức khen thưởng, kỷ luật, loại biến động,
--                        lý do nghỉ việc). Mỗi dòng là MỘT phiên bản của (kind, code) — version = seq của chuỗi
--                        '<kind>:<code>'; phiên bản mới nhất là hiện hành. Ngừng sử dụng = phiên bản status 'retired'
--                        (BR-01: không xóa). Thuộc tính riêng của loại (cờ lãnh đạo, hệ số PCCV, nhóm tương đương, nhóm
--                        chuyên ngành theo trình độ …) ở attrs (jsonb, kiểm ở domain; số là chuỗi thập phân — R-CODE-01).
--      - grade_scale   — BẢNG HỆ SỐ có hiệu lực của một ngạch/CDNN: số bậc, hệ số từng bậc (numeric(5,2)[]), thời gian
--                        giữ bậc; chuỗi 'scale:<mã ngạch>', hiệu lực theo valid_from (BR-02, AC-02: đổi hệ số từ ngày
--                        hiệu lực mới; quyết định đã ban hành giữ hệ số cũ — W11.25 tra as-of ngày hưởng).
--      - rule_param    — tham số có phiên bản: quy tắc nâng bậc ('promotion'), bảng quy đổi thành tích
--                        ('achievement_conversion'); chuỗi 'rule:<key>', hiệu lực theo valid_from; payload jsonb kiểm ở
--                        domain (số thập phân là chuỗi).
--    placeholder = true: dữ liệu mẫu cài khi triển khai, CHƯA được Phòng HCTH xác nhận (Q-070) — mọi lần ghi của người
--    dùng ghi placeholder = false.
-- 3. Mã ngạch đã chuẩn hóa (bỏ khoảng trắng, xuống dòng — FN-19 AC-03) trước khi ghi; CHECK chặn khoảng trắng.
-- 4. Vai: r_hrm (vai nhóm sinh từ owns_schema — dbadmin; app_platform là thành viên) SELECT, INSERT. k_ref như mọi
--    module pf sinh sau 000092 (R-DB-05).

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_hrm' AND NOT rolcanlogin) THEN
    RAISE EXCEPTION 'thiếu vai nhóm r_hrm (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028, ADR-0050)';
  END IF;
END
$$;

CREATE SCHEMA pf_hrm;
REVOKE ALL ON SCHEMA pf_hrm FROM PUBLIC;

CREATE TABLE pf_hrm.catalog_item (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,                 -- RB-2
  stream_key        text        NOT NULL,                 -- '<kind>:<code>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  kind              text        NOT NULL CHECK (kind IN ('position', 'grade', 'education_level', 'major_group', 'major',
                                  'ethnicity', 'religion', 'political_theory', 'state_mgmt', 'foreign_language',
                                  'language_level', 'it_level', 'award_form', 'discipline_form', 'change_type',
                                  'leave_reason')),
  code              text        NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$'),
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
  sort_order        integer     NOT NULL CHECK (sort_order BETWEEN 0 AND 100000),
  status            text        NOT NULL CHECK (status IN ('active', 'retired')),
  attrs             jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(attrs) = 'object'),
  placeholder       boolean     NOT NULL,
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
  CONSTRAINT catalog_item_stream CHECK (stream_key = kind || ':' || code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX catalog_item_kind ON pf_hrm.catalog_item (tenant_id, kind, code, seq);
SELECT shared.apply_ledger('pf_hrm.catalog_item', 'pf_hrm.catalog_item');

CREATE TABLE pf_hrm.grade_scale (
  id                uuid           NOT NULL,              -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid           NOT NULL,
  stream_key        text           NOT NULL,              -- 'scale:<grade_code>'
  seq               bigint         NOT NULL,
  grade_code        text           NOT NULL CHECK (grade_code ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$'),
  valid_from        date           NOT NULL,              -- ngày hiệu lực của bảng hệ số (giờ VN)
  steps             smallint       NOT NULL CHECK (steps BETWEEN 1 AND 20),
  coefficients      numeric(5,2)[] NOT NULL CHECK (cardinality(coefficients) = steps AND array_position(coefficients, NULL) IS NULL),
  hold_months       smallint       NOT NULL CHECK (hold_months BETWEEN 1 AND 120),
  note              text           NULL CHECK (note IS NULL OR char_length(note) <= 500),
  placeholder       boolean        NOT NULL,
  event_time        timestamptz    NOT NULL,
  recorded_at       timestamptz    NOT NULL DEFAULT now(),
  supersedes_id     uuid           NULL,
  correction_reason text           NULL,
  actor_ref         uuid           NOT NULL,
  device_id         text           NULL,
  idem_key          text           NOT NULL,
  rule_version      text           NOT NULL,
  prev_hash         bytea          NOT NULL,
  row_hash          bytea          NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT grade_scale_stream CHECK (stream_key = 'scale:' || grade_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX grade_scale_grade ON pf_hrm.grade_scale (tenant_id, grade_code, valid_from, seq);
SELECT shared.apply_ledger('pf_hrm.grade_scale', 'pf_hrm.grade_scale');

CREATE TABLE pf_hrm.rule_param (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'rule:<key>'
  seq               bigint      NOT NULL,
  key               text        NOT NULL CHECK (key IN ('promotion', 'achievement_conversion')),
  valid_from        date        NOT NULL,
  payload           jsonb       NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
  note              text        NULL CHECK (note IS NULL OR char_length(note) <= 500),
  placeholder       boolean     NOT NULL,
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
  CONSTRAINT rule_param_stream CHECK (stream_key = 'rule:' || key)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX rule_param_key ON pf_hrm.rule_param (tenant_id, key, valid_from, seq);
SELECT shared.apply_ledger('pf_hrm.rule_param', 'pf_hrm.rule_param');

REVOKE ALL ON pf_hrm.catalog_item, pf_hrm.grade_scale, pf_hrm.rule_param FROM PUBLIC;

-- Vai của module: sổ chỉ ghi thêm (R-05, R-20).
GRANT USAGE ON SCHEMA pf_hrm TO r_hrm;
GRANT SELECT, INSERT ON pf_hrm.catalog_item, pf_hrm.grade_scale, pf_hrm.rule_param TO r_hrm;

-- Module pf mới có r_* (docs/dev/modules.md, W2.3 bước 5): đọc k_ref + biến ngữ cảnh (R-DB-05).
GRANT USAGE ON SCHEMA k_ref TO r_hrm;
GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) TO r_hrm;
GRANT SELECT ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope TO r_hrm;
