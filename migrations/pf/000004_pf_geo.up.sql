-- pf_geo: thửa ruộng có ranh giới theo phiên bản (KN FN-05 US-PLOT-01..04, HTX
-- FN-07 US-PLOT-01..05, GIS FN-05/FN-07) và ranh giới hành chính dùng chung.
--
-- Mẫu (ADR-0003 mức B, mục 8):
--   - plot_boundary: SỔ chỉ ghi thêm, mỗi dòng một phiên bản ranh giới của một
--     thửa (stream_key = 'plot:<ref>', seq = số phiên bản). Sửa ranh giới = phiên
--     bản mới trỏ supersedes_id về phiên bản trước, kèm lý do — không UPDATE hình.
--   - plot_event: SỔ biến động thuộc tính thửa (tạo, gán HTX/nông hộ, ngừng dùng).
--   - plot: bảng chiếu trạng thái hiện tại (R-21), cập nhật cùng giao dịch; có
--     sync_seq cho chiều xuống của đồng bộ ngoại tuyến (docs/dev/sync-contract.md).
--   - boundary_conflict: hàng chờ xử lý chồng lấn/sửa đồng thời (mục 15: giữ cả
--     hai, có người phụ trách + SLA C-8 tạm 72 giờ).
--   - admin_boundary: hình ranh giới tỉnh/xã nhập từ GeoJSON (GIS FN-07 BR-26/27).
--
-- Diện tích (ha) tính trắc địa trong DB: ST_Area(geom::geography) / 10000 trên
-- ellipsoid WGS84, numeric(14,4). Toạ độ chỉ nhận WGS84 (EPSG:4326).
-- KHÔNG có dữ liệu cá nhân ở đây (R-04): nông hộ chỉ là household_ref.

-- PostGIS do Bootstrap tạo sẵn (extension không trusted); câu lệnh này là no-op.
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE SCHEMA pf_geo;
REVOKE ALL ON SCHEMA pf_geo FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_geo TO geo_app;

-- ---------------------------------------------------------------------------
-- 1. Sổ phiên bản ranh giới thửa
-- ---------------------------------------------------------------------------
CREATE TABLE pf_geo.plot_boundary (
  id                uuid                         NOT NULL,     -- UUIDv7; ngoại tuyến do máy sinh (R-11)
  tenant_id         uuid                         NOT NULL,
  stream_key        text                         NOT NULL,     -- 'plot:<plot_ref>'
  seq               bigint                       NOT NULL,     -- = số phiên bản ranh giới
  plot_ref          uuid                         NOT NULL,
  geom              geometry(MultiPolygon, 4326) NOT NULL,
  area_ha           numeric(14,4)                NOT NULL CHECK (area_ha > 0),   -- trắc địa, tính trong DB
  point_count       integer                      NOT NULL CHECK (point_count >= 4),
  gps_accuracy_m    numeric(8,2)                 NULL CHECK (gps_accuracy_m IS NULL OR gps_accuracy_m >= 0),
  source            text                         NOT NULL CHECK (source IN ('kn_web', 'kn_field_app', 'htx_app', 'import_file', 'cadastral')),
  commune_code      text                         NOT NULL,     -- ảnh chụp lúc ghi
  cooperative_ref   uuid                         NULL,
  -- false: bản ghi được giữ nhưng không thành hiện hành (sửa đồng thời ngoại tuyến → xung đột chờ xử lý).
  became_current    boolean                      NOT NULL,
  event_time        timestamptz                  NOT NULL,     -- R-13: lúc vẽ tại hiện trường
  recorded_at       timestamptz                  NOT NULL DEFAULT now(),
  supersedes_id     uuid                         NULL,         -- phiên bản trước bị thay thế
  correction_reason text                         NULL,
  actor_ref         uuid                         NOT NULL,     -- drawn_by
  device_id         text                         NULL,
  idem_key          text                         NOT NULL,
  rule_version      text                         NOT NULL,
  prev_hash         bytea                        NOT NULL,
  row_hash          bytea                        NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX plot_boundary_idem   ON pf_geo.plot_boundary (tenant_id, idem_key);
CREATE INDEX plot_boundary_stream ON pf_geo.plot_boundary (tenant_id, stream_key, seq);
CREATE INDEX plot_boundary_plot   ON pf_geo.plot_boundary (tenant_id, plot_ref, seq);
CREATE INDEX plot_boundary_id     ON pf_geo.plot_boundary (id);

SELECT shared.apply_ledger('pf_geo.plot_boundary', 'pf_geo.plot_boundary');

-- ---------------------------------------------------------------------------
-- 2. Sổ biến động thuộc tính thửa
-- ---------------------------------------------------------------------------
CREATE TABLE pf_geo.plot_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'plot:<plot_ref>'
  seq               bigint      NOT NULL,
  plot_ref          uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('created', 'assigned', 'retired')),
  code              text        NOT NULL,
  name              text        NULL,
  commune_code      text        NOT NULL,
  cooperative_ref   uuid        NULL,
  household_ref     uuid        NULL,
  status            text        NOT NULL CHECK (status IN ('draft', 'active', 'retired')),
  reason            text        NULL,
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
  -- GIS BR-17: ngừng dùng (xóa mềm) bắt buộc có lý do.
  CHECK (action <> 'retired' OR char_length(btrim(reason)) >= 10)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX plot_event_idem   ON pf_geo.plot_event (tenant_id, idem_key);
CREATE INDEX plot_event_stream ON pf_geo.plot_event (tenant_id, stream_key, seq);

SELECT shared.apply_ledger('pf_geo.plot_event', 'pf_geo.plot_event');

-- ---------------------------------------------------------------------------
-- 3. Trạng thái hiện tại của thửa (bảng chiếu, R-21; dựng lại được từ hai sổ, R-22)
-- ---------------------------------------------------------------------------
-- Mã thửa hiển thị: TH0000001… (nhãn, không mang nghĩa địa lý — ADR-0009).
CREATE SEQUENCE pf_geo.plot_code_seq;
-- Con trỏ chiều xuống của đồng bộ: mỗi lần dòng plot đổi lấy giá trị mới.
CREATE SEQUENCE pf_geo.plot_sync_seq;

CREATE TABLE pf_geo.plot (
  tenant_id           uuid                         NOT NULL,
  ref                 uuid                         NOT NULL,
  code                text                         NOT NULL CHECK (length(code) BETWEEN 1 AND 40),
  name                text                         NULL CHECK (name IS NULL OR length(name) BETWEEN 1 AND 200),
  commune_code        text                         NOT NULL,
  cooperative_ref     uuid                         NULL,       -- NULL: KN vẽ trước khi gán HTX ("Nháp - KN vẽ trước")
  household_ref       uuid                         NULL,
  status              text                         NOT NULL CHECK (status IN ('draft', 'active', 'retired')),
  status_reason       text                         NULL,
  current_version     bigint                       NOT NULL CHECK (current_version >= 1),
  current_boundary_id uuid                         NOT NULL,
  geom                geometry(MultiPolygon, 4326) NOT NULL,
  area_ha             numeric(14,4)                NOT NULL CHECK (area_ha > 0),
  source              text                         NOT NULL,
  needs_review        boolean                      NOT NULL DEFAULT false,  -- có xung đột chồng lấn chờ xử lý
  revision            bigint                       NOT NULL DEFAULT 1,      -- tăng mỗi lần đổi (version của sync)
  sync_seq            bigint                       NOT NULL DEFAULT nextval('pf_geo.plot_sync_seq'),
  created_by          uuid                         NOT NULL,
  created_at          timestamptz                  NOT NULL DEFAULT now(),
  updated_at          timestamptz                  NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, ref),
  UNIQUE (tenant_id, code),
  CHECK ((status = 'draft') = (cooperative_ref IS NULL) OR status = 'retired'),
  CHECK (household_ref IS NULL OR cooperative_ref IS NOT NULL)
);
CREATE INDEX plot_geom        ON pf_geo.plot USING gist (geom);
CREATE INDEX plot_commune     ON pf_geo.plot (tenant_id, commune_code);
CREATE INDEX plot_cooperative ON pf_geo.plot (tenant_id, cooperative_ref, ref);
CREATE INDEX plot_sync        ON pf_geo.plot (tenant_id, sync_seq);
SELECT shared.apply_tenant_rls('pf_geo.plot');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_geo.plot
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_geo.plot
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 4. Xung đột ranh giới chờ xử lý (mục 15, C-8)
-- ---------------------------------------------------------------------------
-- Bảng quy trình: open → resolved. Nội dung phát hiện bất biến; chỉ đổi trạng thái.
CREATE TABLE pf_geo.boundary_conflict (
  tenant_id       uuid          NOT NULL,
  id              uuid          NOT NULL,                  -- UUIDv7
  kind            text          NOT NULL CHECK (kind IN ('same_coop_overlap', 'cross_coop_overlap', 'concurrent_edit')),
  plot_ref        uuid          NOT NULL,
  boundary_id     uuid          NOT NULL,                  -- phiên bản gây xung đột (luôn được giữ trong sổ)
  other_plot_refs uuid[]        NOT NULL DEFAULT '{}',
  max_overlap_pct numeric(14,4) NULL CHECK (max_overlap_pct IS NULL OR max_overlap_pct >= 0),
  commune_code    text          NOT NULL,
  owner_ref       uuid          NULL,                      -- tổ chức phụ trách (HTX); NULL = cán bộ KN của xã
  due_at          timestamptz   NOT NULL,                  -- SLA tạm 72 giờ (C-8)
  status          text          NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
  detected_at     timestamptz   NOT NULL,
  resolved_by     uuid          NULL,
  resolved_at     timestamptz   NULL,
  resolution_note text          NULL,
  PRIMARY KEY (tenant_id, id),
  CHECK ((status = 'resolved') = (resolved_by IS NOT NULL AND resolved_at IS NOT NULL)),
  CHECK (status <> 'resolved' OR char_length(btrim(resolution_note)) >= 10),
  CHECK (due_at > detected_at)
);
CREATE INDEX boundary_conflict_plot ON pf_geo.boundary_conflict (tenant_id, plot_ref, status);
CREATE INDEX boundary_conflict_open ON pf_geo.boundary_conflict (tenant_id, commune_code, status, due_at);
SELECT shared.apply_tenant_rls('pf_geo.boundary_conflict');

CREATE FUNCTION pf_geo.fn_boundary_conflict_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status = 'resolved' THEN
    RAISE EXCEPTION 'boundary_conflict %: xung đột đã xử lý là bất biến', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.tenant_id <> OLD.tenant_id OR NEW.id <> OLD.id OR NEW.kind <> OLD.kind OR NEW.plot_ref <> OLD.plot_ref
     OR NEW.boundary_id <> OLD.boundary_id OR NEW.other_plot_refs <> OLD.other_plot_refs
     OR NEW.max_overlap_pct IS DISTINCT FROM OLD.max_overlap_pct OR NEW.commune_code <> OLD.commune_code
     OR NEW.owner_ref IS DISTINCT FROM OLD.owner_ref OR NEW.due_at <> OLD.due_at OR NEW.detected_at <> OLD.detected_at THEN
    RAISE EXCEPTION 'boundary_conflict %: nội dung phát hiện bất biến, chỉ đổi được trạng thái', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_boundary_conflict_guard BEFORE UPDATE ON pf_geo.boundary_conflict
  FOR EACH ROW EXECUTE FUNCTION pf_geo.fn_boundary_conflict_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_geo.boundary_conflict
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_geo.boundary_conflict
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 5. Ranh giới hành chính dùng chung (GIS FN-07 BR-21/26/27)
-- ---------------------------------------------------------------------------
-- Một dòng = một lần nhập hình cho một mã đơn vị. Nhập lại → dòng cũ được đóng
-- (superseded_at), dòng mới là hiện hành. Không xóa; hình đã nhập bất biến.
CREATE TABLE pf_geo.admin_boundary (
  tenant_id     uuid                         NOT NULL,
  id            uuid                         NOT NULL,
  unit_code     text                         NOT NULL CHECK (unit_code ~ '^[0-9A-Z][0-9A-Z.-]{0,39}$'),
  level         text                         NOT NULL CHECK (level IN ('province', 'commune')),
  name          text                         NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  geom          geometry(MultiPolygon, 4326) NOT NULL,
  source        text                         NOT NULL CHECK (length(source) BETWEEN 1 AND 300),
  imported_by   uuid                         NOT NULL,
  imported_at   timestamptz                  NOT NULL DEFAULT now(),
  superseded_at timestamptz                  NULL,
  superseded_by uuid                         NULL,
  PRIMARY KEY (tenant_id, id),
  CHECK ((superseded_at IS NULL) = (superseded_by IS NULL))
);
CREATE UNIQUE INDEX admin_boundary_current ON pf_geo.admin_boundary (tenant_id, unit_code) WHERE superseded_at IS NULL;
CREATE INDEX admin_boundary_geom ON pf_geo.admin_boundary USING gist (geom);
SELECT shared.apply_tenant_rls('pf_geo.admin_boundary');

CREATE FUNCTION pf_geo.fn_admin_boundary_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.superseded_at IS NOT NULL THEN
    RAISE EXCEPTION 'admin_boundary %: phiên bản đã bị thay là bất biến', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.tenant_id <> OLD.tenant_id OR NEW.id <> OLD.id OR NEW.unit_code <> OLD.unit_code OR NEW.level <> OLD.level
     OR NEW.name <> OLD.name OR NOT ST_Equals(NEW.geom, OLD.geom) OR NEW.source <> OLD.source
     OR NEW.imported_by <> OLD.imported_by OR NEW.imported_at <> OLD.imported_at THEN
    RAISE EXCEPTION 'admin_boundary %: chỉ được đóng phiên bản, không được sửa nội dung', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_admin_boundary_guard BEFORE UPDATE ON pf_geo.admin_boundary
  FOR EACH ROW EXECUTE FUNCTION pf_geo.fn_admin_boundary_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_geo.admin_boundary
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_geo.admin_boundary
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 6. Quyền của geo_app (R-05). Sổ: chỉ SELECT, INSERT. Không DELETE ở đâu cả.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_geo.plot_boundary, pf_geo.plot_event TO geo_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_geo.plot_boundary, pf_geo.plot_event FROM geo_app;
GRANT SELECT, INSERT, UPDATE ON pf_geo.plot, pf_geo.boundary_conflict, pf_geo.admin_boundary TO geo_app;
REVOKE DELETE, TRUNCATE ON pf_geo.plot, pf_geo.boundary_conflict, pf_geo.admin_boundary FROM geo_app;
GRANT USAGE ON SEQUENCE pf_geo.plot_code_seq, pf_geo.plot_sync_seq TO geo_app;
