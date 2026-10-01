# ADR-0025: Hồ sơ cấp phát Phase K — cấu hình, số bản sao, cấp tenant bên thứ ba, quyền của đơn vị thẩm định

**Trạng thái:** Đề xuất · **Ngày:** 01/10/2026 (G5-0) · **Thành phần:** chart `deploy/k8s/charts/agrigreen` (lớp giá trị Phase K — việc của agent hạ tầng G5), `cmd/api-public`, `pf/apiclient`, `pf/metering`, `pf/party`, realm Keycloak · **Liên quan:** R-01, RB-5, mục 2 và Phụ lục A của kiến trúc, [ADR-0008](0008-phan-vung-mang.md), [ADR-0021](0021-ha-tang-kubernetes-g4.md), [ADR-0022](0022-public-api-ben-thu-ba.md)…[ADR-0024](0024-diem-kiem-soat-xuat-du-lieu-dlp.md)

## Bối cảnh

Mục 2 của kiến trúc: "Phase K không phải điểm thiết kế — nó là trạng thái cấp phát của ĐTK 2". R-01: chuyển giai đoạn là **thay cấu hình và số bản sao, không sửa mã**. G5 (Q4/2028) bật Phase K với bên thứ ba đầu tiên. G6 (Q1/2029) chuyển cấu hình Phase 1, cũng không sửa mã.

## Quyết định

### 1. Hồ sơ cấu hình

Agent hạ tầng G5 tạo **một lớp giá trị** `values-phasek.yaml`, chồng lên `values-prod.yaml`. Lớp này chỉ chứa những thay đổi sau. Mọi thay đổi khác là dấu hiệu ranh giới module sai (R-01).

| Hạng mục | ĐTK 1 / G4 | Phase K |
|---|---|---|
| `api-public` số bản sao | 0 | 2 (HA, `PodDisruptionBudget` minAvailable 1) |
| `AG_PUBLIC_API_ENABLED` | không đặt | `1` |
| Host ingress `partner.<domain>` | không có | có, TLS, middleware giới hạn theo IP (ADR-0022 mục 7), giới hạn thân 1 MiB |
| NetworkPolicy | — | **F08** Ingress → `api-public` 8085 · **F19** `api-public` → PgBouncer cụm pf 5432 · Prometheus → `api-public` cổng số đo (mở rộng F53) |
| Luồng **cấm** (`check_flows.py` thêm) | — | `api-public` → `identity-vault`, → RabbitMQ, → DB/PgBouncer mkg/ts, → Internet; Internet → `api-platform` qua host `partner.<domain>` |
| Bí mật (ExternalSecret, KMS trong lãnh thổ) | — | `AG_APIKEY_PEPPER_V1`, `AG_APIKEY_PEPPER_CURRENT=1`, mật khẩu vai `public_api`, `apiclient_app`, `metering_app`, và vai đọc của module chủ dữ liệu mà `api-public` mở (`cropcycle_app`, `geo_app`, `mrv_app`, `masterdata_app`, `party_app`) |
| `AG_PUBLIC_OPERATOR_TENANT` | — | tenant đơn vị vận hành (chờ khách) |
| `AG_EGRESS_MODE` (`api-platform`, `worker`) | `observe` | `enforce` |
| `AG_VALKEY_URL` | — | khuyến nghị: Valkey trong vùng dữ liệu, luồng mới từ `api-public` |
| Consumer/job `worker` | — | thêm `pf.metering.statement`, `pf.apiclient.export`, `pf.notify.egress_anomaly` (đăng ký trong `assembly.Queues`, luôn có mã, chọn bằng cấu hình như G4) |
| Keycloak | — | client `agrigreen-public` (audience) và client bí mật cho từng đơn vị thẩm định, **chỉ khi** khách chọn `oauth_client_credentials` |
| Luật Prometheus | — | `agrigreen-egress.rules.yml`: bất thường xuất, checkpoint lỗi, metering lỗi, tỷ lệ 429/5xx của `api-public` |

ĐTK 1 và G4 giữ `api-public` ở 0 bản sao. Ảnh image vẫn được build và ký như mọi đơn vị khác.

### 2. Cấp tenant và khách hàng API cho bên thứ ba (thủ tục, không sửa mã)

Runbook `docs/runbooks/public-api-onboarding.md` (agent vận hành G5 viết) đi theo thứ tự:

0. **Pháp lý (ngoài hệ thống).** Hợp đồng dữ liệu hoặc DPA giữa chủ dữ liệu, đơn vị vận hành và bên thứ ba: mục đích, sản phẩm, thời hạn, điều khoản tái chia sẻ. Ghi số hợp đồng vào mục đích của cấp quyền.
1. **Tenant bên thứ ba.** Cấp như đã cấp tenant MKG ở ADR-0020:
   - tenant UUIDv7 mới, tổ chức gốc ở `pf/party`;
   - nếu bên thứ ba có người dùng: tài khoản Keycloak mang `tenant_id` qua `pf/accounts`.
   
   Bên thứ ba chỉ gọi API thì không cần người dùng.
2. **Cấp quyền chia sẻ của chủ dữ liệu.** `platform_admin` của bên cấp đề xuất `POST /v1/share-grants` với `grantee` = tenant bên thứ ba, `kinds` = `dp_<sản phẩm>`, mục đích, hạn. Một `platform_admin` khác duyệt (ADR-0020, R-56).
3. **Khách hàng API**, ở tenant đơn vị vận hành, trên Admin API (MFA):
   - `api_client_admin` đăng ký khách hàng: `client_tenant_id`, tên hiển thị tổ chức (không phải người), `contact_ref`, phương thức xác thực;
   - đề xuất đăng ký sản phẩm và scope, gói hạn mức;
   - `api_client_approver` khác người duyệt.
4. **Phát hành khóa.** `api_client_admin` tạo khóa; bí mật hiện một lần. Bí mật giao cho đầu mối của bên thứ ba qua kênh riêng (không email thường), ghi biên bản giao nhận. Xoay khóa định kỳ (≤ 365 ngày) và ngay khi nghi lộ.
5. **Thử nghiệm chấp nhận.**
   - `GET /v1/data-products` thấy đúng sản phẩm;
   - một lời gọi mỗi sản phẩm → một dòng `usage_event` + một dòng `egress_event` ở tenant bên cấp;
   - thu hồi cấp quyền → 403 ngay ở lời gọi kế tiếp.
6. **Vận hành hằng tháng.** `billing_officer` lập bảng kê, `billing_approver` duyệt, tải CSV để lập hóa đơn ngoài hệ thống (ADR-0023).

Ngừng hợp tác:
- thu hồi khóa (lý do bắt buộc);
- thu hồi cấp quyền ở bên cấp;
- đăng ký sản phẩm sang `revoked`.

Sổ giữ nguyên (RB-1).

### 3. Đơn vị thẩm định (VVB)

- Đơn vị thẩm định là **tenant bên thứ ba** như mục 2, vai trò hợp đồng `verifier`.
- G5 phục vụ sản phẩm `mrv_package_status`:
  - trạng thái gói xuất MRV, băm gói, băm ảnh chụp, kết quả kiểm lại (`verify`), số bằng chứng tổng hợp theo kỳ/địa bàn;
  - **không** có ảnh, tọa độ thửa, mã nông hộ.
  
  Điều này đáp ứng KN US-MRV-05 AC: chuyên gia thẩm định chỉ xem, không sửa, mọi lần truy cập có nhật ký (sổ sử dụng + sổ xuất + `partner_read_log`).
- **Tải gói bằng chứng** (`dp:mrv_package_status:export`) **chưa bật** cho tới khi có ba điều kiện:
  - C-1 chốt danh mục trường;
  - có "bản cho đơn vị thẩm định": mã tham chiếu hộ được bút danh hóa theo từng đơn vị thẩm định, ảnh chỉ khi đồng ý theo mục đích `mrv.verification` còn hiệu lực;
  - DPA đã ký.
  
  Hợp đồng dữ liệu đã có chỗ cho scope `export`, đánh dấu chờ khách.
- Xác thực: khóa API. Nếu khách yêu cầu thì thêm OAuth client credentials (ADR-0022 mục 5). Cổng web tương tác cho chuyên gia thẩm định (vai `mrv_viewer` trong tenant của chính họ, đọc qua cấp quyền) **ngoài G5**.

### 4. Bên thứ ba đầu tiên (đề xuất, chờ khách chốt)

Prototype chỉ có một sản phẩm dữ liệu có giá: "Data Licensing" dự báo sản lượng cho nhà máy (EVN/VFT). Vì vậy đề xuất bên thứ ba đầu tiên là **một nhà máy thu mua rơm (sinh khối)**, với hai sản phẩm `collection_volume` và `hub_capacity`. Sản phẩm thứ ba `mrv_package_status` dành cho đơn vị thẩm định khi có dự án tín chỉ. Danh sách đầy đủ ở g5-conventions §6.

## Hệ quả

- Kiểm thử hồ sơ:
  - `check_flows.py` render `values-phasek.yaml`: luồng mới được phép, luồng cấm bị chặn;
  - kiểm thử Go: `api-public` thoát lỗi khi `AG_PUBLIC_API_ENABLED` ≠ `1`;
  - diễn tập bật Phase K trên staging: chỉ `helm upgrade -f values-phasek.yaml`, không build lại image.
- G6 (Phase 1) dùng cùng cơ chế: lớp giá trị mới, không sửa mã.
- Việc mở, chờ khách:
  - pháp nhân vận hành;
  - bên thứ ba đầu tiên;
  - mẫu hợp đồng dữ liệu/DPA;
  - kênh giao bí mật;
  - có cần mTLS không;
  - nhà cung cấp Valkey trong lãnh thổ.
