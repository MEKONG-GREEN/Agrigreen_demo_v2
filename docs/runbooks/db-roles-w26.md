# Chuyển vai DB sang vai đăng nhập đơn vị (W2.6, ADR-0028)

Áp một lần khi lên bản có W2.6. Sau bản này, ứng dụng chỉ đăng nhập DB bằng **vai đơn vị** `app_*`. Mỗi giao dịch của module chạy `SET LOCAL ROLE r_<module>`.

Vai cũ `<module>_app` vẫn còn, nhưng:

- không còn quyền trực tiếp nào (pf 000119, mkg 000035, vault 000003);
- vẫn là thành viên `r_<module>`, nên nhị phân cũ chạy được khi lùi bản.

Vai đăng nhập không theo module (`platform_api`, `public_api`, `worker_platform`, `mkg_api`, `worker_internal`, `ingest_mkg`, `ingest_ts`, `ts_reader`) giữ nguyên quyền một kỳ. Bản phát hành sau xóa mọi vai cũ.

## Vai đăng nhập mới

| Cụm | Vai | Đơn vị (và lệnh dùng chung) | Biến mật khẩu |
|---|---|---|---|
| pf | `app_platform` | `api-platform` | `AG_DB_PASSWORD_APP_PLATFORM` |
| pf | `app_worker` | `worker`, `drill`, `formimport` | `AG_DB_PASSWORD_APP_WORKER` |
| pf | `app_public` | `api-public` | `AG_DB_PASSWORD_APP_PUBLIC` |
| pf | `app_sync` | `objsync` / engine đồng bộ | `AG_DB_PASSWORD_APP_SYNC` |
| pf | `app_agg` | `worker -agg-run` — chỉ `r_agg` (ADR-0031 §3, gói FC; [agg-jobs.md](agg-jobs.md)) | `AG_DB_PASSWORD_APP_AGG` (Helm: Secret `pf-db-agg`, chỉ worker) |
| mkg | `app_internal` | `api-internal` | `AG_DB_PASSWORD_APP_INTERNAL` |
| mkg | `app_worker_internal` | `worker-internal`, `rebuilddrill` | `AG_DB_PASSWORD_APP_WORKER_INTERNAL` |
| mkg + ts | `app_ingest` | `ingest` (một vai, hai cụm, **cùng** mật khẩu) | `AG_DB_PASSWORD_APP_INGEST` |
| vault | `app_vault` | `identity-vault` | `AG_DB_PASSWORD_APP_VAULT` |

Các vai `ag_migrator`, `ag_outbox_publisher`, `mkg_monitor` và `ts_monitor` giữ nguyên. Số bí mật đăng nhập mỗi cụm: pf 8 (gồm `app_agg`, gói FC), mkg 7, ts 5, vault 4 — gồm `ag_ops_jobs` (rà soát 08/10/2026, shared 000009: job vận hành xuyên tenant). Không kể `ag_ops_jobs` thì là 6/6/4/3, khớp mục tiêu 4–6 của §4.

## Thứ tự triển khai

1. **Bí mật.** Thêm các khóa `AG_DB_PASSWORD_APP_*` ở bảng trên vào kho bí mật trước `helm upgrade` release `data`:
   - `<remotePrefix>/db/mkg`: `APP_INTERNAL`, `APP_WORKER_INTERNAL`, `APP_INGEST`. Cụm ts đọc `APP_INGEST` từ `db/mkg` qua `keyRemotes`;
   - `<remotePrefix>/db/vault`: `APP_VAULT`;
   - `<remotePrefix>/db/pf`: bốn khóa `APP_*` đã có từ W2.3.

   Compose: chạy lại `scripts/gen-dev-secrets` hoặc thêm `AG_DB_PASSWORD_APP_VAULT` vào `deploy/compose/.env`.
2. **Bootstrap.** Chạy `migrate bootstrap` bản W2.6 (Job `db-bootstrap` của release `data`). Bước này tạo:
   - `r_*` của mkg/vault và `r_ops`;
   - vai đăng nhập đơn vị mới;
   - membership vai cũ → `r_<module>` (INHERIT).

   Phải chạy **trước** bước 3. Thiếu vai nhóm hoặc membership thì migration lỗi, không lặng lẽ bỏ qua.
3. **Migrate.** Chạy `migrate up` (pf 000119, mkg 000035, ts 000004, vault 000003). Mỗi migration chép quyền sang `r_*` rồi thu hồi quyền trực tiếp của vai cũ. Ứng dụng bản cũ vẫn chạy trong lúc này, nhờ membership.
4. **Ứng dụng.** Triển khai bản W2.6 của mọi đơn vị. Pod chỉ còn nhận Secret `pf-db-roles`, `pf-public-db-roles`, `mkg-db-roles`, `vault-db-roles` với khóa `APP_*`. Secret `ts-db-roles` không còn.
5. **Kiểm.** Kiểm các điểm sau:
   - `/readyz` của mọi đơn vị trả 200;
   - nhật ký DB: `current_user` là `app_*`, không còn phiên `<module>_app`;
   - `SELECT usename, count(*) FROM pg_stat_activity GROUP BY 1` trên từng cụm.

PgBouncer (compose, CI) giữ dòng `userlist` của vai cũ trong kỳ này. CNPG Pooler ở K8s dùng `auth_query`, không cần sửa.

## Lùi bản

Triển khai lại bản trước bước 4. Không cần lùi migration: vai cũ vẫn có đủ quyền qua `r_<module>`. Vai không theo module vẫn còn quyền trực tiếp. **Không** xóa vai cũ, cũng không gỡ chúng khỏi `userlist`, cho tới bản phát hành sau W2.6.

## Cỡ pool

Mỗi đơn vị mở **một** pool `app_*`. Cỡ mặc định là `AG_DB_MAX_CONNS=20`, theo trần ADR-0028 §3: 4 vai × 20 ở cụm pf, ĐTK 2.

Các module của đơn vị dùng chung pool này. Nếu chờ kết nối kéo dài (`pgxpool` acquire), có hai cách xử lý:

- tăng `AG_DB_MAX_CONNS`;
- tăng `default_pool_size` của Pooler cho vai đó.

Hạn chờ mở giao dịch (P0-POOL-SHARED, #126): `AG_DB_ACQUIRE_TIMEOUT` (Go duration, mặc định `30s`; `0` = không giới hạn, chỉ theo ctx) giới hạn bước lấy kết nối + `BEGIN` của mọi giao dịch qua `tenant.WithTenantTx`. Pool cạn thì lời gọi nhận lỗi `tenant.ErrAcquireTimeout` thay vì treo. Mã không gọi dịch vụ ngoài (SMTP, kho đối tượng, KMS, HTTP) khi đang giữ giao dịch — xem `docs/kn/gia-dinh/P0-POOL-SHARED.md`.
