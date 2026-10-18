-- pf/price (`cap/price`, W11.71 — ADR-0049, kiến trúc v4.1 §26, BRD Báo giá nông sản v1.3): KHUNG module năng lực giá.
--
-- Gói này chỉ dựng khung; bảng nghiệp vụ thật do các gói sau thêm vào schema pf_price (mỗi gói một migration riêng):
--   W11.72 xã báo giá + phân công · W11.73 mặt hàng theo thời kỳ (nhóm, gán xã, đề xuất, bộ theo vụ, "thay thế cho") ·
--   W11.74 lịch báo giá, giờ khóa, ngưỡng · W11.75 phiếu báo giá + xác minh · W11.76 bảng giá ngày có phiên bản, công bố
--   (view pf_price_pub.v1_board_daily).
--
-- 1. Schema pf_price (nội bộ) và pf_price_pub (view công bố R-DB-10 — CHƯA có view: view đầu tiên v1_board_daily là của
--    W11.76; schema dựng sẵn để vai r_price_pub, r_metrics, r_agg có đích ổn định).
-- 2. Hai danh mục nền, sổ chỉ ghi thêm (mẫu 8.1, shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo
--    tenant). Mỗi dòng là MỘT phiên bản của một mã (version = seq của chuỗi '<loại>:<mã>'); hiệu lực tính theo valid_from
--    (BRD FN-AD-04.03 BR-02: mọi thay đổi danh mục áp dụng từ ngày báo giá kế tiếp, không ảnh hưởng phiếu đã có). Ngưng
--    = phiên bản mới status 'retired' (không xóa — BR-04). Phép chiếu "đang hiệu lực tại ngày d" ở store (DISTINCT ON).
--      - unit             — đơn vị tính (kg, tạ, tấn, lít, con, trái …), dùng chung cho mặt hàng báo giá và điều tra giá thành.
--      - commodity_group  — nhóm hàng (mặc định 5 nhóm BRD: Lúa gạo, Rau màu, Chăn nuôi, Thủy hải sản, Vật tư nông nghiệp).
--    Mã là chuỗi ổn định do người quản trị đặt (không tái sử dụng — mã đã có chỉ ra phiên bản mới).
-- 3. Không dữ liệu cá nhân (R-04): tên danh mục, mã, mã tham chiếu người ghi. Không cột số thực: giá, % ở các gói sau là
--    numeric (R-CODE-01; mã Go cấm float trong internal/platform/price — agnofloat).
-- 4. Vai: r_price (vai nhóm sinh từ owns_schema — dbadmin; app_platform là thành viên) SELECT, INSERT; r_price_pub USAGE
--    schema công bố (SELECT view do migration của view cấp). k_ref như mọi module pf sinh sau 000092 (R-DB-05).

DO $$
DECLARE
  r text;
BEGIN
  FOREACH r IN ARRAY ARRAY['r_price', 'r_price_pub'] LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r AND NOT rolcanlogin) THEN
      RAISE EXCEPTION 'thiếu vai nhóm % (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028, ADR-0049)', r;
    END IF;
  END LOOP;
END
$$;

CREATE SCHEMA pf_price;
REVOKE ALL ON SCHEMA pf_price FROM PUBLIC;

CREATE SCHEMA pf_price_pub;
REVOKE ALL ON SCHEMA pf_price_pub FROM PUBLIC;

CREATE TABLE pf_price.unit (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,                 -- RB-2
  stream_key        text        NOT NULL,                 -- 'unit:<code>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  code              text        NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]{0,19}$'),
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 50),
  sort_order        integer     NOT NULL CHECK (sort_order BETWEEN 0 AND 100000),
  status            text        NOT NULL CHECK (status IN ('active', 'retired')),
  valid_from        date        NOT NULL,                 -- ngày báo giá đầu tiên phiên bản có hiệu lực (giờ VN)
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
  CONSTRAINT unit_stream CHECK (stream_key = 'unit:' || code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX unit_code ON pf_price.unit (tenant_id, code, valid_from, seq);
SELECT shared.apply_ledger('pf_price.unit', 'pf_price.unit');

CREATE TABLE pf_price.commodity_group (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'group:<code>'
  seq               bigint      NOT NULL,                 -- = số phiên bản
  code              text        NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]{0,39}$'),
  name              text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
  sort_order        integer     NOT NULL CHECK (sort_order BETWEEN 0 AND 100000),
  status            text        NOT NULL CHECK (status IN ('active', 'retired')),
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
  CONSTRAINT commodity_group_stream CHECK (stream_key = 'group:' || code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX commodity_group_code ON pf_price.commodity_group (tenant_id, code, valid_from, seq);
SELECT shared.apply_ledger('pf_price.commodity_group', 'pf_price.commodity_group');

REVOKE ALL ON pf_price.unit, pf_price.commodity_group FROM PUBLIC;

-- Vai của module: sổ chỉ ghi thêm (R-05, R-20).
GRANT USAGE ON SCHEMA pf_price TO r_price;
GRANT SELECT, INSERT ON pf_price.unit, pf_price.commodity_group TO r_price;

-- Schema công bố: chỉ r_price_pub (thành viên: r_metrics, r_agg — bootstrap). View đầu tiên ở W11.76.
GRANT USAGE ON SCHEMA pf_price_pub TO r_price_pub;

-- Module pf mới có r_* (docs/dev/modules.md, W2.3 bước 5): đọc k_ref + biến ngữ cảnh (R-DB-05).
GRANT USAGE ON SCHEMA k_ref TO r_price;
GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) TO r_price;
GRANT SELECT ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope TO r_price;
