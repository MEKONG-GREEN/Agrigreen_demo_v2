# ADR-0055: Tách hệ thống Khuyến nông

**Trạng thái:** Đề xuất · **Ngày:** 14/10/2026 (gói KNSPLIT-0) · **Yêu cầu:** chủ sản phẩm "tách KN thành hệ thống riêng" và đồng thuận rà soát nhóm (hai giai đoạn) · **Liên quan:** CEO D2 ([ADR-0052](0052-dong-so-huu-mkg-htx-neo-mst.md)), D24/D25 ([ADR-0041](0041-mfa-theo-nhom-d24.md), realm quản trị `agrigreen-kn-admin`), v4.3 ADR-22 "hai hệ thống thông tin" (số giữ chỗ 0040, chưa viết), [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md)/[ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md) (tiền lệ MKG), [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), [ADR-0053](0053-hop-dong-so-lieu-da-chot-closed-figures.md) · **Thành phần:** [kiểm kê](../kn/tach-he-thong/kiem-ke.md), `tools/knboundary`, `migrations/kn_split_boundary_integration_test.go`

## Bối cảnh

Khuyến nông (KN) hiện trộn với nền tảng và các ứng dụng khác:
- một binary `api-platform`/`worker`, một `apps/web-ops`, một realm `agrigreen`, một cụm `pg-pf`;
- module chỉ của KN (`pf/extension`, `pf/price`, `pf/hrm`, `pf/contact`) nằm cạnh module nền tảng ở `internal/platform`;
- phần riêng KN còn nằm trong module dùng chung:
  - `pf/accounts`: vai KN, `KNContext`, ủy quyền, đơn vị nội bộ, job vô hiệu;
  - `pf/party`: menu KN, phòng ban;
  - `pf/forms`: hồ sơ `kn_*`;
  - `pf/masterdata`: danh mục CNTS;
  - `pf/notify`: loại thông báo Báo giá, kỳ báo cáo, lịch công tác;
  - `pf/reports`: nguồn `extensionsrc`;
  - `internal/shared/authz/kn.go`.

Kiểm kê KNSPLIT-0 ([kiem-ke.md](../kn/tach-he-thong/kiem-ke.md)) cho thấy:
- **Hướng KN → nền tảng đã sạch.** KN chỉ import `port/`, không có SQL tới bảng của module khác.
- **Hướng nền tảng → KN chưa sạch.** Có 22 import port KN, 212 ký hiệu và tệp KN trong module dùng chung, 46 thao tác route KN do module khác phục vụ, và 59 import KN trong vỏ web.
- **Dữ liệu HTX.** Vai module KN không có quyền gì trên dữ liệu HTX. Người dùng KN thì đọc và ghi dữ liệu HTX qua route KN do module HTX phục vụ, ở tenant lưu ký, theo `p_scope`, không qua cấp phép (ADR-0052 §1, ĐTK 1).

**MKG đã tách và là tiền lệ:**
- mã ở `internal/mkg/*`;
- binary riêng (`api-internal`, `worker-internal`, `ingest`, `edge-agent`);
- cụm `pg-mkg`, frontend `apps/web-internal`, client riêng;
- liên thông qua cổng tích hợp như mọi đối tác (ADR-0033).

MKG là một **hệ thống thông tin** riêng theo v4.3 ADR-22 (cấp độ 2), khác Nền tảng (cấp độ 3). D25 đặt realm quản trị riêng cho từng hệ; với KN là `agrigreen-kn-admin`.

Mốc G1 là 15/12/2026. Nếu G1 có dữ liệu thật và bị coi là "đưa vào sử dụng" theo NĐ 331/2026, hồ sơ cấp độ phải xong trước mốc đó (QĐ-1, Q-001). Phạm vi hồ sơ phụ thuộc vào việc KN là một hệ riêng hay một phần của Nền tảng (QĐ-3, Q-002).

## Quyết định

### 1. Hai giai đoạn

| Giai đoạn | Khi nào | Gồm | Không gồm |
|---|---|---|---|
| **Logic** | Trước G1 (15/12/2026) | Không gian tên `internal/kn/*`. Phần riêng KN rời module dùng chung, đăng ký vào nền tảng qua port/registry. `pf/*` không import `kn/*`. KN đọc dữ liệu HTX chỉ qua `_pub` và cấp phép (D2). App web KN và client Keycloak riêng trong realm `agrigreen` | Đổi tên schema, binary, realm, cụm |
| **Vật lý** | Sau G1, chỉ trong phạm vi hồ sơ NĐ 331 đòi (mục 7) | `cmd/api-kn`, `cmd/worker-kn`; có thể realm và cụm riêng; tài khoản KN sang tenant KN với cấp phép hàng loạt | Viết lại sổ, đổi chuỗi băm |

Giai đoạn logic tự nó có giá trị: ranh giới được kiểm bằng máy. Nó cũng là điều kiện cần của giai đoạn vật lý. Giai đoạn vật lý chỉ làm khi có lý do pháp lý hoặc vận hành (mục 7).

### 2. Bố cục đích (giai đoạn logic)

```
internal/kn/              module KN — kind domain/capability theo ADR-0027, zones [pf]
  extension/ price/ hrm/ contact/   (chuyển từ internal/platform, giữ name pf/<x> trong module.yaml tới KNSPLIT-5)
  accounts/               phần KN của pf/accounts: luật vai KN, nhập cán bộ, ngữ cảnh KN, đơn vị nội bộ
  menu/                   menu KN, mục menu phòng ban (đăng ký vào registry menu của pf/party)
  collect/                route /v1/kn/{cooperatives,households,plots,machine-counts} — gọi port HTX
  authz/                  KNContext, RequireKN, vai ngữ cảnh kn_tw/kn_tinh/kn_xa/admin
internal/platform/*       nền tảng: không import internal/kn (cổng knboundary)
apps/web-kn               app web KN (Next.js), client agrigreen-web-kn
apps/web-ops              vỏ chung + HTX/CGH/SIM/quản trị nền tảng
apps/field-app            app hiện trường KN (đã riêng)
packages/agrigreen_kn_mobile  client /v1/kn, /v1/extension tách khỏi agrigreen_mobile
app/KN/app.yaml           modules trỏ module KN; frontends [apps/web-kn, apps/field-app]; clients [agrigreen-web-kn, agrigreen-field-app]
```

Tên module (`pf/extension`, …), tên vai DB (`r_extension`, …) và tên schema (`pf_extension`, …) giữ nguyên ở giai đoạn logic. Chuyển thư mục không đổi hợp đồng nào.

### 3. Cái gì ở lại nền tảng, cái gì sang KN

| Ở lại nền tảng (KN dùng như mọi ứng dụng) | Sang KN | Nền tảng mở điểm mở rộng (port/registry) |
|---|---|---|
| Tài khoản, MFA, khóa, vô hiệu 45 ngày, phiên, ủy quyền chung, phiên hỗ trợ | Luật vai KN × loại Nút, nhập cán bộ KN, ngữ cảnh KN, đơn vị nội bộ | `pf/accounts`: validator gán vai theo ứng dụng; bộ phân giải ngữ cảnh ứng dụng |
| Nút, cây tổ chức, vùng, đơn vị hành chính, phòng ban (dữ liệu) | Menu KN, mục menu phòng ban, thu thập HTX cho KN | `pf/party`: registry menu theo ứng dụng |
| `cap/forms` (engine kỳ, nộp, rà soát) | Hồ sơ `kn`, `kn_cnts`, `kn_khtc` (đã ở gốc lắp ráp) | `formsport.Profile` mang vai, bỏ `authz.KN*` trong `pf/forms` |
| `cap/masterdata` (engine danh mục, tham số có phiên bản) | Định nghĩa danh mục và tham số CNTS | Registry danh mục (manifest/seed) |
| `cap/notify` (kênh, gửi, RB-7) | Loại thông báo Báo giá, kỳ báo cáo, lịch công tác | Registry loại thông báo: sự kiện → mẫu → người nhận |
| `cap/reports` (engine mẫu, xuất bản) | Nguồn `extensionsrc` | Registry nguồn báo cáo |
| `authz` (Principal, vai realm) | `KNContext`, `RequireKN`, vai ngữ cảnh | Khái niệm chung "ngữ cảnh ứng dụng" |
| `cooperative`, `cropcycle`, `mechanization` (dữ liệu HTX, D2) | Route `/v1/kn/*` đang ở các module này | Port đọc/ghi HTX có kiểm cấp phép |
| Vỏ web, xác thực web, lưới biểu mẫu | `src/kn/**`, `src/app/kn/**`, mock KN | Hợp đồng phiên/menu chung ở vỏ; registry mock |

`pf/contact` (`cap/directory`, ADR-0054) và `pf/price` (`cap/price`, ADR-0049) được khai là module **năng lực**, dự kiến dùng chung về sau. Đề xuất: xếp chúng vào KN tới khi có ứng dụng thứ hai dùng (quy tắc ba, R-MOD-09). Xem Q-KNS-05.

### 4. Ranh giới dữ liệu với HTX

- Dữ liệu canh tác HTX do (MKG, HTX) đồng sở hữu. Nó ở tenant lưu ký, trong schema của `pf/cooperative`, `pf/cropcycle`, `pf/mechanization` (D2, ADR-0052).
- KN là **bên thứ ba**. KN đọc dữ liệu HTX theo hai đường:
  1. **View `_pub`** của module HTX cho số tổng hợp. Thêm `pf/kn` vào `dbadmin.PubReaders` cho `pf/cooperative`, `pf/cropcycle` khi có view; mỗi mục là thay đổi hợp đồng.
  2. **Port của module HTX**, dưới cấp phép `share_grant` `intra_platform`, `purpose_code = khuyen-nong`, `basis_ref` = thỏa thuận HTX. RLS `p_share` (ADR-0030) chặn ở DB.

  Vai module KN không bao giờ có quyền trên schema nội bộ HTX. Cổng `TestNoCrossModuleSchemaPrivileges_W91` và kiểm thử KNSPLIT-0 giữ điều này.
- **Ghi thay** (KN nhập hộ, thửa, máy cho HTX theo thỏa thuận, ADR-0052 §7):
  - đi qua port ghi của module HTX;
  - kiểm cấp phép có quyền ghi (mới; `p_share` hiện chỉ SELECT);
  - dòng sổ ghi `source`/`created_by_kind = kn`.

  Nếu PO bỏ ghi thay (Q-KNS-04), port ghi chỉ còn cho HTX tạm do KN lập.
- **Kiểm thử âm** (`migrations/kn_split_boundary_integration_test.go`) chạy ngay hôm nay:
  - vai KN không đọc và không ghi bảng hay sổ HTX: PASS;
  - người dùng KN ở tenant riêng chỉ thấy phạm vi cấp phép, không ghi được, mất quyền khi thu hồi hoặc hết hạn: PASS;
  - người dùng KN ở tenant lưu ký: 5 ca **XFAIL** có lý do. Ca nào hết vi phạm thì báo XPASS và buộc đổi thành khẳng định cứng.

### 5. Tenant và di chuyển cấp phép (KNSPLIT-4)

1. **Tạo tenant KN** (một tenant cho hệ KN, hoặc một tenant mỗi Trung tâm KN tỉnh: Q-KNS-03). Nút KN (TW, tỉnh, tổ xã, phòng ban) chuyển sang tenant KN bằng sự kiện `org_event` (dời Nút), không ghi lại sổ HTX.
2. **Cấp phép hàng loạt** từ tenant lưu ký sang tenant KN: mỗi neo HTX (hoặc mỗi Nút tỉnh), `basis_ref = htx_anchor.agreement_ref`, mục đích `khuyen-nong`, các khóa `shares` cần (`pf_coop.household`, `pf_coop.plot`, `pf_coop.season`, `pf_coop.diary_entry`, `pf_mechanization.machine`). Dùng luồng hàng loạt W4.1 có duyệt bốn mắt. Chạy thử trước, báo cáo số dòng thấy được trước và sau theo từng cán bộ.
3. **Chuyển tài khoản KN**: gán vai sang Nút ở tenant KN (`pf/accounts`, sổ gán vai), giữ `sub` Keycloak. Có giai đoạn song song: cờ theo tỉnh, tài khoản được chọn tenant lưu ký hoặc KN, đối chiếu kết quả.
4. **Dữ liệu riêng của KN** (`pf_extension`, `pf_price`, `pf_hrm`, `pf_contact`) đang ở tenant lưu ký. Chọn một trong hai:
   - giữ `tenant_id` cũ và khai tenant lưu ký là chủ của dữ liệu KN cũ;
   - ghi bù qua sổ (sự kiện "chuyển chủ", không UPDATE lịch sử, RB-1).

   Đề xuất: giữ, như D2 đã làm với HTX (Q-KNS-03).
5. **Đóng:** đổi 5 ca XFAIL của kiểm thử KNSPLIT-0 thành khẳng định cứng. Bỏ nhánh "KN ở tenant lưu ký" trong tài liệu D2.

### 6. Realm, client, frontend, binary, cụm

| Mục | Giai đoạn logic (trước G1) | Giai đoạn vật lý (sau G1, nếu cần) |
|---|---|---|
| Realm người dùng | `agrigreen` chung | Realm `agrigreen-kn` nếu hồ sơ đòi tách lớp định danh. Không thì khai `agrigreen` là "kết nối" giữa các hệ (ADR-22) |
| Realm quản trị | `agrigreen-kn-admin` (D25, đã có) | Giữ |
| Client | Thêm `agrigreen-web-kn` (public, PKCE, chỉ vai KN qua client scope); giữ `agrigreen-field-app` | Chuyển sang realm KN |
| Frontend | `apps/web-kn` tách từ `apps/web-ops` (KNSPLIT-3); vỏ dùng chung thành gói hoặc sao chép có chủ | — |
| Binary | `api-platform`, `worker` chung; KN lắp ráp ở `cmd/api-platform/assembly` | `cmd/api-kn`, `cmd/worker-kn` với vai đăng nhập `app_kn` (ADR-0028) |
| Cụm DB | `pg-pf` chung; schema KN giữ tên | Có thể `pg-kn`: KN đọc HTX qua cổng tích hợp như MKG (ADR-0033), không đọc tại chỗ. Tốn kém, chỉ làm khi hồ sơ đòi không kết nối DB |
| K8s | Namespace và chart chung | Namespace `agrigreen-kn`, NetworkPolicy riêng (ADR-0008 bảng luồng) |

### 7. Khi nào làm giai đoạn vật lý (gắn hồ sơ NĐ 331)

Giai đoạn vật lý chỉ làm khi **ít nhất một** điều sau đúng. Mỗi điều do pháp chế hoặc đơn vị chuyên trách ANM xác nhận bằng văn bản:

1. KN được khai là **hệ thống thông tin riêng** trong hồ sơ cấp độ (không phải một phần của "Nền tảng").
2. Hồ sơ của hệ KN đòi **không kết nối DB** với hệ khác. Ví dụ đúng cách hiểu ADR-22 cho MKG: chỉ nói chuyện qua cổng. Khi đó cần cụm `pg-kn` và cổng tích hợp.
3. Hồ sơ đòi **tài khoản quản trị riêng từng hệ** ở lớp ứng dụng: binary và vai đăng nhập DB riêng. Riêng ở lớp định danh thì D25 đã đáp ứng bằng `agrigreen-kn-admin`.
4. Cấp độ của hệ KN **khác** cấp độ Nền tảng (ví dụ KN cấp 2, Nền tảng cấp 3). Biện pháp kỹ thuật khi đó không áp chung được.
5. Bên vận hành KN (Trung tâm KN) **khác** bên vận hành Nền tảng, và hợp đồng đòi tách hạ tầng.

Nếu không điều nào đúng, giai đoạn logic là trạng thái cuối. Hồ sơ khai KN là phân hệ của Nền tảng, có ranh giới module được kiểm bằng máy. Bằng chứng: báo cáo `knboundary`, cổng W9.1, kiểm thử KNSPLIT-0. W10.16 đưa các báo cáo này vào gói bằng chứng hồ sơ.

### 8. Gói việc theo thứ tự

| Gói | Việc | Phụ thuộc | Xong khi |
|---|---|---|---|
| **KNSPLIT-0** | Kiểm kê, cổng `knboundary` (baseline 376 mục, chỉ co lại), ADR này, kiểm thử âm quyền dữ liệu | — | Gói này |
| **KNSPLIT-1** | Chuyển `internal/platform/{extension,price,hrm,contact}` → `internal/kn/*` (đổi đường dẫn, giữ tên module/vai/schema); `tests/kn77` → KN; sửa `kn_modules` và baseline cùng PR | Sau WH-A (menu, mock web, OpenAPI theo gói) để tránh xung đột | `knboundary` xanh với đường dẫn mới; `modgen -check`, `archlint` xanh |
| **KNSPLIT-2** | Tách phần KN khỏi module dùng chung (mục 3): `authz.KN*`, phần KN của `accounts`, menu `party`, danh mục CNTS, loại thông báo `notify`, nguồn `reports`; route `/v1/kn/*` sang KN qua port HTX. Có thể chia 2a (`notify`, `reports`, `forms`, `masterdata` — registry) và 2b (`accounts`, `party`, `authz`, route) | KNSPLIT-1 | Mục `import-into-kn`, `kn-symbol-in-shared`, `route-owner`, `sql-into-kn` (trừ mục migration vĩnh viễn) về 0 |
| **KNSPLIT-3** | `apps/web-kn` + client `agrigreen-web-kn`; vỏ chung không import KN; `packages/agrigreen_kn_mobile` | KNSPLIT-2 (hợp đồng menu/phiên chung) | `web-import-into-kn`, `web-kn-import-app` về 0; `app/KN/app.yaml` trỏ `apps/web-kn` |
| **KNSPLIT-4** | Tenant KN, dời Nút KN, cấp phép hàng loạt, chuyển tài khoản, port ghi thay có kiểm cấp phép (mục 5) | KNSPLIT-2; H-15 (thỏa thuận HTX mẫu); Q-KNS-03, Q-KNS-04 | 5 ca XFAIL thành khẳng định cứng |
| **KNSPLIT-5+** | Giai đoạn vật lý (mục 6, 7): `cmd/api-kn`, `cmd/worker-kn`, `app_kn`; realm, cụm nếu cần | Sau G1; chỉ khi mục 7 đúng | Hồ sơ NĐ 331 của hệ KN được duyệt |

## Câu hỏi cho chủ sản phẩm

| # | Câu hỏi | Vì sao cần | Đề xuất của nhóm |
|---|---|---|---|
| **Q-KNS-01** | "Tách riêng" nghĩa là gì: (a) độc lập **logic** (mã, ranh giới, giao diện, dữ liệu qua cấp phép), (b) độc lập **vận hành** (triển khai, nâng cấp, sự cố của KN không ảnh hưởng hệ khác và ngược lại), hay (c) **hệ thống thông tin riêng** theo NĐ 331? | Từ (a) sang (c), chi phí tăng mỗi bậc khoảng một cấp số | (a) trước G1; (b) và (c) chỉ khi mục 7 đúng |
| **Q-KNS-02** | Hồ sơ NĐ 331 có đòi **ranh giới vật lý** cho KN không (DB, realm, binary, mạng riêng)? Ai xác nhận và khi nào? Gắn với QĐ-1/Q-001 và QĐ-3/Q-002 | Quyết định có KNSPLIT-5 hay không | Hỏi pháp chế cùng Q-002 (hạn 24/10/2026) |
| **Q-KNS-03** | Tenant KN: một cho cả hệ KN, hay một mỗi Trung tâm KN tỉnh? Dữ liệu KN cũ (`pf_extension`…) giữ ở tenant lưu ký hay chuyển chủ bằng sổ? | Số cấp phép hàng loạt, cách báo cáo liên tỉnh, công di chuyển | Một tenant cho hệ KN; Nút tỉnh là phạm vi; dữ liệu cũ giữ `tenant_id` |
| **Q-KNS-04** | Sau tách, KN còn **ghi thay** dữ liệu HTX (vẽ thửa, hộ, đếm máy) không, hay chỉ ghi cho HTX tạm do KN lập? | Quyết định port ghi có kiểm cấp phép (mới) | Ghi thay theo thỏa thuận, qua port ghi, có nhãn nguồn `kn` |
| **Q-KNS-05** | `pf/contact` (danh bạ) và `pf/price` (Báo giá) là của KN hay năng lực dùng chung (ADR-0054, ADR-0049)? | Chúng nằm ở KN hay nền tảng sau tách | Xếp vào KN tới khi có ứng dụng thứ hai dùng |
| **Q-KNS-06** | Thu thập MRV ở `apps/field-app` (vai `mrv_*` trong `app/KN/app.yaml`) thuộc KN hay HTX/MKG? | MRV là dữ liệu thương mại có chữ ký số (ADR-24) | Hỏi cùng ADR-24 |

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Tách vật lý ngay trước G1 (binary, realm, cụm) | Không kịp 15/12. Phải làm lại cấp phép, chuỗi băm, hợp đồng `_pub`, vận hành hai cụm. Chưa có căn cứ pháp lý đòi (Q-KNS-02) |
| Chỉ chuyển thư mục, không cổng kiểm | Ghép nối mới quay lại ngay. 40 gói đang chạy song song |
| Đổi tên schema `pf_extension` → `kn_extension` khi chuyển | Đổi chuỗi băm sổ, `ledger_idem`, cấp quyền, hợp đồng `_pub`, mà không có lợi ích kiểm chứng được |
| KN đọc HTX qua cổng tích hợp như MKG ngay | Đúng cho cụm riêng (KNSPLIT-5). Trong cùng cụm thì đọc tại chỗ có cấp phép (ADR-0030) rẻ hơn và đã kiểm thử |
| Coi KN là ứng dụng thuần trên nền tảng, không tách | Không đáp ứng yêu cầu của chủ sản phẩm. Phần KN trong module dùng chung làm nền tảng khó dùng cho ứng dụng khác |

## Hệ quả

- **Cổng mới.** `go run ./tools/knboundary` chạy trong `make gates` và CI. CI chạy với `-base origin/<nhánh đích>` để baseline chỉ co lại.
  - Mọi gói (không chỉ KNSPLIT) thêm phần riêng KN vào module dùng chung, hoặc thêm import KN vào nền tảng hay vỏ web, sẽ hỏng cổng.
  - Cách sửa là đăng ký qua port/registry, không thêm vào baseline.
- **Kiểm thử XFAIL.** Kiểm thử dữ liệu KNSPLIT-0 giữ 5 ca XFAIL tới KNSPLIT-4. Hết vi phạm thì ca báo XPASS và hỏng, để buộc cập nhật.
- **Tên giữ nguyên ở giai đoạn logic:** module, vai DB, schema, route.
- **Hồ sơ NĐ 331.** Báo cáo của cổng và kiểm thử là bằng chứng ranh giới cho W10.16.

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| KNSPLIT-0 (14/10/2026) | Kiểm kê [docs/kn/tach-he-thong/kiem-ke.md](../kn/tach-he-thong/kiem-ke.md); `tools/knboundary` (8 quy tắc, baseline 376 mục, `-list`/`-prune`/`-base`, kiểm thử trên repo mẫu và repo thật); `make gates`, `make gates-base`, CI; `migrations/kn_split_boundary_integration_test.go` (3 kiểm thử, 5 ca XFAIL) | — |
