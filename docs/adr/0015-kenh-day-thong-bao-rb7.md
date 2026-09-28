# ADR-0015: Kênh đẩy thông báo và RB-7

**Trạng thái:** Đề xuất · **Ngày:** 28/09/2026 · **Module:** `internal/platform/notify`

## Bối cảnh
US-LIB-06, HTX US-NEWS-03, KN US-TASK-02/03 và US-SYNC-02 cần báo cho cán bộ/nông dân ngay khi có chỉ đạo, cảnh báo, tài liệu mới, nhiệm vụ leo thang, và cần yêu cầu máy đồng bộ ngay. Đẩy tới Android/iOS thực tế chỉ đi qua dịch vụ của Google (FCM) và Apple (APNs), nằm **ngoài lãnh thổ** — trái RB-7/ADR-0007 nếu mang nội dung hoặc dữ liệu cá nhân.

## Quyết định
1. **Hộp thư trong app luôn bật** (`pf_notify.notification`, trong lãnh thổ). Mọi nội dung chỉ đọc qua API của ta (`GET /v1/notifications`) hoặc entity đồng bộ `notification`.
2. **FCM HTTP v1 chỉ mang thông điệp DỮ LIỆU**: `data = {kind, notification_id}` (với `sync_now` chỉ `kind`). Không có khối `notification`, không tiêu đề, nội dung, tên, SĐT, mã đơn vị hay actor_ref. iOS gửi dạng nền (`content-available: 1`, `apns-push-type: background`). App nhận xong gọi API để lấy nội dung và tự hiện thông báo cục bộ. Kiểm thử `TestFCMPayloadIsDataOnly`, `TestPushDataOnlyAndPerDeviceFailures` khẳng định JSON gửi đi.
3. Token đẩy là định danh thiết bị: lưu trong lãnh thổ, gắn với thiết bị pf/sync đã đăng ký và chưa thu hồi, không ghi log, không trả ra API. Thu hồi thiết bị, token bị FCM báo `UNREGISTERED`, tổ chức bị vô hiệu hóa → ngừng dùng (không xóa cứng).
4. **Email** chỉ cho **tóm tắt chỉ đạo khẩn**, qua SMTP **trong lãnh thổ** (`AG_SMTP_*`), tới hộp thư **công vụ** cấu hình (`AG_SMTP_DIGEST_TO`) — email cá nhân là PII, chỉ ở kho định danh. Thư chỉ có mã chỉ đạo, thời điểm, mã đơn vị, đường dẫn.
5. **Zalo ZNS**: có giao diện `channel.ZNS`, bản mặc định trả `ErrZNSDisabled`. ZNS cần SĐT (PII) và Zalo là bên thứ ba — muốn bật phải có ADR riêng: kho định danh (vùng vault) tra SĐT theo `subject_ref` và gọi Zalo, pf/notify không bao giờ thấy SĐT; cần đồng ý theo mục đích.
6. Người nhận tìm **không cần PII**: danh sách `actor_ref` trong sự kiện hoặc danh bạ cán bộ (`extension/port.StaffDirectory`); không có danh sách chính xác → **thông báo chủ đề** (đơn vị đã mở rộng tỉnh → xã / tổ chức / vai), client thấy theo phạm vi lúc đọc. Người phạm vi toàn quốc chỉ nhận chủ đề toàn quốc (không tràn).

## Hệ quả
- Dev/Local không cấu hình `AG_FCM_*` → kênh đẩy chỉ ghi log. Production cần tài khoản dịch vụ Firebase (`AG_FCM_PROJECT_ID`, `AG_FCM_CREDENTIALS_FILE`).
- Máy tắt mạng dữ liệu vẫn có hộp thư qua pull (`entity: notification`, 30 ngày gần nhất).
- Thiết bị cho thông báo chủ đề và nudge theo đơn vị được chọn theo **ảnh chụp phạm vi/vai lúc gắn token**; app gắn lại token mỗi lần khởi động để làm mới.
- Lỗi tạm của một thiết bị không chặn thiết bị khác; thử lại qua giao lại hàng đợi (tối đa 5) rồi DLQ.
