## AUTH-V2-RESET-NOPIN — Tự đặt lại mật khẩu qua email, bỏ PIN Mobile (#255, [ADR-0057](../../adr/0057-tu-dat-lai-mat-khau-qua-email.md))

Căn cứ: BRD 2.0 Báo giá v2.0 FN-CH-00.01 (luồng phụ "Quên mật khẩu?", "Mobile: không có đăng nhập bằng PIN (tạm bỏ)"), HCTH v4.0 FN-05 (luồng phụ "Mobile: không có đăng nhập bằng PIN"). Hai BRD cùng ý; HCTH FN-05 không có mục "Quên mật khẩu?", dùng FN-CH-00.01 (chức năng dùng chung).

| Câu hỏi / mục | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| "5 yêu cầu mỗi giờ cho một tài khoản" | Đếm yêu cầu được xếp gửi trong 60 phút trượt; yêu cầu thứ 6 vẫn trả 202, không gửi (không lộ tài khoản có thật) | `domain.ResetPerHour`, `app.RequestPasswordReset` | Đổi hằng số + test `TestPasswordResetRateLimitFivePerHourWithoutLeak` |
| Giới hạn theo IP | Dồn 5, sau đó 1 yêu cầu/phút mỗi IP (trong bộ nhớ từng bản sao), cộng giới hạn chung 30/phút của `/v1/public`; vượt → 429 như nhau với mọi email | `handler/password_reset.go` | Đổi `resetPerMinute`, `resetBurst` |
| "Liên kết hiệu lực 30 phút" | Tính từ lúc worker sinh mã và gửi email | `domain.ResetLinkTTL` | — |
| Tài khoản nào được gửi | Đang hoạt động (`enabled`), email đã xác minh, có `tenant_id`; tài khoản bị khóa không được gửi và không đổi được mật khẩu | `app.RequestPasswordReset`, `ConfirmPasswordReset` | — |
| Admin MFA sau đặt lại | Không đụng TOTP; lần đăng nhập sau vẫn qua OTP như cũ | Keycloak realm `agrigreen` (browser flow) | — |
| Khóa tạm 15 phút do sai mật khẩu (AC-04) | Đặt lại mật khẩu không xóa khóa tạm | `ConfirmPasswordReset` không gọi `ClearBruteForce` | — |
| Thu hồi phiên | Thu hồi mọi phiên Keycloak trước và sau khi đặt mật khẩu; các liên kết còn mở khác hết dùng | `ConfirmPasswordReset` | — |
| Liên kết trong email | `<AG_WEB_OPS_URL>/login/dat-lai-mat-khau#token=…` (mã sau `#`, trang xóa khỏi thanh địa chỉ ngay khi đọc) | `cmd/worker/accounts.go`, `apps/web-ops/src/app/login/dat-lai-mat-khau/page.tsx` | — |
| Trang "Quên mật khẩu" gốc của Keycloak | Giữ nguyên (luồng `agrigreen password recovery` có OTP, P0-VPS-OPS); web không dẫn tới | `deploy/compose/keycloak/agrigreen-realm.json` | Tắt `resetPasswordAllowed` nếu muốn chỉ còn một đường |
| PIN Mobile | Gỡ màn PIN, trạng thái PIN, provider PinService khỏi `apps/field-app` (App KN); nút "Khóa" thành "Đăng xuất". Không có API PIN ở máy chủ; test khẳng định route PIN 404. App HTX/MKG ngoài BRD KN giữ PIN | `apps/field-app`, `packages/agrigreen_mobile` (`AuthFlow.pins` tùy chọn) | — |
| Dữ liệu PIN cũ | Băm PIN trong kho bí mật của máy giữ nguyên, không đọc, không xóa | `SecureKeys.pin*` | — |
