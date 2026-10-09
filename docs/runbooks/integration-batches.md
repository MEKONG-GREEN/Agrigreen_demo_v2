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

## 6. Kênh tệp cùng lược đồ (W5.8, R-INT-11)

Cho đối tác không có đội IT (HTX nhỏ, cơ quan chỉ có Excel): cùng hợp đồng, cùng scope `ingest:<hợp đồng>:write`, khác kênh.

```
POST /v1/files/{contract}?contract_version=N   (thân = tệp; Content-Type application/x-ndjson | text/csv)
  → kiểm hợp đồng (mặc định từ chối) + ingest.channels có "file" + giới hạn byte/bản ghi
  → tách bản ghi (NDJSON: mỗi dòng một object; CSV: tiêu đề = tên trường, ',' hoặc ';', BOM được)
  → sổ pf_apiclient.ingest_file (pf 000128; idem theo SHA-256 tệp)
  → chia theo ingest.max_batch → mỗi phần là phong bì ingest_batch.v1 (batch_ref = UUIDv5(mã tệp, k))
  → ingest.Service.Submit của W5.4 (cùng hàng đợi đối tác, inbox, ValidateRecord, port chủ, kết quả từng bản ghi)
GET /v1/files/{id}            trạng thái gộp: incomplete | queued | completed | dead_lettered
GET /v1/files/{id}/results    tệp kết quả (?format=csv|ndjson): record, entity_key, outcome, problem_code, detail
```

- **Một đường kiểm:** cổng chỉ tách tệp; ô CSV được đổi sang kiểu JSON của trường (số, boolean, null cho trường nullable rỗng) chỉ khi đúng dạng — ô sai dạng giữ chuỗi để hợp đồng từ chối BẢN GHI (`integration.record_invalid` trong tệp kết quả), không phải cả tệp. Cả tệp bị từ chối (400 `integration.file_invalid`) chỉ khi không tách được bản ghi.
- **Idempotent theo tổng kiểm:** tải lại đúng tệp → cùng mã tệp (`Idempotent-Replayed: true`), lô đã gửi là gọi lặp, lô còn thiếu được gửi tiếp. Đây là cách xử lý 429 `integration.queue_saturated` giữa tệp (tệp ở `incomplete`): chờ `Retry-After` rồi tải lại cùng tệp. Sửa một byte = tệp mới.
- **Giới hạn:** `AG_GATEWAY_INGEST_MAX_FILE_BYTES` (mặc định 8 MiB, trần 100 MiB — route có giới hạn thân riêng, Ingress/Traefik phải cho qua tối thiểu cùng cỡ), `AG_GATEWAY_INGEST_MAX_FILE_RECORDS` (mặc định 10 000). Số lô của một tệp tính vào hạn hàng đợi của đối tác (`AG_GATEWAY_INGEST_MAX_QUEUED`) — tệp 10 000 bản ghi × `max_batch` 500 = 20 lô.
- **Điểm kiểm soát:** cả ba route đi qua đường ống `/v1` (khóa [+ mTLS], hạn tần suất, sổ đo lường trước byte — đóng khi lỗi): POST = `export_request` 0 đơn vị (`job_id` = mã tệp), GET = `job_status`. Tệp kết quả chỉ có khóa thực thể + mã problem của CHÍNH đối tác, không giá trị bản ghi; ô CSV bắt đầu bằng `= + - @` được thêm `'`. Không lưu nội dung tệp (bản ghi đã ở `ingest_batch.items`).
- **Kiểm tra:** `SELECT id, contract, contract_version, format, record_count, batch_count, event_time FROM pf_apiclient.ingest_file` (dưới tenant đối tác); lô của tệp tra theo `batch_ref` ở `ingest_batch`.
- **Không làm:** URL tải lên ký sẵn vào kho đối tượng (sẽ vòng qua sổ đo lường và kiểm hợp đồng đồng bộ — ADR-0033 §2 chỉ cho URL ký ở chiều xuất), SFTP. Tải XUỐNG feed theo tệp = ảnh chụp `GET /v1/snapshots/…` (W5.3).

## 7. Môi trường thử của đối tác (W5.8, R-INT-08)

Một **triển khai riêng** (namespace/compose riêng, DB riêng, pepper riêng) chạy `api-platform` + `integration-gateway` với `AG_PARTNER_SANDBOX=1`, dữ liệu do `cmd/seed-synthetic` sinh — không có dữ liệu thật.

| Bảo đảm | Cơ chế |
|---|---|
| Khóa thử khác khóa thật | `api-platform` ở chế độ thử phát hành khóa `agks_<prefix>_<bí mật>`; khóa thật là `agk_…` |
| Khóa thử không dùng được ở production | Cổng thật từ chối mọi khóa `agks_` (401, log `apiclient.sandbox_key_refused`) trước khi tra DB; cổng thử từ chối khóa `agk_` |
| Production không có chế độ thử | `AG_ENV=production` + `AG_PARTNER_SANDBOX=1` → `api-platform` và `integration-gateway` không khởi động (`ErrSandboxInProduction`, kiểm trước mọi kết nối) |
| Đối tác biết đang ở môi trường thử | Mọi phản hồi của cổng thử có header `AG-Environment: sandbox` |
| Cùng hợp đồng, cùng đường ống | Không có nhánh mã riêng cho thử: feed, lô, kênh tệp, sổ đo lường chạy y như thật; hợp đồng `proposed` mở bằng `AG_GATEWAY_INGEST_ALLOW_PROPOSED=1` / `AG_GATEWAY_FEED_ALLOW_PROPOSED=1` (chỉ ngoài production) |

**Cấp môi trường thử:**

1. Dựng stack riêng với `AG_ENV=staging` (hoặc `test`), DB riêng (`cmd/migrate bootstrap` + `up`), pepper riêng (`AG_APIKEY_PEPPER_V1` khác production), `AG_PARTNER_SANDBOX=1` cho **cả** `api-platform` và `integration-gateway`, `AG_GATEWAY_ENABLED=1`, `AG_GATEWAY_INGEST_ALLOW_PROPOSED=1`, `AG_GATEWAY_FEED_ALLOW_PROPOSED=1`; host riêng (vd `sandbox.partner.<domain>`), không chung Ingress với production.
2. Sinh dữ liệu tổng hợp: `go run ./cmd/seed-synthetic -app-url … -admin-url … -seed <n>` (kế hoạch `platform`; `-plan mkg` nếu cần luồng MKG). Bộ sinh từ chối `AG_ENV=production`. Chạy job dựng feed (`cmd/worker -jobs apiclient_feed_build`) để feed có phiên bản.
3. Cấp đối tác như mục 3 trên Admin API của stack thử (đề xuất ≠ duyệt): khách hàng API với `client_tenant_id` là tenant tổng hợp của đối tác, đăng ký `product = <hợp đồng nạp/feed>`, gói hạn mức, phát hành khóa — khóa trả về bắt đầu bằng `agks_`. Gửi khóa cho đối tác qua kênh bảo mật như khóa thật.
4. Đối tác thử: `POST /v1/batches/{contract}`, `POST /v1/files/{contract}?contract_version=1` (mẫu bản ghi ở `contracts/integration/examples/`), `GET /v1/feeds/{feed}`. Kiểm tra header `AG-Environment: sandbox`.
5. Khi chuyển sang thật: cấp khách hàng + khóa MỚI ở production (khóa thử không bao giờ chuyển được).

## 8. Luồng song phương (W5.6, ADR-0034)

Hợp đồng `collection_schedule`, `roll_count_statement` (MKG ghi), `htx_roll_confirmation` (HTX ghi) còn `proposed` — **production từ chối tới khi C-12 chấp nhận** (cổng: cờ `AG_GATEWAY_INGEST_ALLOW_PROPOSED` / `AG_GATEWAY_FEED_ALLOW_PROPOSED`; api-platform: `AG_BILATERAL_ALLOW_PROPOSED=1` — đặt ở production thì không khởi động). Van `valve_collection` chạy song song tới W5.10.

1. **Cặp:** người quản lý HTX (vai `coop_manager`, đơn vị công tác = Nút HTX có hồ sơ) gọi `POST /v1/bilateral/pairs {counterparty_tenant_id: <tenant MKG>}` trên App HTX. Chấm dứt: `POST /v1/bilateral/pairs/{pair_id}/terminate {reason}` — bản ghi cũ giữ nguyên, MKG bị từ chối ghi (`integration.pair_missing` trong kết quả lô) và đọc feed cặp (403) từ lời gọi kế tiếp; lập lại mở lại.
2. **Khách hàng API của MKG** (mục 3): đăng ký `collection_schedule` + `roll_count_statement` scope `ingest:<tên>:write`, `htx_roll_confirmation` scope `feed:htx_roll_confirmation:read` (hợp đồng song phương nhận thêm scope đọc). Khóa đặt ở api-internal `AG_FIELDOPS_BILATERAL_API_KEY` + `AG_INTEGRATION_GATEWAY_URL`; job `fieldops_bilateral` (`AG_MKG_JOBS`, chu kỳ `AG_FIELDOPS_BILATERAL_INTERVAL`, mặc định 5m).
3. **Luồng:** `valve_export` → `mkg_fieldops.bilateral_send` (pending) → lô `POST /v1/batches/{hợp đồng}` (batch_ref ghi trước — lỗi tạm gửi lại đúng lô) → sổ `pf_coop.bilateral_record` → HTX xem `GET /v1/bilateral/pairs/{pair_id}/records`, xác nhận/phản đối `POST /v1/bilateral/confirmations` → job `apiclient_feed_build` (cmd/worker, mặc định 15m) chép sổ vào feed cặp theo seq → MKG kéo `GET /v1/feeds/bilateral?pair=<htx_ref>` vào `mkg_fieldops.bilateral_feed_record` → đối soát `GET /v1/feeds/{hợp đồng}/checksums?pair=&from=&to=` (`bilateral_day.v1`, ngày = `recorded_at` của sổ).
4. **Kiểm tra:** `SELECT status, problem_code, count(*) FROM mkg_fieldops.bilateral_send GROUP BY 1, 2` (cụm mkg); log `job.fieldops_bilateral`, `fieldops.bilateral_checksum_compare` (`outcome=match`). Cụm pf: `max(seq)` của `pf_coop.bilateral_record` theo `pair_id` bằng `pf_apiclient.feed_head.last_seq` (`feed = 'bilateral'`, `partition_key = pair_id`) sau mỗi lượt dựng feed.
5. **Sự cố:** `fieldops.bilateral_checksum_mismatch` → chạy lại job (kéo feed), xem `not_in_feed`; nếu đầu feed < `max(seq)` của sổ: log `integration.dataset_build_failed dataset=bilateral` ở worker (seq không liên tục / bản ghi lệch hợp đồng). Từ chối `integration.pair_missing` tự xếp lại sau 1 giờ (cặp lập muộn). Lô `dead_lettered` → hàng gửi về pending, gửi lô mới.
