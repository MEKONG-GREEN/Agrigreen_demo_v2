### pf/price (`cap/price` — báo cáo giá, W11.77)

| HTTP | code | Khi nào |
|---|---|---|
| 422 | `price.report_empty` | Xuất báo cáo giá (Bảng giá tổng hợp, Biến động, Dashboard) khi bộ lọc không có dữ liệu / chưa có bảng giá công bố |
| 409 | `price.stat_month_open` | Khóa thống kê phiếu của tháng chưa kết thúc (còn ngày báo giá chưa qua giờ khóa) |
| 409 | `price.stat_month_state` | Khóa tháng đã khóa / mở lại tháng chưa khóa |

### Phiên sửa phiếu (#237, BRD Báo giá v2.0 FN-TI-02.04 BR-06/AC-02)

| HTTP | code | Khi nào |
|---|---|---|
| 409 | `price.edit_lease_lost` | Ghi giá/ngày/xóa hoặc gia hạn với handle thiếu, sai, hết hạn hoặc thuộc người khác; nhập/lưu phiếu đã tồn tại khi một người khác đang giữ lease. Tải lại quyền sửa; không tự ghi khi chưa chiếm lại. CAS vẫn trả `price.ticket_stale`. |

Chiếm phiên khi người khác giữ trả 200 với `held=true,mine=false`, tên trong phạm vi phiếu và `handle_id=null`; giao diện chỉ xem. Đọc/gia hạn/nhả không tiết lộ handle của người giữ. Quyền, trạng thái và lỗi cũ vẫn áp dụng trước điều kiện lease.
