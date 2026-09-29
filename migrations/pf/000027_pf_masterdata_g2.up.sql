-- pf_masterdata G2: tham số và danh mục G2 (cropcycle, GIS, waterway, simulation) và
-- nhập danh mục hàng loạt từ Excel (HTX US-CAT-02).
--
-- Tham số G2 (cropcycle.gps_match_tolerance_m, geo.hub_capacity_*_pct,
-- waterway.*, distance.haversine_factor, activation.grant_lifespan_hours, ngưỡng
-- năng suất bất thường theo giống…) và danh mục vessel_class được NẠP THEO TENANT
-- bằng cơ chế tham số có phiên bản sẵn có (seeddata/params.json, catalogs.json →
-- Service.Seed, đã duyệt bởi tác nhân hệ thống, R-56). Seed chạy lặp an toàn: tenant
-- đã có chỉ nhận khóa/mục MỚI khi chạy lại (POST /v1/masterdata/seed hoặc gốc lắp
-- ráp khi tạo tenant). Migration không ghi dữ liệu tham số (không biết tenant; R-25).
--
-- catalog_import: mỗi lần nhập tệp danh mục một dòng — số dòng tạo/bỏ qua/lỗi và
-- kết quả từng dòng (mã, lý do; danh mục không phải dữ liệu cá nhân). Chống lặp theo
-- Idempotency-Key và cho xem lại kết quả không cần tải tệp riêng (US-CAT-02 AC-2).

CREATE TABLE pf_masterdata.catalog_import (
  tenant_id     uuid        NOT NULL,
  id            uuid        NOT NULL,                  -- UUIDv7
  actor_ref     uuid        NOT NULL,
  format        text        NOT NULL CHECK (format IN ('csv', 'xlsx')),
  file_sha256   bytea       NOT NULL CHECK (octet_length(file_sha256) = 32),
  total_rows    integer     NOT NULL CHECK (total_rows >= 0),
  created_rows  integer     NOT NULL CHECK (created_rows >= 0),
  skipped_rows  integer     NOT NULL CHECK (skipped_rows >= 0),   -- đã có, cùng nội dung
  rejected_rows integer     NOT NULL CHECK (rejected_rows >= 0),
  rows          jsonb       NOT NULL CHECK (jsonb_typeof(rows) = 'array'),
  idem_key      text        NOT NULL,
  event_time    timestamptz NOT NULL,
  recorded_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, idem_key),
  CHECK (created_rows + skipped_rows + rejected_rows = total_rows)
);
CREATE INDEX catalog_import_recent ON pf_masterdata.catalog_import (tenant_id, recorded_at DESC);
SELECT shared.apply_tenant_rls('pf_masterdata.catalog_import');
CREATE TRIGGER trg_no_update BEFORE UPDATE ON pf_masterdata.catalog_import
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_masterdata.catalog_import
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_masterdata.catalog_import
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

GRANT SELECT, INSERT ON pf_masterdata.catalog_import TO masterdata_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_masterdata.catalog_import FROM masterdata_app;
