# Bản vá Keycloak KN v7.7 — gói P1

Đã áp vào `deploy/compose/keycloak/agrigreen-realm.json` (kiểm bởi `cmd/api-platform/realm_test.go`). Người tích hợp chỉ còn hai việc ở `deploy/**` (ngoài quyền của gói P1): gắn theme và cập nhật realm production (`deploy/vps/prepare.sh` sinh từ cùng tệp).

## Đã đổi trong realm

| Mục | Trước | Sau | Căn cứ |
|---|---|---|---|
| Khóa do sai mật khẩu | `permanentLockout: true` (GIS US-LOG-01 AC-3) | `permanentLockout: false`, `failureFactor 5`, `maxDeltaTimeSeconds 900`, `waitIncrementSeconds 900`, `maxFailureWaitSeconds 900`, `bruteForceStrategy LINEAR` → khóa tạm 15 phút, tự mở | FN-CH-00.01, N-10, X-06. **Q-21** (số lần khóa tạm trước khi khóa hẳn): khi khách trả lời, đặt `permanentLockout: true` + `maxTemporaryLockouts: N` |
| Đăng nhập email hoặc SĐT | chỉ username/email | `loginWithEmailAllowed` giữ; pf/accounts tạo tài khoản KN với `username` = SĐT chuẩn hóa `0xxxxxxxxx` (vắng SĐT → email); `duplicateEmailsAllowed: false` | FN-CH-00.01, X-09 |
| Phiên | `ssoSessionIdleTimeout 3600` | realm: idle 14400, max 86400, offline idle/max 259200 (bật); client `agrigreen-web-ops`: idle 14400, max 36000; `agrigreen-field-app`: phiên 86400, ngoại tuyến 259200 (PIN 6 số giữ trong app); `web-internal`, `htx-app`, `mkg-field-app`: idle 3600 như trước | N-09, X-07 |
| Lần đầu đăng nhập | — | không đổi: required action `UPDATE_PASSWORD` qua liên kết kích hoạt (`execute-actions-email`), không gửi mật khẩu tạm; thông điệp `updatePasswordTitle` | FN-CH-00.01 B2, X-04 |
| Vai realm | — | `kn_user` (cổng vào bề mặt KN; vai nghiệp vụ ở sổ gán vai trò pf/accounts) | gap §4.5 |
| Thông điệp `vi`/`en` | khóa = "liên hệ Admin" | `accountTemporarilyDisabledMessage` "… tạm khóa 15 phút …", `invalidUserMessage` "Sai tài khoản hoặc mật khẩu. Sau 5 lần sai, tài khoản bị khóa tạm 15 phút.", `usernameOrEmail` "Email hoặc số điện thoại" | Hình 3a |

GIS US-LOG-01 AC-3 (khóa vĩnh viễn) bị thay bởi BRD v7.7 (mới hơn, gap §4.6) — chủ sản phẩm xác nhận cùng Q-21.

## Việc của người tích hợp

1. **Theme `agrigreen`** (bản tối thiểu ở `docs/brd/kn-p1-keycloak-theme/agrigreen/login/`): gắn vào container Keycloak, vd trong `deploy/compose/docker-compose.yml` dịch vụ `keycloak`:
   `volumes: ["./keycloak:/opt/keycloak/data/import:ro", "../../docs/brd/kn-p1-keycloak-theme/agrigreen:/opt/keycloak/themes/agrigreen:ro"]`
   rồi thêm `"loginTheme": "agrigreen"` vào realm. Chưa đặt `loginTheme` trong realm để Keycloak không cảnh báo khi chưa gắn theme.
2. **"Còn n lần thử"**: template FreeMarker của Keycloak không có số lần sai còn lại; cần SPI authenticator (đọc `BruteForceProtector`) — chưa làm, banner ghi quy tắc "sau 5 lần sai khóa tạm 15 phút" (phương án dự phòng của gap §4.6).
3. **Production**: `deploy/vps/prepare.sh` sinh realm từ tệp này — realm đã nhập (`--import-realm` chỉ nhập khi chưa có) cần áp các thuộc tính trên bằng Admin Console/REST.
