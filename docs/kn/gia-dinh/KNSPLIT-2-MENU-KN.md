## KNSPLIT-2-MENU-KN — Builder menu và module ownership do KN sở hữu

Issue #76, con của #53 (ADR-0055). Hợp đồng shared #75 (`internal/platform/party/port/menu.go`, [ghi chú registry](../../dev/party-menu-registry.md)) đã gộp dev `a943fbaf`. Gói này chỉ thêm mã/tài liệu làn KN; chưa nối assembly (việc của #53 sau review), chưa đổi golden, JSON nguồn, bộ sinh hay bảy thao tác HTX `/v1/kn/cooperatives` của #67.

| Mục | Quyết định / cách triển khai | Bằng chứng |
|---|---|---|
| Nơi đặt | Gói `internal/kn/extension/menu` (thuần, không DB) + `extension.MenuCatalog` ở gốc module. pf/extension đã sở hữu bảng menu nền (`menus/base.json`) và `NavigationPolicy`; nguồn của hrm/price/contact vẫn nằm ở module đó, xuất qua `hrm.MenuSource()`, `price.MenuSource()`, `contact.MenuSource()` (embed `menus/base.json`). Không import chéo module ngoài port; không gói không-module ở `internal/kn` | archlint, modgen, knboundary 314 |
| Không bản sao cây | `menu.Compile` đọc đúng các tài liệu WH-A và gộp theo ngữ nghĩa `scripts/gen-module-menus.mjs` (thứ tự order/id, gộp đối tượng, nối mảng chặn trùng Key, replacements kiểm `before`, kiểm bộ menu/navigation). Kiểm bảng biên dịch bằng sâu với `registeredMenuJSON` mà bộ sinh ghi vào party domain | TestCompiledTablesMatchGeneratedCompatibilityCatalog |
| Thành phần | KN sở hữu composition: khuôn theo bộ menu × đối tượng, nhóm "Nghiệp vụ" sau "Điều hành", "Báo cáo cho KHTC", "Dùng chung" + mục theo đối tượng, traits (Needs manager/nhiệm vụ khi `FilterTraits`), form availability (`Requires` qua `partyport.FormAvailability`, nối muộn bằng `UseFormAvailability`), mục "Sắp có", nhóm "Phòng ban" của Admin, cây một phòng ban | parity + builder_test |
| Alias | Alias bộ menu `generic`/rỗng + mã có khuôn riêng do KN giải trong builder và trong `Catalog.NavigationPolicy()` (đường đăng ký không qua bộ chuẩn hóa compatibility). Alias trình bày giữ nguyên, kể cả cặp dashboard HCTH `/kn/nhan-su/dashboard` (`tinh.hcth.dashboard`, `tinh.chung.dashboard_nhan_su`); route chỉ đăng ký sở hữu một lần | TestPresentationAliasesKeptAndOwnedOnce |
| Sở hữu route | Mỗi route lá của mọi bảng và mọi trang mặc định đăng ký một `MenuRoute` (mẫu `/kn/phong-ban/{department}/…`). Module theo `menuModules`/`menuDeptModules` của nguồn; route chưa ánh xạ (hôm nay không bị lọc theo tenant) thuộc lõi luôn bật: `/kn/dung-chung/*` → pf/contact, còn lại (quản trị, tổng quan, `/kn/du-lieu/htx`) → pf/party như hiện hành. Module tắt lọc mọi bản trình bày của route | TestRouteOwnership, ảnh chụp với 5 bộ module tắt |
| Kiểm phiên/hồ sơ | Builder chỉ đọc request đã kiểm, không đọc phiên cũ trong ctx, không dựng Principal/ScopeAll, metadata phòng ban chỉ từ request. Từ chối (`menu.ErrDenied`, 403) khi: đối tượng lạ; chế độ không khớp đối tượng; thiếu đơn vị; phòng ban rỗng, khác đơn vị, chưa mở/ngưng/đã xóa (chế độ phòng ban); bộ menu lạ kể cả rỗng không ánh xạ; MenuDepartmentModules không có đúng metadata của phòng ban đó; Admin có phòng ban khác đơn vị hoặc bộ menu lạ. Lỗi FormAvailability là lỗi cuối. Không fallback | TestBuilderDeniesInvalidSessionOrProfile, TestFormAvailabilityErrorsAreFinal |
| Khác compatibility có chủ đích | Compatibility trả cây chỉ có "Dùng chung" (phòng ban) hoặc mục khung chung (Admin) cho bộ menu lạ/rỗng không ánh xạ; đường đăng ký từ chối theo hợp đồng #75. HTTP hiện hành đã chặn phòng ban chưa mở trước builder nên không đổi hành vi người dùng | ca từ chối |
| Phiên bản | `menu.Version = "kn.menu/v5"` = party `domain.MenuVersion`; đổi menu phải tăng cả hai cho tới khi #53 gỡ cầu nối | ca vàng kiểm `menu_version` |

### Cách #53 nối (gốc lắp ráp, sau review)

```go
catalog, err := extension.MenuCatalog(hrm.MenuSource(), price.MenuSource(), contact.MenuSource())
menus, err := catalog.Registry() // *partyport.Menus = NewMenus(catalog.Build, catalog.Routes()...)
pty := party.New(unit, clk, party.Deps{..., Menus: menus, NavigationPolicy: catalog.NavigationPolicy()})
catalog.UseFormAvailability(rep.FormAvailability()) // cùng nguồn đang nối cho compatibility
```

`MenuCatalog` từ chối thiếu/thừa/trùng nguồn thành viên (`hrm-menus`, `price-menus`, `contact-menus`). `catalog.NavigationPolicy()` thay `extension.NavigationPolicy()` khi bật `Menus` (cùng dữ liệu, thêm giải alias bộ menu); `extension.NavigationPolicy()` giữ nguyên cho đường compatibility.

### Parity

- Ca vàng web `kn-menu.json` (18 ca: 5 phòng ban × kn_tw/kn_tinh/kn_xa, Trồng trọt có/không đột xuất, Admin): `Menus.Build` + mô phỏng bước shared (lọc module, bỏ mục cha/nhóm rỗng, NavigationPolicy trên lá đang bật) cho JSON giống từng byte phần nhóm/mục/trang đích/phiên bản. Phòng ban của ca vàng được đặt Đang áp dụng vì builder từ chối phòng chưa mở (bảng không phụ thuộc trạng thái).
- Ảnh chụp `internal/kn/extension/menu/testdata/compat-menus.json`: 657 ca sinh từ hàm compatibility của party (domain/app ở dev `a943fbaf`): 11 phòng ban (6 bộ menu + 5 alias generic/rỗng) × 3 đối tượng × 4 bộ traits × có/không mẫu đột xuất (264 ca), × 5 bộ module tắt (165 ca); Admin 2 tập phòng ban (seed, trạng thái trộn/đã xóa/alias) × 6 bộ module tắt (12 ca); MenuDepartmentModules 18 phòng ban × 2 trạng thái × 6 bộ module tắt (216 ca, 132 ca compatibility từ chối vì không còn trang — đường KN từ chối cùng chỗ). Kết quả: 429 + 12 + 84 ca khớp từng byte, 132 ca từ chối khớp.
- Sinh lại ảnh chụp khi nguồn menu đổi (vd #98): chép `testdata/compat_snapshot_gen.go.txt` thành `internal/platform/party/app/zz_knmenu_snapshot_test.go` (tạm, không commit), chạy `AG_KN_MENU_SNAPSHOT=<tuyệt đối>/internal/kn/extension/menu/testdata/compat-menus.json go test -count=1 -run TestKNMenuCompatSnapshot ./internal/platform/party/app`, xóa tệp tạm, rồi chạy `go test ./internal/kn/extension/menu`. Đã đề xuất làn shared giữ bộ sinh này trong party để khỏi thao tác tạm.

### Còn mở

#53 nối assembly và kiểm parity byte trên cấu hình chạy thật (HTTP `/v1/menu`, `/v1/departments/{code}/modules`, ContextNavigation) trước khi gỡ cầu nối compatibility. Khác biệt của bước shared (không phải builder): Admin không thuộc Nút xem mọi đơn vị có thể có hai phòng ban cùng mã → khóa `phong_ban.<mã>` trùng bị registry từ chối; nhóm "Phòng ban" rỗng (không phòng ban) bị bỏ thay vì giữ nhóm rỗng.
