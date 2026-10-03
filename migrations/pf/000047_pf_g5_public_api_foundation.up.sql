-- Cụm pf — nền móng G5 (G5-0, docs/dev/g5-conventions.md §2; ADR-0022..0024).
--
-- 1. Schema của hai module mới, chỉ USAGE cho đúng vai (R-05: mỗi module một vai, chỉ quyền trên schema
--    của nó). Bảng, RLS (shared.apply_tenant_rls), sổ (shared.apply_ledger) và GRANT trên bảng do
--    migration của từng module tạo:
--
--      pf_apiclient   khách hàng API, sổ khóa API (chỉ prefix + HMAC), đăng ký sản phẩm, gói hạn mức,
--                     việc xuất bất đồng bộ                                         (pf 000048, apiclient_app)
--      pf_metering    sổ sử dụng usage_event (chứng từ), bộ đếm hạn mức, sổ xuất egress_event (DLP),
--                     bảng kê theo kỳ                                               (pf 000049, metering_app)
--
--    Vai apiclient_app, metering_app và public_api (tiến trình cmd/api-public — chỉ bảng vận hành
--    schema shared qua ag_shared_user, như platform_api) khai ở internal/shared/dbadmin (G5-0).
--    Không có quyền xuyên schema: api-public đọc module chủ dữ liệu qua port/ bằng pool vai của chính
--    module đó (R-05, R-06).
--
-- 2. Loại cấp quyền chia sẻ cho sản phẩm dữ liệu Public API (ADR-0020 mở rộng, ADR-0022 mục 3):
--    bên cấp (chủ dữ liệu) → tenant BÊN THỨ BA, loại "dp_<tên hợp đồng contracts/dataproduct>".
--    Thêm loại = sửa CHECK của sổ share_grant_event (bảng chiếu và sổ đọc không ràng buộc danh sách
--    loại). Không sửa dòng nào đã ghi (RB-1, R-25).

CREATE SCHEMA pf_apiclient;
REVOKE ALL ON SCHEMA pf_apiclient FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_apiclient TO apiclient_app;

CREATE SCHEMA pf_metering;
REVOKE ALL ON SCHEMA pf_metering FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_metering TO metering_app;

ALTER TABLE pf_party.share_grant_event DROP CONSTRAINT share_grant_event_kinds_check;
ALTER TABLE pf_party.share_grant_event ADD CONSTRAINT share_grant_event_kinds_check
  CHECK (cardinality(kinds) BETWEEN 1 AND 16 AND kinds <@ ARRAY[
    'cooperative_directory', 'plot_directory', 'season_harvest_dates', 'htx_collection_responses',
    'waterway_routes',
    'valve_hub', 'valve_collection', 'valve_mrv_evidence',
    'dp_collection_volume', 'dp_hub_capacity', 'dp_mrv_package_status']::text[]);
