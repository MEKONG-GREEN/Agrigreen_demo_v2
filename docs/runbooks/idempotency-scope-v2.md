# Nâng dấu chống gửi trùng v2

W11.46 / IDEM-SCOPE-001/002: dấu lệnh POST bao gồm đường dẫn tài nguyên thực, truy vấn chuẩn hóa, đơn vị, vai/MFA và dấu phạm vi do middleware giải ở server. Cache không chứa token. Module cấp quyền gắn phạm vi qua `WithAuthorizationScope` trước middleware chống trùng; hạ tầng không biết nghiệp vụ KN. Accounts giải lại toàn bộ vai/phạm vi/ủy quyền mỗi request.

Khóa vẫn theo tenant, giữ 48 giờ; khác người/phạm vi/tài nguyên trả 422 `idempotency_key_reused`, không phát phản hồi cũ và không chạy handler. Phiên và payload giống nhau vẫn trả đúng phản hồi cũ. Không sửa bảng/cache/sổ lịch sử.

Khi triển khai từ v1, khóa còn sống được tạo bằng dấu cũ sẽ nhận 422 vì không thể xác minh tài nguyên/phạm vi cũ an toàn. Không xóa cache, không tự đổi khóa rồi gửi lại thao tác chưa rõ kết quả. Đọc trạng thái nghiệp vụ để đối chiếu trước; nếu cần thử lại cùng thao tác với khóa cũ, chờ hết hạn cache (48 giờ), lớp idem của sổ vẫn bảo vệ phần đã commit. Chỉ tạo khóa mới cho thao tác mới sau khi đối chiếu. Có thể dừng nhận thao tác ghi 48 giờ trước nâng cấp để tránh lệnh đang chờ, theo cửa sổ vận hành được duyệt.

Kiểm thử: `TestRosterReplayAuthorization_W1146` (HTTP + PostgreSQL, thay tháng/phiên và thu hẹp cùng phiên); `TestIdempotencyContextBinding_W1146` (middleware accounts thật); `TestRequestHashAuthorization` (truy vấn/đơn vị/vai/MFA và dấu phạm vi, refresh AuthTime không đổi quyền).

Phản hồi chứa vị trí check-in là ngoại lệ lược dữ liệu nhạy cảm bằng `SetStoredBody`, tương tự bí mật kích hoạt: phản hồi gửi lại giữ metadata, `location=null`. Bản trả đầu và GET lịch có thể chứa vị trí cho người đăng ký/người duyệt khi tọa độ chưa bị xóa. Cache không giữ tọa độ ngoài bảng có hạn lưu; cờ point_stored là sự kiện đã từng ghi, không chứng minh vị trí còn tồn tại. Kiểm chứng bằng CNTS-CHECKIN-PRIVACY-001.
