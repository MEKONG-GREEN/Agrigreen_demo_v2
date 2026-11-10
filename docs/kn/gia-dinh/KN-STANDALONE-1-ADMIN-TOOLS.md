## KN-STANDALONE-1-ADMIN-TOOLS — gỡ 10 công cụ nền tảng khỏi app KN (#264)

Căn cứ: BRD 2.0 (Trồng trọt v7.7, CNTS v4.0, KHTC v2.3) không có FN cho nhóm "Công cụ nền tảng (tạm)"; kế hoạch `PLAN-KN-STANDALONE-20261107.md` nguyên tắc 2 và mặc định Q5; audit `kn-standalone-audit-20261107.md` mục 2.1 (dòng "Công cụ nền tảng (tạm)") và 2.3.

| Mục | Quyết định | Bằng chứng |
|---|---|---|
| Menu | Bỏ mục cha `admin.qt.cong_cu` và 10 lá (`/quan-tri/{tham-so,to-chuc,mau-bao-cao,xung-dot,giam-sat-dong-bo,khach-hang-api,su-dung-api,bang-ke,nhat-ky-xuat}`, `/dong-bo`) khỏi `internal/kn/extension/menus/base.json`, cùng 8 dòng `menuModules` chỉ dùng cho các route này. Sinh lại `menu_departments.go`, `kn.ts` (`node scripts/gen-module-menus.mjs`), ca vàng `kn-menu.json` (`AG_UPDATE_WEB_FIXTURES=1 … TestWebMenuFixture`, PG tạm), ảnh chụp `compat-menus.json` (`AG_UPDATE_MENU_SNAPSHOT=1 … TestCompatMenuSnapshotCurrent`). Giữ `kn.menu/v16` (như BF-ACC-KN-MENU, Tech Lead tăng gộp) | `TestAdminMenuDropsPlatformTools`, `TestKNSPLIT2MenuParity…`, `kn_integration_test.go` (GET `/v1/menu` Admin) |
| Cây tổ chức G1 | Trùng FN-AD-04.02 / CNTS-HCTH FN-02: KN chỉ giữ `/kn/quan-tri/co-cau-to-chuc` (API `/v1/orgs/tree`, `/v1/orgs/:ref/{move,region,impact}`, `/communes/ensure`) | `TestAdminMenuDropsPlatformTools`, test tích hợp (orgs/tree 200 trong phiên KN) |
| Route web | `isAllowed` (AppShell): phiên KN đang active không vào 10 route (và đường con) dù tài khoản có vai realm quản trị — hiện màn không có quyền. Phiên nền tảng (không ngữ cảnh KN) giữ nguyên theo vai ở `nav.ts`. Không sửa `nav.ts` để không chồng gói S-2 #263 (gói đó chặn mọi đường ngoài `/kn` trong phiên KN; khi gộp, kiểm tra này trùng và vô hại) | `shell.test.tsx` |
| API | Middleware ngữ cảnh phiên của pf/accounts (chạy cho mọi route của Application API và Admin API): request mang `X-AG-Context` hợp lệ tới route của các màn đã gỡ → 403 `accounts.kn_surface_removed`. Danh sách theo mẫu route (`internal/platform/accounts/handler/kn_surface.go`). Không chặn hạ tầng KN còn dùng: `/v1/sync/push|pull`, `POST /v1/devices`, `/v1/devices/:id/push-token` (đồng bộ ngoại tuyến của TT FN-XA-03.02/03.05, CNTS FN-09..12), `/v1/masterdata/params/value`, `/v1/reports/forms…`. `GET /v1/devices` chặn theo phương thức. Không xóa route/handler/dữ liệu: MKG/HTX/nội bộ vẫn gọi được ngoài phiên KN. 403 (không phải 404) vì route còn dùng nội bộ (tiêu chí chung của gói gỡ) | `TestKNStandaloneAdminToolsClosedForKNSession` (35 lời gọi âm + 4 dương + phiên nền tảng) |
| Màn KN dẫn sang công cụ | Bỏ liên kết "Đề xuất giá trị mới" (`/quan-tri/tham-so?prefix=cnts.`) ở Danh mục CNTS › Ngưỡng; màn chỉ hiển thị giá trị đã duyệt | `CntsCatalogScreen.tsx` |
| Giới hạn | Cổng API dựa trên ngữ cảnh phiên KN. Tài khoản Admin KN luôn có `platform_admin` (`accounts.kn_admin_grant`); gọi KHÔNG kèm `X-AG-Context` là phiên nền tảng và vẫn theo vai realm. Tách hẳn thuộc S-10b (profile `kn`, realm riêng — Q8) | — |

### Tham số KN theo Q5 (không làm màn tham số mới)

Giá trị nạp sẵn có phiên bản (đăng ký `extension.CNTSDiaryMasterdataParams`, `extension/masterdata_params.json`, lịch nạp sẵn `reportcycle/domain`) đã đúng số BRD 2.0 nên **không thêm migration/seed mới** (thêm bản trùng giá trị chỉ tạo phiên bản thừa). `TestKNParamsMatchBRD2` khóa các giá trị này.

| Tham số | Giá trị đang chạy | BRD 2.0 |
|---|---|---|
| `cnts.diary.edit_window_days` | 7 ngày, hiệu lực 01/01/2026 | CNTS v4.0 FN-10 BR-02 ("sửa trong 7 ngày nếu chưa xác thực"); Mục III.5 vòng đời "Lần ghi nhật ký (BM-08, BM-09, BM-19)". BRD không nêu mốc tính 7 ngày; mã giữ cách tính từ ngày ghi (NC-04) như trước |
| `cnts.diary.gps_retention_days` | 365 ngày (giả định Q-005) | BRD 2.0 không nêu thời hạn lưu GPS (chỉ UR-TECH-02, FN-10 BR-01: không sửa được). Giữ nguyên, vẫn là giả định |
| `extension.ksgm_week_starts` | Mặc định 1, 8, 15, 22 (`DefaultKSGMStarts`) | CNTS v4.0 Mục III.3.0 H04, KS-10, UR-TINH-08: 1–7, 8–14, 15–21, 22–cuối tháng |
| `extension.report_period_rule` | `week_start=wed;month=week_end` | KHTC v2.3 Mục I.6, GD-05 (tuần Thứ Tư–Thứ Ba), BR-DL-06 (tháng theo Thứ Ba cuối tuần) |
| `extension.report_period_rule:chan_nuoi_thuy_san` | `week_start=tue;month=week_end` (= `CNTSRule`) | CNTS v4.0 H04 (BM-04, BM-17: thứ Ba–thứ Hai; tuần vắt quý/năm theo ngày cuối tuần). BRD 2.0 vênh với KHTC; theo bảng trường của FN CNTS (PLAN-BRD2 1A) |
| `extension.target_sown_area_ha` | Không đổi | Trồng trọt v7.7 FN-TI-02.02 B4: cột "Tỉnh giao" Cấp Tỉnh nhập khi phát hành biểu — không phải tham số cố định |

Chỗ BRD còn hở (ghi nhận, không làm trong gói): CNTS v4.0 H04 cho "Cấp Tỉnh sửa được ranh giới [tuần KSGM] từng tháng" và UR-TINH-13 cho Cấp Tỉnh sửa ngưỡng cảnh báo. Trước gói này hai việc đó chỉ làm được qua `/quan-tri/tham-so` (màn nền tảng, không thuộc menu Cấp Tỉnh). Theo Q5 không làm màn tham số chung; cần một gói theo FN CNTS nếu muốn Cấp Tỉnh tự sửa.

### Tệp và thứ tự dựng

| Tệp | Bước | Căn cứ |
|---|---|---|
| `internal/kn/extension/menus/base.json`, tệp sinh menu (`menu_departments.go`, `kn.ts`, `kn-menu.json`, `compat-menus.json`) | 2 | Nguyên tắc 2; FN-AD-04.02 |
| `internal/platform/accounts/handler/kn_surface.go`, `kn.go` | 4 | Nguyên tắc 2 (API 403) |
| `apps/web-ops/src/components/AppShell.tsx`, `kn/cnts/catalog/CntsCatalogScreen.tsx` | 5 | Nguyên tắc 2 (route web); Q5 |
| `internal/kn/extension/module_brd2_params_test.go` | 2 | Q5; CNTS FN-10 BR-02, H04; KHTC GD-05, BR-DL-06 |
| Test: `menu/builder_test.go`, `party/app/modules_test.go`, `party/handler/{kn,modules}_integration_test.go`, `tests/kn77/standalone_admin_tools_integration_test.go`, `shell.test.tsx` | — | Test âm web + API, parity menu |

### Chạy và kiểm

`go test ./internal/kn/extension/... ./internal/platform/party/... ./internal/platform/accounts/...`; DB thật: `.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./tests/kn77 ./internal/platform/party/handler ./internal/platform/accounts/handler`; web trong `apps/web-ops`: `npx vitest run src/shell/shell.test.tsx`.
