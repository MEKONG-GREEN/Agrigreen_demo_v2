### pf/price (`cap/price` — báo cáo giá, W11.77)

| HTTP | code | Khi nào |
|---|---|---|
| 422 | `price.report_empty` | Xuất báo cáo giá (Bảng giá tổng hợp, Biến động, Dashboard) khi bộ lọc không có dữ liệu / chưa có bảng giá công bố |
| 409 | `price.stat_month_open` | Khóa thống kê phiếu của tháng chưa kết thúc (còn ngày báo giá chưa qua giờ khóa) |
| 409 | `price.stat_month_state` | Khóa tháng đã khóa / mở lại tháng chưa khóa |


## FN-TI-02.13 — Báo cáo tuần chương trình (Báo giá v2.0)

| Mã | HTTP | Điều kiện và xử lý |
|---|---|---|
| `price.weekly_not_found` | 404 | Chương trình/kỳ/hạng mục không thuộc tenant/profile được phép. Chọn lại kỳ hiện có. |
| `price.weekly_closed` | 409 | Tuần đã chốt hoặc chưa đủ điều kiện tạo tuần kế. Tuần chốt chỉ xem; tạo tuần từ tuần đã chốt mới nhất. |
| `price.weekly_stale` | 409 | Phiên bản program thay đổi sau khi mở/preview. Tải lại dữ liệu mới trước khi thao tác. |
| `price.weekly_confirmation` | 409 | Thiếu xác nhận chốt/xóa mục có dữ liệu/ghi đè Excel. Xem cảnh báo rồi xác nhận hoặc hủy. |
| `price.weekly_import_changed` | 409 | Mã băm tệp khác preview. Xem trước lại cùng tệp rồi ghi nhận. |

`validation_failed` (422) có trường/chi tiết: số âm, số mũ, quá chữ số, tiền không nguyên đồng, số trên nhóm, tên trùng, parent đã có dữ liệu, cấu trúc nhóm không hợp lệ, tệp sai mẫu hoặc công thức trong cột nhập. Dòng Excel lỗi được trả riêng và không cản dòng hợp lệ. `forbidden` (403) chặn ngoài phòng TTTT, ngữ cảnh chỉ xem, Admin chưa hỗ trợ hoặc hỗ trợ thiếu MFA. Đọc của Xã/TW trả 200 với report null, version 0 và các danh sách rỗng; không đọc ledger. `idempotency_key_required`/`conflict` áp cho khóa thiếu hoặc dùng lại cho yêu cầu khác, luôn sau kiểm quyền hiện tại.
