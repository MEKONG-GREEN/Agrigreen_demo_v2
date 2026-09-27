# ADR-0013: Quan sát: trace_id từ G0, OTLP từ G1

**Trạng thái:** Chấp nhận, đã hiện thực OTLP ở G1 · **Ngày:** 27/09/2026

## Quyết định
- G0: log JSON qua `slog` với các trường chuẩn R-91; `trace_id` theo W3C traceparent, trả về ở header `X-Trace-Id` và trong mọi problem+json (R-63); `trace_id` đi kèm header của thông điệp RabbitMQ.
- G1: thêm OpenTelemetry SDK, xuất OTLP tới Tempo/Prometheus/Loki **tự vận hành trong lãnh thổ** (R-90, RB-7).
- Chỉ số nghiệp vụ bắt buộc có cảnh báo (mục 19) làm cùng module sinh ra chúng.

## Hiện thực G1
- `telemetry.Setup(ctx, service)` đọc `AG_OTLP_ENDPOINT`. Biến này **rỗng thì tắt**: không cài provider, không cài propagator, hành vi giữ y như G0.
  - `AG_OTLP_PROTOCOL`: `grpc` (mặc định) hoặc `http`.
  - `AG_OTLP_INSECURE`, `AG_OTLP_SAMPLE_RATIO`, `AG_ENV`, `AG_VERSION`.
- Chỉ dùng propagator W3C `traceparent`. **Không** dùng Baggage, vì baggage có thể mang dữ liệu người dùng qua ranh giới dịch vụ.
- HTTP: `httpx.Trace` mở span máy chủ, là con của `traceparent` gửi tới.
  - `trace_id` của span dùng chung cho `X-Trace-Id`, `trace_id` trong problem+json và log.
  - Metric `http.server.request.duration` chỉ gắn route mẫu, không gắn đường dẫn thật.
- Hàng đợi:
  - `outbox.Enqueue` lưu `traceparent` của ctx vào cột `shared.outbox.trace_parent` (migration `shared/000002`).
  - Publisher mở span producer là con của trace đó và ghi `traceparent` vào header AMQP.
  - `inbox.StartConsume` đọc header và mở span consumer, là con của span producer.
  - Header `trace_id` vẫn giữ như G0.
- SQL: `telemetry.PgxTracer`, tracer tự viết cho pgx.
  - Chỉ ghi tên thao tác (SELECT/INSERT…), tên database và SQLSTATE. **Không** ghi văn bản SQL, tham số hay thông điệp lỗi (R-14).
  - Chỉ tạo span khi đã có span cha.
- Metric vận hành:
  - `ag.outbox.backlog` và `ag.outbox.oldest_age` (outbox-publisher);
  - `ag.rabbitmq.dlq.depth` (worker, qua management API, `AG_RABBITMQ_MGMT_URL`);
  - `ag.job.runs`;
  - `ag.projection.lag`: chỗ đăng ký cho module có bảng chiếu, qua `telemetry.RegisterProjectionLag`.
  - Ngưỡng cảnh báo đề xuất ở `docs/runbooks/README.md`.
- Log vẫn ra stdout. Loki gom log và nối với trace qua trường `trace_id`. Chưa dùng OTLP logs.

## Còn thiếu
- `cmd/api-platform`, `cmd/api-internal`, `cmd/identity-vault` chưa gọi `telemetry.Setup`. Người tích hợp cần thêm lời gọi này **trước** `db.Open`.
- Chưa có span cho lời gọi HTTP ra ngoài (dịch vụ ngoài, kho định danh). Thêm khi có client đầu tiên.
- Ngưỡng cảnh báo chốt khi dựng Alertmanager.
