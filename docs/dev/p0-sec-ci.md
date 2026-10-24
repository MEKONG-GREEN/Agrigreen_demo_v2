# P0-SEC-CI — CI và image PostgreSQL (#132)

## Phạm vi

- CI chạy trên push `dev`/`main`, PR vào `dev`/`main`, và lệnh chạy tay. Giữ nguyên điều kiện chỉ phát hành/ký image trên push `main`; không phát hành từ PR hoặc `dev`. Các bước scanner không bị bỏ qua. Cổng Trivy của image PostgreSQL vốn chỉ báo cáo được đổi sang exit 1, cùng ngưỡng CRITICAL/HIGH có bản vá như image khác.
- Mức Go tối thiểu và toolchain là 1.26.10; Docker build dùng cùng phiên bản. Release chính thức ngày 19/10/2026 sửa crypto/x509: <https://go.dev/doc/devel/release#go1.26.10>.
- Excelize ghim `v2.11.1-0.20261020033641-611b222fd8f7` (commit upstream 20/10), vì chưa có thẻ 2.11.1 tại lúc chọn bản vá. Gỡ toàn bộ chín suppress Excelize khỏi `.trivyignore`; không coi govulncheck im lặng là thay thế cho Trivy/GHSA. Nguồn advisory upstream: <https://github.com/qax-os/excelize/security/advisories>.
- Image `deploy/compose/postgres` và `backup` mặc định chạy `postgres:postgres`, có HEALTHCHECK `pg_isready`; tạo sẵn thư mục dữ liệu/socket thuộc postgres khi build.

## Volume và kiểm thật

Named volume mới lấy ownership của thư mục trong image. Volume có sẵn phải thuộc UID/GID postgres của image; không cấp root cho dịch vụ để tự sửa quyền, không xóa dữ liệu. Kiểm tra ownership trước deploy; nếu sai thì vận hành dừng DB và sửa quyền volume có kiểm soát theo runbook VPS.

`sh scripts/test-postgres-images.sh postgres` và `... backup` build image trên Linux/Docker thật, xác nhận Config.User và PID1 khác root, khởi tạo volume mới, cài PostGIS/pgRouting, HEALTHCHECK healthy, ghi/đọc sau restart. Biến thể backup còn tạo stanza, check, full backup mã hóa và khôi phục vào volume sạch, kiểm bản ghi qua PostgreSQL thật. Mật khẩu kho mã hóa ngẫu nhiên chỉ ở envfile tạm; không in env/inspect/log chứa credential. Trust auth chỉ ở container smoke `--network none`, không publish cổng và không sửa cấu hình production.

Workflow độc lập `.github/workflows/postgres-images.yml` chạy matrix hai image trên PR/push vào dev/main khi đổi tệp liên quan, và hỗ trợ workflow_dispatch. Đạt cú pháp YAML/Dockerfile không thay thế kết quả runtime. Windows hiện không có Docker/WSL; chỉ ghi PASS runtime khi có run Linux hoàn tất. Toàn CI cũng phải xanh trước tích hợp, không suy image smoke xanh thành cả CI xanh.

## Bằng chứng

Kết quả kiểm và commit chính xác được ghi trên issue #132 trước ready. Không thay mã KN, API hay migration. Không sửa main hoặc đưa khóa thật vào repo.
