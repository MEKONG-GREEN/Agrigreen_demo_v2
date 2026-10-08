# Xác minh chuỗi băm

Mục 8.2, mục 20 (cổng "Chuỗi băm") và mục 22 (kiểm sau phục hồi).

## Chạy

```bash
AG_DB_URL=… AG_DB_PASSWORD_AG_MIGRATOR=… migrate verify-chains
```

Với mỗi cụm, và với mỗi `(sổ, tenant, stream)` trong `shared.chain_head`:

1. Tìm bảng sổ theo tham số của trigger `trg_chain`.
2. Mở giao dịch chỉ đọc (REPEATABLE READ) có `app.tenant_id` của chuỗi. `ag_migrator` là owner nhưng vẫn bị FORCE RLS, nên phải đặt tenant thì mới thấy dòng.
3. Chạy `shared.verify_chain`, hàm dựng lại `seq`, `prev_hash` và `row_hash`.
4. Đối chiếu với đầu chuỗi: số dòng phải bằng `last_seq`, và `row_hash` của dòng cuối phải bằng `last_hash`. Bước này bắt được trường hợp bị **xóa dòng cuối**, điều mà `verify_chain` không tự thấy.

Mã thoát 0 là mọi chuỗi hợp lệ. Mã thoát 1 in ra từng dòng `ĐỨT <cụm> sổ=… bảng=… tenant=… stream=… seq=…: <lý do>`.

Lịch đề xuất:

- CronJob hằng tuần ngoài giờ cao điểm.
- Bắt buộc sau mỗi lần phục hồi (mục 22) và trong diễn tập phục hồi hàng quý.

Thời gian chạy tỷ lệ với tổng số dòng sổ, vì mỗi chuỗi dùng một giao dịch.

## Khi có chuỗi đứt: sự cố P1 (mục 23)

Đứt chuỗi băm là sự cố P1. Có thể có người vượt trigger (superuser, `session_replication_role`, sửa tệp dữ liệu), hoặc bản phục hồi bị hỏng.

1. **Không sửa gì trên DB.** Không "vá" chuỗi, không cập nhật `chain_head`.
2. Chụp bằng chứng:
   ```sql
   BEGIN; SELECT set_config('app.tenant_id', '<tenant>', true);
   SELECT seq, id, recorded_at, encode(prev_hash,'hex'), encode(row_hash,'hex')
     FROM <bảng> WHERE tenant_id = '<tenant>' AND stream_key = '<stream>' ORDER BY seq;
   ROLLBACK;
   ```
3. So với manifest neo gần nhất `anchors/<cụm>/<ngày>.json` (chữ ký kiểm được, xem [anchor.md](anchor.md)). Nhờ đó khoanh được khoảng thời gian: đầu chuỗi khớp neo ngày D mà sai ở hiện tại thì sửa đổi xảy ra sau D.
4. Kiểm nhật ký truy cập đặc quyền (bastion, ghi phiên) trong khoảng đó.
5. Phục hồi dòng đúng từ bản sao lưu đã kiểm, **bằng quy trình phục hồi**, không dùng lệnh tay trên production. Sau đó chạy lại `verify-chains`.
6. Nếu dòng có dữ liệu cá nhân hoặc số liệu tài chính thì áp cả quy trình của nghĩa vụ báo cáo tương ứng (mục 23).

## Tên sổ là định danh logic

Tên sổ (`TG_ARGV[0]` của `trg_chain`, cột `ledger` của `shared.chain_head`, `ledger_idem`, `chain_anchor`) là **định danh logic bất biến**, không phải vị trí bảng. W3.3 (pf 000132) chuyển 9 sổ HTX/hộ/thửa/vụ sang schema `pf_coop` nhưng giữ tên cũ (`pf_geo.plot_boundary`, `pf_cropcycle.farm_log`, `pf_party.kn_profile_event`…, quyết định PO 09/10/2026 — [thiết kế](../design/cooperative.md) mục 8.3). `verify-chains` ánh xạ tên sổ → bảng qua `pg_trigger.tgargs`, nên báo cáo hiện tên cũ cạnh bảng `pf_coop.*` — đúng như thiết kế, không phải lỗi. Riêng `shared.ledger_added_column` khóa theo tên bảng thật và được đăng ký bù dưới tên `pf_coop.*` ([runbook chuyển schema](cooperative-schema-move.md)).

## Kiểm thử

`internal/shared/opsjobs`, `TestVerifyAllChainsDetectsTampering`: kiểm thử sửa một dòng (vượt trigger bằng `session_replication_role = replica`) và xóa dòng cuối. Cả hai phải bị phát hiện đúng `seq`, còn chuỗi không bị đụng vẫn hợp lệ.
