# Giám sát đồng bộ tích hợp, hàng chờ lỗi (DLQ) và cảnh báo Admin

Áp dụng cho GIS US-SYNC-01 (FN-22), GIS US-SYNC-02 (FN-23), CGH FN-07 (BR-04, AC-02, AC-03) và R-62.

Mỗi luồng đồng bộ theo sự kiện là một **consumer** của `cmd/worker` (danh sách: `assembly.Queues()`), ví dụ `pf.mechanization.area_sync` (App HTX → Bản đồ CGH), `pf.geo.hub_valve` (ERP/MKG → GIS qua van công bố), `pf.cropcycle.gps_log`. Mỗi consumer có quorum queue `<consumer>` (`x-delivery-limit` 5) và hàng chờ lỗi `<consumer>.dlq` ([rabbitmq-queues.md](rabbitmq-queues.md)).

## 1. Dữ liệu giám sát

| Việc | Ghi ở đâu | Ghi chú |
|---|---|---|
| Xử lý thành công | `shared.inbox` (`processed_at`, `consumer`, `event_type`) | cùng giao dịch với kết quả (R-61) |
| Xử lý thất bại (mỗi lần giao) | `pf_sync.integration_failure` (migration pf 000034) | worker ghi bằng vai `sync_app` trong giao dịch riêng, sau khi giao dịch của handler rollback. Chỉ lưu **mã lỗi đã chuẩn hóa**: mã problem (`validation_failed`, `mechanization.state_conflict`…), `db:<SQLSTATE>`, `timeout`, `canceled`, `internal` — không lưu thông điệp tự do (R-14) |
| Lần giao cuối (vào DLQ) | cùng bảng, `dead_lettered = true`, tối đa một dòng cho (sự kiện, consumer) | `x-delivery-count ≥ 5` ở lần giao đang xử lý → NACK lần này đưa thông điệp sang `<consumer>.dlq` |
| Độ sâu DLQ | metric `ag.rabbitmq.dlq.depth{queue}` (worker) và màn giám sát (api-platform) | cần `AG_RABBITMQ_MGMT_URL` ở cả hai tiến trình |
| Diện tích vụ App HTX (CGH FN-07) | `pf_mechanization.area_sync_log` (migration pf 000035) | mỗi lần nhận/nhập: nguồn App HTX/Nhập tay, kết quả QT-03 (`applied`, `kept_locked`, `stale_ignored`) |

Màn **Giám sát đồng bộ** (`/quan-tri/giam-sat-dong-bo`, vai `platform_admin`, `device_admin`) đọc:

- `GET /v1/sync/integrations?window_hours=24` (Admin API): mỗi consumer một dòng — nguồn, trạng thái (`ok` / `error` / `idle`), số sự kiện đã xử lý trong cửa sổ, lần thành công gần nhất, số lần lỗi, số sự kiện vào DLQ, mã lỗi gần nhất, độ sâu DLQ hiện tại (`null` khi chưa cấu hình management API).
- `GET /v1/sync/integrations/log?consumer=&status=ok|error&from=&to=`: nhật ký từng sự kiện (thời điểm, nguồn, loại sự kiện, số bản ghi, thành công/lỗi, mã lỗi, lần giao), mới nhất trước.

Trạng thái `error` khi: lần thất bại gần nhất muộn hơn lần thành công gần nhất, hoặc có sự kiện vào DLQ trong cửa sổ, hoặc DLQ đang có thông điệp.

## 2. Cảnh báo Admin (trong ứng dụng)

1. Lần giao cuối thất bại → pf/sync ghi dòng `dead_lettered` và phát `pf.sync.integration.dead_lettered` (schema `contracts/events/pf.sync.integration.dead_lettered.v1.json`) đúng một lần cho (sự kiện, consumer).
2. Consumer `pf.notify.integration` tạo **thông báo khẩn** kind `integration_dead_lettered` tới chủ đề toàn quốc vai `platform_admin` (chuông web, đẩy tới thiết bị đã đăng ký). Nội dung: nguồn, loại sự kiện, số lần thử, mã lỗi.
3. Job `mechanization_area_feed_check` (worker, leader, mỗi giờ): tenant đã từng nhận dữ liệu App HTX mà lần nhận gần nhất cũ hơn `cgh.area_feed_stale_hours` (tham số có phê duyệt, mặc định 48 giờ) → ghi `pf_mechanization.area_feed_alert` cho ngày nghiệp vụ và phát `pf.mechanization.area_feed.stale` → thông báo kind `area_feed_stale` tới vai `cgh_admin`. Tối đa **một lần mỗi tenant mỗi ngày**. Màn Bản đồ CGH đồng thời hiện banner "đang dùng dữ liệu nhập tay dự phòng" (`GET /v1/mechanization/area-sync-status`).

## 3. Luật cảnh báo vận hành (Prometheus / OpenTelemetry)

Các tiến trình Go xuất metric qua OTLP (`AG_OTLP_ENDPOINT`, ADR-0013). Ở môi trường có Prometheus trong lãnh thổ, thêm exporter Prometheus vào OpenTelemetry Collector (Local/Dev `deploy/compose/otel/otel-collector.yaml` chỉ có exporter `debug`):

```yaml
exporters:
  prometheus:
    endpoint: 0.0.0.0:8889          # Prometheus scrape collector:8889
    namespace: ""                   # giữ nguyên tên: ag.rabbitmq.dlq.depth → ag_rabbitmq_dlq_depth
    resource_to_telemetry_conversion: {enabled: true}
service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [prometheus]
```

Luật cảnh báo (tệp `agrigreen-integration.rules.yml` của Prometheus):

```yaml
groups:
  - name: agrigreen-integration
    rules:
      # GIS US-SYNC-02 / R-62: có thông điệp trong bất kỳ DLQ nào.
      - alert: AgrigreenDLQNotEmpty
        expr: max by (queue) (ag_rabbitmq_dlq_depth) > 0
        for: 5m
        labels:
          severity: page
          rule: R-62
        annotations:
          summary: "DLQ {{ $labels.queue }} có {{ $value }} thông điệp"
          description: >-
            Sự kiện đồng bộ đã hết 5 lần thử. Mở màn Giám sát đồng bộ (/quan-tri/giam-sat-dong-bo),
            xem mã lỗi của nguồn tương ứng, xử lý theo docs/runbooks/integration-alerts.md mục 4.
      # Metric DLQ biến mất (worker không đọc được management API) — không để cảnh báo trên im lặng.
      - alert: AgrigreenDLQMetricAbsent
        expr: absent(ag_rabbitmq_dlq_depth)
        for: 15m
        labels:
          severity: ticket
        annotations:
          summary: "Không có metric ag.rabbitmq.dlq.depth — kiểm AG_RABBITMQ_MGMT_URL của worker"
      # Job kiểm độ tươi / job định kỳ lỗi (CGH FN-07 AC-02).
      - alert: AgrigreenJobFailed
        expr: increase(ag_job_runs_total{outcome="error"}[1h]) > 0
        labels:
          severity: ticket
        annotations:
          summary: "Job {{ $labels.job }} chạy lỗi trong 1 giờ qua"
```

Alertmanager định tuyến `severity: page` tới trực vận hành (kênh trong lãnh thổ, RB-7). Tên metric Prometheus là tên OTel thay dấu chấm bằng gạch dưới; bộ đếm có hậu tố `_total`. Kiểm luật bằng `promtool check rules agrigreen-integration.rules.yml`.

## 4. Xử lý khi có sự kiện trong DLQ

1. **Xác định nguồn và lỗi**: màn Giám sát đồng bộ → dòng có trạng thái "Lỗi" → nhật ký lọc `status=error`: `event_type`, `event_id`, mã lỗi. Log worker cùng `event_id` (`outcome=dead_letter`) có `trace_id` để tra trace.
2. **Sửa nguyên nhân**: lỗi dữ liệu (`validation_failed`, mã `<module>.*`) → sửa dữ liệu nguồn hoặc dữ liệu chủ (vd HTX ngừng hoạt động, vụ không có trong danh mục); lỗi hạ tầng (`db:*`, `timeout`) → khắc phục DB/mạng. Lỗi do mã → bản vá.
3. **Đưa lại vào hàng đợi chính** (sau khi đã sửa): dùng management UI (Queues → `<consumer>.dlq` → *Move messages* tới `<consumer>`) hoặc shovel tạm:
   ```
   rabbitmqctl set_parameter shovel dlq-replay '{"src-queue":"<consumer>.dlq","dest-queue":"<consumer>","src-delete-after":"queue-length"}'
   ```
   Consumer idempotent theo `event_id` (inbox) nên đưa lại an toàn. **Không xóa** DLQ hay thông điệp trong DLQ bằng tay (R-62) — thông điệp không thể xử lý được thì giữ lại, ghi quyết định vào sổ sự cố.
4. Theo dõi màn Giám sát đồng bộ: trạng thái trở về "Bình thường" khi DLQ = 0 và lần thành công gần nhất muộn hơn lần lỗi gần nhất.

## 5. Dữ liệu App HTX quá hạn độ tươi (CGH FN-07)

- Kiểm App HTX còn gửi `pf.cropcycle.coop_season_area.changed` không (nhật ký `GET /v1/mechanization/area-sync-log`, màn giám sát nguồn "App HTX → Bản đồ CGH").
- Trong lúc chờ, Admin CGH nhập tay diện tích vụ và **khóa** bản ghi ở màn Bản đồ CGH (QT-03: bản khóa không bị App HTX ghi đè); khi App HTX gửi lại dữ liệu hợp lệ, mở khóa để ưu tiên App HTX như cũ.
- Ngưỡng đổi bằng tham số `cgh.area_feed_stale_hours` (Tham số & phê duyệt, R-56).
