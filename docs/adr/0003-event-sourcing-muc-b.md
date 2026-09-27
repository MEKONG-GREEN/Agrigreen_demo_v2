# ADR-0003: Event sourcing mức B

**Trạng thái:** Chấp nhận · **Ngày:** 27/09/2026

## Quyết định
- Sổ là nguồn sự thật **chỉ ở `mkg/measurement` và `mkg/costing`**.
- Các module khác: bảng nghiệp vụ là nguồn, sổ đóng vai outbox có chuỗi băm.
- Mọi bảng sổ theo mẫu mục 8.1 và được áp bằng một lệnh duy nhất: `SELECT shared.apply_ledger('<schema.bảng>', '<tên sổ>')`. Lệnh này tạo:
  - trigger chuỗi băm;
  - trigger chặn UPDATE/DELETE/TRUNCATE;
  - RLS;
  - phân vùng DEFAULT cộng 24 tháng tới.
- Bảng chiếu `*_current` cập nhật trong cùng giao dịch (R-21).

## Kiểm chứng
- `TestAppendOnlySixWays`: 6 phép sửa/xóa bằng vai ứng dụng và bằng owner đều thất bại, kể cả TRUNCATE thẳng vào phân vùng.
- `TestChainSequenceAndTamperDetection`: superuser vượt trigger để sửa một dòng thì `verify_chain` báo đứt đúng seq.
- `TestCatalogGates` và `TestCatalogGatesDetectViolations`: view cổng rỗng trên schema thật, và bắt đúng bảng vi phạm khi có.
