### pf/extension (`dom/extension` — phiếu Báo cáo cho KHTC, W11.63)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.khtc_form_not_found` | Phòng không thuộc kỳ KHTC, kỳ không có; phòng xem phiếu của kỳ đã hủy (BR-DL-18 — phiếu gỡ khỏi module các phòng) |
| 409 | `extension.khtc_form_locked` | Lưu nháp / gửi khi phiếu Đã gửi (chưa có yêu cầu bổ sung — BR-DL-11), Đã xác nhận, Đã khóa, Không gửi hoặc kỳ đã đóng |
| 422 | `extension.khtc_form_invalid` | Gửi KHTC khi còn lỗi chặn của 3 quy tắc (yêu cầu của mẫu; số âm phải có lý do). Trường mở rộng `issues` liệt kê từng ô / dòng điều chỉnh / mục lời văn. Cảnh báo "lũy kế > 100% KH" không chặn |
| 409 | `extension.khtc_form_not_reviewable` | KHTC "Yêu cầu bổ sung" khi phiếu không ở Đã gửi, đã gửi đủ số lần tối đa (3/3) hoặc kỳ đã đóng; "Xác nhận" khi phiếu không ở Đã gửi |
| 409 | `extension.khtc_form_template_outdated` | Kỳ mở trước khi cài biểu KHTC-PHIEU phiên bản có trường `bang_so_lieu` (phiên bản 1 của W11.62) — hủy và mở lại kỳ |

Mã dùng lại của mã đã có (xem docs/errors.md): xung đột phiên bản bài nộp của cap/forms khi `base_attempt` lệch; số liệu sai biểu của cap/forms (trường `bang_so_lieu`) khi nộp thẳng /v1/forms không mang bảng đã niêm phong; dịch vụ phụ thuộc chưa sẵn sàng khi chưa nối cổng Entry.
