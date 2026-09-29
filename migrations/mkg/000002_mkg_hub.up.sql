-- mkg_hub: hub, kho, bãi của MKG; lô có N nguồn kèm tỷ lệ; mẻ biến đổi (sấy,
-- nén) ghi thành phần đầu vào tại thời điểm mẻ chạy; dịch chuyển tồn bất biến;
-- bảng chiếu tồn theo lô (docs/design/mkg.md mục 4; mục 16 tài liệu kiến trúc).
--
-- Mức event sourcing B (mục 8.3): hub KHÔNG thuộc nhóm "sổ là nguồn" (chỉ
-- measurement và costing), nhưng mọi sự việc nghiệp vụ vẫn là dòng chỉ ghi thêm
-- có chuỗi băm (RB-1) và bảng chiếu *_current / lot_balance cập nhật trong CÙNG
-- giao dịch (R-21), dựng lại được từ sổ (R-22).
--
--   facility_version  SỔ phiên bản dữ liệu chủ cơ sở (đề xuất). stream 'facility:<id>', seq = số phiên bản.
--   facility_decision SỔ quyết định phê duyệt / từ chối một phiên bản (người duyệt ≠ người đề xuất, R-56).
--   facility_current  bảng chiếu phiên bản đã duyệt mới nhất (dữ liệu chủ hiện hành).
--   lot               SỔ lô: nguồn gốc N nguồn với tỷ lệ Σ = 1 CHÍNH XÁC (kiểm cả trong DB).
--   batch             SỔ mẻ biến đổi: đầu vào/đầu ra, hao hụt = vào − ra ≥ 0.
--   dispatch          SỔ phiếu xuất: phân bổ theo lô, phiên bản quy tắc rủi ro đã dùng.
--   stock_movement    SỔ dịch chuyển tồn theo lô (stream 'lot:<id>'); sửa sai = dòng bù ngược dấu.
--   lot_balance       bảng chiếu tồn còn lại theo lô — LÔ là nguồn sự thật duy nhất của tồn kho.
--
-- Khối lượng kg numeric(14,3), tỷ lệ numeric(9,6), độ ẩm numeric(5,2) (R-10).
-- Tọa độ WGS84 geometry(…, 4326). Không có dữ liệu cá nhân (R-04): HTX, thửa,
-- bên giao nhận chỉ là *_ref.

-- PostGIS do Bootstrap tạo sẵn ở cụm mkg; câu lệnh này là no-op.
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE SCHEMA mkg_hub;
REVOKE ALL ON SCHEMA mkg_hub FROM PUBLIC;
GRANT USAGE ON SCHEMA mkg_hub TO hub_app;

-- Bất biến tỷ lệ nguồn: mảng khác rỗng, mỗi phần tử có kind/ref/ratio, ratio > 0,
-- tối đa 6 chữ số thập phân, tổng ĐÚNG bằng 1 (so sánh numeric, không float).
CREATE FUNCTION mkg_hub.sources_valid(p jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
SET search_path = pg_catalog, pg_temp
AS $$
  -- coalesce: CHECK coi NULL là đạt, nên mọi nhánh NULL phải thành false.
  SELECT coalesce(
         jsonb_typeof(p) = 'array'
     AND jsonb_array_length(p) >= 1
     AND (SELECT bool_and(
                   e ? 'kind' AND e ? 'ref' AND e ? 'ratio'
               AND jsonb_typeof(e->'ratio') = 'string'
               AND CASE WHEN (e->>'ratio') ~ '^[0-9]+(\.[0-9]{1,6})?$'
                        THEN (e->>'ratio')::numeric > 0 ELSE false END)
          FROM jsonb_array_elements(p) e)
     AND (SELECT sum((e->>'ratio')::numeric) FROM jsonb_array_elements(p) e
           WHERE (e->>'ratio') ~ '^[0-9]+(\.[0-9]{1,6})?$') = 1,
         false)
$$;
GRANT EXECUTE ON FUNCTION mkg_hub.sources_valid(jsonb) TO hub_app;

-- ---------------------------------------------------------------------------
-- 1. Dữ liệu chủ cơ sở có phiên bản — không xóa cứng (ngừng dùng = phiên bản status 'inactive')
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_hub.facility_version (
  id                 uuid                         NOT NULL,     -- UUIDv7 (R-11)
  tenant_id          uuid                         NOT NULL,     -- tài khoản MKG (RB-2)
  stream_key         text                         NOT NULL,     -- 'facility:<facility_id>'
  seq                bigint                       NOT NULL,     -- = số phiên bản
  facility_id        uuid                         NOT NULL,
  code               text                         NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9_-]{1,31}$'),
  name               text                         NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  kind               text                         NOT NULL CHECK (kind IN ('hub', 'warehouse', 'yard')),
  parent_facility_id uuid                         NULL,         -- kho/bãi thuộc một hub (cùng schema, không FK vì bảng phân vùng)
  location           geometry(Point, 4326)        NOT NULL,
  boundary           geometry(MultiPolygon, 4326) NULL,
  province_code      text                         NOT NULL CHECK (province_code ~ '^[0-9]{2}$'),
  status             text                         NOT NULL CHECK (status IN ('active', 'inactive')),
  capacity_kg        numeric(14,3)                NOT NULL CHECK (capacity_kg >= 0),
  effective_from     timestamptz                  NOT NULL,
  event_time         timestamptz                  NOT NULL,     -- lúc đề xuất (R-13)
  recorded_at        timestamptz                  NOT NULL DEFAULT now(),
  supersedes_id      uuid                         NULL,         -- phiên bản trước (từ phiên bản 2)
  correction_reason  text                         NULL,         -- lý do thay đổi
  actor_ref          uuid                         NOT NULL,     -- người đề xuất
  device_id          text                         NULL,
  idem_key           text                         NOT NULL,
  rule_version       text                         NOT NULL,
  prev_hash          bytea                        NOT NULL,
  row_hash           bytea                        NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX facility_version_idem     ON mkg_hub.facility_version (tenant_id, idem_key);
CREATE INDEX facility_version_stream   ON mkg_hub.facility_version (tenant_id, stream_key, seq);
CREATE INDEX facility_version_facility ON mkg_hub.facility_version (tenant_id, facility_id, seq);
CREATE INDEX facility_version_id       ON mkg_hub.facility_version (id);
SELECT shared.apply_ledger('mkg_hub.facility_version', 'mkg_hub.facility_version');

CREATE TABLE mkg_hub.facility_decision (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'facility:<facility_id>'
  seq               bigint      NOT NULL,
  facility_id       uuid        NOT NULL,
  version_id        uuid        NOT NULL,
  version_seq       bigint      NOT NULL CHECK (version_seq >= 1),
  decision          text        NOT NULL CHECK (decision IN ('approved', 'rejected')),
  reason            text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
  proposer_ref      uuid        NOT NULL,                 -- chép từ phiên bản để kiểm toán R-56
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người phê duyệt
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (actor_ref <> proposer_ref),                     -- R-56 ở tầng DB (lớp thứ hai sau app/)
  CHECK (decision = 'approved' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX facility_decision_idem    ON mkg_hub.facility_decision (tenant_id, idem_key);
CREATE INDEX facility_decision_stream  ON mkg_hub.facility_decision (tenant_id, stream_key, seq);
CREATE INDEX facility_decision_version ON mkg_hub.facility_decision (tenant_id, version_id);
SELECT shared.apply_ledger('mkg_hub.facility_decision', 'mkg_hub.facility_decision');

CREATE TABLE mkg_hub.facility_current (
  tenant_id          uuid                         NOT NULL,
  facility_id        uuid                         NOT NULL,
  code               text                         NOT NULL,
  name               text                         NOT NULL,
  kind               text                         NOT NULL,
  parent_facility_id uuid                         NULL,
  location           geometry(Point, 4326)        NOT NULL,
  boundary           geometry(MultiPolygon, 4326) NULL,
  province_code      text                         NOT NULL,
  status             text                         NOT NULL,
  capacity_kg        numeric(14,3)                NOT NULL,
  effective_from     timestamptz                  NOT NULL,
  version_id         uuid                         NOT NULL,
  version_seq        bigint                       NOT NULL,
  proposed_by        uuid                         NOT NULL,
  approved_by        uuid                         NOT NULL,
  approved_at        timestamptz                  NOT NULL,
  updated_at         timestamptz                  NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, facility_id),
  UNIQUE (tenant_id, code)
);
SELECT shared.apply_tenant_rls('mkg_hub.facility_current');

-- ---------------------------------------------------------------------------
-- 2. Lô, mẻ biến đổi, phiếu xuất
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_hub.lot (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'facility:<facility_id>'
  seq               bigint        NOT NULL,
  facility_id       uuid          NOT NULL,
  code              text          NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9_-]{1,39}$'),
  product           text          NOT NULL CHECK (product IN ('straw_bale', 'straw_loose', 'dried_straw', 'compressed_bale')),
  origin            text          NOT NULL CHECK (origin IN ('receipt', 'transform')),
  batch_id          uuid          NULL,
  initial_kg        numeric(14,3) NOT NULL CHECK (initial_kg > 0),
  moisture_pct      numeric(5,2)  NULL CHECK (moisture_pct IS NULL OR (moisture_pct >= 0 AND moisture_pct <= 100)),
  -- [{kind, ref, ratio, mass_kg?}] — kind: plot | cooperative | roll_count | supplier | lot
  sources           jsonb         NOT NULL CHECK (mkg_hub.sources_valid(sources)),
  event_time        timestamptz   NOT NULL,               -- lúc nhận / lúc mẻ ra lô
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
  CHECK ((origin = 'transform') = (batch_id IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX lot_idem     ON mkg_hub.lot (tenant_id, idem_key);
CREATE INDEX lot_stream   ON mkg_hub.lot (tenant_id, stream_key, seq);
CREATE INDEX lot_id       ON mkg_hub.lot (id);
CREATE INDEX lot_batch    ON mkg_hub.lot (tenant_id, batch_id) WHERE batch_id IS NOT NULL;
CREATE INDEX lot_sources  ON mkg_hub.lot USING gin (sources jsonb_path_ops);
SELECT shared.apply_ledger('mkg_hub.lot', 'mkg_hub.lot');

CREATE TABLE mkg_hub.batch (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'facility:<facility_id>'
  seq               bigint        NOT NULL,
  facility_id       uuid          NOT NULL,
  kind              text          NOT NULL CHECK (kind IN ('drying', 'compression', 'sorting')),
  -- Thành phần đầu vào TẠI THỜI ĐIỂM MẺ CHẠY: [{lot_id, mass_kg, moisture_pct, sources}]
  inputs            jsonb         NOT NULL CHECK (jsonb_typeof(inputs) = 'array' AND jsonb_array_length(inputs) >= 1),
  outputs           jsonb         NOT NULL CHECK (jsonb_typeof(outputs) = 'array' AND jsonb_array_length(outputs) >= 1),
  input_kg          numeric(14,3) NOT NULL CHECK (input_kg > 0),
  output_kg         numeric(14,3) NOT NULL CHECK (output_kg > 0),
  loss_kg           numeric(14,3) NOT NULL CHECK (loss_kg >= 0),
  event_time        timestamptz   NOT NULL,               -- lúc mẻ chạy
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
  CHECK (loss_kg = input_kg - output_kg)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX batch_idem   ON mkg_hub.batch (tenant_id, idem_key);
CREATE INDEX batch_stream ON mkg_hub.batch (tenant_id, stream_key, seq);
CREATE INDEX batch_id     ON mkg_hub.batch (id);
SELECT shared.apply_ledger('mkg_hub.batch', 'mkg_hub.batch');

CREATE TABLE mkg_hub.dispatch (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'facility:<facility_id>'
  seq               bigint        NOT NULL,
  facility_id       uuid          NOT NULL,
  mass_kg           numeric(14,3) NOT NULL CHECK (mass_kg > 0),
  destination_ref   uuid          NULL,                   -- chuyến / đơn hàng (mkg/logistics, mkg/orders) — UUID thuần
  allocations       jsonb         NOT NULL CHECK (jsonb_typeof(allocations) = 'array' AND jsonb_array_length(allocations) >= 1),
  strategy          text          NOT NULL CHECK (strategy IN ('risk_suggested', 'manual')),
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,
  device_id         text          NULL,
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,               -- phiên bản quy tắc rủi ro xuống cấp đã dùng
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX dispatch_idem   ON mkg_hub.dispatch (tenant_id, idem_key);
CREATE INDEX dispatch_stream ON mkg_hub.dispatch (tenant_id, stream_key, seq);
CREATE INDEX dispatch_id     ON mkg_hub.dispatch (id);
SELECT shared.apply_ledger('mkg_hub.dispatch', 'mkg_hub.dispatch');

-- ---------------------------------------------------------------------------
-- 3. Dịch chuyển tồn — sổ chỉ ghi thêm theo lô
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_hub.stock_movement (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,               -- 'lot:<lot_id>'
  seq               bigint        NOT NULL,
  facility_id       uuid          NOT NULL,
  lot_id            uuid          NOT NULL,
  kind              text          NOT NULL CHECK (kind IN ('receive', 'transform_in', 'transform_out', 'dispatch', 'correction')),
  qty_kg            numeric(14,3) NOT NULL CHECK (qty_kg <> 0),   -- có dấu: + vào lô, − ra khỏi lô
  balance_after_kg  numeric(14,3) NOT NULL CHECK (balance_after_kg >= 0),
  ref_type          text          NULL CHECK (ref_type IS NULL OR ref_type IN ('batch', 'dispatch', 'movement')),
  ref_id            uuid          NULL,
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,                   -- dòng bị bù (chỉ với kind = 'correction')
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
  CHECK ((kind = 'correction') = (supersedes_id IS NOT NULL)),
  CHECK (kind NOT IN ('receive', 'transform_in') OR qty_kg > 0),
  CHECK (kind NOT IN ('transform_out', 'dispatch') OR qty_kg < 0)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX stock_movement_idem       ON mkg_hub.stock_movement (tenant_id, idem_key);
CREATE INDEX stock_movement_stream     ON mkg_hub.stock_movement (tenant_id, stream_key, seq);
CREATE INDEX stock_movement_id         ON mkg_hub.stock_movement (id);
CREATE INDEX stock_movement_facility   ON mkg_hub.stock_movement (tenant_id, facility_id, event_time);
CREATE INDEX stock_movement_supersedes ON mkg_hub.stock_movement (tenant_id, supersedes_id) WHERE supersedes_id IS NOT NULL;
SELECT shared.apply_ledger('mkg_hub.stock_movement', 'mkg_hub.stock_movement');

-- Bảng chiếu tồn theo lô (R-21). Tổng tồn cơ sở = Σ remaining_kg — không có cột
-- "tồn cơ sở" riêng để khỏi lệch với lô (prototype review 24/09/2026, A03/R3).
CREATE TABLE mkg_hub.lot_balance (
  tenant_id     uuid          NOT NULL,
  lot_id        uuid          NOT NULL,
  facility_id   uuid          NOT NULL,
  code          text          NOT NULL,
  product       text          NOT NULL,
  moisture_pct  numeric(5,2)  NULL,
  received_at   timestamptz   NOT NULL,                   -- event_time của lô: tuổi lưu trữ
  initial_kg    numeric(14,3) NOT NULL,
  remaining_kg  numeric(14,3) NOT NULL CHECK (remaining_kg >= 0),
  last_seq      bigint        NOT NULL,
  status        text          NOT NULL CHECK (status IN ('in_stock', 'depleted')),
  updated_at    timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, lot_id),
  CHECK ((status = 'depleted') = (remaining_kg = 0))
);
CREATE INDEX lot_balance_facility ON mkg_hub.lot_balance (tenant_id, facility_id, status);
SELECT shared.apply_tenant_rls('mkg_hub.lot_balance');

GRANT SELECT, INSERT ON mkg_hub.facility_version, mkg_hub.facility_decision, mkg_hub.lot, mkg_hub.batch,
                        mkg_hub.dispatch, mkg_hub.stock_movement TO hub_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_hub.facility_version, mkg_hub.facility_decision, mkg_hub.lot, mkg_hub.batch,
                                   mkg_hub.dispatch, mkg_hub.stock_movement FROM hub_app;
-- Bảng chiếu: được UPDATE (R-21); DELETE chỉ để dựng lại từ sổ (R-22).
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_hub.facility_current, mkg_hub.lot_balance TO hub_app;
