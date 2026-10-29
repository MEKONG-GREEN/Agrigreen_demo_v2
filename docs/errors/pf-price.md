### pf/price (`cap/price` — báo cáo giá, W11.77)

| HTTP | code | Khi nào |
|---|---|---|
| 422 | `price.report_empty` | Xuất báo cáo giá (Bảng giá tổng hợp, Biến động, Dashboard) khi bộ lọc không có dữ liệu / chưa có bảng giá công bố |
| 409 | `price.stat_month_open` | Khóa thống kê phiếu của tháng chưa kết thúc (còn ngày báo giá chưa qua giờ khóa) |
| 409 | `price.stat_month_state` | Khóa tháng đã khóa / mở lại tháng chưa khóa |
| 422 | `validation_failed` | Cấu hình ngưỡng `pct`, `flat_pct`, `survey_pct`: phải là chuỗi thập phân thường trong (0;100], tối đa2 số lẻ, raw-length không quá32; từ chối số mũ/chuỗi dài trước Decimal. `errors[].field` chỉ đúng ô lỗi (BF-SWEEP-PRICE #165). |
