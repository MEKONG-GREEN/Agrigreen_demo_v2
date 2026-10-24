# P0 — PostgreSQL 17: Bullseye/PostGIS 3.5 → Trixie/PostGIS 3.6

## Bắt buộc trước triển khai

Image mới giữ PostgreSQL major 17, dùng `postgis/postgis:17-3.6` (PostGIS 3.6.4, Debian 13 Trixie). Đây không phải thao tác thay image rồi gắn lại volume cũ. Thay OS/glibc/ICU có thể làm sai thứ tự chỉ mục văn bản. Upstream yêu cầu ưu tiên **pg_dump/pg_restore vào database/volume mới**: <https://github.com/postgis/docker-postgis#readme>. Tag `17-3.5` đã đóng băng tháng10/2026; Debian11 LTS hết hạn31/08/2026: <https://www.debian.org/News/2026/20260831>. Chuyển URL apt sang archive chỉ sửa tải gói, không khôi phục hỗ trợ bảo mật; phương án đó đã được thay bằng base còn hỗ trợ.

Entrypoint mới từ chối PGDATA có `PG_VERSION` nhưng thiếu/sai `.agrigreen-runtime-compat`, exit78 trước chạy PostgreSQL. Chỉ hook khởi tạo sạch ghi marker `postgres17-trixie-postgis3.6-v1`. Không có biến môi trường bỏ qua; không tự chép marker vào dữ liệu Bullseye và không dùng `update-postgis.sh`/`ALTER EXTENSION` để thay thế phục hồi logic. Sao lưu vật lý Bullseye/pgBackRest cũ không được đưa thẳng sang image Trixie.

## Trình tự vận hành (chưa chạy trên VPS thật)

1. Chốt cửa sổ bảo trì, dừng luồng ghi/API/worker và ghi lại commit/image digest, phiên bản PG/PostGIS/pgRouting, locale/collation, danh sách DB/role/extension. Giữ nguyên volume Bullseye và image cũ để rollback. Chỉ vận hành được giao quyền mới thực hiện; không xóa volume cũ.
2. Tạo bản sao lưu có mã hóa, quyền tệp hạn chế; chạy thử kiểm bản sao trước chuyển. Dump từng DB bằng `pg_dump --format=custom` trên PostgreSQL17 cũ; dump globals bằng `pg_dumpall --globals-only --no-role-passwords`. Không in nội dung dump, password hay URL có credential. Credential dùng tệp bảo vệ/secret store. Kiểm checksum, số dòng theo tenant, sổ/audit/checksum nghiệp vụ trước/sau.
3. Khởi tạo **volume mới có tên khác** bằng image Trixie. Không chạy `compose down -v`, không gắn volume cũ vào image mới, không nâng PostgreSQL major18. Guard phải tạo marker qua init sạch.
4. Khôi phục globals/roles trước dữ liệu, giữ ownership, grants, SECURITY DEFINER/RLS. Rà các role bootstrap đã có (vd postgres) trước nạp globals để tránh lệnh CREATE ROLE trùng; không dùng `--no-owner` để bỏ qua lỗi. Phục hồi mật khẩu runtime từ secret store, không từ repo. Tạo/restore từng DB bằng `pg_restore --exit-on-error`, dùng cấu hình role phù hợp. Extensions tạo mới trên target3.6; xác nhận PostGIS/pgRouting và các hàm app dùng tương thích.
5. Vì phục hồi logic xây lại chỉ mục trên OS mới, đối chiếu collation thực tế không có mismatch, chỉ mục text/GiST hợp lệ, truy vấn spatial/routing đúng. Chạy migration/fixture/RLS/tenant tests và smoke theo app trên bản phục hồi; kiểm ledger không đổi, số dòng + checksum + golden Excel. Không coi harness tối thiểu thay kiểm dữ liệu nghiệp vụ thật.
6. Chỉ chuyển API/worker sang DB mới sau bằng chứng đủ. Ghi digest/volume mới để rollback rõ ràng. Nếu cần rollback, dừng luồng ghi mới trước và chuyển về bộ image+volume Bullseye nguyên trạng trong cửa sổ đã duyệt; không trộn vật lý hai OS. Sau khi chấp thuận và hết hạn giữ, việc dọn dữ liệu cũ là tác vụ vận hành riêng.

## Diễn tập CI thực

`sh scripts/test-postgres-logical-upgrade.sh` tạo hai volume riêng, khởi tạo old `17-3.5` bằng user postgres, fixture có text tiếng Việt/ICU, Point/GiST, role/grant/RLS hai tenant. New entrypoint phải từ chối read-only volume cũ exit78. Script pg_dump/pg_restore vào NEW target17-3.6 và kiểm version3.6, dữ liệu/hình học, RLS, chỉ mục text và collation. Đây là ca tương thích tối thiểu, không phải diễn tập dữ liệu VPS thực.

`test-postgres-images.sh backup` kiểm full backup mã hóa và phục hồi vật lý **cùng image/OS mới**, rồi kiểm marker tồn tại và dữ liệu đọc được. Hai Docker context có cùng guard/hook và CI đối chiếu bằng cmp. Toàn bộ container smoke `--network none`, không publish port, dùng dữ liệu giả; chỉ xóa volume mà chính script vừa tạo. Mật khẩu kho backup ngẫu nhiên không in ra.
