# Webhook mỏng, ngắt mạch và đối soát của cổng tích hợp (W5.7)

Nguồn: [ADR-0033](../adr/0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md) §4 (Hiện thực W5.7), R-INT-07, R-INT-09, R-INT-10. Mã: `internal/platform/apiclient/webhook` (Guard, chữ ký, ngắt mạch, bộ giao), Admin API `internal/platform/apiclient/handler/webhooks.go`, cổng `internal/platform/apiclient/public/{webhooks,checksums}.go`, job `cmd/worker/webhooks.go`. Migration pf 000130. Lỗi: [errors.md](../errors.md#cổng-tích-hợp--webhook-mỏng-ngắt-mạch-đối-soát-w57-adr-0033-4). Chung về cổng: [integration-gateway.md](integration-gateway.md).

## 1. Luồng

1. Quản trị viên đơn vị vận hành (MFA, `api_client_admin`) đăng ký: `POST /v1/api-clients/{id}/webhooks` `{url, feeds, failure_threshold?}` + `Idempotency-Key`. Phản hồi 201 có `secret` (`whsec_…`) **một lần** — chuyển cho đối tác qua kênh an toàn như khóa API.
2. Job dựng feed (W5.3) nối mục mới → `feed_head.last_seq` tăng.
3. Job `apiclient_webhook_dispatch` (leader `cmd/worker`) thấy `last_seq` > seq đã báo → `POST <url>` thân `{"feed","cursor","occurred_at"}`, header `AG-Webhook-Id`, `AG-Webhook-Timestamp`, `AG-Webhook-Signature: t=<giây>,v1=<hex>`.
4. Đối tác xác minh chữ ký, trả 2xx, rồi gọi `GET /v1/feeds/{feed}?cursor=<cursor của chính mình>` tới khi `has_more = false`. Chỉ báo bị mất / mạch mở **không mất dữ liệu** — đối tác vẫn có thể thăm dò feed.

Nhiều mục mới giữa hai lượt được gộp thành MỘT chỉ báo (cursor mới nhất).

## 2. Biến môi trường

| Biến | Tiến trình | Mặc định | Ý nghĩa |
|---|---|---|---|
| `AG_WEBHOOK_KEY_V<n>`, `AG_WEBHOOK_KEY_CURRENT` | api-platform, worker | — | Khóa chủ (≥ 32 byte, trình quản lý bí mật) dẫn xuất bí mật webhook. Thiếu: đăng ký/xoay → 503; worker không chạy job (cảnh báo). Đổi khóa: thêm `V<n+1>`, đặt `CURRENT`, GIỮ khóa cũ tới khi mọi webhook đã xoay bí mật |
| `AG_WEBHOOK_ALLOW_HOSTS` | api-platform, worker | — | Danh sách tên miền đích cho phép (dấu phẩy; tên con được nhận). Rỗng = mọi tên miền công khai qua Guard |
| `AG_WEBHOOK_ALLOW_CIDRS` | api-platform, worker | — | CHỈ dev/test: dải miễn chặn SSRF (vd `127.0.0.0/8`) + cho mọi cổng. **Production: từ chối khởi động** |
| `AG_WEBHOOK_DISPATCH_INTERVAL` | worker | `30s` | Chu kỳ job (≥ 5s) |
| `AG_WEBHOOK_BREAKER_COOLDOWN` | worker | `5m` | Chờ trước lần thử half-open đầu (≥ 30s); ×2 mỗi lần thử lỗi, trần 1 giờ |
| `AG_PUBLIC_OPERATOR_TENANT` | worker | — | Không đặt → không job |

Chạy: `cmd/worker -jobs apiclient_webhook_dispatch` (hoặc `all`). **Mạng:** worker cần egress TCP 443/8443 ra Internet (giao trực tiếp, không proxy — Guard kiểm địa chỉ đích thật). Bổ sung NetworkPolicy của worker trước khi bật ở production (chart chưa có).

## 3. Chặn SSRF

- Lúc đăng ký: `https`; cổng 443/8443; không userinfo, fragment; tên máy phải là tên miền đầy đủ, không `localhost`/`.local`/`.internal`/`.lan`/`.svc`/`.cluster.local`/…; IP literal và **mọi** địa chỉ phân giải ngoài: loopback, RFC 1918, `0.0.0.0/8`, `100.64.0.0/10`, `169.254.0.0/16` (metadata đám mây), dải tài liệu/benchmark, multicast, `240.0.0.0/4`, `::/96`, ULA `fc00::/7`, link-local `fe80::/10`, NAT64 `64:ff9b::/96`, 6to4 `2002::/16`, Teredo `2001::/32` (IPv4-mapped được bỏ ánh xạ trước khi so).
- Lúc kết nối: `net.Dialer.Control` kiểm địa chỉ thật sắp nối → tên máy đổi bản ghi DNS sang địa chỉ nội bộ sau khi đăng ký (DNS rebinding) bị chặn: lần giao ghi `blocked` / `blocked_address`, tính vào ngắt mạch.
- Không theo chuyển hướng (3xx = lỗi `redirect`), không proxy, TLS ≥ 1.2, timeout 10 giây, đọc tối đa 4 KiB thân phản hồi rồi bỏ.

## 4. Ngắt mạch

| Trạng thái | Hành vi |
|---|---|
| `closed` | Giao mọi chỉ báo đến hạn; lỗi → lùi `10s × 2^(lần−1)` (nhiễu [d/2, d], trần 10 phút) |
| `open` | Sau N lỗi liên tiếp (mặc định 5, đặt theo webhook). Không giao tới `next_probe_at`. Log cảnh báo `apiclient.webhook_breaker_opened` (host, số lỗi, lớp lỗi) |
| `half_open` | Tới hạn: đúng MỘT lần thử thật. Thành công → `closed` (log `apiclient.webhook_breaker_closed`); lỗi → `open`, thời gian chờ ×2 |

Xem: `GET /v1/api-clients/{id}/webhooks` (Admin — kèm 10 lần giao gần nhất) hoặc đối tác tự xem `GET /v1/webhooks`.

Xử lý khi mạch mở:
1. Đọc `breaker.last_error` + `recent_deliveries[].http_status`: `http_status` 401/403 → đối tác xác minh chữ ký sai (bí mật cũ? lệch đồng hồ > 5 phút?); 5xx/`timeout`/`connect` → đầu nhận của đối tác lỗi; `tls` → chứng chỉ đối tác; `blocked_address`/`url_rejected` → DNS đối tác trỏ vào dải nội bộ hoặc URL không còn qua chính sách — **không** nới chính sách, yêu cầu đối tác sửa.
2. Báo đối tác (kênh quản trị theo hợp đồng). Đối tác vẫn đọc feed bằng thăm dò.
3. Sau khi đối tác xác nhận đã sửa: chờ lần thử half-open tự động, hoặc đóng ngay `POST …/webhooks/{webhook_id}/reset-breaker` `{reason}` (ghi sổ `breaker_reset`).
4. Đối tác ngừng dùng: `POST …/disable` `{reason}`; dùng lại: `…/enable` (mạch đóng lại).

## 5. Xoay bí mật

`POST /v1/api-clients/{id}/webhooks/{webhook_id}/rotate-secret` + `Idempotency-Key` → `secret` mới (một lần). Trong 24 giờ, header có hai `v1` (mới trước, cũ sau) — đối tác chấp nhận nếu một giá trị khớp, đổi bí mật bất kỳ lúc nào trong khoảng đó. Lộ bí mật: xoay ngay, rồi tắt + đăng ký lại nếu cần cắt bí mật cũ trước 24 giờ.

## 6. Mẫu xác minh phía đối tác

```text
header = AG-Webhook-Signature  ("t=1791520000,v1=<hex>[,v1=<hex>]")
signed = t + "." + <thân thô, đúng byte nhận được>
ok     = any(constant_time_eq(hex(HMAC_SHA256(secret_bytes, signed)), v1) for v1 in header)
         && |now − t| ≤ 300 giây
secret_bytes = base64url_decode(secret bỏ tiền tố "whsec_")
```

Idempotent theo `AG-Webhook-Id`. Thư viện Go: `webhook.Verify` (re-export `apiclient.VerifyWebhookSignature`).

## 7. Đối soát (R-INT-10)

`GET /v1/feeds/{feed}/checksums` trên cổng:

- **Tập dữ liệu** (`harvest_progress_by_commune`, `collection_volume_by_commune`, `admin_units`, …): `versions[] = {version, contract_version, built_at, as_of, record_count, sha256}`; `sha256` = SHA-256 của ảnh chụp NDJSON (`GET /v1/snapshots/{dataset}/{version}`), bất biến. Đối tác so số bản ghi + băm của bản sao cục bộ dựng từ feed; lệch → đồng bộ lại từ ảnh chụp.
- **Song phương** (`collection_schedule`, `roll_count_statement`, `htx_roll_confirmation`): `days[] = {date, record_count, sha256}` theo `bilateral_day.v1` — SHA-256 các dòng `<business_key>\t<contract_version>\t<payload_sha256>\n` sắp theo (business_key theo byte, contract_version), `payload_sha256` = SHA-256 của JSON chuẩn bản ghi; ngày = ngày nghiệp vụ (giờ VN) cổng nhận lô. **Tạm** trên sổ đệm `pf_apiclient.ingest_staging` của chính bên gọi — W5.6 chuyển sang sổ song phương, bên HTX tính cùng công thức trên bản ghi của mình.

## 8. Truy vấn hữu ích (tenant đơn vị vận hành)

```sql
-- Webhook đang mở mạch
SELECT w.webhook_id, w.client_id, w.host, b.consecutive_failures, b.last_error, b.next_probe_at
FROM pf_apiclient.webhook_current w JOIN pf_apiclient.webhook_breaker b USING (tenant_id, webhook_id)
WHERE b.state <> 'closed';

-- Tỉ lệ giao 24 giờ qua theo kết cục
SELECT outcome, error_class, count(*) FROM pf_apiclient.webhook_delivery
WHERE event_time > now() - interval '24 hours' GROUP BY 1, 2 ORDER BY 3 DESC;
```

Metric: `ag_apiclient_webhook_deliveries_total{outcome,error_class}`, `ag_apiclient_webhook_delivery_duration_seconds`, `ag_apiclient_webhook_breaker_transitions_total{to}`.
