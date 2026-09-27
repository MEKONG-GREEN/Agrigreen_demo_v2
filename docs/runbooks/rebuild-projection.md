# Dựng lại bảng chiếu từ sổ (R-22)

Sổ là nguồn, bảng chiếu `*_current` là dữ liệu dẫn xuất (mục 8.3). Theo R-22, mỗi bảng chiếu phải có lệnh dựng lại từ sổ và phải được diễn tập ở cổng G4 trên bản sao dữ liệu thật. Ở G1 chưa có bảng chiếu nào vì event sourcing mức B chỉ áp cho `measurement` và `costing` (ADR-0003). Tài liệu này là **khuôn** để module G2 hiện thực.

## Module phải cung cấp

1. **Hàm SQL** `<schema>.rebuild_<bảng_chiếu>(p_tenant uuid)`, viết trong migration của module:
   - `SECURITY INVOKER`: người gọi đặt `app.tenant_id`, RLS vẫn áp.
   - Dựng lại vào bảng tạm `pg_temp.<bảng>_rebuild` từ sổ, theo `ORDER BY stream_key, seq`. Bỏ các bản ghi đã bị bù (`supersedes_id`).
   - Trả số dòng và một băm tổng: `sha256(string_agg(canonical(row) ORDER BY khóa))`.
2. **Hàm so sánh** `<schema>.diff_<bảng_chiếu>(p_tenant uuid)`: liệt kê khóa lệch giữa bảng tạm và bảng chiếu hiện tại.
3. **Lệnh** `make rebuild-projection TABLE=<schema.bảng> [TENANT=<uuid>] [APPLY=1]`. Lệnh gọi `cmd/migrate rebuild-projection` (thêm cùng module đầu tiên có bảng chiếu).
4. **Nguồn độ trễ** cho metric `ag.projection.lag`: gọi `telemetry.RegisterProjectionLag` lúc khởi động, trả `now() - max(recorded_at của sổ đã chiếu)` cho mỗi bảng chiếu.

## Quy trình

1. **Chạy khô** (mặc định). Lệnh dựng vào bảng tạm và in số dòng, băm tổng, danh sách lệch. Không ghi gì.
2. So băm tổng với lần chạy trước hoặc với bản sao. Lệch mà không giải thích được thì dừng và mở sự cố (P1 nếu là số liệu tài chính, mục 23).
3. **Áp** (`APPLY=1`, chỉ khi đã có phê duyệt thay đổi). Trong **một giao dịch** cho mỗi tenant:
   - `LOCK TABLE <bảng_chiếu> IN SHARE ROW EXCLUSIVE MODE`, để chặn ghi đồng thời (ghi sổ + chiếu cùng tx theo R-21);
   - `DELETE` các dòng của tenant rồi `INSERT` từ bảng tạm. Bảng chiếu **được** sửa vì nó không phải sổ;
   - ghi `audit` và outbox `…projection.rebuilt`.
4. Chạy lại bước 1: danh sách lệch phải rỗng.
5. Kiểm `ag.projection.lag` về lại mức bình thường.

## Diễn tập G4

- Chạy trên bản phục hồi từ sao lưu production vào môi trường sạch (mục 22). Staging chỉ dùng dữ liệu ẩn danh (R-95).
- Ghi lại thời gian dựng lại cho từng bảng và băm tổng trước/sau. Hai băm phải bằng nhau.
- Chạy `migrate verify-chains` trước khi dựng lại. Chuỗi đứt thì dựng lại vô nghĩa, xử lý theo [verify-chains.md](verify-chains.md) trước.
