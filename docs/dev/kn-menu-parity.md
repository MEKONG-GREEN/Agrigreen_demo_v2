# KNSPLIT-2-MENU-PARITY (#122)

Con của #53, phụ thuộc #76 (đã gộp dev). Codex dựng checkpoint phần shared; Claude tiếp nhận 26/10/2026 (Codex hết quota), rebase lên dev sau #98 BF-MENU, chồng W, #146 `kn.menu/v7`, #120.

## Nối ở gốc lắp ráp

`cmd/api-platform/assembly/modules.go`:

```go
menuCatalog, err := extension.MenuCatalog(hrm.MenuSource(), price.MenuSource(), contact.MenuSource())
menus, err := menuCatalog.Registry()
pty := party.New(unit, clk, party.Deps{..., Menus: menus, NavigationPolicy: menuCatalog.NavigationPolicy()})
// sau khi dựng reports:
pty.UseFormAvailability(rep.FormAvailability())
menuCatalog.UseFormAvailability(rep.FormAvailability())
```

Mọi lỗi của catalog/registry làm hỏng khởi động (không fallback về compatibility). `menuCatalog.NavigationPolicy()` thay `extension.NavigationPolicy()` vì đường đăng ký không qua bộ chuẩn hóa alias bộ menu của party; `extension.NavigationPolicy()` vẫn dùng cho đường compatibility (kiểm thử parity).

## Registry shared: nhóm rỗng chủ động

`app.registeredMenu` giữ nhóm mà owner chủ động trả rỗng (nhóm "Phòng ban" của Admin khi đơn vị chưa có phòng ban, như compatibility), vẫn bỏ nhóm có mục nhưng bị lọc hết vì module tắt. Không hard-code tên nhóm, không nới duplicate-key, không đổi kiểm phiên/tenant, lỗi cấu hình vẫn là lỗi cuối. Regression: `TestRegisteredMenuPreservesOwnerEmptyGroupAndPrunesFilteredGroup`.

## Parity trực tiếp

`tests/kn77/menu_parity_integration_test.go` (`TestKNSPLIT2MenuParityConfiguredMatchesCompatibility`) dựng gốc lắp ráp thật (harness) và một bề mặt Application API thứ hai cùng mọi module, chỉ pf/party dựng với `Menus` nil + `extension.NavigationPolicy()`, cùng nguồn phiên (pf/accounts), cùng FormAvailability (pf/reports), cùng PostgreSQL/tenant. Cùng token và X-AG-Context gọi cả hai bề mặt; 200 phải giống từng byte, lỗi phải cùng mã HTTP và mã lỗi; không có 5xx.

Phủ:

- đơn vị TW và tỉnh; phòng ban: 5 phòng seed (Trồng trọt mở, còn lại chưa mở), một phòng mở cho mỗi bộ menu (6 bộ), phòng ngưng, phòng đã xóa (404), alias `generic` của 4 mã có khuôn riêng + phòng mới `generic` ở TW;
- `/v1/menu` và `/v1/departments/{code}/modules` cho kn_tw, kn_tinh, kn_xa, admin: quản trị nền tảng chỉ định đối tượng/đơn vị/phòng ban; vai realm gắn Nút (suy đơn vị từ token); Admin không gắn Nút (mọi đơn vị);
- phiên KN thật: Trưởng phòng (Lãnh đạo), chuyên viên Tỉnh/Xã/TW, nhiệm vụ QTNV báo giá, Ban giám đốc chỉ xem, phiên Admin KN, phiên hỗ trợ;
- ContextNavigation: mọi đối tượng × phòng ban × 4 bộ traits, Admin theo đơn vị và không đơn vị;
- 7 bộ module tắt (không tắt; pf/extension; pf/hrm; pf/price; pf/geo; nhóm masterdata/reports/sync/metering/mechanization; tắt hết), mỗi lượt vượt TTL cache module.

Kết quả lần chạy 26/10/2026: không lệch nội dung menu ở ca nào: 2.163 cặp HTTP (1.589 ca 200 giống từng byte, trong đó 21 ca Admin mọi đơn vị qua khác biệt 1 bên dưới; 574 ca cùng từ chối, cùng mã lỗi), 1.533 ca ContextNavigation (1.113 cùng trang đích, 420 cùng lỗi). Kiểm thử hỏng nếu gỡ dòng nối ở gốc lắp ráp (đã chạy thử).

Mẫu báo cáo đột xuất (FormAvailability = true) không dựng được qua HTTP trong kiểm thử này; phủ bởi ảnh chụp 657 ca bên dưới (traits × có/không mẫu × module tắt).

## Khác biệt có chủ đích (kiểm tường minh)

1. **Admin không gắn Nút, hai đơn vị có phòng ban cùng mã** (vd TW và tỉnh cùng `trong_trot`): compatibility trả hai mục cùng khóa `phong_ban.<mã>`; builder KN (#76) thêm hậu tố `.<org_ref>` vào khóa của các mục trùng để khóa anh em duy nhất (registry từ chối khóa trùng). Nhãn, biểu tượng, trạng thái, mục con và route giữ nguyên; mã không trùng giữ khóa cũ. Kiểm thử bỏ hậu tố rồi so từng byte, và hỏng nếu thiếu hậu tố khi trùng hoặc có hậu tố khi không trùng. Lưu ý web: `menuAdapter.adminDepartments` lấy mã từ khóa để tra tên ngắn của phòng **chưa mở**; với mục trùng mã có hậu tố, tra không thấy nên hiện tên đầy đủ thay tên ngắn. Chỉ xảy ra khi Admin không gắn Nút và hai đơn vị có phòng ban chưa mở cùng mã.
2. **`org_ref` tường minh khác đơn vị của phiên đang chọn** (người không phải quản trị nền tảng): đường đăng ký trả 403 (#75 `checkMenuUnit`), compatibility trả menu của đơn vị kia. Web chỉ gọi `GET /v1/menu` không tham số nên người dùng không gặp.
3. **Bộ menu lạ, hoặc rỗng mà mã chưa map** (#75/#76): builder từ chối, compatibility trả cây rút gọn. Không tạo được qua API (kiểm bộ menu đã biết khi tạo/sửa, cột DB `NOT NULL DEFAULT 'generic'`); không có ca HTTP.

## Ảnh chụp compatibility

`internal/kn/extension/menu/testdata/compat-menus.json` (657 ca) do `TestCompatMenuSnapshotCurrent` (`internal/platform/party/app/menu_compat_snapshot_test.go`) sinh từ hàm compatibility của party; kiểm thử hỏng khi ảnh chụp lệch bảng hiện hành. Sinh lại khi menu đổi: `AG_UPDATE_MENU_SNAPSHOT=1 go test -count=1 -run TestCompatMenuSnapshotCurrent ./internal/platform/party/app`, rồi `go test ./internal/kn/extension/menu` (builder KN so từng byte). Ảnh chụp giữ tới khi #53 gỡ cầu nối compatibility.

Phiên bản menu giữ `kn.menu/v7` (nội dung không đổi).
