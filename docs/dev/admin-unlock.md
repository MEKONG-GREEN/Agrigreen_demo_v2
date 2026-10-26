# ADMIN-UNLOCK (#42)

Backend đã có thao tác mở khóa chống dò Keycloak và ghi ledger/outbox với prev_cause=brute_force. Phần bổ sung là trường login_locked trên GET chi tiết tài khoản, giúp màn Admin phân biệt khóa đăng nhập sai với tài khoản enabled.

Field là optional boolean: chỉ GET detail có true/false sau khi đọc Keycloak thành công; list/create bỏ field, không thực hiện N+1 và không suy unknown thành false. enabled giữ nguyên trạng thái kích hoạt. Kiểm quyền Admin/MFA/tenant/tổ chức trước khi tra khóa; lỗi phụ thuộc giữ cơ chế accounts.dependency_unavailable. Không ghi trạng thái runtime này vào ledger.

Giao diện, schema OpenAPI và tệp sinh thuộc issue con #61; không tuyên bố #42 hoàn tất khi phần này chưa tích hợp. Không đổi luồng lý do >=20 ký tự, Idempotency-Key, xóa lockout hay điều kiện không mở khóa tổ chức đang vô hiệu hóa.

Kiểm: HTTP PG thật trước/trong/sau khóa, dịch vụ unavailable, list không đọc lockout và yêu cầu ngoài tỉnh bị từ chối trước lookup; giữ ca hiện hành audit brute_force.

## Cập nhật sau P0-VPS-OPS (#131, rebase 26/10/2026)

Realm đổi từ khóa hẳn sang khóa TẠM 15 phút (permanentLockout false, MULTIPLE, waitIncrementSeconds = maxFailureWaitSeconds = 900): khóa tự hết, Admin vẫn mở sớm được qua POST /v1/accounts/users/{id}/unlock. GET chi tiết thêm login_locked_until (RFC 3339, optional) — chỉ có khi login_locked=true và suy được: lastFailure của Keycloak attack-detection + keycloak.DefaultLockoutWait (15 phút, ghi đè bằng AG_KC_LOCKOUT_WAIT; lockout_test giữ khớp hai tệp realm). Không suy được → bỏ field, không đoán. Sau mốc tự hết, chi tiết trả login_locked=false và unlock trả accounts.state_conflict (không ghi sự kiện mở khóa rỗng).

Port mới: Directory.LoginLockStatus (Locked, Until). Bản trong bộ nhớ mô phỏng khóa tự hết theo đồng hồ SetNow; emulator trả lastFailure như Keycloak 26.
