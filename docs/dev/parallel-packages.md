# Gói chạy song song — quy ước tránh xung đột gộp (WH-B)

Khoảng 40 gói (agent) chạy song song và gộp vào `dev`. Trước 13/10/2026 hầu hết lần gộp đều xung đột ở cùng vài tệp và số migration/ADR bị cấp lại. Quy ước dưới đây làm cho một gói thường chỉ **thêm tệp mới**. Phần menu, mock web và OpenAPI là của gói WH-A (trang riêng). Danh sách việc theo thứ tự: [package-checklist.md](package-checklist.md).

## Migration: tên mốc UTC, không cấp số

- Tệp mới: `migrations/<cụm>/YYYYMMDDHHMMSS_<cụm>_<module>_<tên>.up.sql`. Mốc là thời điểm tạo tệp theo **UTC**. Lấy tên bằng `go run ./tools/migratecheck -new pf_price_approval` (hoặc `-new shared/<tên>`).
- Tệp cũ `NNNNNN_*.up.sql` giữ nguyên. Sổ đã áp (`schema_migrations_<cụm>` của golang-migrate, `shared.schema_migrations` của bộ chạy v2) không đổi. Phiên bản 14 chữ số luôn lớn hơn mọi số 6 chữ số, nên tệp mới chạy sau mọi tệp cũ ở cả hai bộ chạy. Đã kiểm trên DB mới và DB có sẵn: `TestTimestampMigrations_WHB`, `TestTimestampFilesOrderAfterLegacy_WHB`.
- **Rủi ro còn lại.** Bộ chạy mặc định vẫn là golang-migrate, và nó **bỏ qua im lặng** tệp có số nhỏ hơn số đã áp. Ví dụ: gói A tạo tệp lúc 09:00 nhưng gộp sau gói B (12:00). Khi chuyển DB đó sang v2, tệp chưa chạy còn bị ghi sổ `import`. Vì vậy:
  - `go run ./tools/migratecheck -base origin/dev migrations` (CI chạy cho mọi PR, so với nhánh đích; trên máy: `make gates-base`) đòi tệp **không có trên nhánh gốc** phải dùng tên mốc và có phiên bản **lớn hơn phiên bản lớn nhất** của thư mục đó trên nhánh gốc.
  - Tệp đã có trên nhánh gốc luôn được chấp nhận, kể cả số tuần tự của gói gộp trước quy ước này.
  - Nếu bị báo, chỉ cần **đổi tên tệp của mình** sang mốc mới (lệnh in sẵn trong thông báo) và sửa chỗ tham chiếu tên tệp (nếu có). Không phải sửa nội dung.
- Không cần kiểm nhánh gốc, `migratecheck` vẫn kiểm: tên đúng một trong hai dạng; phiên bản không trùng; mốc là ngày giờ hợp lệ, không trước 2026, không quá 24 giờ trong tương lai (mốc tương lai sẽ chặn mọi tệp sau nó); tệp mốc ở thư mục cụm bắt đầu bằng `<cụm>_`.
- Vì sao chọn mốc thời gian: bộ chạy v2 (`internal/shared/migrator`) và golang-migrate đều đọc phiên bản là số nguyên 64 bit. Thứ tự topo của v2 giữ thứ tự số trong mỗi module và coi tệp toàn cụm là rào chắn, nên một số lớn không phá ràng buộc nào. Không cần thư mục theo module (bước 2 của ADR-0038).

## ADR: số không trùng, giữ chỗ có sổ

- [docs/adr/README.md](../adr/README.md) có bảng chỉ mục và mục **"Số đã giữ chỗ"**.
- `go run ./tools/docscheck` kiểm:
  - số trong tên tệp `NNNN-*.md` không trùng;
  - bảng chỉ mục khớp đúng các tệp;
  - số giữ chỗ không trùng ADR đã viết.
- Viết ADR mới: lấy số trống lớn nhất + 1, thêm dòng chỉ mục, và xóa dòng giữ chỗ nếu có. Nếu gói song song đã gộp cùng số, gói gộp sau đổi sang số trống kế tiếp. Ví dụ: `cap/export` giữ 0050, nhưng 0050 đã thành `dom/hrm`, nên `cap/export` chuyển sang 0051.

## Tài liệu theo gói (thêm tệp, không sửa tệp chung)

| Thay cho | Gói thêm tệp | Quy ước |
|---|---|---|
| mục trong `docs/kn/gia-dinh-brd-2026-10.md` | `docs/kn/gia-dinh/<gói>.md` (dòng đầu `## <gói> — …`) | [README](../kn/gia-dinh/README.md) |
| dòng trong `docs/roadmap.md` mục "BRD KN 10/2026" | `docs/roadmap.d/kn-2026-10/<gói>.md` (`- [x] <gói> (…): …`) | [README](../roadmap.d/README.md) |
| mục module mới trong `docs/errors.md` | `docs/errors/<cụm>-<module>.md` (`### <cụm>/<module>` + bảng) | [README](../errors/README.md) |

- `docscheck` kiểm tên tệp, dòng đầu, liên kết tương đối và mã lỗi không trùng (kể cả với `errors.md`).
- Mục cũ trong `errors.md` để nguyên, vì `g4.test.ts`/`g5.test.tsx` đọc chúng.
- Mục khác của `roadmap.md` (ngoài đợt KN) giữ cách cũ cho tới khi có đợt song phương mới. Tạo thư mục `roadmap.d/<mục>/` cho đợt đó.

## Kiểm thử DB: lỗi hạ tầng tách khỏi lỗi kiểm thử

- `pgtest` thăm dò PostgreSQL trước khi dựng database và chờ tối đa `AG_TEST_PG_WAIT` giây (mặc định 120; `0` = không chờ).
  - Không sẵn sàng (từ chối kết nối, `the database system is starting up`/`in recovery`, `57P0x`, `08xxx`, quá số kết nối): gói **HỎNG một lần** với dòng `INFRA: PostgreSQL unavailable …`, không chạy kiểm thử nào. Không còn hàng chục kiểm thử HỎNG 0.00s.
  - Đang chạy mà PostgreSQL sập: `pgtest.Get` thăm dò (tối đa 5 s một lần) và chờ lại. Hồi phục thì chạy tiếp. Không hồi phục thì kiểm thử đó HỎNG với `INFRA: …`, các kiểm thử sau BỎ QUA cùng nhãn (gói vẫn HỎNG).
  - Gói kế tiếp trong `go test -p 1 ./...` thấy dấu "vừa sập" (10 phút) nên chỉ chờ 10 s.
  - Lỗi không phải hạ tầng (sai mật khẩu, SQL lỗi) báo ngay như cũ.
- **Cụm PostgreSQL tạm** (khuyên dùng khi cụm dùng chung 5432 hay sập). `scripts/pg-ephemeral.ps1` chạy `initdb` trong thư mục tạm, chọn cổng trống trên 127.0.0.1, đặt mật khẩu ngẫu nhiên (không in ra), cấu hình nhanh (`fsync=off`, `max_locks_per_transaction=256`), kiểm `postgis`/`btree_gist`, đặt `AG_TEST_PG_ADMIN_URL`, chạy lệnh rồi dừng và xóa cụm:

  ```powershell
  .\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 ./internal/platform/party/...
  # hoặc giữ cho cả phiên:
  . .\scripts\pg-ephemeral.ps1 start
  go test -count=1 -p 1 ./tests/kn77/...
  . .\scripts\pg-ephemeral.ps1 stop
  ```

  Script không bao giờ dùng cổng 5432 và chỉ `pg_ctl stop` thư mục có dấu do chính nó tạo. Vai và database của từng gói vẫn do `pgtest` (`dbadmin.Bootstrap`) tạo như trên cụm dùng chung. Mỗi gói kiểm thử dựng lại migration (khoảng 1–2 phút). Không cần dừng/khởi động cụm dùng chung.
