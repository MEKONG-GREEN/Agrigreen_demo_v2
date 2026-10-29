# Phần chủ KN của pf/accounts (KNSPLIT-2)

Hệ Khuyến nông khai báo các chính sách tài khoản của mình qua port chung của `pf/accounts`. Mã nằm ở `internal/kn/extension`; accounts không chứa luật KN mới. Gốc lắp ráp (shared, Codex) mới là nơi inject vào `accounts.Deps`. Mặc định hiện hành (`nil`) vẫn dùng bridge legacy cho tới khi rollout #57.

## Vai realm KN (#95)

Factory: `extension.RoleRegistrations() []accport.RoleRegistration` và `extension.RoleCatalog(base *accport.RoleCatalog) (*accport.RoleCatalog, error)`.

| Vai | LoginTOTP (D24) | MFARole | OrgKinds | Grantors |
|---|---|---|---|---|
| `kn_national_admin` | có | không | `ttkn_national` | `platform_admin`, `account_admin` |
| `kn_province_admin` | có | không | `ttkn_province` | `platform_admin`, `account_admin` |
| `kn_officer` | có | không | `extension_team` | `platform_admin`, `account_admin`, `account_admin_province` |
| `kn_user` | có | không | (không hạn) | `platform_admin`, `account_admin` |

- Cách gọi ở gốc lắp ráp: `roles, err := extension.RoleCatalog(accport.NativeRoleCatalog())`, có lỗi thì dừng khởi động, rồi truyền `accounts.Deps{Roles: roles}`.
- Fail closed: base `nil` hoặc không hợp lệ, hay base đã có vai KN (ví dụ `LegacyRoleCatalog()`), đều trả `ErrKNRoleCatalog`. Không có fallback về bridge và không override.
- Vai KN không phải vai quản trị tài khoản. Phiên ứng dụng KN không cộng quyền realm. Factory không đọc request, phiên hay trạng thái toàn cục. Mỗi lần gọi trả bản sao mới.
- Thứ tự collection khác bridge, vì vai KN được nối vào cuối. Chính sách thì giống hệt. Việc đối chiếu thứ tự trước khi chuyển cấu hình thuộc #57.

Bằng chứng parity:

- `internal/kn/extension/module_role_catalog_test.go`: snapshot chính sách (managed, MFA, LoginTOTP, OrgKinds, grantable theo người gọi) của legacy bằng snapshot của native + KN, và bằng golden `testdata/role_catalog.golden.json`. Kèm truy vấn từng vai, fail closed và các kiểm sao chép.
- `tests/knaccounts/role_catalog_pg_test.go`: trên PostgreSQL thật, cùng kịch bản HTTP (catalog `/v1/accounts/grants` theo người gọi; tạo tài khoản theo vai × loại tổ chức × người cấp; sửa lần gán; nhập CSV) cho kết quả giống hệt giữa module legacy và module có catalog KN. Kiểm thêm cách ly instance.
