## KN-STANDALONE-6-CNTS-HTX — CNTS độc lập với dữ liệu HTX

Nguồn: BRD 2.0 CNTS v4.0 (`16j8ccqhjAMtXmqh8tkvNogvdBdprexrY`), Function chi tiết FN-18/FN-19;
UR-TINH-17/19 và cấu trúc dữ liệu của điểm trình diễn; kế hoạch đã duyệt `PLAN-KN-STANDALONE-20261107.md`, S-6, issue #266.

### Lựa chọn trong gói

- FN-18 B1–B2: gỡ thống kê thửa HTX khỏi service, dữ liệu API, contract và web. Giữ endpoint bản đồ, vùng nuôi/vùng dịch,
  điểm/hộ mô hình, trang trại và số ảnh hưởng; phạm vi xã/tỉnh (AC-02), ngày lịch sử (BR-01/AC-03), BR-02/03 giữ nguyên.
- FN-19 B1: chọn cách bỏ kiểm publication HTX như issue cho phép. Điểm do KN quản lý; `cooperative_ref` tùy chọn còn lưu
  như mã cũ, kiểm cú pháp UUID như trước, không tra cứu hoặc ghi HTX. Giữ quyền chủ nhiệm (BR-01/AC-04), khóa phiên bản,
  idempotency, sổ chỉ ghi thêm, RLS và vault. Không dựng nguồn hồ sơ KN mới hoặc mở rộng phần FN-19 đang chờ #221.
- Không có migration, chuyển dữ liệu, thu hồi quyền dùng chung hoặc xóa cổng `PlotStatistics`: các consumer legacy khác
  trong `pf/extension` và hệ HTX còn dùng cổng/publication này. Chỉ bỏ dây nối vào bản đồ CNTS.
- Audit ai gọi: lớp cũ chỉ đi qua `cntsmap`, contract và `src/kn/cnts/map`; field-app không gọi `cnts/map/layers` hoặc đọc
  `plot_stats`/`plots_ready`. Không có khóa menu hay route riêng của lớp cần xóa; menu, registry và field-app không đổi.

### Tệp → bước → yêu cầu

| Tệp | Bước PLAN-BRD2 §1A | FN/UR/BR |
|---|---|---|
| `internal/kn/extension/cntsmodel/store/store.go` | 2: bỏ truy vấn HTX | FN-19 B1, UR-TINH-19 |
| `internal/kn/extension/cntsmodel/app/{sites,service}.go` | 3: bỏ kiểm tồn tại HTX | FN-19 B1; BR-01/AC-04 giữ |
| `internal/kn/extension/cntsmap/app/{layers,service}.go`, `cntsmap/store/store.go`, `internal/kn/extension/module.go` | 3: bỏ lớp và dây nối CNTS | FN-18 B1–B2, BR-01–03, AC-02/03 |
| `internal/kn/extension/cntsmap/handler/http.go`, `contracts/openapi/modules/extension/cnts-map/application.yaml` | 4: bỏ trường của lớp cũ | FN-18 B1–B2 |
| `contracts/openapi/application.yaml`, `apps/web-ops/src/api/generated/application.ts` | 4: sinh bằng `npm run gen:api` | Contract FN-18 |
| `apps/web-ops/src/kn/cnts/map/MapScreen.tsx`, `src/api/mock/kn-cnts-map.ts` | 5: gỡ bảng thống kê HTX | FN-18 B1–B2, UR-TINH-17 |
| `cntsmap/handler/*integration_test.go`, `cntsmodel/handler/*integration_test.go`, `src/kn/cnts/map/map.test.ts`, `tests/e2e/kn-cnts-standalone.spec.ts` | 2–5: hồi quy | FN-18/FN-19, phân quyền, độc lập HTX |

Bước 1 không thêm dependency. Không sửa biểu KNQG hoặc lớp bản đồ khác.

### Kiểm chứng và chạy lại

`TestCNTSMapStandaloneWithoutHTX` và `TestCNTSModelStandaloneWithoutHTX` dùng DB PostgreSQL/PostGIS thật của
`pg-ephemeral`: publication không có dòng; trường hợp thiếu schema đổi tên namespace tạm **trước** khởi tạo module,
kiểm `to_regnamespace` là NULL, khôi phục bằng cleanup. Không DROP dữ liệu; không dùng DB dev. Map kiểm không gọi cả
cổng HTX đã cấp, không trả hai trường cũ; endpoint 200 và phạm vi xã còn đúng. Site kiểm tạo/sửa với mã không có trong
publication, giữ lịch sử/idempotency, từ chối chủ nhiệm/xã khác. Toàn bộ test cũ của map/site, menu parity và kn77 là hồi quy.

Từ gốc repo, đặt Go theo `go.mod` và môi trường theo CONTEXT §2, chạy:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/extension/cntsmap/... ./internal/kn/extension/cntsmodel/... ./internal/kn/extension ./internal/kn/extension/menu/... ./tests/kn77 ./migrations ./tools/migratecheck
```

Web: tại `apps/web-ops`, `npm ci`, `npm run gen:api`, `npm run check:api`, `npm run lint`, `npx tsc --noEmit`,
`npx vitest run`, `npm run build:mock`, rồi `npx playwright test tests/e2e/kn-cnts-standalone.spec.ts` trên bản production.
Phép làm sai có kiểm soát: đưa trường `plot_stats` trở lại payload; native assertion phải FAIL, khôi phục đúng byte rồi PASS.
Chạy API/web theo runbook hiện hành; gói không cần migrate hay seed HTX.
