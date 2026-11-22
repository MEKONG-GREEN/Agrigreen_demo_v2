-- pf/extension (`dom/extension`) — KN-STANDALONE-5-TT-DATA (#271, kế hoạch KN-STANDALONE quyết định D1): THỬA RUỘNG CỦA
-- KN (Trồng trọt FN-XA-03.05 "Vẽ ranh giới thửa ruộng") về kho KN tự quản. Trước đây thửa KN ghi vào module HTX
-- (pf_coop.plot, plot_boundary, plot_event); từ đây KN ghi sổ riêng, khóa HTX theo hồ sơ KN (kn_producer, kind
-- cooperative). Bảng HTX GIỮ NGUYÊN, chỉ đọc một lần để chép — không DROP, không UPDATE/DELETE.
--
-- kn_plot_version — sổ chỉ ghi thêm (shared.apply_ledger: DB tính seq, prev_hash, row_hash; chặn UPDATE/DELETE; RLS
--   tenant), chuỗi 'kn_plot:<plot_ref>'. Mỗi dòng là ẢNH CHỤP ĐỦ thửa sau sự việc (hình hiện hành + thuộc tính);
--   Log là hiệu giữa hai ảnh chụp liên tiếp. Sự việc: migrated (chép một lần từ pf_coop), created, boundary_corrected
--   (phiên bản ranh giới mới, lý do ≥ 10 ký tự), attributes_changed. boundary_id = id của dòng đã ghi hình hiện hành.
-- kn_plot — bảng chiếu trạng thái hiện hành (R-21, cập nhật cùng giao dịch với sổ), sync_seq cho chiều xuống đồng bộ
--   ngoại tuyến (entity kn_plot). Diện tích luôn tính trong PostGIS: ST_Area(geom::geography) / 10000, numeric(14,4).
--   cooperative_ref: hồ sơ KN loại HTX (pf_extension.kn_producer.producer_ref), không phải HTX nền tảng.
--   legacy_cooperative_ref: HTX nền tảng của thửa lúc chép (chỉ đối chiếu, không ghi lại).
-- Không dữ liệu cá nhân: tên nông dân ở kho định danh (farmer_ref). Vai r_extension: sổ SELECT, INSERT; bảng chiếu
-- SELECT, INSERT, UPDATE.
SET LOCAL lock_timeout = '5s';

CREATE SEQUENCE pf_extension.kn_plot_code_seq;
CREATE SEQUENCE pf_extension.kn_plot_sync_seq;

CREATE TABLE pf_extension.kn_plot_version (
  id               uuid                         NOT NULL,      -- UUIDv7 (R-11)
  tenant_id        uuid                         NOT NULL,
  stream_key       text                         NOT NULL,      -- 'kn_plot:<plot_ref>'
  seq              bigint                       NOT NULL,
  plot_ref         uuid                         NOT NULL,
  event            text                         NOT NULL CHECK (event IN ('migrated', 'created', 'boundary_corrected', 'attributes_changed')),
  code             text                         NOT NULL CHECK (char_length(code) BETWEEN 1 AND 40),
  commune_code     text                         NOT NULL CHECK (char_length(commune_code) BETWEEN 1 AND 20),
  boundary_id      uuid                         NOT NULL,
  geom             geometry(MultiPolygon, 4326) NOT NULL,
  area_ha          numeric(14,4)                NOT NULL CHECK (area_ha > 0),
  cooperative_ref  uuid                         NULL,
  farmer_ref       uuid                         NULL,
  lot_name         text                         NULL CHECK (lot_name IS NULL OR char_length(lot_name) BETWEEN 1 AND 200),
  note             text                         NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  declared_area_ha numeric(14,4)                NULL CHECK (declared_area_ha IS NULL OR declared_area_ha > 0),
  shape            text                         NULL CHECK (shape IS NULL OR shape IN ('polygon', 'circle', 'square')),
  department_code  text                         NULL CHECK (department_code IS NULL OR department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  source           text                         NOT NULL CHECK (source IN ('kn_web', 'kn_field_app', 'migrated')),
  reason           text                         NULL CHECK (reason IS NULL OR char_length(reason) <= 500),
  event_time       timestamptz                  NOT NULL,
  recorded_at      timestamptz                  NOT NULL DEFAULT now(),
  actor_ref        uuid                         NOT NULL,
  on_behalf_of     uuid                         NULL,
  support_reason   text                         NULL CHECK (support_reason IS NULL OR char_length(support_reason) BETWEEN 10 AND 500),
  device_id        text                         NULL,
  idem_key         text                         NOT NULL,
  rule_version     text                         NOT NULL,
  prev_hash        bytea                        NOT NULL,
  row_hash         bytea                        NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT kn_plot_version_stream CHECK (stream_key = 'kn_plot:' || plot_ref::text),
  CONSTRAINT kn_plot_version_reason CHECK (event <> 'boundary_corrected' OR char_length(btrim(reason)) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX kn_plot_version_ref ON pf_extension.kn_plot_version (tenant_id, plot_ref, seq);
CREATE INDEX kn_plot_version_idem ON pf_extension.kn_plot_version (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_extension.kn_plot_version', 'pf_extension.kn_plot_version');

CREATE TABLE pf_extension.kn_plot (
  tenant_id              uuid                         NOT NULL,
  ref                    uuid                         NOT NULL,
  code                   text                         NOT NULL CHECK (char_length(code) BETWEEN 1 AND 40),
  commune_code           text                         NOT NULL CHECK (char_length(commune_code) BETWEEN 1 AND 20),
  cooperative_ref        uuid                         NULL,
  farmer_ref             uuid                         NULL,
  lot_name               text                         NULL CHECK (lot_name IS NULL OR char_length(lot_name) BETWEEN 1 AND 200),
  note                   text                         NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  declared_area_ha       numeric(14,4)                NULL CHECK (declared_area_ha IS NULL OR declared_area_ha > 0),
  shape                  text                         NULL CHECK (shape IS NULL OR shape IN ('polygon', 'circle', 'square')),
  department_code        text                         NULL CHECK (department_code IS NULL OR department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  version                bigint                       NOT NULL CHECK (version >= 1),   -- seq của ảnh chụp mới nhất
  boundary_id            uuid                         NOT NULL,
  geom                   geometry(MultiPolygon, 4326) NOT NULL,
  area_ha                numeric(14,4)                NOT NULL CHECK (area_ha > 0),
  legacy_cooperative_ref uuid                         NULL,
  sync_seq               bigint                       NOT NULL DEFAULT nextval('pf_extension.kn_plot_sync_seq'),
  created_by             uuid                         NOT NULL,
  created_at             timestamptz                  NOT NULL DEFAULT now(),
  updated_at             timestamptz                  NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, ref),
  UNIQUE (tenant_id, code)
);
CREATE INDEX kn_plot_geom ON pf_extension.kn_plot USING gist (geom);
CREATE INDEX kn_plot_commune ON pf_extension.kn_plot (tenant_id, commune_code, ref);
CREATE INDEX kn_plot_coop ON pf_extension.kn_plot (tenant_id, cooperative_ref, ref);
CREATE INDEX kn_plot_sync ON pf_extension.kn_plot (tenant_id, sync_seq);
SELECT shared.apply_tenant_rls('pf_extension.kn_plot');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_extension.kn_plot
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_extension.kn_plot
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

REVOKE ALL ON pf_extension.kn_plot_version, pf_extension.kn_plot FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.kn_plot_version TO r_extension;
GRANT SELECT, INSERT, UPDATE ON pf_extension.kn_plot TO r_extension;
GRANT USAGE ON SEQUENCE pf_extension.kn_plot_code_seq, pf_extension.kn_plot_sync_seq TO r_extension;
-- Chép dữ liệu cũ: migration kế tiếp 20261110013755_pf_extension_kn_plot_copy.
