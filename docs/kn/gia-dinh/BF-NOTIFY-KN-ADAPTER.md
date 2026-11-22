## BF-NOTIFY-KN-ADAPTER — CNTS: chính sách theo biểu và ổ dịch Khẩn trên cổng kênh notify (#121)

Phụ thuộc #115 (BF-NOTIFY-CNTS, `DeliveryChannels`, `TextTemplate`), #108 (BF-CN-PUBLISH), #111 (BF-CN-OUTBREAK) — cả ba đã ở dev.
Gói chỉ làm phần còn thiếu; không sửa `internal/platform/notify`.

| Mục | Hiện trạng dev trước gói | Gói này |
|---|---|---|
| Cấu hình nhắc theo biểu (#108): trước hạn N giờ, khi trễ, nhắc lặp hằng ngày, leo thang trễ liên tiếp N kỳ, người nhận (cán bộ xã của xã; người phụ trách biểu + Lãnh đạo phòng khi leo thang), kênh, nội dung mẫu | Có (`cntsalert` config + job `Remind`, cảnh báo trễ liên tiếp đi qua `cnts_alert.changed`) | Không đổi |
| Kênh nhắc → `NotificationDraft.Channels` | Có từ #108 nhưng áp cho MỌI sự kiện có `channels` → sự kiện cũ (golden 46 ca của KNSPLIT) đổi hành vi: `TestCNTSOwnedRenderersMatchLegacy` đỏ trên dev (16 ca nhắc) | Trường thêm `channels_explicit` (tùy chọn, omitempty) trong `pf.extension.cnts_form.reminded` v1; job nhắc theo cấu hình biểu đặt true. Renderer chỉ dùng kênh tường minh khi có cờ; không cờ → hành vi cũ (hộp thư + push, email theo `channels`). Golden không sửa, xanh lại |
| Nhắc đợt điều tra (W11.50) | `channels: []` → hành vi cũ | Không đổi (không đánh dấu) |
| Ổ dịch Khẩn (#111) | `outbreak.reported` → `cntsalert` cảnh báo Khẩn số 12 → `cnts_alert.changed` level=urgent → renderer: Severity urgent, hộp thư + push (mặc định), email người nhận cụ thể; không người nhận → chủ đề Cấp Tỉnh của tỉnh, không email | Không đổi mã (đã đúng hợp đồng #115: không email-only cho Khẩn). Giữ `Channels` nil để không đổi golden cảnh báo; nil = hộp thư + push + email theo cờ, tương đương tường minh `{InApp, Push, Email}` |
| Mẫu nội dung | KN thay `{bieu}`, `{ky}`, `{han}` (một lượt, `strings.Replacer`, ≤ 500 ký tự) lúc phát sự kiện; notify Normalize/Redact khi lưu | Không chuyển sang `notifyport.TextTemplate` (`{{biến}}`): cấu hình đã lưu dùng cú pháp `{…}`; dùng `TextTemplate` ở `cntsalert` là cạnh import KN→notify mới (knboundary). Thêm test biến/độ dài. Câu hỏi PO bên dưới |
| Không PII | Tiêu đề cảnh báo ổ dịch: mã bệnh, loài, mã xã, tổng đàn/số chết (tổng hợp); không người báo, không hộ, không tọa độ | Test khẳng định |
| KN không ghi dữ liệu HTX | Renderer chỉ dựng bản nháp; notify ghi `pf_notify` | Không đổi |

Câu hỏi PO: (1) có cần đổi mẫu nhắc sang cú pháp `{{form_name}}`… của notify (phải chuyển đổi cấu hình đã lưu) không — hiện giữ `{bieu}`, `{ky}`, `{han}`; (2) cảnh báo (kể cả Khẩn ổ dịch) chưa có cấu hình kênh theo loại — mặc định hộp thư + push, Khẩn thêm email; có cần cho Cấp Tỉnh chọn kênh theo loại cảnh báo không.

Kiểm chứng: `tests/notifyconsumers` (golden 46 ca) đỏ trên dev → xanh; `internal/kn/extension` `TestCNTSReminderChannels_BF_CN_PUBLISH`, `TestCNTSReminderLegacyChannelsKeepDefault_BF_NOTIFY_KN_ADAPTER`, `TestCNTSOutbreakUrgentAlertDrafts_BF_NOTIFY_KN_ADAPTER`; `cntsalert/domain` `TestFormReminderRenderVariables_BF_NOTIFY_KN_ADAPTER`; DB `tests/notify` `TestCNTSReminderAndOutbreakChannels_BF_NOTIFY_KN_ADAPTER`.
