-- pf/extension (`dom/extension`, phòng ban ke_hoach_tai_chinh) — W11.61, D4 KH1, BRD KHTC v2.2 FN-AD-04.01, FN-TI-02.02:
-- DANH MỤC KHTC + DANH SÁCH CHƯƠNG TRÌNH/DỰ ÁN (CT/DA) + KẾ HOẠCH NĂM có phiên bản theo quyết định (QĐ).
--
-- 1. khtc_catalog_item — sổ chỉ ghi thêm (shared.apply_ledger): Danh mục KHTC FN-AD-04.01 — loại báo cáo, ĐVT, lĩnh vực,
--    nguồn kinh phí, người ký, nơi nhận, căn cứ. Mặt hàng giá cả KHÔNG ở đây —
--    một danh mục ở `cap/price` (Q-082 đề xuất của dev), KHTC đọc. Mỗi dòng là MỘT phiên bản của (kind, code) — chuỗi
--    '<kind>:<code>', version = seq; bản mới nhất là hiện hành. "Ngừng dùng" = phiên bản status 'retired' (mục đã dùng
--    không xóa). default_for = các mẫu báo cáo (mau_1…mau_5) nhận mục này làm mặc định (người ký, nơi nhận, căn cứ).
--    Thuộc tính riêng của loại (chức vụ, thẩm quyền ký của người ký; nhóm hàng, ĐVT của mặt hàng) ở attrs.
-- 2. khtc_program — sổ chỉ ghi thêm: DANH SÁCH CT/DA CHỦ (quyết định D20, Q-061 đề xuất: KHTC là chủ danh sách). Định
--    danh bền program_ref (UUIDv7) — CNTS (W11.47) gắn điểm/hộ, thu – chi từ nhật ký CNTS tham chiếu program_ref qua
--    port extension/port.KHTCPrograms; không module nào đọc thẳng bảng. Chuỗi 'program:<program_ref>', bản mới nhất là
--    hiện hành; ngừng = status 'retired'. Mã, thứ tự, mã La Mã THEO NĂM nằm ở kế hoạch năm (BR-DL-21), không ở đây.
-- 3. khtc_plan_version — sổ chỉ ghi thêm: phiên bản Kế hoạch năm. Chuỗi 'plan:<năm>', version = seq (phiên bản 1 = lập
--    kế hoạch, ≥ 2 = "Điều chỉnh theo QĐ"). Số QĐ, ngày QĐ, ngày hiệu lực (BR-DL-13: phiên bản dùng cho một kỳ = ngày hiệu
--    lực ≤ ngày chốt, gần nhất). entries_hash = sha256 nội dung chuẩn hóa của mọi dòng khtc_plan_entry của phiên bản
--    (toàn vẹn nội dung nằm trong chuỗi băm của sổ).
-- 4. khtc_plan_entry — bảng CHỈ GHI THÊM (shared.apply_append_only): toàn bộ cây của MỘT phiên bản (6 tab FN-TI-02.02:
--    programs, prev_year, central_project, cooperation, research, advisory). Cây: program (tham chiếu khtc_program) →
--    group (nhóm) → item (hạng mục, đúng một phòng phụ trách). entry_ref bền qua các phiên bản của cùng năm (so sánh,
--    lịch sử, phiếu W11.63 tham chiếu hạng mục). Phiên bản mới ghi lại cả cây — bản cũ không bị ghi đè.
-- Số lượng, kinh phí là numeric (R-CODE-01, không float). Vai r_extension: SELECT, INSERT.

CREATE TABLE pf_extension.khtc_catalog_item (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- '<kind>:<code>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  kind              text        NOT NULL CHECK (kind IN ('report_type', 'unit', 'field', 'funding_source', 'signer',
                                  'recipient', 'basis')),
  code              text        NOT NULL CHECK (code ~ '^[a-z0-9][a-z0-9._-]{0,39}$'),
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 500),
  sort_order        integer     NOT NULL CHECK (sort_order BETWEEN 0 AND 100000),
  status            text        NOT NULL CHECK (status IN ('active', 'retired')),
  default_for       text[]      NOT NULL DEFAULT '{}' CHECK (array_position(default_for, NULL) IS NULL),
  attrs             jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(attrs) = 'object'),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  actor_ref         uuid        NOT NULL,
  on_behalf_of      uuid        NULL,                     -- chế độ hỗ trợ của Admin
  support_reason    text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT khtc_catalog_item_stream CHECK (stream_key = kind || ':' || code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_catalog_item_kind ON pf_extension.khtc_catalog_item (tenant_id, kind, code, seq);
SELECT shared.apply_ledger('pf_extension.khtc_catalog_item', 'pf_extension.khtc_catalog_item');

CREATE TABLE pf_extension.khtc_program (
  id                uuid        NOT NULL,                 -- UUIDv7 — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'program:<program_ref>'
  seq               bigint      NOT NULL,
  program_ref       uuid        NOT NULL,                 -- định danh bền của CT/DA (D20)
  code              text        NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$'),
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 500),
  kind              text        NOT NULL CHECK (kind IN ('program', 'project', 'central_project', 'cooperation',
                                  'research', 'advisory')),
  field_code        text        NULL,                     -- lĩnh vực (khtc_catalog_item kind 'field')
  owner_department  text        NULL CHECK (owner_department IS NULL OR owner_department ~ '^[a-z][a-z0-9_]{1,39}$'),
  start_year        smallint    NULL CHECK (start_year IS NULL OR start_year BETWEEN 2000 AND 2100),
  end_year          smallint    NULL CHECK (end_year IS NULL OR end_year BETWEEN 2000 AND 2100),
  status            text        NOT NULL CHECK (status IN ('active', 'retired')),
  note              text        NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  actor_ref         uuid        NOT NULL,
  on_behalf_of      uuid        NULL,
  support_reason    text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK (end_year IS NULL OR start_year IS NULL OR end_year >= start_year),
  CONSTRAINT khtc_program_stream CHECK (stream_key = 'program:' || program_ref::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_program_ref ON pf_extension.khtc_program (tenant_id, program_ref, seq);
CREATE INDEX khtc_program_code ON pf_extension.khtc_program (tenant_id, code);
SELECT shared.apply_ledger('pf_extension.khtc_program', 'pf_extension.khtc_program');

CREATE TABLE pf_extension.khtc_plan_version (
  id                uuid        NOT NULL,                 -- UUIDv7 — mã phiên bản
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'plan:<năm>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  plan_year         smallint    NOT NULL CHECK (plan_year BETWEEN 2000 AND 2100),
  basis             text        NOT NULL CHECK (basis IN ('initial', 'adjust')),
  decision_no       text        NULL CHECK (decision_no IS NULL OR char_length(decision_no) BETWEEN 1 AND 100),
  decision_date     date        NULL,
  effective_from    date        NOT NULL,
  copied_from_year  smallint    NULL CHECK (copied_from_year IS NULL OR copied_from_year BETWEEN 2000 AND 2100),
  note              text        NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  entries_count     integer     NOT NULL CHECK (entries_count BETWEEN 0 AND 5000),
  entries_hash      bytea       NOT NULL CHECK (length(entries_hash) = 32),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  actor_ref         uuid        NOT NULL,
  on_behalf_of      uuid        NULL,
  support_reason    text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  -- "Điều chỉnh theo QĐ": số QĐ, ngày QĐ, ngày hiệu lực bắt buộc (FN-TI-02.02 B5).
  CHECK (basis = 'initial' OR (decision_no IS NOT NULL AND decision_date IS NOT NULL)),
  CONSTRAINT khtc_plan_version_stream CHECK (stream_key = 'plan:' || plan_year::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_plan_version_year ON pf_extension.khtc_plan_version (tenant_id, plan_year, seq);
CREATE UNIQUE INDEX khtc_plan_version_id ON pf_extension.khtc_plan_version (tenant_id, id, recorded_at);
SELECT shared.apply_ledger('pf_extension.khtc_plan_version', 'pf_extension.khtc_plan_version');

CREATE TABLE pf_extension.khtc_plan_entry (
  tenant_id         uuid          NOT NULL,
  version_id        uuid          NOT NULL,               -- khtc_plan_version.id (cùng giao dịch)
  plan_year         smallint      NOT NULL CHECK (plan_year BETWEEN 2000 AND 2100),
  entry_ref         uuid          NOT NULL,               -- bền qua các phiên bản của năm
  position          integer       NOT NULL CHECK (position BETWEEN 0 AND 4999), -- thứ tự trong phiên bản (cha trước con)
  tab               text          NOT NULL CHECK (tab IN ('programs', 'prev_year', 'central_project', 'cooperation',
                                    'research', 'advisory')),
  kind              text          NOT NULL CHECK (kind IN ('program', 'group', 'item')),
  parent_ref        uuid          NULL,
  program_ref       uuid          NULL,                   -- khtc_program.program_ref (dòng program bắt buộc)
  sort_order        integer       NOT NULL CHECK (sort_order BETWEEN 0 AND 100000),
  code              text          NULL CHECK (code IS NULL OR char_length(code) BETWEEN 1 AND 40),
  name              text          NOT NULL CHECK (char_length(name) BETWEEN 1 AND 1000),
  unit_code         text          NULL,                   -- ĐVT (danh mục KHTC kind 'unit')
  planned_qty       numeric(20,4) NULL CHECK (planned_qty IS NULL OR planned_qty >= 0),
  scale_note        text          NULL CHECK (scale_note IS NULL OR char_length(scale_note) <= 1000),
  budget            numeric(20,2) NULL CHECK (budget IS NULL OR budget >= 0),  -- kinh phí KH (đồng)
  funding_code      text          NULL,                   -- nguồn kinh phí (kind 'funding_source')
  field_code        text          NULL,                   -- lĩnh vực (kind 'field')
  department_code   text          NULL CHECK (department_code IS NULL OR department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  communes          text[]        NOT NULL DEFAULT '{}' CHECK (cardinality(communes) <= 500),
  points_count      integer       NULL CHECK (points_count IS NULL OR points_count BETWEEN 0 AND 100000),
  data_source       text          NULL CHECK (data_source IS NULL OR data_source IN ('form', 'module')),
  source_key        text          NULL CHECK (source_key IS NULL OR source_key ~ '^[a-z][a-z0-9_.:-]{1,99}$'),
  tracking          text          NOT NULL CHECK (tracking IN ('tracking', 'stopped')),
  stopped_from      date          NULL,
  stop_decision     text          NULL CHECK (stop_decision IS NULL OR char_length(stop_decision) <= 100),
  note              text          NULL CHECK (note IS NULL OR char_length(note) <= 2000),
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, version_id, entry_ref),
  CHECK (kind <> 'program' OR (program_ref IS NOT NULL AND parent_ref IS NULL)),
  CHECK (kind = 'program' OR parent_ref IS NOT NULL),
  -- Hạng mục có đúng một phòng phụ trách (FN-TI-02.02 quy tắc); nguồn module phải có khóa nguồn (BR-DL-10).
  CHECK (kind <> 'item' OR (department_code IS NOT NULL AND data_source IS NOT NULL)),
  CHECK (data_source IS DISTINCT FROM 'module' OR source_key IS NOT NULL),
  CHECK ((tracking = 'stopped') = (stopped_from IS NOT NULL))
);
CREATE INDEX khtc_plan_entry_dept ON pf_extension.khtc_plan_entry (tenant_id, plan_year, department_code);
CREATE INDEX khtc_plan_entry_program ON pf_extension.khtc_plan_entry (tenant_id, program_ref) WHERE program_ref IS NOT NULL;
SELECT shared.apply_append_only('pf_extension.khtc_plan_entry');

REVOKE ALL ON pf_extension.khtc_catalog_item, pf_extension.khtc_program, pf_extension.khtc_plan_version,
  pf_extension.khtc_plan_entry FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.khtc_catalog_item, pf_extension.khtc_program, pf_extension.khtc_plan_version,
  pf_extension.khtc_plan_entry TO r_extension;
