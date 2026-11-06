# Nghiệm thu đăng nhập dùng chung theo BRD 2.0 — tự đặt lại mật khẩu, bỏ PIN Mobile (#255)

Căn cứ: Báo giá v2.0 FN-CH-00.01, HCTH v4.0 FN-05 (chức năng dùng chung của Hệ thống Khuyến nông). Thiết kế: [ADR-0057](../adr/0057-tu-dat-lai-mat-khau-qua-email.md). Phần chọn vai trò/phòng ban (AC-01…AC-03) không đổi ở gói này.

| Mã BRD 2.0 | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| FN-CH-00.01 luồng phụ "Quên mật khẩu?" | Nhập email, nhận liên kết đặt lại, tự đặt mật khẩu mới, không qua Admin | `TestPasswordResetHappyPathStoresOnlyHashAndRevokesSessions` (PG thật), `apps/web-ops/src/auth/password-reset.test.tsx`, e2e `kn-shell.spec.ts` | đạt |
| FN-CH-00.01 — liên kết hiệu lực 30 phút | Quá 30 phút → 410 | `TestPasswordResetLinkExpiresAfter30Minutes` | đạt |
| FN-CH-00.01 — dùng một lần | Dùng lại → 410; liên kết cũ hết dùng khi đã đặt bằng liên kết mới | `TestPasswordResetLinkIsSingleUse`, `TestPasswordResetNewLinkUseRetiresOlderLinksAndMailFailureRetries` | đạt |
| FN-CH-00.01 — tối đa 5 yêu cầu mỗi giờ cho một tài khoản | Yêu cầu thứ 6 trong giờ không gửi; phản hồi giống hệt (không lộ email) | `TestPasswordResetRateLimitFivePerHourWithoutLeak`, `TestPasswordResetIPRateLimitSameForAnyEmail` | đạt |
| FN-CH-00.01 — Admin đặt lại xong vẫn xác thực MFA | TOTP giữ nguyên; đặt lại không tạo phiên | `ConfirmPasswordReset` không đụng TOTP; thu hồi mọi phiên (`Logouts` ≥ 2) | đạt |
| Bảo mật (ADR-0057) | Mã sai/bị sửa → 410; DB chỉ giữ SHA-256 của bí mật; gửi lỗi → giao lại | `TestPasswordResetWrongOrTamperedTokenRejected`, `TestResetTokenRoundTripAndTamper` | đạt |
| FN-CH-00.01, FN-05 — "Mobile: không có đăng nhập bằng PIN" | App KN đăng nhập xong vào thẳng; phiên ngoại tuyến 72 giờ; không có route PIN | Flutter `widget_smoke_test.dart`, `packages/agrigreen_mobile/test/auth_test.dart` (nhóm "không có PIN"), `TestPasswordResetPublicAndNoPinRoutes`, `TestOpenAPIHasNoPinPaths` | đạt |
