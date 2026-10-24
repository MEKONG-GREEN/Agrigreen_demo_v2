## P0-NOTIFY-FAILCLOSED — Thông báo không báo "Đã gửi" khi chưa cấu hình SMTP/FCM (#127)

Phạm vi: kênh gửi ra ngoài của pf/notify (email theo người nhận, email tóm tắt, đẩy FCM) và bộ gửi email của pf/reports.
Không migration (bảng `push_delivery`/`email_delivery` đã có `status = dropped` và `last_error` tự do ≤ 200 ký tự). Ngoại
lệ làn shared (Claude làm, Codex review chéo).

### Lựa chọn: kênh TẮT thay vì từ chối khởi động

- Thiếu `AG_SMTP_*` / `AG_FCM_*` → `channel.DisabledMailer` / `channel.DisabledPusher`: `Send` trả `ErrChannelDisabled`
  kèm lý do; consumer ghi bản gửi `dropped`, `last_error = channel_disabled:<lý do>`, **không** tra địa chỉ, **không**
  thử lại, không bao giờ `sent`. Thông báo trong ứng dụng không đổi.
- Lý do không từ chối khởi động: VPS demo/UAT hiện chạy có chủ đích không SMTP/FCM; worker còn chạy nhiều consumer khác
  (báo cáo, đồng bộ, tổng hợp) — dừng cả tiến trình vì thiếu email là hỏng nặng hơn lỗi gốc. Kênh tắt vẫn "nói thật".
- Áp dụng **mọi** môi trường (kể cả máy phát triển và máy demo): không còn LogMailer/LogPusher mặc định. Giả gửi (ghi log)
  chỉ khi đặt rõ `AG_MAILER=log` / `AG_PUSHER=log` VÀ `AG_ENV ∈ {development, dev, local, test}`
  (`residency.LocalDev`); nơi khác (staging, máy demo `AG_DEPLOY_ENV=demo` chạy `AG_ENV=staging`, production, AG_ENV
  trống) → lỗi cấu hình, worker từ chối dựng kênh. `AG_MAILER=smtp`/`AG_PUSHER=fcm` thiếu cấu hình cũng là lỗi.
- Liên kết trong email: ngoài máy phát triển `AG_WEB_OPS_URL` thiếu/không tuyệt đối/localhost/loopback →
  kênh email theo người nhận TẮT (`link_base_missing`); email tóm tắt (không chứa liên kết web-ops) giữ SMTP. pf/reports:
  `AG_WEB_OPS_URL` sai → bỏ dòng "đăng nhập để tải lại"; `AG_REPORT_LINK_BASE` sai (vd mặc định compose dev
  `http://localhost:8080` trên VPS) → rỗng → gửi Chi cục kiểu liên kết "Gửi lỗi" nêu biến.
- pf/reports giữ cơ chế sẵn có (bộ gửi nil → lịch "Bỏ gửi", Chi cục "Gửi lỗi" kèm lý do); thêm `AG_MAILER` cùng luật.

### Hiển thị cho quản trị

- `GET /v1/notify/delivery-health` (Admin API, vai `admin`, `kn_province_admin`, `kn_national_admin`, `platform_admin`):
  đếm lần gửi 7 ngày theo trạng thái, `disabled` (bản bị bỏ vì kênh tắt), `status ∈ ok|disabled|failing|idle`,
  `disabled_reason`, `warning` tiếng Việt. Tính từ bản ghi lần gửi của tenant vì api-platform không đọc cấu hình kênh
  của worker; kênh chưa từng có bản gửi → `idle` (cảnh báo cấu hình khi đó ở log khởi động worker).
- Log khởi động worker: `kênh email|push TẮT …` mức WARN kèm lý do.
- Chưa làm màn web riêng (ngoài phạm vi P0); web có kiểu sinh `adminNotifyDeliveryHealth` để gắn sau.

### Giả định / còn mở

- Không thêm mã `problem.*` mới (endpoint chỉ trả `forbidden`/`mfa_required` sẵn có).
- `reports` handler `smtp_configured` ở api-platform vẫn là `false` (api-platform không nối bộ gửi) — giữ nguyên, ngoài
  phạm vi; nguồn đúng là `delivery-health` + trạng thái lượt gửi.
- Không đổi kết nối DB của `cmd/worker/reports.go` (#128) hay giao dịch của `digest.go` (#126).
