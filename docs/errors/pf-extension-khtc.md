### pf/extension (`dom/extension` — Danh mục KHTC, danh sách CT/DA, Kế hoạch năm, W11.61)

| HTTP | code | Khi nào |
|---|---|---|
| 409 | `extension.khtc_catalog_exists` | Thêm mục Danh mục KHTC (`POST /v1/kn/khtc/catalogs/{kind}`) mà mã đã có trong loại đó, kể cả mục đã ngừng dùng |
| 404 | `extension.khtc_catalog_not_found` | Sửa/xem lịch sử mục danh mục không có |
| 409 | `extension.khtc_default_conflict` | Người ký đang dùng nhận mặc định cho mẫu (`default_for`) mà mẫu đó đã có người ký mặc định đang dùng khác (FN-AD-04.01) |
| 409 | `extension.khtc_program_exists` | Mã CT/DA (không phân biệt hoa thường) đã thuộc CT/DA khác trong danh sách chủ |
| 404 | `extension.khtc_program_not_found` | CT/DA không có (sửa, lịch sử) |
| 404 | `extension.khtc_plan_not_found` | Năm chưa có kế hoạch, phiên bản (`?version=`) không có, chưa có phiên bản hiệu lực tại `?at=`, hoặc "Sao chép từ năm trước" khi năm trước chưa có kế hoạch |
| 409 | `extension.khtc_plan_version_conflict` | Lưu kế hoạch với `base_version` khác phiên bản mới nhất (người khác đã điều chỉnh); thêm `latest_version` |
| 422 | `extension.khtc_plan_invalid` | Cây kế hoạch sai (lỗi theo trường `entries.<i>.<trường>`): cấp CT/DA → nhóm → hạng mục, cha đứng trước, cùng tab; hạng mục thiếu phòng phụ trách; nguồn module thiếu khóa nguồn; ngừng theo dõi thiếu ngày; ĐVT/nguồn KP/lĩnh vực/CT-DA không có hoặc đã ngừng |
| 422 | `extension.khtc_effective_date_invalid` | Ngày hiệu lực ngoài năm kế hoạch; "Điều chỉnh theo QĐ" thiếu số QĐ, ngày QĐ hoặc ngày hiệu lực; ngày hiệu lực không sau ngày chốt kỳ KHTC đã đóng gần nhất (khi W11.62 nối) |
| 409 | `extension.khtc_entry_in_use` | Phiên bản mới bỏ dòng đã có số liệu ở phiếu (khi W11.63 nối) — chỉ được ngừng theo dõi |
| 422 | `extension.khtc_import_invalid` | Tệp Excel/CSV kế hoạch năm không đọc được, thiếu cột Tab/Cấp/Tên, hoặc không có dòng hợp lệ |
