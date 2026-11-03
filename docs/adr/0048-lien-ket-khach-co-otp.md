# ADR-0048: Liên kết khách có OTP, capability tách khỏi danh tính KN

- Trạng thái: chấp nhận khung dormant; chưa bật luồng khách HCTH.
- Gói: #184 W10.18-GUEST-OTP; tích hợp còn lại: #200 W10.18-GUEST-INTEGRATION.
- Căn cứ: BRD HCTH v3.0 FN21 BR03/BR06, AC21-05; ADR-0037, ADR-0050; ma trận W10.18/Q021.

## Quyết định

`pf/guestlink` cung cấp capability có phạm vi tenant, owner, một chủ thể, một tài nguyên và một bước. Nó không tạo Principal, JWT, vai trò hay phiên Keycloak; MFA và quyền cấp/thu hồi vẫn do owner kiểm tra trên Principal gốc. Registry đăng ký module; factory assembly riêng luôn dùng Config tắt. Không đổi constructor/route HRM: endpoint guest-link hiện hành tiếp tục từ chối khi chưa có adapter thật.

Tenant được ghim bởi cấu hình instance đáng tin, không chọn từ token/header. Owner trả thời hạn, trạng thái khóa, revision của bước và attestation liên hệ đã xác minh. Có địa chỉ email không đồng nghĩa đã xác minh. Khi khóa/mở lại, thay đổi ánh xạ chủ thể hoặc liên hệ/attestation, owner phải đổi revision; write của owner phải kiểm tra lại revision/khóa trong chính giao dịch ghi dữ liệu của owner. Khung chung chỉ trả Binding, không viết dữ liệu HRM hay HTX.

Token link/session dùng HMAC-SHA256 với khóa máy chủ 32 byte, purpose riêng, nonce UUIDv7 ngẫu nhiên và metadata bất biến; token gồm tenant định tuyến và giá trị PRF 256 bit. DB chỉ giữ hash có khóa và tham chiếu; metadata được xác thực lại trước sử dụng. OTP sáu chữ số được dẫn xuất riêng theo grant/revision/challenge; so sánh HMAC kích thước cố định. Session buộc thời hạn vào proof và không gia hạn theo lượt dùng. Token/OTP/liên hệ/IP thô không lưu ledger hay in bằng fmt/JSON/slog.

Hai ledger append-only có RLS tenant và role module riêng. Khóa advisory theo thứ tự cố định bảo vệ cấp lặp, xoay link, quota và tiêu thụ OTP. Lỗi nghiệp vụ phải commit audit/lượt thử; lỗi DB rollback và trả unavailable. Owner/transport I/O luôn ngoài giao dịch capability, được kiểm bằng callback I/O thực trên pool một connection.

## Chính sách ban đầu cần PO xác nhận

Mặc định: link tối đa 24 giờ và bị chặn bởi thời hạn owner; OTP 5 phút, session 15 phút; không kéo dài khi dùng. Tối đa 10 mở/link, 5 lần xác minh/grant (gồm cả thành công, gửi lại không đặt lại), 3 challenge/link, cooldown 60 giây; cửa sổ IP và chủ thể 15 phút, mỗi cửa sổ 30 yêu cầu. Cấu hình chỉ có thể nằm trong biên an toàn đã kiểm tra. Lựa chọn 5 thử/10 mở theo Q021; chưa phải xác nhận PO chính thức. Session mới thay session cũ. Lỗi khóa/hết hạn/revision được ghi thu hồi vĩnh viễn; quay ngược đồng hồ không hồi sinh.

Sender chỉ báo Sent khi adapter xác nhận thực; Disabled/Dropped không xác minh được. Retry giữ challenge/khóa delivery ổn định; adapter phải có durable dedup theo khóa. Lát này chỉ định nghĩa port và double kiểm thử email; không có SMTP/SMS thật, không nhận cấu hình giả để bật dịch vụ. Kết quả gửi được ghi sau transport, kể cả owner vừa khóa; bước xác minh vẫn kiểm tra owner/revision mới.

## Ranh giới bàn giao

#200 phải thực hiện attestation liên hệ, sender/outbox email có durable dedup, adapter owner HRM, khóa nguyên tử khi submit, thu hồi theo revision, route public/session cookie/CSRF/referrer/trusted IP, chống enumeration/timing và E2E nghiệp vụ thật. SMS chỉ sau cấu hình/đồng ý phù hợp. Chưa có các phần này thì không công bố AC21-05 hoàn thành hoặc bật endpoint. Các test double ở #184 không thay thế bằng chứng owner/transport sản xuất.

Tham khảo: [OWASP Forgot Password](https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html), [OWASP Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html), [Go crypto/hmac](https://pkg.go.dev/crypto/hmac). Đây là cơ sở thiết kế, không tuyên bố chứng nhận hay mức bảo đảm MFA của email.
