# ADR-0055: Tách hệ thống Khuyến nông

**Trạng thái:** Đề xuất. Chủ sản phẩm đã trả lời Q-KNS-01, 02, 05 ngày 14/10/2026. Q-KNS-03, 04, 06 còn mở · **Ngày:** 14/10/2026 (gói KNSPLIT-0) · **Yêu cầu:** chủ sản phẩm "tách KN thành hệ thống riêng", đồng thuận rà soát nhóm (hai giai đoạn), trả lời PO 14/10/2026 · **Thay thế một phần:** [ADR-0049](0049-cap-price-pf-price.md) (`cap/price` dùng chung → Báo giá của KN), [ADR-0054](0054-cap-directory-pf-contact.md) (`cap/directory` dùng chung → Contact của KN) · **Liên quan:**
- CEO D2 ([ADR-0052](0052-dong-so-huu-mkg-htx-neo-mst.md));
- D24/D25 ([ADR-0041](0041-mfa-theo-nhom-d24.md), realm quản trị `agrigreen-kn-admin`);
- v4.3 ADR-22 "hai hệ thống thông tin" (số giữ chỗ 0040, chưa viết);
- tiền lệ MKG: [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md);
- [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), [ADR-0031](0031-vai-tong-hop-r-agg.md), [ADR-0035](0035-cap-phep-tong-hop-theo-xa-nguong-k.md), [ADR-0053](0053-hop-dong-so-lieu-da-chot-closed-figures.md).

**Thành phần:** [kiểm kê](../kn/tach-he-thong/kiem-ke.md), `tools/knboundary`, `migrations/kn_split_boundary_integration_test.go`

## Bối cảnh

Khuyến nông (KN) hiện trộn với nền tảng và các ứng dụng khác:
- **Hạ tầng chung:** một binary `api-platform`/`worker`, một `apps/web-ops`, một realm `agrigreen`, một cụm `pg-pf`.
- **Module chỉ của KN nằm cạnh module nền tảng ở `internal/platform`:** `pf/extension`, `pf/price`, `pf/hrm`, `pf/contact`.
- **Phần riêng KN còn nằm trong module dùng chung:**
  - `pf/accounts`: vai KN, `KNContext`, ủy quyền, đơn vị nội bộ, job vô hiệu;
  - `pf/party`: menu KN, phòng ban;
  - `pf/forms`: hồ sơ `kn_*`;
  - `pf/masterdata`: danh mục CNTS;
  - `pf/notify`: loại thông báo Báo giá, kỳ báo cáo, lịch công tác;
  - `pf/reports`: nguồn `extensionsrc`;
  - `internal/shared/authz/kn.go`.

Kiểm kê KNSPLIT-0 ([kiem-ke.md](../kn/tach-he-thong/kiem-ke.md)) cho thấy:
- **Hướng KN → nền tảng gần sạch.** KN chỉ import `port/` và không có SQL tới bảng của module khác. Riêng `pf/extension` gọi trong tiến trình port của `pf/cooperative` (`Plots`, `Cooperatives`, `HTXConfirmations`; 4 tệp).
- **Hướng nền tảng → KN chưa sạch.**
  - 22 import port KN.
  - 212 ký hiệu và tệp KN trong module dùng chung.
  - 46 thao tác route KN do module khác phục vụ, trong đó `/v1/kn/{plots,households,machine-counts}` do chính module HTX phục vụ.
  - 59 import KN trong vỏ web.
- **Dữ liệu HTX.**
  - Vai module KN không có quyền gì trên dữ liệu HTX.
  - Người dùng KN thì đọc và ghi dữ liệu HTX qua route KN do module HTX phục vụ. Họ ở tenant lưu ký, đi theo `p_scope`, không qua cấp phép (ADR-0052 §1, ĐTK 1).

**MKG đã tách và là tiền lệ:**
- mã ở `internal/mkg/*`, binary riêng, cụm `pg-mkg`, frontend và client riêng;
- liên thông qua cổng tích hợp như mọi đối tác (ADR-0033).

## Trả lời của chủ sản phẩm (14/10/2026)

**Q-KNS-01 — "tách riêng" nghĩa là gì: ĐÃ TRẢ LỜI.**
- KN là **hệ thống độc lập**.
- KN chỉ trao đổi dữ liệu với hệ khác **qua nền tảng dữ liệu dùng chung**: hợp đồng `_pub` của nền tảng, data product và lớp tích hợp.
- **Không còn gọi trong tiến trình** vào module của hệ khác.

**Q-KNS-02 — NĐ 331 có đòi ranh giới vật lý không: ĐÃ TRẢ LỜI, KHÔNG.**
- Đích là **độc lập logic**.
- Tách vật lý (binary, realm, cụm riêng) là tùy chọn, sau G1, chỉ khi vận hành cần.

**Q-KNS-05 — Báo giá và Contact: ĐÃ TRẢ LỜI.**
- Hệ nào có chức năng này thì **sở hữu bản riêng**. Không có năng lực dùng chung.
- Đối chiếu BRD mới:
  - **Báo giá** thuộc Phòng Thông tin Tuyên truyền của Trung tâm KN. BRD "Hệ thống Khuyến nông — Module: Báo giá nông sản (Phòng Thông tin Tuyên truyền)" v1.3, 07/10/2026, §I.1 và phân quyền Admin/TW/Tỉnh (Phòng TTTT)/Xã. Vậy `pf/price` là **của KN**.
  - **Contact** là FN-13 của BRD "Nền tảng Khuyến nông số — Module 2: Hành chính - Tổng hợp" v3.0, 07/10/2026, chức năng của Phòng HCTH. Các yêu cầu liên quan: UR-GEN-01, UR-GEN-02, UR-QG-02, UR-ADM-10. Vậy `pf/contact` là **của KN**.
- Nếu hệ khác (HTX, MKG) cần báo giá hay danh bạ, hệ đó dựng bản của mình. Nếu cần dữ liệu báo giá KN, đọc qua `pf_price_pub`.

## Quyết định

### 1. KN là hệ độc lập về logic

- **Quy tắc ranh giới.** KN được dùng **dịch vụ nền tảng dùng chung** qua `port/`: tài khoản và xác thực, Nút/cây/đơn vị hành chính, `cap/forms`, `cap/notify`, `cap/reports`, `cap/masterdata`, `cap/sync`, `cap/geo`, `cap/metrics`, kho định danh.
- **KN không gọi trong tiến trình** module của **hệ khác**: HTX (`pf/cooperative`, `pf/cropcycle`, `pf/mechanization`), MKG (`internal/mkg`). Cấm cả `port/` của các module đó.
- **Dữ liệu của hệ khác** KN chỉ nhận qua **nền tảng dữ liệu**:
  - view `_pub` có phiên bản;
  - data product (`contracts/dataproduct`);
  - lớp tích hợp: feed, nạp theo lô, cổng tích hợp ADR-0033.
- **Chiều ngược lại cũng vậy.** Hệ khác đọc dữ liệu KN qua `pf_extension_pub`, `pf_price_pub` và data product của KN.
- **Hai giai đoạn:**

| Giai đoạn | Khi nào | Gồm |
|---|---|---|
| **Logic (đích)** | Trước G1 (15/12/2026) và sau đó | Không gian tên `internal/kn/*`. Phần riêng KN rời module dùng chung, đăng ký qua port/registry của nền tảng. `pf/*` không import `kn/*`. KN không import module HTX/MKG. Dữ liệu HTX qua nền tảng dữ liệu (mục 4). App web KN, client Keycloak riêng trong realm `agrigreen`. Tài khoản KN ở tenant KN. Giữ tên schema vật lý, binary và cụm chung |
| **Vật lý (tùy chọn)** | Sau G1, chỉ khi vận hành cần (mục 7) | `cmd/api-kn`, `cmd/worker-kn`; có thể realm và cụm riêng |

### 2. Bố cục đích

```
internal/kn/              module KN — kind domain/capability theo ADR-0027, zones [pf]
  extension/ price/ hrm/ contact/   (chuyển từ internal/platform, giữ name pf/<x> trong module.yaml)
  accounts/               phần KN của pf/accounts: luật vai KN, nhập cán bộ, ngữ cảnh KN, đơn vị nội bộ
  menu/                   menu KN, mục menu phòng ban (đăng ký vào registry menu của pf/party)
  collect/                màn KN về HTX/hộ/thửa/máy — đọc qua nền tảng dữ liệu, ghi qua lớp tích hợp (mục 4)
  authz/                  KNContext, RequireKN, vai ngữ cảnh kn_tw/kn_tinh/kn_xa/admin
internal/platform/*       nền tảng + hệ HTX: không import internal/kn (cổng knboundary)
apps/web-kn               app web KN, client agrigreen-web-kn
apps/web-ops              vỏ chung + HTX/CGH/SIM/quản trị nền tảng
apps/field-app            app hiện trường KN (đã riêng)
packages/agrigreen_kn_mobile  client /v1/kn, /v1/extension tách khỏi agrigreen_mobile
app/KN/app.yaml           frontends [apps/web-kn, apps/field-app]; clients [agrigreen-web-kn, agrigreen-field-app]
```

Tên module, vai DB và schema (`pf_extension`, `pf_price`, `pf_hrm`, `pf_contact`) giữ nguyên. Lý do: chuỗi băm sổ, `ledger_idem`, cấp quyền và hợp đồng `_pub` gắn với tên.

### 3. Cái gì ở lại nền tảng, cái gì sang KN

| Ở lại nền tảng (dịch vụ dùng chung) | Sang KN | Nền tảng mở điểm mở rộng |
|---|---|---|
| Tài khoản, MFA, khóa, vô hiệu 45 ngày, phiên, ủy quyền chung, phiên hỗ trợ | Luật vai KN × loại Nút, nhập cán bộ KN, ngữ cảnh KN, đơn vị nội bộ | `pf/accounts`: validator gán vai theo ứng dụng; bộ phân giải ngữ cảnh ứng dụng |
| Nút, cây tổ chức, vùng, đơn vị hành chính, phòng ban (dữ liệu) | Menu KN, mục menu phòng ban, màn thu thập HTX cho KN | `pf/party`: registry menu theo ứng dụng |
| `cap/forms` | Hồ sơ `kn`, `kn_cnts`, `kn_khtc` (đã ở gốc lắp ráp) | `formsport.Profile` mang vai; bỏ `authz.KN*` trong `pf/forms` |
| `cap/masterdata` | Danh mục và tham số CNTS | Registry danh mục |
| `cap/notify` | Loại thông báo Báo giá, kỳ báo cáo, lịch công tác | Registry loại thông báo |
| `cap/reports` | Nguồn `extensionsrc` | Registry nguồn báo cáo |
| `authz` (Principal, vai realm) | `KNContext`, `RequireKN`, vai ngữ cảnh | Ngữ cảnh ứng dụng chung |
| — | **Báo giá** (`pf/price`), **Contact** (`pf/contact`), Nhân sự (`pf/hrm`), nghiệp vụ KN (`pf/extension`) | — |
| Hệ HTX: `cooperative`, `cropcycle`, `mechanization` (không phải dịch vụ dùng chung) | Route `/v1/kn/*` đang do các module này phục vụ | View `_pub`, data product HTX; điểm nạp của lớp tích hợp |

### 4. Ranh giới dữ liệu với HTX

- **Chủ dữ liệu.** Dữ liệu canh tác HTX do (MKG, HTX) đồng sở hữu và nằm ở tenant lưu ký (D2, ADR-0052). KN là **bên thứ ba** và là **hệ khác**.
- **Đọc số tổng hợp**: view `_pub` có phiên bản của module HTX (`pf_coop_pub`, `pf_cropcycle_pub`, `pf_mechanization_pub`).
  - Thêm `pf/extension` (và module KN sau này) vào `dbadmin.PubReaders` cho các chủ đó. Mỗi mục là thay đổi hợp đồng, có `consumers` ở `contracts/pubview`.
  - Số liệu theo xã chịu ngưỡng k của ADR-0031/0035 khi cần.
- **Đọc dòng chi tiết** (thửa, hộ, hồ sơ HTX cho màn thu thập của KN): **data product** hoặc **feed của lớp tích hợp**.
  - Lọc theo cấp phép `share_grant` (`purpose_code = khuyen-nong`, `basis_ref` = thỏa thuận HTX `htx_anchor.agreement_ref`).
  - Ở cùng cụm, data product là view `_pub` theo cấp phép: RLS `p_share` (ADR-0030) chạy dưới vai bên đọc. Khác cụm thì đi qua cổng như MKG.
  - Không gọi port `cooperative` trong tiến trình.
- **Ghi thay** (KN nhập hộ, thửa, đếm máy cho HTX theo thỏa thuận, ADR-0052 §7):
  - đi qua **lớp tích hợp**: nạp theo lô hoặc lệnh có idempotency tới hệ HTX, như đối tác (ADR-0033);
  - có kiểm cấp phép có quyền ghi, nhãn nguồn `kn` trên dòng sổ.

  Q-KNS-04 (KN còn ghi thay không) vẫn mở.
- **Hôm nay (baseline):**
  - `pf/extension` gọi `coopport.Plots`, `Cooperatives`, `HTXConfirmations` trong tiến trình (quy tắc `kn-import-htx`, 4 tệp);
  - route `/v1/kn/{cooperatives,households,plots,machine-counts}` do `party`, `cooperative`, `mechanization` phục vụ, đọc và ghi bảng HTX trực tiếp dưới ngữ cảnh người dùng KN.

  Tất cả là ghép nối phải gỡ ở KNSPLIT-2 (đổi nguồn đọc) và KNSPLIT-4 (tenant, cấp phép, ghi qua lớp tích hợp).
- **Kiểm thử âm** (`migrations/kn_split_boundary_integration_test.go`):
  - vai KN không đọc, không ghi bảng hay sổ HTX: PASS;
  - người dùng KN ở tenant riêng chỉ thấy phạm vi cấp phép, không ghi được, mất quyền khi thu hồi hoặc hết hạn: PASS;
  - người dùng KN ở tenant lưu ký: 5 ca **XFAIL** có lý do. Ca nào hết vi phạm thì báo XPASS và buộc đổi thành khẳng định cứng.

### 5. Tenant và di chuyển cấp phép (KNSPLIT-4)

1. **Tạo tenant KN** (Q-KNS-03: một cho cả hệ hay mỗi Trung tâm tỉnh). Dời Nút KN sang tenant KN bằng `org_event`, không ghi lại sổ HTX.
2. **Cấp phép hàng loạt** tenant lưu ký → tenant KN, mỗi neo HTX hoặc Nút tỉnh:
   - `basis_ref = agreement_ref`, mục đích `khuyen-nong`;
   - khóa `shares` cần: `pf_coop.household`, `pf_coop.plot`, `pf_coop.season`, `pf_coop.diary_entry`, `pf_mechanization.machine`;
   - dùng luồng hàng loạt W4.1 có duyệt bốn mắt;
   - chạy thử, báo cáo số dòng thấy được trước và sau theo từng cán bộ.
3. **Chuyển tài khoản KN** sang Nút ở tenant KN, giữ `sub` Keycloak. Có giai đoạn song song, cờ theo tỉnh, đối chiếu kết quả.
4. **Dữ liệu riêng KN** đang ở tenant lưu ký: đề xuất giữ `tenant_id` như D2 (Q-KNS-03).
5. **Đóng:** đổi 5 ca XFAIL thành khẳng định cứng.

### 6. Realm, client, frontend, binary, cụm

| Mục | Đích (logic) | Tùy chọn sau G1 (mục 7) |
|---|---|---|
| Realm người dùng | `agrigreen` chung; khai là dịch vụ định danh dùng chung | Realm `agrigreen-kn` |
| Realm quản trị | `agrigreen-kn-admin` (D25, đã có) | Giữ |
| Client | Thêm `agrigreen-web-kn` (public, PKCE, chỉ vai KN qua client scope); giữ `agrigreen-field-app` | Chuyển realm |
| Frontend | `apps/web-kn` tách từ `apps/web-ops` | — |
| Binary | `api-platform`, `worker` chung | `cmd/api-kn`, `cmd/worker-kn`, vai `app_kn` (ADR-0028) |
| Cụm DB | `pg-pf` chung | `pg-kn`; KN đọc HTX qua cổng như MKG |
| K8s | Namespace và chart chung | Namespace `agrigreen-kn`, NetworkPolicy riêng |

### 7. Khi nào làm tách vật lý (tùy chọn, sau G1)

NĐ 331 **không** đòi ranh giới vật lý cho KN (trả lời PO Q-KNS-02, 14/10/2026). Vì vậy tách vật lý **không** gắn hồ sơ cấp độ.

KNSPLIT-5+ chỉ mở khi vận hành có nhu cầu đo được, ví dụ:
- KN cần nhịp phát hành hoặc cửa sổ bảo trì riêng mà binary chung cản trở;
- tải hoặc sự cố của KN ảnh hưởng hệ khác (hoặc ngược lại) vượt SLO đã chốt;
- bên vận hành KN tách khỏi bên vận hành nền tảng theo hợp đồng.

Mỗi lần mở cần quyết định của PO kèm số đo. Giai đoạn logic là trạng thái đích. Báo cáo `knboundary`, cổng W9.1 và kiểm thử KNSPLIT-0 là bằng chứng ranh giới cho hồ sơ (W10.16).

### 8. Gói việc theo thứ tự

| Gói | Việc | Phụ thuộc | Xong khi |
|---|---|---|---|
| **KNSPLIT-0** | Kiểm kê, cổng `knboundary` (9 quy tắc gồm `kn-import-htx`, baseline chỉ co lại), ADR này, kiểm thử âm quyền dữ liệu | — | Gói này |
| **KNSPLIT-1** | Chuyển `internal/platform/{extension,price,hrm,contact}` → `internal/kn/*` (giữ tên module/vai/schema); `tests/kn77` → KN; sửa `kn_modules` và baseline cùng PR | Sau WH-A | `knboundary`, `modgen -check`, `archlint` xanh |
| **KNSPLIT-2** | Tách phần KN khỏi module dùng chung (mục 3). **Thay đọc trong tiến trình `coopport.*` của `pf/extension` bằng đọc qua nền tảng dữ liệu** (view `_pub` HTX + `PubReaders`; data product có cấp phép cho dòng chi tiết). Chuyển route `/v1/kn/*` sang KN, đọc qua nền tảng dữ liệu. Chia 2a (`notify`, `reports`, `forms`, `masterdata` registry; view/data product HTX) và 2b (`accounts`, `party`, `authz`, route) | KNSPLIT-1 | `import-into-kn`, `kn-import-htx`, `kn-symbol-in-shared`, `route-owner`, `sql-into-kn` (trừ mục migration vĩnh viễn) về 0 |
| **KNSPLIT-3** | `apps/web-kn` + client `agrigreen-web-kn`; vỏ chung không import KN; `packages/agrigreen_kn_mobile` | KNSPLIT-2 | `web-import-into-kn`, `web-kn-import-app` về 0 |
| **KNSPLIT-4** | Tenant KN, cấp phép hàng loạt, chuyển tài khoản (mục 5). **Ghi thay dữ liệu HTX qua lớp tích hợp** thay cho route KN ghi trực tiếp bảng HTX (nếu Q-KNS-04 giữ ghi thay). Data product dòng chi tiết chạy dưới tenant KN và `p_share` | KNSPLIT-2; H-15; Q-KNS-03, Q-KNS-04 | 5 ca XFAIL thành khẳng định cứng |
| **KNSPLIT-5+** (tùy chọn) | Tách vật lý (mục 6) | Sau G1; chỉ khi mục 7 đúng và PO quyết | — |

## Câu hỏi cho chủ sản phẩm

| # | Câu hỏi | Trạng thái |
|---|---|---|
| Q-KNS-01 | "Tách riêng" là logic, vận hành hay hệ thống thông tin riêng? | **Đã trả lời 14/10/2026:** "KN là hệ thống độc lập, trao đổi dữ liệu với hệ khác chỉ qua nền tảng dữ liệu dùng chung (hợp đồng `_pub`, data product, lớp tích hợp); không gọi trong tiến trình vào module của hệ khác" |
| Q-KNS-02 | NĐ 331 có đòi ranh giới vật lý? | **Đã trả lời 14/10/2026:** "Không. Đích là độc lập logic; tách vật lý (binary, realm, cụm) là tùy chọn sau G1, chỉ khi vận hành cần" |
| Q-KNS-03 | Tenant KN: một cho cả hệ hay mỗi Trung tâm tỉnh? Dữ liệu KN cũ giữ `tenant_id` hay chuyển chủ? | Mở. Đề xuất: một tenant, giữ `tenant_id` |
| Q-KNS-04 | Sau tách, KN còn ghi thay dữ liệu HTX không? | Mở. Đề xuất: có, qua lớp tích hợp, nhãn nguồn `kn` |
| Q-KNS-05 | `pf/price`, `pf/contact` là của KN hay dùng chung? | **Đã trả lời 14/10/2026:** "Hệ nào có chức năng thì sở hữu bản riêng, không có năng lực dùng chung". Theo BRD: Báo giá là của KN (Phòng TTTT), Contact FN-13 là của KN (HCTH). Cả hai thuộc KN |
| Q-KNS-06 | Thu thập MRV ở `apps/field-app` thuộc KN hay HTX/MKG? | Mở (hỏi cùng ADR-24) |

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Tách vật lý ngay trước G1 | Không kịp. NĐ 331 không đòi (Q-KNS-02) |
| KN gọi port HTX trong tiến trình dưới cấp phép (đề xuất ban đầu của KNSPLIT-0) | Trái trả lời Q-KNS-01: hệ độc lập chỉ trao đổi qua nền tảng dữ liệu |
| Báo giá, Contact là năng lực dùng chung (ADR-0049, ADR-0054) | Trái trả lời Q-KNS-05 và BRD: chức năng của phòng ban Trung tâm KN |
| Chỉ chuyển thư mục, không cổng kiểm | Ghép nối mới quay lại ngay khi 40 gói chạy song song |
| Đổi tên schema khi chuyển | Đổi chuỗi băm, cấp quyền, hợp đồng `_pub`, không có lợi ích kiểm chứng được |

## Hệ quả

- **Cổng mới.** `go run ./tools/knboundary` chạy trong `make gates` và CI (`-base` để baseline chỉ co lại).
  - Gói nào thêm import port HTX vào KN, phần riêng KN vào module dùng chung, hay import KN vào nền tảng hoặc vỏ web sẽ hỏng cổng.
- **KNSPLIT-2 cần data product HTX.** Cần view `_pub` hoặc data product HTX mới, kèm hợp đồng có phiên bản và `consumers`. Việc này nằm trong phạm vi hồ sơ của hệ HTX.
- **ADR-0049 và ADR-0054 bị thay thế một phần.**
  - `pf/price` và `pf/contact` không còn là năng lực dùng chung.
  - Mã, hợp đồng và quyết định dữ liệu bên trong mỗi module giữ nguyên.
- **Kiểm thử XFAIL.** Kiểm thử dữ liệu giữ 5 ca XFAIL tới KNSPLIT-4.

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| KNSPLIT-0 (14/10/2026) | Kiểm kê; `tools/knboundary` (9 quy tắc, `-list`/`-prune`/`-base`, kiểm thử trên repo mẫu và repo thật); `make gates`, `make gates-base`, CI; `migrations/kn_split_boundary_integration_test.go` (3 kiểm thử, 5 ca XFAIL). Sau trả lời PO 14/10: thêm quy tắc `kn-import-htx`; `pf/price` và `pf/contact` là của KN | — |
