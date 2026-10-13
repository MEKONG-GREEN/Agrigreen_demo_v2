## NOTIF — Thông báo Báo giá nông sản: chuông + email (pf 20261013070025)

| Câu hỏi / mục | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| Loại thông báo (W11.74 dùng tạm `report_task_due`) | Loại riêng `price_reminder`, `price_alert`, `price_verify_request`, `price_verify_reply`, `price_verify_decision`, `price_lock_summary`, `price_board_approved`, `price_board_reopened` (CHECK `kind`, pf 20261013070025) | `notify/port/price.go`, `apps/web-ops/src/notify/notify.ts` | — |
| Người nhận Cấp Xã | Cán bộ Xã phụ trách chính của xã TẠI NGÀY báo giá (`Point.Officer`); xã "Chưa có cán bộ Xã" → không ai nhận | `price/app/events.go`, `ticket_review.go` | — |
| Người nhận Cấp Tỉnh | Cán bộ Cấp Tỉnh được phân công xã (`Point.Owner`) + mọi người có cờ QTNV báo giá; xã chưa phân công → mọi Cấp Tỉnh của phòng ban giá + QTNV. Tổng hợp 12:00, duyệt / mở duyệt bảng giá → mọi Cấp Tỉnh + QTNV. Người gây ra sự việc không tự nhận | `notify/app/price.go`, `assembly.NotifyPriceDesk` | Đổi danh sách ở `PriceDesk` (gốc lắp ráp) |
| Không tra được Cấp Tỉnh (chưa nối pf/accounts) | Một thông báo chủ đề theo vai `kn_tinh` / quản trị TTKN tỉnh, KHÔNG email | `domain.PriceTinhAudience` | — |
| Kênh email | Email theo người nhận cho: nhắc nhập giá (theo cờ email của lịch W11.74), cảnh báo Tạm tính, yêu cầu / phản hồi / kết quả xác minh, tổng hợp 12:00 (NFR "Thông báo"). Duyệt / mở duyệt bảng giá: chỉ chuông (BRD không nêu email) | `domain.PriceMessages` | Bật `Email` trong bộ dựng |
| Địa chỉ email | Tra lúc gửi qua pf/accounts (Keycloak, email ĐÃ XÁC MINH); không lưu ở `pf_notify`; log chỉ ghi dạng che `ng•••@miền`. Chưa có email → chỉ trong ứng dụng (`dropped no_email`, FN-XA-03.04 ngoại lệ) | `notify/app/email.go` | — |
| Kênh gửi khi chưa đặt `AG_SMTP_HOST` | `LogMailer` (Local/Dev: ghi số người nhận + tiêu đề), chưa đặt `AG_KC_*` → không email | `notify.ChannelsFromEnv`, `cmd/worker/notify.go` | Đặt biến môi trường SMTP / Keycloak |
| Lỗi gửi email | Lỗi tạm (tra địa chỉ / máy chủ thư) → thử lại tới 5 lần qua hàng đợi; email đã gửi không gửi lại khi lô giao lại | `HandleEmail` | — |
| Cảnh báo khi nhập thay (Cấp Tỉnh) | Không phát `pf.price.alert.raised` (người nhập thay là Cấp Tỉnh đã biết); "cán bộ Xã nhận thông báo nhập thay" (FN-TI-02.09 hậu điều kiện) CHƯA làm | `writeEntry` | Thêm sự kiện nhập thay nếu PO cần |
| Hiệu chỉnh giá (FN-TI-02.04 "cán bộ Xã nhận thông báo hiệu chỉnh") | Phát `pf.price.verification.decided` với `decision = adjusted` | `Correct` | — |
| Tổng hợp 12:00 | Phát cùng giao dịch với dòng nhật ký `lock` của job `price_calendar` — đúng một lần mỗi (tenant, bên dùng, ngày); số liệu tại mốc khóa: xã phải báo giá, đã gửi, nộp muộn, không nộp, cảnh báo chưa xử lý | `price/app/lock.go`, `TicketService.LockSummary` | — |
| Liên kết trong email | `AG_WEB_OPS_URL` + đường dẫn màn (`/kn/bao-gia/nhap`, `/xac-minh`, `/kiem-duyet`); W11.74 trỏ `/kn/bao-gia/nhap-gia` (không có route) — sửa thành `/kn/bao-gia/nhap` | `domain/price.go` | — |
