# Nhận số đo cân — `cmd/ingest`, sổ thiết bị, vhost `mkg.internal`, tầng lạnh `ts`

Áp dụng cho vùng mkg G3 (mục 13.2, 14, 17, 19, 23; ADR-0017, 0018, 0019). Agent biên (cài, cấu hình, cập nhật) xem [edge-agent.md](edge-agent.md); runbook này là phía **máy chủ**: dịch vụ ingest, đăng ký/duyệt thiết bị, thay khóa, mất đĩa, hàng đợi nội bộ, cảnh báo và chuyển tầng lạnh.

## 1. Dịch vụ `ingest`

| Mục | Giá trị |
|---|---|
| Image | `docker build --build-arg CMD=ingest .` (CI job `images`, `agrigreen-ingest`) |
| Cổng vận hành | `AG_HTTP_ADDR` (`:8091`): `/healthz`, `/readyz` (mọi pool ping được). Chỉ trong cụm — Compose không publish |
| Cổng nhận lô | `AG_INGEST_HTTP_ADDR` (`:8443`): `POST /v1/ingest/batches`, mTLS **bắt buộc** (`AG_INGEST_TLS_CERT`, `AG_INGEST_TLS_KEY`, `AG_INGEST_CLIENT_CA`) |
| DB | `AG_TS_DB_URL` → `ag_ts` vai `ingest_ts` (ghi) + `ts_reader` (đọc cửa sổ); `AG_MKG_DB_URL` → `ag_mkg` vai `ingest_mkg` (sổ thiết bị, phiếu cân, outbox). Hai pool, không câu SQL nào chạm hai cụm (R-07). Qua PgBouncer: `AG_DB_VIA_PGBOUNCER=1` |
| Tham số tạm | `AG_INGEST_RATE_PER_MIN` (120), `AG_INGEST_RATE_BURST` (60), `AG_INGEST_SKEW_FLAG_MS` (2000, tạm), `AG_INGEST_INTEGRATE_INTERVAL` (15s), `AG_INGEST_STUCK_AFTER` (30m, tạm) |
| Healthcheck (distroless) | `/app -healthcheck http://127.0.0.1:8091/readyz` |

**Phơi mạng.** 8443 chỉ được nghe trên mạng OT/VPN của hub:

- Local/Dev: `127.0.0.1:8443` trên máy; agent mô phỏng gọi `https://ingest:8443` trong mạng compose.
- VPS (`docker-compose.vps.yml`): đặt `AG_INGEST_BIND_ADDR` trong `.env` = IP giao diện WireGuard/VPN của VPS (ví dụ `10.88.0.1`), tường lửa (ufw/nftables) chỉ mở `8443/tcp` từ dải VPN của các hub. **Không** thêm ingest vào Caddy, không bind `0.0.0.0` — thiết bị biên không đi qua internet công cộng (mục 17).
- Production: listener sau VPN site-to-site tới vùng OT.
- CA: `AG_INGEST_CLIENT_CA` chỉ chứa **CA thiết bị riêng**, dev cũng vậy (`deploy/compose/.certs/device-ca.crt`). Chứng thư dịch vụ nội bộ như `optimizer` do CA dịch vụ ký nên **không bắt tay được** với 8443.
- Ingest từ chối khởi động nếu CA client đã ký chứng thư máy chủ. Kiểm này luôn bật ở `AG_ENV=production`, và bật ở Compose qua `AG_INGEST_REQUIRE_DEVICE_CA=1`. Dựng và vận hành CA thiết bị trong lãnh thổ: [device-pki.md](device-pki.md).

## 2. Compose Local/Dev: agent mô phỏng đầu-cuối

```
sh scripts/gen-dev-secrets.sh                # .env + chứng thư (gen-dev-certs: ingest.*, device-ca.*, device-edge-sim-01.*)
docker compose -f deploy/compose/docker-compose.yml up -d --build --wait
sh scripts/dev-edge-bootstrap.sh --wait 120  # đề xuất + duyệt kỳ, chờ lô đầu tiên vào ts_weigh
```

Các bước dựng (một lần, chạy lại an toàn):

1. **`edge-sim-init`** (busybox, root): chép `deploy/compose/.certs/device-edge-sim-01.{crt,key}` vào volume `edge_sim_data` (`tls/device.*`), khóa `0600` chủ `65532`. Agent từ chối khóa đọc được bởi người khác, mà tệp bind từ máy là `0644`.
2. **`edge-sim-enroll`**: `edge-agent enroll -config /etc/edge-agent/enroll.json -epoch 1 -skip-if-enrolled`. Lần đầu sinh khóa ký nguồn Ed25519 và `state.json` kỳ 1 trong volume; các lần sau in kỳ hiện tại và thoát 0. `enroll` cũng sinh `client.key`/`client.csr` — ở dev **không dùng**: chứng thư client lấy từ `gen-dev-certs` (CN = SAN = `edge-sim-01`, do `device-ca` ký) thay cho việc CA thiết bị ký CSR.
3. **`edge-sim`**: `edge-agent run -config /etc/edge-agent/run.json`, bộ điều hợp `simulator` 2 Hz, đẩy tới `https://ingest:8443/v1/ingest/batches`, `backoff_max` 10 s. Trước khi sổ thiết bị có kỳ đã duyệt, ingest trả `403 ingest.unknown_device` → agent giữ đệm, báo động `ingest_rejected`, thử lại mỗi 10 s.
4. **`scripts/dev-edge-bootstrap.sh`** (trên máy, cần `curl`, `jq`):
   - tạo hai danh tính dịch vụ dev trong realm bằng `kcadm` (không sửa tệp realm): `agrigreen-dev-edge-proposer` (vai `scale_supervisor`) và `agrigreen-dev-edge-approver` (vai `device_approver`) — hai `sub` khác nhau nên đúng R-56. Token có `tenant_id` MKG dev cố định (`MKG_TENANT`) và `amr [pwd, otp]` vì Admin API bắt buộc MFA; claim này **chỉ** ở realm Local/CI;
   - đọc `device_id`, `epoch`, `public_key_base64`, `key_id`, `cert_sha256` bằng `edge-agent info` trong container (khóa riêng không rời volume);
   - `POST /v1/measurement/devices` (8083) bằng người đề xuất, rồi `POST …/epochs/<n>/decision {"decision":"approved"}` bằng người duyệt; kiểm lại `GET /v1/measurement/devices/<id>` (8081) là `active`;
   - `--wait N`: chờ tới khi `ts_weigh.reading_batch` có lô của thiết bị (psql superuser trong container postgres).

   Kỳ đã `active` cùng khóa và chứng thư thì bỏ qua. CI `compose-smoke` chạy script này rồi kiểm `raw_reading`, `acked_counter > 0` và consumer trên `mkg.internal`.

Sinh lại chứng thư (`gen-dev-certs --force`) hoặc xóa volume `edge_sim_data` tương đương **thay chứng thư / mất đĩa**: kỳ cũ trong sổ không còn khớp, script dừng và in lệnh chuyển kỳ (mục 4, 5). Cách nhanh nhất ở dev là `docker compose down -v` (xóa cả DB).

## 3. Đăng ký và duyệt thiết bị (hai người, R-56)

Vai: `scale_supervisor` đề xuất, `device_approver` duyệt (**khác người**; DB có CHECK, app trả `self_approval`). Bề mặt: Admin API vùng mkg (MFA, `api-internal` `:8083`), web vận hành khi có màn.

1. Kỹ thuật viên tại hub cài agent và chạy `edge-agent enroll -epoch <N>` ([edge-agent.md](edge-agent.md) mục 3). `N` = kỳ lớn nhất trong sổ + 1 (thiết bị mới: 1). Gửi cho giám sát cân qua kênh nội bộ: `device_id`, `epoch`, `public_key_base64`, `key_id`, `csr_sha256`, CSR.
2. **CA thiết bị** ([device-pki.md](device-pki.md) mục 4) ký CSR sau khi kiểm `csr_sha256` khớp (gọi điện/đối chiếu trực tiếp — không tin tệp gửi qua email). Subject: CN = SAN DNS = `device_id`, EKU `clientAuth`, hạn ≤ 825 ngày. Đặt chứng thư vào `ingest.cert_file`; `edge-agent info` → `cert_sha256`.
3. `scale_supervisor` đề xuất:
   ```
   POST /v1/measurement/devices            Idempotency-Key: <khóa>
   {"device_id":"scale-hub01-in","epoch":N,"facility_ref":"<hub>","point":"HUB_IN",
    "public_key":"<base64 32 byte>","cert_sha256":"<hex>","scale_model":"<mẫu cân — C-6>"}
   ```
4. `device_approver` (người khác) đối chiếu `key_id`, `cert_sha256`, cơ sở, điểm cân với biên bản lắp đặt rồi duyệt:
   `POST /v1/measurement/devices/<id>/epochs/<N>/decision {"decision":"approved"}` (từ chối: `"rejected"` + `reason` 10..500 ký tự). Duyệt kỳ mới tự chuyển kỳ cũ sang `superseded`.
5. `systemctl enable --now edge-agent`. Kiểm: `GET /v1/measurement/devices/<id>` có `health.last_seen_at` mới; cảnh báo `AgrigreenEdgeAgentDisconnected` không bật.

Thu hồi ngay (nghi lộ khóa, thiết bị mất): `POST …/epochs/<N>/revoke {"reason":"…"}` — một người (`device_approver` hoặc `scale_supervisor`) vì chỉ giảm tin cậy; lô mới bị `403 ingest.device_revoked`. Thu hồi luôn chứng thư ở CA (CRL). Lô đã nhận trước đó giữ nguyên (RB-1); nếu nghi giả mạo, mở tranh chấp cho các lô liên quan.

## 4. Thay khóa định kỳ / gia hạn chứng thư

Mỗi lần thay khóa là **một kỳ mới**, bộ đếm bắt đầu lại từ 1 và không bao giờ dùng lại (ADR-0019 §5). Chu kỳ đề xuất: trước khi chứng thư hết hạn 30 ngày, hoặc ngay khi nghi lộ khóa.

1. Chọn thời điểm không có phiên cân mở trên thiết bị (màn phiên cân; `GET /v1/measurement/sessions/<id>`); phiên đang mở phải đóng ở kỳ cũ.
2. Chờ `edge_unacked_readings = 0` (đệm đã đẩy hết), dừng dịch vụ.
3. `edge-agent enroll -rotate -epoch <N+1>` — khóa, chứng thư, đệm, state cũ đổi tên `*.retired-e<N>-…`, không xóa.
4. Lặp mục 3 bước 2–4 cho kỳ `N+1` (CSR mới, đề xuất, người khác duyệt). Kỳ `N` tự `superseded`.
5. Khởi động agent. Lô kỳ `N` gửi muộn (nếu có) bị từ chối — vì vậy bước 2 bắt buộc.

Chứng thư máy chủ ingest: thay tệp `AG_INGEST_TLS_CERT/KEY` rồi khởi động lại ingest (agent tự kết nối lại, lô chờ gửi lại byte-cho-byte, idempotent). Đổi CA thiết bị: phát hành CA mới, để ingest tin **cả hai** CA (gộp PEM trong `AG_INGEST_CLIENT_CA`) trong thời gian chuyển, thay khóa từng thiết bị theo trên, rồi gỡ CA cũ.

## 5. Mất đĩa, cài lại → kỳ mới

| Agent báo | Việc |
|---|---|
| `ErrStateLost`, `ErrCounterGap`, `ErrKeyLost`, WAL hỏng | `edge-agent enroll -epoch <N+1> -abandon-buffer` (thêm `-rotate` nếu còn state). Đệm cũ được lưu trữ, **không gửi được** |
| Máy/đĩa mới hoàn toàn (`ErrNotEnrolled`) | `edge-agent enroll -epoch <N+1>` |

Sau đó đăng ký kỳ mới như mục 3. Ghi sự cố:

- khoảng bộ đếm cuối cùng ingest đã nhận của kỳ `N` (`SELECT max(counter_to) FROM ts_weigh.reading_batch WHERE device_id=… AND epoch=N`, vai `ts_reader`) so với `last_counter` agent báo lần cuối (`device_current.last_counter`) — phần chênh là số đo mất;
- phiên cân của khoảng mất: `ag.ingest.integration.stuck` sẽ bật cho phiên có cửa sổ không đủ lô. Xử lý bằng phiếu nhập tay có lý do (Q-MKG-04, cân hỏng) — **không** sửa bảng `ts_weigh`;
- thư mục `*.retired-*` giữ trên thiết bị (hoặc chép về kho pháp chứng) tới hết cửa sổ kiểm toán (C-7).

Không bao giờ sửa tay `state.json`, chép WAL từ máy khác hay đặt lại kỳ cũ: làm vậy là dùng lại bộ đếm, ingest sẽ trả 409 và dữ liệu mất giá trị chứng cứ.

## 6. Vhost `mkg.internal` (ADR-0018)

### 6.1 Kiểm tra

```
rabbitmqctl -q list_permissions -p mkg.internal         # CHỈ mkg_internal
rabbitmqctl -q list_queues -p mkg.internal name messages consumers
```

Mọi tên trong `cmd/worker-internal/assembly.Queues()` và `<tên>.dlq` phải có (outbox-publisher khai báo khi khởi động); hàng đợi chính có `consumers ≥ 1` khi `worker-internal` chạy. Vhost nền tảng `/` không có hàng đợi `mkg.*`.

### 6.2 Tài khoản và quyền

- Tạo/cập nhật: Compose `rabbitmq-init` (management API, idempotent) — vhost `mkg.internal` (quorum mặc định), user `mkg_internal`, quyền `.*` trên vhost này, rồi **gỡ** quyền RabbitMQ tự cấp cho người tạo (`agrigreen`).
- Đổi mật khẩu `AMQP_INTERNAL_PASSWORD`: đặt mật khẩu mới trong `.env`/kho bí mật → chạy lại `rabbitmq-init` (PUT user) → khởi động lại `outbox-publisher` và `worker-internal`. Trong lúc chuyển, sự kiện nội bộ tồn trên outbox cụm mkg (không mất, không rơi sang vhost nền tảng).
- Không bao giờ cấp cho `agrigreen` quyền trên `mkg.internal`; CI `compose-smoke` hỏng nếu có.
- Production: bật plugin `rabbitmq_prometheus`; scrape `/metrics/detailed?vhost=mkg.internal&family=queue_coarse_metrics&family=queue_consumer_count` cho luật mục 7.

### 6.3 DLQ

Như [integration-alerts.md](integration-alerts.md) mục 4 nhưng trên vhost `mkg.internal` và bằng tài khoản `mkg_internal` (màn Giám sát đồng bộ của nền tảng **không** thấy các hàng đợi này — đúng thiết kế). Tìm nguyên nhân qua log `worker-internal` (`outcome=dead_letter`, `event_id`), sửa, rồi đưa lại:

```
rabbitmqctl set_parameter -p mkg.internal shovel dlq-replay \
  '{"src-queue":"mkg.hub.weigh_ticket.dlq","dest-queue":"mkg.hub.weigh_ticket","src-delete-after":"queue-length"}'
```

Consumer idempotent theo `event_id` (shared.inbox cụm mkg, R-61). Không xóa DLQ/thông điệp bằng tay (R-62).

## 7. Cảnh báo

Luật: [`deploy/observability/prometheus/agrigreen-mkg-ingest.rules.yml`](../../deploy/observability/prometheus/agrigreen-mkg-ingest.rules.yml) (`promtool check rules` trong CI). Nguồn:

- `ag_ingest_*`: OTLP của ingest → OpenTelemetry Collector với exporter prometheus ([integration-alerts.md](integration-alerts.md) mục 3; Local/Dev chỉ có exporter `debug`);
- `edge_*`: `/metrics` cục bộ của agent — Prometheus agent tại hub remote-write qua VPN;
- `ag_mkg_*`: sql_exporter, target `ag_mkg`, vai `mkg_monitor`, collector [`mkg-measurement.collector.yml`](../../deploy/observability/sql-exporter/mkg-measurement.collector.yml).
- `ag_ts_*`: sql_exporter, target `ag_ts`, vai `ts_monitor`, collector [`ts-weigh.collector.yml`](../../deploy/observability/sql-exporter/ts-weigh.collector.yml).
- Cách hai vai giám sát (mkg 000013, ts 000003) đếm xuyên tenant:
  - vai NOBYPASSRLS, **không** SELECT được bảng gốc;
  - chỉ EXECUTE các hàm gộp SECURITY DEFINER trong schema `mkg_monitor` / `ts_monitor`, chỉ trả số đếm/tuổi, không tenant, không khối lượng;
  - chủ hàm là `mkg_monitor_owner` / `ts_monitor_owner` (NOLOGIN, BYPASSRLS), chỉ được GRANT theo cột vận hành.

  Kiểm thử: `TestMonitorRolesCannotReadBaseTables`, `TestMonitorFunctionsAggregateAcrossTenants`. Thêm chỉ số mới là thêm hàm trong migration mới, không cấp SELECT bảng cho vai giám sát;
- `rabbitmq_detailed_*`: plugin `rabbitmq_prometheus` (mục 6.2).

| Luật | Ngưỡng | Mức | Xử lý |
|---|---|---|---|
| `AgrigreenEdgeBufferHigh` / `…Reported` | đệm > 70 % trong 15 phút | P3 | 7.1 |
| `AgrigreenEdgeBufferCritical` / `…Reported` | đệm > 90 % trong 5 phút | P1 | 7.1 |
| `AgrigreenEdgeAgentDisconnected` | kỳ `active` không có lô > 15 phút (tạm) | **P2** (§23) | 7.2 |
| `AgrigreenIngestNoAcceptedBatches` | 0 lô nhận trong 15 phút khi có thiết bị active | P2 | 7.2 |
| `AgrigreenIngestBatchesRejected` | lô bị từ chối theo `code` | P3 | 7.3 |
| `AgrigreenIngestCounterGap` | khoảng trống bộ đếm | P3 | mục 5 |
| `AgrigreenIngestClockSkewFlagged` | > 5 % lô gắn cờ lệch đồng hồ trong 30 phút (tạm) | P3 | 7.5 |
| `AgrigreenIngestIntegrationStuck` | `ag.ingest.integration.stuck` tăng | P2 | 7.4 |
| `AgrigreenOverToleranceRateHigh` | > 10 % đối soát 24 giờ vượt dung sai (§19; tạm — C-4) | P3 | 7.6 |
| `AgrigreenOverToleranceCritical` | có đối soát `critical` | P3 | 7.6 |
| `AgrigreenDisputesOpenHigh` / `…OpenTooLong` | > 10 tranh chấp mở / mở > 7 ngày (tạm) | P3 | 7.6 |
| `AgrigreenMkgInternalDLQNotEmpty` | DLQ `mkg.internal` > 0 trong 5 phút (R-62) | P2 | 6.3 |
| `AgrigreenMkgInternalNoConsumer` | hàng đợi chính 0 consumer 10 phút | P2 | khởi động/kiểm `worker-internal` |

**7.1 Đệm đầy.** Đường đẩy hỏng: VPN, ingest down, lô bị từ chối liên tục (xem `edge_alarm` tại chỗ). Khôi phục đường đẩy trước; agent tự xả đệm theo thứ tự bộ đếm. Không giảm `buffer.hours` dưới 72, không xóa WAL.

**7.2 Mất kết nối.** `curl 127.0.0.1:9108/healthz` qua VPN tại hub; `journalctl -u edge-agent`; ping ingest từ hub; log ingest `outcome=rejected` với `device_id`. Ingest không nhận lô nào → kiểm `/readyz`, pool `ag_ts`/`ag_mkg`, chứng thư máy chủ hết hạn.

**7.3 Lô bị từ chối.** `ingest.unknown_device` (chưa duyệt / sai kỳ), `ingest.cert_mismatch` (chứng thư mới chưa có trong sổ → kỳ mới, mục 4), `ingest.device_revoked`, `ingest.device_mismatch` (CN/SAN ≠ `device_id`), `edge.*` (hợp đồng/chữ ký — bản agent lạ hoặc bị giả, cách ly). `ingest.rate_limited` (429) không phải từ chối: agent lùi lại và gửi lại.

**7.4 Phiên kẹt.** Phiên đã đóng nhưng cửa sổ bộ đếm thiếu lô (agent còn đệm, mất đĩa) hoặc không có số đo `stable`. Chờ agent xả đệm; nếu mất hẳn → phiếu nhập tay có lý do (Q-MKG-04).

**7.5 Lệch đồng hồ.** Chỉ ảnh hưởng `device_time` (thông tin). Kiểm chrony/NTP nội bộ tại hub, `edge_ntp_synced`.

**7.6 Dung sai, tranh chấp.** Nghiệp vụ: `reconciliation_officer` / `dispute_officer` xử lý theo màn đối soát/tranh chấp. Tỉ lệ vượt dung sai tăng đột ngột ở một hub → nghi cân lệch chuẩn: đối chiếu `ag_mkg_device_*` của hub đó, yêu cầu kiểm định cân.

## 8. Tầng lạnh phân vùng `ts_weigh` (ADR-0017, R-71) — CHỜ C-7

Cửa sổ kiểm toán (bao lâu số đo thô ở tầng nóng) là câu hỏi khách **C-7, chưa có số**. Tới khi có: **không** tách phân vùng nào, mọi dữ liệu ở tầng nóng. Quy trình dưới đây là bản chuẩn bị, chạy **thủ công, hai người** (người thực hiện + người xác minh), cho tới khi có job. Chuyển tầng không phải xóa (RB-1): phân vùng chỉ được bỏ sau khi bản xuất đã xác minh và khôi phục thử.

Phạm vi: `ts_weigh.raw_reading` (phân vùng tháng `ts_weigh.raw_reading_YYYYMM` theo `received_at` UTC). `ts_weigh.reading_batch` không phân vùng (khóa idempotency toàn cục, nhỏ): **giữ ở tầng nóng**, chỉ xuất kèm các dòng của tháng để bản lạnh tự kiểm được chữ ký.

1. **Chuẩn bị.** Tháng `M` đã quá cửa sổ C-7; phiếu cân của mọi phiên trong tháng đã ghi (không còn phiên kẹt đọc cửa sổ của `M`); có sao lưu pgBackRest mới (full) chứa `M`.
2. **Tách** (vai `ag_migrator`, cửa sổ bảo trì):
   ```sql
   ALTER TABLE ts_weigh.raw_reading DETACH PARTITION ts_weigh.raw_reading_202609;   -- không CONCURRENTLY: bảng có phân vùng DEFAULT
   ```
   Bảng tách ra vẫn mang trigger chặn UPDATE/DELETE/TRUNCATE và RLS; ingest không ghi vào tháng cũ (received_at = now()).
3. **Xuất nén** (người thực hiện; tài khoản BYPASSRLS chỉ đọc hoặc superuser trong giao dịch `READ ONLY`):
   ```
   psql … -c "\copy (SELECT * FROM ts_weigh.raw_reading_202609 ORDER BY tenant_id, device_id, epoch, counter) TO STDOUT WITH (FORMAT csv, HEADER)" | zstd -19 > raw_reading_202609.csv.zst
   psql … -c "\copy (SELECT * FROM ts_weigh.reading_batch WHERE received_at >= '2026-09-01' AND received_at < '2026-10-01' ORDER BY device_id, epoch, counter_from) TO STDOUT WITH (FORMAT csv, HEADER)" | zstd -19 > reading_batch_202609.csv.zst
   ```
   `manifest.json`: cụm, bảng, tháng, số dòng mỗi tệp (`SELECT count(*)`), SHA-256 từng tệp, `min/max(received_at)`, phiên bản migration, người thực hiện. **Ký** Ed25519 như manifest neo hằng ngày (`opsjobs.SignManifest`, khóa `AG_ANCHOR_SIGNING_KEY`, cùng dạng kiểm của `cmd/drill`) — tạm thời bằng `openssl pkeyutl -sign -rawin` với khóa ký neo trên máy quản trị. Tải ba tệp + chữ ký vào kho đối tượng **trong lãnh thổ** có khóa đối tượng (object lock, RB-7, mục 22), tiền tố `cold/ts_weigh/raw_reading/2026/09/`, credential không có quyền xóa.
4. **Xác minh** (người xác minh, khác người thực hiện):
   - tải lại từ kho, kiểm chữ ký manifest và SHA-256 từng tệp;
   - **khôi phục thử** vào database nháp: `CREATE TABLE … (LIKE ts_weigh.raw_reading)`, `\copy … FROM` bản giải nén; so số dòng với manifest và với bảng đã tách; so `sum(hashtext(row::text))` giữa bảng nháp và bảng tách;
   - kiểm lại **chữ ký nguồn** một mẫu lô (≥ 1 % lô, tối thiểu 20, chọn ngẫu nhiên): dựng dạng chuẩn `edgewire` từ `reading_batch` + `raw_reading` của bản khôi phục, `edgewire.Verify` bằng khóa công khai của kỳ trong sổ thiết bị. Công cụ dòng lệnh cho bước này **chưa có** (việc mở: lệnh con kiểu `drill verify-ts-export`); tới lúc đó dùng kiểm thử Go tạm dựa trên `internal/mkg/measurement/port/edgewire`;
   - ghi biên bản (manifest, kết quả, hai người ký) vào nhật ký vận hành.
5. **Bỏ bảng đã tách** — chỉ khi bước 4 đạt và sau một chu kỳ sao lưu full nữa: cần tắt trigger chặn trên bảng đã tách bằng thay đổi có phê duyệt (migration/ADR bổ sung khi có C-7 — hiện **chưa** có đường nào được phép). Tới lúc đó giữ bảng đã tách trong schema lưu trữ riêng (`ALTER TABLE … SET SCHEMA ts_weigh_archive`), vẫn đọc được bằng `ts_reader` nếu cấp.
6. **Truy xuất lại** (tranh chấp, kiểm toán): tải bản lạnh, kiểm chữ ký manifest, khôi phục vào database nháp như bước 4; không gắn lại vào bảng nóng.

Diễn tập: mỗi quý khôi phục thử một bản lạnh ngẫu nhiên cùng lúc với diễn tập phục hồi ([restore-drill.md](restore-drill.md)).
