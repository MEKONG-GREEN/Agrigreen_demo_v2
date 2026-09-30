-- pf_party G4-0: loại dữ liệu cấp quyền chia sẻ mới `waterway_routes` (ADR-0020; docs/dev/g4-conventions.md §5).
--
-- mkg/logistics (G4, proto FM-05 — mỗi lượt xuống ghe một chuyến vận tải thủy) cần định tuyến đường thủy
-- trên mạng CÓ PHIÊN BẢN của pf/waterway (mục 16). Theo ADR-0020, dữ liệu nền tảng chỉ tới MKG qua bề mặt
-- đối tác của Application API công khai khi tenant chương trình đã duyệt một cấp quyền có loại đó:
--
--   GET  /v1/partner/waterway/route   hai điểm (from, to)
--   POST /v1/partner/waterway/route   dãy điểm (chuyến nhiều điểm dừng)
--
-- Kết quả chỉ gồm hình học đường đi, km, nguồn tính, phiên bản mạng, đoạn và ràng buộc tải trọng — dữ liệu
-- hạ tầng, không dữ liệu cá nhân. Mọi lời gọi ghi một dòng pf_party.partner_read_log (tenant bên cấp).
--
-- Thêm loại = sửa CHECK của sổ share_grant_event (bảng chiếu share_grant / share_grant_inbound và sổ đọc
-- partner_read_log không ràng buộc danh sách loại). Không sửa dòng nào đã ghi (RB-1, R-25).
ALTER TABLE pf_party.share_grant_event DROP CONSTRAINT share_grant_event_kinds_check;
ALTER TABLE pf_party.share_grant_event ADD CONSTRAINT share_grant_event_kinds_check
  CHECK (cardinality(kinds) BETWEEN 1 AND 16 AND kinds <@ ARRAY[
    'cooperative_directory', 'plot_directory', 'season_harvest_dates', 'htx_collection_responses',
    'waterway_routes',
    'valve_hub', 'valve_collection', 'valve_mrv_evidence']::text[]);
