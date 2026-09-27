# Cài môi trường phát triển

## Cách 1: Docker Compose (Linux, macOS, Windows có Docker Desktop)

```bash
cp deploy/compose/.env.example deploy/compose/.env
```

Điền mật khẩu vào `deploy/compose/.env`, rồi chạy:

```bash
docker compose -f deploy/compose/docker-compose.yml up -d --build
```

Compose dựng PostgreSQL 17 + PostGIS, PgBouncer (transaction mode), RabbitMQ, Valkey, Keycloak (realm `agrigreen`), MinIO. Nó chạy `migrate bootstrap` và `migrate up` trước, rồi khởi động `api-platform`, `outbox-publisher` và `worker`.

> Compose và realm Keycloak **chưa được chạy thử trên máy dựng G0**, vì máy đó không có Docker. CI (`.github/workflows/ci.yml`) là nơi kiểm chứng chính.

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

## Vai DB

Danh sách vai nằm ở [internal/shared/dbadmin/dbadmin.go](../internal/shared/dbadmin/dbadmin.go). Khi thêm module mới:

1. Thêm một vai `<module>_app` vào danh sách đó.
2. `GRANT` quyền trên schema của module trong migration.
3. Mở pool riêng cho module ở lệnh `cmd/` tương ứng (R-05).
