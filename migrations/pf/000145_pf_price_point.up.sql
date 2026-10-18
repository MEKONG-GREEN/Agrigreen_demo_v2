-- pf/price (`cap/price`, W11.72 — ADR-0049 mục 4, BRD Báo giá nông sản v1.3 FN-AD-04.02, FN-AD-04.01): XÃ BÁO GIÁ +
-- PHÂN CÔNG — nhóm vùng, xã báo giá (↔ mã ĐVHC có hiệu lực, ADR-0032), người phụ trách xã theo ngày hiệu lực.
--
-- Cùng mẫu với pf 000138/000141: mọi bảng là sổ chỉ ghi thêm (shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE,
-- RLS theo tenant); mỗi dòng là MỘT phiên bản của một chuỗi (version = seq), hiệu lực theo valid_from (date, giờ VN).
-- Phiên bản có hiệu lực tại ngày d = valid_from lớn nhất ≤ d, cùng ngày lấy seq lớn nhất (phép chiếu ở domain).
--
-- 1. region_group — nhóm vùng (chuỗi 'region:<code>'; mặc định An Giang, Kiên Giang), cùng cột với commodity_group của
--    000138 để dùng chung phép chiếu danh mục nền. Q-053: nhóm vùng là thuộc tính riêng của Báo giá (không dùng khu vực
--    của pf/party).
-- 2. collection_point — xã báo giá (chuỗi 'point:<code>'). Mã do hệ thống sinh 'XBG' + 4 chữ số, không tái sử dụng
--    (BR-01). admin_unit_code = mã ĐVHC xã của danh mục dùng chung (pf_masterdata.admin_unit, đọc qua bảng chiếu
--    k_ref.admin_unit) — kiểm ở ứng dụng: cấp xã, có hiệu lực tại ngày hiệu lực của phiên bản, không gắn với xã báo giá
--    khác đang hoạt động (luồng ngoại lệ, AC-02). Sắp xếp ĐVHC = phiên bản mới cùng mã xã, mã ĐVHC mới (giữ lịch sử).
--    name = tên xã theo ĐVHC tại ngày hiệu lực (ảnh chụp cho báo cáo các ngày đã qua). region_code tham chiếu mã nhóm vùng
--    (kiểm ở ứng dụng theo ngày). Ngưng = phiên bản 'retired' kèm lý do (BR-02: không xóa).
-- 3. point_officer — phân công người phụ trách xã (chuỗi '<role>:<point_code>'): role 'xa' = cán bộ Xã phụ trách chính
--    (một người tại một thời điểm — BR-01 FN-AD-04.01), 'tinh' = cán bộ Cấp Tỉnh được phân công (BR-02). officer_ref NULL
--    = bỏ phân công từ ngày hiệu lực. Lịch sử theo ngày hiệu lực (AC-02: B nhập từ 05/10, phiếu trước đó vẫn của A).
--    point_code tham chiếu collection_point.code (kiểm ở ứng dụng — sổ có phiên bản, không khóa ngoại).
--
-- Không dữ liệu cá nhân (R-04): mã, tên ĐVHC, mã tham chiếu cán bộ (tên/SĐT ở pf/accounts). Không cột số thực (R-CODE-01).
-- Vai r_price SELECT, INSERT (sổ chỉ ghi thêm — R-05, R-20). k_ref.admin_unit: r_price đã SELECT (pf 000138).

CREATE TABLE pf_price.region_group (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,                 -- RB-2
  stream_key        text        NOT NULL,                 -- 'region:<code>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  code              text        NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]{0,39}$'),
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
  sort_order        integer     NOT NULL CHECK (sort_order BETWEEN 0 AND 100000),
  status            text        NOT NULL CHECK (status IN ('active', 'retired')),
  valid_from        date        NOT NULL,
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
  CONSTRAINT region_group_stream CHECK (stream_key = 'region:' || code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX region_group_code ON pf_price.region_group (tenant_id, code, valid_from, seq);
SELECT shared.apply_ledger('pf_price.region_group', 'pf_price.region_group');

CREATE TABLE pf_price.collection_point (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'point:<code>'
  seq               bigint      NOT NULL,
  code              text        NOT NULL CHECK (code ~ '^XBG[0-9]{4}$'),
  admin_unit_code   text        NOT NULL CHECK (admin_unit_code ~ '^[0-9A-Za-z][0-9A-Za-z_.-]{0,19}$'),
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
  region_code       text        NOT NULL CHECK (region_code ~ '^[a-z][a-z0-9_]{0,39}$'),
  status            text        NOT NULL CHECK (status IN ('active', 'retired')),
  valid_from        date        NOT NULL,                 -- ngày báo giá đầu tiên phiên bản có hiệu lực (giờ VN)
  note              text        NULL CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 500),
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
  CHECK (status = 'active' OR note IS NOT NULL),          -- ngưng phải có lý do (FN-AD-04.02 B3)
  CONSTRAINT collection_point_stream CHECK (stream_key = 'point:' || code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX collection_point_code ON pf_price.collection_point (tenant_id, code, valid_from, seq);
CREATE INDEX collection_point_admin_unit ON pf_price.collection_point (tenant_id, admin_unit_code);
SELECT shared.apply_ledger('pf_price.collection_point', 'pf_price.collection_point');

CREATE TABLE pf_price.point_officer (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- '<role>:<point_code>'
  seq               bigint      NOT NULL,
  point_code        text        NOT NULL CHECK (point_code ~ '^XBG[0-9]{4}$'),
  role              text        NOT NULL CHECK (role IN ('xa', 'tinh')),
  officer_ref       uuid        NULL,                     -- NULL = bỏ phân công từ valid_from
  valid_from        date        NOT NULL,
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
  CONSTRAINT point_officer_stream CHECK (stream_key = role || ':' || point_code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX point_officer_point ON pf_price.point_officer (tenant_id, point_code, role, valid_from, seq);
CREATE INDEX point_officer_ref ON pf_price.point_officer (tenant_id, officer_ref);
SELECT shared.apply_ledger('pf_price.point_officer', 'pf_price.point_officer');

REVOKE ALL ON pf_price.region_group, pf_price.collection_point, pf_price.point_officer FROM PUBLIC;

-- Vai của module: sổ chỉ ghi thêm (R-05, R-20).
GRANT SELECT, INSERT ON pf_price.region_group, pf_price.collection_point, pf_price.point_officer TO r_price;
