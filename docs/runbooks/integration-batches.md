# Nạp theo lô của cổng tích hợp (W5.4)

Nguồn: [ADR-0033](../adr/0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md) §3, kiến trúc v4.1 §41.3, R-INT-03, R-INT-04. Mã: `internal/platform/apiclient/ingest`, route `internal/platform/apiclient/public/batches.go`, nối ở `cmd/integration-gateway/assembly` và `cmd/integration-gateway/gateway/ingest.go`. Hợp đồng: `contracts/integration/ingest/`. Lỗi: [errors.md](../errors.md#cổng-tích-hợp--nạp-theo-lô-w54-post-v1batchescontract-get-v1batchesid-adr-0033-3). Chung về cổng: [integration-gateway.md](integration-gateway.md).

## 1. Luồng

`POST /v1/batches/{contract}` (khóa API [+ mTLS], scope `ingest:<hợp đồng>:write`, `Idempotency-Key` = `batch_ref`)
→ kiểm hợp đồng (mặc định từ chối) + phong bì `ingest_batch.v1` + `max_batch` → sổ `pf_apiclient.ingest_batch` → **202** + mã lô + `Location: /v1/batches/{id}`
→ hàng đợi riêng của đối tác `pf.apiclient.ingest.<client_id>` → bộ xử lý trong tiến trình cổng (inbox `shared.inbox`, consumer = tên hàng đợi)
→ từng bản ghi: lược đồ hợp đồng, `entity_key`, khóa nghiệp vụ → port module chủ → sổ `ingest_record_result` → kết cục `ingest_batch_outcome` (`completed`).

Port chủ lỗi tạm → giao dịch rollback, thông điệp giao lại (lùi `inbox.RetryBackoff`); quá `inbox.DeliveryLimit` (5) → DLQ `pf.apiclient.ingest.<client_id>.dlq`, kết cục `dead_lettered` (`integration.owner_unavailable`).

Mọi sổ thuộc **tenant đối tác** (RLS), vai `r_apiclient` dưới đăng nhập `app_public`. Không có dữ liệu cá nhân.

## 2. Biến môi trường (cổng)

| Biến | Mặc định | Ý nghĩa |
|---|---|---|
| `AG_AMQP_URL` | — | RabbitMQ vhost `/` của cụm pf. Có → quorum queue + DLQ từng đối tác. Không có: dev/test dùng hàng đợi trong tiến trình (không bền, một bản sao); **production tắt `/v1/batches` (503)** |
| `AG_GATEWAY_INGEST_MAX_QUEUED` | 50 | Số lô chờ tối đa mỗi đối tác (`x-max-length`, `reject-publish`) — vượt → 429 `integration.queue_saturated` |
| `AG_GATEWAY_INGEST_RETRY_AFTER` | 30 | `Retry-After` (giây) của 429 hàng đợi đầy |
| `AG_GATEWAY_INGEST_ALLOW_PROPOSED` | — | `1` = phục vụ hợp đồng nạp `proposed`/`draft` (dev/test, staging). **Production: cổng không khởi động** nếu đặt |

Tài khoản RabbitMQ của cổng cần quyền configure/write/read trên `^pf\.apiclient\.ingest\..*` và write `ag.dlx` (khai báo hàng đợi + DLQ khi gặp đối tác mới). Thêm vào `rabbitmq.users` của chart khi bật cổng ở production.

## 3. Cấp quyền nạp cho đối tác

Như [public-api-onboarding.md](public-api-onboarding.md), nhưng đăng ký (subscription) có `product` = **tên hợp đồng nạp** (vd `roll_count_statement`) và `scopes: ["ingest:roll_count_statement:write"]` (đề xuất ≠ duyệt). Hợp đồng phải phục vụ được ở môi trường đó:

- ba hợp đồng song phương `collection_schedule`, `roll_count_statement`, `htx_roll_confirmation` đang `proposed` (C-12) → chỉ staging/dev với `AG_GATEWAY_INGEST_ALLOW_PROPOSED=1`;
- đến W5.6, ba hợp đồng này đi vào **sổ đệm** `pf_apiclient.ingest_staging` (port chủ chưa có); W5.6 nối port thật ở `assembly.Config.IngestOwners` và chuyển dữ liệu đã đệm. Van RabbitMQ `valve_collection` của MKG giữ nguyên tới W5.10.

## 4. Kiểm tra

```bash
rabbitmqctl -q list_queues name messages consumers | grep pf.apiclient.ingest
```

- Mỗi đối tác đã gửi lô có `pf.apiclient.ingest.<client_id>` (`consumers` = số bản sao cổng) và `.dlq`.
- Lô của một đối tác: `GET /v1/batches/{id}` bằng khóa của chính đối tác. Vận hành tra sổ (vai quản trị, `app.tenant_id` = tenant đối tác):

```sql
SELECT b.id, b.contract, b.item_count, o.outcome, o.accepted, o.rejected, o.problem_code
  FROM pf_apiclient.ingest_batch b LEFT JOIN pf_apiclient.ingest_batch_outcome o ON o.batch_id = b.id
 WHERE b.client_id = '<client_id>' ORDER BY b.event_time DESC LIMIT 20;
```

## 5. Sự cố

| Dấu hiệu | Nguyên nhân thường gặp | Xử lý |
|---|---|---|
| Đối tác nhận 429 `integration.queue_saturated` liên tục | Consumer của đối tác không chạy / port chủ chậm | Kiểm `consumers` của hàng đợi; log `ingest.consumer_restart`; tăng `AG_GATEWAY_INGEST_MAX_QUEUED` chỉ khi đã đo (§42: một đối tác vượt hạn mức không làm chậm đối tác khác) |
| Lô `queued` mãi | Thông điệp chưa phát (503 `integration.queue_unavailable` lúc nhận) | Đối tác gửi lại **cùng** `Idempotency-Key` + cùng thân → cổng phát lại; inbox khử trùng |
| Lô `dead_lettered` | Port module chủ lỗi tạm ≥ 6 lần giao | Sửa nguyên nhân (log `ingest.batch_failed`, `ingest.dead_lettered`); đối tác gửi lại bằng `batch_ref` mới (khóa nghiệp vụ idempotent ở port chủ nên an toàn). **Không** xóa DLQ bằng tay (R-62); xả DLQ theo quy trình thay đổi sau khi đối tác đã gửi lại |
| 403 `integration.contract_not_servable` ở staging | Thiếu `AG_GATEWAY_INGEST_ALLOW_PROPOSED=1` cho hợp đồng `proposed` | Đặt cờ (không bao giờ ở production) |
| 503 `apiclient.dependency_unavailable` ở `/v1/batches` | Production thiếu `AG_AMQP_URL` | Đặt `AG_AMQP_URL` + tài khoản RabbitMQ (mục 2) |
