---
paths:
  - "migrations/**/*"
---
# Migration SQL

Nguồn: AGENTS.md §7, [g1-conventions §2](../../docs/dev/g1-conventions.md), [modules.md](../../docs/dev/modules.md) (vai DB, biến ngữ cảnh, `k_ref`, cổng W9.1, `_pub`), [parallel-packages.md](../../docs/dev/parallel-packages.md), [fail-closed-config.md](../../docs/conventions/fail-closed-config.md). Khi lệch, nguồn thắng. Máy chặn phần lớn: `migratecheck`, `agmodgraph`, cổng tích hợp trong `migrations/*_test.go`.

## Tệp
- Tạo tên bằng `go run ./tools/migratecheck -new <cụm>_<module>_<tên>` (mốc UTC). Không tự đặt số.
- Sau mỗi lần rebase: `go run ./tools/migratecheck -base origin/dev migrations`; tệp xếp trước dev → tạo lại tên và sửa tham chiếu.
- Một tệp = một schema; chỉ đụng schema của module mình.
- **Forward-only:** không `.down.sql`; không sửa tệp đã gộp; không `UPDATE`/`DELETE`/`TRUNCATE` mức câu lệnh (R-25).

## Sửa bảng đã có
- `SET LOCAL lock_timeout = '5s';` trước câu `ALTER TABLE` đầu tiên (`SET LOCAL`, không `SET`).
- `ADD CONSTRAINT … CHECK` / `FOREIGN KEY` phải `NOT VALID`, rồi `ALTER TABLE … VALIDATE CONSTRAINT …` ở câu riêng. Bảng tạo trong cùng tệp được miễn.

## Bảng mới
- Mọi bảng trong schema nghiệp vụ có `tenant_id` và gọi `SELECT shared.apply_tenant_rls('<schema.bảng>');` (ADR-0005).
- Không khóa ngoại / join xuyên schema; tham chiếu module khác là UUID thuần.
- Cuối tệp `GRANT … TO r_<module>` — **không bao giờ** cấp cho vai đăng nhập `app_*`.
- Module pf mới: `GRANT USAGE ON SCHEMA k_ref` + `SELECT` các quan hệ `k_ref` + `EXECUTE k_ref.ctx_*` cho `r_<module>` (R-DB-05).
- Module cần quyền chéo: công bố view/hàm trong `pf_<module>_pub` (R-DB-10), không cấp thẳng quyền bảng.

## Bảng sổ (append-only)
- Theo mẫu §15 v4.1, rồi `SELECT shared.apply_ledger('<schema.bảng>', '<schema.bảng>');` — xem `migrations/pf/000001_pf_party_consent.up.sql`. Không GRANT UPDATE/DELETE.
- Thêm cột vào sổ đã có: NULL được, không `DEFAULT`, và ngay sau `ADD COLUMN` gọi `SELECT shared.register_ledger_columns('<schema.bảng>', ARRAY['cột', …]);` trong cùng tệp. Không `DROP`/`RENAME`/đổi kiểu cột sổ. Sổ đã có dữ liệu thì cân nhắc sổ phụ.
- Không có khóa duy nhất trên bảng sổ (phân vùng, ADR-0012); `fn_chain` đã chống trùng `idem_key`.

## RLS và biến ngữ cảnh
- **Fail-closed:** không có nhánh "bảng chiếu (`k_ref`, closure) rỗng thì cho qua". Chiếu rỗng → không thấy gì.
- Đọc biến phiên chỉ qua `k_ref.ctx_uuid('app.…')`, `k_ref.ctx_text('app.…')`. Cấm `current_setting(…)::uuid`.
- Viết thẳng điều kiện, ví dụ `EXISTS (SELECT 1 FROM k_ref.node_closure c WHERE c.ancestor = k_ref.ctx_uuid('app.node_id') AND c.descendant = <bảng>.node_id)`. Không gọi hàm không inline được trong policy (cổng `EXPLAIN` DB-14).
- Cấm `SET ROLE`, `SET SESSION`, `RESET`, `set_config(…, false)`.

## Kiểm
- `go run ./tools/migratecheck -base origin/dev migrations`, `go run ./tools/modgen -check`, `go run ./tools/archlint ./...`, rồi test DB của module và `./migrations/` liên quan (phải thấy `--- PASS`).
