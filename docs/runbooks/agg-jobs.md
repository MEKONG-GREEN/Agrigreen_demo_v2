# Job tổng hợp `r_agg` (W6.4)

ADR-0031 §3–§7, R-DB-11, R-SEC-03. Mã: `internal/shared/db/agg.go` (`db.WithAggTx`), `internal/platform/metrics/app/agg.go` (`Agg`: `Propose`, `Decide`, `Run`), `internal/platform/metrics/app/aggjobs.go` (job đã khai), migration **pf 000129** (`pf_metrics.agg_job`, `pf_metrics.agg_run`).

> **Production:** chưa bật. Bộ chạy chưa nối vào `cmd/worker`; job vượt đơn vị chỉ bật sau khi ngưỡng k được ký (C-4, K-8, Q5-3). Tới lúc đó k sàn là `publicdata.k_min` = 10 (tạm).

## Vai

| Vai | Thuộc tính | Quyền | Ai dùng |
|---|---|---|---|
| `r_agg` | NOLOGIN, **BYPASSRLS**, INHERIT | thành viên mọi `r_<module>_pub` (SELECT view `_pub`); USAGE `k_ref` + SELECT **cột** `k_ref.org.tenant_id` (pf 000129). Không quyền ghi, không quyền schema nội bộ | chỉ `db.WithAggTx` |
| `app_worker` | LOGIN, NOINHERIT | thành viên `r_agg` (INHERIT FALSE, SET TRUE) | đơn vị chạy job (`dbadmin.LoginUnit.Agg`) |

Danh mục vai BYPASSRLS (ADR-0031 §6, kiểm ở `TestAggRole_W64` và `TestSchemaIsolationAggRole_W64`): `r_agg`, `ag_outbox_publisher`, `ag_ops_jobs`, `mkg_monitor_owner`, `ts_monitor_owner`.

**Lệch ADR-0031 §3:** chưa có vai đăng nhập riêng `app_agg` (+ `pg_hba` chỉ từ worker, `default_transaction_read_only`, `statement_timeout` của vai). Thay bằng: `app_worker` NOINHERIT là thành viên; chỉ `internal/shared/db` được đổi vai (archlint `agnosessionset`: `SET LOCAL ROLE`, `set_config('role', …)`, tên `r_agg` ngoài `shared/db`/`shared/dbadmin`, gọi `db.WithAggTx` ngoài `internal/platform/metrics/app` đều bị chặn); giao dịch job đặt `transaction_read_only = on` và `statement_timeout = 10min` cục bộ. Tách `app_agg` trước khi bật production (thêm `LoginUnit` mới, bí mật `AG_DB_PASSWORD_APP_AGG`, bỏ `Agg` khỏi `app_worker`).

## Lên bản (thứ tự)

1. `cmd/migrate bootstrap` bản W6.4 (tài khoản quản trị cụm) — tạo `r_agg` (NOLOGIN BYPASSRLS, thành viên `r_<module>_pub`) và cấp `r_agg` cho `app_worker`. Chạy lặp an toàn.
2. `cmd/migrate up` — pf 000129 dừng với lỗi "thiếu vai r_agg … chạy cmd/migrate bootstrap bản W6.4" nếu bỏ bước 1.
3. Kiểm: `SELECT rolbypassrls, rolcanlogin FROM pg_roles WHERE rolname = 'r_agg'` → `t, f`; `go test -run 'SchemaIsolation|LoginRoles' ./migrations/` trên staging.

Lùi bản: không cần thu hồi — nhị phân cũ không gọi `db.WithAggTx`. Không xóa sổ `agg_job`/`agg_run` (chỉ ghi thêm).

## Thêm hoặc sửa job (bốn mắt)

1. Lập trình viên thêm/sửa `domain.AggJob` trong `aggjobs.go`: **mọi** thay đổi (SQL, view, chiều, số đo, mục đích, k) phải tăng `Version` — bộ chạy so băm định nghĩa (`AggJob.Hash`) với dòng đã duyệt.
2. Người đề xuất gọi `Agg.Propose` (tenant quản trị): kiểm định nghĩa (chỉ đọc view `_pub` đã khai, k ≥ k sàn), phiên bản lớn hơn mọi phiên bản đã đề xuất; ghi `agg_job` (`proposed`) + yêu cầu `pf_workflow.request` kind `agg.job`, subject = id đề xuất — cùng giao dịch.
3. Người khác gọi `Agg.Decide` (`approved` | `rejected` + ghi chú): quyết định `pf_workflow.decision` + dòng `agg_job` cùng giao dịch; người quyết định = người đề xuất → `self_approval_forbidden` (app, CHECK `decision_four_eyes`, CHECK `agg_job_four_eyes`).
4. Bộ chạy chỉ chạy phiên bản **đã duyệt mới nhất**: phiên bản cũ (đã có phiên bản mới hơn được duyệt), chưa duyệt, bị từ chối, định nghĩa khác băm → `metrics.agg_job_not_approved`, không đổi vai.

Chưa có HTTP/CLI: gọi qua gốc lắp ráp (`metrics.Agg(clk, metrics.AggDeps{…})`) khi bật. Danh mục job có trong rà soát quyền định kỳ (R-SEC-04): đọc `pf_metrics.agg_job` của tenant quản trị.

## Chạy và nhật ký

`Agg.Run(ctx, job, actor, k)` (k = 0 → k áp = max(k job, k sàn)):

1. Giao dịch CHỈ ĐỌC dưới `r_metrics`, `app.tenant_id` = tenant quản trị → kiểm job đã duyệt (trên). Lỗi → ROLLBACK, không đổi vai.
2. `SET LOCAL ROLE r_agg` (xóa biến ngữ cảnh `app.*`) → liệt kê tenant qua `k_ref.org` (vượt RLS) → với từng tenant đặt `app.tenant_id` cục bộ và chạy SQL của job trên view `_pub`. View `_pub` (W6.3) chạy bằng quyền chủ `ag_migrator` có FORCE RLS, nên BYPASSRLS của bên đọc **không** xuyên qua view — đọc theo lát tenant là chủ ý; tenant không có dòng `k_ref.org` không được đếm.
3. Gộp nhóm và ẩn nhóm có ít hơn k tenant góp **trong tiến trình**; bản ghi chi tiết không rời bộ chạy. Job công bố cả tổng phải tự ẩn ô bù (ADR-0031 §4).
4. Ghi một dòng `pf_metrics.agg_run` (giao dịch `r_metrics` riêng): job, phiên bản, băm, dòng danh mục đã duyệt, người/tiến trình gọi, bắt đầu/kết thúc, số tenant/dòng đọc, số nhóm ra/bị ẩn, k, kết quả `succeeded` | `failed` | `refused` + mã lỗi. Không ghi được → trả `metrics.dependency_unavailable`, **không** trả đầu ra.

Theo dõi: `SELECT job_id, version, outcome, error_code, k_used, groups_out, groups_suppressed, started_at, finished_at FROM pf_metrics.agg_run WHERE tenant_id = '<tenant quản trị>' ORDER BY recorded_at DESC` (dưới `r_metrics`). Cảnh báo khi có `refused` không mong đợi (định nghĩa sửa mà chưa duyệt) hoặc `failed` liên tiếp.

## Sự cố

| Triệu chứng | Nguyên nhân | Xử lý |
|---|---|---|
| `metrics.agg_job_not_approved` | chưa đề xuất/duyệt phiên bản trong mã, đã duyệt phiên bản mới hơn, sửa định nghĩa không tăng phiên bản | đề xuất + duyệt đúng phiên bản; triển khai nhị phân khớp phiên bản đã duyệt |
| `metrics.agg_k_below_floor` | k xin < k sàn hoặc k job < `publicdata.k_min` mới | tăng k của job (phiên bản mới) |
| `42501` khi đổi vai | bootstrap W6.4 chưa chạy (thiếu `r_agg` hoặc `app_worker` chưa là thành viên) | chạy `cmd/migrate bootstrap` |
| `57014` (statement_timeout) | SQL của job chậm (> 10 phút mỗi câu) | tối ưu SQL (phiên bản mới), chạy trên bản sao chỉ đọc |
