# Đăng ký menu, mock và OpenAPI theo module (WH-A)

Nguồn do module sở hữu; tệp gộp được sinh, không sửa tay. Chạy từ `apps/web-ops`: `npm run gen:api` (gồm `gen:modules`), rồi `npm run check:api`. Lệnh kiểm tra chạy các ca âm của bộ ghép, kiểm tra đầu ra hiện hành và mã API. CI hiện có gọi `check:api`; build/dev cũng ghép nguồn trước khi chạy. Node24, không thêm thư viện hay yêu cầu Go cho bộ ghép.

## Menu

Thêm `internal/platform/<module>/menus/<feature>.json` (sau KNSPLIT-1 cũng nhận `internal/kn/<module>/menus/`). Mỗi đăng ký có `id` duy nhất, `order` nguyên và `tables`. Ví dụ thêm mục vào `deptBusiness.<menu_profile>.<audience>`; thêm ánh xạ `menuModules`/`menuDeptModules` khi thuộc module bật theo tenant. Các bảng hiện hành và kiểu Go nằm trong `party/domain/menu*.go`; JSON dùng tên trường Go (`Key`, `Label`, `Route`, `Icon`, `ComingSoon`, `Needs`, `Children`, `Requires`).

Bộ ghép tự phát hiện tệp, ghép map, nối array theo thứ tự đăng ký, từ chối khóa mục trùng và giá trị vô hướng bị ghi đè. Đổi mục Sắp có bằng `replacements: [{path: [...], before: <toàn bộ giá trị cũ>, after: <giá trị mới>}]`; giá trị cũ không khớp thì dừng. Không ghi đè âm thầm. Profile mới thêm vào `MenuProfiles`, `deptBusiness`, `deptProfileByCode`; không sửa bảng profiles ở Go.

`implementedMenu` sinh từ mục sẵn sàng; bộ ghép kiểm đường dẫn có trang thật trong `src/app` (hỗ trợ segment động). Go vẫn kiểm trùng khóa, quyền, danh sách ca vàng; web vẫn đối chiếu độc lập từng ca với golden do Go sinh. Mock menu dùng cùng đăng ký qua vùng sinh trong `kn.ts`, giữ cách lọc và dữ liệu phiên hiện hành. Thay menu có chủ đích phải tăng phiên bản và sinh ca vàng bằng `AG_UPDATE_WEB_FIXTURES=1 go test ./internal/platform/party/handler -run TestWebMenuFixture` trên PostgreSQL thật.

WH-A giữ phần tương thích đã kiểm kê trong `party/domain/menu_departments.go`; registry đọc JSON biên dịch trong binary, không đọc tệp lúc chạy. Đây là bước chuẩn bị cho KNSPLIT-2, chưa phải đã loại nghiệp vụ KN khỏi party. Không thêm miễn trừ ranh giới.

## Mock

Mỗi provider giữ route và trạng thái trong tệp riêng. Thêm `src/api/mock/registrations/<feature>.json`: `{"id":"feature","source":"./kn-feature","app":[{"export":"featureRoutes","order":1000}],"admin":[]}`. `source` tương đối dưới thư mục mock; mỗi mặt app/admin có ưu tiên nguyên duy nhất. Số nhỏ được thử trước; thứ tự route trong mỗi batch giữ nguyên. Tệp mới không sửa handler hay danh sách import. Bộ ghép kiểm provider/export tồn tại và từ chối ưu tiên trùng.

Các provider KN (`./kn*`) ghép trong vùng sinh của điểm vào KN có sẵn `kn-admin.ts`, trả đăng ký qua hàm để tránh đọc binding trước khởi tạo trong vòng phụ thuộc hiện hành. Handler chỉ lấy đăng ký từ điểm vào đó, ghép với provider chung và sắp theo ưu tiên cũ. Giữ đường vào đã có giúp không tạo coupling mới; KNSPLIT-2 sẽ tiếp tục đảo phụ thuộc. Các route inline cũ đã chuyển vào tệp `legacy-<module>.ts`/`kn-legacy-extension.ts`; không đổi kiểm quyền, trạng thái, guard hay cache gửi lại.

## OpenAPI

Nguồn ở `contracts/openapi/modules/<owner>/{application,admin}.yaml`, tự quét cả thư mục con. Phần info/server/security ở `<surface>.base.yaml`; `application.yaml`/`admin.yaml` là đầu ra. Fragment chứa `paths:` và/hoặc `components:` với section chuẩn OpenAPI. Trước mỗi path/component thêm `# @ag-order: <số nguyên>` ở đầu dòng. Số hiện tại giữ nguyên thứ tự bản nền; nguồn mới dùng số lớn hơn hoặc đặt vào khoảng thứ tự mong muốn. Nếu trùng thứ tự, thứ tự đường dẫn tệp rồi khóa quyết định ổn định. Không tách các thao tác cùng path sang hai chủ sở hữu: bộ ghép từ chối path/component trùng.

Nội dung YAML được ghép nguyên văn, không chuyển JSON hay đổi cách scanner nhận diện route. Bộ sinh OpenAPI hiện hành tiếp tục kiểm cấu trúc và tham chiếu. Fragment dùng chung đặt `common`, phần có một chủ sở hữu đi theo tag và đồ thị tham chiếu của operation. Marker kết thúc `# @ag-end` tùy chọn giữ dòng trống YAML gốc mà không tạo blank-EOF trong fragment; bộ ghép bỏ marker. Không sửa tệp migration trong gói WH-A.


## Ngữ cảnh của biểu mẫu (KNSPLIT-2)

Cấu hình ứng dụng được truyền vào danh sách Profile khi dựng cap/forms. Profile.ResolveActor nhận Principal đã xác thực và cấu hình profile; ứng dụng trả ResolvedActor gồm vai, phòng ban, phạm vi đơn vị, Nút và bộ lọc cơ sở/hộ. KN cung cấp extension.FormProfiles(); gốc lắp ráp chỉ nối cấu hình, forms không import KN.

Resolver trả handled=true khi có ngữ cảnh ứng dụng. Không đủ quyền phải trả lỗi; lỗi được trả thẳng và không rơi về quyền realm. handled=false chỉ dành cho trường hợp không có ngữ cảnh ứng dụng áp dụng, khi đó chỉ xét vai realm đã cấu hình. Không suy ngữ cảnh ứng dụng ngầm từ Principal. Chính sách phòng ban và giới hạn mục tiêu thuộc ứng dụng. Forms luôn kiểm phạm vi đơn vị cha trước bộ lọc mục tiêu.

ActorRef và TenantID lấy từ Principal; ResolvedActor không có trường để thay hai giá trị đó. Callback chỉ cấu hình phía server, không nhận qua HTTP và không xuất trong JSON của Profile. Cấu hình mới cần kiểm lỗi từ chối không fallback, phạm vi/giới hạn mục tiêu và cách ly tenant qua DB thật. Mẫu chứng minh: TestRegisteredActorDatabaseTenantIsolation và TestApplicationFormRegistrationPreservesPolicy.

KNSPLIT-2-FORMS-A đã đăng ký các consumer KN trước; bước B bỏ nhánh KNContext khỏi lõi forms và chuyển fixture sang resolver. Profile chưa đăng ký không được suy quyền phiên ứng dụng. Đây chỉ là phần biểu mẫu; các registry và ranh giới dữ liệu khác vẫn cần hoàn tất để nghiệm thu tách KN.


## Policy phiên của reports (KNSPLIT-2-REPORTS-A)

`reports.Deps.ResolveSession` nhận `port.SessionResolver` do ứng dụng cung cấp. Projection chỉ chứa grant và bộ lọc, không actor/tenant: capability luôn giữ Principal đã xác thực. Operation là forms.read/dispatch.read/dispatch.write; handled denial hoặc error là kết quả cuối, thiếu grant/filter/phạm vi dispatch thì từ chối. Realm đọc mẫu giữ ưu tiên hiện hành. KN đăng ký `extension.ReportSessionResolver(orgs)` tại assembly/worker và KN77; bước B đã gỡ adapter legacy: thiếu resolver hoặc unhandled không cấp quyền phiên; không suy từ Principal.KN. Native realm đọc form vẫn giữ nguyên. Các role group realm cho HTX/CGH chưa được gỡ trong bước này. Không nhận resolver từ HTTP hoặc JSON.


## Nhóm quyền và cấu hình reports (KNSPLIT-2-REPORTS-C)

`reports.Deps.Policies` nhận các `port.Policy` tin cậy lúc lắp ráp. Port chỉ DTO, reports/app clone thành registry bất biến; giữ native roles của capability, nối các quyền theo operation/source và các vai có organization scope. Tham số attachment do ứng dụng đăng ký, xung đột key bị chặn ngay bootstrap. KN sở hữu `extension.ReportPolicy()`, đăng ký tại API/worker/KN77. Bước D đã gỡ adapter cũ: slice nil hoặc rỗng đều chỉ giữ native capability grants, không suy vai hay attachment parameter của ứng dụng. Không nhận policy từ HTTP; actor/tenant/scope dữ liệu không do registration đúc.


## Catalog reports (KNSPLIT-2-REPORTS-E)

`reports.Deps.Catalog` nhận các `port.CatalogRegistration` lúc lắp ráp. Native sources/templates thuộc capability; ứng dụng đăng ký metadata của mình qua DTO, reports/app tạo registry bất biến riêng cho mỗi Service. Không đăng ký qua HTTP, không mutate global Sources. Cùng catalog được dùng khi validate mẫu, kiểm source role, giải template, chuẩn hóa lịch/bộ lọc và dựng cột xuất. KN sở hữu `extension.ReportCatalog()`; fixture lắp ráp thật đặt ở tests/reports và KN77. F đã bỏ legacy adapter: nil hoặc empty slice chỉ có native4source/4template CGH/HTX; KN phải đăng ký catalog rõ ràng. Cột/tiêu đề/bố cục cũ được bảo vệ bằng golden legacy byte.
