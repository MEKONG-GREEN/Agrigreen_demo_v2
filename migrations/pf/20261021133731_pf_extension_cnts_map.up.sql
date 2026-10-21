-- pf/extension (`dom/extension`, phòng ban chan_nuoi_thuy_san) — W11.52, D4 CN12, BRD CNTS v3.7 FN-18, UR-TINH-17:
-- BẢN ĐỒ VÙNG NUÔI, VÙNG DỊCH.
--
-- cnts_map_zone — sổ chỉ ghi thêm (shared.apply_ledger), chuỗi 'czone:<zone_ref>': mỗi dòng là MỘT phiên bản đủ trường của
-- một hình vẽ trên bản đồ CNTS: vùng chăn nuôi, vùng nuôi thủy sản, vùng dịch, vùng bị uy hiếp, vùng đệm (Polygon/
-- MultiPolygon, vẽ tay hoặc theo bán kính quanh một tâm) và hướng di chuyển (LineString). Hình học PostGIS EPSG:4326, hợp lệ
-- (ST_IsValid) và không rỗng — kiểm ở cả ứng dụng lẫn ràng buộc.
--   * Sửa hình = phiên bản mới có lý do; ngưng hiển thị = sự việc 'retired' có lý do; khôi phục = 'restored'. Không có
--     UPDATE/DELETE (FN-18 BR-01 "vùng dịch lưu theo ngày", AC-03 "chọn ngày cũ thấy vùng đã lưu của ngày đó").
--   * effective_on là NGÀY ÁP DỤNG của phiên bản: bản đồ ngày D lấy, với mỗi vùng, phiên bản mới nhất có effective_on <= D
--     (không lấy vùng mà phiên bản đó là 'retired'). Phiên bản sau không được có ngày áp dụng sớm hơn phiên bản trước.
--   * Vùng theo bán kính: center + radius_m, geom = ST_Buffer(center::geography, radius_m) (tính ở câu INSERT).
--   * outbreak_ref liên kết ổ dịch của Báo cáo dịch bệnh BM-10 (cnts_outbreak, W11.49); parent_zone_ref: vùng uy hiếp/vùng
--     đệm sinh từ một vùng dịch.
--   * communes: các xã của vùng (phạm vi xem của Cấp Xã — FN-18 AC-02); province_code: tỉnh sở hữu.
--   * area_ha: diện tích trắc địa (ST_Area geography) của vùng — chỉ để hiển thị.
-- Không có dữ liệu cá nhân: hộ, điểm trong vùng được tính khi đọc từ cnts_model_point (W11.47), không chép vào sổ.
-- Vai r_extension: SELECT, INSERT.

CREATE TABLE pf_extension.cnts_map_zone (
  id                  uuid          NOT NULL,             -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'czone:<zone_ref>'
  seq                 bigint        NOT NULL,             -- = số phiên bản
  zone_ref            uuid          NOT NULL,             -- định danh bền của vùng
  event               text          NOT NULL CHECK (event IN ('created', 'updated', 'retired', 'restored')),
  province_code       text          NOT NULL CHECK (province_code ~ '^[A-Za-z0-9._-]{1,20}$'),
  communes            text[]        NOT NULL CHECK (cardinality(communes) BETWEEN 1 AND 200)
                        CHECK (array_position(communes, NULL) IS NULL),
  kind                text          NOT NULL CHECK (kind IN ('vung_chan_nuoi', 'vung_thuy_san', 'vung_dich', 'vung_uy_hiep',
                                                             'vung_dem', 'huong_di_chuyen')),
  name                text          NOT NULL CHECK (char_length(name) BETWEEN 1 AND 300),
  draw_mode           text          NOT NULL CHECK (draw_mode IN ('polygon', 'radius', 'line')),
  geom                geometry(Geometry, 4326) NOT NULL,
  center              geometry(Point, 4326) NULL,           -- tâm của vùng theo bán kính
  radius_m            numeric(10,1) NULL CHECK (radius_m IS NULL OR radius_m BETWEEN 10 AND 50000),
  area_ha             numeric(14,4) NULL CHECK (area_ha IS NULL OR area_ha >= 0),
  outbreak_ref        uuid          NULL,                 -- ổ dịch BM-10 (cnts_outbreak, W11.49)
  parent_zone_ref     uuid          NULL,                 -- vùng dịch sinh ra vùng uy hiếp/vùng đệm
  species_code        text          NULL CHECK (species_code IS NULL OR char_length(species_code) BETWEEN 1 AND 64), -- cnts_species
  disease_code        text          NULL CHECK (disease_code IS NULL OR char_length(disease_code) BETWEEN 1 AND 64), -- cnts_disease
  effective_on        date          NOT NULL,             -- ngày áp dụng của phiên bản (thanh trượt theo ngày)
  note                text          NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  reason              text          NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 1000),
  event_time          timestamptz   NOT NULL,
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  on_behalf_of        uuid          NULL,                 -- chế độ hỗ trợ của Admin
  support_reason      text          NULL,
  idem_key            text          NOT NULL,
  rule_version        text          NOT NULL,
  prev_hash           bytea         NOT NULL,
  row_hash            bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT cnts_map_zone_stream CHECK (stream_key = 'czone:' || zone_ref::text),
  CONSTRAINT cnts_map_zone_valid CHECK (ST_IsValid(geom) AND NOT ST_IsEmpty(geom)),
  CONSTRAINT cnts_map_zone_shape CHECK (
    (kind = 'huong_di_chuyen' AND draw_mode = 'line' AND GeometryType(geom) = 'LINESTRING')
    OR (kind <> 'huong_di_chuyen' AND draw_mode IN ('polygon', 'radius') AND GeometryType(geom) IN ('POLYGON', 'MULTIPOLYGON'))),
  CONSTRAINT cnts_map_zone_radius CHECK ((draw_mode = 'radius') = (center IS NOT NULL AND radius_m IS NOT NULL)),
  CONSTRAINT cnts_map_zone_outbreak CHECK (outbreak_ref IS NULL OR kind IN ('vung_dich', 'vung_uy_hiep', 'vung_dem', 'huong_di_chuyen')),
  CONSTRAINT cnts_map_zone_reason CHECK (event = 'created' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_map_zone_ref ON pf_extension.cnts_map_zone (tenant_id, zone_ref, seq);
CREATE INDEX cnts_map_zone_day ON pf_extension.cnts_map_zone (tenant_id, province_code, effective_on);
CREATE INDEX cnts_map_zone_outbreak ON pf_extension.cnts_map_zone (tenant_id, outbreak_ref) WHERE outbreak_ref IS NOT NULL;
CREATE INDEX cnts_map_zone_geom ON pf_extension.cnts_map_zone USING gist (geom);
SELECT shared.apply_ledger('pf_extension.cnts_map_zone', 'pf_extension.cnts_map_zone');

REVOKE ALL ON pf_extension.cnts_map_zone FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_map_zone TO r_extension;
