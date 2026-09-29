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

## Cảnh báo "phân vùng DEFAULT có ~N dòng"

Có dòng rơi vào `DEFAULT`, tức từng thiếu phân vùng tháng. Con số là ước lượng từ thống kê (`n_live_tup`), vì owner bị FORCE RLS nên không `count(*)` được.

Trong trường hợp này `create_monthly_partitions` cho tháng đó **sẽ lỗi**: PostgreSQL không cho tạo phân vùng khi `DEFAULT` đã có dòng thuộc khoảng đó. **Không** chuyển dòng bằng `DELETE`/`INSERT`, vì bảng sổ chỉ ghi thêm và chuỗi băm sẽ đứt. Cách xử lý:

1. Tăng `AG_PARTITION_MONTHS_AHEAD` để các tháng **sau** vẫn được tạo.
2. Để dòng nằm lại trong `DEFAULT`. Truy vấn và `verify_chain` vẫn đúng, chỉ mất lợi ích cắt tỉa phân vùng.
3. Ghi vào nhật ký vận hành và tìm nguyên nhân (thường là job `maintain` không chạy).

## Kiểm nhanh

```sql
SELECT inhrelid::regclass FROM pg_inherits
WHERE inhparent = 'mkg_fieldops.roll_count'::regclass ORDER BY 1 DESC LIMIT 3;
```
