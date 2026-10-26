# Catalog vai realm của accounts

`accounts.Deps.Roles` nhận `*port.RoleCatalog` bất biến, do máy chủ cấu hình cho từng instance. `LegacyRoleCatalog()` chụp danh mục hiện hành; `ExtendRoleCatalog(base, registrations...)` tạo bản mới với vai bổ sung. Các collection đầu vào/đầu ra được sao chép; đăng ký không sửa global map, base hoặc instance khác, kể cả khi đăng ký lỗi giữa chừng.

Vai đăng ký là managed role. Nó có yêu cầu LoginTOTP, đánh dấu MFARole, các loại tổ chức được phép và danh sách grantor đã thuộc AdminRoles nền tảng. MFARole luôn kéo theo LoginTOTP. Không ghi đè vai có sẵn; không tạo admin, cấp platform_admin, sửa floor MFA hoặc mở rộng quyền trên các vai native khác. Vai dịch vụ/marker/default không được quản lý qua cổng này.

Module chụp bridge legacy một lần khi không cấu hình Roles. Catalog cấu hình nhưng không hợp lệ bị từ chối trước đăng ký route; Service dựng trực tiếp cũng kiểm trước thao tác người dùng/worker. Không fallback từ cấu hình lỗi. Catalog HTTP và consumer tạo/sửa vai, lọc managed roles, kiểm loại tổ chức, TOTP và trạng thái MFA dùng cùng snapshot. Caller vẫn phải có realm AdminRoles và MFA; phiên ứng dụng không cộng quyền vào realm, không đổi tenant/node.

Đây là bước đầu #66 của #52, chưa gỡ định nghĩa KN khỏi bridge. Không thay ledger/schema/HTTP contract, không sửa ACL hoặc realm. Vai bổ sung phải tồn tại trong realm Keycloak; registry không tự tạo vai realm. Khi adapter chủ KN và kiểm thử đã sẵn sàng, bước tiếp theo mới chuyển định nghĩa legacy; assignment/attrs/unit/delegation vẫn thuộc #52. Không dùng cấu hình này để tránh kiểm module enablement hiện có.

## Nền native để ghép khai báo của owner (#96)

`NativeRoleCatalog()` là nền cấu hình tùy chọn chỉ gồm chính sách realm native. Gốc lắp ráp có thể gọi `ExtendRoleCatalog(port.NativeRoleCatalog(), registrations...)` sau khi owner đã cung cấp và kiểm chứng các khai báo của mình. Factory chọn danh sách vai native tường minh, giữ quyền cấp vai của ba quản trị tài khoản, MFA/LoginTOTP native và các ràng buộc loại tổ chức hiện hành. Nó chụp các collection độc lập; không sửa global policy hay lấy chính sách từ phiên/request. Vai bổ sung không trở thành quản trị tài khoản.

Quản trị tỉnh trong nền native vẫn cấp được `coop_manager`. Các ràng buộc loại tổ chức của `account_admin_province` và `content_editor_province` vẫn được giữ, dù có liên quan tổ chức dùng bởi ứng dụng. Factory chọn khóa native và sao chép giá trị từ map hiện có, không suy ra quyền hoặc quyền sở hữu bằng prefix tên vai.

Gói #96 chưa đổi `LegacyRoleCatalog()`, `ExtendRoleCatalog(nil, ...)`, `accounts.Deps.Roles` khi bỏ trống hoặc cấu hình lắp ráp mặc định. Không gỡ vai đang dùng khỏi realm hay bridge; không inject khai báo owner. #95 cung cấp khai báo owner và kiểm parity hành vi; việc chuyển cấu hình chạy thật thuộc #57, sau khi đủ owner adapters và bằng chứng của consumer.

**Thứ tự collection:** `ExtendRoleCatalog` nối các vai mới vào cuối. Vì vậy nền native cộng khai báo owner có thể khác thứ tự `ManagedRoles()`, `LoginTOTPRoles()` và `Grantable()` của bridge, dù cùng chính sách. Kiểm native của #96 giữ thứ tự tương đối hiện hành của vai native, không khẳng định parity thứ tự toàn bộ catalog sau composition. #57 phải đối chiếu thứ tự observable và golden trước chuyển cấu hình; không cập nhật golden chỉ để che khác biệt này.

Kiểm thử thuần đối chiếu nền native với chính sách hiện hành, các sàn MFA/cấp quyền, composition với vai giả lập, defensive copies và lỗi đăng ký nguyên tử. Kiểm HTTP dùng cùng fixture #66 với nền native được inject, xác nhận vai giả lập tuân thủ MFA, phiên ứng dụng không thêm quyền admin và instance mặc định vẫn dùng bridge.
