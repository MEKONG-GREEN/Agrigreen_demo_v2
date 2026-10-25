### pf/extension (`dom/extension` — Kỳ KHTC, W11.62)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.khtc_period_not_found` | Không có kỳ KHTC với mã này trong tenant |
| 409 | `extension.khtc_period_duplicate` | Mở kỳ cùng loại, cùng năm, cùng số tuần/tháng/quý (khóa kỳ) khi kỳ cũ chưa hủy (DB: `khtc_period_duplicate`) |
| 409 | `extension.khtc_period_not_closable` | Đóng kỳ khi chưa Đủ dữ liệu và chưa Trễ hạn từ N ngày (`extension.khtc_close_late_days`, mặc định 3 — BR-DL-15) |
| 409 | `extension.khtc_period_not_cancellable` | Hủy kỳ không ở Đã mở / Đang thu thập / Trễ hạn / Đủ dữ liệu, hoặc báo cáo đã có Bản cuối (BR-DL-18) |
| 409 | `extension.khtc_period_terminal` | Sửa hạn, đóng, hủy kỳ đã đóng hoặc đã hủy (DB: `khtc_period_event_terminal`) |
| 409 | `extension.khtc_period_cancelling` | Đóng kỳ, đánh dấu Bản cuối hoặc hủy kỳ lần nữa trong lúc kỳ đang được hủy (ý định hủy còn hạn — DB: `khtc_period_cancelling`, `khtc_report_cancelling`; P0-POOL-SHARED-2) |
