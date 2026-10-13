### pf/extension (`dom/extension` — cảnh báo CNTS, vòng đời cảnh báo, cấu hình nhắc nhở, W11.43)

Dùng lại mã đã có của module: `extension.read_only` (ngữ cảnh chỉ xem), `extension.support_reason_required`,
`extension.dependency_unavailable` (chưa nối cap/forms `SubmissionFeed` hoặc danh bạ cán bộ pf/accounts).

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.cnts_alert_not_found` | Cảnh báo không có, hoặc ngoài phạm vi người gọi (Cấp Xã: xã không phụ trách và không được giao) |
| 409 | `extension.cnts_alert_transition` | Thao tác không hợp lệ ở trạng thái hiện tại: cảnh báo đã đóng; "Nhận xử lý" khi chưa Đã giao |
| 409 | `extension.cnts_alert_version_conflict` | `base_seq` khác số sự việc cuối — đã có người cập nhật cảnh báo (khóa lạc quan) |
| 422 | `extension.cnts_alert_invalid` | Thiếu người được giao, người được giao không phải cán bộ phòng CNTS của tỉnh (Cấp Xã không phụ trách xã của cảnh báo), hạn xử lý không sau hiện tại hoặc quá 30 ngày, kết quả xử lý dưới 10 ký tự, ghi chú quá dài |
| 422 | `extension.cnts_alert_config_invalid` | Cấu hình nhắc nhở sai (biểu không có, giờ nhắc trước hạn ngoài 0–168, giờ nhắc lặp không dạng HH:MM, số kỳ trễ liên tiếp ngoài 0–12, kênh không hỗ trợ, nội dung quá 500 ký tự) hoặc cấu hình xử lý sai (loại không có, hạn xử lý ngoài 1–720 giờ, đặt mức Khẩn cho loại có mức cố định) |
