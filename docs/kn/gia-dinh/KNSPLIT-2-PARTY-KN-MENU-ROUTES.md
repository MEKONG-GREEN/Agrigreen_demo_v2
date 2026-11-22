## KNSPLIT-2-PARTY-KN-MENU-ROUTES — Menu, cây module phòng ban và nạp phòng ban sang chủ KN (#222, phần sau #118)

Nguồn: issue #222 (con #118, #53, #47), ADR-0055, `tools/knboundary/boundary.yaml`. Theo đúng mẫu #118: pf/party giữ dữ liệu và luật, chủ KN giữ binding HTTP qua cổng `party/port.DepartmentAdmin`. Menu builder/alias của #76 không làm lại.

### Mapping trước và sau

| Thao tác | Trước | Sau |
|---|---|---|
| Application GET /v1/menu, GET /v1/departments/{code}/modules | `party/handler.KN.RegisterApp` | `extension/departments.Handler.RegisterApp` |
| Admin POST /v1/orgs/{ref}/departments/seed | `party/handler.KN.RegisterAdmin` | `extension/departments.Handler.RegisterAdmin` |

Đường dẫn, tham số (kể cả `?role=` cũ), thân JSON, `Cache-Control: no-store`, mã HTTP và mã lỗi (`party.menu_audience_forbidden`, `party.department_not_open`, `party.department_not_found`, `party.org_not_found`) giữ nguyên; OpenAPI chỉ đổi tag `party` → `extension` (nguồn chuyển sang `contracts/openapi/modules/extension/departments/`).

### Quyết định kỹ thuật

- **Cổng:** `DepartmentAdmin` thêm `Menu`, `DepartmentModules`, `SeedDepartments`; kiểu `MenuQuery`, `MenuView`, `MenuGroupView`, `MenuItemView`, `DepartmentSeedResult` chuyển sang `party/port`, kiểu cũ của `party/app` thành bí danh (use case không đổi). Không thêm cổng/nối mới ở gốc lắp ráp: `kn.UseDepartmentAdmin(pty.DepartmentAdmin())` của #118 phủ luôn.
- **Thân KNMenu và bộ sinh fixture:** thân JSON là `port.MenuDocument`/`port.NewMenuDocument` (trường xếp theo chữ cái để byte trùng thân `gin.H` cũ). Chủ KN và ca vàng `TestWebMenuFixture` cùng dùng hàm này, nên bộ sinh `apps/web-ops/src/api/mock/fixtures/kn-menu.json` giữ nguyên chỗ (`go test ./internal/platform/party/handler -run TestWebMenuFixture`) — không phải đổi `rebase-merge.ps1`; `kn-menu.json` không đổi byte nào.
- **Nạp phòng ban dưới /v1/orgs:** giữ đường dẫn, thêm tiền tố hẹp `/v1/orgs/{ref}/departments` vào `kn_shared_route_prefixes` (bề mặt KN do chủ KN phục vụ). `/v1/orgs` còn lại (cây, khu vực, ảnh hưởng, bảo đảm xã) vẫn là primitive của pf/party, ngoài bề mặt KN.
- **Đổi tên `handler.KN` (mục 4 của #222) chưa làm:** kiểu `KN` còn được `party/handler/kn_collect.go` (`KN.RegisterKNCollect`, /v1/kn/cooperatives*) dùng; đổi tên kiểu hay chuyển khai báo sang tệp khác đều sinh khóa ghép nối kn-symbol-in-shared mới (baseline chỉ được co). Gỡ được khi /v1/kn/cooperatives* rời party.

### Cổng bật module (R-MOD-08)

Route của chủ KN đi qua cổng bật module `pf/extension` (per_tenant). Nạp phòng ban đóng 403 `module_not_enabled` khi tenant tắt pf/extension, như `/v1/departments*` (#118). Menu và cây module phòng ban thì KHÔNG được đóng: menu tự lọc mục của module đã tắt và phải chọn được trang còn bật (kn77 `TestKH_FN_CH_00_01_DefaultRouteKHTC`) — trước khi chuyển chúng ở pf/party (luôn bật). Vì vậy `extension.Module.GateExemptRoutes()` khai `/v1/menu`, `/v1/departments/:code/modules` và `assembly.Gated` cho hai route này đi qua không cổng (`TestGatedExemptRoutes`); hành vi HTTP giữ nguyên.

### Bằng chứng

- Route map: `party.TestDepartmentCatalogRoutesNotRegisteredByParty` (party không đăng ký /v1/menu, /v1/departments/*, seed), `departments.TestRouteMap`.
- Binding/DTO từng byte: `departments.TestMenuBinding`, `TestSeedBinding`, `TestUnwiredFailsClosed`; ca vàng `TestWebMenuFixture` không đổi.
- PG thật trên gốc lắp ráp: `tests/kndepartments.TestKNSPLIT2MenuAndSeedHTTPOnOwner`; luật party qua cổng: `party/handler` (`TestUR_KT_02_MenuByConfigDepartmentToggle`, `TestW1106_SeedReplacesLegacyTempCodes`, `TestRegisteredMenusHTTPAndContextShareCurrentTenantCatalog`); `tests/kn77` (menu parity).
- knboundary 299 → 297 (bỏ route-owner `/v1/menu`, `/v1/departments/{code}/modules`), không thêm mục.
