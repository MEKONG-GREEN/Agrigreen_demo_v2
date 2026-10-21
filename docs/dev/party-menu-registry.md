# Đăng ký menu theo instance (#75)

`party.Deps.Menus` nhận `*port.Menus` từ `port.NewMenus(builder, routes...)`. Nil giữ cây compatibility hiện tại. Registration không toàn cục; danh sách route được sao chép. `MenuRoute{Pattern, Module}` chỉ nhận module trong registry và đường dẫn nội bộ; placeholder là một segment `{name}`. Hai pattern giao nhau, route lạ hoặc module không khớp bị từ chối.

`MenuBuilder(context.Context, MenuBuildRequest)` nhận `NavigationRequest` đã được máy chủ kiểm cùng `Mode` (`MenuDepartment`, `MenuAdmin`, `MenuDepartmentModules`) và `Departments` hiện hành đọc trong tenant. Builder không trả actor/tenant/audience/unit. Request slice được sao chép. `MenuDefinition{Version, DefaultRoute, Groups}` và `MenuItem` là presentation đã giải availability/traits: leaf có Route, Module, Enabled; parent chỉ có Children. Shared kiểm và sao chép sâu cây, lọc module bị tắt, rồi tái dùng NavigationPolicy để chọn leaf đang bật. Không có policy vẫn kiểm default trong cây khả dụng; không còn leaf thì từ chối. Lỗi builder/policy/module là kết quả cuối, không fallback catalog cũ.

HTTP menu, menu một phòng ban và ContextNavigation dùng cùng builder. ContextNavigation nạp lại metadata phòng ban trong tenant hiện hành. Với phiên được chọn, query HTTP node khác bị từ chối. ContextNavigation là cổng máy chủ sau khi accounts kiểm assignment/department mới: ctx có thể còn header của phiên cũ, nên không ép unit mới bằng unit của phiên cũ; chỉ nạp lại metadata unit mới trong tenant đã xác thực. Không dựng Principal hay đổi tenant. Catalog được cấu hình tự sở hữu aliases của profile; shared không áp canonicalizer compatibility lên NavigationPolicy của catalog đó.

Owner #76 phải dựng builder từ các nguồn WH-A hiện có, giữ nguyên JSON/menu version khi chưa đổi hành vi, sở hữu composition/traits/form availability/route ownership và kiểm phiên/profile. Không nối giả factory trong shared. Chưa đổi assembly, KN/web/script/golden hoặc bảy route HTX thuộc #67. #53 còn mở tới khi chuyển owner và gỡ traits/profile validation/aliases/seeds/routes cũ; gói này không chứng minh đã tách toàn bộ KN.

## Kiểm chứng 21/10/2026

Bản sửa preselection đã qua app 2.653s và toàn bộ party/handler trên PostgreSQL tạm 174.281s; ca đăng ký menu/context/tenant/node 0.26s, TestWebMenuFixture 0.01s PASS, không SKIP/INFRA. Giữ nguyên golden. Các consumer accounts, API assembly 117.534s và KN77 367.598s đã đạt trước bản sửa cục bộ ContextNavigation.

Lượt source đầu lỗi biên dịch fixture p91; lượt handler kế bị ngắt PostgreSQL trước khi chạy test. Các lỗi đó được giữ trong log, không tính là PASS. Lượt phục hồi dùng cụm riêng port 53990, đã dừng và xóa sau exit 0. Build/vet/modgen, migratecheck/archlint/contractcheck/knboundary, lint đều đạt; boundary 314, lint 0 issues. Docscheck phát hiện dòng roadmap thiếu checkbox, đã sửa riêng tài liệu và kiểm lại.
