# ADR-0012: Tính duy nhất trên bảng sổ phân vùng

**Trạng thái:** Chấp nhận · **Ngày:** 27/09/2026

## Vấn đề
Mẫu mục 8.1 khai báo `PRIMARY KEY (id)`, `UNIQUE (tenant_id, idem_key)` và `UNIQUE (tenant_id, stream_key, seq)` trên bảng `PARTITION BY RANGE (recorded_at)`. PostgreSQL **từ chối** cách khai báo này: khóa duy nhất của bảng phân vùng phải chứa khóa phân vùng.

## Quyết định
- `PRIMARY KEY (id, recorded_at)`.
- Tính duy nhất của `(tenant, idem_key)` và `(tenant, stream, seq)` do `shared.fn_chain` bảo đảm qua hai bảng không phân vùng:
  - `shared.ledger_idem`: khóa chính (sổ, tenant, idem_key). Trùng thì báo `unique_violation`, Go quy về `ledger.ErrDuplicate`.
  - `shared.chain_head`: đầu chuỗi, khóa `FOR UPDATE`, nên các lần ghi cùng chuỗi được xếp hàng.
- `recorded_at` do hệ thống đặt; truyền giá trị khác `now()` sẽ bị từ chối.
- Băm tính với `timezone = UTC` cố định trong hàm, nên kết quả không phụ thuộc múi giờ phiên (`TestHashIndependentOfSessionTimezone`).

## Giới hạn đã biết
- Owner có thể `ALTER TABLE ... DISABLE TRIGGER`. `migratecheck` cấm lệnh này trong migration; chuỗi băm và neo hằng ngày phát hiện được sửa đổi sau đó.
- Không dùng cột generated trong bảng sổ: cột này được tính sau trigger BEFORE nên làm lệch băm.
