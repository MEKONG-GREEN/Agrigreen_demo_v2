-- mkg_engine W8.2 (ADR-0036 §3; kiến trúc v4.1 §27.2 C, §41.4): KHO QUAN SÁT của engine điều phối nội bộ. Module mới
-- mkg/engine (kind domain, vùng mkg, vai nhóm r_engine suy từ registry — ADR-0028): engine (W8.3, Python, chờ MKG v1.3)
-- không giữ dữ liệu nghiệp vụ; đầu vào "quan sát trạng thái ngoài" nằm ở đây, chủ là module mkg phía dữ liệu của engine.
--
--   observation            SỔ chỉ ghi thêm (shared.apply_ledger: chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS tenant,
--                          phân vùng tháng theo recorded_at). Một dòng = một quan sát (kind, entity_key) với source
--                          (api | manual | sensor), as_of (mốc dữ liệu của NGUỒN — với feed là as_of của trang do cổng
--                          trả), max_age (hợp đồng feed), giá trị decimal + đơn vị, uncertainty (nửa độ rộng tuyệt đối,
--                          cùng đơn vị với giá trị, ≥ 0) + uncertainty_basis (mã căn cứ: quantization, k_suppressed_residual,
--                          pending_c9, forecast, missing_value…), payload = bản ghi hợp đồng (đã kiểm ở cổng VÀ ở bộ nạp),
--                          nguồn gốc feed (feed, contract_version, feed_seq, event_id, feed_cursor). Rút (retract của feed,
--                          thu hồi cấp quyền) = dòng retracted, không xóa. Idempotent: idem_key = source|feed|kind|entity|
--                          event_id (áp lại cùng mục feed → bỏ qua; shared.ledger_idem chặn trùng ở mức DB).
--                          Quá hạn (now > as_of hiệu lực + max_age) → bên đọc gắn cờ stale, không bao giờ lẫn vào tươi.
--   observation_feed_state bảng ĐỆM một dòng mỗi feed × tenant MKG: cursor đã áp, as_of + max_age của lần đọc tới đầu
--                          feed (caught_up — as_of hiệu lực của quan sát còn hiệu lực tiến theo nó), revoked_at, lỗi gần
--                          nhất. Như mkg_fieldops.directory_feed_state (mkg 000036, W5.5) nhưng cursor RIÊNG của engine.
--
-- Số 000038 (bản kế hoạch đầu ghi mkg 000026 — đã lỗi thời: 000023…000032 dành theo thứ tự gộp, 000033–000036 đã dùng,
-- 000037 của W5.6). Không dữ liệu cá nhân (R-04): mọi hợp đồng feed nguồn có personal_data = false.
-- Cột thêm vào sổ về sau: NULL, không DEFAULT, đăng ký shared.register_ledger_columns (ADR-0012, quy tắc ADD COLUMN).

CREATE SCHEMA mkg_engine;
REVOKE ALL ON SCHEMA mkg_engine FROM PUBLIC;
GRANT USAGE ON SCHEMA mkg_engine TO r_engine;

CREATE TABLE mkg_engine.observation (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                -- '<source>:<feed>' (api) | '<source>:<kind>' (manual, sensor)
  seq               bigint      NOT NULL,
  idem_key          text        NOT NULL CHECK (char_length(idem_key) BETWEEN 1 AND 400),
  kind              text        NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]{1,47}$'),
  entity_key        text        NOT NULL CHECK (char_length(entity_key) BETWEEN 1 AND 200),
  source            text        NOT NULL CHECK (source IN ('api', 'manual', 'sensor')),
  feed              text        NULL CHECK (feed IS NULL OR feed ~ '^[a-z][a-z0-9_]{2,47}$'),
  contract_version  integer     NULL CHECK (contract_version IS NULL OR contract_version >= 1),
  feed_seq          bigint      NULL CHECK (feed_seq IS NULL OR feed_seq >= 1),
  event_id          uuid        NULL,
  feed_cursor       text        NULL CHECK (feed_cursor IS NULL OR char_length(feed_cursor) BETWEEN 1 AND 200),
  as_of             timestamptz NOT NULL,
  max_age_seconds   integer     NOT NULL CHECK (max_age_seconds > 0),
  retracted         boolean     NOT NULL,
  value             numeric     NULL,
  unit              text        NULL CHECK (unit IS NULL OR unit IN ('ha', 'ratio')),
  uncertainty       numeric     NULL CHECK (uncertainty IS NULL OR uncertainty >= 0),
  uncertainty_basis text[]      NOT NULL,
  payload           jsonb       NOT NULL CHECK (jsonb_typeof(payload) IN ('object', 'null')),
  mapper_version    text        NOT NULL CHECK (char_length(mapper_version) BETWEEN 1 AND 40),
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((value IS NULL) = (unit IS NULL)),
  CHECK (uncertainty IS NULL OR value IS NOT NULL),
  CHECK (NOT retracted OR (value IS NULL AND jsonb_typeof(payload) = 'null')),
  CHECK (source <> 'api' OR (feed IS NOT NULL AND contract_version IS NOT NULL)),
  CHECK (source = 'api' OR (feed IS NULL AND feed_seq IS NULL AND event_id IS NULL AND feed_cursor IS NULL))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX observation_idem   ON mkg_engine.observation (tenant_id, idem_key);
CREATE INDEX observation_latest ON mkg_engine.observation (tenant_id, kind, entity_key, as_of DESC, recorded_at DESC, seq DESC);
CREATE INDEX observation_feed   ON mkg_engine.observation (tenant_id, feed, kind, entity_key) WHERE feed IS NOT NULL;

SELECT shared.apply_ledger('mkg_engine.observation', 'mkg_engine.observation');

CREATE TABLE mkg_engine.observation_feed_state (
  tenant_id         uuid        NOT NULL,
  feed              text        NOT NULL CHECK (feed ~ '^[a-z][a-z0-9_]{2,47}$'),
  feed_cursor       text        NULL CHECK (feed_cursor IS NULL OR char_length(feed_cursor) BETWEEN 1 AND 200),
  contract_version  integer     NULL CHECK (contract_version IS NULL OR contract_version >= 1),
  caught_up         boolean     NOT NULL DEFAULT false,
  as_of             timestamptz NULL,                    -- as_of của trang cuối khi caught_up (cổng trả)
  max_age_seconds   integer     NULL CHECK (max_age_seconds IS NULL OR max_age_seconds > 0),
  revoked_at        timestamptz NULL,
  last_pulled_at    timestamptz NULL,
  last_error        text        NULL CHECK (last_error IS NULL OR char_length(last_error) <= 300),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, feed),
  CHECK (NOT caught_up OR as_of IS NOT NULL),
  CHECK (revoked_at IS NULL OR NOT caught_up)
);
SELECT shared.apply_tenant_rls('mkg_engine.observation_feed_state');

-- Sổ: chỉ SELECT, INSERT (trigger chặn sửa/xóa kể cả khi lỡ cấp). Bảng đệm: không DELETE (đặt lại = UPDATE).
REVOKE ALL ON mkg_engine.observation, mkg_engine.observation_feed_state FROM PUBLIC;
GRANT SELECT, INSERT ON mkg_engine.observation TO r_engine;
GRANT SELECT, INSERT, UPDATE ON mkg_engine.observation_feed_state TO r_engine;
