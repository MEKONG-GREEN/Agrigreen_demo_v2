-- pf/price (`cap/price`, W11.78 — BRD Báo giá nông sản v1.3 FN-TI-02.10…02.12, FN-TW-01.04; Mục II.3 bảng 3.4, II.4
-- CT-05, CT-06): ĐIỀU TRA GIÁ THÀNH SẢN XUẤT THEO VỤ — đợt điều tra, xã điều tra + chỉ tiêu, cán bộ điều tra, phiếu điều
-- tra nhập từ tệp (hai bước: xem trước → ghi nhận), lần nhập + dòng lỗi / cảnh báo, bảng từ đồng nghĩa.
--
-- Cùng mẫu pf 000148: mọi bảng là sổ chỉ ghi thêm (shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS
-- theo tenant); mỗi dòng là MỘT phiên bản của một chuỗi, trạng thái hiện hành = phiên bản có seq lớn nhất. "Đang rà soát"
-- khi hết thời gian thu thập, cờ bất thường CT-06 (trung vị giá bán cùng mặt hàng trong đợt), giá thành CT-05 là PHÉP
-- CHIẾU lúc đọc — không job, không cột tính sẵn (BR-01 FN-TI-02.11: mọi giá trị tính toán do hệ thống tính lại).
--
-- 1. survey_round — đầu đợt (chuỗi 'sround:<round_id>'): tên, vụ, năm, thời gian thu thập, nhóm cây áp dụng, trạng thái
--    'draft' (Nháp) → 'collecting' (Đang thu thập) → 'reviewing' (Đang rà soát — đóng sớm) → 'closed' (Đã chốt); mở lại
--    đợt đã chốt về 'reviewing' (Quản trị nghiệp vụ, lý do bắt buộc), gia hạn có lý do.
-- 2. survey_commune — xã điều tra của đợt (chuỗi 'scommune:<round_id>:<mã ĐVHC xã>') + chỉ tiêu số phiếu theo 4 nhóm cây.
-- 3. survey_officer — cán bộ điều tra của xã (chuỗi 'sofficer:<round_id>:<officer_id>'): KHÔNG họ tên / SĐT — chỉ mã
--    chủ thể ở kho định danh (subject_ref, R-04), đơn vị công tác (tên tổ chức, không phải dữ liệu cá nhân).
-- 4. survey_form — phiếu điều tra (chuỗi 'sform:<form_id>'): xã, nhóm cây, mặt hàng (danh mục dùng chung — BR-07
--    FN-AD-04.03), nông hộ = mã chủ thể ở kho định danh (farmer_ref; họ tên, SĐT ở vault — BR-03 FN-TI-02.11, NĐ 13/2023),
--    diện tích, sản lượng (kg), giá bán, thu khác, 6 khoản chi phí (numeric — không float, R-CODE-01). Trạng thái
--    'imported' (Đã nhập) → 'verify' (Cần xác minh) → 'verified' (Đã xác minh) → 'approved' (Đã duyệt) / 'rejected'
--    (Loại); 'deleted' (Đã xóa — xóa mềm có lý do khi đợt chưa chốt). verified_price = giá bán hiệu chỉnh sau xác minh (giữ
--    sale_price gốc — BR-04 FN-TI-02.12); note = nội dung xác minh / ghi chú duyệt / lý do loại / lý do xóa.
-- 5. survey_import — lần nhập tệp (chuỗi 'simport:<import_id>'): mã băm SHA-256 của tệp (nhập lại cùng tệp không tạo
--    phiếu trùng), tên tệp, số dòng / đã ghi / bỏ qua / cảnh báo / lỗi.
-- 6. survey_issue — dòng lỗi / cảnh báo của lần nhập (chuỗi 'sissue:<import_id>:<số dòng>'): "báo cáo lỗi được lưu"
--    (hậu điều kiện FN-TI-02.11). Thông điệp KHÔNG chứa giá trị của tệp (không họ tên, SĐT — R-14).
-- 7. survey_synonym — bảng từ đồng nghĩa của tenant (chuỗi 'ssyn:<kind>:<khóa chuẩn hóa>'): tên mặt hàng / tên xã đã
--    chuẩn hóa (bỏ dấu, chữ thường, gộp khoảng trắng) → mã mặt hàng / mã ĐVHC; dùng cho các lần nhập sau.
--
-- Vai r_price SELECT, INSERT (sổ chỉ ghi thêm — R-05, R-20).

CREATE TABLE pf_price.survey_round (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'sround:<round_id>'
  seq               bigint      NOT NULL,
  round_id          uuid        NOT NULL,
  profile           text        NOT NULL CHECK (profile ~ '^[a-z][a-z0-9_]{0,29}$'),
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
  season            text        NOT NULL CHECK (season IN ('dong_xuan', 'he_thu', 'thu_dong', 'khac')),
  year              integer     NOT NULL CHECK (year BETWEEN 2000 AND 2100),
  collect_from      date        NOT NULL,
  collect_to        date        NOT NULL,
  crops             text[]      NOT NULL CHECK (crops <@ ARRAY['lua', 'rau_mau', 'nam', 'cay_an_trai']::text[]),
  note              text        NULL CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 500),
  status            text        NOT NULL CHECK (status IN ('draft', 'collecting', 'reviewing', 'closed')),
  op                text        NOT NULL CHECK (op IN ('create', 'edit', 'open', 'close_early', 'extend', 'close', 'reopen')),
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
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
  CHECK (collect_from <= collect_to),
  CHECK (op NOT IN ('extend', 'reopen') OR reason IS NOT NULL),
  CHECK (status = 'draft' OR cardinality(crops) > 0),
  CONSTRAINT survey_round_stream CHECK (stream_key = 'sround:' || round_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX survey_round_id_seq ON pf_price.survey_round (tenant_id, round_id, seq);
SELECT shared.apply_ledger('pf_price.survey_round', 'pf_price.survey_round');

CREATE TABLE pf_price.survey_commune (
  id                 uuid        NOT NULL,
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,                -- 'scommune:<round_id>:<commune_code>'
  seq                bigint      NOT NULL,
  round_id           uuid        NOT NULL,
  commune_code       text        NOT NULL CHECK (char_length(commune_code) BETWEEN 1 AND 32),
  target_lua         integer     NOT NULL CHECK (target_lua >= 0),
  target_rau_mau     integer     NOT NULL CHECK (target_rau_mau >= 0),
  target_nam         integer     NOT NULL CHECK (target_nam >= 0),
  target_cay_an_trai integer     NOT NULL CHECK (target_cay_an_trai >= 0),
  active             boolean     NOT NULL,
  event_time         timestamptz NOT NULL,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  supersedes_id      uuid        NULL,
  correction_reason  text        NULL,
  actor_ref          uuid        NOT NULL,
  device_id          text        NULL,
  idem_key           text        NOT NULL,
  rule_version       text        NOT NULL,
  prev_hash          bytea       NOT NULL,
  row_hash           bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT survey_commune_stream CHECK (stream_key = 'scommune:' || round_id::text || ':' || commune_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX survey_commune_round ON pf_price.survey_commune (tenant_id, round_id, commune_code, seq);
SELECT shared.apply_ledger('pf_price.survey_commune', 'pf_price.survey_commune');

CREATE TABLE pf_price.survey_officer (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'sofficer:<round_id>:<officer_id>'
  seq               bigint      NOT NULL,
  round_id          uuid        NOT NULL,
  officer_id        uuid        NOT NULL,
  commune_code      text        NOT NULL CHECK (char_length(commune_code) BETWEEN 1 AND 32),
  subject_ref       uuid        NOT NULL,                 -- chủ thể ở kho định danh (họ tên, SĐT — R-04)
  unit_name         text        NOT NULL CHECK (char_length(unit_name) BETWEEN 1 AND 200),
  active            boolean     NOT NULL,
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
  CONSTRAINT survey_officer_stream CHECK (stream_key = 'sofficer:' || round_id::text || ':' || officer_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX survey_officer_round ON pf_price.survey_officer (tenant_id, round_id, officer_id, seq);
SELECT shared.apply_ledger('pf_price.survey_officer', 'pf_price.survey_officer');

CREATE TABLE pf_price.survey_form (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'sform:<form_id>'
  seq               bigint        NOT NULL,
  form_id           uuid          NOT NULL,
  round_id          uuid          NOT NULL,
  import_id         uuid          NOT NULL,
  row_no            integer       NOT NULL CHECK (row_no >= 1),
  commune_code      text          NOT NULL CHECK (char_length(commune_code) BETWEEN 1 AND 32),
  crop              text          NOT NULL CHECK (crop IN ('lua', 'rau_mau', 'nam', 'cay_an_trai')),
  item_code         text          NOT NULL CHECK (item_code ~ '^MH[0-9]{5}$'),
  farmer_ref        uuid          NOT NULL,               -- chủ thể nông hộ ở kho định danh (họ tên, SĐT — R-04)
  has_phone         boolean       NOT NULL,               -- có SĐT → chống trùng theo SĐT
  officer_id        uuid          NULL,                   -- cán bộ điều tra đã khai báo (NULL = không khớp)
  area_ha           numeric(12,4) NOT NULL CHECK (area_ha > 0),
  output_kg         numeric(16,2) NOT NULL CHECK (output_kg > 0),
  sale_price        numeric(14,2) NOT NULL CHECK (sale_price > 0),
  other_income      numeric(16,2) NOT NULL CHECK (other_income >= 0),
  cost_seed         numeric(16,2) NOT NULL CHECK (cost_seed >= 0),
  cost_fertilizer   numeric(16,2) NOT NULL CHECK (cost_fertilizer >= 0),
  cost_pesticide    numeric(16,2) NOT NULL CHECK (cost_pesticide >= 0),
  cost_labor        numeric(16,2) NOT NULL CHECK (cost_labor >= 0),
  cost_machine      numeric(16,2) NOT NULL CHECK (cost_machine >= 0),
  cost_other        numeric(16,2) NOT NULL CHECK (cost_other >= 0),
  status            text          NOT NULL CHECK (status IN ('imported', 'verify', 'verified', 'approved', 'rejected', 'deleted')),
  op                text          NOT NULL CHECK (op IN ('import', 'request_verify', 'record_verify', 'approve', 'reject', 'delete',
                                                         'reopen')),
  note              text          NULL CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 1000),
  verified_price    numeric(14,2) NULL CHECK (verified_price IS NULL OR verified_price > 0),
  unverifiable      boolean       NOT NULL,               -- "Không xác minh được" — Cấp Tỉnh tự quyết
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,
  device_id         text          NULL,
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (op NOT IN ('record_verify', 'reject', 'delete') OR note IS NOT NULL),   -- BR-04, B3 FN-TI-02.12
  CHECK (op <> 'import' OR status = 'imported'),
  CHECK ((status = 'deleted') = (op = 'delete')),
  CONSTRAINT survey_form_stream CHECK (stream_key = 'sform:' || form_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX survey_form_round ON pf_price.survey_form (tenant_id, round_id, form_id, seq);
CREATE INDEX survey_form_farmer ON pf_price.survey_form (tenant_id, round_id, farmer_ref, item_code);
SELECT shared.apply_ledger('pf_price.survey_form', 'pf_price.survey_form');

CREATE TABLE pf_price.survey_import (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'simport:<import_id>'
  seq               bigint      NOT NULL,
  import_id         uuid        NOT NULL,
  round_id          uuid        NOT NULL,
  file_sha256       text        NOT NULL CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
  file_name         text        NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 200),
  rows_total        integer     NOT NULL CHECK (rows_total >= 0),
  created           integer     NOT NULL CHECK (created >= 0),
  skipped           integer     NOT NULL CHECK (skipped >= 0),   -- trùng phiếu, bỏ qua
  warnings          integer     NOT NULL CHECK (warnings >= 0),
  errors            integer     NOT NULL CHECK (errors >= 0),
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
  CHECK (created + skipped + errors <= rows_total),
  CONSTRAINT survey_import_stream CHECK (stream_key = 'simport:' || import_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX survey_import_round ON pf_price.survey_import (tenant_id, round_id);
SELECT shared.apply_ledger('pf_price.survey_import', 'pf_price.survey_import');

CREATE TABLE pf_price.survey_issue (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'sissue:<import_id>:<row_no>'
  seq               bigint      NOT NULL,
  import_id         uuid        NOT NULL,
  row_no            integer     NOT NULL CHECK (row_no >= 1),
  level             text        NOT NULL CHECK (level IN ('error', 'warning')),
  message           text        NOT NULL CHECK (char_length(message) BETWEEN 1 AND 1000),
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
  CONSTRAINT survey_issue_stream CHECK (stream_key = 'sissue:' || import_id::text || ':' || row_no::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX survey_issue_import ON pf_price.survey_issue (tenant_id, import_id, row_no);
SELECT shared.apply_ledger('pf_price.survey_issue', 'pf_price.survey_issue');

CREATE TABLE pf_price.survey_synonym (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'ssyn:<kind>:<raw_key>'
  seq               bigint      NOT NULL,
  kind              text        NOT NULL CHECK (kind IN ('item', 'commune')),
  raw_key           text        NOT NULL CHECK (char_length(raw_key) BETWEEN 1 AND 200),
  target_code       text        NOT NULL CHECK (char_length(target_code) BETWEEN 1 AND 32),
  active            boolean     NOT NULL,
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
  CONSTRAINT survey_synonym_stream CHECK (stream_key = 'ssyn:' || kind || ':' || raw_key)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX survey_synonym_kind ON pf_price.survey_synonym (tenant_id, kind, raw_key, seq);
SELECT shared.apply_ledger('pf_price.survey_synonym', 'pf_price.survey_synonym');

REVOKE ALL ON pf_price.survey_round, pf_price.survey_commune, pf_price.survey_officer, pf_price.survey_form,
  pf_price.survey_import, pf_price.survey_issue, pf_price.survey_synonym FROM PUBLIC;

-- Vai của module: sổ chỉ ghi thêm (R-05, R-20).
GRANT SELECT, INSERT ON pf_price.survey_round, pf_price.survey_commune, pf_price.survey_officer, pf_price.survey_form,
  pf_price.survey_import, pf_price.survey_issue, pf_price.survey_synonym TO r_price;
