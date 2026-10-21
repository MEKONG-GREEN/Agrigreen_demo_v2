### pf/extension (`dom/extension` — nhật ký nuôi BM-08/09/19, W11.48)

Dùng lại mã đã có của module: `extension.read_only`, `extension.support_reason_required`, `extension.dependency_unavailable`
(chưa nối đơn vị hành chính; tải/xem ảnh khi chưa nối kho tệp `pf/evidence` hoặc kho định danh), `forbidden` (không phải người
ghi nhật ký của hộ / không phải chủ nhiệm, lãnh đạo phòng), `not_found` (hộ mô hình không có hoặc ngoài phạm vi).

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.cnts_diary_cycle_not_found` | Không có lứa nuôi hoặc hộ của lứa ngoài phạm vi người gọi |
| 422 | `extension.cnts_diary_cycle_invalid` | Lứa sai: mẫu nhật ký (khác mẫu của điểm), giống, ngày nhận giống, số con, vật tư; kết thúc lứa thiếu tổng thu NC-15 hoặc số tiền không nguyên/âm |
| 409 | `extension.cnts_diary_cycle_version_conflict` | `base_version` khác phiên bản hiện hành; mở lại lứa đang nuôi |
| 409 | `extension.cnts_diary_cycle_closed` | Ghi/sửa lứa, lần ghi khi lứa đã kết thúc, hộ đã rút hoặc dự án/điểm đã kết thúc |
| 409 | `extension.cnts_diary_cycle_open_exists` | Hộ đang có lứa chưa kết thúc |
| 404 | `extension.cnts_diary_entry_not_found` | Không có lần ghi, ngoài phạm vi, hoặc bản nháp của người khác |
| 422 | `extension.cnts_diary_entry_invalid` | Chỉ số không có trong mẫu/sai kiểu, loại lần ghi, ngày ghi (tương lai, trước ngày nhận giống, ghi bù > 7 ngày), thời điểm thiết bị (tương lai, ngoại tuyến > 30 ngày), tọa độ ngoài Việt Nam, ảnh chưa tải/không thuộc hộ |
| 422 | `extension.cnts_diary_entry_blocked` | Gửi/điều chỉnh vi phạm quy tắc chặn: NC-05 số sống ≠ sống lần trước − chết − xuất bán trong ngày; NC-06 > 0 thiếu NC-07; BM-19 cân đo thiếu ảnh cân mẫu (NT-13); có cảnh báo môi trường nước thiếu biện pháp (NT-12). `errors[]` nêu từng quy tắc |
| 409 | `extension.cnts_diary_entry_version_conflict` | `base_version` khác phiên bản hiện hành |
| 409 | `extension.cnts_diary_entry_state` | Thao tác không hợp lệ ở trạng thái hiện tại (vd xác thực bản nháp/yêu cầu bổ sung, điều chỉnh bản chưa xác thực) |
| 409 | `extension.cnts_diary_entry_locked` | Sửa lần ghi đã xác thực (dùng Điều chỉnh) hoặc lần ghi đã gửi quá `cnts.diary_edit_days` ngày |
| 409 | `extension.cnts_diary_entry_day_taken` | BM-08/09: lứa đã có lần ghi của ngày ghi |
| 403 | `extension.cnts_diary_self_verify` | Người xác thực là người ghi (hoặc đã tạo/sửa/gửi lần ghi) |
| 422 | `extension.cnts_diary_photo_invalid` | Ảnh rỗng, > 500 KB, không phải JPEG/PNG/WebP, loại ảnh sai, thời điểm chụp ở tương lai, mã ảnh đã dùng cho hộ khác |
| 409 | `extension.cnts_diary_photo_no_key` | Chủ hộ không có khóa công khai nông hộ ở kho định danh (đã xóa hoặc không phải chủ thể nông hộ) |
| 404 | `extension.cnts_diary_photo_not_found` | Ảnh không có, hoặc người gọi không phải người ghi được / người rà soát được / Admin |
