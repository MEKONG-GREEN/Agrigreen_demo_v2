# ADR-0023: `pf/metering` — sổ đo lường sử dụng là chứng từ, bộ đếm hạn mức, bảng kê theo kỳ

**Trạng thái:** Đề xuất · **Ngày:** 01/10/2026 (G5-0) · **Thành phần:** `pf/metering` (schema `pf_metering`, vai `metering_app`, migration pf 000082), `cmd/api-public`, `cmd/worker` · **Liên quan:** [ADR-0022](0022-public-api-ben-thu-ba.md), [ADR-0024](0024-diem-kiem-soat-xuat-du-lieu-dlp.md), mục 8 và 16 của kiến trúc

## Bối cảnh

Mục 16 của kiến trúc: `pf/metering` là sổ đo lường sử dụng, là **chứng từ**, theo mẫu sổ mục 8. Mỗi lời gọi Public API ghi một dòng. R-10 cấm float trong package tiền và khối lượng; `agnofloat` đã phủ đoạn `metering`. Không yêu cầu nào của khách nói về thu phí. Dấu hiệu duy nhất là prototype: gói "Data Licensing" `SUB-DATA` giá cố định 450 000 000 đ cho nhà máy, chưa ghi kỳ hạn. Vì vậy giá phải là **tham số có phiên bản, tùy chọn**, và để "chờ khách".

## Quyết định

### 1. Sổ sử dụng `pf_metering.usage_event`

Sổ theo đúng mẫu mục 8.1:
- chỉ ghi thêm, ba lớp cưỡng chế (`shared.apply_ledger`);
- chuỗi băm;
- phân vùng theo tháng của `recorded_at`;
- RLS (`shared.apply_tenant_rls`).

**Một dòng mỗi lời gọi đã xác thực** của Public API, kể cả lời gọi bị từ chối sau xác thực (403, 404, 422, 429) và lời gọi lặp idempotent. Lời gọi chưa xác thực được (khóa sai hoặc thiếu) **không** ghi sổ, vì không quy được về khách hàng và ghi vào sổ sẽ mở đường tấn công ngập sổ. Những lời gọi này chỉ vào log + metric `ag_public_unauthenticated_total`, kèm `key_prefix` nếu đọc được.

| Cột | Nội dung |
|---|---|
| `id` (UUIDv7), `tenant_id` | tenant **đơn vị vận hành** (ADR-0022 mục 3) |
| `stream_key` | `client:<client_id>:<YYYY-MM>`: chuỗi băm theo khách hàng và kỳ tính phí. `seq`, `prev_hash`, `row_hash` theo mẫu |
| `client_id`, `client_tenant_id`, `key_prefix`, `auth_method` | ai gọi. Không bao giờ có bí mật khóa |
| `product`, `contract_version`, `scope` | sản phẩm dữ liệu, phiên bản hợp đồng dữ liệu, scope dùng. Rỗng với route không phải sản phẩm, vd `/v1/usage` |
| `route_template`, `method` | vd `GET /v1/data/collection-volumes`. **Mẫu route**, không phải URL thật: không có giá trị truy vấn |
| `status`, `problem_code` | mã HTTP, mã lỗi RFC 9457 nếu có |
| `data_tenant_id` | tenant chủ dữ liệu đã phục vụ (bên cấp). NULL khi không phục vụ dữ liệu |
| `units` (integer ≥ 0), `rows_out` (integer ≥ 0), `bytes_out` (bigint ≥ 0) | đơn vị tính phí theo quy tắc có phiên bản, số phần tử, số byte thân phản hồi |
| `latency_ms` (integer ≥ 0) | độ trễ phía máy chủ |
| `replayed` (boolean) | lời gọi lặp idempotent: `units = 0` |
| `job_id` | việc xuất bất đồng bộ, nếu có |
| `event_time`, `recorded_at` | lúc nhận request và lúc ghi (R-13) |
| `actor_ref` | `client_id`, theo mẫu |
| `idem_key` | `request_id` (UUIDv7 sinh ở `api-public`), `UNIQUE (tenant_id, idem_key)` |
| `rule_version` | phiên bản quy tắc tính đơn vị, vd `metering.units.v1` |
| `trace_id` | R-63 |
| `supersedes_id`, `correction_reason` | bù trừ (vd hoàn đơn vị khi sự cố phía nền tảng). Không bao giờ UPDATE |

**Thứ tự ghi và đóng khi lỗi:**
1. `api-public` dựng phản hồi xong (phần tử đã qua `dataproduct.Validate`).
2. Gọi `meteringport.Ledger.Record`, trong **một** giao dịch: ghi `usage_event`, cập nhật `usage_counter_current`, ghi `egress_event` ở tenant chủ dữ liệu (ADR-0024).
3. Ghi xong mới gửi thân.

Ghi lỗi → `503 metering.unavailable`, không gửi dữ liệu (như `party.partner_audit_unavailable` của ADR-0020).

`Record` của đơn vị vượt hạn mức trả `metering.quota_exceeded`. Khi đó dòng sổ vẫn được ghi, với `status = 429` và `units = 0`.

### 2. Đơn vị tính phí

Quy tắc là hằng có phiên bản trong `metering/port` (`RuleUnitsV1`) và được ghim vào `rule_version`:
- đọc thành công (2xx) = **1 đơn vị**;
- yêu cầu xuất được nhận (202) = **0**;
- tải tệp xuất = `ceil(rows / 1000)`, tối thiểu 1;
- lỗi 4xx/5xx và lời gọi lặp = **0**.

Đổi quy tắc → `metering.units.v2`. Dòng cũ không đổi.

### 3. Bộ đếm hạn mức và cửa sổ

- `usage_counter_current` là bảng chiếu `(tenant_id, client_id, product, window_kind ∈ {day, month}, window_start) → units, calls`. Bảng được cập nhật **cùng giao dịch** với sổ và khóa dòng theo `(client_id, product, window)`.
- Đây là nguồn **thẩm quyền** của hạn mức. Valkey chỉ là giới hạn tần suất (ADR-0022 mục 7).
- Ngày và tháng tính theo giờ Việt Nam (`clock.BusinessDate`).
- Dựng lại được từ sổ (R-22): `RebuildTenant`.

Gói hạn mức của khách hàng (`quota_plan`) là bản ghi có phiên bản ở `pf_apiclient`: người đặt `api_client_admin` ≠ người duyệt `api_client_approver`. `pf_metering` đọc qua `apiclientport.Plans`. Mặc định (tạm, chờ khách):

| Khóa | Mặc định |
|---|---|
| `requests_per_minute` | 60 |
| `daily_units` | 10 000 |
| `monthly_units` | 200 000 |
| `max_export_rows` | 100 000 mỗi việc xuất |

### 4. Bảng kê theo kỳ (chứng từ lập hóa đơn)

Kỳ = **tháng dương lịch**, giờ Việt Nam. Theo mẫu mục 12:

1. `billing_officer` yêu cầu lập bảng kê kỳ `YYYY-MM` cho một khách hàng hoặc mọi khách hàng: `POST /v1/metering/statements` trên Admin API → 202 + job, sự kiện `pf.metering.statement.requested`.
2. Worker nạp **ảnh chụp bất biến**: tổng đơn vị, số lời gọi theo (khách hàng × sản phẩm × lớp trạng thái) của các dòng `usage_event` thuộc stream của kỳ. Ảnh chụp ghi kèm `first_seq`, `last_seq`, `row_hash` cuối chuỗi và băm SHA-256 ảnh chụp.
3. Bảng kê `statement` (sổ, `stream_key = statement:<client_id>:<YYYY-MM>`) gồm:
   - dòng đơn vị;
   - nếu có tham số giá: đơn giá (`shared/money.Price`, `numeric(18,4)`), thành tiền (`money.Amount`, `numeric(18,2)`), `price_version`;
   - trạng thái `draft`.
4. `billing_approver` duyệt (≠ người yêu cầu, R-56, `app/` **và** CHECK DB) → `approved`, đóng băng. Sai sót → bảng kê mới `supersedes_id` + lý do.
5. Tải bảng kê (CSV UTF-8 tất định + JSON, băm SHA-256) qua Admin API để lập hóa đơn **ngoài hệ thống**. Lần tải đi qua điểm kiểm soát xuất, kênh `report_export`.

  Nền tảng **không** đẩy sang `mkg/acctbridge`: khác vùng (R-03), và chưa có quyết định ai lập hóa đơn.

Giá là **tham số có phiên bản, tùy chọn**, chờ khách:
- tên tham số `metering.price.<plan>.<product>.per_unit_vnd` và `metering.price.<plan>.subscription_vnd_per_month`;
- lưu ở `pf/masterdata` (setter ≠ approver), áp theo thời điểm của kỳ.

Chưa đặt giá → bảng kê chỉ có đơn vị, `amount = null`. Prototype `SUB-DATA` (450 000 000 đ) **chưa** được dùng làm mặc định: chưa rõ kỳ hạn và phạm vi.

### 5. Khách hàng xem sử dụng của mình

`GET /v1/usage?from=&to=` trên Public API, scope `usage:read`. Trả tổng theo ngày × sản phẩm từ bộ đếm, chỉ của `client_id` của khóa. Lời gọi này cũng ghi sổ, với `units = 0`.

### 6. Lưu giữ

Sổ sử dụng và bảng kê là chứng từ. Thời hạn lưu tối thiểu theo pháp luật kế toán (đề xuất **10 năm**, chờ pháp chế xác nhận). Hết hạn nóng thì **chuyển tầng**, không xóa (RB-1). Neo chuỗi hằng ngày dùng job `anchor` sẵn có.

## Hệ quả

- `agnofloat` đã cưỡng chế `metering` theo tên đoạn: mọi package `internal/platform/metering/**` không được dùng float.
- Kiểm thử bắt buộc:
  - sáu phép thử UPDATE/DELETE/TRUNCATE trên `usage_event`, `statement`;
  - `verify_chain`;
  - cách ly tenant;
  - ghi sổ lỗi → không trả dữ liệu;
  - lời gọi lặp idempotent → `units = 0`;
  - bộ đếm = tổng sổ sau dựng lại;
  - bảng kê tất định (cùng ảnh chụp → cùng băm);
  - người duyệt ≠ người yêu cầu.
- Việc mở, chờ khách:
  - mô hình tính phí: theo đơn vị, thuê bao cố định, hay bậc thang;
  - đơn giá;
  - ai lập hóa đơn;
  - thời hạn lưu chứng từ;
  - có hoàn đơn vị khi sự cố phía nền tảng không.
