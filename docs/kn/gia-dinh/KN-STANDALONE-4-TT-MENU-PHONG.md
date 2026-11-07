## KN-STANDALONE-4-TT-MENU-PHONG — Menu từng phòng theo BRD 2.0

BRD 2.0 Function chi tiết là căn cứ: TT v7.7 FN-CH-00.01, FN-TW-01.01–02, FN-TI-02.01–06, FN-XA-03.01–02/07, FN-AD-04.05–07; CNTS v4.0 FN-06/07/17/19/20; HCTH v4.0 FN-19 (UR-ADM-08, BR-04); KHTC v2.3 FN-AD-04.01–03; Báo giá v2.0 FN-AD-04.03 (BR-06). Áp dụng Q3/Q4 của kế hoạch standalone đã duyệt ngày 07/11/2026.

- Chỉ TT có các báo cáo T01–V06, phát hành, rà soát và tổng hợp TT. Xóa bản chép trong nguồn menu của phòng khác; danh mục nông nghiệp chỉ nằm trong nhánh Admin TT. Danh mục CNTS/KHTC/HCTH và mặt hàng Báo giá giữ chủ sở hữu riêng. Tên hiển thị chung TTTT giữ “Phòng Thông tin Tuyên truyền” theo BRD Báo giá; mã ổn định không đổi.
- CNTS FN-20 chỉ ở CNTS; API đọc/ghi kho văn bản từ chối ngữ cảnh khác, kể cả Admin chọn phòng khác. Kho tài liệu nhân sự HCTH vẫn dùng module của HCTH.
- Q4 gỡ màn và menu báo cáo đột xuất TT. Không phát hành mới; bản ghi cũ không xóa, đọc lịch sử được, các helper ghi từ chối sửa bản đột xuất cũ; job tự chốt/chốt lại/nhắc cũng bỏ qua các bản này. Không đổi báo cáo phát sinh KHTC hay chiến dịch CNTS.
- Server kiểm API TT bằng ngữ cảnh KN đã xác thực (`RequireSession`) và cây menu hiện hành từ cổng `DepartmentAdmin.Menu` trước khi bind/use case; profile alias và mã phòng custom giữ phạm vi dữ liệu riêng; quyền vai, xã/tỉnh, chỉ xem, hỗ trợ, module và RLS hiện hữu vẫn kiểm tại use case. Use case ghi kiểm lại eligibility catalog trước tx để cổng sync không đi vòng HTTP gate. Shell web chỉ kiểm hiển thị dựa trên menu server; không coi shell là lớp xác thực API, không thêm cookie/proxy hay cơ chế xác thực.
- Worker đọc danh mục phòng hiện hành qua read-side `party.ProvinceDepartments` dưới tenant tin cậy, trước giao dịch extension; catalog KN giải profile/alias như menu. Bộ lọc tỉnh + phòng đặt trước LIMIT để không bỏ đói TT. Chỉ phòng TT đang áp dụng được auto close/reclose/remind; phòng khác, chưa mở/ngưng, đã xóa, không xác định và adhoc giữ lịch sử chỉ đọc. Không tạo phiên KN giả cho job.
- Field-app bỏ màn/khóa đột xuất TT, bản `report_task` adhoc cũ chỉ xem và không xếp bài nộp mới; `forms_task/forms_submission` native giữ nguyên; trang chủ chỉ mở TT tasks khi lá enabled có trong menu server. Bản đệm menu cần v17+ và cùng context_id; bản đệm cũ/thiếu metadata, API từ chối và chưa có menu không cấp quyền TT mặc định. Chỉ thay bản đệm menu sau một phản hồi mới hợp lệ, giữ forms, outbox và dữ liệu đồng bộ khác.
- Menu owner, catalog compatibility và mock dùng nguồn JSON chung, phiên bản `kn.menu/v17`; fixture và snapshot sinh bằng generator/test. Phòng generic chưa có module không mượn menu TT; vẫn quản trị phòng qua nhóm hệ thống.

### Tệp và bước triển khai

| Tệp | Bước | Căn cứ |
|---|---|---|
| `internal/kn/{extension,hrm,price}/menus/*.json` | 3/5 | Menu từng FN/phòng, Q4 |
| `internal/kn/extension/menu/*`, `internal/platform/party/{domain,app}/*menu*` | 3 | Cùng catalog, Admin và alias profile |
| `internal/platform/party/{port,app,store}/kn.go`, `internal/platform/party/module.go`, `cmd/api-platform/assembly/modules.go` | 1/2/3 | Read-side phòng theo tenant/tỉnh, RLS và nối dependency worker |
| `internal/kn/extension/reportcycle/store/{batch,task}.go`, `internal/kn/extension/reportcycle/app/job.go`, `internal/kn/extension/module_reportcycle.go` | 2/3 | S-4/Q4: chỉ worker TT hợp lệ, không ghi lịch sử đã gỡ |
| `internal/kn/extension/reportcycle/{handler,app}/*`, `internal/kn/extension/module.go` | 3/4 | TT FN-TW/TI/XA, FN-AD-04.07, Q4 |
| `internal/kn/extension/depthome/app/library.go` | 3/4 | CNTS FN-20, UR-TINH-25 |
| `apps/web-ops/src/app/kn/bao-cao/dot-xuat/route.ts` | 4/5 | Q4: gỡ page, exact route trả 404 trước catch-all KN |
| `apps/web-ops/src/kn/{depthome,cycle/screens,price,khtc}/*` | 5 | Q3: Thông tin Tuyên truyền; Q4/FN20; Admin mở lá hiện hành của phòng |
| `apps/field-app/lib/{application,domain,ui/kn}/*`, `apps/field-app/test/kn_standalone_menu_test.dart` | 3/5 | S-4/Q4: menu đã xác nhận theo phiên, không fallback TT, giữ dữ liệu ngoại tuyến |
| `tests/kn77/*`, menu tests, web contract/department tests | Kiểm | Âm 403/404, parity và hồi quy FN hợp lệ |

Không thêm package ngoài, schema hay migration, không sửa dữ liệu và mã HTX/MKG. Foundation surface map sẽ được cập nhật sau khi gói #262 vào dev.

### Chạy và kiểm

Từ gốc worktree, dùng toolchain của `go.mod`; sinh catalog: `node scripts/gen-module-menus.mjs`. Sinh fixture qua PostgreSQL tạm riêng: đặt `AG_UPDATE_WEB_FIXTURES=1`, `AG_UPDATE_MENU_SNAPSHOT=1` rồi `./scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v -run 'TestWebMenuFixture|TestCompatMenuSnapshotCurrent' ./internal/platform/party/app ./internal/platform/party/handler`.

Kiểm API/menu thật: `./scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/extension/menu/... ./internal/kn/extension/reportcycle/... ./internal/kn/extension/depthome/... ./internal/platform/party/... ./tests/kn77`. Web: `npm ci`, `npm run check:api`, `npm run lint`, `npm run typecheck`, `npm test`, `npm run build:mock`, rồi Playwright `tests/e2e/kn-department-menus.spec.ts` trên bản production mock riêng. Không cần migrate mới.

Field-app: `dart format --output=none --set-exit-if-changed lib test`, `flutter analyze`, `flutter test` trong `apps/field-app`; chạy `flutter analyze`, `flutter test` trong `packages/agrigreen_mobile`. Regression menu độc lập: `flutter test test/kn_standalone_menu_test.dart`.
