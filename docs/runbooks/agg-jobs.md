# Job tổng hợp `r_agg` (W6.4)

ADR-0031 §3–§7, R-DB-11, R-SEC-03. Mã: `internal/shared/db/agg.go` (`db.WithAggTx`), `internal/platform/metrics/app/agg.go` (`Agg`: `Propose`, `Decide`, `Run`), `internal/platform/metrics/app/aggjobs.go` (job đã khai), migration **pf 000129** (`pf_metrics.agg_job`, `pf_metrics.agg_run`).

> **Production:** chưa bật theo lịch. Bộ chạy nối vào `cmd/worker` dạng lệnh một lần `worker -agg-run <job>` (gói FC); chạy định kỳ chỉ bật sau khi ngưỡng k được ký (C-4, K-8, Q5-3). Tới lúc đó k sàn là `publicdata.k_min` = 10 (tạm).

## Vai

| Vai | Thuộc tính | Quyền | Ai dùng |
|---|---|---|---|
| `r_agg` | NOLOGIN, **BYPASSRLS**, INHERIT | thành viên mọi `r_<module>_pub` (SELECT view `_pub`); USAGE `k_ref` + SELECT **cột** `k_ref.org.tenant_id` (pf 000129). Không quyền ghi, không quyền schema nội bộ | chỉ `db.WithAggTx` |
| `app_agg` | LOGIN, NOINHERIT; mặc định `default_transaction_read_only = on`, `statement_timeout = 10min` (`ALTER ROLE … SET`, bootstrap) | **chỉ** thành viên `r_agg` (INHERIT FALSE, SET TRUE) — không `r_*` nào khác, không `ag_shared_user`, tự nó không quyền bảng nào | pool 2 kết nối của `worker -agg-run` (`dbadmin.AppAgg`) |
| `app_worker` | LOGIN, NOINHERIT | thành viên `r_metrics` (danh mục, kiểm duyệt, nhật ký) — **không** thuộc `r_agg` | giao dịch 1 và nhật ký của `Agg.Run`; `Propose`/`Decide` |

Bootstrap thu hồi mọi thành viên `r_agg` khác `app_agg` (`dbadmin.ExclusiveMembers`) — grant `app_worker` → `r_agg` của W6.4 bị gỡ ở lần bootstrap đầu tiên của bản FC.

Danh mục vai BYPASSRLS (ADR-0031 §6, kiểm ở `TestAggRole_W64` và `TestSchemaIsolationAggRole_W64`): `r_agg`, `ag_outbox_publisher`, `ag_ops_jobs`, `mkg_monitor_owner`, `ts_monitor_owner`.

Chỉ `internal/shared/db` được đổi vai (archlint `agnosessionset`: `SET LOCAL ROLE`, `set_config('role', …)`, tên `r_agg` ngoài `shared/db`/`shared/dbadmin`, gọi `db.WithAggTx` ngoài `internal/platform/metrics/app` đều bị chặn); giao dịch job vẫn đặt `transaction_read_only = on` và `statement_timeout = 10min` cục bộ (lớp thứ hai sau mặc định của vai).

**Còn mở:** `pg_hba` chỉ cho `app_agg` từ đơn vị worker — repo chưa quản lý `pg_hba` (CNPG/PgBouncer dùng `auth_query`); trước khi chạy định kỳ ở production thêm luật `pg_hba` (hoặc NetworkPolicy tới pooler) cho `app_agg`. Bí mật `pf-db-agg` (Helm) chỉ worker gắn (`check_flows.py`).

## Lên bản (thứ tự)

Bản W6.4 (lần đầu):

1. `cmd/migrate bootstrap` (tài khoản quản trị cụm) — tạo `r_agg` (NOLOGIN BYPASSRLS, thành viên `r_<module>_pub`) và `app_agg`. Chạy lặp an toàn.
2. `cmd/migrate up` — pf 000129 dừng với lỗi "thiếu vai r_agg … chạy cmd/migrate bootstrap bản W6.4" nếu bỏ bước 1.
3. Kiểm: `SELECT rolbypassrls, rolcanlogin FROM pg_roles WHERE rolname = 'r_agg'` → `t, f`; `go test -run 'SchemaIsolation|LoginRoles' ./migrations/` trên staging.

Bản FC (tách `app_agg` khỏi `app_worker`, không migration):

1. Kho bí mật: thêm `AG_DB_PASSWORD_APP_AGG` vào `<remotePrefix>/db/pf` **trước** — Job bootstrap đọc nó qua `pg-pf-roles`, worker qua Secret mới `pf-db-agg` (thiếu → bootstrap dùng `AG_DB_APP_PASSWORD` chung và worker không đăng nhập được `app_agg`). Dev: Secret `pg-pf-roles` đã có thì `dev-secrets.ps1` giữ nguyên — xóa rồi chạy lại để có khóa mới. Compose: chạy lại `scripts/gen-dev-secrets` hoặc để rỗng (dùng mật khẩu chung).
2. `cmd/migrate bootstrap` bản FC — tạo `app_agg` (+ tham số mặc định), cấp `r_agg`, **thu hồi** `r_agg` khỏi `app_worker`.
3. PgBouncer Local/Dev: khởi động lại `pgbouncer` (userlist có `app_agg`). Kubernetes (pooler CNPG, `auth_query`): không cần.
4. Nhị phân worker mới (cờ `-agg-run`). Thứ tự 2 ↔ 4 không quan trọng: nhị phân cũ không gọi `db.WithAggTx`.
5. Kiểm: `SELECT m.rolname FROM pg_auth_members a JOIN pg_roles g ON g.oid = a.roleid JOIN pg_roles m ON m.oid = a.member WHERE g.rolname = 'r_agg'` → chỉ `app_agg`; `go test -run 'AggRole|LoginRoles' ./migrations/`.

Lùi bản: không cần thu hồi — nhị phân cũ không gọi `db.WithAggTx`; bootstrap bản cũ cấp lại `r_agg` cho `app_worker` (vô hại tới khi chạy lại bootstrap mới), `app_agg` còn lại không dùng. Không xóa sổ `agg_job`/`agg_run` (chỉ ghi thêm).

## Thêm hoặc sửa job (bốn mắt)

1. Lập trình viên thêm/sửa `domain.AggJob` trong `aggjobs.go`: **mọi** thay đổi (SQL, view, chiều, số đo, mục đích, k) phải tăng `Version` — bộ chạy so băm định nghĩa (`AggJob.Hash`) với dòng đã duyệt.
2. Người đề xuất gọi `Agg.Propose` (tenant quản trị): kiểm định nghĩa (chỉ đọc view `_pub` đã khai, k ≥ k sàn), phiên bản lớn hơn mọi phiên bản đã đề xuất; ghi `agg_job` (`proposed`) + yêu cầu `pf_workflow.request` kind `agg.job`, subject = id đề xuất — cùng giao dịch.
3. Người khác gọi `Agg.Decide` (`approved` | `rejected` + ghi chú): quyết định `pf_workflow.decision` + dòng `agg_job` cùng giao dịch; người quyết định = người đề xuất → `self_approval_forbidden` (app, CHECK `decision_four_eyes`, CHECK `agg_job_four_eyes`).
4. Bộ chạy chỉ chạy phiên bản **đã duyệt mới nhất**: phiên bản cũ (đã có phiên bản mới hơn được duyệt), chưa duyệt, bị từ chối, định nghĩa khác băm → `metrics.agg_job_not_approved`, không đổi vai.

Chưa có HTTP/CLI cho đề xuất/duyệt: gọi qua gốc lắp ráp (`metrics.Agg(clk, metrics.AggDeps{…})`, `Approvals` = cap/workflow — đơn vị có `r_workflow`, không phải worker). Danh mục job có trong rà soát quyền định kỳ (R-SEC-04): đọc `pf_metrics.agg_job` của tenant quản trị.

## Chạy và nhật ký

Lệnh: `worker -agg-run <job_id> -agg-actor <uuid người chạy> [-agg-k <k>]` với `AG_AGG_GOVERNANCE_TENANT` (tenant quản trị), `AG_DB_PASSWORD_APP_AGG`, `AG_DB_PASSWORD_APP_WORKER`; pool `app_agg` 2 kết nối trên `AG_DB_REPLICA_URL` khi có, không thì `AG_DB_DIRECT_URL`/`AG_DB_URL`. Log chỉ có số tenant/dòng/nhóm (không chi tiết). Thoát 1 khi lỗi hoặc bị từ chối.

`Agg.Run(ctx, job, actor, k)` (k = 0 → k áp = max(k job, k sàn)):

1. Giao dịch CHỈ ĐỌC trên pool `app_worker` dưới `r_metrics`, `app.tenant_id` = tenant quản trị → kiểm job đã duyệt (trên). Lỗi → ROLLBACK, không mở giao dịch tổng hợp.
2. Giao dịch CHỈ ĐỌC thứ hai trên pool `app_agg`: `SET LOCAL ROLE r_agg` (xóa biến ngữ cảnh `app.*`) → liệt kê tenant qua `k_ref.org` (vượt RLS) → với từng tenant đặt `app.tenant_id` cục bộ và chạy SQL của job trên view `_pub`. View `_pub` (W6.3) chạy bằng quyền chủ `ag_migrator` có FORCE RLS, nên BYPASSRLS của bên đọc **không** xuyên qua view — đọc theo lát tenant là chủ ý; tenant không có dòng `k_ref.org` không được đếm.
3. Gộp nhóm và ẩn nhóm có ít hơn k tenant góp **trong tiến trình**; bản ghi chi tiết không rời bộ chạy. Job công bố cả tổng phải tự ẩn ô bù (ADR-0031 §4).
4. Ghi một dòng `pf_metrics.agg_run` (giao dịch `r_metrics` riêng): job, phiên bản, băm, dòng danh mục đã duyệt, người/tiến trình gọi, bắt đầu/kết thúc, số tenant/dòng đọc, số nhóm ra/bị ẩn, k, kết quả `succeeded` | `failed` | `refused` + mã lỗi. Không ghi được → trả `metrics.dependency_unavailable`, **không** trả đầu ra.

Theo dõi: `SELECT job_id, version, outcome, error_code, k_used, groups_out, groups_suppressed, started_at, finished_at FROM pf_metrics.agg_run WHERE tenant_id = '<tenant quản trị>' ORDER BY recorded_at DESC` (dưới `r_metrics`). Cảnh báo khi có `refused` không mong đợi (định nghĩa sửa mà chưa duyệt) hoặc `failed` liên tiếp.

## Sự cố

| Triệu chứng | Nguyên nhân | Xử lý |
|---|---|---|
| `metrics.agg_job_not_approved` | chưa đề xuất/duyệt phiên bản trong mã, đã duyệt phiên bản mới hơn, sửa định nghĩa không tăng phiên bản | đề xuất + duyệt đúng phiên bản; triển khai nhị phân khớp phiên bản đã duyệt |
| `metrics.agg_k_below_floor` | k xin < k sàn hoặc k job < `publicdata.k_min` mới | tăng k của job (phiên bản mới) |
| `42501` khi đổi vai | bootstrap bản FC chưa chạy (thiếu `app_agg` hoặc chưa là thành viên `r_agg`), hoặc pool truyền vào `AggDB` không phải `app_agg` | chạy `cmd/migrate bootstrap`; kiểm `AG_DB_PASSWORD_APP_AGG` |
| `28P01` khi mở pool `app_agg` | mật khẩu ở `pf-db-agg` khác lúc bootstrap (khóa thêm sau bootstrap) | chạy lại bootstrap sau khi khóa có ở `pg-pf-roles` |
| `25006` trong giao dịch ghi của `app_agg` | mặc định của vai là chỉ đọc (chủ ý) | `app_agg` không bao giờ ghi; nhật ký đi qua `app_worker` |
| `57014` (statement_timeout) | SQL của job chậm (> 10 phút mỗi câu) | tối ưu SQL (phiên bản mới), chạy trên bản sao chỉ đọc |
