# BF-NOTIFY-CNTS: cổng kênh và mẫu thông báo

`notify/port.NotificationDraft.Channels` là lựa chọn kênh do module nguồn khai báo. `nil` giữ hành vi cũ: hộp thư + push, email theo cờ `Email`. Khi khác nil, `DeliveryChannels{InApp, Push, Email}` thay thế hoàn toàn mặc định/cờ Email cũ. Ít nhất một kênh phải bật. Đã chọn email thì cần RecipientRef cụ thể khác nil/UUID zero, và cấu hình danh bạ/kênh mail; thiếu cấu hình trả lỗi dependency, rollback toàn phong bì và cho phép retry. Email chưa xác minh tiếp tục dropped theo hợp đồng cũ. Topic không được giả broadcast email.

Hộp thư transport vẫn lưu một dòng làm khóa dedup và neo email/push, nhưng `in_app=false` không hiện ở List/Unread/Get/ReadAll/Sync và không phát notification.created. Push tắt thì không tra token hoặc tạo delivery. Email tắt thì không xếp mail. Không thay API public, tenant/RLS, danh bạ, data-only FCM hoặc RB-7. Migration expand-only `20261023173440_pf_notify_delivery_channels.up.sql` thêm `in_app NOT NULL DEFAULT true`; native/producer cũ giữ nguyên hành vi và golden.

`notify/port.TextTemplate{Title, Body}.Render(values)` chỉ thay `{{variable_name}}`, không có biểu thức/hàm/truy cập thuộc tính/thực thi HTML hay thay biến đệ quy. Biến thiếu, cú pháp sai, UTF-8 sai hoặc kết quả vượt 300/2000 rune trả lỗi nguyên tử. Dữ liệu biến vẫn là plain text; module nguồn phải chọn biến không PII, lớp Normalize/Redact của notify tiếp tục che dữ liệu nhạy cảm trước khi lưu/gửi. Không nhét tên CNTS hoặc template nghiệp vụ vào shared.

## Bàn giao làn KN

#108/BF-CN-PUBLISH sở hữu chính sách theo biểu: trước hạn/khi trễ/lặp/leo thang N kỳ, người nhận/phạm vi, mẫu/biến và renderer/planner chuyển lựa chọn sang Channels. #111/BF-CN-OUTBREAK sở hữu sự kiện ổ dịch Khẩn, người nhận Phòng CNTS và source link. Dùng registries Kinds/Consumers/Renderers/Builders hiện có, không thêm switch hoặc import KN vào notify. Lịch và trạng thái nghiệp vụ vẫn ở KN. Chỉ gửi email tới actor đã xác định, không hạ fail-closed để đoán người nhận. Cổng này không chứng nhận toàn bộ CN-07/UP-08 đã hoàn tất trước khi các gói owner tích hợp.

## Kiểm chứng

Regression: đủ 7 tổ hợp kênh và nil legacy; HTTP inbox/unread/mark/read-all và sync loại dòng không chọn app; DB queue email/push/urgent, outbox và replay; tenant khác không đọc được; template vẫn redact; cấu hình email thiếu/choice rỗng/topic-email rollback cùng sibling và retry được sau sửa. Các kiểm đầy đủ và hash commit được ghi trên issue #115 sau actual exit, không coi SKIP là PASS.
