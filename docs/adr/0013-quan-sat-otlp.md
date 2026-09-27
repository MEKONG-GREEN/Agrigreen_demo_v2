# ADR-0013: Quan sát: trace_id từ G0, OTLP từ G1

**Trạng thái:** Chấp nhận · **Ngày:** 27/09/2026

## Quyết định
- G0: log JSON qua `slog` với các trường chuẩn R-91; `trace_id` theo W3C traceparent, trả về ở header `X-Trace-Id` và trong mọi problem+json (R-63); `trace_id` đi kèm header của thông điệp RabbitMQ.
- G1: thêm OpenTelemetry SDK, xuất OTLP tới Tempo/Prometheus/Loki **tự vận hành trong lãnh thổ** (R-90, RB-7).
- Chỉ số nghiệp vụ bắt buộc có cảnh báo (mục 19) làm cùng module sinh ra chúng.
