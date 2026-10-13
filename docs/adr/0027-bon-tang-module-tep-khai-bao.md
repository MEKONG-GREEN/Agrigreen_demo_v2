# ADR-0027: Bốn tầng module khai trong tệp khai báo, ứng dụng là tổ hợp, module gọi nhau trong tiến trình

**Trạng thái:** Đề xuất — chờ MKG ký (A7) · **Ngày:** 04/10/2026 (gói W0.1) · **Phụ lục B v4.1:** mục 1, 2 · **Xung đột:** X-13 · **Thành phần:** mọi module `internal/{platform,mkg,vault}/*`, `cmd/*/assembly`, `tools/archlint` · **Liên quan:** [ADR-0001](0001-go-ngon-ngu-dich-vu.md), [ADR-0011](0011-module-nghiep-vu-cac-app.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), kiến trúc v4.1 §5, §7, §8, Phụ lục D

## Bối cảnh

Kiến trúc v4.1 (§5, §7) chia module thành bốn tầng:

| Tầng v4.1 | Ví dụ |
|---|---|
| Lõi `k/*` | `k/access`, `k/orgtree`, `k/ref`, `k/ledger`, `k/jobs`, `k/audit`, `k/ui-shell` |
| Năng lực dùng chung `cap/*` | `cap/masterdata`, `cap/workflow`, `cap/forms`, `cap/export`, `cap/analytics`, `cap/geo`, … |
| Nghiệp vụ `dom/*`, `mkg/*` | `dom/extension`, `dom/cooperative`, `dom/mechanization`, `mkg/fieldops`, … |
| Ứng dụng `app/*` | `app/KN`, `app/HTX`, `app/CGH`, `app/SIM`, `app/MKG`, `app/NH`, `app/PF` |

Mỗi module có tệp khai báo `module.yaml` (`kind`, `zones`, `depends` có phiên bản, `provides`, `permissions`, `shares`, `owns_schema`, `enable`). Mỗi ứng dụng có `app.yaml` liệt kê module và cấu hình. Phụ thuộc chỉ đi xuống (R-MOD-05), CI kiểm từ các tệp khai báo.

Repo hiện tại (bản đối chiếu §2.1):
- tổ chức theo **vùng** `internal/{platform,mkg,vault,shared}`, không theo tầng;
- khoảng 20 module nền tảng, 7 module mkg, `vault/identity`, khoảng 28 thư viện `shared/*`;
- chưa có `module.yaml`, `app.yaml`. Phụ thuộc chỉ thấy qua import `port/`;
- ứng dụng được ghép ở `cmd/<đơn vị>/assembly`, đăng ký lúc biên dịch, không dùng gói `plugin` (đã đúng §7.4);
- `archlint/agboundary` kiểm vùng và `port/`, không kiểm tầng.

Phụ lục D của v4.1 cho phép giữ tên `pf/*`, `mkg/*`, miễn là đưa mô hình v4.x vào `docs/` qua ADR.

KN v7.7 (D2) coi **phòng ban là module**: `pf_party.department` bật theo trạng thái, menu lấy từ API. R-MOD-08 của v4.1 lại bật module **theo đơn vị tham gia**. Hai khái niệm trùng tên "module" (X-13).

## Quyết định

### 1. Tầng là thuộc tính khai báo, không phải thư mục

- Mỗi module có `module.yaml` cạnh `module.go`. Trường `kind: kernel | capability | domain` mang tầng.
- **Giữ nguyên** thư mục `internal/platform/*`, `internal/mkg/*`, `internal/vault/*`, tên module, tên Go package và tên schema `pf_*`, `mkg_*`.
- Tên v4.1 (`k/*`, `cap/*`, `dom/*`) là **tên tham chiếu** trong tài liệu, ghi ở trường `aka` của `module.yaml`.
- Schema giữ quy ước repo `pf_<module>` (không đổi sang `<tầng>_<module>` của R-DB-01). View công bố là `pf_<module>_pub`.
- `internal/shared/*` là **thư viện** dùng chung, không phải module: không có `module.yaml`, không có schema riêng ngoài `shared`. Chúng giữ vai `k/ledger`, `k/jobs`.

### 2. Bảng ánh xạ tên (Phụ lục D)

| Module repo | Tên v4.1 | `kind` | Ghi chú |
|---|---|---|---|
| `pf/accounts`, `pf/apiclient` | `k/access` | kernel | `pf/apiclient` (định danh máy) về `k/access` ở W7.2 |
| `pf/party` | `k/orgtree` + phần cấp phép của `k/access` | kernel | Cây tổ chức, cấp phép chia sẻ (ADR-0020, ADR-0030). HTX, nông hộ chuyển sang `pf/cooperative` (W3.x) |
| `pf/kref` (mới, W2.5) | `k/ref` | kernel | Xem [ADR-0029](0029-k-ref-ban-chieu-loi-moi-cum.md) |
| `internal/shared/{ledger,outbox,inbox,idempotency,opsjobs,…}` | `k/ledger`, `k/jobs` | thư viện | Không có `module.yaml` |
| — | `k/audit`, `k/ui-shell` | — | Chưa có module. Nhật ký rải rác; shell là `apps/web-ops` |
| `pf/masterdata` | `cap/masterdata` | capability | Đơn vị hành chính có hiệu lực: ADR-0032 (W0.2) |
| `pf/notify`, `pf/content`, `pf/sync` | `cap/notify`, `cap/content`, `cap/sync` | capability | Cùng tên |
| `pf/geo` | `cap/geo` | capability | Hình học thửa chuyển sang `pf/cooperative` (W3.3) |
| `pf/reports` (+ `extension/reportcycle`) | — (`hosts: [cap/export, cap/import]`) | capability | `cap/forms` đã tách ở W6.2 (`pf/forms`). W6.6: `cap/export`, `cap/import` chưa tách — khai `hosts` ở mọi module đang cài, không còn là `aka` của `pf/reports` |
| `pf/forms` (mới, W6.2) | `cap/forms` | capability | Biểu theo kỳ, chốt kỳ kèm ảnh chụp đóng băng (pf 000121–000122) |
| `pf/metrics` | `cap/analytics` | capability | Chỉ đọc `_pub` (W6.3) |
| `pf/evidence` (mới, W6.5) — schema `pf_files` | `cap/evidence` | capability | Chuyển `pf_files` từ `pf/sync` (pf 000123) |
| `pf/workflow` (mới, W6.1) | `cap/workflow` | capability | Duyệt bốn mắt dùng chung (pf 000118) |
| — | `cap/task` | — | Chưa có module. `cap/task` khai `hosts` ở `pf/extension` (W6.6) |
| `pf/price` (mới, W11.71) | `cap/price` | capability | [ADR-0049](0049-cap-price-pf-price.md); khung pf 000138, nghiệp vụ Báo giá ở W11.72–W11.76 |
| `pf/contact` (mới, W11.23) | `cap/directory` | capability | [ADR-0053](0053-cap-directory-pf-contact.md); Contact FN-13 dùng chung (pf 20261013165159), PII qua thẻ danh bạ của kho định danh |
| `pf/hrm` (mới, W11.21) | `dom/hrm` | domain | [ADR-0050](0050-dom-hrm-pf-hrm.md); khung + danh mục nhân sự pf 000147, hồ sơ và nghiệp vụ HCTH ở W11.22–W11.28 |
| `pf/extdata` | — | chốt ở W1.1 | Không có tên v4.1 tương ứng |
| `pf/metering` | `dom/metering` | domain | ADR-0023 |
| `pf/extension` | `dom/extension` | domain | |
| `pf/cooperative` (mới, W3.2) gom từ `party`, `geo`, `cropcycle` | `dom/cooperative` | domain | Chủ duy nhất HTX, hộ, thửa, vụ |
| `pf/cropcycle` | phần của `dom/cooperative` | domain | Vụ chuyển sang `pf/cooperative` (W3.3); van còn lại đến G2 |
| `pf/mechanization`, `pf/waterway`, `pf/mrv` | `dom/mechanization`, `dom/waterway`, `dom/mrv` | domain | |
| `pf/simulation` | `dom/hubsim` | domain | |
| `mkg/*` (7 module) | `mkg/*` | domain | `zones: [internal]` |
| `vault/identity` | (kho định danh hộ) | kernel | Cụm riêng (ADR-0021) |

Bảng là điểm khởi đầu. `kind` cuối cùng chốt khi viết `module.yaml` ở W1.1. Đổi `kind` sau đó cần sửa ADR này.

### 3. Ứng dụng là tổ hợp, đơn vị triển khai là nhị phân

- **Ứng dụng** (`app/<KN|HTX|CGH|SIM|MKG|NH|PF>/app.yaml`) là sản phẩm người dùng thấy: danh sách module + cấu hình theo `config_schema` + vai seed + trang đích.
- **Đơn vị triển khai** là một nhị phân trong `cmd/*` (`api-platform`, `worker`, `api-internal`, …). Một đơn vị có thể phục vụ nhiều ứng dụng.
- CI kiểm: tập module ghép trong `cmd/*/assembly` ⊆ hợp các `app.yaml` mà đơn vị đó phục vụ (W1.3).
- Bản lite cho khách là **cấu hình build**, không phải nhánh mã (§7.4).

### 4. Module gọi nhau trong tiến trình (B2)

- Cùng vùng: gọi qua `port/` đã khai trong `depends`, **trong tiến trình**. Không tách microservice theo module.
- Khác vùng: chỉ qua API hoặc sự kiện (giữ R-03, ADR-0008).
- Registry lúc biên dịch; không dùng `plugin` của Go (đã có).
- Ranh giới module **chống nhầm lẫn, không chống mã độc**. Lớp chặn thứ hai ở DB là vai `r_<module>` (ADR-0028).

### 5. Quy tắc kiểm trong CI

- Đồ thị `depends` không chu trình, chỉ đi xuống theo `kind`: kernel ← capability ← domain ← ứng dụng (R-MOD-05).
- Import `port/` của module khác phải có trong `depends`; ràng buộc phiên bản thỏa.
- `owns_schema` khớp schema mà migration tạo.
- Module `kind: capability` không so sánh tên ứng dụng (R-MOD-04, W9.1).
- `agboundary` hiện có giữ nguyên; analyzer mới `agmodgraph` bổ sung (W1.2).

### 6. Phòng ban KN là cấu hình của `dom/extension` (X-13)

Giữ cả hai cơ chế, tách nghĩa:
- **Bật module theo đơn vị** (R-MOD-08) là cơ chế của v4.1: sổ `module_enablement` (đơn vị × module × trạng thái). Module chưa bật → `module_not_enabled`, quyền liên quan không cấp được, menu không hiện (W1.4).
- **Phòng ban** (Trồng trọt, Chăn nuôi, …) **không phải module**. Đó là cấu hình bên trong `dom/extension` (`pf/extension`) của một đơn vị: không có `module.yaml`, không có schema riêng, không có port riêng.
- `pf_party.department` và trạng thái bật phòng ban giữ nguyên như dữ liệu cấu hình. Menu `web-ops` lọc theo **cả hai**: module đã bật cho đơn vị, rồi phòng ban đã bật trong `dom/extension`.
- Khi một phòng ban cần chức năng dùng chung (biểu mẫu, nhiệm vụ), nó dùng `cap/*` qua cấu hình của `dom/extension`, không tạo module mới.

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Đổi thư mục sang `internal/{k,cap,dom}/*` và đổi tên module, schema | Chạm mọi import, migration, vai DB, chart, tài liệu G1–G5. Rủi ro cao, không thêm khả năng nào. Phụ lục D cho giữ tên |
| Không khai báo, chỉ dựa vào import `port/` | Không kiểm được tầng, phiên bản, quyền, `shares`, bật theo đơn vị. Không đạt R-MOD-05, R-MOD-08 |
| Microservice theo module | Trái B2. Tăng độ trễ, giao dịch phân tán, vận hành nặng cho đội nhỏ |
| Phòng ban là module thật (có `module.yaml`) | Phòng ban chỉ có ở KN, không đạt quy tắc ba (R-MOD-09). Làm loãng nghĩa "module" và đồ thị phụ thuộc |

## Hệ quả

- Khoảng 28 tệp `module.yaml` mới. Mỗi thay đổi `depends`/`provides`/`permissions` từ nay đi qua review như thay đổi hợp đồng.
- Tài liệu dùng hai tên cho một module trong giai đoạn chuyển. Bảng ở mục 2 là tham chiếu chuẩn.
- ADR-0011 (module nghiệp vụ cho các app) giữ nguyên nội dung; module nó thêm được gán `kind` theo bảng trên.
- `pf/party` tạm giữ cả cây tổ chức và HTX/hộ cho đến W3.3. Trong thời gian đó `kind: kernel` của nó rộng hơn thực tế; analyzer cho phép ngoại lệ có tên, hết hạn ở W3.3.
- Không đổi hành vi chạy. Không có migration.

## Hiện thực

| Gói | Việc |
|---|---|
| W1.1 | `module.yaml` cho mọi module, gói `internal/shared/modreg` đọc và kiểm lược đồ, gán `kind` |
| W1.2 | Analyzer `agmodgraph` trong `tools/archlint`, vào CI |
| W1.3 | `app/*/app.yaml`, kiểm `cmd/*/assembly` ⊆ hợp `app.yaml` |
| W1.4 | Bật module theo đơn vị (`module_enablement`, pf 000088), menu lọc |
| W3.1 | Thiết kế ranh giới `pf/cooperative`: kiểm kê bảng, port, `module.yaml`, kế hoạch W3.3–W3.4 — [docs/design/cooperative.md](../design/cooperative.md) |
| W3.2 | Module `pf/cooperative` (`kind: domain`) — xong 08/10/2026: mặt tiền không DB, đỉnh lá của đồ thị (nguồn cũ nối ở gốc lắp ráp, `cmd/internal/coopwire`); chỉ module domain import `cooperativeport` (R-MOD-05), tầng dưới dùng interface tự khai ([ghi chú](../design/cooperative.md#16-ghi-chú-hiện-thực-w32)) |
| W3.3 | Chuyển schema `pf_coop` (pf 000131–000133) — xong 09/10/2026: `pf/cooperative` sở hữu `pf_coop` (`owns_schema`, vai `r_cooperative`), mã vụ/thửa/hộ chuyển vào `cooperative/{season,plot,member}`; `party`, `geo`, `cropcycle` lên 2.0.0 (bỏ port cũ); module tầng dưới khai interface của mình (`partyport.CoopProfiles`, `geoport.PlotGeometry`/`MapLayers`), gốc lắp ráp nối (R-MOD-05); `agmodgraph` cho phép đúng một dạng sửa đối tượng của schema khác — `ALTER … SET SCHEMA <schema của mình>`; tên sự kiện v1 giữ tiền tố cũ, `modreg` suy module phát từ `provides.events` khi tiền tố không khai ([ghi chú](../design/cooperative.md#17-ghi-chú-hiện-thực-w33)) |
| W6.1 | `pf/workflow` (`cap/workflow`, capability, pf 000118). `pf/masterdata` (capability) import `workflow/port` trực tiếp; `pf/party` (kernel) KHÔNG được import tầng trên (mục 5) nên khai interface của mình (`party/port.FourEyes`) và gốc lắp ráp `cmd/api-platform/assembly` nối cài đặt của workflow — mẫu đảo phụ thuộc cho module lõi cần năng lực `cap/*`, không thêm ngoại lệ `agmodgraph` |
| W6.2 | `pf/forms` (`cap/forms`, capability, pf 000121–000122) import `workflow/port` trực tiếp (capability → capability, cùng tầng) cho rà soát bốn mắt; `pf/extension` (domain) import `forms/port` trực tiếp cho ghi song song chốt/mở lại kỳ KN (đi xuống tầng, không đảo phụ thuộc). Cấu hình theo bên dùng (§7.2 `cap/forms: {hierarchy, publish_by, close_by}`) là `forms/port.Profile` do gốc lắp ráp truyền (`assembly.KNProfile`) — `app.yaml` chưa mang cấu hình module. `aka cap/forms` chuyển từ `pf/reports` sang `pf/forms`. Không thêm ngoại lệ `agmodgraph` |
| W6.3 | View công bố `pf_<module>_pub` (pf 000124): chỉ thị `//ag:pubview <schema>_pub.<tên> v<N>` trong `port/` của module chủ → `provides.publications` dạng `<schema>_pub.<tên>/v<N>` (view thật `v<N>_<tên>`); `modgen` gán `<cụm>_<module>_pub` cho chủ của `<cụm>_<module>`; `modreg` kiểm schema của view thuộc `owns_schema`; `agmodgraph` cho migration công bố `<cụm>_pub_*` tạo view trong schema `_pub` của nhiều module (không đụng schema nội bộ). `pf/metrics` có `aka: [cap/analytics]` — giữ tên repo |
| W6.5 | `pf/evidence` (`cap/evidence`, capability, pf 000123) — **module mới** thay vì mở rộng `pf/sync`: bảng mục 2 gán `pf_files` cho `cap/evidence`, `Open(purpose)` là năng lực dùng chung. Sở hữu `pf_files` (mã SQL chuyển khỏi `pf/sync`, `pf/sync` giữ route `/v1/files` và gọi `evidence/port.Files`); bộ ba R-DB-07 NULL được trên `pf_files.file`, sổ `pf_files.access_log`. Module chủ phân giải chủ thể qua `evidence/port.Resolver` — interface bên tiêu thụ, gốc lắp ráp nối `cooperativeport.Resolve` (capability không import domain, mẫu W6.1); phạm vi/đồng ý cũng là interface tự khai để không thêm cạnh vào chu trình `pf/party ↔ pf/sync` |
| W6.6 | `cap/import`, `cap/task`, `cap/export` **chưa tách** (R-MOD-09): trường tay mới `hosts` trong `module.yaml` khai năng lực `cap/*` chưa tách mà module đang cài (`modreg`: dạng `cap/<tên>`, không trùng `aka`, lỗi nếu năng lực đã có module mang `aka` đó; `Registry.Hosts`). Kiểm kê: `cap/import` 8 module (đọc tệp đã là thư viện `shared/tabular`, phần còn lại là ánh xạ cột của module chủ), `cap/export` 3 việc xuất bất đồng bộ (`pf/reports`, `pf/mrv`, `pf/apiclient` — khung chung đã ở `shared/outbox`/`objstore`, khác nhau ở lõi DLP/băm/ngưỡng k) + 5 tải đồng bộ, `cap/task` 1 (`pf/extension`). Tách `cap/export` cần schema mới và dời ba sổ chỉ ghi thêm — không làm trong gói không migration. Điều kiện kích hoạt tách ghi ở [modules.md](../dev/modules.md#năng-lực-chưa-tách-capimport-captask-capexport-w66) |
| W9.1 | Lint cấm so sánh tên ứng dụng trong module `capability` |
| W11.05 | `cap/forms` tổng quát đơn vị lá (pf 000140): sổ phụ `pf_forms.period_spec` (loại đơn vị lá, đơn vị cha, giới hạn lần gửi, khóa kỳ), `held_submission` (Chờ mở kỳ), sự kiện `cancelled`; `forms/port.Profile` thêm `UnitKinds`, `Departments`, `MaxSubmissions`, khóa vai `vai@phòng_ban`. Gốc lắp ráp truyền ba Profile KN (`kn`, `kn_cnts`, `kn_khtc`). Không đổi đồ thị module, không ngoại lệ mới |

## Liên quan

- Kiến trúc v4.1: §5 (bốn tầng, ma trận 5.1), §7.1–7.4 (tệp khai báo, quy tắc R-MOD-01…10), §8 (cấu trúc repo), Phụ lục B mục 1–2, Phụ lục D.
- Bản đối chiếu: §2.1, §3.6, X-13.
- [ADR-0001](0001-go-ngon-ngu-dich-vu.md) (Go, một khối triển khai), [ADR-0011](0011-module-nghiep-vu-cac-app.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md) (vai DB theo module), [ADR-0029](0029-k-ref-ban-chieu-loi-moi-cum.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md) (`shares`).
