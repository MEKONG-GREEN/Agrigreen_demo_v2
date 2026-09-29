# Hàng đợi consumer RabbitMQ — khai báo trước, không mất sự kiện lần khởi động đầu

## Vấn đề

`outbox-publisher` phát mọi sự kiện lên topic exchange `ag.events` (routing key = `event_type`). Exchange **không giữ** thông điệp: nếu lúc phát chưa có hàng đợi nào bind `event_type` đó, broker vẫn xác nhận (publisher confirm) và bỏ thông điệp — dòng outbox đã được đánh dấu phát, sự kiện mất vĩnh viễn.

Trước đây hàng đợi chỉ được khai báo khi consumer khởi động (`cmd/worker` gọi `inbox.DeclareQueue` cho consumer được chọn bằng `-modules`; optimizer Python khai báo `pf.simulation.optimizer` khi chạy). Vì vậy sự kiện phát trước **lần khởi động đầu** của một consumer bị mất, ví dụ:

- dựng môi trường mới: `api-platform` + `outbox-publisher` chạy trước, `worker` hoặc `optimizer` lên sau;
- thêm consumer mới ở bản phát hành mới: API phát sự kiện mới trước khi worker bản mới lên;
- chạy worker với `-modules=` một phần, consumer còn lại chưa từng chạy.

## Cách làm hiện tại

Danh sách hàng đợi duy nhất là `assembly.Queues()` (`cmd/api-platform/assembly/queues.go`): mọi consumer của `cmd/worker` (G1, pf/notify, pf/reports, liên module G2 `assembly.Consumers`) và hàng đợi của optimizer `pf.simulation.optimizer`.

1. **`outbox-publisher` khai báo toàn bộ hàng đợi khi khởi động**, TRƯỚC khi phát thông điệp đầu tiên (`assembly.DeclareQueues`, idempotent): quorum queue `<consumer>` (`x-delivery-limit` 5, thư chết → `ag.dlx` khóa `<consumer>`), DLQ `<consumer>.dlq`, binding các `event_type`. Khai báo lỗi → publisher dừng (không phát khi chưa có hàng đợi). Log: `event=startup` `"đã khai báo hàng đợi consumer"` kèm `queues=<số>`.
2. Worker và optimizer vẫn khai báo lại hàng đợi của mình khi chạy — cùng tham số, nên không xung đột (RabbitMQ trả `PRECONDITION_FAILED` nếu tham số khác: khi đổi tham số hàng đợi phải đổi đồng thời `inbox.DeclareQueue` và `optimizer/src/agrigreen_optimizer/consumer.py`).
3. Thông điệp phát khi consumer chưa chạy nằm trong hàng đợi (bền, quorum) tới khi consumer lên; inbox của consumer bảo đảm xử lý đúng một lần.

Kiểm thử `cmd/worker` `TestWorkerConsumersDeclaredByPublisher` đòi mọi consumer của worker có trong `assembly.Queues()` với đúng các sự kiện — thêm consumer mới thì thêm vào `Queues()` (thường là tự động: consumer pf/notify lấy từ `notify.Module.Consumers()`, consumer liên module từ `assembly.Consumers`).

### Vhost nội bộ MKG `mkg.internal` (G3, ADR-0018)

- Sự kiện `shared.outbox.kind = internal` của cụm mkg được phát lên vhost riêng `mkg.internal`, không lên vhost nền tảng. Kết nối là `AG_AMQP_INTERNAL_URL`, tài khoản `mkg_internal`. Tài khoản nền tảng (`agrigreen`) **không** có quyền trên vhost này. Compose `rabbitmq-init` tạo vhost, tạo tài khoản rồi gỡ quyền mà RabbitMQ tự cấp cho người tạo. CI `compose-smoke` kiểm lại việc này.
- Danh sách hàng đợi là `cmd/worker-internal/assembly.Queues()`. Khi khởi động, `outbox-publisher` khai báo chúng trên vhost nội bộ, dùng cùng tham số với vhost nền tảng.
- Thiếu `AG_AMQP_INTERNAL_URL` thì sự kiện nội bộ **tồn trên outbox** (`ag.outbox.backlog` cụm mkg tăng), không bao giờ rơi sang vhost nền tảng. Đặt biến rồi khởi động lại publisher; sự kiện được phát theo thứ tự `id`.
- Kiểm tra: `rabbitmqctl -q list_permissions -p mkg.internal` chỉ có `mkg_internal`; `rabbitmqctl -q list_queues -p mkg.internal name messages consumers`.

## Vận hành

- **Thứ tự triển khai** không còn quan trọng với việc mất sự kiện, nhưng bản `outbox-publisher` phải **cùng hoặc mới hơn** bản API: publisher cũ không biết hàng đợi của consumer mới. Khi phát hành có consumer mới: cập nhật publisher trước (hoặc cùng lúc) API.
- **Kiểm tra hàng đợi đã có**: `rabbitmqctl -q list_queues name messages consumers` — mọi tên trong `assembly.Queues()` và `<tên>.dlq` phải có ngay sau khi publisher khởi động, kể cả khi `consumers = 0`.
- **Hàng đợi đọng** (consumer chưa từng chạy, `messages` tăng): khởi động consumer tương ứng (`worker -modules=<tên>` hoặc optimizer). Không xóa hàng đợi hay DLQ bằng tay (R-62).
- **Bỏ một consumer** (ngừng dùng hẳn): gỡ khỏi worker và `Queues()`, xả hết thông điệp còn lại rồi mới `rabbitmqctl delete_queue` theo quy trình thay đổi — hàng đợi không còn ai đọc sẽ tích thông điệp mãi.
- **Sự kiện đã mất trước bản sửa này** (môi trường dựng trước khi có bước khai báo): tra các dòng `shared.outbox` theo `event_type` và khoảng thời gian rồi phát lại thủ công lên `ag.events` (G2 chưa có công cụ phát lại tự động) — consumer idempotent qua inbox theo `event_id` nên phát lại an toàn.
