# Chính sách phiên của accounts

`accounts.Deps.SessionPolicies` là cấu hình của một instance, do gốc lắp ráp nhận từ chủ ứng dụng. Callback không được serialize.

- `SupportAudit(ctx)` trả nhãn nhật ký hỗ trợ: context/session, người được hỗ trợ, vai hiển thị, phòng ban và lý do. Actor, tenant, thời điểm và trace vẫn do accounts lấy từ xác thực/máy chủ.
- `DefaultUnit(ctx)` trả đơn vị mặc định và cờ handled. Chủ phiên đã chọn phải được xử lý; provider lỗi hoặc từ chối phiên đã chọn không được rơi về tổ chức trong token hay dữ liệu legacy. Request native không chọn chủ có thể trả handled=false.

Đăng ký phải ở máy chủ, dùng chủ phiên đã xác thực. Payload riêng được giải mã ở adapter của ứng dụng; accounts không giải mã payload đó. Kết quả callback không cấp quyền sửa danh tính, vai realm, MFA hoặc tenant.

## Chuyển hai bước

#51 thêm port và các consumer shared. Khi chưa cấu hình callback, bridge legacy hiện hành tạm giữ cho KN; chưa coi việc đảo phụ thuộc accounts đã hoàn tất. #60 giao Claude adapter KN. Sau khi adapter, assembly và các kiểm thử của chủ ứng dụng đã gộp, gói tiếp theo mới gỡ bridge. Có cấu hình callback thì lỗi luôn là kết quả cuối, kể cả principal còn dữ liệu legacy hợp lệ.

Không thay HTTP/OpenAPI, migration hoặc hợp đồng lưu trữ. Không mở quyền quản trị đơn vị nội bộ bằng cổng đơn vị mặc định; nó chỉ chọn giá trị truy vấn mặc định. Các kiểm tra quyền ghi hiện có được giữ nguyên.
