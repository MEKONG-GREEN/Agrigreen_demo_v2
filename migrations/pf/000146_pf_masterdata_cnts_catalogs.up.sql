-- pf_masterdata: danh mục Chăn nuôi – Thủy sản (W11.41, BRD CNTS v3.7 FN-06 / UR-ADM-04; D4 CN1). Chỉ đụng
-- schema pf_masterdata.
--
-- Các danh mục CNTS (cnts_species, cnts_breed, cnts_vaccine, cnts_disease, cnts_chemical, cnts_slaughter_facility,
-- cnts_aquaculture_object, cnts_fishing_group, cnts_indicator, cnts_model_type, cnts_document_type,
-- cnts_vaccination_schedule) dùng lại bảng chiếu catalog_item + sổ catalog_change (000002): tên danh mục chỉ ràng
-- buộc bằng biểu thức chính quy, thuộc tính là jsonb kiểm ở domain/ (attrSpecs). Ngưỡng mặc định là tham số có
-- phiên bản (param_version, khóa cnts.*) — không cần bảng.
--
--   1. catalog_effective — SỔ PHỤ (append-only, shared.apply_ledger) cạnh catalog_change: giá trị có hiệu lực TỪ
--      NGÀY của mục danh mục CNTS (FN-06 BR-03: xã, mộc dấu, loài được giết mổ, loại cơ sở, quy trình tiêm…
--      "sửa là tạo giá trị mới từ ngày áp dụng, không ghi đè giá trị cũ"). Mỗi lần tạo/sửa/ngừng/dùng lại một mục
--      CNTS ghi một dòng (CÙNG giao dịch với dòng catalog_change, change_id trỏ dòng đó); hiệu lực đến = valid_from
--      của dòng kế tiếp. Tra theo ngày: dòng valid_from ≤ ngày lớn nhất (cùng ngày → seq lớn nhất). Không ADD COLUMN
--      vào catalog_change (sổ có dữ liệu, ADR-0012).
--   2. Chỉ mục duy nhất facility_ref của cơ sở giết mổ (UUID do hệ thống cấp, cố định qua mọi lần sửa) — khóa mà
--      phạm vi Cấp Xã theo cơ sở (W11.07 facility_refs) và biểu BM-01 (W11.42) tham chiếu.
--   3. Chỉ mục GIN mã mộc dấu GS/GC (mỗi cơ sở nhiều mã) — kiểm "một mã mộc dấu chỉ thuộc một cơ sở" và tra cơ sở
--      theo mã.

SET LOCAL lock_timeout = '5s';

-- ---------------------------------------------------------------------------
-- 1. Sổ phụ hiệu lực. stream_key = 'catalog:<catalog>:<code>' (cùng chuỗi với catalog_change).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_masterdata.catalog_effective (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  catalog           text        NOT NULL CHECK (catalog ~ '^cnts_[a-z_]{1,34}$'),
  code              text        NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$'),
  change_id         uuid        NOT NULL,                 -- dòng catalog_change cùng giao dịch
  valid_from        date        NOT NULL,                 -- ngày nghiệp vụ (giờ Việt Nam) bắt đầu hiệu lực
  after             jsonb       NOT NULL CHECK (jsonb_typeof(after) = 'object'), -- {name, attrs, active}
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX catalog_effective_stream ON pf_masterdata.catalog_effective (tenant_id, stream_key, seq);
CREATE INDEX catalog_effective_idem   ON pf_masterdata.catalog_effective (tenant_id, idem_key);
CREATE INDEX catalog_effective_at     ON pf_masterdata.catalog_effective (tenant_id, catalog, code, valid_from DESC, seq DESC);
SELECT shared.apply_ledger('pf_masterdata.catalog_effective', 'pf_masterdata.catalog_effective');

REVOKE ALL ON pf_masterdata.catalog_effective FROM PUBLIC;
GRANT SELECT, INSERT ON pf_masterdata.catalog_effective TO r_masterdata;

-- ---------------------------------------------------------------------------
-- 2–3. Chỉ mục của cơ sở giết mổ trên bảng chiếu catalog_item (danh mục mới, chưa có dòng).
-- ---------------------------------------------------------------------------
CREATE UNIQUE INDEX catalog_item_cnts_facility_ref ON pf_masterdata.catalog_item (tenant_id, (attrs->>'facility_ref'))
  WHERE catalog = 'cnts_slaughter_facility';
CREATE INDEX catalog_item_cnts_stamps ON pf_masterdata.catalog_item
  USING gin ((string_to_array(coalesce(attrs->>'stamp_codes_gs', ''), ',') || string_to_array(coalesce(attrs->>'stamp_codes_gc', ''), ',')))
  WHERE catalog = 'cnts_slaughter_facility';
