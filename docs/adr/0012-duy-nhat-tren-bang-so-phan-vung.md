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

## Đối chiếu v4.1 (04/10/2026)

Ghi lệch theo gói W0.3 ([kế hoạch](../kien-truc-v4.1-ke-hoach.md)), mục X-09 của [bản đối chiếu](../kien-truc-v4.1-doi-chieu.md). Quyết định ở trên **giữ nguyên**, trừ hạn lưu idempotency.

| Điểm | Kiến trúc v4.1 (§15.2–15.3, Phụ lục B mục 6) | Repo | Cách xử lý |
|---|---|---|---|
| `PRIMARY KEY (id, recorded_at)` | Mẫu đã sửa (DB-01) | Như v4.1 từ G1 | Khớp |
| Vị trí `chain_head` | Mỗi schema một bảng đầu chuỗi | **Một bảng chung** `shared.chain_head` cho mọi sổ trong cụm | **Giữ vị trí, ghi lệch.** Khóa của bảng chung đã có tên sổ, nên các sổ không tranh khóa với nhau. Mọi sổ đi qua một hàm `shared.fn_chain` được kiểm thử. Tách theo schema cần migration trên mọi sổ mà không thêm bảo đảm nào |
| Vị trí bảng idempotency | Mỗi schema một bảng | **Một bảng chung** `shared.ledger_idem` | Giữ vị trí, ghi lệch (cùng lý do) |
| Hạn lưu idempotency | **30 ngày** | Vĩnh viễn | **Theo v4.1.** Gói W2.1 (DB-11, migration `shared 000006`) thêm `expires_at` và job dọn. Sau W2.1, chống trùng **chỉ bảo đảm trong 30 ngày** (gấp 10 lần cửa sổ ngoại tuyến 72 giờ). Khóa vẫn nằm ở cột `idem_key` của sổ để tra cứu |

Vai DB của v4.1 (`SET LOCAL ROLE r_<module>`, ADR-0028) không cần quyền trên hai bảng chung: trigger `shared.fn_chain` là `SECURITY DEFINER` (migration `shared 000001`), và hai bảng không cấp quyền cho vai ứng dụng. Kiểm thử ghi sổ dưới vai `r_<module>` thuộc gói W2.3.
