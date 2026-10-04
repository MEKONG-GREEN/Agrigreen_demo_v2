# ADR-0017: Kho chuỗi thời gian — PostgreSQL phân vùng tháng ở cụm `ts`

**Trạng thái:** Đề xuất · **Ngày:** 30/09/2026 · **Thành phần:** cụm `ts` (database `ag_ts`), `migrations/ts/`, `cmd/ingest`, `mkg/measurement`, `migrate maintain`

## Bối cảnh

- Tài liệu kiến trúc mục 4 ghi stack "PostgreSQL 17+, PostGIS, pgRouting, **TimescaleDB**". Mục 5 xếp `ingest`, `edge-agent` vào vùng biên với schema `ts_*`. Mục 13.2 đặt số đo cân thô ở `ts_weigh.raw_reading`.
- R-07 cấm join bảng `ts_*` với bảng nghiệp vụ trong một câu SQL, vì "Phase 1 tách cụm chuỗi thời gian".
- R-71: luồng số đo thô **không xóa**. Sau cửa sổ kiểm toán `[KÝ]` (câu C-7, chưa có số) thì chuyển sang tầng lạnh có nén.
- RB-4: giấy phép phải cho phép dùng trong nền tảng thu phí và phải có đường thoát. TimescaleDB có hai phần:
  - phần lõi Apache-2.0: hypertable, chunk;
  - phần **TSL (Timescale License)**: nén cột, continuous aggregate, chính sách lưu giữ, tiering. TSL cấm cung cấp TimescaleDB như dịch vụ cơ sở dữ liệu cho bên thứ ba, và điều khoản có thể đổi theo nhà cung cấp.

  Nhà cung cấp PostgreSQL được quản trị trong lãnh thổ (RB-7, ADR-0007) chưa chắc có sẵn extension này.
- Tải G3 nhỏ: số cân HUB_IN/HUB_OUT ở vài hub, mỗi đầu cân tối đa vài số đo mỗi giây. Telemetry phương tiện và cảm biến kho (`ts_env`) đến sau.

## Quyết định

1. **Cụm mới `ts`** (`migrations.TS`, database `ag_ts`, thư mục `migrations/ts/`), thêm vào `migrations.Clusters`.
   - Bootstrap, `migrate up|verify|maintain|anchor`, outbox-publisher và `pgtest` đều tự đi qua cụm này.
   - ĐTK 1 dùng chung instance PostgreSQL với cụm `pf`/`mkg`, nhưng **khác database, khác tài khoản** (R-05). Muốn tách vật lý (Phase 1) thì đổi URL, không sửa mã (R-01).
   - Schema `ts_weigh` dùng ngay từ G3. Schema `ts_env` (môi trường kho, telemetry) để dành cho `ts/000002`.
2. **PostgreSQL 17 thuần, phân vùng khai báo theo tháng.**
   - Bảng số đo phân vùng `RANGE (received_at)`, theo giờ hệ thống chứ không theo giờ thiết bị (ADR-0019). Có phân vùng DEFAULT làm lưới an toàn. Tạo trước 24 tháng.
   - `migrate maintain` (`opsjobs.EnsurePartitions`) tạo tiếp phân vùng cho **mọi bảng chỉ ghi thêm có phân vùng**, gồm bảng sổ và bảng `ts_*`.
3. **Bố cục tương thích Timescale.** Mỗi bảng có một cột thời gian phân vùng và khóa thiết bị, không có khóa ngoại, không có UNIQUE trên bảng phân vùng (ADR-0012). Nếu sau này đổi sang hypertable, việc chuyển chỉ gồm `create_hypertable(... migrate_data)` trên bảng mới rồi đổi đọc/ghi. Không phải thiết kế lại.
4. **Không dùng tính năng TSL** (nén, continuous aggregate, chính sách lưu giữ). `migratecheck` hỏng build khi migration có `CREATE EXTENSION … timescaledb`. Nếu cần extension lõi Apache-2.0, phải có ADR mới.
5. **Chỉ ghi thêm.**
   - Dùng `shared.apply_append_only` (migration `shared/000003`) cho trigger chặn UPDATE/DELETE/TRUNCATE (áp cả owner), RLS tenant và phân vùng.
   - Vai ứng dụng chỉ có `SELECT, INSERT`: `ingest_ts` ghi, `ts_reader` đọc.
   - `shared.v_missing_immutability` bắt **mọi bảng `ts_*`** thiếu trigger chặn.
   - Toàn vẹn **không** dựa vào chuỗi băm từng dòng mà dựa vào **lô có chữ ký nguồn**: `reading_batch` giữ `payload_sha256` + `signature` + `key_id` (ADR-0019).
6. **Tầng lạnh (R-71).** Hết cửa sổ kiểm toán C-7 thì làm theo thứ tự:
   1. `DETACH PARTITION` phân vùng tháng;
   2. xuất nén (CSV/Parquet nén, cùng manifest ký số Ed25519 như neo hằng ngày, `opsjobs.SignManifest`) vào kho đối tượng **trong lãnh thổ** có khóa đối tượng (RB-7, mục 22);
   3. xác minh bản xuất: đếm dòng, checksum, và kiểm lại chữ ký nguồn của một mẫu lô;
   4. chỉ sau khi xác minh xong mới bỏ phân vùng đã tách.

   Như vậy chuyển tầng không phải là xóa (RB-1). Runbook và job viết khi có số C-7. Tới lúc đó dữ liệu ở tầng nóng.

## Hệ quả

- Không thêm dependency hay extension nào. Cụm `ts` không cần `dbadmin.Extensions`.
- Truy vấn cửa sổ lô (tích phân phiếu cân, R-70) đọc theo `(tenant_id, device_id, epoch, counter)`. Phiếu cân ghi ở cụm `mkg`, và ingest giữ **hai pool** (`ingest_ts`, `ingest_mkg`). Không có câu SQL nào chạm cả hai cụm (R-07).
- Kiểm thử: `migrations/ts_weigh_integration_test.go` gồm:
  - sáu phép thử UPDATE/DELETE/TRUNCATE bằng vai ứng dụng và owner đều phải hỏng;
  - bộ đếm đơn điệu;
  - `received_at` do hệ đặt;
  - cách ly tenant;
  - `migrate maintain` trên cụm ts;
  - kiểm thử ngược của view cổng.
- Nếu tải thật (G4, diễn tập ĐTK 2) vượt khả năng phân vùng tháng thuần, cân nhắc phân vùng tuần hoặc extension lõi Apache-2.0 qua ADR mới. Mô hình dữ liệu không đổi.

## Đối chiếu v4.1 (04/10/2026)

Ghi lệch theo gói W0.3 ([kế hoạch](../kien-truc-v4.1-ke-hoach.md)), mục X-05 của [bản đối chiếu](../kien-truc-v4.1-doi-chieu.md). Quyết định ở trên **giữ nguyên**.

| Điểm | Kiến trúc v4.1 (§6, §21, Phụ lục B mục 13) | Repo (ADR này) |
|---|---|---|
| Công nghệ | TimescaleDB ở cụm `mkg-ts` | PostgreSQL 17 thuần, phân vùng tháng, cụm `ts` |
| Tầng lạnh | Nén (tính năng TSL) | Không nén trong DB. Xuất tầng lạnh có ký ra kho đối tượng trong lãnh thổ (runbook `ingest.md` §8) |
| RLS | Không RLS trên chuỗi thời gian | Có RLS tenant (`shared.apply_append_only`) |
| Chỉ ghi thêm, không join với bảng nghiệp vụ | Có | Có (`migratecheck`, R-07) |

- **Lý do giữ:** nén, continuous aggregate và chính sách lưu giữ của TimescaleDB đều thuộc giấy phép **TSL**. TSL không qua RB-4 (giấy phép phải cho dùng trong nền tảng thu phí và có đường thoát). Nếu chỉ dùng phần lõi Apache-2.0 thì không có nén, tức là mất đúng lý do v4.1 chọn TimescaleDB.
- **Tên cụm:** `ts` của repo tương ứng `mkg-ts` của v4.1. Cụm này chỉ chứa dữ liệu vùng nội bộ MKG, đúng tinh thần Phụ lục B mục 13 ("chỉ ở vùng nội bộ một đơn vị"). Không đổi tên.
- **RLS:** giữ. RLS trên bảng chỉ ghi thêm tốn ít và chặn được lỗi cấu hình tenant. Nếu diễn tập tải G4 cho thấy RLS là nút cổ chai thì xử lý qua ADR mới.
- **Đường chuyển:** bố cục đã tương thích hypertable (quyết định 3). Chỉ cần hỏi lại MKG và pháp chế khi thật sự cần nén ([cau-hoi-ton-dong.md](../cau-hoi-ton-dong.md) H-4).
- Đây là ghi lệch, không sửa v4.1. MKG ký cùng các ADR v4.1 ở mục A7.
