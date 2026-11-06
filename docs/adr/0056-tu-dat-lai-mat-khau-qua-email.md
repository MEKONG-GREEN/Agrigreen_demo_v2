# ADR-0056: Tự đặt lại mật khẩu qua email, bỏ PIN Mobile

**Trạng thái:** Chấp nhận — gói #255 AUTH-V2-RESET-NOPIN. **Ngày:** 06/11/2026.
**Căn cứ:** BRD 2.0 — Báo giá v2.0 FN-CH-00.01 (luồng phụ “Quên mật khẩu?”, “Mobile: không có đăng nhập bằng PIN”), HCTH v4.0 FN-05 (luồng phụ “Mobile: không có đăng nhập bằng PIN”).
**Liên quan:** [ADR-0010](0010-kenh-nong-ho.md) (không gửi mật khẩu tạm), [ADR-0041](0041-mfa-theo-nhom-d24.md) (TOTP cán bộ KN), [ADR-0048](0048-lien-ket-khach-co-otp.md) (mẫu mã một lần), liên kết tải Chi cục của pf/reports (mẫu mã trong email + consumer gửi ngoài giao dịch).

## Bối cảnh

BRD 2.0: người dùng nhập email, nhận liên kết đặt lại **hiệu lực 30 phút, dùng một lần, tối đa 5 yêu cầu mỗi giờ cho một tài khoản**, tự đặt mật khẩu mới không qua Admin; tài khoản Admin đặt lại xong vẫn phải xác thực MFA. Mobile bỏ đăng nhập bằng PIN; phiên đã đăng nhập dùng tiếp tối đa 72 giờ, bảo vệ bằng khóa màn hình thiết bị.

Mật khẩu chỉ nằm trong Keycloak (realm `agrigreen`). Trang “Quên mật khẩu” sẵn có của Keycloak không giới hạn số yêu cầu theo tài khoản và không bắt buộc thu hồi phiên (ô “Đăng xuất khỏi thiết bị khác” người dùng bỏ chọn được), nên không đủ BRD.

## Quyết định

1. **pf/accounts giữ luồng**, hai route công khai trên Application API (nhóm `/v1/public`, có giới hạn theo IP):
   - `POST /v1/public/password-reset` `{email}` → luôn `202` cùng một thân cho mọi email đúng cú pháp, **không lộ email có tồn tại hay không**. Chỉ tài khoản đang hoạt động, có email đã xác minh, có `tenant_id` mới được xếp gửi.
   - `POST /v1/public/password-reset/confirm` `{token, password}` → `204`; liên kết sai/hết hạn/đã dùng → một mã chung `410 accounts.reset_link_invalid`; mật khẩu không đạt chính sách realm → `422 accounts.password_policy` (liên kết vẫn dùng lại được).
2. **Mã đặt lại**: `tenant_id ‖ reset_id ‖ 32 byte ngẫu nhiên` (crypto/rand, 256 bit bí mật), base64url 86 ký tự — cùng dạng liên kết tải Chi cục. DB chỉ giữ **SHA-256 của phần bí mật**; so sánh thời gian hằng. Mã nằm ở phần `#` của URL trang web nên không vào log máy chủ hay Referer.
3. **Dùng một lần, hết hạn 30 phút** tính từ lúc gửi. Xác nhận nhận việc bằng lease ngắn (`claimed_until`) trong giao dịch, gọi Keycloak ngoài giao dịch, rồi ghi `used_at`; đổi thành công thì mọi liên kết còn mở khác của tài khoản cũng hết dùng.
4. **Giới hạn tần suất**: tối đa 5 yêu cầu được xếp gửi mỗi giờ cho một tài khoản (đếm trong DB, khóa advisory theo tài khoản); yêu cầu thứ 6 vẫn trả `202` và không gửi — để không phân biệt được tài khoản có thật. Theo IP: bộ giới hạn bộ nhớ của route (ngoài giới hạn chung của `/v1/public`); vượt → `429` như nhau với mọi email. Không lưu email hay IP thô: bảng chỉ có `user_ref`.
5. **Gửi mail qua outbox**: giao dịch yêu cầu chỉ ghi dòng `password_reset` + sự kiện nội bộ `pf.accounts.password_reset.requested` (không có mã, không PII). Consumer ở cmd/worker sinh mã, ghi băm trong giao dịch ngắn, đọc email ở Keycloak và **gửi SMTP ngoài mọi giao dịch DB** (như pf/reports), rồi ghi `sent_at`. Thời gian phản hồi API không phụ thuộc việc gửi mail. Gửi lỗi → sự kiện được giao lại, mã mới thay mã chưa gửi.
6. **Thu hồi phiên**: xác nhận đặt lại gọi Keycloak `logout` (mọi phiên) trước và sau khi đặt mật khẩu (`reset-password`, `temporary=false`); chính sách mật khẩu của realm do Keycloak kiểm. Không đụng TOTP, không xóa khóa tạm do đăng nhập sai: tài khoản bắt MFA vẫn phải nhập OTP ở lần đăng nhập sau (BRD), khóa 15 phút (AC-04) vẫn giữ.
7. **Không in mã/mật khẩu ra log**: log chỉ có `tenant_id`, `reset_id`, `user_ref`, `outcome`. Mật khẩu chỉ đi qua bộ nhớ tới Keycloak Admin REST qua TLS.
8. **Bỏ PIN Mobile (app KN `apps/field-app`)**: gỡ màn PIN, trạng thái PIN và nhà cung cấp PinService; sau đăng nhập trực tuyến vào thẳng ứng dụng, phiên ngoại tuyến 72 giờ giữ nguyên. Không có API PIN ở máy chủ (PIN chỉ là dữ liệu cục bộ trên máy); test route khẳng định không có đường dẫn PIN nào (404). App HTX/MKG ngoài phạm vi BRD KN giữ nguyên `PinService` trong package dùng chung. Dữ liệu PIN cũ trên máy (băm trong Keystore) không xóa, không còn được đọc.

## Hệ quả

- Web: “Quên mật khẩu?” dẫn tới `/login/quen-mat-khau` (nhập email) và email dẫn tới `/login/dat-lai-mat-khau#token=…`; không còn câu “mật khẩu do quản trị viên đặt lại”.
- Cấu hình worker: `AG_ACCOUNTS_RESET_URL` (URL trang đặt lại, https), SMTP `AG_SMTP_*` của pf/notify. Thiếu → consumer lỗi, sự kiện vào DLQ (không mất yêu cầu, không gửi giả).
- Trang “Quên mật khẩu” gốc của Keycloak vẫn bật (luồng `agrigreen password recovery` có OTP trước khi đặt lại, P0-VPS-OPS); web không dẫn tới đó. Tắt hẳn là quyết định cấu hình realm riêng.
- Bộ giới hạn theo IP nằm trong bộ nhớ từng bản sao; giới hạn theo tài khoản nằm trong DB nên đúng trên nhiều bản sao.
