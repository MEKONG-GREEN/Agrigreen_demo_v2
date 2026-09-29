-- pf_waterway: mạng đường thủy CÓ PHIÊN BẢN (mục 16; GIS FN-09, BR-34..40,
-- US-WATER-01/02/03).
--
--   1. segment_revision    sổ đoạn sông/kênh (nháp | đã xác nhận | ngừng dùng); sửa = bản ghi bù
--   2. structure_revision  sổ công trình vượt sông (cầu, cống, âu thuyền, đường dây điện)
--   3. build_job           job dựng mạng (mẫu bất đồng bộ mục 12, R-52)
--   4. network_version     phiên bản mạng: built → published → superseded; MỘT bản phát hành/tenant
--   5. vertex, edge        đồ thị của từng phiên bản cho pgRouting (id bigint), chỉ ghi thêm
--   6. version_segment     tập đoạn đã vào phiên bản kèm tải trọng suy ra (lớp bản đồ US-WATER-01)
--   7. build_finding       phát hiện khi dựng: giao cắt, nối chữ T, khe hở, đầu treo, tuyến cô lập
--
-- Tuyến NHÁP không bao giờ vào đồ thị: bộ dựng chỉ đọc bản hiện hành có
-- status = 'confirmed' tại mốc as_of của job. Phiên bản cũ giữ nguyên (không
-- sửa, không xóa) để định tuyến ghim phiên bản vẫn chạy sau khi phát hành bản mới.
-- Topology dựng trong Go (pgRouting 4 đã bỏ pgr_createTopology); CSDL chỉ dùng pgr_dijkstra.

CREATE SCHEMA pf_waterway;
REVOKE ALL ON SCHEMA pf_waterway FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_waterway TO waterway_app;

-- ---------------------------------------------------------------------------
-- 1. Sổ đoạn sông/kênh
-- ---------------------------------------------------------------------------
-- length_m do CSDL tính (geodesic) — không bao giờ nhận từ client (US-WATER-02 AC-1,
-- FN-20 BR-03); ràng buộc CHECK bảo đảm điều đó ở lớp DB.
-- max_load_tons là tải trọng KHAI BÁO (nếu cần); tải trọng dùng để định tuyến
-- được SUY RA khi dựng mạng từ rộng/sâu/công trình theo quy tắc phương tiện.
CREATE TABLE pf_waterway.segment_revision (
  id                uuid        NOT NULL,               -- UUIDv7 của bản ghi
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,               -- 'segment:<segment_ref>'
  seq               bigint      NOT NULL,
  segment_ref       uuid        NOT NULL,               -- định danh ổn định của đoạn qua mọi bản sửa
  revision          integer     NOT NULL CHECK (revision > 0),
  name              text        NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
  waterway_class    text        NOT NULL CHECK (waterway_class IN ('kenh_chinh', 'noi_dong', 'muong', 'song')),
  status            text        NOT NULL CHECK (status IN ('draft', 'confirmed', 'retired')),
  geom              geometry(LineString, 4326) NOT NULL CHECK (ST_NPoints(geom) >= 2 AND ST_IsValid(geom)),
  length_m          numeric(14,2) NOT NULL,
  width_m           numeric(8,2)  NULL CHECK (width_m IS NULL OR width_m > 0),
  depth_m           numeric(8,2)  NULL CHECK (depth_m IS NULL OR depth_m > 0),   -- độ sâu khống chế mùa khô
  max_load_tons     numeric(10,2) NULL CHECK (max_load_tons IS NULL OR max_load_tons > 0),
  data_source       text        NOT NULL CHECK (data_source IN ('digitised', 'official', 'survey', 'osm')),
  note              text        NULL CHECK (note IS NULL OR length(note) <= 1000),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,                   -- bản sửa trước (bản ghi bù, RB-1)
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,               -- 'segment:<ref>:<revision>' — một bản cho mỗi số sửa
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK ((supersedes_id IS NULL) = (revision = 1)),
  CHECK (correction_reason IS NULL OR length(btrim(correction_reason)) >= 5),
  CHECK (length_m = round(ST_Length(geom::geography)::numeric, 2))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX segment_revision_ref    ON pf_waterway.segment_revision (tenant_id, segment_ref, revision);
CREATE INDEX segment_revision_stream ON pf_waterway.segment_revision (tenant_id, stream_key, seq);
CREATE INDEX segment_revision_idem   ON pf_waterway.segment_revision (tenant_id, idem_key);
CREATE INDEX segment_revision_geom   ON pf_waterway.segment_revision USING gist (geom);
SELECT shared.apply_ledger('pf_waterway.segment_revision', 'pf_waterway.segment_revision');

-- ---------------------------------------------------------------------------
-- 2. Sổ công trình vượt sông
-- ---------------------------------------------------------------------------
-- Công trình gắn vào một đoạn; khi dựng mạng nó ràng buộc CẠNH gần nhất của đoạn
-- đó (tĩnh không cao/rộng, độ sâu tại công trình). Vị trí phải nằm trên tuyến
-- (kiểm ở app/ theo tham số waterway.structure_max_offset_m).
CREATE TABLE pf_waterway.structure_revision (
  id                 uuid        NOT NULL,
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,              -- 'structure:<structure_ref>'
  seq                bigint      NOT NULL,
  structure_ref      uuid        NOT NULL,
  revision           integer     NOT NULL CHECK (revision > 0),
  kind               text        NOT NULL CHECK (kind IN ('cau', 'cong', 'au_thuyen', 'duong_day_dien')),
  name               text        NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
  status             text        NOT NULL CHECK (status IN ('active', 'retired')),
  segment_ref        uuid        NOT NULL,              -- đoạn mang công trình (cùng module)
  geom               geometry(Point, 4326) NOT NULL,
  clearance_height_m numeric(8,2) NULL CHECK (clearance_height_m IS NULL OR clearance_height_m > 0),
  clearance_width_m  numeric(8,2) NULL CHECK (clearance_width_m IS NULL OR clearance_width_m > 0),
  depth_m            numeric(8,2) NULL CHECK (depth_m IS NULL OR depth_m > 0),
  survey_date        date        NULL,
  data_source        text        NOT NULL CHECK (data_source IN ('digitised', 'official', 'survey', 'osm')),
  note               text        NULL CHECK (note IS NULL OR length(note) <= 1000),
  event_time         timestamptz NOT NULL,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  supersedes_id      uuid        NULL,
  correction_reason  text        NULL,
  actor_ref          uuid        NOT NULL,
  device_id          text        NULL,
  idem_key           text        NOT NULL,              -- 'structure:<ref>:<revision>'
  rule_version       text        NOT NULL,
  prev_hash          bytea       NOT NULL,
  row_hash           bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK ((supersedes_id IS NULL) = (revision = 1)),
  CHECK (correction_reason IS NULL OR length(btrim(correction_reason)) >= 5)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX structure_revision_ref     ON pf_waterway.structure_revision (tenant_id, structure_ref, revision);
CREATE INDEX structure_revision_segment ON pf_waterway.structure_revision (tenant_id, segment_ref);
CREATE INDEX structure_revision_stream  ON pf_waterway.structure_revision (tenant_id, stream_key, seq);
CREATE INDEX structure_revision_idem    ON pf_waterway.structure_revision (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_waterway.structure_revision', 'pf_waterway.structure_revision');

-- ---------------------------------------------------------------------------
-- 3. Job dựng mạng (mục 12). Bảng vận hành: được UPDATE cột trạng thái.
-- ---------------------------------------------------------------------------
-- params là ảnh chụp tham số dựng (ngưỡng, biên an toàn, lớp phương tiện) tại lúc
-- yêu cầu: worker không cần đọc pf/masterdata và dựng lại được đúng kết quả.
CREATE TABLE pf_waterway.build_job (
  id                 uuid        PRIMARY KEY,           -- UUIDv7 = build_id
  tenant_id          uuid        NOT NULL,
  status             text        NOT NULL CHECK (status IN ('queued', 'completed', 'failed')),
  params             jsonb       NOT NULL CHECK (jsonb_typeof(params) = 'object'),
  requested_by       uuid        NOT NULL,
  requested_at       timestamptz NOT NULL DEFAULT now(), -- mốc as_of của sổ
  idem_key           text        NOT NULL,
  network_version_id uuid        NULL,
  reused             boolean     NULL,                  -- true: tập đoạn trùng phiên bản đã dựng, không tạo bản mới
  error_code         text        NULL,
  completed_at       timestamptz NULL,
  attempts           integer     NOT NULL DEFAULT 0,
  UNIQUE (tenant_id, idem_key),
  CHECK ((status = 'completed') = (network_version_id IS NOT NULL AND completed_at IS NOT NULL))
);
SELECT shared.apply_tenant_rls('pf_waterway.build_job');

CREATE FUNCTION pf_waterway.fn_build_job_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status IN ('completed', 'failed') THEN
    RAISE EXCEPTION 'build_job %: job đã % là bất biến', OLD.id, OLD.status
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id OR NEW.params <> OLD.params
     OR NEW.requested_by <> OLD.requested_by OR NEW.requested_at <> OLD.requested_at OR NEW.idem_key <> OLD.idem_key THEN
    RAISE EXCEPTION 'build_job %: yêu cầu dựng bất biến, chỉ đổi được trạng thái', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_build_job_guard BEFORE UPDATE ON pf_waterway.build_job
  FOR EACH ROW EXECUTE FUNCTION pf_waterway.fn_build_job_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_waterway.build_job
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_waterway.build_job
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 4. Phiên bản mạng
-- ---------------------------------------------------------------------------
-- Nội dung bất biến; chỉ đổi trạng thái built → published → superseded.
-- R-56 ở lớp DB: người phát hành không bao giờ là người dựng.
CREATE TABLE pf_waterway.network_version (
  id                  uuid        PRIMARY KEY,
  tenant_id           uuid        NOT NULL,
  version_no          integer     NOT NULL CHECK (version_no > 0),
  status              text        NOT NULL CHECK (status IN ('built', 'published', 'superseded')),
  segment_set_sha256  text        NOT NULL CHECK (segment_set_sha256 ~ '^[0-9a-f]{64}$'),
  graph_sha256        text        NOT NULL CHECK (graph_sha256 ~ '^[0-9a-f]{64}$'),
  params              jsonb       NOT NULL CHECK (jsonb_typeof(params) = 'object'), -- ngưỡng và quy tắc phương tiện đã dùng
  rule_version        text        NOT NULL,
  as_of               timestamptz NOT NULL,
  segment_count       integer     NOT NULL CHECK (segment_count >= 0),
  structure_count     integer     NOT NULL CHECK (structure_count >= 0),
  vertex_count        integer     NOT NULL CHECK (vertex_count >= 0),
  edge_count          integer     NOT NULL CHECK (edge_count >= 0),
  component_count     integer     NOT NULL CHECK (component_count >= 0),
  finding_count       integer     NOT NULL CHECK (finding_count >= 0),
  build_job_id        uuid        NOT NULL,
  built_by            uuid        NOT NULL,
  built_at            timestamptz NOT NULL DEFAULT now(),
  published_by        uuid        NULL,
  published_at        timestamptz NULL,
  effective_from      timestamptz NULL,
  superseded_at       timestamptz NULL,
  superseded_by       uuid        NULL,                 -- phiên bản phát hành thay thế
  UNIQUE (tenant_id, version_no),
  CHECK (published_by IS NULL OR published_by <> built_by),
  CHECK ((status IN ('published', 'superseded')) = (published_by IS NOT NULL AND published_at IS NOT NULL AND effective_from IS NOT NULL)),
  CHECK ((status = 'superseded') = (superseded_at IS NOT NULL AND superseded_by IS NOT NULL))
);
-- Chỉ MỘT phiên bản có hiệu lực tại một thời điểm.
CREATE UNIQUE INDEX network_version_one_published ON pf_waterway.network_version (tenant_id) WHERE status = 'published';
CREATE INDEX network_version_hash ON pf_waterway.network_version (tenant_id, segment_set_sha256);
SELECT shared.apply_tenant_rls('pf_waterway.network_version');

CREATE FUNCTION pf_waterway.fn_network_version_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id OR NEW.version_no <> OLD.version_no
     OR NEW.segment_set_sha256 <> OLD.segment_set_sha256 OR NEW.graph_sha256 <> OLD.graph_sha256
     OR NEW.params <> OLD.params OR NEW.rule_version <> OLD.rule_version OR NEW.as_of <> OLD.as_of
     OR NEW.vertex_count <> OLD.vertex_count OR NEW.edge_count <> OLD.edge_count
     OR NEW.build_job_id <> OLD.build_job_id OR NEW.built_by <> OLD.built_by OR NEW.built_at <> OLD.built_at THEN
    RAISE EXCEPTION 'network_version %: nội dung phiên bản bất biến, chỉ đổi được trạng thái', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NOT ((OLD.status = 'built' AND NEW.status = 'published')
       OR (OLD.status = 'published' AND NEW.status = 'superseded')) THEN
    RAISE EXCEPTION 'network_version %: chuyển trạng thái % → % không hợp lệ', OLD.id, OLD.status, NEW.status
      USING ERRCODE = 'check_violation';
  END IF;
  IF OLD.status = 'published' AND (NEW.published_by <> OLD.published_by OR NEW.published_at <> OLD.published_at
     OR NEW.effective_from <> OLD.effective_from) THEN
    RAISE EXCEPTION 'network_version %: thông tin phát hành bất biến', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_network_version_guard BEFORE UPDATE ON pf_waterway.network_version
  FOR EACH ROW EXECUTE FUNCTION pf_waterway.fn_network_version_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_waterway.network_version
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_waterway.network_version
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 5. Đồ thị của từng phiên bản (chỉ ghi thêm). id bigint theo yêu cầu pgRouting,
--    đánh số tất định 1..n trong mỗi phiên bản.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_waterway.vertex (
  network_version_id uuid   NOT NULL,
  tenant_id          uuid   NOT NULL,
  id                 bigint NOT NULL CHECK (id > 0),
  geom               geometry(Point, 4326) NOT NULL,
  degree             integer NOT NULL CHECK (degree >= 0),
  PRIMARY KEY (network_version_id, id)
);
CREATE INDEX vertex_geom ON pf_waterway.vertex USING gist (geom);
SELECT shared.apply_tenant_rls('pf_waterway.vertex');

-- max_load_tons: tải trọng lưu thông dùng để lọc (suy ra từ rộng/sâu/công trình theo
-- quy tắc phương tiện; khai báo nhỏ hơn thì lấy khai báo). NULL = chưa kết luận
-- được (thiếu số liệu) — bị loại khi có lọc tải trọng, không bao giờ coi là đi được.
CREATE TABLE pf_waterway.edge (
  network_version_id  uuid   NOT NULL,
  tenant_id           uuid   NOT NULL,
  id                  bigint NOT NULL CHECK (id > 0),
  source              bigint NOT NULL,
  target              bigint NOT NULL,
  cost_m              double precision NOT NULL CHECK (cost_m >= 0),     -- geodesic, CSDL tính
  reverse_cost        double precision NOT NULL CHECK (reverse_cost >= 0),
  max_load_tons       numeric(10,2) NULL CHECK (max_load_tons IS NULL OR max_load_tons >= 0),
  derived_max_load_tons numeric(10,2) NULL,
  certain             boolean NOT NULL,
  vessel_code         text    NULL,
  limiting            text    NOT NULL,                -- width | depth | clearance | declared | none | unknown:<…>
  limiting_structure_ref uuid NULL,
  width_m             numeric(8,2) NULL,
  depth_m             numeric(8,2) NULL,
  clearance_m         numeric(8,2) NULL,
  segment_ref         uuid   NOT NULL,
  segment_revision_id uuid   NOT NULL,
  data_source         text   NOT NULL,
  geom                geometry(LineString, 4326) NOT NULL,
  PRIMARY KEY (network_version_id, id),
  CHECK (cost_m = reverse_cost)
);
CREATE INDEX edge_geom    ON pf_waterway.edge USING gist (geom);
CREATE INDEX edge_segment ON pf_waterway.edge (network_version_id, segment_ref);
SELECT shared.apply_tenant_rls('pf_waterway.edge');

-- ---------------------------------------------------------------------------
-- 6. Đoạn đã vào phiên bản (lớp bản đồ GIS US-WATER-01: đánh dấu tuyến phù hợp tải trọng)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_waterway.version_segment (
  network_version_id  uuid   NOT NULL,
  tenant_id           uuid   NOT NULL,
  segment_ref         uuid   NOT NULL,
  segment_revision_id uuid   NOT NULL,
  revision            integer NOT NULL,
  name                text   NOT NULL,
  waterway_class      text   NOT NULL,
  data_source         text   NOT NULL,
  length_m            numeric(14,2) NOT NULL,
  width_m             numeric(8,2)  NULL,
  depth_m             numeric(8,2)  NULL,
  declared_max_load_tons numeric(10,2) NULL,
  max_load_tons       numeric(10,2) NULL,              -- bất lợi nhất trên các cạnh của đoạn; NULL = chưa kết luận
  certain             boolean NOT NULL,
  limiting            text    NOT NULL,
  limiting_structure_ref uuid NULL,
  edge_count          integer NOT NULL,
  geom                geometry(LineString, 4326) NOT NULL,
  PRIMARY KEY (network_version_id, segment_ref)
);
CREATE INDEX version_segment_geom ON pf_waterway.version_segment USING gist (geom);
SELECT shared.apply_tenant_rls('pf_waterway.version_segment');

-- ---------------------------------------------------------------------------
-- 7. Phát hiện khi dựng
-- ---------------------------------------------------------------------------
CREATE TABLE pf_waterway.build_finding (
  network_version_id uuid   NOT NULL,
  tenant_id          uuid   NOT NULL,
  id                 bigint NOT NULL CHECK (id > 0),
  kind               text   NOT NULL CHECK (kind IN ('crossing', 't_join', 'gap_joined', 'gap_reported', 'dangling', 'isolated')),
  segment_refs       uuid[] NOT NULL,
  geom               geometry(Point, 4326) NOT NULL,
  gap_m              numeric(10,2) NULL,
  auto_fixed         boolean NOT NULL,
  message            text   NOT NULL,
  PRIMARY KEY (network_version_id, id)
);
CREATE INDEX build_finding_kind ON pf_waterway.build_finding (network_version_id, kind, id);
SELECT shared.apply_tenant_rls('pf_waterway.build_finding');

-- Đồ thị đã dựng là bất biến: chặn sửa/xóa/truncate kể cả bằng owner.
CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON pf_waterway.vertex
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON pf_waterway.vertex
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON pf_waterway.edge
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON pf_waterway.edge
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON pf_waterway.version_segment
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON pf_waterway.version_segment
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON pf_waterway.build_finding
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON pf_waterway.build_finding
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- Quyền (R-05). Sổ và đồ thị: chỉ SELECT, INSERT.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_waterway.segment_revision, pf_waterway.structure_revision,
  pf_waterway.vertex, pf_waterway.edge, pf_waterway.version_segment, pf_waterway.build_finding TO waterway_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_waterway.segment_revision, pf_waterway.structure_revision,
  pf_waterway.vertex, pf_waterway.edge, pf_waterway.version_segment, pf_waterway.build_finding FROM waterway_app;
GRANT SELECT, INSERT, UPDATE ON pf_waterway.build_job, pf_waterway.network_version TO waterway_app;
REVOKE DELETE, TRUNCATE ON pf_waterway.build_job, pf_waterway.network_version FROM waterway_app;
