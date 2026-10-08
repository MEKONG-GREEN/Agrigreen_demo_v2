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
| Hạn lưu idempotency | **30 ngày** | **30 ngày** (W2.1, `shared 000006`) | **Theo v4.1 — đã áp dụng (W2.1, 04/10/2026).** Chống trùng `idem_key` của sổ **chỉ bảo đảm trong 30 ngày** kể từ lần ghi đầu (gấp 10 lần cửa sổ ngoại tuyến 72 giờ). Khóa vẫn nằm ở cột `idem_key` của sổ để tra cứu. Chi tiết ở mục dưới |

### Hạn 30 ngày của `shared.ledger_idem` (W2.1, DB-11)

- Cột `expires_at timestamptz NOT NULL DEFAULT now() + interval '30 days'` (bằng `recorded_at` + 30 ngày) và chỉ mục `ledger_idem_expires`. Dòng có trước migration nhận hạn = lúc chạy migration + 30 ngày (migration không được `UPDATE`, R-25); cách này chỉ kéo dài chống trùng của dòng cũ.
- **Trong hạn:** ghi lại cùng khóa → `unique_violation` trên `ledger_idem_pkey` → `ledger.ErrDuplicate`, như trước.
- **Hết hạn** (`expires_at <= now()`): ghi lại cùng khóa được nhận như **bản ghi mới**, kể cả khi job dọn chưa chạy — `shared.fn_chain` thay dòng đăng ký hết hạn (trỏ `row_id` sang bản ghi mới, hạn mới 30 ngày). Hành vi vì thế không phụ thuộc lịch dọn. Sổ khi đó có hai bản ghi cùng `idem_key`; truy vấn tra cứu theo `idem_key` cần chọn bản ghi mới nhất nếu có thể gặp phát lại sau 30 ngày.
- **Dọn:** `shared.purge_expired_ledger_idem(batch)` (`SECURITY DEFINER`, xóa theo lô). Vai job `ag_outbox_publisher` chỉ có `EXECUTE`, không có quyền bảng. `ledger_idem` là bảng vận hành, không phải bảng sổ (không có `trg_immutable`/`trg_chain`), nên được xóa. Job `idempotency` của `worker` (mỗi giờ) và `migrate purge-idempotency` (CronJob theo vùng, `AG_CLUSTERS`) dọn cả `shared.idempotency_key` và `shared.ledger_idem` của từng cụm; log `ledger_idem_deleted=<số>`.
- Kiểm thử: `TestLedgerIdemExpiryDefault`, `TestDuplicateIdemKeyExpiresAfter30Days` (`internal/shared/ledger`), `TestPurgeLedgerIdemEveryCluster`, `TestLedgerReplayAcceptedAfterPurge` (`internal/shared/opsjobs`).

Vai DB của v4.1 (`SET LOCAL ROLE r_<module>`, ADR-0028) không cần quyền trên hai bảng chung: trigger `shared.fn_chain` là `SECURITY DEFINER` (migration `shared 000001`), và hai bảng không cấp quyền cho vai ứng dụng. Kiểm thử ghi sổ dưới vai `r_<module>` thuộc gói W2.3.

## Hiện thực — cột thêm vào sổ sau khi đã có dòng (fx2, `shared 000011`, 08/10/2026)

**Lỗi (đã chứng minh bằng kiểm thử).** `shared.fn_chain` băm `sha256(prev_hash || canonical(to_jsonb(NEW)))`: **cả dòng**, chỉ bỏ `recorded_at`, `prev_hash`, `row_hash`, không `jsonb_strip_nulls`. `shared.verify_chain` băm lại `to_jsonb(dòng hiện tại)`. Sau `ALTER TABLE <sổ> ADD COLUMN`, dòng cũ hiện thêm khóa `"<cột>": null` (hoặc giá trị `DEFAULT`), nên băm lại khác `row_hash`. Job xác minh (`opsjobs.VerifyAllChains` → chỉ số và cảnh báo đứt chuỗi) báo `row_hash không khớp nội dung dòng` ở **dòng đầu tiên của mọi chuỗi có dòng cũ**. Đây là P1 giả, và nó che mất tín hiệu sửa dữ liệu thật. Có hai migration đã gây lỗi này:
- `mkg 000006`: `mkg_fieldops.roll_count` (4 cột). Sổ này có bản ghi mẫu từ G0.
- `pf 000079`: `pf_geo.plot_boundary` (4 cột) và `pf_geo.plot_event` (8 cột).

Database dựng mới từ đầu không gặp lỗi, vì lúc thêm cột sổ còn rỗng. Database đã có dữ liệu trước hai migration này (dev, UAT) thì gặp. Các `ADD COLUMN` khác trong repo đều trên bảng thường, không phải sổ.

**Sửa.** Không đổi `fn_chain`. Không đổi `row_hash` nào, không `UPDATE` dòng sổ.
- `shared.ledger_added_column(ledger_table, column_name, epoch)` (`ag:no-rls`, siêu dữ liệu lược đồ) ghi các cột thêm vào sổ sau khi tạo, theo đợt tăng dần cho từng sổ. Hàm `shared.ledger_added_columns(regclass)` (`SECURITY DEFINER`) trả các đợt, đợt mới nhất trước. Vì là `SECURITY DEFINER`, `verify_chain` chạy dưới vai ứng dụng vẫn đọc được mà không cần cấp quyền bảng.
- `verify_chain`: nếu băm cả dòng không khớp, hàm bỏ lần lượt khóa của từng đợt, đợt mới nhất trước, rồi băm lại. Mỗi đợt chỉ được bỏ khi **mọi cột của đợt đang NULL**. Dòng ghi trước đợt k vì thế xác minh được, kể cả khi có nhiều đợt.
- **Không làm yếu chuỗi băm.** Dòng chỉ được nhận khi `row_hash` đã lưu bằng đúng băm của nội dung **thiếu các khóa null** đó. Mọi sửa đổi giá trị đều vẫn lệch băm: đổi cột thêm của dòng cũ từ NULL sang có giá trị, đưa cột thêm của dòng mới từ có giá trị về NULL, hay sửa cột gốc. Kể cả đăng ký sai cột vào `ledger_added_column` cũng chỉ có thể làm hỏng việc xác minh, không thể làm một dòng bị sửa qua được.
- `shared.register_ledger_columns(bảng, ARRAY[cột…])`: migration thêm cột vào sổ **phải** gọi hàm này ngay sau `ADD COLUMN`. Mỗi lần gọi là một đợt. Hàm chỉ nhận cột thỏa cả ba điều kiện: NULL được, không `DEFAULT`, không generated. `DEFAULT` bị từ chối (`23514`) vì nó làm dòng cũ hiện một giá trị chưa từng được ghi, tức một sự kiện giả trên sổ chỉ ghi thêm. Hàm cũng từ chối bảng không có `trg_chain`. Hàm đăng ký trên bảng cha; phân vùng được quy về bảng cha (`pg_partition_root`).
- Đăng ký bù cho `mkg 000006` và `pf 000079` (đợt 1) nằm ngay trong `shared 000011`. Tên bảng ghi dạng văn bản, vì `shared` chạy trước migration của cụm nên lúc đó bảng có thể chưa tồn tại. Dòng đăng ký của cụm khác vô hại: chỉ dùng khi tên bảng khớp.
- `migratecheck` (`CheckLedgerColumns`) áp trên bảng sổ, tức bảng khai qua `shared.apply_ledger`:
  - `ADD COLUMN` phải NULL được, không `DEFAULT`, `NOT NULL` hay `GENERATED`, và phải có `register_ledger_columns` cho đúng cột trong cùng tệp.
  - Cấm `DROP COLUMN`, `RENAME COLUMN` và đổi kiểu cột, vì cả ba đổi khóa hoặc biểu diễn của dòng đã băm.
  - Hai tệp cũ được miễn theo số (`ledgerColumnsGrandfathered`, danh sách đóng).
  - Như các quy tắc khác, công cụ không thấy `ALTER` nằm trong khối `DO`.

**Kiểm thử.**
- `TestLedgerAddColumnKeepsChainVerifiable` (`internal/shared/ledger`) chạy trên sổ tạm:
  - cột chưa đăng ký thì đứt giả, đăng ký xong thì sạch;
  - hai đợt cột, có dòng xen giữa;
  - `ADD COLUMN … DEFAULT` làm đứt chuỗi và bị từ chối đăng ký;
  - 5 kiểu sửa vượt trigger đều bị bắt đúng `seq`;
  - xác minh dưới vai ứng dụng.
- `TestLedgerAddColumnUpgradePF` / `…MKG` (`migrations`):
  - dựng database riêng, áp `shared` tới đầu, rồi áp cụm tới `pf 000078` / `mkg 000005`;
  - ghi dòng sổ, nâng cấp lên đầu, ghi thêm dòng có giá trị ở cột mới;
  - `opsjobs.VerifyAllChains` trên cả database phải sạch.
  
  Trước `shared 000011`, hai kiểm thử này **hỏng** đúng như mô tả ở trên.
- `TestLedgerColumns*` (`tools/migratecheck`).
