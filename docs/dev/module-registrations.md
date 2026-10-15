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
