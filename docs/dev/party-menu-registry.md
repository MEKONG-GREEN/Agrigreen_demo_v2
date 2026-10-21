# Đăng ký menu theo instance (#75)

`party.Deps.Menus` nhận `*port.Menus` từ `port.NewMenus(builder, routes...)`. Nil giữ cây compatibility hiện tại. Registration không toàn cục; danh sách route được sao chép. `MenuRoute{Pattern, Module}` chỉ nhận module trong registry và đường dẫn nội bộ; placeholder là một segment `{name}`. Hai pattern giao nhau, route lạ hoặc module không khớp bị từ chối.

Một route đã đăng ký có thể xuất hiện ở nhiều mục trình bày với key/nhãn khác nhau, kể cả khác nhóm hoặc mục lồng nhau; registry giữ nguyên thứ tự và từng mục, không khử trùng lặp theo route. Owner đăng ký ownership một lần cho route, không tạo registration mới theo mỗi alias vì kiểm pattern giao nhau vẫn giữ nguyên. Mỗi leaf vẫn phải thuộc đúng module sở hữu route, key trong cùng danh sách anh em và key nhóm vẫn duy nhất. Khi module bị tắt, mọi bản trình bày của route đó đều bị lọc trước khi chọn landing. Điều này giữ các liên kết điều hướng cùng đích hiện có, như dashboard của phòng và liên kết dùng chung, mà không đổi quyền.

`MenuBuilder(context.Context, MenuBuildRequest)` nhận `NavigationRequest` đã được máy chủ kiểm cùng `Mode` (`MenuDepartment`, `MenuAdmin`, `MenuDepartmentModules`) và `Departments` hiện hành đọc trong tenant. Builder không trả actor/tenant/audience/unit. Request slice được sao chép. `MenuDefinition{Version, DefaultRoute, Groups}` và `MenuItem` là presentation đã giải availability/traits: leaf có Route, Module, Enabled; parent chỉ có Children. Shared kiểm và sao chép sâu cây, lọc module bị tắt, rồi tái dùng NavigationPolicy để chọn leaf đang bật. Không có policy vẫn kiểm default trong cây khả dụng; không còn leaf thì từ chối. Lỗi builder/policy/module là kết quả cuối, không fallback catalog cũ.

HTTP menu, menu một phòng ban và ContextNavigation dùng cùng builder. ContextNavigation nạp lại metadata phòng ban trong tenant hiện hành. Với phiên được chọn, query HTTP node khác bị từ chối. ContextNavigation là cổng máy chủ sau khi accounts kiểm assignment/department mới: ctx có thể còn header của phiên cũ, nên không ép unit mới bằng unit của phiên cũ; chỉ nạp lại metadata unit mới trong tenant đã xác thực. Không dựng Principal hay đổi tenant. Catalog được cấu hình tự sở hữu aliases của profile; shared không áp canonicalizer compatibility lên NavigationPolicy của catalog đó.

Owner #76 phải dựng builder từ các nguồn WH-A hiện có, giữ nguyên JSON/menu version khi chưa đổi hành vi, sở hữu composition/traits/form availability/route ownership và kiểm phiên/profile. Không nối giả factory trong shared. Chưa đổi assembly, KN/web/script/golden hoặc bảy route HTX thuộc #67. #53 còn mở tới khi chuyển owner và gỡ traits/profile validation/aliases/seeds/routes cũ; gói này không chứng minh đã tách toàn bộ KN.

## Kiểm chứng 21/10/2026

Bản sửa preselection đã qua app 2.653s và toàn bộ party/handler trên PostgreSQL tạm 174.281s; ca đăng ký menu/context/tenant/node 0.26s, TestWebMenuFixture 0.01s PASS, không SKIP/INFRA. Giữ nguyên golden. Các consumer accounts, API assembly 117.534s và KN77 367.598s đã đạt trước bản sửa cục bộ ContextNavigation.

Lượt source đầu lỗi biên dịch fixture p91; lượt handler kế bị ngắt PostgreSQL trước khi chạy test. Các lỗi đó được giữ trong log, không tính là PASS. Lượt phục hồi dùng cụm riêng port 53990, đã dừng và xóa sau exit 0. Build/vet/modgen, migratecheck/archlint/contractcheck/knboundary, lint đều đạt; boundary 314, lint 0 issues. Docscheck phát hiện dòng roadmap thiếu checkbox, đã sửa riêng tài liệu và kiểm lại.

## Điều chỉnh alias 22/10/2026

Đối chiếu tĩnh fixture HCTH phát hiện hai key khác nhau cùng trỏ dashboard nhân sự. Bản sửa bỏ giới hạn trùng route ở cây trình bày, giữ nguyên kiểm pattern/ownership và key; bổ sung unit regression cho alias giữa nhóm/lồng nhau, bản sao output và các trường hợp ownership/key không hợp lệ. Ca HTTP registry hiện có cũng kiểm cả hai liên kết được giữ, alias đầu không khả dụng nhưng alias sau vẫn thành landing cùng ContextNavigation, và cả hai cùng bị bỏ khi module bị tắt. Fixture/golden không thay đổi.

Nguồn `fc1cdda94a66dd7d68af57259ac78d1ba3a8d5b2` đã kiểm thật với Go 1.26.9:

- Unit port/app: exit 0, 10 test cấp đầu PASS, port 0.465s và app 1.368s. Alias, bản sao output, key và ownership negatives đều đạt.
- PostgreSQL riêng: `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/party/... ./cmd/api-platform/assembly` exit 0; 85 test cấp đầu PASS, không FAIL/SKIP/INFRA kể cả subtest. App 1.993s, domain 0.547s, handler 115.722s, port 0.736s và API assembly 106.553s. `TestRegisteredMenusHTTPAndContextShareCurrentTenantCatalog` PASS 0.22s; `TestWebMenuFixture` PASS 0.00s, không bật cập nhật fixture. Cụm tạm 62092 đã dừng và dọn.
- Build, vet, modgen-check, migratecheck, archlint, contractcheck, docscheck, knboundary và lint đều exit 0; boundary 314, lint 0 issues. Gofmt chỉ còn hai fixture archlint cũ, không đổi từ trước bản sửa. Raw logs và proof ở `%TEMP%/agrigreen-ticket75-alias-{pure,pg,static}-proof.json` cùng log từng bước. Không lặp accounts HTTP/KN77 đã đạt trước đó vì sửa lần này chỉ đổi validation cây trình bày đã cấu hình; default/nil và nguồn các consumer đó giữ nguyên.

Owner #76 và assembly vẫn cần thực hiện theo hợp đồng, rồi kiểm byte parity trên cấu hình chạy thật trước khi gỡ bridge trong #53. Gói shared cần Claude Review OK trước khi gộp.
