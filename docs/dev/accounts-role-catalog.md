# Catalog vai realm của accounts

`accounts.Deps.Roles` nhận `*port.RoleCatalog` bất biến, do máy chủ cấu hình cho từng instance. `LegacyRoleCatalog()` chụp danh mục hiện hành; `ExtendRoleCatalog(base, registrations...)` tạo bản mới với vai bổ sung. Các collection đầu vào/đầu ra được sao chép; đăng ký không sửa global map, base hoặc instance khác, kể cả khi đăng ký lỗi giữa chừng.

Vai đăng ký là managed role. Nó có yêu cầu LoginTOTP, đánh dấu MFARole, các loại tổ chức được phép và danh sách grantor đã thuộc AdminRoles nền tảng. MFARole luôn kéo theo LoginTOTP. Không ghi đè vai có sẵn; không tạo admin, cấp platform_admin, sửa floor MFA hoặc mở rộng quyền trên các vai native khác. Vai dịch vụ/marker/default không được quản lý qua cổng này.

Module chụp bridge legacy một lần khi không cấu hình Roles. Catalog cấu hình nhưng không hợp lệ bị từ chối trước đăng ký route; Service dựng trực tiếp cũng kiểm trước thao tác người dùng/worker. Không fallback từ cấu hình lỗi. Catalog HTTP và consumer tạo/sửa vai, lọc managed roles, kiểm loại tổ chức, TOTP và trạng thái MFA dùng cùng snapshot. Caller vẫn phải có realm AdminRoles và MFA; phiên ứng dụng không cộng quyền vào realm, không đổi tenant/node.

Đây là bước đầu #66 của #52, chưa gỡ định nghĩa KN khỏi bridge. Không thay ledger/schema/HTTP contract, không sửa ACL hoặc realm. Vai bổ sung phải tồn tại trong realm Keycloak; registry không tự tạo vai realm. Khi adapter chủ KN và kiểm thử đã sẵn sàng, bước tiếp theo mới chuyển định nghĩa legacy; assignment/attrs/unit/delegation vẫn thuộc #52. Không dùng cấu hình này để tránh kiểm module enablement hiện có.
