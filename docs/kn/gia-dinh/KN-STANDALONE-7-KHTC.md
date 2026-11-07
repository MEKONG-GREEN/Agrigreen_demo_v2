## KN-STANDALONE-7-KHTC — KHTC độc lập (#267, gồm #220)

Căn cứ duy nhất: BRD 2.0 KHTC v2.3 ngày 08/10/2026, Drive `1PwFVpnfY60et4OAXn1CGG_KLXhkfCI35`; DOCX SHA-256 `15859351f90574bf667190b1a12160465ba03f8ca9e408b94c90d285a3502e25`. Chỉ dùng Function chi tiết và bảng dữ liệu. Kế hoạch đã duyệt: `PLAN-KN-STANDALONE-20261107`, S-7. Không có FN cho nguồn ngoài; FN-TI-02.05–02.09, UR-TI-06–10/12/13 và BR-DL-04/09/19 chỉ dùng số liệu KN theo nguồn module/phiếu. FN-AD-04.03 của Admin được giữ.

### Kiểm kê nơi gọi và cách giữ lịch sử

- `module_khtcext.go` là điểm gắn HTTP duy nhất; `khtcreport/app/capture.go` là consumer nghiệp vụ duy nhất của khtcext. Không có consumer HTX/MKG. Gỡ package nhập ngoài, handler, mock, nguồn OpenAPI, màn/component và đăng ký menu. API cũ trả404.
- `closedfig/htx.go` chỉ được ClosedFigures/Periods gọi, nối bởi `module_closedfig.go`, `module_depthome.go` và API assembly. Gỡ nhánh gán lại trạng thái và wiring. Dữ liệu công bố đi qua nguyên trạng; giữ giải mã/trạng thái nguồn và adapter HTX còn dùng ở hệ khác, không đổi publication HTX.
- Chỉ gỡ `tinh.khtc.so_lieu_ngoai` và `tinh.khtc.mau_bao_cao`; Admin vẫn có mẫu phiếu và mục lục mẫu. Không sửa logic phòng ban của S-4.
- Migration mới chỉ thu INSERT/UPDATE/DELETE/TRUNCATE của `r_extension` trên bốn bảng `khtc_ext_*`, giữ SELECT/RLS, metadata, sổ và mọi dữ liệu. Không DML, DROP, reseal hay sửa migration cũ.
- `Data.External`, `WithExternal` và renderer Excel cũ giữ để giải mã/đối chiếu payload lịch sử. Bản soạn mới và thao tác **Cập nhật số liệu** không đọc bảng nguồn ngoài. Save lời văn và xuất từ bản đã lưu tiếp tục dùng số liệu đóng băng theo hợp đồng hiện hữu; không tự cập nhật số liệu. Get.changed vẫn so active capture gốc, nên nguồn đã gỡ có thể làm bản cũ hiện Nguồn đã thay đổi. Không sửa băm hay payload đã lưu.
- Route web cũ được thay bằng phản hồi404 trước layout/catch-all để bảo đảm status thật; không dựng màn nguồn ngoài. Không dùng streamed notFound200 làm bằng chứng.

### Tệp → bước triển khai → căn cứ

| Tệp/nhóm | Bước | FN/UR/BR |
|---|---|---|
| Không thêm dependency | 1 | S-7 |
| `migrations/pf/*_pf_extension_khtc_external_readonly.up.sql` | 2 | BR-DL-04, giữ lịch sử |
| `khtcreport/app/capture.go`, `khtcreport/domain/report.go`, `closedfig/*`, `module_{closedfig,depthome,objects}.go` | 3 | FN-TI-02.01/02.05–09; BR-DL-04/09 |
| `module.go`, API assembly, xóa `khtcext`, fragment `khtc-external`, schema legacy tại `khtc-report` | 4 | S-7; FN-TI-02.05–09 |
| Menu nguồn, schema/registry/fixture sinh, màn báo cáo, mock, route404 | 5 | FN-TI-02.05–09, FN-AD-04.03; #220 |
| `standalone267_*`, `khtc_standalone267_integration_test.go`, web/E2E `standalone267` | Kiểm | API404, role42501, legacy hash/chain/bytes, Mẫu1–5, menu Admin |

### Cách chạy

Từ worktree với toolchain trong `go.mod`, chạy `go run ./tools/modgen`; trong `apps/web-ops` chạy `npm ci` và `npm run gen:api`. Áp migration qua runner chuẩn hoặc PG tạm. Sinh menu bằng `AG_UPDATE_WEB_FIXTURES=1` với `TestWebMenuFixture`, snapshot compatibility bằng `AG_UPDATE_MENU_SNAPSHOT=1` với `TestCompatMenuSnapshotCurrent`; không sửa tệp sinh tay.

Kiểm DB: `scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/extension/closedfig/... ./internal/kn/extension/khtcreport/... ./internal/kn/extension/menu/... ./tests/kn77`. Kiểm Word/Excel: `TestStandalone267AllFiveExportBaseline` đối chiếu20 OOXML từ nguồn trước khi gỡ (chỉ chuẩn hóa timestamp metadata và ZIP metadata). Không bật lại biến capture baseline khi kiểm bản sửa.

Web: `npm run check:api`, `npm run lint`, `npm run typecheck`, `npx vitest run`, `npm run build:mock`; Playwright `kn-khtc-standalone267.spec.ts` trên production mock. Hồi quy archive/Word và callback trong KN77 vẫn phải chạy. Cổng/race/CI đúng SHA theo CONTEXT §3.3; chuẩn bị local không thay chứng nhận CI hoặc UAT khách. Sau foundations/S-4 vào dev, cập nhật surface map và tăng phiên bản catalog tiếp theo để loại cache cũ, không xóa form/outbox offline.
