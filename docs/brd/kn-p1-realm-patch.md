# Bản vá Keycloak KN v7.7 — gói P1

Đã áp vào `deploy/compose/keycloak/agrigreen-realm.json` (kiểm bởi `cmd/api-platform/realm_test.go`). Theme đăng nhập đã gắn (gói KN-fix); còn lại: SPI "còn n lần thử" và cập nhật realm production đã nhập (`deploy/vps/prepare.sh` sinh từ cùng tệp).

## Đã đổi trong realm

| Mục | Trước | Sau | Căn cứ |
|---|---|---|---|
| Khóa do sai mật khẩu | `permanentLockout: true` (GIS US-LOG-01 AC-3) | **A3 / Q-21 đã chốt 03/10/2026**: `bruteForceStrategy MULTIPLE`, `failureFactor 5`, `waitIncrementSeconds 900`, `maxFailureWaitSeconds 900` → mỗi 5 lần sai khóa tạm 15 phút, tự mở; `permanentLockout: true` + `maxTemporaryLockouts: 3` → sau 3 lần khóa tạm, lần khóa kế tiếp là khóa hẳn (Keycloak 26.3 đặt `enabled = false`, thông điệp "liên hệ Admin", Admin mở qua `POST /v1/accounts/users/{id}/unlock`); `maxDeltaTimeSeconds 86400` = cửa sổ đếm 24 giờ (Keycloak xóa bộ đếm lần sai và số lần khóa tạm khi lần sai kế tiếp cách lần trước > 24 giờ hoặc khi đăng nhập thành công). Trước đó (tạm): `permanentLockout: false`, `maxDeltaTimeSeconds 900`, `LINEAR` | FN-CH-00.01, N-10, X-06, Q-21 (= A3) |
| Đăng nhập email hoặc SĐT | chỉ username/email | `loginWithEmailAllowed` giữ; pf/accounts tạo tài khoản KN với `username` = SĐT chuẩn hóa `0xxxxxxxxx` (vắng SĐT → email); `duplicateEmailsAllowed: false` | FN-CH-00.01, X-09 |
| Phiên | `ssoSessionIdleTimeout 3600` | realm: idle 14400, max 86400, offline idle/max 259200 (bật); client `agrigreen-web-ops`: idle 14400, max 36000; `agrigreen-field-app`: phiên 86400, ngoại tuyến 259200 (PIN 6 số giữ trong app); `web-internal`, `htx-app`, `mkg-field-app`: idle 3600 như trước | N-09, X-07 |
| Lần đầu đăng nhập | — | không đổi: required action `UPDATE_PASSWORD` qua liên kết kích hoạt (`execute-actions-email`), không gửi mật khẩu tạm; thông điệp `updatePasswordTitle` | FN-CH-00.01 B2, X-04 |
| Vai realm | — | `kn_user` (cổng vào bề mặt KN; vai nghiệp vụ ở sổ gán vai trò pf/accounts) | gap §4.5 |
| Thông điệp `vi`/`en` | khóa = "liên hệ Admin" | `accountTemporarilyDisabledMessage` "… tạm khóa 15 phút …", `invalidUserMessage` "Sai tài khoản hoặc mật khẩu. Sau 5 lần sai, tài khoản bị khóa tạm 15 phút; bị khóa tạm 3 lần trong 24 giờ thì khóa hẳn (liên hệ Admin).", `usernameOrEmail` "Email hoặc số điện thoại" | Hình 3a |

GIS US-LOG-01 AC-3 (khóa vĩnh viễn) bị thay bởi BRD v7.7 (mới hơn, gap §4.6); chủ sản phẩm chốt Q-21 ngày 03/10/2026 (A3, `docs/cau-hoi-ton-dong.md`): khóa tạm, khóa hẳn sau 3 lần khóa tạm trong 24 giờ.

## Theme đăng nhập `agrigreen` (đã gắn — gói KN-fix)

- Thư mục theme: `deploy/compose/keycloak-themes/agrigreen/login/` (chuyển từ `docs/brd/kn-p1-keycloak-theme/`): `theme.properties` (kế thừa `keycloak`, PKCE giữ nguyên, form mật khẩu vẫn ở Keycloak), `messages/messages_vi.properties`, `resources/css/agrigreen.css`.
- Compose: dịch vụ `keycloak` trong `deploy/compose/docker-compose.yml` gắn `./keycloak-themes/agrigreen:/opt/keycloak/themes/agrigreen:ro`. `docker-compose.vps.yml` chỉ ghi đè mount `/opt/keycloak/data/import` (realm sinh bởi `prepare.sh`) nên kế thừa mount theme.
- Realm: `"loginTheme": "agrigreen"` trong `deploy/compose/keycloak/agrigreen-realm.json` (VPS sinh từ cùng tệp). `cmd/api-platform/realm_test.go` kiểm `loginTheme`, sự có mặt của `theme.properties` và dòng mount trong compose.
- K8s: Keycloak chạy bằng chart riêng (namespace `agrigreen-iam`, `deploy/k8s/charts/agrigreen` chỉ có NetworkPolicy/Ingress tới nó) — khi dựng chart Keycloak, đóng theme vào image (`COPY deploy/compose/keycloak-themes/agrigreen /opt/keycloak/themes/agrigreen` rồi `kc.sh build`) hoặc gắn ConfigMap/volume cùng đường dẫn.

## Việc còn lại

1. **"Còn n lần thử"**: template FreeMarker của Keycloak không có số lần sai còn lại; cần SPI authenticator (đọc `BruteForceProtector`) — chưa làm, banner ghi quy tắc "sau 5 lần sai khóa tạm 15 phút" (phương án dự phòng của gap §4.6).
2. **Production đã chạy**: `--import-realm` chỉ nhập khi realm chưa có — realm đã nhập cần áp các thuộc tính trên (kể cả `loginTheme` và bộ chống dò A3: `bruteForceStrategy`, `maxDeltaTimeSeconds`, `permanentLockout`, `maxTemporaryLockouts`) bằng Admin Console/REST (Realm settings › Themes › Login theme = `agrigreen`).
