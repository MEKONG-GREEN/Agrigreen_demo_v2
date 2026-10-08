# Phân vùng bảng sổ

Bảng sổ phân vùng theo tháng của `recorded_at` (UTC), có thêm phân vùng `DEFAULT` làm lưới an toàn (ADR-0012). `shared.apply_ledger` tạo sẵn 24 tháng khi tạo bảng. Lệnh `maintain` giữ cho phân vùng luôn đi trước.

## Chạy

```bash
AG_DB_URL=postgres://postgres-primary:5432/?sslmode=verify-full \
AG_DB_PASSWORD_AG_MIGRATOR=… \
migrate maintain            # mặc định 24 tháng tới; AG_PARTITION_MONTHS_AHEAD=36 để đổi
```

- Lệnh tìm bảng sổ trong mọi cụm theo đúng tiêu chí của `shared.v_missing_immutability` (bảng có cột `row_hash`), cùng mọi bảng **chỉ ghi thêm có phân vùng không có chuỗi băm** (`shared.apply_append_only` — chuỗi thời gian cụm `ts`, vd `ts_weigh.raw_reading`, ADR-0017; `opsjobs.AppendOnlyPartitionedTables`), rồi gọi `shared.create_monthly_partitions` cho từng bảng.
- Phân vùng mới được áp RLS và trigger chặn TRUNCATE (`shared.secure_partition`). Cổng catalog vẫn sạch.
- Lệnh idempotent. Chạy lặp lại chỉ tạo phần còn thiếu (`TestEnsurePartitionsIdempotent`).
- Lịch đề xuất là CronJob ngày 1 hằng tháng, cộng một lần sau mỗi `migrate up`. Không cần leader: DDL đã tự tuần tự hóa.

## Phân vùng DEFAULT có dòng (cảnh báo `AgLedgerDefaultPartitionRows`)

Có dòng rơi vào `DEFAULT`, tức từng thiếu phân vùng tháng (job `maintain` trễ hoặc không chạy). Chỉ số `ag_db_ledger_default_rows{table}` (hàm `shared.ledger_default_rows()`, shared 000010) là ước lượng từ thống kê (`n_live_tup`), vì owner bị FORCE RLS nên không `count(*)` được. Nguồn: worker (cụm của lịch, vai `ag_ops_jobs`) và sql_exporter (target `mkg`, `ts`). Cảnh báo khi > 0 trong 15 phút.

Trước shared 000010, `create_monthly_partitions` cho tháng đó **lỗi mãi** (PostgreSQL không cho tạo phân vùng khi `DEFAULT` đã có dòng thuộc khoảng đó). Nay tự xử lý:

- `migrate maintain` thấy `DEFAULT` có dòng (thống kê) → gọi `shared.drain_default_partition`: trong **một giao dịch** tách `DEFAULT`, tạo phân vùng cho **mọi tháng có dòng** (kể cả tháng đã qua), chép nguyên dòng sang (seq, `prev_hash`, `row_hash`, `recorded_at` giữ nguyên — chuỗi băm không đứt, `fn_chain` không chạy lại), xóa khỏi bảng đã tách, gắn phân vùng mới và gắn lại `DEFAULT`, áp lại RLS + chặn TRUNCATE. Lệnh in `CẢNH BÁO … đã chuyển N dòng`.
- `create_monthly_partitions` gặp `check_violation` (thống kê chưa kịp thấy) cũng gọi cùng hàm.
- Trigger chặn sửa/xóa không bị tắt: PostgreSQL bỏ trigger nhân bản khỏi phân vùng khi tách, bảng tháng mới chưa gắn nên chưa có trigger; khi gắn, trigger của bảng cha được nhân bản lại.
- Giữ khóa ACCESS EXCLUSIVE trên bảng cha suốt giao dịch (`lock_timeout` 10 giây): ghi/đọc bảng đó chờ trong lúc chuyển. Số dòng thường nhỏ; nếu `DEFAULT` lớn, chạy `maintain` ngoài giờ cao điểm. Hết `lock_timeout` → lỗi, không đổi gì, chạy lại sau.

Sau khi chạy: chỉ số về 0 khi thống kê cập nhật (vài giây). Ghi vào nhật ký vận hành và tìm nguyên nhân job trễ. Bảng phân vùng không theo mẫu (không phải RANGE một cột `timestamptz`) → hàm báo lỗi rõ ràng, xử lý tay.

## Kiểm nhanh

```sql
SELECT inhrelid::regclass FROM pg_inherits
WHERE inhparent = 'mkg_fieldops.roll_count'::regclass ORDER BY 1 DESC LIMIT 3;
```
