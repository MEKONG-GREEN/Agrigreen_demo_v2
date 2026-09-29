-- pf_geo G2: ranh giới HTX, cơ sở (kho/Hub/nhà máy) của tenant, sức chứa, mô
-- hình đọc hub MKG qua van công bố, đường bộ và log GPS (docs/dev/g2-conventions.md §2).
--
-- Theo đúng mẫu plot_boundary của 000004 (ADR-0003 mức B, mục 8):
--   - cooperative_boundary: SỔ phiên bản ranh giới HTX (GIS US-BND-02/03/07, BRD FN-04).
--     Sửa = phiên bản mới trỏ supersedes_id + lý do; ngừng dùng/khôi phục (xóa mềm,
--     BR-14) cũng là một dòng sổ, mang lại hình đang có để giữ minh chứng.
--     cooperative_area là bảng chiếu hiện hành (R-21). Điểm pf_party.cooperative.location
--     vẫn là marker/dự phòng, đọc qua port của party — không join xuyên schema (R-06).
--   - facility_event: SỔ biến động cơ sở của tenant (kho bên thứ ba, Hub, nhà máy;
--     GIS US-BND-05/06/08). facility là bảng chiếu. Không xóa cứng: ngừng dùng.
--   - facility_capacity: SỔ ảnh chụp sức chứa (GIS US-WH-01, BRD FN-06, BR-52): nguồn
--     manual | import (Admin nhập) | valve (hub MKG qua van). capacity_latest là bảng
--     chiếu mới nhất theo từng nguồn; số Admin nhập KHÔNG bị lần đồng bộ sau ghi đè —
--     chỉ khi Admin "nhả" (action release, có lý do) thì số của van mới có hiệu lực.
--   - hub_view: mô hình đọc hub MKG nhận qua van (hợp đồng hub_facility v1); chỉ nhận
--     phiên bản mới hơn (idempotent theo (hub_ref, version)).
--   - road_segment: SỔ phiên bản tuyến đường bộ (GIS US-ROAD-01/02/03, BRD FN-08,
--     BR-29/30); road là bảng chiếu. Chiều dài trắc địa tính trong DB.
--   - gps_fix: SỔ log toạ độ (HTX US-GPS-03): chỉ đọc, không sửa/xóa, giữ ≥ 12 tháng
--     (phân vùng theo tháng, không có job dọn).
--
-- Diện tích/chiều dài tính trắc địa trong DB trên ellipsoid WGS84 (geography).
-- Toạ độ chỉ nhận WGS84 (EPSG:4326). Không có dữ liệu cá nhân (R-04): người thao
-- tác chỉ là actor_ref; "địa chỉ" kho/Hub là mã tỉnh/xã + toạ độ — không có cột địa chỉ chữ
-- (cổng R-04 cấm cột address ngoài vault).

-- ---------------------------------------------------------------------------
-- 1. Sổ phiên bản ranh giới HTX
-- ---------------------------------------------------------------------------
CREATE TABLE pf_geo.cooperative_boundary (
  id                uuid                         NOT NULL,     -- UUIDv7
  tenant_id         uuid                         NOT NULL,
  stream_key        text                         NOT NULL,     -- 'cooperative:<cooperative_ref>'
  seq               bigint                       NOT NULL,     -- = số phiên bản
  cooperative_ref   uuid                         NOT NULL,     -- pf_party (UUID thuần, R-06)
  action            text                         NOT NULL CHECK (action IN ('recorded', 'retired', 'restored')),
  geom              geometry(MultiPolygon, 4326) NOT NULL,
  area_ha           numeric(14,4)                NOT NULL CHECK (area_ha > 0),   -- trắc địa, tính trong DB
  point_count       integer                      NOT NULL CHECK (point_count >= 4),
  source            text                         NOT NULL CHECK (source IN ('geo_web', 'htx_app', 'import_file', 'party_sync')),
  commune_code      text                         NOT NULL,     -- đơn vị hành chính của HTX lúc ghi (phạm vi)
  event_time        timestamptz                  NOT NULL,
  recorded_at       timestamptz                  NOT NULL DEFAULT now(),
  supersedes_id     uuid                         NULL,
  correction_reason text                         NULL,
  actor_ref         uuid                         NOT NULL,
  device_id         text                         NULL,
  idem_key          text                         NOT NULL,
  rule_version      text                         NOT NULL,
  prev_hash         bytea                        NOT NULL,
  row_hash          bytea                        NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR char_length(btrim(correction_reason)) >= 10),
  -- ngừng dùng/khôi phục luôn trỏ phiên bản đang thay, kèm lý do (BR-14)
  CHECK (action = 'recorded' OR supersedes_id IS NOT NULL)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX cooperative_boundary_idem   ON pf_geo.cooperative_boundary (tenant_id, idem_key);
CREATE INDEX cooperative_boundary_stream ON pf_geo.cooperative_boundary (tenant_id, stream_key, seq);
CREATE INDEX cooperative_boundary_coop   ON pf_geo.cooperative_boundary (tenant_id, cooperative_ref, seq);
CREATE INDEX cooperative_boundary_id     ON pf_geo.cooperative_boundary (id);

SELECT shared.apply_ledger('pf_geo.cooperative_boundary', 'pf_geo.cooperative_boundary');

-- Bảng chiếu ranh giới HTX hiện hành (R-21; dựng lại được từ sổ, R-22).
CREATE TABLE pf_geo.cooperative_area (
  tenant_id           uuid                         NOT NULL,
  cooperative_ref     uuid                         NOT NULL,
  commune_code        text                         NOT NULL,
  status              text                         NOT NULL CHECK (status IN ('active', 'retired')),
  status_reason       text                         NULL,
  current_version     bigint                       NOT NULL CHECK (current_version >= 1),
  current_boundary_id uuid                         NOT NULL,
  geom                geometry(MultiPolygon, 4326) NOT NULL,
  area_ha             numeric(14,4)                NOT NULL CHECK (area_ha > 0),
  source              text                         NOT NULL,
  revision            bigint                       NOT NULL DEFAULT 1,
  created_by          uuid                         NOT NULL,
  created_at          timestamptz                  NOT NULL DEFAULT now(),
  updated_at          timestamptz                  NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, cooperative_ref)
);
CREATE INDEX cooperative_area_geom    ON pf_geo.cooperative_area USING gist (geom);
CREATE INDEX cooperative_area_commune ON pf_geo.cooperative_area (tenant_id, commune_code);
SELECT shared.apply_tenant_rls('pf_geo.cooperative_area');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_geo.cooperative_area
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_geo.cooperative_area
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 2. Sổ biến động cơ sở (kho / Hub / nhà máy) của tenant
-- ---------------------------------------------------------------------------
CREATE TABLE pf_geo.facility_event (
  id                uuid                         NOT NULL,
  tenant_id         uuid                         NOT NULL,
  stream_key        text                         NOT NULL,     -- 'facility:<facility_ref>'
  seq               bigint                       NOT NULL,
  facility_ref      uuid                         NOT NULL,
  action            text                         NOT NULL CHECK (action IN ('created', 'updated', 'retired', 'restored')),
  kind              text                         NOT NULL CHECK (kind IN ('kho', 'hub', 'nha_may')),
  code              text                         NOT NULL CHECK (code ~ '^[0-9A-Za-z][0-9A-Za-z._-]{0,39}$'),
  name              text                         NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
  point             geometry(Point, 4326)        NOT NULL,     -- marker
  geom              geometry(MultiPolygon, 4326) NULL,         -- ranh giới khuôn viên (tùy chọn)
  area_ha           numeric(14,4)                NULL CHECK (area_ha IS NULL OR area_ha > 0),
  province_code     text                         NOT NULL,     -- địa chỉ = đơn vị hành chính (không lưu địa chỉ chữ, R-04)
  commune_code      text                         NOT NULL,
  capacity          numeric(14,3)                NOT NULL CHECK (capacity > 0),   -- sức chứa danh định, theo unit
  unit              text                         NOT NULL CHECK (unit IN ('tan', 'm3', 'ke')),
  status            text                         NOT NULL CHECK (status IN ('active', 'retired')),
  event_time        timestamptz                  NOT NULL,
  recorded_at       timestamptz                  NOT NULL DEFAULT now(),
  supersedes_id     uuid                         NULL,         -- dòng sổ trước của cơ sở
  correction_reason text                         NULL,
  actor_ref         uuid                         NOT NULL,
  device_id         text                         NULL,
  idem_key          text                         NOT NULL,
  rule_version      text                         NOT NULL,
  prev_hash         bytea                        NOT NULL,
  row_hash          bytea                        NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((geom IS NULL) = (area_ha IS NULL)),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR char_length(btrim(correction_reason)) >= 10),
  CHECK ((action = 'created') = (supersedes_id IS NULL)),
  CHECK ((action = 'retired') = (status = 'retired'))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX facility_event_idem     ON pf_geo.facility_event (tenant_id, idem_key);
CREATE INDEX facility_event_stream   ON pf_geo.facility_event (tenant_id, stream_key, seq);
CREATE INDEX facility_event_facility ON pf_geo.facility_event (tenant_id, facility_ref, seq);

SELECT shared.apply_ledger('pf_geo.facility_event', 'pf_geo.facility_event');

CREATE TABLE pf_geo.facility (
  tenant_id        uuid                         NOT NULL,
  ref              uuid                         NOT NULL,
  kind             text                         NOT NULL CHECK (kind IN ('kho', 'hub', 'nha_may')),
  code             text                         NOT NULL,
  name             text                         NOT NULL,
  point            geometry(Point, 4326)        NOT NULL,
  geom             geometry(MultiPolygon, 4326) NULL,
  area_ha          numeric(14,4)                NULL,
  province_code    text                         NOT NULL,
  commune_code     text                         NOT NULL,
  capacity         numeric(14,3)                NOT NULL CHECK (capacity > 0),
  unit             text                         NOT NULL,
  status           text                         NOT NULL CHECK (status IN ('active', 'retired')),
  status_reason    text                         NULL,
  current_version  bigint                       NOT NULL CHECK (current_version >= 1),
  current_event_id uuid                         NOT NULL,
  revision         bigint                       NOT NULL DEFAULT 1,
  created_by       uuid                         NOT NULL,
  created_at       timestamptz                  NOT NULL DEFAULT now(),
  updated_at       timestamptz                  NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, ref),
  UNIQUE (tenant_id, code)
);
CREATE INDEX facility_point    ON pf_geo.facility USING gist (point);
CREATE INDEX facility_province ON pf_geo.facility (tenant_id, province_code);
SELECT shared.apply_tenant_rls('pf_geo.facility');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_geo.facility
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_geo.facility
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 3. Mô hình đọc hub MKG nhận qua van công bố (hub_facility v1)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_geo.hub_view (
  tenant_id     uuid                         NOT NULL,
  hub_ref       uuid                         NOT NULL,     -- định danh hub bên mkg/hub
  code          text                         NOT NULL CHECK (char_length(code) BETWEEN 1 AND 40),
  name          text                         NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
  kind          text                         NOT NULL CHECK (kind IN ('kho', 'hub', 'nha_may')),
  point         geometry(Point, 4326)        NOT NULL,
  geom          geometry(MultiPolygon, 4326) NULL,
  province_code text                         NULL,
  commune_code  text                         NULL,
  capacity      numeric(14,3)                NULL CHECK (capacity IS NULL OR capacity > 0),
  unit          text                         NOT NULL DEFAULT 'tan' CHECK (unit IN ('tan', 'm3', 'ke')),
  status        text                         NOT NULL CHECK (status IN ('active', 'retired')),
  version       bigint                       NOT NULL CHECK (version >= 1),   -- phiên bản công bố
  published_at  timestamptz                  NOT NULL,
  received_at   timestamptz                  NOT NULL DEFAULT now(),
  updated_at    timestamptz                  NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, hub_ref)
);
CREATE INDEX hub_view_point ON pf_geo.hub_view USING gist (point);
SELECT shared.apply_tenant_rls('pf_geo.hub_view');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_geo.hub_view
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_geo.hub_view
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 4. Sổ ảnh chụp sức chứa (cơ sở của tenant và hub MKG) — BR-52
-- ---------------------------------------------------------------------------
CREATE TABLE pf_geo.facility_capacity (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,     -- 'capacity:<subject_kind>:<subject_ref>'
  seq               bigint        NOT NULL,
  subject_kind      text          NOT NULL CHECK (subject_kind IN ('facility', 'hub')),
  subject_ref       uuid          NOT NULL,     -- facility.ref hoặc hub_view.hub_ref
  action            text          NOT NULL CHECK (action IN ('snapshot', 'release')),
  source            text          NOT NULL CHECK (source IN ('manual', 'valve', 'import')),
  as_of             timestamptz   NOT NULL,     -- thời điểm số liệu
  capacity          numeric(14,3) NULL CHECK (capacity IS NULL OR capacity > 0),
  used              numeric(14,3) NULL CHECK (used IS NULL OR used >= 0),
  unit              text          NULL CHECK (unit IS NULL OR unit IN ('tan', 'm3', 'ke')),
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
  CHECK (correction_reason IS NULL OR char_length(btrim(correction_reason)) >= 10),
  CHECK (action <> 'snapshot' OR (capacity IS NOT NULL AND used IS NOT NULL AND unit IS NOT NULL)),
  -- nhả số Admin nhập (xác nhận áp dụng số đồng bộ, BR-52): chỉ nguồn Admin, bắt buộc lý do
  CHECK (action <> 'release' OR (source IN ('manual', 'import') AND correction_reason IS NOT NULL)),
  CHECK (source <> 'valve' OR subject_kind = 'hub')
) PARTITION BY RANGE (recorded_at);

CREATE INDEX facility_capacity_idem    ON pf_geo.facility_capacity (tenant_id, idem_key);
CREATE INDEX facility_capacity_stream  ON pf_geo.facility_capacity (tenant_id, stream_key, seq);
CREATE INDEX facility_capacity_subject ON pf_geo.facility_capacity (tenant_id, subject_kind, subject_ref, as_of);

SELECT shared.apply_ledger('pf_geo.facility_capacity', 'pf_geo.facility_capacity');

-- Bảng chiếu: ảnh chụp mới nhất (theo as_of) của từng nguồn cho từng đối tượng.
CREATE TABLE pf_geo.capacity_latest (
  tenant_id    uuid          NOT NULL,
  subject_kind text          NOT NULL CHECK (subject_kind IN ('facility', 'hub')),
  subject_ref  uuid          NOT NULL,
  source       text          NOT NULL CHECK (source IN ('manual', 'valve', 'import')),
  snapshot_id  uuid          NOT NULL,
  as_of        timestamptz   NOT NULL,
  capacity     numeric(14,3) NOT NULL CHECK (capacity > 0),
  used         numeric(14,3) NOT NULL CHECK (used >= 0),
  unit         text          NOT NULL,
  released     boolean       NOT NULL DEFAULT false,   -- số Admin đã được nhả (BR-52)
  released_at  timestamptz   NULL,
  updated_at   timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, subject_kind, subject_ref, source),
  CHECK (released = (released_at IS NOT NULL)),
  CHECK (NOT released OR source IN ('manual', 'import'))
);
SELECT shared.apply_tenant_rls('pf_geo.capacity_latest');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_geo.capacity_latest
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_geo.capacity_latest
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 5. Sổ phiên bản tuyến đường bộ
-- ---------------------------------------------------------------------------
CREATE TABLE pf_geo.road_segment (
  id                uuid                       NOT NULL,
  tenant_id         uuid                       NOT NULL,
  stream_key        text                       NOT NULL,     -- 'road:<road_ref>'
  seq               bigint                     NOT NULL,
  road_ref          uuid                       NOT NULL,
  action            text                       NOT NULL CHECK (action IN ('recorded', 'retired', 'restored')),
  name              text                       NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
  road_class        text                       NOT NULL CHECK (road_class IN ('quoc_lo', 'tinh_lo', 'huyen_lo', 'truc_xa', 'bo_ruong')),
  max_load_tons     numeric(10,3)              NOT NULL CHECK (max_load_tons > 0),   -- BR-29
  geom              geometry(LineString, 4326) NOT NULL,
  length_m          numeric(14,2)              NOT NULL CHECK (length_m > 0),        -- trắc địa, tính trong DB
  point_count       integer                    NOT NULL CHECK (point_count >= 2),
  province_code     text                       NOT NULL,
  event_time        timestamptz                NOT NULL,
  recorded_at       timestamptz                NOT NULL DEFAULT now(),
  supersedes_id     uuid                       NULL,
  correction_reason text                       NULL,
  actor_ref         uuid                       NOT NULL,
  device_id         text                       NULL,
  idem_key          text                       NOT NULL,
  rule_version      text                       NOT NULL,
  prev_hash         bytea                      NOT NULL,
  row_hash          bytea                      NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR char_length(btrim(correction_reason)) >= 10),
  CHECK (action = 'recorded' OR supersedes_id IS NOT NULL)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX road_segment_idem   ON pf_geo.road_segment (tenant_id, idem_key);
CREATE INDEX road_segment_stream ON pf_geo.road_segment (tenant_id, stream_key, seq);
CREATE INDEX road_segment_road   ON pf_geo.road_segment (tenant_id, road_ref, seq);

SELECT shared.apply_ledger('pf_geo.road_segment', 'pf_geo.road_segment');

CREATE TABLE pf_geo.road (
  tenant_id          uuid                       NOT NULL,
  ref                uuid                       NOT NULL,
  name               text                       NOT NULL,
  road_class         text                       NOT NULL,
  max_load_tons      numeric(10,3)              NOT NULL CHECK (max_load_tons > 0),
  geom               geometry(LineString, 4326) NOT NULL,
  length_m           numeric(14,2)              NOT NULL CHECK (length_m > 0),
  province_code      text                       NOT NULL,
  status             text                       NOT NULL CHECK (status IN ('active', 'retired')),
  status_reason      text                       NULL,
  current_version    bigint                     NOT NULL CHECK (current_version >= 1),
  current_segment_id uuid                       NOT NULL,
  revision           bigint                     NOT NULL DEFAULT 1,
  created_by         uuid                       NOT NULL,
  created_at         timestamptz                NOT NULL DEFAULT now(),
  updated_at         timestamptz                NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, ref)
);
CREATE INDEX road_geom ON pf_geo.road USING gist (geom);
CREATE INDEX road_load ON pf_geo.road (tenant_id, max_load_tons);
SELECT shared.apply_tenant_rls('pf_geo.road');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_geo.road
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_geo.road
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 6. Sổ log toạ độ GPS (HTX US-GPS-03) — chỉ đọc, giữ ≥ 12 tháng, không xóa
-- ---------------------------------------------------------------------------
CREATE TABLE pf_geo.gps_fix (
  id                  uuid                  NOT NULL,     -- UUIDv7 do máy sinh lúc lấy toạ độ (gửi lại an toàn)
  tenant_id           uuid                  NOT NULL,
  stream_key          text                  NOT NULL,     -- 'gps:<actor_ref>'
  seq                 bigint                NOT NULL,
  actor_ref           uuid                  NOT NULL,
  device_id           text                  NULL,
  purpose             text                  NOT NULL CHECK (purpose IN ('season_open', 'farm_log', 'harvest', 'support', 'plot')),
  target_ref          uuid                  NULL,         -- bản ghi nghiệp vụ (vụ, nhật ký, yêu cầu hỗ trợ...) của module gọi
  plot_ref            uuid                  NULL,
  commune_code        text                  NULL,         -- phạm vi đọc; NULL chỉ quản trị toàn quốc thấy
  cooperative_ref     uuid                  NULL,
  point               geometry(Point, 4326) NULL,
  accuracy_m          numeric(8,2)          NULL CHECK (accuracy_m IS NULL OR accuracy_m >= 0),
  source              text                  NOT NULL CHECK (source IN ('device', 'photo_exif', 'manual_pick')),
  distance_to_plot_m  numeric(12,1)         NULL CHECK (distance_to_plot_m IS NULL OR distance_to_plot_m >= 0),
  event_time          timestamptz           NOT NULL,     -- lúc lấy toạ độ
  recorded_at         timestamptz           NOT NULL DEFAULT now(),
  supersedes_id       uuid                  NULL CHECK (supersedes_id IS NULL),       -- log không có bản sửa (AC-1)
  correction_reason   text                  NULL CHECK (correction_reason IS NULL),
  idem_key            text                  NOT NULL,
  rule_version        text                  NOT NULL,
  prev_hash           bytea                 NOT NULL,
  row_hash            bytea                 NOT NULL,
  PRIMARY KEY (id, recorded_at),
  -- chọn lô thủ công khi không có GPS (HTX US-GPS-02) thì không có điểm nhưng phải có lô
  CHECK (point IS NOT NULL OR (source = 'manual_pick' AND plot_ref IS NOT NULL))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX gps_fix_idem    ON pf_geo.gps_fix (tenant_id, idem_key);
CREATE INDEX gps_fix_stream  ON pf_geo.gps_fix (tenant_id, stream_key, seq);
CREATE INDEX gps_fix_id      ON pf_geo.gps_fix (tenant_id, id);
CREATE INDEX gps_fix_target  ON pf_geo.gps_fix (tenant_id, target_ref);
CREATE INDEX gps_fix_commune ON pf_geo.gps_fix (tenant_id, commune_code, id);

SELECT shared.apply_ledger('pf_geo.gps_fix', 'pf_geo.gps_fix');

-- ---------------------------------------------------------------------------
-- 7. Quyền của geo_app (R-05). Sổ: chỉ SELECT, INSERT. Không DELETE ở đâu cả.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_geo.cooperative_boundary, pf_geo.facility_event, pf_geo.facility_capacity,
  pf_geo.road_segment, pf_geo.gps_fix TO geo_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_geo.cooperative_boundary, pf_geo.facility_event, pf_geo.facility_capacity,
  pf_geo.road_segment, pf_geo.gps_fix FROM geo_app;
GRANT SELECT, INSERT, UPDATE ON pf_geo.cooperative_area, pf_geo.facility, pf_geo.hub_view, pf_geo.capacity_latest,
  pf_geo.road TO geo_app;
REVOKE DELETE, TRUNCATE ON pf_geo.cooperative_area, pf_geo.facility, pf_geo.hub_view, pf_geo.capacity_latest,
  pf_geo.road FROM geo_app;
