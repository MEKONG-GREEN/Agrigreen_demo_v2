# ADMIN-UNLOCK (#42)

Backend đã có thao tác mở khóa chống dò Keycloak và ghi ledger/outbox với prev_cause=brute_force. Phần bổ sung là trường login_locked trên GET chi tiết tài khoản, giúp màn Admin phân biệt khóa đăng nhập sai với tài khoản enabled.

Field là optional boolean: chỉ GET detail có true/false sau khi đọc Keycloak thành công; list/create bỏ field, không thực hiện N+1 và không suy unknown thành false. enabled giữ nguyên trạng thái kích hoạt. Kiểm quyền Admin/MFA/tenant/tổ chức trước khi tra khóa; lỗi phụ thuộc giữ cơ chế accounts.dependency_unavailable. Không ghi trạng thái runtime này vào ledger.

Giao diện, schema OpenAPI và tệp sinh thuộc issue con #61; không tuyên bố #42 hoàn tất khi phần này chưa tích hợp. Không đổi luồng lý do >=20 ký tự, Idempotency-Key, xóa lockout hay điều kiện không mở khóa tổ chức đang vô hiệu hóa.

Kiểm: HTTP PG thật trước/trong/sau khóa, dịch vụ unavailable, list không đọc lockout và yêu cầu ngoài tỉnh bị từ chối trước lookup; giữ ca hiện hành audit brute_force.
