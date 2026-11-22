### pf/extension (`dom/extension` — hồ sơ KN riêng HTX / Nông hộ / Tổ hợp tác, KNSPLIT-2-KN-RECORDS #67)

Dùng lại mã đã có của module: `extension.read_only` (Cấp Quốc gia ghi), `extension.out_of_scope` (xã ngoài phạm vi khi
lọc, thêm, sửa), `extension.support_reason_required`, `extension.dependency_unavailable` (chưa nối kho định danh, đơn vị
hành chính hoặc publication HTX đã công bố); `forbidden` (Cấp Tỉnh ghi); `validation_failed` theo trường (thiếu Xã/phường,
Loại, Tên, Người đại diện/Chủ hộ, Điện thoại; SĐT sai; diện tích không phải số thập phân thường 0–1.000.000, tối đa 4 chữ
số lẻ; MST sai; liên kết HTX đã công bố sai; HTX/THT cha không hợp lệ; lý do ngắn hoặc có SĐT/CCCD).

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.kn_record_not_found` | Không có hồ sơ, hồ sơ ở xã ngoài phạm vi người gọi, hoặc tenant khác |
| 409 | `extension.kn_record_name_taken` | Thêm/sửa/kích hoạt lại HTX hoặc Tổ hợp tác trùng tên (không phân biệt hoa thường, khoảng trắng) với hồ sơ cùng loại đang hoạt động trong cùng xã |
| 409 | `extension.kn_record_household_exists` | Thêm/sửa/kích hoạt lại Nông hộ có chủ hộ (cùng chủ thể kho định danh, theo SĐT) đã có hồ sơ Nông hộ đang hoạt động |
| 409 | `extension.kn_record_version_conflict` | `base_version` khác phiên bản hiện hành (người khác vừa sửa) |
| 409 | `extension.kn_record_state_conflict` | Sửa hồ sơ đang ngừng hoạt động; vô hiệu hóa/kích hoạt lại hồ sơ đã ở trạng thái đó |
