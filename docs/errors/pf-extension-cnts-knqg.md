### pf/extension (`dom/extension` — mẫu biểu dự án KNQG Mẫu 01, 03, 04, 05, 07, W11.53)

Dùng lại mã đã có của module: `extension.read_only` (ngữ cảnh chỉ xem; Cấp TW mọi thao tác trừ xác nhận/trả lại Mẫu 07),
`extension.support_reason_required`, `extension.dependency_unavailable` (chưa nối kho tệp — bản scan đã ký; chưa nối
pf/export — Word), `extension.cnts_project_closed` (lập/sửa biểu của dự án đã kết thúc). Lỗi của pf/export (`export.*`) đi
nguyên.

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.cnts_knqg_not_found` | Không có biểu, hoặc ngoài phạm vi người gọi (Cấp Xã: không phải Mẫu 05 của điểm thuộc xã mình; Cấp TW: dự án không phải KNQG, Mẫu 07 chưa gửi lần nào); lập biểu cho dự án không có/ngoài phạm vi |
| 422 | `extension.cnts_knqg_invalid` | Mẫu, kỳ (tháng YYYY-MM cho Mẫu 01, năm YYYY cho Mẫu 07), điểm (chỉ và bắt buộc với Mẫu 05, thuộc dự án, chưa đóng) sai; nội dung có ô/bảng/cột không có trong mẫu, sai kiểu (số không âm, số nguyên, ngày), quá độ dài/số dòng/200 KB; văn bản có SĐT/CCCD |
| 422 | `extension.cnts_knqg_project_kind` | Lập biểu cho dự án không phải loại khuyến nông quốc gia |
| 422 | `extension.cnts_knqg_incomplete` | Trình duyệt/trình ký/ký/gửi khi thiếu ô bắt buộc; Mẫu 05 — thực tế khác số từ nhật ký mà không ghi chú (quy tắc liên biểu 9), chỉ tiêu Không đạt không ghi chú |
| 409 | `extension.cnts_knqg_exists` | Đã có biểu hiệu lực: Mẫu 01 cùng dự án × tháng, Mẫu 07 cùng dự án × năm, Mẫu 05 cùng điểm (mở biểu đó, lập bản điều chỉnh nếu đã duyệt/ký) |
| 409 | `extension.cnts_knqg_version_conflict` | `base_version` khác phiên bản hiện hành |
| 409 | `extension.cnts_knqg_state` | Thao tác không áp dụng cho trạng thái hiện tại (vd duyệt biểu chưa trình, ký biểu chưa trình ký, điều chỉnh Mẫu 07 đã xác nhận) |
| 409 | `extension.cnts_knqg_locked` | Sửa nội dung biểu đã trình/duyệt/chờ ký/đã ký/đã gửi/đã xác nhận (FN-19 BR-02, BR-03) |
| 409 | `extension.cnts_knqg_needs_signed_05` | Gửi Mẫu 07 khi còn điểm (chưa đóng) chưa có Mẫu 05 Đã ký, kể cả điểm có bản điều chỉnh chưa ký (FN-19 AC-03, quy tắc liên biểu 10) — `detail` liệt kê điểm |
| 422 | `extension.cnts_knqg_scan_invalid` | Bản scan rỗng, quá 10 MB, không phải PDF/JPG/PNG (kiểm đuôi và chữ ký đầu tệp) |
| 404 | `extension.cnts_knqg_scan_not_found` | Biểu (hoặc phiên bản yêu cầu) chưa có bản scan đã ký |
