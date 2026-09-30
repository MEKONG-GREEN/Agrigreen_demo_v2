# ADR-0006: Engine đồng bộ ngoại tuyến và phương án xử lý slot

**Trạng thái:** Chấp nhận — 30/09/2026, theo quyết định của chủ sản phẩm trong phiên làm việc · **Ngày:** 27/09/2026

## Bối cảnh
- Mục 15: cửa sổ ngoại tuyến 24–48 giờ.
- Dữ liệu chiều xuống theo phạm vi phân công; chiều lên chỉ ghi thêm, dùng UUIDv7 + idem_key sinh trên máy.

## Hướng đi
- Nếu engine dùng logical replication: phải thỏa **sáu điều kiện** ở mục 15. Slot `failover = true` đang chờ nhà cung cấp trả lời (23.1).
- Chiều lên luôn đi qua Application API có Idempotency-Key. **Không** ghi thẳng vào DB.
- Chỉ số nghiệp vụ `standby_replication_lag_seconds` có cảnh báo.

## Quyết định (30/09/2026)
- **Engine = Application API.** Pull/push dùng cursor mờ ký HMAC (`AG_SYNC_CURSOR_KEY`, bắt buộc ở production) và Idempotency-Key. Hợp đồng ở [sync-contract.md](../dev/sync-contract.md). ĐTK 1 và G2 **không** dùng logical replication.
- Vì vậy câu hỏi 23.1 (slot `failover = true`) và sáu điều kiện ở mục 15 **chưa cần trả lời**. Muốn đổi sang logical replication thì phải có ADR mới, và ADR đó phải thỏa đủ sáu điều kiện.
- SLA xung đột C-8 tạm đặt 72 giờ, là tham số `sync.conflict_sla_hours` có phê duyệt. Khách chốt số giờ thì chỉ đổi tham số, không đổi mã.
- Chỉ số bắt buộc có cảnh báo: `ag.sync.conflict.overdue` (xung đột quá SLA) và `ag.db.standby_replication_lag` (giây; Prometheus `ag_db_standby_replication_lag_seconds`) cho bản sao chờ của cơ sở dữ liệu.

## Triển khai
Đối chiếu từng cam kết với mã và kiểm thử: [trien-khai-g1.md](trien-khai-g1.md#adr-0006-engine-đồng-bộ-ngoại-tuyến).
