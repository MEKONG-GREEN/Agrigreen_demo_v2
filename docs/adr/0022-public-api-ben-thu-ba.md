# ADR-0022: Public API cho bên thứ ba — đơn vị triển khai riêng, khóa API lưu băm, scope, quota

**Trạng thái:** Đề xuất · Sửa đổi bởi [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md) (§1 thành `cmd/integration-gateway`, §8 URL ký cho ảnh chụp), [ADR-0035](0035-cap-phep-tong-hop-theo-xa-nguong-k.md) (§9 k theo phiên bản tập dữ liệu) (đề xuất) · **Ngày:** 01/10/2026 (G5-0) · **Thành phần:** `cmd/api-public` (mới), `pf/apiclient` (mới), `pf/metering` ([ADR-0023](0023-so-do-luong-su-dung-metering.md)), điểm kiểm soát xuất ([ADR-0024](0024-diem-kiem-soat-xuat-du-lieu-dlp.md)), `pf/party` (loại cấp quyền `dp_*`, [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md)), `contracts/openapi/public.yaml`, `contracts/dataproduct/*` · **Quy ước:** [g5-conventions.md](../dev/g5-conventions.md)

## Bối cảnh

- Mục 11 của kiến trúc có bốn bề mặt API. Bề mặt Public (đối tác) ở `partner.<domain>` xác thực bằng **khóa API lưu băm, scope, quota**, **tắt ở ĐTK 1**, và **mọi lời gọi ghi vào sổ đo lường**. Phụ lục A, dòng G5: "Phase K; `pf/metering` và Public API bật với bên thứ ba đầu tiên". Mục 2: người dùng ĐTK 2 gồm "bên thứ ba, đơn vị thẩm định".
- R-01, RB-5: chuyển giai đoạn chỉ là đổi cấu hình và số bản sao, không sửa mã. Vì vậy Public API phải có mã và kiểm thử trước Phase K, và mặc định ở trạng thái tắt.
- **Không nhầm** với bề mặt `/v1/partner/*` của ADR-0020. Bề mặt đó nằm trên **Application API** (OIDC, vai `mkg_sync`), là cửa nền tảng → MKG cho hệ nội bộ của MKG, không dành cho bên thứ ba.
- BRD và backlog **không có** yêu cầu nào về Public API, khóa API hay thu phí. Prototype `mekong-green` có:
  - đối tác loại `customer`/`vendor`/`vvb`/`buyer` (nhà máy VFT, EVN, VVB-SGS, quỹ mua tín chỉ);
  - vai `vvb_auditor` chỉ đọc MRV/GIS/báo cáo;
  - gói "Data Licensing" dự báo sản lượng cho nhà máy, giá cố định 450 000 000 đ (`SUB-DATA`).
  
  Prototype không có khóa API, sổ sử dụng hay ngưỡng gom nhóm. Backlog KN US-MRV-05 ghi chuyên gia thẩm định chỉ xem, không sửa, mọi lần truy cập có nhật ký.

## Quyết định

### 1. Đơn vị triển khai riêng `cmd/api-public`

Public API là **một tiến trình riêng** `cmd/api-public` (Deployment `api-public` ở namespace `agrigreen-pf`). Nó **không** là listener thứ ba của `api-platform`.

| Tiêu chí | Tiến trình riêng (chọn) | Listener thứ ba trong `api-platform` |
|---|---|---|
| Bề mặt tấn công | Nhị phân chỉ chứa route Public, không có handler Application/Admin, không có client kho định danh | Mọi handler Application + Admin nằm trong tiến trình nghe từ Internet qua `partner.<domain>` |
| Phân vùng mạng (ADR-0008) | Một luồng riêng Ingress → `api-public` | Luồng Ingress → `api-platform` phải mở thêm cổng |
| Bật/tắt theo R-01 | Số bản sao 0 → N + host ingress + biến bật | Biến môi trường trong một tiến trình dùng chung |
| Bí mật | Pepper khóa API chỉ có ở `api-public` (và Admin API khi cấp khóa) | Pepper nằm cạnh mọi bí mật khác của `api-platform` |
| Co giãn | Theo tải bên thứ ba, độc lập | Kéo theo Application/Admin |

Cấu hình:

| Biến | Ý nghĩa | ĐTK 1 | Phase K |
|---|---|---|---|
| `AG_PUBLIC_API_ENABLED` | `1` thì nghe; khác `1` thì tiến trình **thoát lỗi** ngay khi khởi động (đóng khi lỗi, không lặng lẽ mở) | không đặt, số bản sao 0 | `1` |
| `AG_PUBLIC_HTTP_ADDR` | mặc định `:8085` | — | `:8085` |
| `AG_PUBLIC_OPERATOR_TENANT` | tenant **đơn vị vận hành** Public API, chủ sổ khách hàng API và sổ đo lường (mục 3) | — | bắt buộc |
| `AG_APIKEY_PEPPER_V<n>`, `AG_APIKEY_PEPPER_CURRENT` | pepper HMAC theo phiên bản, ≥ 32 byte, từ trình quản lý bí mật (mục 4) | — | bắt buộc |
| `AG_PUBLIC_OIDC_ISSUER`, `AG_PUBLIC_OIDC_AUDIENCE` | tùy chọn: client credentials cho đơn vị thẩm định (mục 5) | — | tùy khách |
| `AG_VALKEY_URL` | tùy chọn: giới hạn tần suất dùng chung giữa các bản sao (mục 7) | — | khuyến nghị |
| `AG_EGRESS_MODE` | `observe` / `enforce` ([ADR-0024](0024-diem-kiem-soat-xuat-du-lieu-dlp.md)). Public API luôn đóng khi lỗi, không phụ thuộc biến này | `observe` | `enforce` |

Cưỡng chế bằng archlint:
- `cmd/api-public` là gốc lắp ráp của vùng nền tảng, **không** được import `internal/mkg/**` (R-03).
- `cmd/api-public` **không** được import kho định danh, kể cả `internal/vault/identity/client`. Bên thứ ba không bao giờ thấy dữ liệu cá nhân (RB-3).

Bề mặt **quản trị** của Public API nằm trên **Admin API** của `api-platform` (MFA bắt buộc), gồm đăng ký khách hàng API, cấp/xoay/thu hồi khóa, đăng ký sản phẩm dữ liệu, gói hạn mức, bảng kê sử dụng. Cổng tự phục vụ cho bên thứ ba **ngoài G5**.

### 2. Module

- **`pf/apiclient`** (schema `pf_apiclient`, vai DB `apiclient_app`):
  - khách hàng API, khóa API (sổ), scope;
  - đăng ký sản phẩm dữ liệu và gói hạn mức của khách hàng;
  - việc xuất bất đồng bộ;
  - handler của sản phẩm dữ liệu. Handler đọc module chủ dữ liệu **qua `port/`**, bằng pool vai của module đó.
- **`pf/metering`** (schema `pf_metering`, vai DB `metering_app`):
  - sổ sử dụng, bộ đếm hạn mức;
  - sổ xuất dữ liệu (điểm kiểm soát DLP);
  - bảng kê theo kỳ.
  
  Xem ADR-0023 và ADR-0024.
- `api-public` mở thêm pool `public_api`. Pool này chỉ có bảng vận hành `shared` (idempotency, outbox), như `platform_api`.

### 3. Mô hình tenant

Nguyên tắc tenant = tổ chức sở hữu dữ liệu ([ADR-0009](0009-mo-hinh-tenant.md)) giữ nguyên:

| Đối tượng | Tenant |
|---|---|
| Bên thứ ba: nhà máy, bên mua tín chỉ, đơn vị thẩm định… | một **tenant riêng** (bên tham gia ở `pf/party`), cấp theo [ADR-0025](0025-ho-so-cap-phat-phase-k.md). Có thể chưa có người dùng OIDC nào |
| Khách hàng API, khóa API, sổ sử dụng, bảng kê | tenant **đơn vị vận hành** (`AG_PUBLIC_OPERATOR_TENANT`). Đơn vị này đăng ký khách hàng, phát hành bảng kê và lập hóa đơn. Mỗi dòng mang `client_tenant_id` = tenant bên thứ ba |
| Dữ liệu được phục vụ | tenant **chủ dữ liệu** (bên cấp), ví dụ tenant chương trình |

Bên thứ ba chỉ đọc được dữ liệu của bên cấp khi có **cấp quyền chia sẻ** theo ADR-0020:
- bên cấp → tenant bên thứ ba;
- loại `dp_<sản phẩm>`;
- người đề xuất ≠ người duyệt;
- có hạn, thu hồi được.

Tham số `grantor` của route chỉ **chọn** bên cấp, như ADR-0020. Tenant của bên gọi luôn lấy từ bản ghi khóa (tương đương R-16 cho khóa API), không bao giờ từ path, query hay body.

`api-public` kiểm cấp quyền bằng cổng mới `party/port.GrantLookup`. Cổng này chỉ đọc, không cần vai OIDC `mkg_sync`. Sau khi kiểm xong, `api-public` đọc dữ liệu bằng `tenant.WithTenantTx(…, grantor, …)` trên pool vai của module chủ dữ liệu. Không có vai BYPASSRLS.

### 4. Khóa API

- **Dạng:** `agk_<prefix>_<secret>`.
  - `prefix`: 12 ký tự base32 thường, sinh ngẫu nhiên, duy nhất. Dùng để tra cứu và để ghi log.
  - `secret`: 32 byte ngẫu nhiên, base64url không đệm.
  - Gửi trong header `X-API-Key`.
- **Lưu:** chỉ `key_prefix` và `key_hmac = HMAC-SHA-256(pepper_v, khóa đầy đủ)` kèm `pepper_version`. So khớp bằng thời gian hằng.
  - Chọn HMAC có pepper thay vì argon2id: bí mật 256 bit ngẫu nhiên nên tấn công vét cạn ngoại tuyến không khả thi kể cả khi lộ bảng. Còn argon2id ở **mỗi lời gọi** tốn hàng chục mili giây CPU và mở đường tấn công từ chối dịch vụ.
  - Pepper nằm ở trình quản lý bí mật (KMS trong lãnh thổ, ADR-0021), không bao giờ ở DB.
  - Xoay pepper: thêm `AG_APIKEY_PEPPER_V<n+1>` và đặt `CURRENT`. Khóa cũ vẫn kiểm được theo `pepper_version` của chính nó, khóa mới dùng phiên bản mới.
- **Hiện bí mật đúng một lần** trong phản hồi tạo khóa (`Cache-Control: no-store`). Bí mật không bao giờ được log, lưu, đưa vào lỗi, URL hay sự kiện (R-14, R-92). Log chỉ có `key_prefix`.
- **Vòng đời là sổ** `pf_apiclient.api_key_event` (RB-1):
  - các sự kiện: `issued`, `rotated` (khóa mới + hạn chồng lấn của khóa cũ, mặc định 7 ngày), `revoked` (bắt buộc lý do, hiệu lực ngay ở lời gọi kế tiếp), `expired`;
  - tối đa **2** khóa hiệu lực mỗi khách hàng để chồng lấn lúc xoay;
  - hạn mặc định 365 ngày.

  Không có cột trạng thái nào bị UPDATE: trạng thái đọc từ bảng chiếu.
- Khóa thuộc **khách hàng API**, không thuộc người. Khách hàng có đầu mối liên hệ `contact_ref`. Tên và điện thoại của đầu mối ở kho định danh hoặc tài khoản (pf/accounts), không ở `pf_apiclient`.

### 5. Xác thực mạnh hơn cho đơn vị thẩm định (tùy chọn)

Khóa API là phương thức **bắt buộc có**. Hai phương thức bổ sung chỉ bật khi khách yêu cầu (`auth_methods` của khách hàng):

- **`oauth_client_credentials`:** client bí mật riêng cho từng đơn vị thẩm định ở Keycloak trong lãnh thổ. JWT có audience `AG_PUBLIC_OIDC_AUDIENCE`, backend tự kiểm issuer, audience, chữ ký (R-54). `azp` ánh xạ về khách hàng API. Scope và đo lường giống hệt khóa API.
- **`mtls`:** ghim vân tay chứng thư client (`mtls_cert_sha256`). Cần ingress chuyển tiếp hoặc kết thúc mTLS cho `partner.<domain>`, nên **để sau** (câu hỏi hạ tầng). Cổng đã có hằng `AuthMTLS`, chưa hiện thực.

### 6. Scope và quyền truy cập hiệu lực

- **Scope:**
  - `dp:<sản phẩm>:read`: đọc;
  - `dp:<sản phẩm>:export`: xuất bất đồng bộ;
  - `usage:read`: bảng kê sử dụng của chính mình.
- Scope được cấp qua **đăng ký sản phẩm**. `api_client_admin` đề xuất, `api_client_approver` duyệt, người duyệt ≠ người đề xuất (R-56, kiểm ở `app/` **và** CHECK DB).
- Một lời gọi được phục vụ khi **đủ cả bốn** điều kiện:
  - khóa hiệu lực;
  - có scope;
  - có cấp quyền `dp_<sản phẩm>` đang hiệu lực từ bên cấp;
  - sản phẩm phục vụ được: hợp đồng dữ liệu `approved`, hoặc `draft` ngoài production (mục 9).
  
  Thiếu một điều kiện → `403 apiclient.forbidden` (không nói thiếu điều kiện nào), trừ khóa sai → `401 apiclient.unauthenticated`.

### 7. Giới hạn tần suất và hạn mức (R-55)

- **Gateway (thô, không thẩm quyền):** middleware của Ingress cho host `partner.<domain>` giới hạn theo IP nguồn (đề xuất 20 rps, burst 40) và giới hạn kích thước thân. Lớp này chỉ để chống ngập, theo R-55.
- **Backend, giới hạn tần suất:** token bucket theo khách hàng (mỗi phút).
  - Có `AG_VALKEY_URL` thì dùng Valkey (mục 4: cache và rate limit, **không** phải nguồn sự thật).
  - Không có thì dùng bộ đếm trong tiến trình, chia hạn mức cho số bản sao.
  
  Vượt → `429 apiclient.rate_limited`.
- **Backend, hạn mức (thẩm quyền):** đơn vị sử dụng theo (khách hàng × sản phẩm) theo **ngày** và **tháng**. Nguồn là bộ đếm `pf_metering`, cập nhật **cùng giao dịch** với dòng sổ sử dụng. Vượt → `429 metering.quota_exceeded` + `Retry-After` tới đầu cửa sổ kế tiếp.

  Gói hạn mức là tham số có phiên bản. Giá trị mặc định là "tạm, chờ khách".
- **Header mọi phản hồi:** `RateLimit-Limit`, `RateLimit-Remaining`, `RateLimit-Reset` (giây), `AG-Quota-Remaining` (đơn vị còn lại trong tháng). Có thêm `Retry-After` khi 429.

### 8. Phiên bản, idempotency, bất đồng bộ, phân trang

- **R-50:** đường dẫn `/v1/...`. Thay đổi phá vỡ → `/v2/...`, chạy song song tối thiểu một kỳ phát hành.
  - Mỗi phản hồi dữ liệu có header `AG-Data-Contract: <sản phẩm>.v<N>`.
  - Hợp đồng dữ liệu đổi phá vỡ → phiên bản hợp đồng mới. Bản cũ trả thêm `Deprecation` và `Sunset`.
- **R-51:** mọi POST tạo tài nguyên (yêu cầu xuất) bắt buộc `Idempotency-Key`, thiếu → `400`. Khóa lưu 48 giờ theo (`client_tenant_id`, khóa) qua `shared/idempotency`. Gọi lặp trả **đúng phản hồi đầu**, không ghi thêm đơn vị tính phí: dòng sổ của lần lặp có `units = 0` và `replayed = true`.
- **R-52:** việc quá 2 giây (xuất theo vụ/tỉnh, gói lớn) → `202 Accepted` + `Location: /v1/jobs/{id}`, theo mẫu mục 12, ảnh chụp đầu vào bất biến.
  - Tệp kết quả tải qua `GET /v1/jobs/{id}/file`, phát luồng qua `api-public`: byte được đo và đi qua điểm kiểm soát.
  - **Không** cấp URL có chữ ký của kho đối tượng cho bên thứ ba: URL đó rò được và vòng qua sổ đo lường.
- **R-53:** phân trang bằng cursor. **R-15/R-63:** lỗi RFC 9457 kèm `trace_id`.

### 9. Hợp đồng dữ liệu (mặc định từ chối)

Mỗi sản phẩm dữ liệu là một tệp `contracts/dataproduct/<tên>.json`, gói Go `contracts/dataproduct`. Hợp đồng gồm:
- trường kiểu hóa, có phiên bản;
- người đề xuất ≠ người phê duyệt;
- mục đích;
- loại cấp quyền `dp_<tên>`, scope;
- ngưỡng gom nhóm k nếu là dữ liệu tổng hợp;
- `personal_data: false` (bắt buộc).

Kiểm khi build và khi chạy:
- **`contractcheck`:**
  - struct `//ag:dataproduct <tên>` chỉ có trường của hợp đồng, đúng kiểu;
  - mỗi sản phẩm có tag cùng tên trong `contracts/openapi/public.yaml`.
- **Lúc chạy:** handler gọi `dataproduct.Get(tên)`, rồi `Validate` từng phần tử trước khi trả. Khóa lạ → không trả, `500 apiclient.contract_violation` và cảnh báo.

Hợp đồng `status: draft` chỉ phục vụ ngoài production. Production chỉ phục vụ `approved`.

## Hệ quả

- Ở ĐTK 1, `api-public` có mã và kiểm thử nhưng số bản sao 0, không có host ingress, `AG_PUBLIC_API_ENABLED` không đặt. Phase K bật bằng cấu hình (ADR-0025).
- Bên thứ ba là tenant, nên cấp quyền chia sẻ, thu hồi và sổ đọc của ADR-0020 dùng lại nguyên vẹn. Bên cấp thấy mọi lần đọc của bên thứ ba ở `partner_read_log` và ở sổ xuất `pf_metering.egress_event` (ADR-0024).
- Mỗi lời gọi đã xác thực tốn một giao dịch ghi `pf_metering` trước khi trả byte. Ước lượng ở ĐTK 2: dưới 50 rps cho bên thứ ba đầu tiên. Đo ở diễn tập tải G5.
- **Việc mở, chờ khách:**
  - bên thứ ba đầu tiên là ai, và sản phẩm nào;
  - đơn vị vận hành là pháp nhân nào;
  - gói hạn mức, đơn giá;
  - k (C-3);
  - đơn vị thẩm định có cần client credentials hoặc mTLS không;
  - DPA với từng bên thứ ba.

  Xem g5-conventions §10.
