### pf/extension (`dom/extension` — kế hoạch tuần BM-05 → Lịch công tác, BF-CN-WEEKLY)

| HTTP | code | Khi nào |
|---|---|---|
| 409 | `extension.cnts_plan_not_registered` | `POST /v1/extension/cnts/weekly-plan/schedule`: chưa có bài nộp BM-05 Phần II của kỳ × xã (hoặc kỳ không bắt đầu từ Thứ Hai) |
| 403 | `extension.cnts_plan_not_owner` | Kế hoạch tuần của kỳ × xã do cán bộ khác đăng ký — chỉ người đăng ký tạo được Lịch công tác từ kế hoạch |
