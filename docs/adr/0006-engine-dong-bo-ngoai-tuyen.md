# ADR-0006: Engine đồng bộ ngoại tuyến và phương án xử lý slot

**Trạng thái:** Đề xuất — chờ câu hỏi 23.1 · **Ngày:** 27/09/2026

## Bối cảnh
- Mục 15: cửa sổ ngoại tuyến 24–48 giờ.
- Dữ liệu chiều xuống theo phạm vi phân công; chiều lên chỉ ghi thêm, dùng UUIDv7 + idem_key sinh trên máy.

## Hướng đi
- Nếu engine dùng logical replication: phải thỏa **sáu điều kiện** ở mục 15. Slot `failover = true` đang chờ nhà cung cấp trả lời (23.1).
- Chiều lên luôn đi qua Application API có Idempotency-Key. **Không** ghi thẳng vào DB.
- Chỉ số nghiệp vụ `standby_replication_lag_seconds` có cảnh báo.

## Cần chốt trước S2 của G1
Tên engine, câu trả lời 23.1, và SLA xử lý bản ghi xung đột (C-8).
