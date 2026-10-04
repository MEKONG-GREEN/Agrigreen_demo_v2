# Đối chiếu mã nguồn với Thiết kế kiến trúc hệ thống v4.1

**Ngày:** 04/10/2026 · **Nhánh:** `arch41`, tách từ `origin/main` @ `c2cfccf` (G1–G5, KN v7.7, quyết định A1–A5, review M4–M7)
**Tài liệu đối chiếu:** *AgriGreen — Thiết kế kiến trúc hệ thống v4.1* (03/10/2026, 1.250 dòng). Bản này thay v3.0, là bản mà mã hiện tại được xây theo.
**Kế hoạch chuyển đổi:** [kien-truc-v4.1-ke-hoach.md](kien-truc-v4.1-ke-hoach.md) · **Câu hỏi mới:** [cau-hoi-ton-dong.md §H](cau-hoi-ton-dong.md#h-kiến-trúc-v41)

Tài liệu này chỉ đối chiếu, không đổi mã. Bằng chứng lấy bằng cách lấy mẫu (grep/glob) trong `migrations/*`, `internal/*`, `cmd/*`, `tools/*`, `contracts/*`, `docs/adr/*` và `docs/dev/*`, không đọc hết từng package. Chỗ nào chưa kiểm chắc thì ghi "cần kiểm".

**Ba tài liệu đi kèm mà v4.1 dẫn chiếu nhưng repo chưa có:**
- SAD Định danh & Phân quyền v1.1 (đặc tả `k/access`, R-ID-01…46);
- Thiết kế chi tiết vùng nội bộ MKG v1.3;
- BRD Lớp SSO và Vai trò v1.1.

Các mục phụ thuộc ba tài liệu này chỉ đánh giá được ở mức tóm tắt mà v4.1 đưa ra.

**Mã câu hỏi:** "C-x" theo Phụ lục C của **v4.1**. Mã của repo (Q5-x, H-x…) theo [cau-hoi-ton-dong.md](cau-hoi-ton-dong.md); bảng ánh xạ ở cuối mục H.

## Chú giải trạng thái

| Mã | Nghĩa |
|---|---|
| **Đ** | Đáp ứng. Có thể khác tên hoặc cách hiện thực nhưng tương đương về hành vi |
| **LN** | Lệch nhỏ. Cùng hướng; còn thiếu một phần, hoặc khác cơ chế nhưng đổi được mà không phải thiết kế lại |
| **LL** | Lệch lớn. Mô hình khác: phải thiết kế lại hoặc chuyển dữ liệu |
| **CC** | Chưa có |
| **KA** | Không áp dụng cho mã: là định hướng, mục phân tích, hoặc việc mua sắm/pháp chế |

Công sức: **S** ≤ 1 gói (một agent, ≤ 2 ngày) · **M** 1–2 gói · **L** ≥ 3 gói, hoặc có chuyển dữ liệu. Rủi ro là rủi ro **khi thay đổi**, không phải rủi ro khi giữ nguyên.

---

## 1. Tóm tắt

### 1.1 v4.1 thay đổi gì so với v3.0

| # | Thay đổi | Ảnh hưởng tới repo |
|---|---|---|
| 1 | **Bốn tầng module:** lõi `k/*` · năng lực dùng chung `cap/*` · nghiệp vụ `dom/*`, `mkg/*` · ứng dụng | Repo chia theo **vùng** (`internal/platform`, `internal/mkg`, `internal/vault`, `internal/shared`), không theo tầng. Chưa có `cap/*` |
| 2 | **Ứng dụng là tổ hợp module**, khai trong `app.yaml` | Ứng dụng hiện là `cmd/*/assembly` cộng `apps/*`, chưa có tệp khai báo |
| 3 | **Tệp `module.yaml`:** phụ thuộc có phiên bản, bật theo đơn vị, CI cưỡng chế | Chưa có. `archlint` cưỡng chế ranh giới vùng và `port/`, chưa kiểm tầng hay phiên bản |
| 4 | **`dom/cooperative` là chủ duy nhất** của HTX, nông hộ, thửa (sửa CROSS-02) | Không có dữ liệu trùng lặp. Nhưng các thực thể này chia cho bốn–năm module: `pf/party`, `pf/geo`, `pf/cropcycle`, `pf/mechanization`, `pf/accounts` |
| 5 | **Chia sẻ trong vùng nền tảng = đọc tại chỗ có cấp phép:** RLS có nhánh `share_scope`, không sao chép (sửa CROSS-03) | Cấp phép hiện là **tenant → tenant** theo danh sách loại dữ liệu, đọc qua `/v1/partner/*` (ADR-0020). RLS chỉ cách ly tenant |
| 6 | **Rà soát DB:** 14 phát hiện, DDL đã chạy thật | Repo đã tự gặp và sửa DB-01, DB-03, DB-13 từ G0 (ADR-0012, ADR-0005). DB-04, DB-11, DB-12 còn nguyên |
| 7 | **`k/access` là lõi** (SAD định danh v1.1) | Phần tương đương rải ở `pf/accounts` (gán vai, ngữ cảnh phiên KN P1), Keycloak, `pf/apiclient` (khóa API), `pf/party` (cấp phép) |
| 8 | **Vùng MKG 27.2:** kế toán mua ngoài · hệ vận hành một khối · engine ba chế độ ở vùng nội bộ, `lib/optcore` dùng chung | `mkg/*` có đủ tám module trừ `mkg/workforce`. Optimizer chỉ phục vụ `pf/simulation` ở vùng nền tảng. Chưa có kho quan sát và `lib/optcore` |
| 9 | **Phần VI mới:** MKG đi qua **cổng tích hợp** như mọi đối tác (R-INT-01); feed theo cursor, nạp theo lô, webhook mỏng | Chưa có cổng. MKG đọc `/v1/partner/*` trên Application API bằng OIDC, ghi qua van RabbitMQ |
| 10 | **Bốn cửa:** thêm **luồng song phương** | Chưa có. Bảng kê số cuộn theo thửa đang đi qua van công bố vào tenant chương trình |

### 1.2 Mức tuân thủ tổng

Đếm trên **257 dòng** của bảng truy vết ở mục 3. Bảng gồm:
- 7 ràng buộc RB;
- **59 quy tắc R-\***: R-ARC 1, R-MOD 10, R-CODE 7, R-API 7, R-EVT 5, R-DB 12, R-SEC 4, R-OPS 2, R-INT 11 (v4.1 không có R-DB-04, R-OPS-02, R-OPS-03);
- **mọi mục 1–44**, mục lớn tách thành nhiều dòng theo ý;
- 14 phát hiện DB;
- 5 cổng của Phụ lục A;
- 20 ADR của Phụ lục B;
- 22 dòng của Phụ lục D.

| Trạng thái | Số dòng | Tỷ lệ |
|---|---:|---:|
| **Đ** Đáp ứng | 77 | 30% |
| **LN** Lệch nhỏ | 109 | 42% |
| **LL** Lệch lớn | 18 | 7% |
| **CC** Chưa có | 48 | 19% |
| **KA** Không áp dụng | 5 | 2% |

**Đọc nhanh:**
- **Đáp ứng tốt:** phần "an toàn dữ liệu" mà v3.0 và v4.1 giống nhau:
  - chỉ ghi thêm ba lớp, chuỗi băm, neo hằng ngày;
  - RLS tenant, mẫu sổ đã sửa DB-01/DB-03/DB-13;
  - outbox/inbox, idempotency;
  - kho định danh, mã hóa theo hộ, phân vùng mạng, Public API tắt ở ĐTK 1.
- **Lệch lớn và chưa có:** dồn vào ba mảng mới của v4.1:
  - **khung module** (manifest, tầng, bật theo đơn vị);
  - **mô hình truy cập DB** (vai đăng nhập theo đơn vị triển khai, `k_ref`, RLS theo Nút và theo cấp phép);
  - **tích hợp liên hệ thống** (cổng, feed, nạp theo lô, luồng song phương).

### 1.3 Mười khoảng cách quan trọng nhất (xếp theo mức chặn G1/G2 của v4.1)

| # | Khoảng cách | Quy tắc | Trạng thái | Công sức | Vì sao xếp ở đây |
|---|---|---|---|---|---|
| 1 | **Chưa có khung module:** không `module.yaml`/`app.yaml`, không kiểm đồ thị tầng/phiên bản trong CI, không bật module theo đơn vị | R-MOD-05, R-MOD-08, §7, §8, Phụ lục A G0 | CC | M | Là điều kiện của mọi gói khác. Mọi thay đổi cấu trúc sau này đều cần CI kiểm |
| 2 | **Vai DB:** khoảng 28 vai đăng nhập `<module>_app`, mỗi vai một pool. v4.1 muốn một vai `NOINHERIT` mỗi đơn vị triển khai, cộng `SET LOCAL ROLE` trong `WithContextTx` | DB-04, R-DB-02, R-DB-03, R-CODE-07 | LL | L | Trần `max_connections` ở ĐTK 2. Nền cho `k_ref` và RLS theo phạm vi |
| 3 | **Không có `k_ref`:** RLS chỉ đọc `app.tenant_id`. Phạm vi Nút, phòng ban, xã phụ trách, mục đích chỉ kiểm ở tầng ứng dụng (`authz.Principal`, KN P1) | §17.1–17.2, R-DB-05, R-DB-08, DB-02, DB-05 | CC/LL | L | Mất lớp chặn thứ hai cho phạm vi trong tenant (Cấp Xã thấy xã khác nếu lỗi mã) |
| 4 | **Mô hình chia sẻ khác:** `share_grant` tenant→tenant theo `kinds`, đọc qua endpoint `/v1/partner/*`. v4.1 muốn cấp phép theo bảng × Nút × mục đích, đọc tại chỗ qua RLS `share_scope` | §17.3, R-DB-08, R-SEC-04, CROSS-03 | LL | L | G2 cần `share_grant` chạy thật cho App HTX/CGH ↔ KN |
| 5 | **Chủ dữ liệu HTX/hộ/thửa chia năm module.** KN P7 ghi vào `pf/party` + `pf/geo` + `pf/mechanization` trong một thao tác | §27.1, DB-07, Phụ lục A G1 | LL (cấu trúc), Đ (dữ liệu không trùng) | L | v4.1 G1: "hợp nhất trước khi nhận dữ liệu thật". Chuyển schema sau khi có dữ liệu thật đắt hơn nhiều |
| 6 | **Chưa có cổng tích hợp.** MKG đi Application API (OIDC `mkg_sync`) và van RabbitMQ — đúng là "lối riêng" mà R-INT-01 cấm | R-INT-01…11, §41, Phụ lục A G1+ | CC/LL | L | G1+ chạy song song G1. Thí điểm cho mọi đối tác |
| 7 | **Chưa có luồng song phương.** `htx_collection_statement` (thửa × số cuộn) đi qua van công bố vào tenant chương trình. Mọi người dùng tenant đó thấy, trái `mkg.md` §3.1 | §38.2, ADR B16, C-12 | CC | M | Mâu thuẫn tồn tại ngay trong repo, không chỉ với v4.1 |
| 8 | **Migration một kho mỗi cụm**, `golang-migrate` một số phiên bản, vai `ag_migrator` chung. v4.1 muốn lịch sử theo schema, thứ tự topo, vai `own_<module>` | DB-12, R-DB-12 | LL | L | Số migration phải cấp **đúng thứ tự gộp** (số nhỏ hơn số đã áp bị bỏ qua im lặng) |
| 9 | **`k/access` chưa thành module:** ngữ cảnh phiên KN nằm ở `pf/accounts`, khóa API ở `pf/apiclient`, cấp phép ở `pf/party`. PII người dùng ở Keycloak. Chưa có epoch ngữ cảnh | §25, §10, SAD R-ID-07 | LN | M (L nếu tách cụm `idp`) | Thu hồi cấp phép cần tăng epoch ngữ cảnh (§17.1). Phụ thuộc SAD v1.1 mà repo chưa có |
| 10 | **Vùng MKG 27.2:** chưa có `mkg/workforce`, engine ba chế độ ở vùng nội bộ, kho quan sát, `lib/optcore` | §27.2, ADR B19–B20 | CC | L | G3 của v4.1, nhưng repo đã có `mkg/*` từ G3–G4. Phải thêm vào chứ không làm lại. Cần tài liệu MKG v1.3 |

Ngay sau mười mục trên:
- DB-11: `shared.ledger_idem` giữ vĩnh viễn (S).
- R-OPS-04: DDL trong tài liệu kiến trúc chạy trong CI (S).
- DB-06: dùng `EXCLUDE` thay trigger, thêm bảng kế thừa có `legal_basis` (S).
- Năng lực dùng chung `cap/workflow`, `cap/forms` (M–L).
- View công bố `_pub` và vai `r_agg` (M).

---

## 2. Đánh giá chuyên đề

### 2.1 Tầng module, `module.yaml`/`app.yaml` và `archlint`

**Hiện trạng:**
- `internal/{platform,mkg,vault,shared}`:
  - **20 module nền tảng:** accounts, apiclient, content, cropcycle, extdata, extension, geo, masterdata, mechanization, metering, metrics, mrv, notify, party, reports, simulation, sync, waterway và hai module khác;
  - **7 module mkg:** acctbridge, costing, fieldops, hub, logistics, measurement, orders;
  - **vault/identity**;
  - **khoảng 28 thư viện `shared/*`**.
- Mỗi module có `module.go` + `{port,domain,app,store,handler}` (g1-conventions). Ứng dụng được ghép ở `cmd/<đơn vị>/assembly`.
- `tools/archlint` có sáu analyzer:
  - `agboundary`: chỉ import `port/` của module khác (R-02). Platform và mkg không import nhau (R-03). Vault chỉ qua `client` (R-04). Gốc lắp ráp theo vùng;
  - `nofloat`: phạm vi costing, measurement, orders, acctbridge, metering, hub, logistics, reports/form*, extension/reportcycle, money, mass;
  - `notimenow`, `piilog`, `noharddelete`;
  - `migratecheck` và `contractcheck` là hai công cụ riêng.

**So với v4.1:**

| Khía cạnh v4.1 | Repo | Đánh giá |
|---|---|---|
| Bốn tầng `k/cap/dom/app` | Vùng `platform/mkg/vault/shared` | LL về tổ chức. **Đề xuất:** không đổi thư mục. Khai `kind: kernel\|capability\|domain` trong `module.yaml`. Phụ lục D đã cho giữ tên `pf/*`, `mkg/*` |
| Ánh xạ tầng | `shared/{outbox,inbox,idempotency,ledger,opsjobs}` ≈ `k/jobs` + `k/ledger`. `pf/accounts` + `pf/party` (cây tổ chức, cấp phép) + `pf/apiclient` ≈ `k/access` + `k/orgtree`. `pf/masterdata`, `pf/notify`, `pf/content`, `pf/sync` ≈ `cap/*` cùng tên. `pf/reports` + `extension/reportcycle` ≈ `cap/forms` + `cap/export` + `cap/import`. `pf/metrics` ≈ `cap/analytics`. `pf/geo` = `cap/geo` **cộng** hình học thửa (thuộc `dom/cooperative`). `pf/extension`, `pf/mechanization`, `pf/waterway`, `pf/simulation` (= `dom/hubsim`), `pf/mrv`, `pf/metering` ≈ `dom/*` | Ánh xạ được, trừ `cap/workflow`, `cap/task`, `cap/evidence`, `cap/price`: chưa có module riêng |
| `module.yaml` (depends có phiên bản, provides, permissions, shares, `owns_schema`, `enable`) | Không có. Phụ thuộc chỉ thấy qua import `port/` | CC |
| `app.yaml` | `cmd/*/assembly` + `apps/{web-ops,htx-app,field-app,mkg-field-app,web-internal}` | CC |
| R-MOD-05: CI kiểm DAG đi xuống và phiên bản | `agboundary` kiểm vùng và `port/`, không kiểm tầng hay chu trình giữa module cùng vùng (Go tự chặn chu trình import) | LN → cần analyzer `agmodgraph` đọc `module.yaml` |
| R-MOD-08: bật theo đơn vị → `module_not_enabled` | Có hai cơ chế: KN v7.7 D2 "phòng ban là module" (`pf_party.department`, bật bằng trạng thái) và `AG_PUBLIC_API_ENABLED`. Chưa có bật/tắt module theo tenant | LN |
| §7.4: registry lúc biên dịch, không plugin | `module.go` + assembly, không dùng `plugin` | Đ |
| `depguard` chỉ `port/` | `agboundary` (tương đương) | Đ |

### 2.2 HTX, nông hộ, thửa — chủ duy nhất `dom/cooperative`

**Nơi hiện giữ dữ liệu** (tenant chương trình, ADR-0009):

| Thực thể v4.1 | Bảng trong repo | Migration | Module |
|---|---|---|---|
| Hồ sơ HTX | `pf_party.org` (kind = cooperative), `pf_party.cooperative`, `org_event`, `kn_profile_event` | pf 000003, 000017, 000032, 000078 | `pf/party` |
| Nông hộ, xã viên | `pf_party.household`, `pf_party.household_membership` (PII → vault qua `household_ref`) | pf 000003 | `pf/party` |
| Thửa (hình học) | `pf_geo.plot`, `plot_boundary`, `plot_event` (+ thuộc tính KN `farmer_ref`, `lot_name`…) | pf 000004, 000018, 000079, 000084 | `pf/geo` |
| Ranh giới, diện tích HTX | `pf_geo.cooperative_boundary`, `cooperative_area` | pf 000022 | `pf/geo` |
| Vụ, nhật ký canh tác (sổ) | `pf_cropcycle.season_event`, `season_current`, `coop_season_area`, farm log | pf 000020, 000021 | `pf/cropcycle` |
| Diện tích vụ cho cân đối CGH | `pf_mechanization.season_area_event`, `season_area` | pf 000007, 000035 | `pf/mechanization` |
| Máy HTX theo loại (KN v7.7) | `pf_mechanization.coop_machine_count*` | pf 000078 | `pf/mechanization` (đúng `dom/mechanization`) |
| Liên kết nông dân ↔ tài khoản | `pf_accounts.farmer_link` | pf 000025 | `pf/accounts` |
| Bảng kê, lịch gom HTX (từ van MKG) | `pf_cropcycle.collection_*` | pf 000040 | `pf/cropcycle` |

**Kết luận:**
1. **Gốc của CROSS-02 không có trong repo.** Mỗi thực thể có đúng một bảng, một tenant chủ. App KN, App HTX, CGH đọc cùng bản ghi qua `port/`, không có ba bảng HTX như ba ứng dụng UAT. Dữ liệu lệch ở UAT là lệch giữa các **prototype**. Việc "hợp nhất dữ liệu ba hệ UAT" (Phụ lục A G1) vẫn cần một công cụ nạp và đối soát, nhưng là việc **dữ liệu**, không phải việc mã.
2. **Về cấu trúc thì lệch lớn.** v4.1 muốn một module, một schema `dom_coop`, một bộ `shares`. Repo chia HTX/hộ cho `pf/party`, thửa cho `pf/geo`, vụ và nhật ký cho `pf/cropcycle`. Hệ quả:
   - KN P7 "sửa hồ sơ HTX + máy + thửa" ghi qua ba module;
   - `pf/party` vừa là `k/orgtree` (cây tổ chức, phòng ban, khu vực) vừa là `dom/cooperative` (HTX, hộ);
   - `pf/geo` vừa là `cap/geo` vừa giữ hình học thửa, trái §26: "hình học thực thể thuộc module chủ".
3. **Đề xuất:**
   - tạo `pf/cooperative` (tên repo theo Phụ lục D, `kind: domain`, schema `pf_coop`);
   - chuyển `cooperative`, `household`, `household_membership`, `plot*`, `cooperative_boundary/area`, `season*`, nhật ký bằng `ALTER TABLE … SET SCHEMA` (không UPDATE, không sao chép);
   - `pf/party` giữ cây tổ chức, phòng ban, khu vực, đồng ý, cấp phép (→ `k/orgtree` + một phần `k/access`);
   - `pf/geo` giữ lớp bản đồ, hub view, tile;
   - `pf/mechanization.season_area` đọc từ `pf/cooperative` qua port (đã là luồng đồng bộ, pf 000035).

   Làm **trước khi KN nhận dữ liệu thật** (15/12/2026). Xem gói W3.x của kế hoạch.

### 2.3 Mô hình chia sẻ

| Điểm | Repo hiện tại | v4.1 | Đánh giá |
|---|---|---|---|
| Đơn vị cấp phép | Tenant → tenant (`grantee_tenant_id`), CHECK khác tenant (pf 000042) | Đơn vị chủ → đơn vị xem, **mục đích**, **bảng được khai** (`shares`), **Nút phạm vi**, hiệu lực, **căn cứ thỏa thuận** | LN về dữ liệu (thiếu Nút, bảng, căn cứ). LL về cơ chế đọc |
| Phạm vi | `kinds` là danh sách có tên: `cooperative_directory`, `plot_directory`, `season_harvest_dates`, `htx_collection_responses`, `waterway_routes`, `valve_*`, `dp_*` | `module_table` × `node_id` (mở rộng sẵn trong `k_ref.share_scope`) | LL |
| Cách đọc | Endpoint riêng `/v1/partner/{cooperatives,plots,season-harvest-dates,htx-collection/responses}` trên Application API. Handler kiểm `GrantLookup`, ghi `partner_read_log` từng lời gọi | **Đọc tại chỗ**: cùng endpoint của module, RLS có nhánh `EXISTS share_scope`, port trả cờ `via_share_grant`, middleware ghi một sự kiện gộp mỗi yêu cầu | LL |
| Duyệt | `platform_admin` của bên cấp đề xuất, admin khác duyệt (CHECK R-56) | Quản trị viên **đơn vị chủ** tạo. Xuất hiện trong rà soát quyền định kỳ | Đ (bốn mắt còn chặt hơn), LN (chưa có rà soát định kỳ) |
| Thu hồi | Trạng thái `revoked` có hiệu lực ngay (đọc bảng chiếu) | Trễ ≤ 5 s qua `k_ref` + tăng epoch ngữ cảnh | Repo hiện **chặt hơn** vì không có bản chiếu. Sau khi có `k_ref` phải đo `k_ref_lag_seconds` |
| Đối tượng | Hiện **chỉ** dùng cho MKG (tenant riêng) và bên thứ ba (`dp_*`) | Hai việc khác nhau: (a) chia sẻ **trong vùng nền tảng** giữa đơn vị → đọc tại chỗ; (b) MKG và đối tác → **cổng tích hợp**, tập dữ liệu dựng sẵn, quyền ở mức tập (R-INT-05) | ADR-0020 trộn (a) với (b). Phải tách |
| KN v7.7 đa đơn vị/phòng ban | Một tenant (TTKN). Phòng ban và xã phụ trách là **ngữ cảnh phiên** (`pf_accounts.session_context`, `X-AG-Context`). `department_code` trên bản ghi KN. Lọc ở `app/` | Phạm vi trong đơn vị = RLS theo `node_closure` (+ biến ngữ cảnh `dept`, `xa_ids` SAD 6.1) | LN: hành vi đúng nhưng chỉ một lớp (ứng dụng) |

**Kết luận:**
- Repo chưa từng sao chép dữ liệu giữa đơn vị. Nghĩa là CROSS-03 dạng "sao chép" không tồn tại. SAD 6.3 chưa bao giờ được hiện thực.
- Nhưng repo cũng chưa có đọc tại chỗ: cấp phép hiện là cổng API riêng cho MKG.
- **Đề xuất:**
  - ADR mới thay một phần ADR-0020: `share_grant` v2 có Nút, bảng, mục đích, căn cứ. `k_ref.share_scope`. Nhánh RLS chỉ trên bảng khai `shares`.
  - Phần MKG/đối tác của ADR-0020 chuyển sang cổng tích hợp (mục 2.7).
  - Giữ sổ `share_grant_event` và CHECK bốn mắt (chặt hơn v4.1).

### 2.4 `k/access` so với `pf/accounts`, KN P1, Keycloak, vault

| Năng lực `k/access` (§25, SAD v1.1) | Repo | Trạng thái |
|---|---|---|
| Tài khoản, vai trò, gán quyền có lịch sử | `pf_accounts.account_event`/`account` (pf 000011). `role_assignment_event`/`_current` (pf 000060): vai × Nút × phòng ban × cấp × xã | Đ (mẫu sổ đúng; SAD 7.1 đã sửa theo DB-01 vì dùng `PRIMARY KEY (id, recorded_at)`) |
| `ResolveContext`, token ngữ cảnh | `POST /v1/me/context` → `context_id`, header `X-AG-Context`, middleware tra ở server (`internal/shared/authz/kn.go`, `pf_accounts.session_context`). Chỉ cho KN; module khác dùng `org_ref` + realm roles (`ScopeResolver`) | LN: hai cơ chế song song, chưa có epoch |
| `Authorize`, quyền do module khai, lớp quyền | Realm roles phẳng + `RoleOrgKinds` + vai KN; mỗi handler tự kiểm | LN (chưa có mã quyền × lớp do module khai — R-MOD-07) |
| Ủy quyền, hỗ trợ | Chế độ hỗ trợ Admin `support_session*`, `on_behalf_of` + lý do (pf 000061, FN-AD-04.07) | Đ |
| Phê duyệt bốn mắt | CHECK R-56 ở từng sổ (share grant, tham số, thiết bị cân) | LN (chưa tập trung về `cap/workflow`) |
| Thiết bị | `pf/sync` thiết bị + thu hồi. Thiết bị cân ở `mkg/measurement` (ADR-0019) | Đ |
| Khóa API (định danh máy) | `pf/apiclient` (pf 000081): khóa HMAC + pepper theo phiên bản, scope, gói hạn mức | LN (đúng chức năng, khác module; chưa mTLS cho đối tác lớn) |
| Cấp phép chia sẻ | `pf/party` share grant (mục 2.3) | LN/LL |
| PII người dùng hệ thống `k_access.account_pii` | Ở Keycloak (tên, email, SĐT). `pf_accounts` chỉ có `user_ref` | LN (xóa được, nhưng không mã hóa mức trường trong DB của ta) |
| PII hộ | `vault` cụm riêng (vault 000001–2), mã hóa phong bì theo hộ (ADR-0004), `access_log`. A1: Cấp Xã có `vault_registrar` mặc định | Đ |
| Chạy ở cụm `idp` | Keycloak DB riêng (`pg-iam` ở K8s, ADR-0021). Dữ liệu Access nằm ở cụm `pf` | LN (ĐTK 1 chấp nhận được nhờ `k_ref`; tách ở G4) |
| Valkey định danh `noeviction` | Một Valkey (compose) | CC (cần SAD R-ID-31) |
| Ba SPI Java (Zalo, OTP SMS, đẩy sự kiện) | Không có `keycloak-ext/` | CC (chờ kênh nông hộ ADR-0010, SMS Q-03) |

**Đề xuất:**
- Đánh dấu `pf/accounts` là hiện thực `k/access` (`kind: kernel` trong `module.yaml`).
- Gom dần khóa API và cấp phép về đây khi có SAD v1.1.
- **Giữ KN P1** (Keycloak chỉ xác thực, vai ở sổ gán vai). Đây đúng tinh thần "tách xác thực và phân quyền" (ADR B14). Bổ sung epoch ngữ cảnh khi làm `k_ref`.

### 2.5 Mười bốn phát hiện DB so với migration hiện tại

| Mã | Mức | Có tồn tại trong repo? | Bằng chứng | Việc cần làm |
|---|---|---|---|---|
| **DB-01** | Nghiêm trọng | **Không.** Đã sửa từ G0 | ADR-0012. Mọi bảng sổ `PRIMARY KEY (id, recorded_at)` (vd. `mkg_fieldops.roll_count`, mkg 000001; `pf_party.share_grant_event`, pf 000042). Duy nhất `(tenant, idem)` và `(tenant, stream, seq)` qua `shared.ledger_idem` + `shared.chain_head` (shared 000001) | Không. Chỉ ghi ADR-0012 là tương đương §15.1 |
| **DB-02** | Nghiêm trọng | **Không, theo nghĩa hẹp:** RLS chỉ dùng `app.tenant_id`, không tra cụm khác. Nhưng vì thế phạm vi Nút và cấp phép không ở DB | `shared.apply_tenant_rls`, `shared.current_tenant()` (shared 000001) | Khi thêm RLS theo Nút/cấp phép **bắt buộc** dùng `k_ref` trong cùng cụm (W2.1) |
| DB-03 | Cao | **Không.** Đã dùng `chain_head` | `shared.chain_head (ledger, tenant_id, stream_key)` + `FOR UPDATE` trong `shared.fn_chain` | Không. Khác v4.1: `chain_head` chung ở schema `shared` có cột `ledger`, không phải mỗi schema một bảng. Tương đương; ghi lệch |
| DB-04 | Cao | **Có.** Khoảng 28 vai `LOGIN` (`accounts_app`, `cropcycle_app`, `geo_app`, …, `waterway_app`, `hub_app`, …, `vault_app`) | `internal/shared/dbadmin/dbadmin.go`, `internal/shared/db` ("mở pool kết nối cho từng vai DB của module"). `api-public` mở pool cho 5–7 vai module (g5-conventions §…) | W2.2: `r_<module>` NOLOGIN + `app_<đơn vị>` LOGIN NOINHERIT + `SET LOCAL ROLE` |
| DB-05 | Cao | **Không**, vì RLS chưa đọc bảng lõi | — | Khi có `k_ref`: khai quan hệ công bố của lõi trong `module.yaml`, chỉ `SELECT` |
| DB-06 | Cao | **Phần lớn đã xử lý, chưa đủ.** `pf_masterdata.admin_unit` có `valid_from/valid_to`, `successor_codes`, trigger chặn chồng lấp + khóa tư vấn, không xóa. Thiếu: ràng buộc `EXCLUDE` khai báo, bảng kế thừa có `area_share`/`legal_basis`/`effective_on`. Đơn vị hành chính **theo tenant** (PK `(tenant_id, code, valid_from)`), v4.1 để chung ở `k_orgtree` | pf 000002 (dòng 20–83), `masterdata/domain/adminunit.go` | W2.4: thêm `EXCLUDE USING gist` (btree_gist) + `admin_unit_succession`. Câu hỏi C-9 (nguồn chính thức) |
| DB-07 | Cao | **Không**, theo nghĩa trùng bảng (mục 2.2). **Có**, theo nghĩa chủ phân tán | Mục 2.2 | W3.x `pf/cooperative` |
| DB-08 | Trung bình | **Có.** Chưa có quy ước bộ ba `(subject_module, subject_type, subject_id)` hay port `Resolve`. Khóa ngoại chỉ trong cùng schema (đạt R-DB-07 vế đầu) | FK nội schema: pf 000003, 000006, 000024, 000062, 000068. Tham chiếu chéo dùng `*_ref` tự do | W6.6 khi làm `cap/evidence` |
| DB-09 | Trung bình | **Không** ở dạng đọc thẳng: `pf/metrics`, `pf/reports` đọc module khác qua `port/` bằng vai của module chủ. **Có** ở dạng "không có hợp đồng đọc phân tích có phiên bản": chưa có schema `_pub`, chưa chạy trên bản sao | g4/g5-conventions (đọc qua `port/`) | W6.4 view `_pub` |
| DB-10 | Trung bình | **Chưa phát sinh.** Tổng hợp k (`collection_volume`) chạy trong **một** tenant bên cấp, không vượt RLS. Chưa có tổng hợp toàn nền tảng | `cropcycle/app/dataproduct.go` (KAnonRule, `publicdata.k_min`) | W6.5 `r_agg` khi có đơn vị thứ hai |
| DB-11 | Trung bình | **Có.** `shared.ledger_idem` giữ khóa idempotency của sổ **vĩnh viễn**, không có hạn. `shared.idempotency_key` của HTTP có hạn 48 giờ + job dọn | shared 000001 (dòng 45–53), shared 000002 (purge idempotency HTTP) | W2.3: hạn 30 ngày + job dọn (khóa vẫn còn ở cột `idem_key` của sổ) |
| DB-12 | Thấp | **Có.** Mỗi cụm một thư mục, một số phiên bản `golang-migrate` (`schema_migrations_<cụm>`), số cấp trước theo module, áp bằng `ag_migrator` chung | `internal/shared/dbadmin` `Migrate`/`runDir`, `tools/migratecheck` | W9.1. **Lưu ý ngay:** số nhỏ hơn số đã áp bị bỏ qua im lặng, nên cấp số theo **thứ tự gộp** |
| **DB-13** | Cao | **Không.** Đã sửa từ G0 | `shared.current_tenant()` = `nullif(current_setting('app.tenant_id', true), '')::uuid`. ADR-0005. `TestNoTenantContextSeesNothing`, `TestContextDoesNotOutliveTransaction` | Khi thêm biến ngữ cảnh mới: hàm `ctx_uuid`/`ctx_text` cùng mẫu (có thể là bí danh của `shared.current_tenant`) |
| **DB-14** | Cao | **Chưa phát sinh.** Không có `in_subtree`/`in_node_scope` trong chính sách | grep `in_node_scope\|in_subtree\|closure` = 0 kết quả | Khi làm RLS theo Nút: viết thẳng `EXISTS … node_closure`/`share_scope`. Thêm cổng `EXPLAIN` |

**Tổng:**
- 5 phát hiện **còn tồn tại**: DB-04, DB-08, DB-11, DB-12, và DB-06 (một phần);
- 4 phát hiện **đã sửa trước**: DB-01, DB-03, DB-13, và DB-07 (về dữ liệu);
- 5 phát hiện **chưa phát sinh** vì repo chưa có tính năng tương ứng: DB-02, DB-05, DB-09 (một phần), DB-10, DB-14. Chúng trở thành ràng buộc thiết kế cho W2.1/W4.2.

### 2.6 Vùng MKG §27.2 so với `mkg/*` và optimizer

| v4.1 | Repo | Trạng thái |
|---|---|---|
| A. Kế toán pháp định **mua ngoài**, nhận bút toán tổng hợp theo kỳ | `mkg/acctbridge` (mkg 000019), `docs/dev/acctbridge-adapter.md`. Bộ điều hợp chờ chọn phần mềm (cau-hoi §D) | Đ |
| B. Hệ vận hành **một khối, một nguồn sự thật cho lô** | `cmd/api-internal` + `cmd/worker-internal` + `ingest`, cụm `ag_mkg` | Đ |
| `mkg/fieldops` mở rộng: đội, máy cuộn, hỏng hóc | mkg 000001, 000006, 000012 (đội) | LN (cần kiểm hỏng hóc máy cuộn) |
| **`mkg/workforce` mới:** dữ liệu cá nhân 1.500+ người, kho định danh nhân sự riêng trong cụm `mkg` | Không có. Nhân sự đội hiện chỉ có `*_ref` | CC (cần MKG v1.3, pháp chế) |
| `mkg/orders` mở rộng **chiều mua từ HTX** | mkg 000016 (đơn bán, giao nhận) | LN |
| `mkg/logistics`: ghe thuê, chặng ghép ghe | mkg 000015 (chuyến, chặng thủy/bộ, `waterway_routes` qua cấp phép, pf 000046) | LN |
| `mkg/measurement`: thêm điểm cân `CONSOLIDATION`, đối soát lô đi thẳng | mkg 000007–000008 (ba điểm cân, phiếu cân dẫn xuất, đối soát). ts 000001 | LN |
| `mkg/hub`: gia phả lô dùng chung cho mẻ hub và ghép ghe | mkg 000002, 000009 | LN |
| `mkg/costing`, sổ là nguồn | mkg 000017–000018, ADR-0003 | Đ |
| C. Engine **ba chế độ** (mô phỏng, kế hoạch cuộn 7–14 ngày, điều phối lập lại kế hoạch), **cả ba ở vùng nội bộ**, không giữ dữ liệu nghiệp vụ | `optimizer/` (Python 3.12, HiGHS tùy chọn) chỉ phục vụ `pf/simulation` (vùng nền tảng) qua mTLS `/internal/v1/simulations/*`. Có `select_hubs` thử nghiệm. Không có T1/T2a/T2b/T3 | CC |
| `lib/optcore` dùng chung với `dom/hubsim` | Mã nằm trong `agrigreen_optimizer` | CC (tách gói không đổi hành vi, có golden test sẵn) |
| Kho quan sát (`kind, source, as_of, max_age, uncertainty`) | Không có. `mkg/fieldops` giữ bản sao HTX/thửa/ngày thu hoạch kéo định kỳ từ `/v1/partner/*` | CC |
| Đầu vào chính sách = sổ tham số từng module | Tham số có phiên bản ở `pf/masterdata`. Phía mkg dùng tham số trong module | LN |
| OR-Tools theo quy trình giấy phép (B20) | ADR-0016 chỉ duyệt HiGHS/highspy. Không có OR-Tools | CC (đúng — chưa được phép dùng) |

### 2.7 Phần VI — cổng tích hợp, luồng song phương

| Cửa/luồng v4.1 | Repo | Đánh giá |
|---|---|---|
| R-INT-01: MKG qua **đúng cổng** của mọi đối tác | Có **ba đường riêng**: (1) MKG → nền tảng qua **van RabbitMQ** (vhost `/`, consumer `pf.geo.hub_valve`, `pf.cropcycle.htx_collection_valve`, `pf.mrv.collection_evidence`); (2) nền tảng → MKG qua **`/v1/partner/*` trên Application API**, OIDC client credentials vai `mkg_sync`; (3) bên thứ ba qua **`cmd/api-public`** (`partner.<domain>`, khóa API, hạn mức, metering) | **LL.** (3) là mầm của cổng tích hợp: đã có xác thực máy, hạn mức theo khách, đo lường, điểm kiểm soát xuất (ADR-0022…0024). (1) và (2) là lối riêng |
| Dữ liệu được cấp phép → feed cursor + ảnh chụp | `/v1/data/*` phân trang cursor, xuất tệp qua job. Tính **theo yêu cầu**, lọc k mỗi lần gọi | LN (R-INT-05: chưa có tập dữ liệu dựng sẵn theo phiên bản; R-INT-06: chưa có "thay đổi kể từ cursor", chưa giữ 30 ngày) |
| Dữ liệu công bố (đối tác → nền tảng) → nạp theo lô 202 | Van qua broker, `valve_export` có `payload_sha256`, `contractcheck` mặc định từ chối | LN (cơ chế đúng tinh thần nhưng không qua cổng, không có mã lô, không có hàng đợi riêng từng đối tác) |
| **Luồng song phương** | Không có. `collection_schedule`, `htx_collection_statement` là **hợp đồng van** (mkg.md §3.2) ghi vào tenant chương trình. `collection_response` (HTX xác nhận) đọc ngược qua `/v1/partner/htx-collection/responses` | CC. Theo §38.2 đây đúng là dữ liệu song phương. Hiện mọi người dùng tenant chương trình có quyền module thấy được (lọc theo `org_ref` ở ứng dụng) |
| Tham chiếu dùng chung (đơn vị hành chính, đồ thị thủy) | Đồ thị thủy qua cấp phép `waterway_routes`. Đơn vị hành chính chưa xuất cho đối tác | LN |
| R-INT-04: vách ngăn theo đối tác | `api-public`: hạn mức tần suất/khối lượng, `429` + `Retry-After`, Valkey tùy chọn (ADR-0022 §7) | LN (chưa có hàng đợi, DLQ riêng từng đối tác) |
| R-INT-07, R-INT-09: webhook mỏng, ngắt mạch | Không có | CC |
| R-INT-08: hợp đồng có phiên bản, môi trường thử | `contracts/{publication,dataproduct,events}` có phiên bản, `contractcheck` kiểm tương thích ngược. Chưa có `contracts/integration/`, chưa có môi trường thử cho đối tác | LN |
| R-INT-10: đối soát tổng kiểm | Van có `payload_sha256`. Đo lường có đối soát (mkg 000008). Chưa có tổng kiểm hằng ngày theo luồng | LN |
| R-INT-11: kênh tệp cùng lược đồ | Nhập Excel cho HTX, mẫu báo cáo (`pf/reports` formimport) — lược đồ riêng | LN |
| §41.4: MKG feed → quan sát, nền tảng ngừng thì MKG không ngừng | `mkg/fieldops` giữ bản sao cục bộ (kéo định kỳ) nên đã không gọi đồng bộ trong đường găng (R-INT-02) | LN (có bản sao, chưa có `as_of`/`max_age`) |

---

## 3. Bảng truy vết đầy đủ

Cột: **Yêu cầu v4.1** · **Ở đâu trong repo** · **TT** (trạng thái) · **Cần thay đổi** · **CS** (công sức) · **RR** (rủi ro khi đổi). "—" = không cần.

### 3.1 Ràng buộc cứng `[KÝ]` và R-ARC (§2–§3)

| Yêu cầu | Ở đâu | TT | Cần thay đổi | CS | RR |
|---|---|---|---|---|---|
| RB-1 Chỉ ghi thêm, ba lớp | `shared.fn_immutable`, `apply_ledger`, `v_missing_immutability` (shared 000001, 000003). `migratecheck` cấm UPDATE/DELETE/TRUNCATE. `agnoharddelete`. ADR-0003, ADR-0011 | Đ | — | — | — |
| RB-2 Cách ly đơn vị, `tenant_id` mọi bảng, RLS | `apply_tenant_rls`, `v_missing_rls`. `TestTenantLeakParallel` qua PgBouncer. ADR-0005 | Đ | Mở rộng sang phạm vi Nút (§17.2) | M | TB |
| RB-3 PII xóa được | `vault` cụm riêng, ADR-0004 (khóa từng hộ), `vault.access_log` | Đ | — | — | — |
| RB-4 Giấy phép | `deploy/policies/allowed-licenses.txt`, ADR-0016, ADR-0017 (không TSL), ADR-0021 (không Barman GPL) | Đ | Rà Grafana/Loki/Tempo (A10) | S | TB |
| RB-5 Mã chịu cấu hình ĐTK 2 | `cmd/internal/clusterenv` (`AG_CLUSTERS`, URL theo cụm), ADR-0021 §6, ADR-0025 (lớp giá trị Phase K) | LN | Bật module theo đơn vị; cấu hình build bản lite (§7.4) | M | Thấp |
| RB-6 Đối soát chéo số đo ở biên | `mkg/measurement` đối soát (mkg 000008). HTX xác nhận số cuộn: `pf_cropcycle.collection_response` (pf 000040) | Đ | Chuyển xác nhận sang luồng song phương (§38.2) | M | TB |
| RB-7 Không lưu ngoài lãnh thổ | ADR-0007, ADR-0015, ADR-0021 (kho bí mật, CA trong lãnh thổ). A4 (liên kết tải trong lãnh thổ) | Đ | — | — | — |
| R-ARC-01 Chuyển giai đoạn = đổi cấu hình | Như RB-5. ADR-0025 | LN | Như RB-5 | M | Thấp |

### 3.2 Phần I — Quyết định (§1–§6)

| Yêu cầu | Ở đâu | TT | Cần thay đổi | CS | RR |
|---|---|---|---|---|---|
| §1 Phạm vi: bảy ứng dụng, lõi, năng lực, biên | `apps/{web-ops,htx-app,field-app,mkg-field-app,web-internal}`, `cmd/edge-agent`, `cmd/ingest`. Kênh nông hộ chỉ có ADR-0010 | LN | Tệp `app.yaml` cho 7 ứng dụng. Kênh NH (G3) | S | Thấp |
| §2 Hai điểm thiết kế, Phase K là cấp phát | `docs/roadmap.md`, ADR-0025 | Đ | — | — | — |
| §3 Bảy ràng buộc | Mục 3.1 | Đ | — | — | — |
| §4 Bốn vùng, cụm, đơn vị triển khai | Cụm `pf`, `mkg`, `ts` (= `mkg-ts`), `vault`. Keycloak `pg-iam`. Đơn vị: `api-platform`, `worker`, `api-internal`, `worker-internal`, `ingest`, `optimizer`, `identity-vault`, `edge-agent`, `api-public`. Chưa có `access`, `sync` riêng, `integration-gateway` | LN | Thêm `cmd/integration-gateway`. `access` ở G4 | L | TB |
| §4 Cửa 1 — Van công bố (mạng lưới) | `contracts/publication`, `valve_export`, `contractcheck`, ADR-0018 | Đ | Đưa qua cổng (nạp theo lô) ở G2 | M | TB |
| §4 Cửa 2 — Tiêu thụ không đặc quyền qua **cổng tích hợp** | `/v1/partner/*` trên Application API (ADR-0020), OIDC `mkg_sync` | LL | Chuyển sang feed của cổng (W5.6) | M | TB |
| §4 Cửa 3 — Van lọc dữ liệu công, k ≥ `[KÝ]` | Sản phẩm dữ liệu `collection_volume` với `publicdata.k_min` = 10 tạm (Q5-3) | LN | Tập dữ liệu dựng sẵn qua `r_agg` (R-DB-11). Chốt k (C-4) | M | Thấp |
| §4 Cửa 4 — Luồng song phương | Không có | CC | ADR B16 + W5.5 | L | Cao (C-12) |
| §4 Trong vùng nền tảng: đọc tại chỗ có cấp phép | Không có (mục 2.3) | LL | W4.1–W4.2 | L | Cao |
| §5 Bốn tầng module | Vùng `platform/mkg/vault/shared` | LL | Khai `kind` trong `module.yaml`; giữ thư mục (ADR mới) | M | Thấp |
| §5.1 Ma trận module × ứng dụng, quy tắc ba | Chưa có `cap/*`. Năng lực nằm trong module nghiệp vụ (`reports`, `extension`) | LL | Tách dần `cap/workflow`, `cap/forms`… (P6) | L | Cao (KN đang chạy) |
| §6 Stack: Go, Python, Go biên | Go 1.23+, `optimizer/` Python 3.12, `cmd/edge-agent` Go | Đ | — | — | — |
| §6 Java SPI Keycloak | Không có `keycloak-ext/` | CC | Khi có Zalo/SMS OTP | M | TB |
| §6 PostgreSQL 17 + PostGIS, pgRouting, pgcrypto, btree_gist | `agrigreen/postgres:dev` (postgis 17-3.5 + pgRouting). btree_gist chưa dùng | LN | Thêm extension khi làm `EXCLUDE` (W2.4) | S | Thấp |
| §6 TimescaleDB chỉ ở `mkg-ts` | Cụm `ts`, **PostgreSQL thuần phân vùng tháng**, cấm TSL (ADR-0017) | LN | Ghi lệch (mục 4, X-05) | S | Thấp |
| §6 PgBouncer **một pool mỗi đơn vị triển khai** | Pool theo vai module | LL | W2.2 | L | Cao |
| §6 RabbitMQ quorum, DLQ, confirm | ADR-0002 | Đ | — | — | — |
| §6 Valkey hai instance (nghiệp vụ, định danh `noeviction`) | Một Valkey | LN | Thêm instance định danh khi có Access/epoch | S | Thấp |
| §6 Keycloak 26.x + Access | Keycloak 26.3 (A3). Access = `pf/accounts` | LN | Mục 2.4 | M | TB |
| §6 S3 trong lãnh thổ | SeaweedFS (compose), ADR-0007 | Đ | — | — | — |
| §6 Đồng bộ ngoại tuyến tự vận hành | Engine = Application API, cursor HMAC (ADR-0006) | Đ | Ghi C-1 không còn chặn (X-06) | — | — |
| §6 OSRM | Cần kiểm (`pf/simulation` có `Router`) | LN | Cần kiểm | S | Thấp |
| §6 Next.js mỗi ứng dụng một BFF; Flutter app hiện trường | `apps/web-ops` (Next.js), `packages/agrigreen_mobile` (Flutter) | LN | BFF theo ứng dụng: cần kiểm | S | Thấp |
| §6 Compose → K8s từ G4 | ADR-0008, ADR-0021, `deploy/k8s` | Đ | — | — | — |
| §6 `lib/optcore` | Không có | CC | W7.1 | M | Thấp |
| §6 Bộ giải HiGHS; OR-Tools chờ ADR | ADR-0016 (highspy MIT) | Đ | ADR OR-Tools khi cần (B20) | S | Thấp |
| §6 `cmd/integration-gateway` | Không có (gần nhất: `cmd/api-public`) | CC | W5.2 | M | TB |
| §6 Quan sát OTel, Prometheus, Loki, Tempo, Grafana | OTel collector, Prometheus, sql-exporter. Loki/Tempo/Grafana chưa dùng vì AGPL (A10) | LN | Quyết A10 (X-08) | S | Thấp |

### 3.3 Phần II — Hợp đồng module và quy ước (§7–§11)

| Yêu cầu | Ở đâu | TT | Cần thay đổi | CS | RR |
|---|---|---|---|---|---|
| §7.1 `module.yaml` | Không có | CC | W1.1 | M | Thấp |
| §7.2 `app.yaml` | Không có | CC | W1.3 | S | Thấp |
| §7.4 Registry lúc biên dịch, không plugin | `module.go`, `cmd/*/assembly` | Đ | Đọc tập module từ `app.yaml` | S | Thấp |
| §7.4 Bản lite = cấu hình build | Không có | CC | Sau W1.3 (G6) | M | Thấp |
| §7.4 depguard chỉ `port/` | `archlint agboundary` | Đ | — | — | — |
| R-MOD-01 Một schema, một vai mỗi module | Schema `pf_<module>`, `mkg_<module>`, một vai `<module>_app`. Ngoại lệ: schema `shared` (outbox, inbox, idempotency, chain_head) | Đ | Vai thành NOLOGIN `r_<module>` (W2.2) | — | — |
| R-MOD-02 Gọi qua port hoặc sự kiện; khác vùng qua API | `agboundary`, ADR-0018/0020 | Đ | — | — | — |
| R-MOD-03 Không sửa model của module khác; điểm mở rộng đã khai | `port/` + DTO. Chưa có điểm mở rộng khai báo | LN | Khai `extension_points` trong `module.yaml` | S | Thấp |
| R-MOD-04 `cap/*` không biết tên ứng dụng | Chưa có `cap/*`. Logic riêng KN nằm ở `shared/authz/kn.go` | LN | Lint cấm so sánh tên app khi có `cap/*`. Đưa `kn.go` về `pf/accounts` | S | Thấp |
| R-MOD-05 DAG đi xuống, CI đọc `module.yaml` | Không có | CC | W1.2 `agmodgraph` | M | Thấp |
| R-MOD-06 Port có semver; kiểm thử hợp đồng của bên tiêu thụ | Port không có phiên bản. Hợp đồng sự kiện/van có kiểm tương thích ngược | LN | Phiên bản port trong `module.yaml`; `contract_test/` | M | Thấp |
| R-MOD-07 Quyền do module khai (mã + lớp), vai do app gom | Realm roles + `RoleOrgKinds` + vai KN v7.7 | LN | `permissions` trong `module.yaml`; seed vai theo app | M | TB |
| R-MOD-08 Bật/tắt theo đơn vị, `module_not_enabled` | Phòng ban KN (D2), `AG_PUBLIC_API_ENABLED` | LN | W1.4 | M | Thấp |
| R-MOD-09 Quy tắc ba | Định hướng | KA | — | — | — |
| R-MOD-10 Mã chung, dữ liệu riêng mỗi vùng | `shared/*` dùng ở cả hai vùng. `idempotency`/`outbox` mỗi cụm. Chưa có `cap/*` hai thể hiện | LN | Khi `cap/*` chạy ở mkg: schema riêng ở cụm mkg | M | Thấp |
| §8 Cấu trúc repo | `internal/{platform,mkg,vault,shared}`, `apps/`, `packages/`, `optimizer/`, `contracts/`, `deploy/{compose,k8s,iac,policies}`. Không có `app/`, `keycloak-ext/`, `migrations/` trong module | LN | Ghi lệch: giữ thư mục, thêm `module.yaml` + `app/<APP>/app.yaml` | S | Thấp |
| §9 Quy ước mã (R-CODE-01 … 07) | `tools/archlint` (nofloat, notimenow, piilog), `shared/{money,mass,ids,clock,problem}` | LN | Theo từng dòng dưới | — | — |
| R-CODE-01 decimal, cấm float | `nofloat` (costing, measurement, orders, acctbridge, metering, mkg/hub, mkg/logistics, reports/*, money, mass) | LN | Thêm `mkg/fieldops`, `pf/mechanization` vào phạm vi | S | Thấp |
| R-CODE-02 UUIDv7 sinh ở nơi tạo | `shared/ids`; thiết bị sinh (sync-contract) | Đ | — | — | — |
| R-CODE-03 `timestamptz` UTC; cấm `time.Now()` trong `domain/` | `shared/clock`, `notimenow` | Đ | — | — | — |
| R-CODE-04 `event_time` + `recorded_at` | Mọi bảng sổ | Đ | — | — | — |
| R-CODE-05 Không PII trong log | `piilog` (tag `pii`) | Đ | — | — | — |
| R-CODE-06 RFC 9457 | `shared/problem`, `docs/errors.md` | Đ | — | — | — |
| R-CODE-07 Mọi truy cập DB qua `WithContextTx`; ngoài giao dịch thì thất bại | `tenant.WithTenantTx`. Ngoài giao dịch: RLS trả rỗng (không lỗi quyền). Vai đăng nhập **có** quyền bảng | LL | W2.2 | L | Cao |
| §10 Bề mặt Application (OIDC + token ngữ cảnh) | OIDC + `X-AG-Context` (KN) | LN | Ngữ cảnh chung cho mọi app (W8.1) | M | TB |
| §10 Admin (LoA 2) | Admin API MFA (ADR-0020, accounts) | Đ | — | — | — |
| §10 Public (khóa API do `k/access`), tắt ở ĐTK 1, ghi `dom/metering` | `cmd/api-public`, `pf/apiclient`, `pf/metering`, mặc định tắt (ADR-0022/0023/0025) | LN | Gộp vào cổng tích hợp; khóa API thuộc `k/access` | M | TB |
| §10 Internal mTLS; Vận hành VPN + MFA | optimizer ↔ api mTLS, ingest mTLS, ADR-0008/0021 | Đ | — | — | — |
| R-API-01 `/v1`, `/v2` song song | Mọi route | Đ | — | — | — |
| R-API-02 Idempotency-Key 48 giờ | `shared.idempotency_key` (`expires_at` 48 giờ), `shared/idempotency` | Đ | — | — | — |
| R-API-03 > 2 s → 202 + `/v1/jobs/{id}` | Mô phỏng, xuất báo cáo, MRV, `public.yaml /v1/jobs/{id}` | Đ | — | — | — |
| R-API-04 Cursor | Phân trang cursor, `next_cursor`; sync cursor HMAC | Đ | — | — | — |
| R-API-05 Tenant từ token | ADR-0009 (R-16) | Đ | — | — | — |
| R-API-06 Backend tự xác minh token, phân quyền ở backend | `shared/authz/oidc.go` | Đ | — | — | — |
| R-API-07 Đường dẫn theo tiền tố module | Theo module repo: `/v1/reports/…`, `/v1/extension/…`, `/v1/geo/…`; riêng đối tác `/v1/partner/*` | LN | `/v1/partner/*` trái nguyên tắc này. Bỏ khi có cổng. Đổi tiền tố theo `cap/*` khi tách module (v2 song song) | M | TB |
| §11 Mẫu việc dài 10 bước | ADR-0002, `shared/outbox` (confirm rồi đánh dấu), inbox, ACK sau commit, ảnh chụp bất biến (simulation) | Đ | — | — | — |
| §11 Phong bì sự kiện (`module`, `zone`) | `envelope.v1.json`: `producer` thay cho `module`, không có `zone` | LN | Thêm `zone` (tùy chọn, v1 tương thích) | S | Thấp |
| R-EVT-01 Tên `<tầng>.<module>.<thực thể>.<động từ>`; CI kiểm tương thích | `pf.<module>.…`, `mkg.<module>.…`; CHECK regex trên `shared.outbox`; `contractcheck` | LN | Giữ tiền tố `pf`/`mkg` (Phụ lục D). Nới regex nếu thêm `k.`/`cap.` | S | Thấp |
| R-EVT-02 Handler idempotent | `shared.inbox (event_id, consumer)` | Đ | — | — | — |
| R-EVT-03 DLQ, cảnh báo, không tự xóa | `x-delivery-limit` 5, `.dlq`, `pf.sync.integration.dead_lettered` | Đ | — | — | — |
| R-EVT-04 Hàng đợi không phải sổ | ADR-0003, mkg.md §3.1(3) | Đ | — | — | — |
| R-EVT-05 Không vượt vùng qua broker trừ van | Van mkg → pf qua vhost `/`. Nội bộ mkg ở vhost `mkg.internal` (ADR-0018) | LN | Đúng chữ của R-EVT-05, nhưng trái §38.1/R-INT-01 (dữ liệu công bố nạp theo lô qua cổng) | M | TB |

### 3.4 Phần III — Dữ liệu (§12–§24)

| Yêu cầu | Ở đâu | TT | Cần thay đổi | CS | RR |
|---|---|---|---|---|---|
| §12 Rà soát DB | Mục 2.5 | LN | Theo từng DB-xx | — | — |
| §12.1 Phép thử T0–T12 | Có tương đương: T1–T3 (`TestHash…`, `migrate verify-chains`, trigger), T5 (CONNECT/GRANT theo vai), T6a/T6e-tenant (`TestTenantLeakParallel`, `TestCannotWriteForeignTenant`), T6d (`TestNoTenantContextSeesNothing`), T7 (trigger chồng lấp). Chưa có: T4, T6b/c (cấp phép), T8–T11, T12 | LN | Thêm T4, T6b/c, T9–T11 khi làm W2.2/W4.2; T12 = R-OPS-04 | M | Thấp |
| §13 Tổ chức cụm, database, schema | `ag_pf`, `ag_mkg`, `ag_ts`, `ag_vault`; schema `pf_*`, `mkg_*`, `ts_*`, `vault`, `shared`. Chưa có `k_ref`, `_pub`, `idp/access` | LN | W2.1, W6.4 | M | Thấp |
| R-DB-01 Tên schema `<tầng>_<module>`, `_pub` | `pf_<module>`, `mkg_<module>` (Phụ lục D chấp nhận). Không có `_pub` | LN | Giữ tên; thêm `_pub` | S | Thấp |
| §13 `vault` instance riêng từ ĐTK 1 | K8s `pg-vault` riêng (ADR-0021). Compose ĐTK 1 dùng chung một instance (chỉ local/dev, ADR-0008) | LN | Bảo đảm ĐTK 1 production dùng instance riêng (G-1) | S | Thấp |
| §14 Ba vai mỗi module (`own_`, `r_`, `r_…_pub`) | Một vai `<module>_app` LOGIN; `ag_migrator` sở hữu mọi schema | LL | W2.2, W9.1 | L | Cao |
| R-DB-02 Vai đăng nhập NOINHERIT; quyền chỉ sau `SET LOCAL ROLE` | Không | CC | W2.2 | L | Cao |
| R-DB-03 Vai do registry gán; lint cấm `SET ROLE` | Không áp dụng (chưa có `SET ROLE`) | CC | W2.2 + lint | S | Thấp |
| §14 Tối đa 80 kết nối máy chủ cụm `pf` | Theo vai module × đơn vị | LL | W2.2 | — | — |
| §15.1 DDL sổ: PK có `recorded_at`, phân vùng tháng | ADR-0012, `apply_ledger` | Đ | — | — | — |
| §15.2 `chain_head` | `shared.chain_head` (một bảng, cột `ledger`), `fn_chain` SECURITY DEFINER | Đ | Ghi lệch (bảng chung thay vì mỗi schema) | — | — |
| §15.3 Idempotency riêng, hạn 30 ngày | `shared.ledger_idem` **không hạn** | LN | W2.3 (DB-11) | S | Thấp |
| §15.4 Ba lớp bất biến + neo | `fn_immutable`, quyền, chuỗi + `shared.anchor_chains` hằng ngày | Đ | Neo ra kho bất biến ngoài DB: cần kiểm (`migrate anchor`) | — | — |
| R-DB-06 Sổ theo mẫu; `v_missing_immutability` rỗng | `shared.v_missing_immutability` (cổng CI) | Đ | — | — | — |
| §16 Bảng chiếu cùng giao dịch; ES mức B; diễn tập dựng lại | ADR-0003, `cmd/rebuilddrill`, `make rebuild-projection` (cần kiểm tên) | Đ | — | — | — |
| §17.1 `k/ref` bản chiếu trong mỗi cụm | Không có | CC | W2.1 | L | TB |
| R-DB-05 Quan hệ công bố của lõi | Không có | CC | Theo W2.1 | S | Thấp |
| §17.2 `ctx_uuid`/`ctx_text` chuẩn hóa chuỗi rỗng | `shared.current_tenant()` cùng mẫu | Đ | Bổ sung các biến khác (node, dept, purpose) | S | Thấp |
| §17.2 Chính sách phạm vi Nút viết thẳng `EXISTS node_closure` | Phạm vi Nút ở tầng ứng dụng (ADR-0009, KN P1) | LL | W3.4 | M | TB |
| §17.3 `share_scope`, nhánh đọc tại chỗ | Không có | CC | W4.2 | L | Cao |
| R-DB-08 FORCE RLS, một policy, chỉ đọc biến qua hàm, không hàm có truy vấn con, `shares`, `WITH CHECK` không có nhánh chia sẻ | FORCE + một policy `p_tenant` + hàm `nullif` đã có. Phần chia sẻ chưa có | LN | Đổi `v_missing_rls` chấp nhận `p_scope`; cổng `EXPLAIN` | M | TB |
| §17.3 Ghi nhật ký mọi lần đọc qua chia sẻ | `pf_party.partner_read_log` (mỗi lời gọi `/v1/partner/*`) | LN | Cờ `via_share_grant` + sự kiện gộp | S | Thấp |
| §18 Tham chiếu chéo module | `*_ref` tự do; FK chỉ trong schema | LN | Quy ước bộ ba + `Resolve` (W6.6) | M | Thấp |
| R-DB-07 Không FK xuyên schema; bộ ba; `Resolve` | Vế 1 đạt (pf 000003…000068 chỉ FK nội schema). Vế 2–3 chưa có | LN | W6.6 | M | Thấp |
| §19 Đơn vị hành chính có hiệu lực | `pf_masterdata.admin_unit` + trigger chồng lấp + `successor_codes` | LN | W2.4 | S | Thấp |
| R-DB-09 Danh mục pháp lý có hiệu lực; trỏ phiên bản tại `event_time` | `admin_unit`, `param_version`, định mức cơ giới hóa có hiệu lực (pf 000007). Báo cáo kỳ cũ theo đơn vị tại thời điểm kỳ: cần kiểm | LN | Kiểm `pf/reports` nhóm theo đơn vị tại kỳ | S | TB |
| §20 Dữ liệu cá nhân: sổ chỉ `*_ref`; vault; ảnh mã hóa theo hộ; PII người dùng `k_access.account_pii` | `vault`, ADR-0004, `piilog`. PII người dùng ở Keycloak | LN | `account_pii` khi có SAD (W8.2) | M | TB |
| §20 Tọa độ thửa là PII gián tiếp, không vào view công bố mức thửa | Sản phẩm dữ liệu không trả thửa (`dataproduct.go`). `/v1/partner/plots` trả hình học thửa cho MKG theo cấp phép | LN | Ghi rõ trong ADR cổng: hình học thửa chỉ qua cấp phép có mục đích | S | TB |
| §21 Chuỗi thời gian chỉ `mkg-ts`, không join, không xóa, lạnh có nén | Cụm `ts` (ADR-0017), `migratecheck` cấm tham chiếu `pf_*`/`mkg_*`, chỉ ghi thêm. Không nén TSL | LN | Ghi lệch (X-05); cửa sổ kiểm toán `[KÝ]` | S | Thấp |
| §22.1 / R-DB-10 Phân tích chỉ đọc `_pub`, trên bản sao | `pf/metrics`, `pf/reports` đọc qua `port/` trên primary | LN | W6.4 | M | Thấp |
| §22.2 / R-DB-11 `r_agg` BYPASSRLS có kiểm soát, ngưỡng k, nhật ký chạy | Không có vai BYPASSRLS cho ứng dụng (chỉ `ag_outbox_publisher`). Ngưỡng k trong một tenant | LN | W6.5 | M | TB |
| §23 / R-DB-12 Lịch sử migration theo schema, thứ tự topo, vai `own_<module>` | `golang-migrate` một bảng mỗi cụm, `ag_migrator` | LL | W9.1 | L | TB |
| §24 Kiểu dữ liệu chuẩn | `numeric(14,3)` (130 cột), `numeric(18,2)`, `numeric(18,4)`. Diện tích dùng `numeric(14,4)` thay `(12,4)`. Thửa tối thiểu **3 điểm** (A2) thay vì 4 | LN | Ghi lệch A2 (X-12) | S | Thấp |
| R-DB-13 Bảng vận hành có hạn lưu | outbox (cần kiểm hạn), inbox, `idempotency_key` 48 giờ, `chain_head`, `session_context` | LN | Khai hạn lưu cho outbox/inbox; `ledger_idem` (W2.3) | S | Thấp |

### 3.5 Mười bốn phát hiện DB — bảng gọn

| Mã | TT | CS | RR |
|---|---|---|---|
| DB-01 | Đ | — | — |
| DB-02 | KA (chưa phát sinh) → ràng buộc của W2.1 | — | — |
| DB-03 | Đ | — | — |
| DB-04 | LL | L | Cao |
| DB-05 | CC (theo W2.1) | S | Thấp |
| DB-06 | LN | S | Thấp |
| DB-07 | LL (cấu trúc) | L | Cao |
| DB-08 | CC | M | Thấp |
| DB-09 | LN | M | Thấp |
| DB-10 | CC | M | TB |
| DB-11 | LN | S | Thấp |
| DB-12 | LL | L | TB |
| DB-13 | Đ | — | — |
| DB-14 | KA (chưa phát sinh) → cổng `EXPLAIN` | S | Thấp |

Chi tiết và bằng chứng ở mục 2.5.

### 3.6 Phần IV — Đặc tả module (§25–§28)

| Yêu cầu | Ở đâu | TT | Cần thay đổi | CS | RR |
|---|---|---|---|---|---|
| §25 `k/access` | `pf/accounts` + Keycloak + `pf/apiclient` + `pf/party` share grant | LN | Mục 2.4, W8.x | L | TB |
| §25 `k/orgtree` (cây Nút, đơn vị hành chính, `AdminUnitAt`) | `pf_party.org` (+ phòng ban, khu vực pf 000062), `pf_masterdata.admin_unit` | LN | Khai `kind: kernel`; xuất `node_closure` cho `k_ref` | M | TB |
| §25 `k/ref` | Không có | CC | W2.1 | L | TB |
| §25 `k/ledger` | `shared` (fn_immutable, fn_chain, canonical, verify_chain, anchor) | Đ | — | — | — |
| §25 `k/jobs` | `shared/{outbox,inbox,idempotency,opsjobs,leader,schedule}` | Đ | — | — | — |
| §25 `k/audit` (sổ nhật ký an ninh, đọc nhật ký cũng ghi) | Rải rác: `vault.access_log`, `partner_read_log`, `account_event`, `support_action_log`, `dispatch_link_access`. Chưa có đẩy SIEM | LN | Module audit gom + đẩy (G4) | M | Thấp |
| §25 `k/ui-shell` (menu từ quyền qua `module.yaml`) | `apps/web-ops` menu theo phòng ban/cấu hình (KN P2/P6) | LN | Menu đọc từ `module.yaml` | M | Thấp |
| §25 Thay SAD 6.3: `share_grant` thuộc `k/access`, đơn vị chủ tạo | `pf/party` | LN | W4.1 | M | TB |
| §26 `cap/masterdata` | `pf/masterdata` (danh mục, tham số có phiên bản, duyệt) | Đ | Duyệt qua `cap/workflow` khi có | — | — |
| §26 `cap/workflow` | Không có module chung. Duyệt bốn mắt rải từng module | CC | W6.1 | M | TB |
| §26 `cap/forms` (chốt kỳ = ảnh chụp đóng băng) | `pf/reports` (mẫu 000065, nhập 000066, gửi 000072) + `extension/reportcycle` (đợt, nộp, kỳ, tổng hợp 000068–000071) | LN | W6.2 | L | Cao |
| §26 `cap/export` | `pf/reports` xuất (ADR-0014), ADR-0026 Google Sheets, băm tệp: cần kiểm | LN | Gom về `cap/export` | M | TB |
| §26 `cap/analytics` | `pf/metrics` (000036–37) | LN | Chỉ đọc `_pub` (W6.4) | M | Thấp |
| §26 `cap/import` | `pf/reports/formimport`, `cmd/formimport`, nhập HTX hàng loạt | LN | Gom (P6) | M | TB |
| §26 `cap/task` | `pf/extension` nhiệm vụ + SLA + leo thang | LN | Tách khi có app thứ hai | M | Thấp |
| §26 `cap/notify` | `pf/notify` (trong app, email, đẩy) | LN | SMS/Zalo khi có kênh | M | Thấp |
| §26 `cap/evidence` | `pf/files` (000010), ADR-0004 | LN | Bộ ba subject + `Open(purpose)` (W6.6) | M | TB |
| §26 `cap/content` | `pf/content` | Đ | — | — | — |
| §26 `cap/price` | Không có | CC | Chờ yêu cầu (NH, KN) | M | Thấp |
| §26 `cap/geo` | `pf/geo` (+ hình học thửa) | LN | Chuyển hình học thửa sang `pf/cooperative` | M | TB |
| §26 `cap/sync` | `pf/sync` (ADR-0006) | Đ | — | — | — |
| §27.1 `dom/cooperative` | Mục 2.2 | LL | W3.1–W3.5 | L | Cao |
| §27.1 `dom/extension` | `pf/extension` (+ lịch công tác 000075, vật tư 000077) | LN | Tách phần `cap/forms`/`cap/task` | M | TB |
| §27.1 `dom/mechanization` (`shares: machine`, định mức dẫn văn bản) | `pf/mechanization` | Đ | Khai `shares` | — | — |
| §27.1 `dom/waterway` | `pf/waterway` | Đ | — | — | — |
| §27.1 `dom/hubsim` (ảnh chụp gồm kết quả định tuyến, `algorithm_version`) | `pf/simulation` + `optimizer/` | Đ | — | — | — |
| §27.1 `dom/mrv` (lưu package kèm băm; danh mục chờ C-2) | `pf/mrv` (000008, 000038, 000045) | Đ | — | — | — |
| §27.1 `dom/metering` | `pf/metering` (ADR-0023) | Đ | — | — | — |
| §27.2 A — Kế toán mua ngoài | `mkg/acctbridge` | Đ | — | — | — |
| §27.2 B — Hệ vận hành một khối | `mkg/*`, `cmd/api-internal` | Đ | — | — | — |
| §27.2 `mkg/workforce` | Không có | CC | W7.4 | L | Cao (PII 1.500 người) |
| §27.2 Mở rộng fieldops/orders/logistics/measurement/hub | Mục 2.6 | LN | W7.5 | L | TB |
| §27.2 C — Engine ba chế độ ở vùng nội bộ | `optimizer/` cho `pf/simulation` | CC | W7.2 | L | TB |
| §27.2 Ba tầng T1/T2a/T2b/T3, vùng đóng băng, tự duyệt năm điều kiện | Không có | CC | W7.2 (cần MKG v1.3) | L | TB |
| §27.2 Kho quan sát + tham số chính sách | Không có | CC | W7.3 | M | Thấp |
| §27.2 Dòng rơm (45–55% đi thẳng, ghe 6–200 tấn) | Tham số mô phỏng (cần kiểm) | LN | Theo v1.3 | M | Thấp |
| §28 KN (G1) | `apps/web-ops`, `apps/field-app`, KN v7.7 | Đ | `app.yaml` | — | — |
| §28 HTX, CGH, SIM (G2) | `apps/htx-app`, `web-ops`, `pf/mechanization`, `pf/simulation` | LN | Chuyển lên module dùng chung (G2 v4.1) | L | TB |
| §28 MKG (G3) | `apps/web-internal`, `apps/mkg-field-app` | Đ | — | — | — |
| §28 NH (G3) | ADR-0010 | LN | Kênh Zalo + `cap/price` | L | TB |
| §28 PF (G1) | Admin API, `web-ops` quản trị | Đ | — | — | — |

### 3.7 Phần V — Vận hành và an toàn (§29–§36)

| Yêu cầu | Ở đâu | TT | Cần thay đổi | CS | RR |
|---|---|---|---|---|---|
| §29 Phân vùng mạng cấp độ 3 | ADR-0008, ADR-0021, `check_flows.py`, NetworkPolicy | Đ | Thêm luồng `integration-gateway`, `access` | S | Thấp |
| R-SEC-01 Internet không chạm vùng dữ liệu; namespace + default deny | ADR-0008/0021 | Đ | — | — | — |
| R-SEC-02 Bằng chứng tách biệt của nhà cung cấp đám mây | Chờ chọn nhà cung cấp (G-1) | KA | Điều kiện mua sắm | — | — |
| §30 Kiểm soát TCVN 11930 | SAST/SBOM/ký image (cần kiểm trong CI), DLP ADR-0024, nhật ký | LN | CI chưa chạy từ G2 (A8). Nhật ký 1 năm (G-6) | M | Cao |
| R-SEC-03 `r_agg` là tài sản đặc quyền, bốn mắt | Không có `r_agg` | CC | W6.5 | S | TB |
| R-SEC-04 Cấp phép do quản trị viên đơn vị chủ, có căn cứ, thời hạn, rà soát | Đơn vị chủ đề xuất + bốn mắt + hạn ≤ 5 năm. Chưa có căn cứ thỏa thuận, chưa có rà soát định kỳ | LN | W4.1 | S | Thấp |
| §31 Quan sát: trường log chuẩn, chỉ số nghiệp vụ | OTel, `ag.sync.conflict.overdue`, `ag_db_standby_replication_lag_seconds`, DLQ, `ag_egress_*`. Chưa có `k_ref_lag_seconds`, đếm đọc qua cấp phép theo đơn vị, `r_agg` | LN | Theo W2.1/W4.2/W6.5 | S | Thấp |
| §32 Cổng CI: đồ thị module | Không | CC | W1.2 | M | Thấp |
| §32 Cổng: ranh giới mã, không nhánh theo app | `agboundary` có; lint tên app chưa | LN | W10.1 | S | Thấp |
| §32 Cổng: hợp đồng port, sự kiện, view công bố | Sự kiện + van có; port, `_pub` chưa | LN | W1.1/W6.4 | M | Thấp |
| §32 Cổng: quyền DB, bất biến, chuỗi băm, rò rỉ đơn vị qua PgBouncer | `v_missing_immutability`, `v_missing_rls`, `verify_chain`, `TestTenantLeakParallel` (`AG_TEST_PGBOUNCER_URL`). Kiểm "không `r_*` trên schema khác": cần thêm | LN | W10.1 | S | Thấp |
| §32 Cổng: `EXPLAIN` chính sách | Không | CC | W10.1 | S | Thấp |
| §32 Cổng: vai DB (ngoài `WithContextTx` thất bại) | Không | CC | W2.2 | S | Thấp |
| §32 Kiểm thử định kỳ (phục hồi, failover, dựng lại, tải) | `restore-drill.yml`, `cmd/drill`, `cmd/rebuilddrill`, `tools/loadgen` | Đ | Thêm RLS có nhánh chia sẻ | S | Thấp |
| R-OPS-04 DDL trong tài liệu kiến trúc chạy trong CI | Không. Repo không có bản tài liệu kiến trúc trong `docs/` | CC | W0.4 | S | Thấp |
| §33 Môi trường; R-OPS-01 PII thật không ra khỏi production; dữ liệu tổng hợp có cây hành chính sau 2025 kèm bảng kế thừa | `cmd/seed-synthetic`, `docs/requirements/danh-muc-hanh-chinh.md`, `successor_codes` | LN | Bảng kế thừa theo W2.4 | S | Thấp |
| §34 Sao lưu, RPO/RTO, thứ tự phục hồi, `k_ref` dựng lại khớp nguồn | pgBackRest (ADR-0021), `restore-drill.yml`, vault bucket riêng. RPO/RTO repo ghi 24 h/4 h (G-2), v4.1 ghi ≤ 1 h | LN | Chốt RPO (G-2); thêm bước `k_ref` | S | TB |
| §35 Ứng cứu sự cố P1–P4, hai nghĩa vụ báo cáo `[KÝ — pháp chế]` | `docs/runbooks/*` | LN | Thêm P1 "đọc chéo không cấp phép", P2 "`k_ref` trễ" | S | Thấp |
| §36 Chế độ hỏng (phân vùng sổ tạo trước, đường nhập tay cân) | Tạo trước 24 tháng + DEFAULT (`apply_ledger`, `migrate maintain`). Nhập tay cân: cần kiểm | LN | Thêm `k_ref`, `chain_head` theo lô | S | Thấp |

### 3.8 Phần VI — Tích hợp liên hệ thống (§37–§44)

| Yêu cầu | Ở đâu | TT | Cần thay đổi | CS | RR |
|---|---|---|---|---|---|
| §37 / R-INT-01 MKG qua đúng cổng, không lối riêng | Van RabbitMQ + `/v1/partner/*` + `mkg_sync` | LL | W5.2–W5.6 | L | TB |
| §38.1 Bốn loại luồng | Có hai loại tương đương (cấp phép, công bố). Chưa có song phương và tham chiếu dùng chung | LL | W5.1 | M | TB |
| §38.2 Luồng song phương (ADR sửa ADR-0020) | Không có; bảng kê đang là van | CC | ADR B16 + W5.5 | L | Cao |
| §38.3 Sáu luồng thí điểm MKG | Đều có nhưng qua đường riêng: ngày thu hoạch (`season_harvest_dates`), đồ thị thủy (`waterway_routes`), lịch gom và số cuộn (van), xác nhận (`htx-collection/responses`). Tiến độ gặt tổng hợp theo xã: chưa có | LN | Chuyển từng luồng sang cổng | L | TB |
| §38.3 Dữ liệu VFT không qua nền tảng; cổng nhận vùng nội bộ dùng chung mã | `mkg/orders` (VFT là khách). Chưa có cổng nhận | LN | Dùng mã cổng cho cổng nhận nội bộ (G3+) | M | Thấp |
| §39 Năm rủi ro hiệu năng | Phân tích | KA | — | — | — |
| §40 Quy tắc tích hợp (R-INT-02 … 11) | Theo từng dòng dưới | LN | — | — | — |
| R-INT-02 Không gọi đồng bộ xuyên hệ thống trong đường găng | `mkg/fieldops` kéo định kỳ về bản sao. Mô phỏng nền tảng không gọi MKG | Đ | Thêm `as_of`/`max_age` (W5.6) | S | Thấp |
| R-INT-03 Bất đồng bộ mặc định, 202 + mã lô | Van bất đồng bộ; chưa có mã lô | LN | W5.4 | M | TB |
| R-INT-04 Vách ngăn theo đối tác | Hạn mức + 429 ở `api-public` | LN | Hàng đợi, DLQ riêng (W5.4) | M | TB |
| R-INT-05 Tính một lần, phát nhiều lần | Tính theo yêu cầu | LN | W5.3 | M | TB |
| R-INT-06 Feed cursor, giữ 30 ngày, ảnh chụp qua URL ký | Phân trang cursor, xuất tệp qua job | LN | W5.3 | M | TB |
| R-INT-07 Webhook mỏng HMAC | Không | CC | W5.7 | M | Thấp |
| R-INT-08 Hợp đồng có phiên bản, môi trường thử đối tác | `contracts/*` có phiên bản; chưa `integration/`, chưa có môi trường thử | LN | W5.1, W5.8 | M | Thấp |
| R-INT-09 Ngắt mạch webhook | Không | CC | W5.7 | S | Thấp |
| R-INT-10 Đối soát tổng kiểm | `payload_sha256` van, đối soát cân | LN | W5.7 | S | Thấp |
| R-INT-11 Kênh tệp cùng lược đồ | Nhập Excel riêng từng module | LN | W5.8 | M | Thấp |
| §41.1 Thành phần cổng | Không (gần nhất `api-public`) | CC | W5.2–W5.4 | L | TB |
| §41.2 Xác thực máy (khóa HMAC, mTLS cho đối tác lớn), phạm vi khóa = cấp phép, ghi metering | Khóa HMAC + pepper, scope `dp:*`, `usage_event` mỗi lời gọi. Chưa mTLS cho đối tác | LN | W5.2 | M | TB |
| §41.3 Ghi từ đối tác: lô → 202 → hàng đợi riêng → inbox → kiểm hợp đồng → port | Không | CC | W5.4 | L | TB |
| §41.4 Phía MKG: bộ điều hợp feed → kho quan sát | Không | CC | W5.6 + W7.3 | M | Thấp |
| §42 Mục tiêu dịch vụ của cổng, phép thử gấp 10 hạn mức | `tools/loadgen` (G4) | CC | W5.9 | M | Thấp |
| §43 Chế độ hỏng của cổng | Không | CC | Theo W5.x | — | — |
| §44 Mở rộng sang các bên | `pf/mrv` xuất package (Đ). Cơ quan nhà nước: không thiết kế trước (đúng) | LN | — | — | — |

### 3.9 Phụ lục A — Lộ trình theo cổng

| Cổng v4.1 | Repo | TT | Ghi chú |
|---|---|---|---|
| G0: khung module, registry, kiểm đồ thị; `k/ledger` sửa DB-01/03; `k/access` tối thiểu; `k/ref`; vai theo đơn vị triển khai; R-OPS-04 | `k/ledger` có. Còn lại chưa có | LL | Gói P0–P2 |
| G1: KN chạy thật trên `cap/import`, `cap/workflow`, `cap/forms`, `dom/cooperative` làm chủ duy nhất | KN v7.7 chạy trên `pf/reports`, `pf/extension`, `pf/party`, `pf/geo` | LN | Chức năng đủ; cấu trúc module chưa đổi |
| G1+: cổng tích hợp tối thiểu; ADR song phương, cấp phép tổng hợp theo xã | Không có | CC | P5 |
| G2: HTX/CGH/SIM lên module dùng chung, `share_grant` chạy thật | Ba app đã chạy trên mã chung (repo G2) nhưng không có `share_grant` nội nền tảng | LN | P4 |
| G3–G6 | Repo đã làm `mkg/*` (G3–G4), K8s (G4), Phase K + Public API (G5). Mốc v4.1 muộn hơn | LN | Đổi thứ tự: repo đi trước ở G3–G5, đi sau ở G0–G1 của v4.1 |

### 3.10 Phụ lục B — 20 ADR phải có trước G1

| # | ADR v4.1 | ADR repo | TT | Việc |
|---|---|---|---|---|
| 1 | Bốn tầng, tệp khai báo, ứng dụng là tổ hợp | ADR-0011 (khác nội dung) | CC | ADR-0027 |
| 2 | Module gọi nhau trong tiến trình, không microservice | Ngầm trong ADR-0001, g1-conventions | LN | Ghép vào ADR-0027 |
| 3 | Go; ngoại lệ Python, Go biên, Java SPI | ADR-0001, ADR-0016 | Đ | — |
| 4 | RabbitMQ | ADR-0002 | Đ | — |
| 5 | Event sourcing mức B | ADR-0003 | Đ | — |
| 6 | Mẫu bảng sổ | ADR-0012 | Đ | Cập nhật hạn `ledger_idem` (DB-11) |
| 7 | Một vai đăng nhập mỗi đơn vị triển khai, `SET LOCAL ROLE` | — | CC | ADR-0028 |
| 8 | `k/ref`, độ trễ có giới hạn | — | CC | ADR-0029 |
| 9 | Đọc tại chỗ có cấp phép | ADR-0020 (khác) | CC | ADR-0030, sửa ADR-0009/0020 |
| 10 | `r_agg` BYPASSRLS có kiểm soát | — | CC | ADR-0031 |
| 11 | Đơn vị hành chính có hiệu lực + kế thừa | Chỉ có migration pf 000002 | LN | ADR-0032 |
| 12 | `vault` cụm riêng từ ĐTK 1 | ADR-0021 (`pg-vault`), ADR-0008 | LN | Ghi rõ ĐTK 1 production |
| 13 | TimescaleDB chỉ ở vùng nội bộ một đơn vị | ADR-0017 (**không** TimescaleDB) | LN | Ghi lệch (X-05) |
| 14 | Tách xác thực và phân quyền; không mạo danh | `kn-v7.7-gap.md` §4.5 phương án A, chế độ hỗ trợ có lý do | LN | ADR-0037 ghi lại KN P1 |
| 15 | MKG qua cổng như mọi đối tác | ADR-0020 (ngược lại) | CC | ADR-0033 |
| 16 | Luồng song phương | — | CC | ADR-0034 (chờ C-12) |
| 17 | Cấp phép tổng hợp theo xã có ngưỡng k | ADR-0022 (`dp_*`, `k_min`) | LN | ADR-0035 |
| 18 | Feed cursor + webhook mỏng | — | CC | Ghép ADR-0033 |
| 19 | Ba chế độ engine ở vùng nội bộ; `lib/optcore` | ADR-0016 (phần giấy phép) | CC | ADR-0036 |
| 20 | OR-Tools theo quy trình giấy phép | ADR-0016 (chỉ HiGHS) | CC | Khi cần, ADR riêng |

### 3.11 Phụ lục D — Ánh xạ quy tắc cũ

| Nguồn | Quy tắc cũ → v4.x | Trạng thái trong repo | TT |
|---|---|---|---|
| v3.0 | R-01 → R-ARC-01 | Như R-ARC-01 | LN |
| v3.0 | R-02 … R-07 → R-MOD-01/02, R-DB-07, mục 21 | `agboundary`, CONNECT theo vai, `migratecheck` cụm ts | Đ |
| v3.0 | R-10 … R-16 → R-CODE-01 … 06, R-API-05 | `nofloat`, `ids`, `clock`, `piilog`, `problem`, tenant từ token | Đ |
| v3.0 | R-20 … R-25 → R-DB-06, R-DB-12 (DDL sửa DB-01, DB-03) | ADR-0012; R-DB-12 chưa | LN |
| v3.0 | R-30 … R-33 → R-DB-08; `WithTenantTx` → `WithContextTx` + `SET LOCAL ROLE` | Vẫn `WithTenantTx` | LL |
| v3.0 | R-40 … R-42 → §20, SAD | vault, ADR-0004 | Đ |
| v3.0 | R-50 … R-56 → R-API-01 … 06; R-56 → `cap/workflow` + SAD | R-API đạt. R-56 bằng CHECK từng module | LN |
| v3.0 | R-60 … R-64 → R-EVT-01 … 04 | ADR-0002, `contractcheck` | Đ |
| v3.0 | R-70, R-71 → §27.2 `mkg/measurement`, §21 | mkg 000007–000008, cụm ts | Đ |
| v3.0 | R-80 … R-82 → R-SEC-01, 02 | ADR-0008/0021 | Đ |
| v3.0 | R-90 … R-92, R-95 → §31, R-OPS-01 | OTel, seed tổng hợp | LN |
| SAD v1.1 | R-ID-01 … 46 giữ nguyên | Repo chưa có SAD. Phần tương ứng: Keycloak (MFA, khóa A3), KN P1, hỗ trợ | LN |
| SAD v1.1 | §6.3 chia sẻ bằng sao chép → thay bằng §17.3 | Repo chưa từng hiện thực sao chép; dùng API + cấp phép (ADR-0020) | LN |
| SAD v1.1 | §7.1 DDL `assignment_event` → mẫu 15.1 | `role_assignment_event` PK `(id, recorded_at)` (pf 000060) | Đ |
| v3.0 R-31, SAD 6.2 | "Thiếu biến → NULL → rỗng" sai trên kết nối tái sử dụng → `ctx_uuid` | `shared.current_tenant()` + ADR-0005 (đã sửa từ G0) | Đ |
| SAD 6.2 | `in_node_scope()` → `k_ref.node_closure`, `share_scope` | Repo không dùng `in_node_scope`; phạm vi ở ứng dụng | LN |
| Module hóa v1.0 | R-MOD-01 … 10 (thêm ngoại lệ lõi) | Mục 3.3 | LN |
| v4.0 | `dom/hubsim`, `dom/waterway` → `pf/simulation`, `waterway_routes` (pf 000046) | Đúng như ghi | Đ |
| v4.0 | `cap/*`, `dom/*`, R-MOD-10 chưa có, đưa vào `docs/` qua ADR | Tài liệu này + ADR-0027 | CC |
| MKG v1.3 | Toàn bộ → §27.2; cổng nhận VFT dùng chung mã cổng | Repo chưa có tài liệu v1.3 | LN |
| v3.0 | "Ba cửa" (mkg.md §1.1) → bốn cửa | `docs/design/mkg.md` §1.1 vẫn ghi ba cửa | LN |
| v3.0 | "Mục x" trong ADR/conventions (số mục v3.0) | Các ADR dẫn mục v3.0 (vd. "mục 8.1", "mục 9", "mục 17") | LN |

> Hai dòng cuối là ánh xạ ngầm do repo phát sinh, không có trong Phụ lục D. Chúng được giữ lại để tính công việc cập nhật tài liệu.

---

## 4. Xung đột giữa v4.1 và các quyết định đã có

| # | Xung đột | Quyết định đã có | v4.1 | Khuyến nghị |
|---|---|---|---|---|
| **X-01** | Ai là **chủ** dữ liệu HTX, hộ, thửa? | ADR-0009: tenant chương trình khuyến nông là chủ. HTX là tổ chức trong tenant đó. App HTX đọc cùng tenant | §5.1: KN đọc `dom/cooperative` **qua cấp phép** (○); HTX dùng trực tiếp (●). C-10 cần "thỏa thuận chia sẻ giữa HTX và cơ quan khuyến nông". Ngụ ý **HTX (hoặc đơn vị vận hành App HTX) là đơn vị chủ** | **Hỏi MKG (H-1).** ĐTK 1 giữ ADR-0009 (một đơn vị, mã đã chạy nhiều đơn vị). Không chặn W3.x vì RLS theo Nút + nhánh chia sẻ chạy được cho cả hai mô hình |
| **X-02** | Chia sẻ giữa đơn vị: tenant→tenant theo `kinds` qua `/v1/partner/*` | ADR-0020 (chấp nhận 30/09) | §17.3: đọc tại chỗ bằng RLS `share_scope`. §37: MKG qua cổng | **Theo v4.1.** ADR-0030 thay phần "cơ chế đọc" của ADR-0020. ADR-0033 thay phần "MKG đọc nền tảng". Giữ sổ cấp phép, bốn mắt và CHECK |
| **X-03** | Van MKG → nền tảng qua RabbitMQ vhost `/` | ADR-0018, mkg.md §1.1 | §38.1: dữ liệu công bố nạp theo lô qua cổng. R-INT-01: không dùng chung hàng đợi | **Giữ cho ĐTK 1, ghi lệch** trong ADR-0033 có hạn: chuyển sang nạp theo lô ở G2. Hợp đồng van (`contracts/publication`) dùng lại nguyên làm hợp đồng nạp |
| **X-04** | Bảng kê số cuộn theo thửa qua van công bố | mkg.md §3.2 (`htx_collection_statement` dự thảo G3-0) trong khi chính §3.1(4) cấm nguồn gốc lô tới thửa/HTX qua van | §38.2: là dữ liệu song phương | **Theo v4.1** (ADR-0034). Cần C-12 (H-2). Trong lúc chờ: hạn chế đọc bảng kê theo `org_ref` của HTX + vai MKG, ghi rõ trong mkg.md |
| **X-05** | Chuỗi thời gian | ADR-0017: PostgreSQL thuần, cấm TSL (RB-4), có RLS | §6, §21: TimescaleDB ở `mkg-ts`, nén tầng lạnh, không RLS | **Giữ ADR-0017, ghi lệch.** TimescaleDB TSL (nén) trái RB-4. Bố cục đã tương thích hypertable. Hỏi MKG chỉ khi cần nén (H-4) |
| **X-06** | Đồng bộ ngoại tuyến | ADR-0006: engine = Application API, không logical replication | §6, C-1, §36: engine tự vận hành, slot failover sáu điều kiện | **Giữ ADR-0006.** C-1 không chặn. Ghi lệch |
| **X-07** | Vị trí engine tối ưu | ADR-0016, `optimizer/` phục vụ `pf/simulation` ở vùng nền tảng | §27.2: cả ba chế độ ở vùng nội bộ. `lib/optcore` dùng chung với `dom/hubsim` | **Theo v4.1, không xung đột thật.** `dom/hubsim` (App SIM) vẫn ở nền tảng, dùng `lib/optcore`. Engine nội bộ là triển khai thứ hai. ADR-0036. Cần MKG v1.3 |
| **X-08** | Quan sát | A10 (mở): Grafana/Loki/Tempo là AGPL, dev đề xuất không dùng | §6: Loki, Tempo, Grafana | **Hỏi MKG (= A10, H-5).** Chỉ dùng nội bộ, không phân phối thì AGPL thường chấp nhận được. Cần pháp chế duyệt theo RB-4 |
| **X-09** | Vị trí `chain_head`, idempotency sổ | ADR-0012: `shared.chain_head`, `shared.ledger_idem` chung, vĩnh viễn | §15.2–15.3: mỗi schema một bảng, idempotency 30 ngày | **Giữ vị trí (ghi lệch). Theo v4.1 về hạn 30 ngày** (W2.3) |
| **X-10** | Vai DB | g1–g5 conventions: vai LOGIN mỗi module, không quyền chéo | §14: NOLOGIN `r_<module>` + LOGIN theo đơn vị triển khai | **Theo v4.1** (ADR-0028). Giữ nguyên "không quyền chéo" (thành `r_*` không có quyền schema khác) |
| **X-11** | Phạm vi trong tenant | ADR-0009, ADR-0005 (R-33): RLS chỉ cách ly tenant, phạm vi ở ứng dụng | §17.2: RLS theo `node_closure` | **Theo v4.1** cho bảng `pf_coop` trước, giữ kiểm ở ứng dụng (hai lớp). Sửa ADR-0005 |
| **X-12** | Thửa tối thiểu 3 hay 4 đỉnh | A2 (03/10): 3 điểm theo BRD v7.7 (pf 000084) | §24: "tối thiểu 4 đỉnh" | **Giữ A2.** 4 đỉnh trong v4.1 có lẽ là 4 điểm của vòng khép kín (3 điểm + điểm đóng). Nhờ kiến trúc sư xác nhận (H-6) |
| **X-13** | Phòng ban là module (KN v7.7 D2) | `pf_party.department` bật theo trạng thái, menu từ API | R-MOD-08: bật module **theo đơn vị** | **Giữ cả hai:** bật module theo đơn vị (W1.4); phòng ban là cấu hình của `dom/extension` trong đơn vị. Ghi trong ADR-0027 |
| **X-14** | Ngữ cảnh phiên | KN P1: `X-AG-Context`, server tra `session_context` | SAD: token ngữ cảnh của Access, epoch, biến DB `node/dept/xa_ids/purpose` | **Giữ KN P1** làm hiện thực ĐTK 1 của `ResolveContext`. Thêm epoch + đặt biến ngữ cảnh trong `WithContextTx` (W2.5) |
| **X-15** | Public API là đơn vị riêng `cmd/api-public` | ADR-0022 | §6, §41: `cmd/integration-gateway` | **Theo v4.1:** đổi tên, mở rộng `api-public` thành cổng (giữ lý do tách tiến trình của ADR-0022). Sửa ADR-0022/0025 |
| **X-16** | Đường dẫn đối tác | ADR-0020: `/v1/partner/*` trên Application API | R-API-07: không đường riêng theo ứng dụng. R-INT-01: không endpoint riêng | **Theo v4.1;** giữ song song một kỳ (R-API-01) rồi bỏ |
| **X-17** | Đơn vị hành chính theo tenant | pf 000002: PK có `tenant_id` | §19: một nguồn ở `k_orgtree` (cụm idp) | **Ghi lệch cho ĐTK 1** (một tenant). Chuyển về lõi khi có đơn vị thứ hai (W8.3). Nguồn chính thức C-9 |
| **X-18** | RPO | Repo/G-2: RPO 24 h, RTO 4 h | §34: RPO ≤ 1 h (`pf`, `mkg`, `vault`) | **Theo v4.1** (WAL liên tục đã có ở pgBackRest). Xác nhận với vận hành (G-2) |
| **X-19** | Mốc G0–G6 | `docs/roadmap.md`: G1–G5 đã có mã | Phụ lục A: G0 30/09 "cần cập nhật trạng thái" | **Hỏi MKG (H-7):** đổi tên cổng v4.1 thành "G0′/G1′…" trong roadmap, tránh nhầm với G1–G5 của repo |

---

## 5. Lưu ý kỹ thuật cho người làm tiếp

1. **Số migration.** Nhiệm vụ ghi "bắt đầu từ pf 000086", nhưng `pf 000086` (`reports_dispatch_recipient_mailed`) và `pf 000087` (`reports_dispatch_link_revoke_oneway`) **đã có** trên `main` từ commit review sau cùng. Kế hoạch bắt đầu từ **pf 000088**.
2. **`golang-migrate` áp theo số phiên bản.** Một migration có số nhỏ hơn số đã áp trên một cụm sẽ **không bao giờ được áp** và không báo lỗi. Vì vậy số trong kế hoạch được cấp theo **thứ tự gộp dự kiến**. Gộp khác thứ tự thì phải cấp lại số trước khi gộp.
3. **Số còn trống ở các cụm khác:** mkg kế tiếp 000022 · shared 000006 · vault 000003 · ts 000004 (000002 dành cho `ts_env` theo ADR-0017).
