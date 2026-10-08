# ADR-0029: `k_ref` — bản chiếu chỉ đọc của dữ liệu lõi trong mỗi cụm, độ trễ có giới hạn

**Trạng thái:** Đề xuất — chờ MKG ký (A7) · **Ngày:** 04/10/2026 (gói W0.1) · **Phụ lục B v4.1:** mục 8 · **Phát hiện DB:** DB-02, DB-05, DB-13, DB-14 · **Thành phần:** module mới `pf/kref` (`k/ref`, `kind: kernel`), schema `k_ref` ở cụm `pf` và `mkg`, `pf/party`, `pf/masterdata`, `restore-drill` · **Liên quan:** [ADR-0005](0005-rls-mot-policy-moi-bang.md), [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), kiến trúc v4.1 §17.1, §34

## Bối cảnh

- RLS hiện chỉ đọc `app.tenant_id` (ADR-0005). Phạm vi Nút, phòng ban, xã phụ trách, mục đích chỉ kiểm ở tầng ứng dụng (`authz.Principal`, KN P1). Lỗi mã ở một handler là đủ để Cấp Xã thấy xã khác: mất lớp chặn thứ hai.
- Muốn RLS kiểm phạm vi Nút và cấp phép, chính sách cần đọc cây tổ chức và cấp phép **trong cùng cụm**. Chính sách không được tra cụm khác (DB-02), và mỗi module chỉ đọc bảng của chính nó (R-MOD-01).
- Kiến trúc v4.1 §17.1 đặt trong mỗi cụm một schema `k_ref` chỉ đọc, do worker của `k/ref` dựng từ sự kiện. Đây là **ngoại lệ duy nhất** của R-MOD-01 (R-DB-05).
- Ở repo, nguồn sự thật của cây tổ chức, cấp phép nằm ở cụm `pf` (`pf_party.org`, `pf_party.share_grant`), đơn vị hành chính ở `pf_masterdata.admin_unit`. v4.1 đặt nguồn ở cụm `idp` (`k_access`, `k_orgtree`); tách như vậy là việc G4 (bản đối chiếu §2.4).

## Quyết định

### 1. Schema `k_ref` trong mỗi cụm dữ liệu

Có ở cụm `pf` và `mkg`. Không cần ở `ts`, `vault` cho đến khi có chính sách đọc nó.

| Quan hệ | Nội dung | Nguồn ở repo |
|---|---|---|
| `org` | Đơn vị tham gia: mã, loại, trạng thái | `pf_party.org` |
| `node_closure` | Cặp (tổ tiên, hậu duệ, độ sâu) của cây Nút | cây `pf_party.org` + khu vực, phòng ban (pf 000062) |
| `admin_unit` | Đơn vị hành chính có hiệu lực theo thời gian | `pf_masterdata.admin_unit` (ADR-0032) |
| `share_grant_active` | Cấp phép chia sẻ đang hiệu lực | `pf_party.share_grant` |
| `share_scope` | (đơn vị xem, mục đích, bảng, đơn vị chủ, Nút) — cấp phép × cây Nút | dựng bởi worker; **rỗng** cho đến W4.2 ([ADR-0030](0030-doc-tai-cho-co-cap-phep.md)) |
| hàm `ctx_uuid(k)`, `ctx_text(k)` | Đọc biến ngữ cảnh, chuỗi rỗng → NULL (DB-13); `STABLE`, được inline | thay dần `shared.current_tenant()` |

Định nghĩa chính xác "Nút" (đơn vị, khu vực, phòng ban nào là Nút) chốt ở W2.5 cùng sự kiện mới `pf.party.org.moved`.

### 2. Ai ghi, ai đọc

- **Chỉ** worker `pf.kref` ghi, bằng vai `r_kref` qua `WithContextTx` (ADR-0028). Không module nào khác có `INSERT/UPDATE/DELETE` trên `k_ref`.
- Mọi `r_*` có `SELECT` trên `k_ref` (R-DB-05). Đọc trực tiếp bảng, **không** qua hàm `SECURITY DEFINER`: hàm như vậy không được planner inline, bị gọi theo từng hàng (DB-14). `node_closure` chỉ chứa cấu trúc cây, không chứa dữ liệu nghiệp vụ.
- Quan hệ `k_ref` không có `tenant_id` theo nghĩa RLS: chúng là danh mục cấu trúc, đọc được bởi mọi vai module. Không đặt PII vào `k_ref`.
- `k_ref` được khai trong `module.yaml` của `pf/kref` ở `provides.publications` (ADR-0027).

### 3. Dựng từ sự kiện, kể cả khi nguồn cùng cụm

- Worker tiêu thụ `pf.party.org.*`, `pf.party.org.moved` (mới), `pf.masterdata.admin_unit.changed`, `pf.party.share_grant.*` qua outbox/inbox hiện có, idempotent theo khóa sự kiện.
- Cây Nút di chuyển hoặc cấp phép thay đổi → worker dựng lại **các dòng liên quan trong một giao dịch** (không có trạng thái nửa vời).
- Cụm `pf` dùng sự kiện dù nguồn cùng cụm, để **một cơ chế** chạy được ở mọi cụm và khi nguồn chuyển sang cụm `idp` (G4) không phải viết lại.
- Cụm `mkg` có `k_ref` cùng hình. Nguồn của nó đi qua kênh được phép cho vùng nội bộ theo bảng luồng ADR-0008 (không kết nối DB chéo cụm, không import chéo vùng). Kênh cụ thể chốt ở W2.5.

### 4. Độ trễ có giới hạn

- Mục tiêu: **≤ 5 giây ở p99** từ lúc sự kiện nguồn được ghi đến lúc `k_ref` phản ánh.
- Chỉ số `k_ref_lag_seconds` (theo quan hệ) có cảnh báo khi vượt 5 giây. Runbook P2 "`k_ref` trễ" (bản đối chiếu §3.7).
- Trong khoảng trễ, đơn vị vừa bị thu hồi cấp phép còn đọc được. Rút ngắn: thu hồi **đồng thời tăng epoch ngữ cảnh** của mọi tài khoản thuộc đơn vị xem; phiên phải làm mới ngữ cảnh ở yêu cầu kế tiếp, và việc làm mới chờ `k_ref` bắt kịp (W4.1, W7.2).
- Bề mặt `/v1/partner/*` hiện có (ADR-0020) tiếp tục kiểm cấp phép trên bảng hiện hành `pf_party.share_grant`, thu hồi có hiệu lực ngay. Nó **chặt hơn** `k_ref` và giữ nguyên cho đến khi cổng tích hợp thay nó (ADR-0033).

### 5. Sao lưu và diễn tập phục hồi

- `k_ref` là dữ liệu **dẫn xuất**. Nguồn sự thật là sổ của `pf/party`, `pf/masterdata`.
- `restore-drill` thêm bước: sau khi phục hồi cụm, xóa và **dựng lại** `k_ref` từ nguồn, rồi so khớp (số dòng + băm theo quan hệ) với một lần dựng độc lập từ nguồn. Lệch → diễn tập thất bại.
- Thứ tự phục hồi: cụm nguồn trước, `k_ref` sau; dịch vụ đọc qua RLS phạm vi chưa mở cho đến khi `k_ref_lag_seconds` về dưới ngưỡng.

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Chính sách RLS gọi hàm `in_subtree()` / `in_node_scope()` (SAD định danh 6.2) | Hàm có truy vấn con không được inline, gọi theo từng hàng (DB-14). Phép thử T9–T11 của v4.1 |
| View trực tiếp lên `pf_party.org` thay vì bản chiếu | Chỉ chạy được trong cụm `pf`; `mkg` không có nguồn. Cho mọi vai đọc schema `pf_party` là vi phạm R-MOD-01 rộng hơn ngoại lệ có tên |
| Kết nối DB chéo cụm (FDW, dblink) | Cấm (R-03, DB-02). Hỏng cụm nguồn làm hỏng RLS cụm khác |
| Đồng bộ đồng thời (ghi `k_ref` trong giao dịch nguồn) | Chỉ được trong cùng cụm; không mở rộng sang `mkg`, `idp` |

## Hệ quả

- Thêm một thành phần có thể trễ hoặc hỏng. Hỏng worker → cây Nút cũ: người dùng **thấy ít hơn hoặc nhiều hơn** trong tối đa thời gian trễ. Giám sát và runbook là bắt buộc trước khi bật chính sách đọc `k_ref` ở production.
- Nút mới tạo chưa vào `node_closure` → dữ liệu gắn Nút đó tạm không thấy qua RLS phạm vi. Luồng tạo Nút ở `pf/party` chờ `k_ref` bắt kịp trước khi trả về (đọc chỉ số trễ hoặc phiên bản), hoặc giao diện báo "đang đồng bộ".
- Phép thử `TestNoTenantContextSeesNothing` mở rộng cho `ctx_uuid`/`ctx_text`.
- `shared.current_tenant()` (ADR-0005) giữ đến khi mọi chính sách chuyển sang `k_ref.ctx_uuid`.

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| W2.4 | Hàm `k_ref.ctx_uuid`, `k_ref.ctx_text`; biến ngữ cảnh trong `WithContextTx` | trong pf 000091 |
| W2.5 | Quan hệ `k_ref`, module `pf/kref`, worker `pf.kref`, sự kiện `pf.party.org.created`/`moved`, `pf.party.share_grant.approved`/`revoked`, `SELECT` cho mọi `r_*`, `k_ref_lag_seconds` + cảnh báo, bước dựng lại trong `restore-drill` | pf 000092, mkg 000022 |
| W3.4 | Chính sách `p_scope` đọc `k_ref.node_closure` trên `pf_coop` | pf 000097 |
| W4.2 | Worker dựng `share_scope` | pf 000099 |

**W2.4 (05/10/2026, pf 000091):** `CREATE SCHEMA IF NOT EXISTS k_ref` cùng `k_ref.ctx_text(k)` và `k_ref.ctx_uuid(k)`. Hai hàm `LANGUAGE sql STABLE PARALLEL SAFE`, thân là một biểu thức, nên được inline (`EXPLAIN VERBOSE` chỉ còn `current_setting`). Khác bản §17.2:

- `ctx_uuid` trả NULL, không ném lỗi, khi giá trị không phải UUID dạng chuẩn;
- tên ngoài `app.[a-z][a-z0-9_]*` bị từ chối, lỗi 22023 qua `k_ref.ctx_reject`.

`REVOKE … FROM PUBLIC`. USAGE schema và EXECUTE cấp cho 17 `r_*`, danh sách khớp `dbadmin.ModuleGroups` (`TestMigration91MatchesModuleGroups`). Kiểm thử: `TestCtxFunctions_W24`, `TestCtxFunctionGrants_W24` (`internal/shared/db`). `shared.current_tenant()` giữ nguyên. Tệp pf 000091 đã thuộc W2.4, nên các quan hệ `k_ref` của W2.5 phải vào số kế tiếp (pf 000092).

**W2.5 (05/10/2026, pf 000092, mkg 000022):**

- **Module.** `internal/platform/kref` (`pf/kref`, `aka: [k/ref]`, `kind: kernel`, `zones: [pf, mkg]`, `enable: always`) sở hữu `k_ref` (`owns_schema`; `modreg` cho phép schema lõi `k_*` với module kernel; mục tạm `k_ref` trong `ops_schemas` đã gỡ). Module không import module nào. Port `kref/port`: `OrgSource`, `ShareGrantSource` (pf/party cài), `AdminUnitSource` (pf/masterdata cài), cùng bằng vai DB của module nguồn.
- **"Nút" = mọi dòng `pf_party.org`.** Khu vực là thuộc tính của Nút xã, phòng ban là cấu hình của đơn vị (KN v7.7 FN-AD-04.02/04.03), không phải Nút.
- **Quan hệ (pf 000092).** `org` (mã, loại, đơn vị hành chính, cha, trạng thái, `source_seq`), `node_closure` (tổ tiên, hậu duệ, độ sâu; `(x, x, 0)` cho chính Nút), `admin_unit` (phiên bản hiệu lực + `successor_codes`), `share_grant_active` (cấp quyền status `active`, tenant = bên cấp), `share_scope` (khóa đúng §17.3, **rỗng** tới W4.2).
- **Lệch §2 — có RLS theo tenant.** Nguồn ở repo là bảng theo tenant (tenant = đơn vị tham gia, RB-2), nên `org`, `node_closure`, `admin_unit` dùng `shared.apply_tenant_rls`. Chính sách `p_scope` của W3.4 (`EXISTS node_closure` cùng tenant) không đổi. `share_grant_active` thấy được ở bên cấp **và** bên nhận. `share_scope` theo `viewer_tenant`. Hai chính sách viết tay đọc biến qua `k_ref.ctx_uuid` (R-DB-08). `WITH CHECK` chỉ cho tenant hiện hành.
- **Quyền.** 18 `r_*` có `SELECT` + `EXECUTE k_ref.ctx_*`; danh sách khớp `dbadmin.ModuleGroups` (`TestMigration92MatchesModuleGroups`). Chỉ `r_kref` có `INSERT/UPDATE/DELETE`, không ai có `TRUNCATE`. `r_kref` là vai nhóm mới, không có vai cũ (`dbadmin.groupOnlyModules`, cụm pf + mkg); thành viên duy nhất là `app_worker` vì `cmd/worker` ghép `pf/kref`. `ag_ops_jobs` (trước là `ag_outbox_publisher`, chuyển ở pf 000114) chỉ đọc cột `tenant_id` của `k_ref.*` và bảng nguồn, để tìm tenant khi dựng lại.
- **Sự kiện.** pf/party phát `pf.party.org.created` (tạo tổ chức) và `pf.party.org.moved` (`MoveOrg`: đổi cha và/hoặc mã ĐVHC tại chỗ). Cả hai mang ảnh chụp sau sự việc + `seq`. pf/party cũng phát `pf.party.share_grant.approved`/`revoked`, không mang mục đích dạng văn bản tự do. `pf.masterdata.admin_unit.changed` đã phát từ lần nhập danh mục (kể cả seed). Đổi tên không phát: tên không có trong `k_ref`.
- **Worker `pf.kref`** (`cmd/worker`, vai đăng nhập `app_worker` → `SET LOCAL ROLE r_kref`) xử lý idempotent qua `shared.inbox`. Mỗi sự kiện áp trong một giao dịch, có khóa tư vấn theo tenant.
  - Ảnh chụp có `seq` ≤ bản đã áp thì bỏ qua.
  - Nút mới: thêm bao đóng từ tổ tiên của cha.
  - Di chuyển: bỏ cặp (tổ tiên ngoài nhánh, hậu duệ trong nhánh), thêm (tổ tiên của cha mới × nhánh). Từ chối tạo vòng.
  - Cha hoặc Nút chưa có trong `k_ref` → `kref.ErrNotYetProjected`: sự kiện được giao lại, vẫn thiếu thì vào DLQ.
  - ĐVHC: bản đóng hiệu lực thắng bản mở.
  - Cấp quyền thu hồi: xóa dòng.
- **Dựng lại toàn bộ.** `kref.Module.Rebuild` / `RebuildTenant`, mỗi tenant một giao dịch:
  - đọc nguồn bằng vai của module nguồn, rồi kiểm cây (`kref.Project` bắt vòng và cha thiếu);
  - xóa và chèn lại theo tenant; bao đóng tính bằng CTE đệ quy trong DB;
  - `share_scope` không đụng.

  Chạy bằng `worker -kref-rebuild`: **bắt buộc một lần sau triển khai W2.5**, vì Nút có trước W2.5 chưa từng phát `created`. Chạy lại sau mỗi lần phục hồi.
- **Diễn tập.** `drill kref -confirm-copy` (bước mới của `restore-drill.yml`, chạy sau `drill check`). Lệnh chụp `k_ref`, dựng lại, chụp lại, rồi so số dòng + SHA-256 theo quan hệ với lần dựng độc lập trong Go (`kref.Expected`). "Sau ≠ mong đợi" là hỏng. "Trước ≠ mong đợi" chỉ được báo, vì `k_ref` trống sau phục hồi là bình thường. Băm không tính `seq` và thời điểm đồng bộ.
- **Chỉ số.** `ag.k_ref.lag` là histogram giây theo `relation`, tính từ `recorded_at` của outbox tới lúc áp xong; Prometheus `ag_k_ref_lag_seconds`. Cảnh báo `AgKRefLagHigh`: p99 > 5 giây trong 5 phút, P2 (`agrigreen-platform.rules.yml`).
- **Cụm mkg (mkg 000022).** Schema, hàm `ctx_*` và năm quan hệ cùng hình với pf. 7 vai module `mkg/*` có `SELECT`, `r_kref` ghi. **Kênh nguồn chốt:** không nối DB chéo cụm, không import chéo vùng. Nguồn là cụm pf, đi qua bề mặt đối tác có cấp quyền (ADR-0020), về sau là cổng tích hợp (ADR-0033). Bên ghi là pf/kref chạy trong `cmd/worker-internal`. Bảng để rỗng cho tới gói nối kênh (W7.4): chưa có chính sách nào của mkg đọc `k_ref`.
- **Kiểm thử.**
  - Đơn vị: `internal/platform/kref` (closure, vòng, băm).
  - Tích hợp `kref`: sự kiện → dòng, di chuyển nhánh, sai thứ tự, idempotent, dựng lại khớp nguồn, RLS, `r_*` chỉ đọc, `r_kref` ghi, `app_platform` không `SET ROLE r_kref` được, cụm mkg.
  - Chấp nhận `TestKRefIncrementalEqualsRebuild_W25`: Admin API thật → outbox → worker = dựng lại = nguồn.
  - Diễn tập: `TestDrillKRefRebuildMatchesSource_W25`.

## Liên quan

- Kiến trúc v4.1: §13 (cụm và schema), §17.1 (R-DB-05), §17.2 (`ctx_uuid`), §34 (dựng lại `k_ref` khi phục hồi), §36, Phụ lục B mục 8, Phụ lục D (SAD 6.2 → `k_ref`).
- Bản đối chiếu: §1.3 mục 3, §2.3, §2.5 (DB-02, DB-05), §3.7.
- [ADR-0005](0005-rls-mot-policy-moi-bang.md), [ADR-0008](0008-phan-vung-mang.md), [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md); ADR-0032, ADR-0033 (gói W0.2).
