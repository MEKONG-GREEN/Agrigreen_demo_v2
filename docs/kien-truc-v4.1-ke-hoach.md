# Kế hoạch chuyển sang kiến trúc v4.1

**Ngày:** 04/10/2026 · **Cơ sở:** [kien-truc-v4.1-doi-chieu.md](kien-truc-v4.1-doi-chieu.md) (mã các khoảng cách, xung đột X-xx) · **Câu hỏi:** [cau-hoi-ton-dong.md §H](cau-hoi-ton-dong.md#h-kiến-trúc-v41)

## 1. Nguyên tắc

1. **Mỗi gói một agent, chạy tuần tự.** Không chạy song song nhiều gói. Mỗi gói kết thúc bằng:
   - một commit, CI xanh;
   - cập nhật `docs/roadmap.md`;
   - nếu gói đổi quyết định đã có: ghi chú triển khai vào ADR liên quan.
2. **Không đổi tên thư mục, không đổi tên module.** Phụ lục D của v4.1 cho giữ `pf/*`, `mkg/*`. Tầng (`kernel | capability | domain`) khai trong `module.yaml`.
3. **Chuyển dần, chạy song song.**
   - Cơ chế mới đi cạnh cơ chế cũ ít nhất một kỳ phát hành: `WithContextTx` cạnh `WithTenantTx`, cổng tích hợp cạnh `/v1/partner/*`.
   - Bỏ cơ chế cũ là một gói riêng.
4. **Không UPDATE/DELETE dữ liệu nghiệp vụ.**
   - Chuyển bảng giữa schema bằng `ALTER TABLE … SET SCHEMA`, không sao chép.
   - Đổi quyền bằng GRANT/REVOKE.
5. **Ưu tiên theo hạn KN nhận dữ liệu thật 15/12/2026.**
   - Những gì phải chuyển dữ liệu (schema `pf_coop`, vai DB) làm **trước** mốc này.
   - Cổng tích hợp và năng lực dùng chung làm sau, vì chỉ thêm mới.
6. **Số migration cấp theo thứ tự gộp.** `golang-migrate` bỏ qua im lặng một số nhỏ hơn số đã áp. Gộp khác thứ tự thì cấp lại số trước khi gộp.
   - `pf 000086`–`000087` đã dùng trên `main`, nên `pf` bắt đầu từ **000088**.
   - Các cụm khác: mkg từ 000022 · shared từ 000006 · vault từ 000003 · ts từ 000004.

## 2. Các giai đoạn và gói

Cột **Khách?**:
- **Không:** làm được ngay, không cần MKG hay khách trả lời;
- **Sau:** làm được mã ngay, nhưng bật production phải chờ câu hỏi;
- **Chặn:** không làm được khi chưa có trả lời hoặc tài liệu.

### Giai đoạn 0 — Quyết định và tài liệu (chỉ tài liệu)

| Gói | Nội dung | Phụ thuộc | Migration | Khách? | CS |
|---|---|---|---|---|---|
| **W0.1** | **Năm ADR lõi:** ADR-0027 (bốn tầng khai trong manifest, ứng dụng là tổ hợp, gọi trong tiến trình — B1, B2; giữ thư mục và tên theo Phụ lục D; phòng ban KN là cấu hình của `dom/extension` — X-13) · ADR-0028 (vai đăng nhập theo đơn vị triển khai + `SET LOCAL ROLE` — B7, X-10) · ADR-0029 (`k_ref` trong mỗi cụm, trễ ≤ 5 s — B8) · ADR-0030 (đọc tại chỗ có cấp phép; sửa ADR-0005, ADR-0009, ADR-0020 phần cơ chế đọc — B9, X-02, X-11) · ADR-0031 (`r_agg` — B10). **Trạng thái: đã viết (chờ ký)** — [ADR-0027…0031](adr/README.md) | — | — | Không (ký: A7) | M |
| **W0.2** | **Năm ADR tích hợp, engine:** ADR-0032 (đơn vị hành chính có hiệu lực + bảng kế thừa — B11, X-17) · ADR-0033 (cổng tích hợp: MKG như mọi đối tác, feed cursor, webhook mỏng; `api-public` → `integration-gateway`; van RabbitMQ giữ đến G2 — B15, B18, X-03, X-15, X-16) · ADR-0034 (luồng song phương — B16, X-04, trạng thái **Đề xuất** chờ C-12) · ADR-0035 (cấp phép tổng hợp theo xã có ngưỡng k — B17) · ADR-0036 (engine ba chế độ ở vùng nội bộ, `lib/optcore` — B19, X-07). **Trạng thái: đã viết (chờ ký; ADR-0034 chờ thêm C-12)** — [ADR-0032…0036](adr/README.md); sửa ADR-0020, 0022, 0025, bổ sung ADR-0016 (dòng trạng thái) | W0.1 | — | Sau (C-12, k) | M |
| **W0.3** | **Ghi lệch và cập nhật tài liệu cũ:** ADR-0012 (vị trí `chain_head` chung — X-09) · ADR-0017 (không TimescaleDB — X-05) · ADR-0006 (C-1 không chặn — X-06) · ADR-0037 ngắn "tách xác thực và phân quyền, không mạo danh" ghi lại KN P1 phương án A (`kn-v7.7-gap.md` §4.5) và chế độ hỗ trợ có lý do (B14, X-14) · `docs/design/mkg.md` §1.1 "bốn cửa" + §3.2 bảng kê là song phương · đổi các dẫn chiếu "mục x v3.0" sang mục v4.1 trong g*-conventions · `docs/roadmap.md` thêm bảng "cổng v4.1 ↔ cổng repo" (X-19) | W0.1, W0.2 | — | Không | S |
| **W0.4** | **R-OPS-04:** đưa bản v4.1 (md) vào `docs/kien-truc/`; công cụ `tools/docsql` trích khối ```sql chạy theo thứ tự trên `pgtest` (dựng sẵn extension, schema, vai như v4.1 mô tả); job CI | — | — | Không | S |

### Giai đoạn 1 — Khung module (G0 của v4.1)

| Gói | Nội dung | Phụ thuộc | Migration | Khách? | CS |
|---|---|---|---|---|---|
| **W1.1** | `module.yaml` cho mọi module `internal/{platform,mkg,vault}/*` (khoảng 28): `kind`, `zones`, `version`, `depends` (từ import `port/` hiện có), `provides` (ports, events từ `contracts/events`, publications), `permissions` (từ realm roles hiện có), `shares` (rỗng), `owns_schema`, `enable`. Gói `internal/shared/modreg` đọc và kiểm lược đồ. Gán `kind: kernel` cho `pf/accounts`, `pf/party` (cây tổ chức). **Trạng thái: xong** — 26 tệp (kernel 4, capability 8, domain 14), `tools/modgen -check`, [hướng dẫn](dev/modules.md) | W0.1 | — | Không | M |
| **W1.2** | Analyzer `agmodgraph` trong `tools/archlint`: đồ thị không chu trình, chỉ đi xuống theo `kind`; import `port/` phải có trong `depends`; ràng buộc phiên bản thỏa; `module.yaml` khớp `owns_schema` với migration. Thêm vào CI. **Trạng thái: xong** — 5 quy tắc, vi phạm hiện có ghi trong `tools/archlint/modgraph_exceptions.yaml` (77 `cycle`, 24 `tier`, 17 `schema`; mỗi mục có gói gỡ: W3.2, W3.3, W5.2, W6.2, W6.3, W7.1, W7.2, W8.5, W9.1), cổng hỏng khi có vi phạm mới hoặc ngoại lệ lỗi thời; `modgen -check` + `archlint` trong job lint CI ([hướng dẫn](dev/modules.md#đồ-thị-module-agmodgraph-w12)) | W1.1 | — | Không | M |
| **W1.3** | `app/<KN\|HTX\|CGH\|SIM\|MKG\|NH\|PF>/app.yaml` (modules, roles seed, landing). Kiểm `cmd/*/assembly` ⊆ hợp các `app.yaml` của đơn vị triển khai | W1.1 | — | Không | S |
| **W1.4** | **Bật module theo đơn vị (R-MOD-08):** sổ + chiếu `pf_party.module_enablement_event`/`_current` (đơn vị × module × trạng thái); middleware trả `module_not_enabled` (RFC 9457); quyền liên quan không cấp được; menu `web-ops` lọc theo | W1.1 | **pf 000088** | Không | M |

### Giai đoạn 2 — Nền dữ liệu (DB-04, DB-06, DB-11, `k_ref`)

| Gói | Nội dung | Phụ thuộc | Migration | Khách? | CS |
|---|---|---|---|---|---|
| **W2.1** | **DB-11:** hạn lưu 30 ngày cho `shared.ledger_idem` (cột `expires_at` mặc định, chỉ mục) + job dọn trong `opsjobs`/`migrate purge-idempotency`. Ghi rõ trong ADR-0012: chống trùng chỉ bảo đảm trong 30 ngày (gấp 10 lần cửa sổ ngoại tuyến 72 giờ); khóa vẫn còn ở cột `idem_key` của sổ để tra cứu | — | **shared 000006** | Không | S |
| **W2.2** | **DB-06:** `CREATE EXTENSION btree_gist`; `EXCLUDE USING gist (tenant_id WITH =, code WITH =, daterange(valid_from, valid_to,'[)') WITH &&)` trên `pf_masterdata.admin_unit` (giữ trigger); bảng `pf_masterdata.admin_unit_succession` (predecessor, successor, effective_on, area_share, legal_basis); điền từ `successor_codes` bằng INSERT … SELECT | W0.2 | **pf 000089** | Sau (C-9 nguồn chính thức) | S |
| **W2.3** | **Vai DB — khung (DB-04, R-DB-02):** tạo `r_<module>` NOLOGIN cho mọi module pf, GRANT quyền hiện có của `<module>_app` sang `r_<module>`; vai `app_platform`, `app_worker`, `app_public`, `app_sync` LOGIN NOINHERIT; `shared/db.WithContextTx(ctx, pool, ModuleID, …)` đặt `SET LOCAL ROLE` + `app.tenant_id`; registry gán `ModuleID → vai` lúc biên dịch; lint cấm chuỗi `SET ROLE` ngoài `shared/db` (R-DB-03). Chuyển **2 module mẫu** (`pf/content`, `pf/notify`). Kiểm thử T4, T5 | W1.1 | **pf 000090**, **shared 000007** | Không | L |
| **W2.4** | **Biến ngữ cảnh trong DB:** `WithContextTx` đặt thêm `app.node_id`, `app.dept`, `app.xa_ids`, `app.purpose`, `app.actor_ref`, `app.on_behalf_of_ref` từ `authz.Principal`/KN context; hàm `k_ref.ctx_uuid`, `k_ref.ctx_text` (cùng mẫu `shared.current_tenant`). Chưa đổi chính sách | W2.3 | (trong pf 000091) | Không | S |
| **W2.5** | **`k_ref` trong cụm pf (và mkg):** schema `k_ref` (`org`, `node_closure`, `admin_unit`, `share_grant_active`, `share_scope` rỗng); worker `pf.kref` dựng từ sự kiện `pf.party.org.*`, `pf.masterdata.admin_unit.changed` (+ sự kiện mới `pf.party.org.moved`); `SELECT` cho mọi `r_*`; chỉ vai worker ghi (R-DB-05); metric `k_ref_lag_seconds` + cảnh báo; bước "dựng lại khớp nguồn" trong `restore-drill` | W2.4, W1.1 | **pf 000091–000092**, **mkg 000022** | Không | L |
| **W2.6** | **Vai DB — chuyển hết:** mọi module pf còn lại, mkg, ts, vault sang `r_*` + `WithContextTx`; bỏ quyền bảng khỏi các vai `<module>_app` (giữ tên vai một kỳ cho PgBouncer userlist); cập nhật `dbadmin`, compose, chart K8s (bí mật giảm từ khoảng 28 xuống 4–6 mỗi cụm); cổng CI "truy vấn ngoài `WithContextTx` thất bại" | W2.3 | **pf 000093**, **mkg 000023**, **ts 000004**, **vault 000003** | Không | L |

### Giai đoạn 3 — `pf/cooperative` làm chủ duy nhất (phải xong trước 15/12/2026)

| Gói | Nội dung | Phụ thuộc | Migration | Khách? | CS |
|---|---|---|---|---|---|
| **W3.1** | **Thiết kế ranh giới** `docs/design/cooperative.md`. Kiểm kê bảng sẽ chuyển (mục 2.2 bản đối chiếu), port mới (`Households`, `Plots`, `Seasons`, `Diary`, `Resolve`), danh sách bên tiêu thụ, `shares: [household, plot, season, diary_entry]`, quyền. Ghi giả định chủ dữ liệu (X-01, H-1) | W0.1 | — | Sau (H-1) | S |
| **W3.2** | **Module Go `pf/cooperative`:** port + app làm mặt tiền. Store tạm gọi port cũ của `party`/`geo`/`cropcycle`. Đổi bên tiêu thụ (`mechanization`, `mrv`, `extension` KN P7, `api-public`, `/v1/partner/*`, `mkg` không bị ảnh hưởng) sang `cooperativeport`. Không đổi DB | W3.1, W1.2 | — | Không | M |
| **W3.3** | **Chuyển schema:** `CREATE SCHEMA pf_coop`; `ALTER TABLE … SET SCHEMA pf_coop` cho `pf_party.{cooperative,household,household_membership}`, `pf_geo.{plot,plot_boundary,plot_event,cooperative_boundary,cooperative_area}`, `pf_cropcycle.{season_event,season_current,coop_season_area,farm log}`; vai `r_coop`; trigger sổ giữ nguyên (tham số `TG_ARGV` tên sổ cũ: thêm tên mới trong `chain_head` bằng INSERT, **không** UPDATE); chuyển store vào `pf/cooperative`. Diễn tập trên bản sao dữ liệu UAT | W3.2, W2.6 | **pf 000094–000096** | Không | L |
| **W3.4** | **RLS theo Nút trên `pf_coop`:** chính sách `p_scope` viết thẳng `EXISTS k_ref.node_closure` (§17.2); `v_missing_rls` chấp nhận `p_scope`; cổng CI `EXPLAIN` cấm SubPlan gọi hàm theo hàng (DB-14). Giữ kiểm ở ứng dụng | W3.3, W2.5 | **pf 000097** | Không | M |
| **W3.5** | **Hợp nhất dữ liệu UAT (CROSS-02):** công cụ nạp từ ba app UAT (KN, HTX, CGH) vào `pf_coop`, so khớp HTX/hộ/thửa, báo cáo lệch, tổng kiểm (R-INT-10) | W3.3 | — | **Chặn** (dữ liệu UAT, C-9) | M |

### Giai đoạn 4 — Chia sẻ trong vùng nền tảng (G2 của v4.1)

| Gói | Nội dung | Phụ thuộc | Migration | Khách? | CS |
|---|---|---|---|---|---|
| **W4.1** | **`share_grant` v2:** thêm `scope_node_ref`, `module_table` (chỉ bảng khai trong `shares`), `purpose_code`, `basis_ref` (thỏa thuận), `grant_class` (`intra_platform` \| `partner`) vào `pf_party.share_grant_event`/`share_grant` (cột NULL được); luồng tạo bởi quản trị viên đơn vị chủ; màn rà soát cấp phép định kỳ (R-SEC-04); thu hồi tăng epoch ngữ cảnh của tài khoản đơn vị xem | W0.1, W2.5 | **pf 000098** | Sau (C-10, H-1) | M |
| **W4.2** | **Đọc tại chỗ:** worker `k_ref` dựng `share_scope` (cấp phép × cây Nút) trong một giao dịch; nhánh `OR EXISTS share_scope` trên các bảng `shares` của `pf_coop`, `pf_mechanization.machine`; `WITH CHECK` không có nhánh; port trả `via_share_grant`; middleware ghi một sự kiện gộp mỗi yêu cầu; metric "đọc qua cấp phép theo đơn vị"; kiểm thử T6a–T6e, T9–T10, thu hồi + kết nối tái sử dụng qua PgBouncer | W4.1, W3.4 | **pf 000099** | Không | L |

### Giai đoạn 5 — Cổng tích hợp (G1+ của v4.1)

| Gói | Nội dung | Phụ thuộc | Migration | Khách? | CS |
|---|---|---|---|---|---|
| **W5.1** | **`contracts/integration/`:** feed `harvest_progress_by_commune` (k), `season_harvest_dates`, `waterway_routes`, `admin_units` (tham chiếu); nạp `collection_schedule`, `roll_count_statement`, `htx_roll_confirmation` (song phương); dùng lại `contracts/publication.Node`; `contractcheck` mở rộng | W0.2 | — | Không | M |
| **W5.2** | **`cmd/integration-gateway`** từ `cmd/api-public`: giữ khóa API HMAC, hạn mức, metering, DLP; thêm mTLS cho đối tác ghi lớn; định danh máy thuộc `pf/accounts` (`k/access`) qua port; `api-public` thành bí danh một kỳ; NetworkPolicy, chart | W5.1, W2.6 | — | Không | M |
| **W5.3** | **Feed và tập dữ liệu dựng sẵn (R-INT-05, R-INT-06):** kho feed chỉ ghi thêm (cursor, khóa thực thể, giữ 30 ngày); job dựng tập dữ liệu theo phiên bản (qua ngưỡng k); ảnh chụp lớn ra kho đối tượng, URL ký có hạn; endpoint `GET /v1/feeds/{feed}?cursor=` | W5.2 | **pf 000100–000101** | Sau (k: C-4) | L |
| **W5.4** | **Nạp theo lô (R-INT-03, R-INT-04, §41.3):** `POST /v1/batches/{contract}` → 202 + mã lô; hàng đợi và DLQ riêng từng đối tác; inbox; kiểm hợp đồng mặc định từ chối; gọi port module chủ; kết quả từng bản ghi; `429` + `Retry-After` | W5.2 | **pf 000102** | Không | L |
| **W5.5** | **MKG đọc qua feed:** bộ điều hợp trong `mkg/fieldops` kéo feed thay `/v1/partner/*`; lưu `as_of`/`max_age`; chạy song song một kỳ; bỏ client `mkg_sync` ở gói sau | W5.3 | **mkg 000024** | Không | M |
| **W5.6** | **Luồng song phương (§38.2):** bản ghi giao dịch MKG ↔ HTX ở `pf_coop` (hoặc `pf_cropcycle`) với chính sách chỉ hai bên (tenant MKG qua cổng; `org_ref` HTX trong tenant chủ); MKG gửi lịch gom, số cuộn qua W5.4 thay van `valve_collection`; HTX xác nhận → feed riêng của cặp; không tham gia tổng hợp; chỉ ghi thêm | W5.4, W5.5, W0.2 | **pf 000103**, **mkg 000025** | **Sau** (C-12 — bật production) | L |
| **W5.7** | **Webhook mỏng + ngắt mạch + đối soát (R-INT-07, R-INT-09, R-INT-10):** đăng ký webhook, chữ ký HMAC, chỉ báo cursor; ngắt sau N lần lỗi; tổng kiểm (số bản ghi + băm) theo phiên bản tập dữ liệu và theo ngày luồng song phương | W5.3 | **pf 000104** | Không | M |
| **W5.8** | **Kênh tệp cùng lược đồ (R-INT-11)** + môi trường thử đối tác dữ liệu tổng hợp (R-INT-08) | W5.4 | — | Không | M |
| **W5.9** | **Kiểm thử tải §42:** `tools/loadgen` kịch bản 200 đối tác, một đối tác gấp 10 lần hạn mức (p95 đối tác khác không tăng quá 10%) | W5.4, W5.7 | — | Sau (C-13, G-3 số liệu) | M |
| **W5.10** | **Bỏ lối riêng:** gỡ `/v1/partner/*`, client `mkg_sync`, consumer van `pf.cropcycle.htx_collection_valve` (giữ `hub_valve`, `mrv` tới khi có hợp đồng nạp tương ứng) | W5.5, W5.6 + một kỳ phát hành | — | Không | S |

### Giai đoạn 6 — Năng lực dùng chung

| Gói | Nội dung | Phụ thuộc | Migration | Khách? | CS |
|---|---|---|---|---|---|
| **W6.1** | **`pf/workflow` (`cap/workflow`):** sổ `request` + `decision`, CHECK người duyệt ≠ người lập ≠ người hưởng; chuyển duyệt tham số (`masterdata`) và cấp phép (`party`) sang dùng | W1.2 | **pf 000105** | Không | M |
| **W6.2** | **`pf/forms` (`cap/forms`):** tách phát hành/nộp/rà soát/chốt/mở lại kỳ + ảnh chụp đóng băng từ `pf/reports` + `extension/reportcycle`; KN dùng qua cấu hình (`hierarchy`, `publish_by`, `close_by`); đường dẫn `/v1/forms/*` (v1 cũ song song) | W6.1 | **pf 000106–000107** | Sau (Q-01: 14 mẫu) | L |
| **W6.3** | **`_pub` + `cap/analytics` (R-DB-10):** schema `pf_<module>_pub`, view có phiên bản khai trong `provides.publications`; `pf/metrics` chỉ đọc `_pub`; chạy trên bản sao chỉ đọc khi có | W2.6 | **pf 000108** | Không | M |
| **W6.4** | **`r_agg` (R-DB-11, R-SEC-03):** vai BYPASSRLS chỉ cho job đã khai; danh sách job có phiên bản; thêm job qua `pf/workflow` (bốn mắt); nhật ký mỗi lần chạy | W6.1, W6.3 | **pf 000109** | Sau (C-4 k) | M |
| **W6.5** | **`cap/evidence` + R-DB-07:** bộ ba `(subject_module, subject_type, subject_id)` trên `pf_files`, port `Resolve` của module chủ, `Open(purpose)` có nhật ký | W3.2 | **pf 000110** | Không | M |
| **W6.6** | **`cap/import`, `cap/task`, `cap/export`:** chỉ tách khi đủ quy tắc ba (R-MOD-09); trước đó khai trong `module.yaml` của module hiện tại | W6.2 | — | Không | M |

### Giai đoạn 7 — `k/access` và migration theo schema

| Gói | Nội dung | Phụ thuộc | Migration | Khách? | CS |
|---|---|---|---|---|---|
| **W7.1** | **Migration theo schema (DB-12, R-DB-12):** bộ chạy mới: bảng `schema_migrations` trong mỗi schema, thứ tự topo từ `module.yaml`, vai `own_<module>`; chuyển trạng thái hiện có (đánh dấu đã áp) không chạy lại; `migratecheck` theo thư mục module | W1.2, W2.6 | **shared 000008** | Không | L |
| **W7.2** | **`k/access` hợp nhất:** ngữ cảnh chung cho mọi app (thay `ScopeResolver` + KN context), epoch ngữ cảnh (Valkey định danh `noeviction`), quyền module × lớp (R-MOD-07), khóa API về `pf/accounts` | W0.1, W4.1 | **pf 000111–000112** | **Chặn** (SAD v1.1) | L |
| **W7.3** | **`account_pii`:** PII người dùng mã hóa mức trường + chỉ mục HMAC | W7.2 | **pf 000113** | **Chặn** (SAD v1.1) | M |
| **W7.4** | **Cụm `idp`:** DB Access riêng, `k_ref` đồng bộ xuyên cụm, thứ tự phục hồi `idp → vault → pf/mkg → k_ref` | W7.2, W2.5 | cụm mới `access` 000001 | Sau (G-1) | L |

### Giai đoạn 8 — Vùng MKG §27.2 (cần tài liệu MKG v1.3)

| Gói | Nội dung | Phụ thuộc | Migration | Khách? | CS |
|---|---|---|---|---|---|
| **W8.1** | **`lib/optcore`:** tách mô hình mạng lưới, hàm chi phí, mô hình cung, bộ giải khỏi `agrigreen_optimizer` thành gói Python dùng chung; golden test giữ nguyên byte | — | — | Không | M |
| **W8.2** | **Kho quan sát** của engine (`kind, source, as_of, max_age, uncertainty`), nạp từ bộ điều hợp feed (W5.5) | W5.5 | **mkg 000026** | Không | M |
| **W8.3** | **Engine nội bộ:** triển khai thứ hai của optimizer ở vùng mkg; chế độ mô phỏng, kế hoạch cuộn 7–14 ngày, điều phối lập lại kế hoạch; T1/T2a/T2b/T3; vùng đóng băng; tự duyệt năm điều kiện | W8.1, W8.2 | — | **Chặn** (MKG v1.3) | L |
| **W8.4** | **`mkg/workforce`** + kho định danh nhân sự trong cụm mkg | W2.6 | **mkg 000027–000028** | **Chặn** (v1.3, pháp chế) | L |
| **W8.5** | **Mở rộng `mkg/*`:** fieldops (hỏng hóc máy cuộn), orders (mua từ HTX), logistics (ghe thuê, ghép ghe), measurement (`CONSOLIDATION`, lô đi thẳng), hub (gia phả lô dùng chung) — tách 2–3 gói khi có v1.3 | W8.4 | **mkg 000029–000032** | **Chặn** (v1.3, C-5, C-6) | L |

### Giai đoạn 9 — Cổng CI còn thiếu

| Gói | Nội dung | Phụ thuộc | Migration | Khách? | CS |
|---|---|---|---|---|---|
| **W9.1** | Lint cấm so sánh tên ứng dụng trong module `kind: capability`; cổng "không `r_*` có quyền trên schema module khác trừ `k_ref`, `_pub`"; cổng `EXPLAIN` chung cho mọi bảng có RLS; thêm `mkg/fieldops`, `pf/mechanization` vào `nofloat` | W2.6, W1.2 | — | Không | S |

## 3. Thứ tự đề xuất

Thứ tự tuần tự (mỗi bước một agent):

```
W0.1 → W0.2 → W0.3 → W0.4
  → W1.1 → W1.2 → W1.3 → W1.4                       (pf 000088)
  → W2.1 (shared 000006) → W2.2 (pf 000089)
  → W2.3 (pf 000090, shared 000007) → W2.4 → W2.5 (pf 000091–92, mkg 000022)
  → W3.1 → W3.2 → W2.6 (pf 000093, mkg 000023, ts 000004, vault 000003)
  → W3.3 (pf 000094–96) → W3.4 (pf 000097)         ── mốc: trước 15/12/2026
  → W4.1 (pf 000098) → W4.2 (pf 000099)
  → W5.1 → W5.2 → W5.3 (pf 000100–01) → W5.4 (pf 000102)
  → W5.5 (mkg 000024) → W5.6 (pf 000103, mkg 000025) → W5.7 (pf 000104)
  → W5.8 → W5.9 → W5.10
  → W6.1 (pf 000105) → W6.2 (pf 000106–07) → W6.3 (pf 000108)
  → W6.4 (pf 000109) → W6.5 (pf 000110) → W6.6
  → W9.1 → W7.1 (shared 000008)
  → W8.1 → W8.2 (mkg 000026)
  [chờ SAD v1.1]  W7.2 (pf 000111–12) → W7.3 (pf 000113) → W7.4
  [chờ MKG v1.3]  W8.3 → W8.4 (mkg 000027–28) → W8.5 (mkg 000029–32)
  [chờ dữ liệu]   W3.5
```

**Đường găng tới 15/12/2026:** W0.1 → W1.1 → W2.3 → W2.5 → W3.2 → W2.6 → W3.3 → W3.4. Tám gói, có ba gói L.

Nếu thiếu thời gian, cắt theo thứ tự sau (vẫn giữ việc chuyển schema trước dữ liệu thật):
1. Cắt W3.4 (RLS theo Nút). Phạm vi vẫn kiểm ở ứng dụng.
2. Cắt W2.6 phần mkg/ts/vault. Các cụm này không nhận dữ liệu KN.

**Không cần khách, làm được ngay:** W0.1, W0.3, W0.4, W1.x, W2.1, W2.3–W2.6, W3.2–W3.4, W4.2, W5.1–W5.5, W5.7, W5.8, W5.10, W6.1, W6.3, W6.5, W6.6, W7.1, W8.1, W8.2, W9.1.

**Làm mã được, bật production chờ câu hỏi:** W0.2 (C-12, k), W2.2 (C-9), W3.1 (H-1), W4.1 (C-10), W5.3 (C-4), W5.6 (C-12), W5.9 (C-13), W6.2 (Q-01), W6.4 (C-4), W7.4 (G-1).

**Bị chặn:** W3.5 (dữ liệu UAT), W7.2–W7.3 (SAD v1.1), W8.3–W8.5 (MKG v1.3).

## 4. Các mục `[KÝ]` của v4.1 mà MKG phải ký

| # | Mục `[KÝ]` | Ở đâu trong v4.1 | Trạng thái trong repo |
|---|---|---|---|
| K-1 | **RB-1** Chỉ ghi thêm, sửa sai bằng bản ghi bù, thời hạn lưu là chuyển tầng | §3 | Đã hiện thực (ADR-0003, 0011, 0012) |
| K-2 | **RB-2** Cách ly theo đơn vị ở tầng dữ liệu | §3 | Đã hiện thực (ADR-0005, 0009) |
| K-3 | **RB-3** Dữ liệu cá nhân xóa được, tương thích RB-1 | §3 | Đã hiện thực (vault, ADR-0004) |
| K-4 | **RB-4** Giấy phép dùng được trong nền tảng thu phí, có đường thoát | §3 | Đã hiện thực (allowed-licenses). Liên quan A10/X-08 |
| K-5 | **RB-5** Mã chịu cấu hình ĐTK 2; chuyển giai đoạn không sửa mã | §3 | Phần lớn (ADR-0021, 0025); thiếu bật module theo đơn vị (W1.4) |
| K-6 | **RB-6** Đối soát chéo mọi số đo ở biên, **ngưỡng dung sai thống nhất trước** | §3 | Cơ chế có; ngưỡng chờ C-5 |
| K-7 | **RB-7** PII, sao lưu, ảnh hiện trường, nhật ký chứng cứ không lưu ngoài lãnh thổ | §3 | Đã hiện thực (ADR-0007, 0015, 0021) |
| K-8 | **Ngưỡng gom nhóm k** của van lọc dữ liệu công và `r_agg` | §4, R-DB-11, C-4 | Đang dùng tạm `publicdata.k_min = 10` (Q5-3) |
| K-9 | **Cửa sổ kiểm toán** của số đo thô trước khi chuyển tầng lạnh có nén | §21 | Chưa có tham số; `ts` chỉ ghi thêm, không chuyển tầng |
| K-10 | **Hai thời hạn báo cáo sự cố** (an toàn thông tin theo cấp độ; dữ liệu cá nhân) — `[KÝ — pháp chế]` | §35 | Chưa có trong runbook |

Kèm theo, cũng cần chữ ký:
- 20 ADR của Phụ lục B: 11 ADR mới (ADR-0027…0037, gói W0.1–W0.3), cộng 7 ADR repo đang ở trạng thái "Đề xuất" (A7);
- "Danh mục Ràng buộc Nền tảng" ở G2 (Phụ lục A).

## 5. Câu hỏi bổ sung

Đã thêm vào [`docs/cau-hoi-ton-dong.md`](cau-hoi-ton-dong.md), mục **H. Kiến trúc v4.1** (H-1 … H-16).
