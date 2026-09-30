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

### Công cụ `cmd/rebuilddrill` (cụm mkg)

G4 hiện thực diễn tập bằng Go thay cho hàm SQL `rebuild_<bảng>` của khuôn trên: mỗi module mkg có bảng chiếu dựng lại
bằng **cùng mã** với `POST /v1/<module>/projections/rebuild` (`<module>.Module.RebuildTenant`, tenant tường minh — chỉ công
cụ vận hành gọi, API vẫn kiểm vai). Với mỗi tenant và mỗi module, lệnh:

1. băm mọi bảng chiếu của schema module (`*_current`, cộng `mkg_hub.lot_balance`) bằng vai DB của chính module (RLS theo
   tenant): số dòng + SHA-256 của các dòng `to_jsonb(dòng) - 'updated_at'` sắp theo văn bản (bỏ cột DEFAULT `now()`);
2. dựng lại từ sổ trong một giao dịch của module (xóa + phát lại sổ; ghi sổ + chiếu cùng giao dịch nên khóa theo module);
3. băm lại và so. Mọi bảng phải **khớp** (`rows_before = rows_after`, `sha256_before = sha256_after`).

Module chưa có hàm dựng lại (hiện là `mkg/fieldops` — bảng chiếu ghi cùng giao dịch với sổ, chưa có lệnh dựng lại) vẫn được
băm, báo cáo ghi **chưa có** — mục mở (R-22 đòi mọi bảng chiếu có lệnh dựng lại). Cụm pf (nền tảng) không thuộc công cụ này
(R-03: gốc lắp ráp một vùng).

Chạy (máy vận hành trong vùng mkg, trỏ vào **bản sao**; lệnh GHI bảng chiếu nên bắt buộc `-confirm-copy`):

```
AG_MKG_DB_URL=postgres://<host bản sao>:5432/?sslmode=require \
AG_DB_APP_PASSWORD=… \
go run ./cmd/rebuilddrill -confirm-copy -out rebuild-drill-$(date +%F).md -json rebuild-drill-$(date +%F).json
```

- `-tenants uuid,uuid` giới hạn tenant (mặc định: mọi tenant có sự kiện trên `shared.outbox` của cụm mkg, đọc bằng vai
  `ag_outbox_publisher`); `-modules hub,costing` giới hạn module; `AG_DB_SUFFIX` cho database có hậu tố (như pgtest).
- Mã thoát 0 = mọi bảng khớp; 1 = có bảng **LỆCH** hoặc lỗi dựng lại (báo cáo vẫn ghi); 2 = sai cấu hình.
- Lưu `rebuild-drill-<ngày>.md` + `.json` vào hồ sơ diễn tập (cùng biên bản phục hồi, [restore-drill.md](restore-drill.md)) và ghi
  kết quả vào dòng A-4 của [g4-acceptance.md](../uat/g4-acceptance.md).

Bảng lệch nghĩa là bảng chiếu hiện hành **không** bằng sổ: sau lần chạy, bảng đã được dựng lại theo sổ trên bản sao — mở sự
cố (P1 nếu là số liệu tài chính: costing, acctbridge, orders), tìm nguyên nhân ghi chiếu lệch trước khi dựng lại trên production
theo quy trình trên (phê duyệt thay đổi, `APPLY`).

Kiểm thử: `cmd/rebuilddrill` `TestRebuildDrillChecksumsBeforeAfter_R22` (dữ liệu sinh qua consumer thật của logistics / orders /
costing trên database kiểm thử; bảng chiếu bị sửa tay → báo LỆCH, lần sau khớp lại), `TestRebuildDrillRequiresConfirmCopy`.
Máy dev chỉ có dữ liệu tổng hợp — diễn tập trên bản sao dữ liệu thật chạy ở staging / môi trường phục hồi (hạ tầng).
