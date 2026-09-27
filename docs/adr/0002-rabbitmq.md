# ADR-0002: RabbitMQ thay vì hàng đợi trong PostgreSQL

**Trạng thái:** Chấp nhận · **Ngày:** 27/09/2026

## Quyết định
- RabbitMQ, quorum queue, DLQ, publisher confirm.
- Mẫu outbox/inbox (mục 12): sự kiện ghi vào `shared.outbox` trong cùng giao dịch nghiệp vụ.
  - `outbox-publisher` phát và chỉ đánh dấu `published_at` **sau** khi broker xác nhận.
  - Consumer INSERT `(event_id, consumer)` vào `shared.inbox` trong cùng giao dịch với kết quả; ACK sau commit.
- Mỗi consumer có queue `<consumer>` và `<consumer>.dlq`, `x-delivery-limit = 5`. DLQ không tự xóa (R-62).

## Kiểm chứng
- `internal/shared/inbox/inbox_integration_test.go`: broker hỏng thì không đánh dấu đã phát; giao lặp thì handler chỉ chạy một lần; handler lỗi thì có thể thử lại.
