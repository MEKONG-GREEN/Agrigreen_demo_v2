# Chạy profile `kn` (Khuyến nông độc lập) cục bộ

Gói KN-STANDALONE-10B-PROFILE-CI (#272). Phạm vi, giả định và giới hạn:
[docs/kn/gia-dinh/KN-STANDALONE-10B-PROFILE-CI.md](../kn/gia-dinh/KN-STANDALONE-10B-PROFILE-CI.md). Chỉ dùng cho
Local/Dev và CI, chỉ với dữ liệu tổng hợp.

## 1. Dựng

Cổng máy trùng stack Local/Dev mặc định (5432, 6432, 8080, 8082, 8090, 8180, 3000…): dừng stack mặc định trước. Profile
`kn` là dự án Compose riêng `agrigreen-kn`, có volume DB riêng, không đụng dữ liệu của dự án `agrigreen`.

```sh
sh scripts/gen-dev-secrets.sh        # PowerShell: ./scripts/gen-dev-secrets.ps1 — sinh deploy/compose/.env nếu chưa có
docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.kn.yml up -d --build --wait
```

| Địa chỉ | Dịch vụ |
|---|---|
| http://localhost:3000 | web-ops (client `kn-web`) |
| http://localhost:8080, http://localhost:8082 | Application API, Admin API (MFA) |
| http://localhost:8180 | Keycloak, realm `kn` (issuer `http://localhost:8180/realms/kn`) |

Không có: `api-internal` (8081/8083), `ingest` (8443), `web-internal` (3001), `optimizer`, `edge-sim`, `api-public`,
`integration-gateway`.

## 2. Seed tenant KN mới và kiểm khói

```sh
KEYCLOAK_ADMIN_PASSWORD=$(sed -n 's/^KEYCLOAK_ADMIN_PASSWORD=//p' deploy/compose/.env) go run ./tools/knsmoke
```

Mỗi lần chạy tạo **một tenant mới** (cây TTKN, 5 phòng ban, 8 module ngoài KN tắt, một tài khoản Cấp Tỉnh) rồi kiểm
khói; in từng dòng `--- PASS/FAIL`, thoát mã khác 0 khi có mục hỏng. Lệnh không in mật khẩu, OTP hay token; hai tài
khoản nó tạo dùng mật khẩu ngẫu nhiên không lưu lại, nên không dùng để đăng nhập tay.

## 3. Đăng nhập bằng trình duyệt

Tạo tài khoản quản trị cho tenant vừa seed trong Keycloak Admin Console (http://localhost:8180, tài khoản `admin`, mật
khẩu ở `KEYCLOAK_ADMIN_PASSWORD` của `deploy/compose/.env`), realm `kn` → Users → Add user:

- thuộc tính `tenant_id` = tenant của lần seed (xem ở tài khoản `kn-root-…` do `knsmoke` tạo);
- vai realm `platform_admin`; đặt mật khẩu ở tab Credentials; lần đăng nhập đầu Keycloak bắt cấu hình TOTP.

Sau đó cấp vai KN cho tài khoản thường ở web: Quản trị hệ thống → Tài khoản. Tài khoản do hệ thống tạo nhận email kích
hoạt; profile `kn` không cấu hình SMTP, nên ở máy cục bộ đặt mật khẩu cho tài khoản đó trong Keycloak Admin Console
(tab Credentials) và bỏ các Required actions.

Trang "Quên mật khẩu" của Keycloak tắt ở realm `kn`; liên kết "Quên mật khẩu?" trên web đi luồng của hệ thống
(ADR-0057) và cần SMTP.

## 4. Dọn

```sh
docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.kn.yml down -v
```

## 5. Không cần Docker

Cùng mã kiểm khói chạy trên gốc lắp ráp thật và PostgreSQL tạm (issuer giả thay Keycloak):

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -v ./tools/knsmoke/...
```

Trên CI: job `kn-standalone` của `.github/workflows/ci.yml`.
