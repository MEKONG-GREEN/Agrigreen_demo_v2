# Chuyển bộ chạy migration sang v2 (W7.1, ADR-0038)

Bộ chạy v2 (`internal/shared/migrator`) ghi lịch sử migration theo module (cột `module` của `shared.schema_migrations`; dời vào schema module là bước 2), áp theo thứ tự topo module và áp mọi tệp **vắng trong sổ** — tệp có số nhỏ hơn số đã áp không còn bị bỏ qua im lặng như `golang-migrate`. Quyết định: [ADR-0038](../adr/0038-bo-chay-migration-theo-module.md).

| Biến | Giá trị | Ý nghĩa |
|---|---|---|
| `AG_MIGRATE_RUNNER` | rỗng / `v1` (mặc định) | `golang-migrate`, như trước |
| | `v2` | bộ chạy theo module |

Lệnh: `migrate up` (theo `AG_MIGRATE_RUNNER`), `migrate verify-runner` (chỉ đọc, đối chiếu hai trạng thái). Cả hai dùng `AG_DB_URL`, `AG_CLUSTERS`, vai `ag_migrator` nối thẳng như các lệnh `migrate` khác.

## Lịch chuyển

1. **Bản phát hành có W7.1** (mặc định v1): chạy `migrate bootstrap` (tạo vai `own_<module>`) và `migrate up` như thường. Sau đó chạy `migrate verify-runner` ở mọi môi trường — phải in `chưa chuyển sang v2 …` và **không** có dòng `LỆCH`.
2. **Chuyển từng môi trường** (dev → UAT → production), trong cửa sổ migration bình thường:
   1. Sao lưu như trước mọi `migrate up` ([backup.md](backup.md)).
   2. Kiểm tay giới hạn đã biết (ADR-0038 §4): tệp nào `golang-migrate` đã bỏ qua im lặng TRƯỚC lần chuyển sẽ bị ghi sổ như đã áp. Kế hoạch v4.1 cấp số theo thứ tự gộp nên không có tệp như vậy; nếu nghi ngờ, đối chiếu ngày gộp của tệp số nhỏ với lần `migrate up` đầu tiên có số lớn hơn.
   3. `AG_MIGRATE_RUNNER=v2 migrate up`. Lần đầu in `ghi sổ N tệp đã áp bởi golang-migrate (không chạy lại)` cho mỗi cụm, rồi áp tệp mới (nếu có).
   4. `migrate verify-runner` → `bộ chạy migration: v1 và v2 khớp`; `migrate verify` → `cổng catalog: sạch`.
   5. Đặt `AG_MIGRATE_RUNNER=v2` cố định cho job migrate của môi trường (chart: biến môi trường của Job migrate; Compose: dịch vụ `migrate`).
3. **Bản phát hành sau:** đổi mặc định sang v2. **Bản tiếp theo:** bỏ v1 (golang-migrate) khỏi `dbadmin`; bảng `schema_migrations_<thư mục>` của v1 giữ lại (không xóa dữ liệu) cho tới khi không còn đường lùi.

## Quay về v1

Trong kỳ chuyển, v2 luôn cập nhật bảng `schema_migrations_<thư mục>` của v1 bằng số lớn nhất đã áp, nên bỏ `AG_MIGRATE_RUNNER` (về v1) không chạy lại tệp nào. Lần v2 sau ghi sổ các tệp v1 đã áp trong lúc đó (`method = import`). Ngoại lệ: tệp ngoài thứ tự (số nhỏ hơn) chỉ v2 áp được — v1 bỏ qua; `verify-runner` báo `LỆCH … v1 bỏ qua im lặng`.

## Xem trạng thái

```sql
-- mọi sổ của database (chạy bằng ag_migrator hoặc quản trị cụm)
SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE c.relname = 'schema_migrations' AND obj_description(c.oid, 'pg_class') LIKE 'ag:no-rls: sổ migration theo module%';
SELECT * FROM shared.schema_migrations WHERE module = 'pf/party' ORDER BY version;
```

Bảng gắn tệp → module → sổ: `go run ./tools/migratecheck -modules migrations`; thứ tự áp các tệp của một module và kiểm riêng chúng: `go run ./tools/migratecheck -module pf/party migrations`.

## Sự cố

| Thông báo | Nguyên nhân | Xử lý |
|---|---|---|
| `lệch tổng kiểm [<tệp>]` | Tệp đã áp bị sửa (R-24 cấm) — hoặc nhị phân build từ nhánh khác | Trả tệp về nội dung đã áp; thay đổi đi vào tệp mới. Không sửa sổ (bảng chỉ ghi thêm). |
| `dòng sổ không có tệp [<tệp>]` | Tệp đã áp bị xóa/đổi tên, hoặc nhị phân cũ hơn database | Dùng nhị phân đúng bản; không xóa tệp đã áp. |
| `ghi trùng` | Cùng tệp ở hai sổ (chỉ xảy ra nếu sửa tay) | Báo nhóm nền tảng; không tự xóa dòng. |
| `golang-migrate schema_migrations_<x> bẩn` | Lần v1 trước hỏng giữa chừng | Xử lý như sự cố golang-migrate cũ: kiểm tệp ở phiên bản đó đã áp hết chưa, sửa cờ `dirty` bằng quản trị cụm, rồi chạy lại. |
| Tệp lỗi khi áp (`migrator: <tệp> dòng N: …`) | Lỗi SQL | Giao dịch của tệp lùi (không ghi sổ, không "dirty"); sửa bằng tệp mới hoặc sửa tệp CHƯA áp ở môi trường nào, chạy lại. |
