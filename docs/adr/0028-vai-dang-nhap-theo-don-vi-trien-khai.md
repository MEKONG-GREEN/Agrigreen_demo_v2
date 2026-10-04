# ADR-0028: Một vai đăng nhập mỗi đơn vị triển khai, `SET LOCAL ROLE r_<module>` trong `WithContextTx`

**Trạng thái:** Đề xuất — chờ MKG ký (A7) · **Ngày:** 04/10/2026 (gói W0.1) · **Phụ lục B v4.1:** mục 7 · **Xung đột:** X-10 · **Phát hiện DB:** DB-04 · **Thành phần:** `internal/shared/db`, `internal/shared/dbadmin`, `tenant.WithTenantTx`, mọi `cmd/*`, compose, chart `deploy/k8s`, PgBouncer · **Liên quan:** [ADR-0005](0005-rls-mot-policy-moi-bang.md), [ADR-0021](0021-ha-tang-kubernetes-g4.md), [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0029](0029-k-ref-ban-chieu-loi-moi-cum.md), kiến trúc v4.1 §14

## Bối cảnh

Hiện trạng (g1–g5 conventions, `internal/shared/dbadmin`):
- khoảng 28 vai `LOGIN` dạng `<module>_app` (`accounts_app`, `geo_app`, …, `hub_app`, `vault_app`);
- mỗi vai một pool. `api-public` mở pool cho 5–7 vai module;
- mỗi vai chỉ có quyền trên schema của module mình ("không quyền chéo");
- RLS đọc `app.tenant_id` do `tenant.WithTenantTx` đặt bằng `set_config(..., true)` (ADR-0005).

Vấn đề (DB-04): số kết nối máy chủ tăng theo số module × số đơn vị triển khai. Ở ĐTK 2, cụm `pf` cần khoảng 250 kết nối nếu mỗi module một pool, so với khoảng 80 nếu mỗi đơn vị triển khai một pool. Mỗi vai là một bí mật phải cấp, xoay vòng, đưa vào chart.

Kiến trúc v4.1 §14:
- `r_<module>` NOLOGIN mang quyền chạy của module;
- một vai đăng nhập `NOINHERIT` mỗi đơn vị triển khai, là thành viên các `r_*` của module nó ghép;
- mỗi giao dịch `WithContextTx` đặt `SET LOCAL ROLE` theo module đang gọi (R-DB-02);
- vai của module do registry gán lúc biên dịch, mã nghiệp vụ không truyền tên vai (R-DB-03).

## Quyết định

### 1. Vai

| Vai | Loại | Quyền |
|---|---|---|
| `r_<module>` | NOLOGIN | Quyền hiện có của `<module>_app`: `SELECT, INSERT` trên bảng sổ; `SELECT, INSERT, UPDATE` trên bảng chiếu và bảng vận hành; `EXECUTE` hàm `shared`; `SELECT` trên `k_ref` (ADR-0029) và trên `_pub` được cấp |
| `app_<đơn vị>` | LOGIN **NOINHERIT** | Thành viên các `r_*` của module ghép trong đơn vị đó. Tự nó không có quyền bảng nào |
| vai migration (hiện có) | LOGIN | Giữ nguyên, chỉ pipeline migration dùng |

Vai đăng nhập đề xuất (chốt ở W2.3, W2.6):

| Cụm | Vai đăng nhập | Đơn vị triển khai |
|---|---|---|
| `pf` | `app_platform` | `api-platform` |
| `pf` | `app_worker` | `worker` (gồm worker `pf.kref`, ADR-0029) |
| `pf` | `app_public` | `api-public` (sau là `integration-gateway`, ADR-0033) |
| `pf` | `app_sync` | engine đồng bộ, `objsync` |
| `mkg` | `app_internal`, `app_worker_internal` | `api-internal`, `worker-internal` |
| `ts` | `app_ingest` | `ingest` |
| `vault` | `app_vault` | `identity-vault` |

`outbox-publisher` giữ vai riêng `ag_outbox_publisher` (không theo module, xem dưới).

`r_*` **không** có quyền trên schema của module khác, trừ `k_ref` và `_pub` đã khai. "Không quyền chéo" của g1–g5 conventions giữ nguyên, chuyển từ vai đăng nhập sang `r_*`.

Các vai đặc quyền hiện có (`ag_outbox_publisher`, chủ hàm giám sát `mkg`/`ts` có BYPASSRLS) **không** được cấp cho bất kỳ `app_*` nào. Danh mục vai BYPASSRLS ở [ADR-0031](0031-vai-tong-hop-r-agg.md).

### 2. `WithContextTx`

```go
func WithContextTx(ctx context.Context, pool *pgxpool.Pool, m ModuleID, fn func(pgx.Tx) error) error
```

Trong một giao dịch, theo thứ tự:
1. `SET LOCAL ROLE <vai của m>`. Tên vai lấy từ registry lúc biên dịch (`ModuleID → r_<module>`), không từ tham số chuỗi.
2. `set_config('app.tenant_id', …, true)` như hiện nay. W2.4 thêm `app.node_id`, `app.dept`, `app.xa_ids`, `app.purpose`, `app.actor_ref`, `app.on_behalf_of_ref`.
3. Chạy `fn`.

Quy tắc:
- Lint cấm chuỗi `SET ROLE`, `SET SESSION`, `RESET ROLE` ngoài `internal/shared/db` (R-DB-03).
- Vì vai đăng nhập NOINHERIT, truy vấn **ngoài** `WithContextTx` bị `permission denied`. Đó là hành vi mong muốn; CI có cổng "truy vấn ngoài `WithContextTx` thất bại".
- `tenant.WithTenantTx` chạy song song một kỳ phát hành như lớp mỏng gọi `WithContextTx`, rồi bỏ (W2.6).
- `tenant.Within` (ADR-0020, đổi tenant trong giao dịch) giữ nguyên ngữ nghĩa, không đổi vai.

### 3. PgBouncer transaction mode

- `SET LOCAL ROLE` và `set_config(..., true)` hết hiệu lực khi COMMIT/ROLLBACK. Kết nối trả về pool luôn ở vai đăng nhập, không ngữ cảnh. Tương thích transaction mode (R-DB-02).
- Cấm mọi `SET` mức phiên (lint ở trên). Đây là điều kiện để không rò vai giữa hai khách dùng chung một kết nối máy chủ.
- PgBouncer gộp theo (vai đăng nhập, database). `userlist` giảm từ khoảng 28 xuống 4–6 dòng mỗi cụm. Trần kết nối ĐTK 2 cụm `pf`: 4 vai × 20 = 80.
- Kiểm thử bắt buộc chạy qua PgBouncer (`AG_TEST_PGBOUNCER_URL`, như ADR-0005):
  - T4: sau giao dịch, `current_user` = vai đăng nhập, `current_setting('app.tenant_id', true)` rỗng;
  - T5: song song nhiều module trên cùng kết nối tái sử dụng, không module nào đọc được bảng của module khác;
  - giữ `TestTenantLeakParallel`, `TestContextDoesNotOutliveTransaction`.
- Câu lệnh chuẩn bị sẵn (statement cache của pgx) phải được kiểm cùng T4/T5: quyền kiểm lúc thực thi theo vai hiện hành.

### 4. Đường chuyển

1. **W2.3 — khung:** tạo `r_<module>` cho mọi module pf, GRANT quyền hiện có của `<module>_app` sang `r_<module>`; tạo `app_platform`, `app_worker`, `app_public`, `app_sync`; `WithContextTx`; registry; lint. Chuyển **2 module mẫu** (`pf/content`, `pf/notify`). Vai `<module>_app` cũ vẫn chạy song song.
2. **W2.6 — chuyển hết:** mọi module pf, mkg, ts, vault. Bỏ quyền bảng khỏi `<module>_app`, giữ tên vai một kỳ cho `userlist` PgBouncer, rồi xóa. Cập nhật `dbadmin`, compose, chart K8s. Bí mật giảm từ khoảng 28 xuống 4–6 mỗi cụm.

Mỗi bước có đường lui: vai cũ còn quyền cho đến khi bước kế tiếp xanh trên CI và staging.

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Giữ một vai đăng nhập mỗi module (hiện trạng) | Trần `max_connections` ở ĐTK 2 (DB-04); nhiều bí mật; không có chỗ đặt vai theo module khi một đơn vị ghép nhiều module |
| Một vai đăng nhập mỗi đơn vị, **không** `SET ROLE` (vai mang hết quyền) | Mất lớp chặn thứ hai giữa các module: lỗi mã ở module A ghi được bảng module B |
| Vai đăng nhập INHERIT | Quyền có sẵn ngoài giao dịch; quên `WithContextTx` vẫn chạy được, không phát hiện |
| `SET ROLE` mức phiên | Rò vai giữa khách khi qua PgBouncer transaction mode |

## Hệ quả

- **Bán kính khi lộ bí mật:** vai đăng nhập là thành viên mọi `r_*` của đơn vị. Lộ một bí mật = lộ quyền mọi module trong đơn vị đó. Mức này bằng hiện trạng, vì tiến trình hiện đã giữ mọi bí mật `<module>_app` của nó. Ranh giới module ở DB là **chống nhầm lẫn**, không phải hàng rào an ninh (v4.1 §7.4).
- Một vai bị chia sẻ bởi nhiều đơn vị → không còn. Phân tách theo đơn vị triển khai rõ hơn trong nhật ký DB (`application_name` + `current_user`).
- RLS (ADR-0005) không đổi bởi ADR này; chỉ nơi đặt ngữ cảnh đổi tên.
- g1–g5 conventions phải sửa đoạn "vai LOGIN mỗi module" khi W2.6 xong.
- Hỗ trợ RB-2 `[KÝ]` (K-2, cách ly theo đơn vị ở tầng dữ liệu): RLS vẫn là lớp cách ly; ADR này thêm lớp theo module.

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| W2.3 | Khung vai, `WithContextTx`, registry, lint, 2 module mẫu, T4/T5 | pf 000090, shared 000007 |
| W2.4 | Biến ngữ cảnh `app.*` trong `WithContextTx` | trong pf 000091 |
| W2.6 | Chuyển mọi module, bỏ quyền vai cũ, cổng CI | pf 000093, mkg 000023, ts 000004, vault 000003 |
| W9.1 | Cổng "không `r_*` có quyền trên schema module khác trừ `k_ref`, `_pub`" | — |

## Liên quan

- Kiến trúc v4.1: §6 (PgBouncer một pool mỗi đơn vị), §14 (R-DB-02, R-DB-03), Phụ lục B mục 7, Phụ lục D (`WithTenantTx` → `WithContextTx`).
- Bản đối chiếu: §1.3 mục 2, §2.5 DB-04, X-10.
- [ADR-0005](0005-rls-mot-policy-moi-bang.md), [ADR-0021](0021-ha-tang-kubernetes-g4.md) (bí mật, PgBouncer ở K8s), [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0029](0029-k-ref-ban-chieu-loi-moi-cum.md), [ADR-0031](0031-vai-tong-hop-r-agg.md).
