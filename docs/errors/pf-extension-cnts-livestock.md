### pf/extension (`dom/extension` — danh sách hộ chăn nuôi CNTS v4.0, CNTS-V4-HOUSEHOLD-LIST #251, ADR-0056)

Route `/v1/extension/cnts/livestock-households*` (BRD CNTS v4.0 FN-10). Dùng lại mã đã có của module: `forbidden` (vai không có
quyền: Cấp TW; nhóm Cấp Tỉnh khác cán bộ tổng hợp sửa/vô hiệu hóa; Cấp Xã vô hiệu hóa), `extension.read_only`,
`extension.out_of_scope` (thêm/đổi hộ sang xã ngoài phạm vi — AC-06), `extension.support_reason_required`,
`extension.dependency_unavailable` (chưa nối đơn vị hành chính, danh mục CNTS, kho định danh; kho không hỗ trợ bản sửa),
`validation_failed` (thân JSON sai kiểu — vd `head_count` 1e100000, thiếu `base_version`/`Idempotency-Key`, sửa không có thay
đổi). Lỗi của kho định danh đi thẳng qua: `vault.unavailable`, `vault.quota_exceeded`, `mfa_required`.

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.cnts_livestock_household_not_found` | Không có hộ hoặc hộ ngoài phạm vi người gọi (Cấp Xã: xã khác) |
| 422 | `extension.cnts_livestock_household_invalid` | HCN-02…HCN-06 sai: thiếu họ tên / quá 200 ký tự, SĐT có ký tự khác chữ số, xã không còn hiệu lực, ấp quá 200 ký tự hoặc chứa SĐT/CCCD, loại chăn nuôi ngoài 4 loại, vật nuôi 0 hoặc > 30 dòng / trùng / không có hoặc đã ngừng trong danh mục FN-06, số con âm; check-in sai (độ chính xác không phải số thập phân thường ≤ 2 chữ số lẻ, tọa độ ngoài Việt Nam, thời điểm GPS ở tương lai); đổi vị trí thiếu lý do; vô hiệu hóa thiếu lý do (5–1000 ký tự) |
| 422 | `extension.cnts_livestock_household_checkin_required` | Thêm hộ từ màn danh sách mà chưa check-in tại nhà hộ (AC-05) |
| 409 | `extension.cnts_livestock_household_version_conflict` | `base_version` khác phiên bản hiện hành |
| 409 | `extension.cnts_livestock_household_duplicate` | Trùng họ tên chủ hộ, ấp, xã với hộ Đang dùng (luồng ngoại lệ FN-10); trường `confirm_distinct` và mở rộng `duplicates` mang mã hộ trùng — gửi lại với `confirm_distinct = true` để lưu |
| 409 | `extension.cnts_livestock_household_deactivated` | Sửa, check-in, vô hiệu hóa hộ đã vô hiệu hóa (BR-04: không kích hoạt lại) |
| 409 | `extension.cnts_livestock_household_phone_taken` | SĐT mới đã thuộc chủ thể khác trong kho định danh |
| 503 | `vault.revisions_unsupported` | Kho định danh được nối không có phần mở rộng `client.Revisions` (bản sửa họ tên/SĐT) — lỗi cấu hình |
