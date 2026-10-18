### pf/hrm (`dom/hrm` — báo cáo Sở NS01–NS06, dashboard nhân sự, W11.28)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `hrm.report_unknown` | Mã biểu ở đường dẫn không thuộc NS01–NS06 |
| 422 | `hrm.report_blocked` | Xuất biểu khi quy tắc kiểm tra mức chặn không đạt (Mục V.2.0: R1 NS06 tổng nhóm ngạch/trình độ/độ tuổi ≠ tổng số; R2 tổng NS05 ≠ số người NS01 cùng ngày chốt) — `detail` liệt kê quy tắc lệch |
| 404 | `hrm.export_not_found` | Tải lại một lần xuất không có (hoặc mã là dòng sổ không phải lần xuất) |
| 503 | `vault.batch_unsupported` | vault/identity (W11.28): kho được nối không có phần mở rộng đọc theo lô (`client.ProfileBatch`) — lỗi cấu hình; pf/hrm tự rơi về đọc từng chủ thể nên không gặp mã này |
