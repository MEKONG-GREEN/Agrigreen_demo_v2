# ADR-0038: Bộ chạy migration theo module — sổ trong schema của module, thứ tự topo, vai `own_<module>`

**Trạng thái:** Đề xuất — hiện thực bước 1 ở W7.1 (09/10/2026); mặc định vẫn bộ chạy cũ một bản phát hành · **Ngày:** 09/10/2026 (gói W7.1) · **Phát hiện DB:** DB-12 · **Yêu cầu:** R-DB-12, kiến trúc v4.1 mục 23 · **Thành phần:** `internal/shared/migrator`, `internal/shared/dbadmin`, `cmd/migrate`, `tools/migratecheck`, `internal/shared/modreg` (danh mục sinh thêm `depends`) · **Liên quan:** [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [kế hoạch v4.1 §1 mục 6](../kien-truc-v4.1-ke-hoach.md), [runbook](../runbooks/migrate-runner-v2.md)

## Bối cảnh

- Mỗi cụm một thư mục `migrations/<cụm>/NNNNNN_*.up.sql`, áp bằng `golang-migrate` với MỘT số phiên bản mỗi thư mục (`schema_migrations_shared`, `schema_migrations_<cụm>`), vai `ag_migrator` chung.
- `golang-migrate` chỉ nhớ số lớn nhất đã áp: tệp có số **nhỏ hơn** số đó (hai nhánh song song cấp số, gộp khác thứ tự) bị **bỏ qua im lặng**. Kế hoạch v4.1 phải cấp số theo thứ tự gộp và cấp lại số khi gộp lệch (§1 mục 6) — mỗi đợt nhiều gói song song là một nguồn lỗi.
- R-DB-12: mỗi schema một lịch sử migration riêng (`schema_migrations` trong chính schema), áp theo thứ tự topo của đồ thị phụ thuộc module (`k/*` → `cap/*` → `dom/*`), chạy bằng vai `own_<module>` không chạm được schema khác.
- 129 tệp đã áp ở mọi môi trường, bất biến (R-24); nhiều tệp cũ đụng schema của nhiều module (ngoại lệ `schema` của agmodgraph, chủ W7.1). Không thể chạy chúng bằng `own_<module>` hay chuyển thư mục mà không đổi lịch sử.

## Quyết định

### 1. Gắn tệp cho module, giữ bố cục thư mục

Giữ `migrations/<cụm>/NNNNNN_*.up.sql` và số chung mỗi cụm. Mỗi tệp được gắn cho một module bằng cùng quy tắc với agmodgraph: tiền tố tên `<cụm>_<module>` dài nhất, nếu không thì module DUY NHẤT sở hữu (`owns_schema`) các schema tệp đụng. Còn lại là **module giả**: `shared` (thư mục `shared/`, thư viện `internal/shared`) và `<cụm>` (tệp toàn cụm — vai nhóm, view công bố, tích hợp nhiều module). `go run ./tools/migratecheck -modules migrations` in bảng gắn.

### 2. Sổ trong schema của module

Mỗi module có `<schema>.schema_migrations(dir, file, version, module, checksum, method, applied_at, applied_by)`, khóa `(dir, file)`, trong schema đầu tiên của `owns_schema` đã có ở database đó (vd `mkg/measurement` ở cụm ts dùng `ts_weigh`). Module giả, hoặc module chưa có schema ở database → `shared.schema_migrations`. Bảng sổ: chỉ ghi thêm (trigger `shared.fn_immutable`), không quyền cho vai nào ngoài chủ, RLS bật + ép với policy toàn quyền (thỏa các migration kiểm "mọi bảng của schema có RLS", vd pf 000133), chú thích `ag:no-rls: sổ migration theo module …` (miễn `v_missing_rls`, và là dấu để bộ chạy tìm sổ). Bộ chạy tự tạo sổ trong cùng giao dịch với tệp đầu tiên của module; **không cần migration** (shared 000008 của kế hoạch đầu lỗi thời; shared 000013 dành lại không dùng).

### 3. "Chờ áp" = vắng trong sổ; thứ tự topo giữ ràng buộc lịch sử

- Tệp chờ áp khi và chỉ khi khóa `<thư mục>/<tên>` không có trong BẤT KỲ sổ nào của database → tệp số nhỏ hơn số đã áp được áp, không còn bị bỏ qua. Mỗi tệp chạy trong một giao dịch cùng dòng sổ (không còn trạng thái "dirty").
- Tổng kiểm SHA-256 (CRLF → LF) của tệp đã ghi sổ phải khớp nội dung hiện tại; lệch → dừng (R-24). Dòng sổ không còn tệp, ghi trùng hai sổ → dừng.
- Thứ tự: `shared/` trước, rồi thư mục cụm theo Kahn với các cạnh luôn đi từ số nhỏ tới số lớn: (a) cùng module theo số; (b) tệp toàn cụm là rào chắn theo số; (c) A phụ thuộc (bắc cầu) B → tệp của B số nhỏ hơn đứng trước tệp của A; (d) tệp tham chiếu `s.` mà tệp số nhỏ hơn đã đụng schema `s` → đứng sau tệp đó. Trong các tệp sẵn sàng chọn theo (hạng topo module, tầng `kind`, tên module, số): `k/*` trước `cap/*` trước `dom/*` khi lịch sử cho phép. Chu trình `depends` (còn ngoại lệ agmodgraph) gộp thành một thành phần cùng hạng. Vì thứ tự số luôn hợp lệ, xếp lại không bao giờ đặt một tệp trước tệp nó cần theo lịch sử. Đồ thị `depends` lúc chạy lấy từ danh mục sinh (`modreg.Depends`, `catalog_gen.go`).

### 4. Kỳ chuyển: song song v1/v2, chuyển trạng thái không chạy lại

- `AG_MIGRATE_RUNNER=v2` chọn bộ chạy mới cho `migrate up` (và `dbadmin.Migrate`, nên cả `pgtest`); mặc định vẫn `golang-migrate` **một bản phát hành**, rồi đổi mặc định, rồi bỏ v1.
- Lần chạy v2 đầu (và mỗi lần sau): mọi tệp có số trong `(L, V]` vắng trong sổ — `L` là số lớn nhất của thư mục trong sổ, `V` là phiên bản `golang-migrate` — được **ghi sổ không chạy** (`method = import`): đó đúng là các tệp v1 đã áp. `golang-migrate` bẩn → dừng.
- v2 giữ bảng `schema_migrations_<thư mục>` của v1 bằng số lớn nhất đã áp (tạo nếu chưa có, cùng DDL), và giữ khóa tư vấn của v1 khi chạy → quay về v1 trong kỳ chuyển không chạy lại tệp, hai bộ chạy không chạy chồng.
- `migrate verify-runner` đối chiếu hai trạng thái, báo lệch (tệp v2 sẽ áp mà v1 đã bỏ qua, lệch tổng kiểm, …) với mã thoát 1. Giới hạn: tệp v1 ĐÃ bỏ qua im lặng trước lần chuyển nằm trong `(0, V]` nên bị ghi sổ như đã áp — kế hoạch đã cấp số theo thứ tự gộp nên không có tệp nào như vậy; runbook có bước kiểm tay.
- CI: `TestRunnerV1V2_W71` (trong `go test ./...`) dựng database mới bằng v1 và v2 cho mọi cụm, so dấu vân tay lược đồ (catalog: quan hệ, cột, ràng buộc, chỉ mục, view, hàm, trigger, policy, kiểu, ACL đã sắp, quyền mặc định; bỏ bảng sổ v2) phải giống hệt; rồi chuyển DB v1 sang v2 (0 tệp chạy lại), tệp ngoài thứ tự được áp, tệp bị sửa thì dừng.

### 5. Vai `own_<module>` và các bước sau

- Bước này: `migrate bootstrap` dựng `own_<module>` (NOLOGIN, `own_<đoạn cuối>` như `r_<đoạn cuối>`) cho mọi module có schema; `ag_migrator` là thành viên. Chưa chuyển quyền sở hữu đối tượng sẵn có và chưa `SET ROLE own_<module>` khi chạy: đổi chủ hàng trăm đối tượng (gồm hàm `SECURITY DEFINER`) và chạy tệp cũ đa schema bằng vai hẹp là thay đổi lớn, không cần cho mục tiêu chính (bỏ lỗi bỏ qua im lặng).
- Bước sau (W7.1 bước 2, cần gói riêng): thư mục theo module `migrations/<cụm>/<module>/` cho tệp MỚI (tệp cũ giữ chỗ — sổ đã theo tên tệp nên dời thư mục chỉ cần ánh xạ khóa); tệp mới phải gắn đúng một module (`migratecheck`); `ALTER … OWNER TO own_<module>` theo schema + `ALTER DEFAULT PRIVILEGES FOR ROLE own_<module>`; chạy tệp của module bằng `SET LOCAL ROLE own_<module>`; khi đó gỡ các ngoại lệ `schema` chủ W7.1 trong `tools/archlint/modgraph_exceptions.yaml` (tệp cũ đa schema được đánh dấu lịch sử).

## Phương án đã cân nhắc

- **Giữ golang-migrate, mỗi module một bảng `x-migrations-table`.** Vẫn chỉ nhớ số lớn nhất mỗi bảng — bỏ qua im lặng vẫn còn trong một module; không có thứ tự giữa module. Loại.
- **Thứ tự topo thuần (bỏ qua số).** Đặt tệp của module tầng thấp trước tệp tầng cao bất kể số: tệp toàn cụm (vai nhóm, view công bố) tạo chu trình với tệp module, và tệp cũ có phụ thuộc SQL không khai trong `depends`. Loại; giữ ràng buộc lịch sử như mục 3.
- **Một sổ chung `shared.schema_migrations` có cột module.** Đơn giản nhưng trái R-DB-12 (sổ trong schema của module — dời/xóa module mang lịch sử theo). Chỉ dùng cho module giả.
- **Dời tệp cũ sang thư mục module ngay.** Đụng các gói đang thêm migration song song (W3.4, W5.6, W8.2) và không cần để bỏ lỗi chính. Để bước 2.

## Hệ quả

- Hết lớp lỗi "số nhỏ hơn bị bỏ qua": khi v2 là mặc định, hai nhánh chỉ còn phải tránh **trùng** số (`migratecheck`), không phải cấp theo thứ tự gộp.
- Mỗi schema module có thêm bảng `schema_migrations` (không tenant, chỉ chủ đọc được); cổng catalog, cổng cô lập schema (W9.1), cổng EXPLAIN không bị ảnh hưởng (đã kiểm với `AG_MIGRATE_RUNNER=v2`).
- Database mới bằng v2 có thể khác v1 ở **thứ tự mục ACL** (GRANT của hai module không liên quan chạy khác thứ tự); quyền như nhau.
- `catalog_gen.go` có thêm `catalogDepends`: đổi `depends` trong `module.yaml` → `go run ./tools/modgen` (như mọi trường danh mục).

## Hiện thực

### W7.1 bước 1 (09/10/2026)

`internal/shared/migrator` (`plan.go` gắn module + thứ tự, `reconcile.go` đối chiếu thuần, `runner.go` `Up`/`Inspect`, `fingerprint.go`), `dbadmin.Migrate` theo `AG_MIGRATE_RUNNER` (+ `MigrateV1`, `MigrateV2`, vai `own_<module>`), `cmd/migrate up` (in bộ chạy) + `verify-runner`, `tools/migratecheck -module|-modules`, `modreg.Depends`. Kiểm thử: đơn vị (hạng topo có chu trình, gắn module, thứ tự giữ ràng buộc, đối chiếu với số ngoài thứ tự, tổng kiểm CRLF), tích hợp `TestRunnerV1V2_W71`, `migratecheck` theo module. Không migration.
