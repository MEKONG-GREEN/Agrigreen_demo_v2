# Cài môi trường phát triển

## Cách 1: Docker Compose — toàn bộ nền tảng (Linux, macOS, Windows có Docker Desktop)

1. **Sinh bí mật** vào `deploy/compose/.env` (đã có trong `.gitignore`, không bao giờ commit; script không in giá trị ra màn hình):
   ```powershell
   .\scripts\gen-dev-secrets.ps1          # Windows
   ```
   ```bash
   sh scripts/gen-dev-secrets.sh          # Linux, macOS, CI
   ```
   Chạy lại khi đã có `.env` thì script giữ nguyên tệp. `-Force` / `--force` sinh lại — khi đó chạy `docker compose ... down -v` trước, vì mật khẩu DB đã lưu trong volume.
   Script cũng gọi `scripts/gen-dev-certs` (`.ps1` / `.sh`, cần `openssl` — Git for Windows có sẵn) để sinh chứng thư mTLS dev của bề mặt Internal (api-platform ↔ optimizer) vào `deploy/compose/.certs/` (gitignore). Thiếu tệp thì `docker compose` báo lỗi bind mount.
2. **Dựng và chờ mọi dịch vụ sẵn sàng:**
   ```bash
   docker compose -f deploy/compose/docker-compose.yml up -d --build --wait
   ```
3. **Kiểm:** `curl http://127.0.0.1:8080/readyz`, `curl http://127.0.0.1:8090/readyz`, `curl http://127.0.0.1:8081/readyz`, mở http://localhost:3000.

| Dịch vụ | Vai trò | Địa chỉ trên máy (chỉ 127.0.0.1) |
|---|---|---|
| `postgres` | PostgreSQL 17 + PostGIS | 5432 |
| `pgbouncer` | transaction mode (R-30); ứng dụng đi qua đây | 6432 |
| `migrate-bootstrap`, `migrate-up` | chạy một lần: tạo vai/database, áp migration | — |
| `rabbitmq` | outbox → consumer | 5672, console 15672 |
| `valkey` | cache (không phải nguồn sự thật) | 6379 |
| `keycloak` | realm `agrigreen` (nhập từ `deploy/compose/keycloak/agrigreen-realm.json`) | 8180 |
| `seaweedfs`, `s3-init` | kho S3 (SeaweedFS — MinIO đã ngừng phát hành image công khai); `s3-init` tạo bucket, thử bật versioning; tài khoản ứng dụng `agrigreen-app` chỉ Read/Write/List trên bucket | 9000 |
| `otel-collector` | nhận OTLP, in ra log (exporter `debug`) | 4317, 4318 |
| `identity-vault` | kho định danh (bề mặt Internal) | 8090 |
| `api-platform` | Application API + Admin API (MFA); Internal API `/internal/v1` cho optimizer (mTLS, chỉ trong mạng compose) | 8080, 8082 (8084 không publish) |
| `api-internal` | API nội bộ MKG (cụm `ag_mkg`, vai `hub_app` + `mkg_api` cho idempotency) | 8081, quản trị MFA 8083 |
| `optimizer` | engine mô phỏng chọn Hub (Python): queue `pf.simulation.optimizer`, gọi Internal API qua mTLS | — |
| `outbox-publisher`, `worker` | phát outbox, consumer + job vận hành (neo chuỗi, dọn idempotency, leo thang SLA) | — |
| `web-ops` | web vận hành Next.js | 3000 |

Ghi chú:
- **Mỗi dịch vụ chỉ nhận biến nó cần** (không có `env_file` chung). `api-platform` không có mật khẩu vai `vault_app`, KEK hay khóa ký neo.
- **Container ứng dụng** chạy `read_only`, `cap_drop: [ALL]`, `no-new-privileges`.
- **Issuer OIDC** là `http://localhost:8180/realms/agrigreen`, tức địa chỉ trình duyệt thấy. Container không gọi được `localhost:8180`, nên `api-platform` và `identity-vault` lấy khóa ký qua `AG_OIDC_JWKS_URL=http://keycloak:8080/...`, vẫn so issuer và audience (R-54).
- **Người dùng demo:** `.\scripts\dev-users.ps1 [-TenantId <uuid>] [-OrgRef <uuid>]` (hoặc `sh scripts/dev-users.sh`) tạo `dev-admin`, `dev-accounts`, `dev-viewer` với mật khẩu ngẫu nhiên in ra MỘT LẦN; vai [MFA] phải cấu hình TOTP ở lần đăng nhập đầu. `cmd/seed-synthetic` lấy token bằng `-kc-token-url … -kc-user dev-admin -kc-otp <6 số>` + `AG_SEED_KC_PASSWORD` qua client **chỉ-dev** `agrigreen-seed` (direct access grants — không bao giờ tạo ở staging/production). Tài khoản cán bộ thật tạo qua Admin API `/v1/accounts/users` (pf/accounts, tài khoản dịch vụ `agrigreen-accounts`, `AG_KC_*`).
- **Realm chưa có người dùng.** Tạo trong console Keycloak (http://localhost:8180, tài khoản `admin`, mật khẩu `KEYCLOAK_ADMIN_PASSWORD` trong `.env`). Đặt thuộc tính `tenant_id` (UUID), `org_ref` (UUID tổ chức trong pf/party) và gán vai. Vai ghi `[MFA]` trong mô tả chỉ dùng được trên Admin API sau khi cấu hình TOTP.
- **CORS:** `AG_CORS_ORIGINS` mặc định `http://localhost:3000,http://127.0.0.1:3000`. So khớp chính xác; không chấp nhận `*`.
- Xem trace và metric: `docker compose -f deploy/compose/docker-compose.yml logs -f otel-collector`.
- Dọn toàn bộ, kể cả dữ liệu: `docker compose -f deploy/compose/docker-compose.yml down -v`.
- **Sao lưu và diễn tập phục hồi:** tệp chồng `deploy/compose/docker-compose.backup.yml` (pgBackRest + WAL, kho S3 thứ hai với danh tính `backup-writer`/`restore-reader`, profile `backup`/`restore`). Xem [runbooks/backup.md](runbooks/backup.md) và [runbooks/restore-drill.md](runbooks/restore-drill.md).

> **Stack Compose chưa được chạy trên máy dựng** (Windows, không có Docker). Tệp đã được kiểm cú pháp và soát kỹ. Job CI `compose-smoke` (`.github/workflows/ci.yml`) là nơi kiểm chứng: dựng toàn bộ bằng `up --wait`, gọi `/healthz`, `/readyz` của `api-platform`, `identity-vault` và trang web, rồi `down -v`.

## Cách 2: Windows không Docker (cách đã dùng để dựng G0)

1. **Go:** giải nén bản zip chính thức từ go.dev vào `%LOCALAPPDATA%\Programs\go`, rồi thêm `...\go\bin` vào PATH của người dùng. Cách này không cần quyền quản trị.
2. **PostgreSQL 17:** giải nén bản zip binaries của EnterpriseDB vào `%LOCALAPPDATA%\Programs\pgsql`. Chép thêm bộ `postgis-bundle-pg17-*.zip` từ download.osgeo.org vào cùng thư mục.
3. **Khởi tạo cụm.** Mật khẩu superuser lưu trong tệp **ngoài repo**:
   ```powershell
   $root = "$env:LOCALAPPDATA\agrigreen-pg"
   & "$env:LOCALAPPDATA\Programs\pgsql\bin\initdb.exe" -D "$root\data" -U postgres --pwfile="$root\superuser.pw" -A scram-sha-256 -E UTF8 --locale=C
   & "$env:LOCALAPPDATA\Programs\pgsql\bin\pg_ctl.exe" -D "$root\data" -l "$root\postgres.log" start
   ```
4. **Đặt biến môi trường cho phiên PowerShell, rồi chạy kiểm thử:**
   ```powershell
   . .\scripts\dev-env.ps1
   go test ./...
   ```

## Biến môi trường

| Biến | Dùng cho |
|---|---|
| `AG_TEST_PG_ADMIN_URL` | Kiểm thử tích hợp: tài khoản quản trị cụm, dùng để tạo vai và database tạm |
| `AG_TEST_PGBOUNCER_URL` | Chạy R-32 qua PgBouncer transaction mode (CI bắt buộc) |
| `AG_DB_APP_PASSWORD` / `AG_DB_PASSWORD_<VAI>` | Mật khẩu vai DB. Không có mặc định trong mã |
| `AG_PG_ADMIN_URL`, `AG_DB_URL`, `AG_PF_DB_URL` | Lệnh `migrate` và `api-platform` |
| `AG_OIDC_ISSUER`, `AG_OIDC_AUDIENCE` | Xác minh token |
| `AG_AMQP_URL` | `outbox-publisher`, `worker` |
| `AG_OIDC_JWKS_URL` | (tùy chọn) `api-platform`, `identity-vault`: lấy khóa ký từ URL này thay vì discovery khi issuer công khai khác địa chỉ nội bộ |
| `AG_CORS_ORIGINS` | `api-platform`: origin web được gọi chéo, phân tách bằng dấu phẩy, khớp chính xác |
| `AG_VAULT_URL`, `AG_SYNC_CURSOR_KEY`, `AG_S3_*` | `api-platform` (kho định danh, cursor đồng bộ, kho đối tượng) |

## Vai DB

Danh sách vai nằm ở [internal/shared/dbadmin/dbadmin.go](../internal/shared/dbadmin/dbadmin.go). Khi thêm module mới:

1. Thêm một vai `<module>_app` vào danh sách đó.
2. `GRANT` quyền trên schema của module trong migration.
3. Mở pool riêng cho module ở lệnh `cmd/` tương ứng (R-05).
