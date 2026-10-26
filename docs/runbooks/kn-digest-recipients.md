# Email tóm tắt KN: hộp thư công vụ theo tenant

KNSPLIT-4B-NOTIFY dùng mapping tin cậy theo tenant cho queue email tóm tắt KN. Trước triển khai, cần khai cả **tenant KN hiện tại** (có thể vẫn là tenant lưu ký trước chuyển) và từng tenant Trung tâm tỉnh được bật. Thiếu mapping giữ hàng chờ, job báo lỗi và không dùng `AG_SMTP_DIGEST_TO`. Nội dung sự kiện và dữ liệu từ client không quyết định địa chỉ nhận thư.

## Cấu hình được duyệt

Worker đọc `AG_KN_DIGEST_RECIPIENTS_JSON` khi dựng module. Ví dụ dưới đây dùng UUID và địa chỉ giả:

```json
[
  {"tenant_id":"01990000-0000-7000-8000-000000000001","to":["vanthu-center-a@example.vn"]},
  {"tenant_id":"01990000-0000-7000-8000-000000000002","to":["vanthu-center-b@example.vn"]}
]
```

Chỉ dùng hộp thư công vụ đúng Trung tâm đã duyệt. Không chép ví dụ vào production. Mỗi tenant có một entry với 1–50 địa chỉ. Mỗi chuỗi chứa một địa chỉ, không display name, CRLF hoặc khoảng trắng bên ngoài. Địa chỉ so không phân biệt hoa thường không được lặp trong một tenant hoặc giữa hai Trung tâm. Hộp thư chung nhận nhiều Trung tâm cần thiết kế quyền riêng; không lách kiểm tra bằng đổi chữ hoa/thường. Giới hạn 1024 entry, 1 MiB. JSON không có trường thừa/trùng hoặc dữ liệu phía sau array. Giá trị rỗng hoặc [] chặn gửi, không bật người nhận toàn cục.

Các biến SMTP HOST/PORT/USERNAME/PASSWORD/FROM tiếp tục theo cấu hình hiện có. Giữ mật khẩu trong nguồn bí mật hiện hành; không ghi mật khẩu, JSON hoặc địa chỉ vào log. Parser chỉ trả lỗi chung. `AG_SMTP_DIGEST_TO` vẫn phục vụ cấu hình generic notify không nối resolver; worker hiện luôn nối resolver KN nên biến này không thay thế mapping thiếu. Email theo người nhận của Báo giá và tài khoản dùng kênh hiện hữu.

## Áp dụng và kiểm chứng

1. Đối chiếu tenant UUID với hồ sơ Trung tâm và mapping đã duyệt. Không suy tenant từ tên tỉnh hoặc địa chỉ email.
2. Lưu bản cấu hình vận hành trước thay đổi. Đặt JSON đã duyệt trong cấu hình worker. Gói này không đổi realm user hoặc tenant trong DB.
3. Khởi động lại worker theo quy trình triển khai. JSON sai làm `buildNotify` lỗi trước mở pool; JSON rỗng chặn delivery tới khi có mapping. Consumer dựng muộn và job dùng cùng builder, không đọc địa chỉ từ request/sự kiện.
4. Trên staging, dùng hai tenant thử và dữ liệu KN riêng, kiểm hộp thư từng tenant nhận đúng tham chiếu. Tenant thiếu mapping phải giữ pending. Kiểm thử tự động dùng Mailer giả và PostgreSQL, chưa chứng minh SMTP thật đã giao thư.
5. Log job giữ `sent`/`outcome`, không ghi mailbox. Khi thiếu mapping, sửa cấu hình và khởi động lại để xử lý pending. Không đánh dấu sent thủ công hoặc xóa queue để làm mất lỗi.
6. Mỗi lượt xử lý tối đa 200 mục/tenant; phần còn lại xử lý lượt kế tiếp. SMTP lỗi hủy giao dịch và giữ pending. Nếu SMTP đã nhận thư nhưng DB commit thất bại, retry có thể gửi lại như cơ chế cũ; gói này chưa bổ sung xác nhận giao thư để bảo đảm đúng một lần.

## Lùi cấu hình

Giữ resolver, đưa mapping về bản trước đã duyệt hoặc dừng riêng job digest theo quy trình vận hành nếu chưa xác định người nhận. Không chuyển về global fallback trong môi trường nhiều tenant. Hàng chờ giữ nguyên; sửa mapping và khởi động lại để tiếp tục. Không sửa dữ liệu HTX/MKG/MRV, MFA hoặc quyết định PO.
