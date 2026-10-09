# ADR-0031: Vai tổng hợp `r_agg` và view công bố cho dashboard, thống kê vượt đơn vị có ngưỡng k

**Trạng thái:** Đề xuất — chờ MKG ký (A7); ngưỡng k `[KÝ]` (K-8, C-4, Q5-3) · **Ngày:** 04/10/2026 (gói W0.1) · **Phụ lục B v4.1:** mục 10 · **Phát hiện DB:** DB-10 · **Thành phần:** `pf/metrics` (`cap/analytics`), schema `pf_<module>_pub`, job tổng hợp trong `worker`, `pf/workflow` (W6.1) · **Liên quan:** [ADR-0005](0005-rls-mot-policy-moi-bang.md), [ADR-0024](0024-diem-kiem-soat-xuat-du-lieu-dlp.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), kiến trúc v4.1 §22

## Bối cảnh

- Dashboard theo người dùng (Cấp Tỉnh xem tỉnh mình) cần đọc dữ liệu nhiều module. R-DB-10: `cap/analytics` chỉ đọc view công bố `<module>_pub`, không đọc bảng nội bộ.
- Một số chỉ số phải tính **trên mọi đơn vị**: chỉ số tài sản dữ liệu (độ phủ, độ tươi, sai số dự báo), van dữ liệu công, tập dữ liệu tổng hợp theo xã cho đối tác (ADR-0035, W0.2). RLS chặn việc này theo thiết kế.
- v4.1 §22.2 (R-DB-11): vai `r_agg` có `BYPASSRLS`, chỉ cho job đã khai và đã duyệt, chạy trên bản sao, đầu ra chỉ ở mức tổng hợp với ngưỡng k, mỗi lần chạy có nhật ký. R-SEC-03: `r_agg` là tài sản đặc quyền, bốn mắt.
- Repo hiện tại (DB-10): tổng hợp có k (`collection_volume`, `cropcycle/app/dataproduct.go`, `publicdata.k_min` = 10 tạm) chạy **trong một** tenant bên cấp, không vượt RLS. Chưa có tổng hợp toàn nền tảng.
- Vai BYPASSRLS đang có: `ag_outbox_publisher`, chủ hàm giám sát `mkg`/`ts` (NOLOGIN, `SECURITY DEFINER` để đếm). Không có vai BYPASSRLS cho mã ứng dụng (R-30, R-31).

## Quyết định

### 1. Hai đường, tách rõ

| Đường | Dùng cho | Vai | RLS |
|---|---|---|---|
| **Dashboard theo ngữ cảnh** | Mọi màn hình người dùng, kể cả Cấp TW xem toàn bộ phạm vi của mình | `r_metrics` (= `r_cap_analytics`) qua `WithContextTx` | **Áp đầy đủ** (tenant, Nút, nhánh chia sẻ) |
| **Job tổng hợp vượt đơn vị** | Chỉ số tài sản dữ liệu, van dữ liệu công, tập dữ liệu dựng sẵn có k | `r_agg` | Vượt — có kiểm soát ở mục 3 |

Không màn hình, endpoint hay truy vấn theo yêu cầu người dùng nào được dùng `r_agg`.

### 2. View công bố `_pub`

- Mỗi module công bố view có phiên bản trong `pf_<module>_pub`, khai ở `provides.publications` của `module.yaml` (ADR-0027). View là hợp đồng như port: đổi bảng nội bộ không được làm vỡ view.
- View tạo với `security_invoker = true`, để RLS và quyền áp theo vai đang gọi (`r_metrics` hay `r_agg`), không theo chủ view.
- `r_<module>_pub` (NOLOGIN) có `SELECT` trên schema `_pub`, được cấp cho `r_metrics` và `r_agg`. Không vai nào trong hai vai này có quyền trên bảng nội bộ.
- View không chứa cột PII trực tiếp (tên, SĐT, số định danh). Mã hộ chỉ ở dạng giả danh.

### 3. `r_agg`

- `r_agg`: NOLOGIN, **BYPASSRLS**, chỉ `SELECT` trên các view `_pub` mà job đã khai cần, và trên `k_ref`. Không có quyền ghi nào.
- Vai đăng nhập riêng `app_agg` (NOINHERIT), chỉ là thành viên `r_agg`, chỉ dùng bởi bộ chạy job tổng hợp. Không `app_*` nào khác được cấp `r_agg` (ADR-0028).
- Chạy trên **bản sao chỉ đọc** khi có. Khi chưa có bản sao (ĐTK 1), chạy trên cụm chính với `default_transaction_read_only = on` và `statement_timeout` đặt cho `app_agg`. `pg_hba` chỉ cho `app_agg` kết nối từ đơn vị `worker`.
- Job đọc qua `r_agg`, gộp nhóm và ẩn nhóm nhỏ **trong tiến trình**, rồi ghi kết quả bằng vai thường (`r_metrics`) qua `WithContextTx` trên cụm chính. Bản ghi chi tiết không rời job.

### 4. Job đã khai

Mỗi job có định nghĩa có phiên bản trong mã (đăng ký lúc biên dịch):
- mã job, phiên bản định nghĩa, view `_pub` đọc, chiều gộp nhóm, chỉ số, tập dữ liệu đầu ra;
- `k` của job: **không nhỏ hơn** ngưỡng k đã ký; job từ chối chạy nếu tham số thấp hơn;
- quy tắc ẩn: nhóm dưới k bị gộp lên cấp cao hơn hoặc ẩn; khi công bố cả tổng và thành phần, ẩn thêm ô bù để không suy ra được nhóm đã ẩn bằng phép trừ.

Thêm hoặc sửa job là **thay đổi đặc quyền**: đi qua `pf/workflow` (W6.1), người duyệt ≠ người đề xuất. Danh sách job có trong rà soát quyền định kỳ (R-SEC-03, R-SEC-04).

### 5. Nhật ký mỗi lần chạy

Sổ chỉ ghi thêm: job, phiên bản định nghĩa, `k`, thời điểm bắt đầu/kết thúc, số bản ghi đọc, số nhóm đầu ra, số nhóm bị ẩn, kết quả. Không ghi được nhật ký → job thất bại, không công bố đầu ra. Đầu ra ra ngoài nền tảng (đối tác, công bố) đi qua điểm kiểm soát xuất (ADR-0024).

### 6. Danh mục vai BYPASSRLS

Chỉ các vai sau được có `BYPASSRLS`; CI kiểm `pg_roles` sau migration và thất bại nếu có vai khác:

| Vai | Lý do | ADR |
|---|---|---|
| `r_agg` | Job tổng hợp đã khai | ADR này |
| `ag_outbox_publisher` | Đọc outbox mọi tenant để phát sự kiện | hiện có |
| chủ hàm giám sát `mkg`, `ts` (NOLOGIN) | Hàm `SECURITY DEFINER` đếm xuyên tenant | hiện có (mkg 000013, ts 000003) |

### 7. Ngưỡng k `[KÝ]`

Ngưỡng k là mục `[KÝ]` của v4.1 (K-8, Phụ lục C C-4, câu hỏi Q5-3). Cho đến khi ký, dùng giá trị tạm `publicdata.k_min = 10` hiện có. Job tổng hợp vượt đơn vị **không bật ở production** trước khi k được ký.

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Không có vai vượt RLS; mỗi đơn vị tự tổng hợp rồi cộng | Không tính được chỉ số toàn nền tảng có k đúng (k phải áp trên nhóm sau khi gộp mọi đơn vị); mỗi đơn vị phải tự công bố số nhỏ |
| Cho dashboard Cấp TW dùng `r_agg` | Biến đặc quyền thành đường thường; mất RLS cho truy vấn tùy biến của người dùng |
| Nhánh "đọc tổng hợp" trong chính sách RLS | Chính sách không biết truy vấn có gộp nhóm hay không; không áp được k ở DB |
| Hàm `SECURITY DEFINER` cho từng chỉ số | Rải đặc quyền trong nhiều hàm, khó rà soát; không có nhật ký chạy thống nhất |

## Hệ quả

- `r_agg` là vai mạnh nhất mà mã ứng dụng chạm tới. Bảo vệ bằng: chỉ `SELECT` trên `_pub`, vai đăng nhập riêng, `pg_hba`, chỉ đọc, job đăng ký lúc biên dịch, bốn mắt, nhật ký, cổng CI danh mục BYPASSRLS.
- Tổng hợp trong một tenant hiện có (`collection_volume`) **không** cần `r_agg` và giữ nguyên cho đến khi có đơn vị thứ hai (DB-10).
- `pf/metrics` chuyển sang chỉ đọc `_pub` (W6.3) trước khi có `r_agg`.
- Thêm chỉ số giám sát: số lần chạy, số nhóm bị ẩn, job thất bại.

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| W6.1 | `pf/workflow` (bốn mắt cho thêm job) | pf 000118 |
| W6.3 | Schema `_pub`, view có phiên bản, vai `r_<module>_pub` (thành viên: `r_metrics`), `pf/metrics` chỉ đọc `_pub`, bản sao chỉ đọc `AG_DB_REPLICA_URL` — xong 09/10/2026 ([modules.md](../dev/modules.md#view-công-bố-_pub-w63)) | pf 000124 |
| W6.4 | `r_agg`, danh mục job, nhật ký chạy, cổng CI danh mục BYPASSRLS — mã xong 09/10/2026, bật production chờ C-4 (ghi chú dưới) | pf 000129 (000109 cũ bỏ) |
| W5.3 | Tập dữ liệu dựng sẵn cho cổng tích hợp qua ngưỡng k | pf 000126–000127 (000100–000101 cũ bỏ) |

Bản đối chiếu ghi gói `r_agg` là "W6.5" ở vài chỗ; số đúng theo kế hoạch là **W6.4**.

**W6.4 (09/10/2026)** — [modules.md](../dev/modules.md#vai-tổng-hợp-r_agg-w64), [runbook](../runbooks/agg-jobs.md):

- **Module chủ:** `pf/metrics` (`cap/analytics`): sổ `pf_metrics.agg_job` (danh mục có phiên bản: job, phiên bản, view `_pub`, SQL, mục đích, k, băm định nghĩa; `proposed` → đúng một `approved`/`rejected`) và `pf_metrics.agg_run` (nhật ký mỗi lần chạy, §5) ở **tenant quản trị**; `pf/workflow` giữ yêu cầu/quyết định (kind `agg.job`) — `r_metrics` gọi `pf_workflow.submit/decide` trong giao dịch của mình (§4 bốn mắt; CHECK `agg_job_four_eyes` ở cả sổ danh mục).
- **§3 vai:** `r_agg` NOLOGIN BYPASSRLS (bootstrap), thành viên `r_<module>_pub`, thêm SELECT cột `k_ref.org.tenant_id`. Lệch ban đầu (`app_worker` là thành viên `r_agg`) đã sửa ở gói FC (dưới).

**Gói FC (09/10/2026)** — tách vai đăng nhập `app_agg` (§3), không migration:

- `dbadmin.AppAgg`: LOGIN NOINHERIT, **chỉ** thành viên `r_agg` (INHERIT FALSE, SET TRUE), cụm pf; `ALTER ROLE app_agg SET default_transaction_read_only = on, statement_timeout = '10min'` (`dbadmin.AggRoleSettings`, bootstrap). `LoginUnit.Agg` bỏ — `app_worker` không còn thuộc `r_agg`; bootstrap thu hồi mọi thành viên `r_agg` khác `app_agg` (`dbadmin.ExclusiveMembers`), nên grant cũ của W6.4 tự gỡ.
- `db.WithAggTx(ctx, b, owner, governance, agg, authorize, fn)`: hai giao dịch chỉ đọc nối tiếp — kiểm duyệt trên pool đơn vị (`app_worker` → `r_metrics`), rồi đổi vai trên pool `app_agg` (`app_agg` không đọc được danh mục, `app_worker` không đổi được sang `r_agg` — kiểm thử 42501 cả hai chiều). Hệ quả: kiểm duyệt và đọc không còn chung một giao dịch; cửa sổ giữa hai giao dịch chỉ cho phép chạy phiên bản vừa bị thay bằng phiên bản mới hơn được duyệt cùng lúc — nhật ký vẫn ghi phiên bản đã kiểm.
- `cmd/worker -agg-run <job> -agg-actor <uuid>`: pool `app_agg` 2 kết nối, trên `AG_DB_REPLICA_URL` khi có (§3 bản sao chỉ đọc); `AG_AGG_GOVERNANCE_TENANT`. Chạy theo lịch vẫn chờ C-4 (§7).
- Triển khai: `AG_DB_PASSWORD_APP_AGG` — Helm Secret riêng `pf-db-agg` chỉ worker gắn (`check_flows.py`) + `pg-pf-roles` cho bootstrap; userlist PgBouncer compose + CI; compose/gen-dev-secrets. **Còn mở:** `pg_hba` chỉ cho `app_agg` từ worker (repo chưa quản lý `pg_hba`). Runbook [agg-jobs.md](../runbooks/agg-jobs.md).
- **§2 lệch đã có từ W6.3:** view `_pub` **không** `security_invoker` — chủ `ag_migrator` (NOBYPASSRLS, bảng gốc FORCE RLS), nên BYPASSRLS của `r_agg` không xuyên qua view. Bộ chạy liệt kê tenant bằng `k_ref.org` (đọc thẳng, vượt RLS) và đọc từng lát tenant (`app.tenant_id` cục bộ) trong cùng giao dịch; k áp trên nhóm sau khi gộp mọi tenant như §4. Chuyển sang view `security_invoker` đòi `r_agg` có quyền bảng gốc — không làm.
- **§4:** job khai trong mã (`domain.AggJob`, `app/aggjobs.go`); bộ chạy so băm định nghĩa với phiên bản đã duyệt **mới nhất** — phiên bản cũ, chưa duyệt, bị từ chối, sửa tại chỗ → `refused` (không đổi vai). Ô bù chưa cần (đầu ra chỉ có nhóm, không có tổng).
- **§6:** cổng `TestAggRole_W64` (danh mục `dbadmin`) + `TestSchemaIsolationAggRole_W64` (`pg_roles` mọi cụm, quyền hiệu lực của `r_agg`).
- **§7:** k sàn đọc qua `KFloorSource` (`publicdata.k_min` của tenant quản trị, chưa đặt → 10); job/k xin dưới sàn bị từ chối. Chạy theo lịch bật sau C-4 (gói FC thêm lệnh một lần `worker -agg-run`, chưa nối `KFloorSource` → 10).

## Liên quan

- Kiến trúc v4.1: §4 (van dữ liệu công, k `[KÝ]`), §22.1 (R-DB-10), §22.2 (R-DB-11), §30 (R-SEC-03), Phụ lục B mục 10, Phụ lục C C-4.
- Bản đối chiếu: §1.3, §2.5 DB-10, §3.4, §3.7.
- Kế hoạch: §4 K-8. Câu hỏi: Q5-3.
- [ADR-0005](0005-rls-mot-policy-moi-bang.md), [ADR-0024](0024-diem-kiem-soat-xuat-du-lieu-dlp.md), [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md); ADR-0035 (gói W0.2).
