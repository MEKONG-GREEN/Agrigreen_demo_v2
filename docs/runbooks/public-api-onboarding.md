# Cấp bên thứ ba cho Public API (G5)

Thủ tục đưa một bên thứ ba (nhà máy sinh khối, bên mua tín chỉ, đơn vị thẩm định…) vào Public API `partner.<domain>`. Đây là **thủ tục vận hành, không sửa mã, không đổi cấu hình hạ tầng** (R-01). Điều kiện: Phase K đã bật theo [phase-k.md](phase-k.md).

Quyết định: [ADR-0022](../adr/0022-public-api-ben-thu-ba.md) (Public API, khóa, scope, hạn mức), [ADR-0023](../adr/0023-so-do-luong-su-dung-metering.md) (sổ đo lường, bảng kê), [ADR-0024](../adr/0024-diem-kiem-soat-xuat-du-lieu-dlp.md) (điểm kiểm soát xuất), [ADR-0025](../adr/0025-ho-so-cap-phat-phase-k.md) mục 2–3 (thủ tục này), [ADR-0020](../adr/0020-lien-thong-platform-mkg-khac-tenant.md) (cấp quyền chia sẻ). Quy ước: [g5-conventions.md](../dev/g5-conventions.md).

Kiểm thử chạy lại đúng các bước 2–5 trên gốc lắp ráp thật: `TestPublicAPIRealWiring_G5Integration` (`cmd/api-public`).

## Ai làm gì

| Bước | Ai | Tenant | Bề mặt |
|---|---|---|---|
| 0. Pháp lý | chủ dữ liệu, đơn vị vận hành, bên thứ ba | — | ngoài hệ thống |
| 1. Tenant bên thứ ba | quản trị nền tảng | tenant bên thứ ba (mới) | Admin API `pf/party` |
| 2. Cấp quyền `dp_*` | `platform_admin` A đề xuất, `platform_admin` B duyệt | tenant **chủ dữ liệu** (bên cấp) | Admin API `/v1/share-grants` |
| 3. Khách hàng API, đăng ký sản phẩm, gói hạn mức | `api_client_admin` đề xuất, `api_client_approver` duyệt | tenant **đơn vị vận hành** | Admin API `/v1/api-clients` |
| 4. Khóa API | `api_client_admin` | tenant đơn vị vận hành | Admin API `/v1/api-clients/{id}/keys` |
| 5. Thử chấp nhận | bên thứ ba + đơn vị vận hành | — | Public API `partner.<domain>` |
| 6. Bảng kê hằng tháng | `billing_officer` yêu cầu, `billing_approver` duyệt | tenant đơn vị vận hành | Admin API `/v1/metering/statements` |

Mọi thao tác Admin API cần đăng nhập Keycloak có **MFA**. Người đề xuất và người duyệt phải là **hai người khác nhau** (R-56): hệ thống từ chối ở `app/` và bằng CHECK DB (`*.self_approval`).

## 0. Pháp lý (ngoài hệ thống)

1. Hợp đồng dữ liệu hoặc DPA ba bên (chủ dữ liệu, đơn vị vận hành, bên thứ ba): mục đích, sản phẩm, độ chi tiết, thời hạn, điều khoản tái chia sẻ, kênh giao bí mật khóa (Q5-8).
2. Ghi **số hợp đồng** để dùng trong trường `purpose` của cấp quyền (bước 2).
3. Sản phẩm sẽ phục vụ phải có hợp đồng dữ liệu `contracts/dataproduct/<sản phẩm>.json` ở trạng thái `approved` trong bản phát hành đang chạy. Production **không** phục vụ hợp đồng `draft` (ADR-0022 mục 9).
4. Đơn vị thẩm định (VVB): chỉ sản phẩm `mrv_package_status`. Tải gói bằng chứng (`export`) **chưa bật** cho tới khi có C-1, bản bút danh theo VVB và DPA (ADR-0025 mục 3).

## 1. Tenant bên thứ ba

- Cấp **tenant UUIDv7 mới** cho bên thứ ba, như đã cấp tenant MKG ở ADR-0020. Tạo tổ chức gốc của tenant đó ở `pf/party` (`POST /v1/orgs`, token có `tenant_id` = tenant mới).
- Bên thứ ba **chỉ gọi API**: không cần tài khoản Keycloak nào. Chỉ khi họ cần người dùng (ngoài G5) mới tạo tài khoản qua `pf/accounts`.
- Ghi tenant vào biên bản: `third_party_tenant_id`.

## 2. Cấp quyền chia sẻ của chủ dữ liệu

`platform_admin` của **bên cấp** (tenant chủ dữ liệu, vd tenant chương trình) đề xuất:

```http
POST https://admin.<domain>/v1/share-grants
Idempotency-Key: <uuid>
{
  "grantee_tenant_id": "<third_party_tenant_id>",
  "kinds": ["dp_collection_volume"],
  "purpose": "HĐ dữ liệu số …/2028 — nhà máy lập kế hoạch thu mua rơm theo xã và vụ",
  "valid_to": "2029-12-31T17:00:00Z"
}
```

- Loại theo sản phẩm: `dp_collection_volume`, `dp_hub_capacity` (cần MKG đồng ý — Q5-6), `dp_mrv_package_status`.
- Một `platform_admin` **khác** duyệt: `POST /v1/share-grants/{id}/approve`.
- Cấp quyền có hạn (tối đa 5 năm). Gia hạn = cấp quyền mới.
- Chưa duyệt, hết hạn, chưa tới `valid_from`, đã thu hồi → Public API trả 403 `apiclient.forbidden` (không nói thiếu điều kiện nào).

## 3. Khách hàng API, đăng ký sản phẩm, gói hạn mức

Ở tenant **đơn vị vận hành** (`AG_PUBLIC_OPERATOR_TENANT`):

1. `api_client_admin` đăng ký khách hàng:
   ```http
   POST /v1/api-clients
   { "client_tenant_id": "<third_party_tenant_id>", "display_name": "<tên tổ chức, không tên người>",
     "purpose": "<mục đích theo hợp đồng>", "contact_ref": "<mã tham chiếu đầu mối>" }
   ```
   `contact_ref` là mã tham chiếu. Tên, số điện thoại của đầu mối **không** nhập vào `pf_apiclient` (ở kho định danh hoặc tài khoản).
2. `api_client_approver` duyệt: `POST /v1/api-clients/{id}/approve`.
3. `api_client_admin` đề xuất đăng ký sản phẩm + scope, `api_client_approver` duyệt:
   ```http
   POST /v1/api-clients/{id}/subscriptions
   { "product": "collection_volume", "scopes": ["dp:collection_volume:read", "dp:collection_volume:export", "usage:read"] }
   POST /v1/api-clients/{id}/subscriptions/{sub_id}/approve
   ```
4. Gói hạn mức (có phiên bản). Không đặt thì dùng mặc định **tạm, chờ khách Q5-5**: 60 lời gọi/phút, 10 000 đơn vị/ngày, 200 000 đơn vị/tháng, 100 000 dòng/tệp xuất.
   ```http
   POST /v1/api-clients/{id}/plans
   { "requests_per_minute": 60, "daily_units": 10000, "monthly_units": 200000, "max_export_rows": 100000 }
   POST /v1/api-clients/{id}/plans/{version}/approve
   ```

## 4. Phát hành khóa và giao bí mật

1. `api_client_admin`: `POST /v1/api-clients/{id}/keys` (kèm `Idempotency-Key`).
2. Phản hồi có `api_key` dạng `agk_<prefix>_<bí mật>`, **chỉ một lần** (`Cache-Control: no-store`). Gọi lặp cùng `Idempotency-Key` trả lại khóa **không** kèm bí mật. Hệ thống chỉ lưu `key_prefix` + HMAC; mất bí mật thì phát hành khóa mới, không khôi phục được.
3. Giao bí mật cho đầu mối của bên thứ ba qua **kênh riêng đã chốt trong hợp đồng** (Q5-8). Không email thường, không chat, không dán vào phiếu hỗ trợ.
4. Ghi biên bản giao nhận: `client_id`, `key_prefix` (không ghi bí mật), thời điểm, người giao, người nhận, kênh.
5. Bên thứ ba gửi khóa trong header `X-API-Key`. Log và sổ chỉ có `key_prefix`.

Mỗi khách hàng tối đa **2** khóa hiệu lực. Hạn khóa **365 ngày**.

## 5. Thử chấp nhận

Bên thứ ba gọi từ hệ thống của họ (hoặc đơn vị vận hành gọi thử cùng họ):

```powershell
curl.exe -s -H "X-API-Key: <khóa>" https://partner.<domain>/v1/data-products
curl.exe -s -H "X-API-Key: <khóa>" "https://partner.<domain>/v1/data/collection-volumes?season_code=2028-DX"
```

Đạt khi:
1. `GET /v1/data-products` thấy **đúng** sản phẩm đã đăng ký, `grantors` có tenant bên cấp.
2. Mỗi sản phẩm một lời gọi → 200, header `AG-Data-Contract: <sản phẩm>.v1`, `RateLimit-*`, `AG-Quota-Remaining`. `collection_volume` không có ô dưới k (`meta.suppressed_cells`, `meta.k_applied`).
3. Mỗi lời gọi đã xác thực → **một** dòng sổ sử dụng ở tenant đơn vị vận hành: Admin API `GET /v1/metering/usage-events?client_id=…` (vai `metering_viewer`).
4. Mỗi lời gọi đọc dữ liệu → **một** dòng `egress_event` kênh `public_api` ở tenant **chủ dữ liệu**: `metering_viewer` của bên cấp xem `GET /v1/metering/egress`.
5. `GET /v1/usage?from=…&to=…` (scope `usage:read`) chỉ trả sử dụng của chính khách hàng.
6. Xuất: `POST /v1/data/collection-volumes/exports` (bắt buộc `Idempotency-Key`) → 202 + `Location: /v1/jobs/{id}`; khi `completed` thì `GET /v1/jobs/{id}/file` trả tệp, header `Digest: sha-256=…` khớp `sha256` của job.
7. Thu hồi cấp quyền thử (trên một cấp quyền thử riêng) → 403 ngay ở lời gọi kế tiếp.
8. Khóa sai → 401, không có dòng sổ.

Ghi kết quả vào biên bản chấp nhận. Lỗi thường gặp:

| Kết quả | Nguyên nhân |
|---|---|
| 401 `apiclient.unauthenticated` | khóa sai, bị thu hồi, hết hạn; khách hàng chưa `active` |
| 403 `apiclient.forbidden` | thiếu scope (đăng ký chưa duyệt), thiếu / hết hạn cấp quyền `dp_*`, hợp đồng `draft` ở production |
| 429 `apiclient.rate_limited` | vượt lời gọi/phút. Xem `Retry-After` |
| 429 `metering.quota_exceeded` | hết đơn vị ngày hoặc tháng. `Retry-After` tới đầu cửa sổ kế tiếp |
| 503 `metering.unavailable` | sổ đo lường không ghi được: **không** có byte dữ liệu nào được gửi (đóng khi lỗi) |

## 6. Vận hành hằng tháng: bảng kê

1. Sau khi kỳ tháng kết thúc (giờ Việt Nam), `billing_officer`: `POST /v1/metering/statements` `{ "period": "2028-11", "client_id": "<id>" }` → 202 + job.
2. `billing_approver` (người khác) kiểm `GET /v1/metering/statements/{id}`, `POST /v1/metering/statements/{id}/verify`, rồi duyệt `POST /v1/metering/statements/{id}/approve`. Bảng kê đã duyệt bị đóng băng (409 `metering.statement_frozen`).
3. Tải CSV `GET /v1/metering/statements/{id}/export.csv` để lập hóa đơn **ngoài hệ thống**. Chưa có đơn giá thì bảng kê chỉ có đơn vị (Q5-4).
4. Sai sót → bảng kê **bù** (`supersedes_id` + `correction_reason`). Bảng kê cũ giữ nguyên (RB-1).

## 7. Xoay khóa

Định kỳ (≤ 365 ngày) và **ngay khi nghi lộ**:

1. `api_client_admin`: `POST /v1/api-clients/{id}/keys/{key_id}/rotate` → khóa mới (bí mật hiện một lần, giao như bước 4). Khóa cũ chuyển `rotated`, còn dùng được **7 ngày** chồng lấn.
2. Bên thứ ba chuyển sang khóa mới trong thời gian chồng lấn.
3. Hết chồng lấn: khóa cũ không xác thực được nữa. Job `apiclient_key_expiry` của `cmd/worker` (mỗi giờ, chạy ở leader) ghi sự kiện `expired` vào sổ khóa. Khóa quá 365 ngày cũng được ghi `expired` như vậy. Job chỉ chạy khi worker có `AG_PUBLIC_OPERATOR_TENANT`.
4. Nghi lộ: **thu hồi ngay** khóa cũ (mục 8), không chờ hết chồng lấn.

Xoay pepper của HMAC (khác với xoay khóa): xem [phase-k.md](phase-k.md) mục 5.

## 8. Thu hồi và ngừng hợp tác

| Việc | Thao tác | Hiệu lực |
|---|---|---|
| Thu hồi một khóa | `POST /v1/api-clients/{id}/keys/{key_id}/revoke` `{ "reason": "<bắt buộc>" }` | lời gọi kế tiếp bằng khóa đó → 401 |
| Tạm ngừng khách hàng | `POST /v1/api-clients/{id}/suspend` (`reactivate` để mở lại) | mọi khóa → 401 |
| Thu hồi đăng ký sản phẩm | `POST /v1/api-clients/{id}/subscriptions/{sub_id}/revoke` | sản phẩm đó → 403 |
| Thu hồi cấp quyền (bên cấp) | `platform_admin` bên cấp: `POST /v1/share-grants/{id}/revoke` `{ "reason": … }` | đọc và tải tệp xuất → 403 ngay |
| Ngừng hợp tác hẳn | thu hồi khóa → thu hồi cấp quyền ở bên cấp → thu hồi đăng ký → `POST /v1/api-clients/{id}/revoke` | |

Sổ khóa, sổ khách hàng, sổ sử dụng, nhật ký xuất **giữ nguyên** (RB-1). Không xóa gì. Bảng kê của kỳ cuối vẫn lập bình thường.

## 9. Cảnh báo xuất bất thường (DLP)

Khi khối lượng xuất trong ngày của (tenant chủ dữ liệu × kênh × bên nhận) vượt ngưỡng lần đầu, `pf/metering` phát `pf.metering.egress.anomaly_detected`. Consumer `pf.notify.egress_anomaly` gửi thông báo khẩn cho `metering_viewer` và `platform_admin` của **tenant chủ dữ liệu** (tạm, chờ khách Q5-5 chốt người nhận). Xem chi tiết ở `GET /v1/metering/egress/anomalies`. Nếu là lạm dụng: tạm ngừng khách hàng (mục 8), rồi xử lý theo hợp đồng.
