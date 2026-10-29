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

## Luật lần gán vai trò KN (#72)

Factory: `extension.AssignmentRules() accport.AssignmentRules`. Adapter không có trạng thái và không nhận actor hay tenant.

- `ValidateAssignment` kiểm theo đúng thứ tự cũ:
  1. vai thuộc `kn_tw | kn_tinh | kn_xa | admin`;
  2. cấp quản lý thuộc danh mục KN (bản tạm Q-05/Q-059, 7 mã);
  3. Admin không có phòng ban và không có xã;
  4. vai khác Admin phải có Nút và ít nhất một phòng ban;
  5. chỉ Cấp Xã có xã phụ trách.

  Lỗi trả về là `accport.AssignmentRuleError{Field, Msg}`, giữ nguyên câu chữ hiện hành.
- `ValidateNodeKind`: `kn_tw` gắn `ttkn_national`; `kn_tinh` và `kn_xa` gắn `ttkn_province`; Admin không hạn loại Nút.
- Accounts vẫn tự kiểm: tenant, Nút đang hoạt động, phòng ban chưa xóa, xã thuộc Nút, phạm vi người gọi, Admin chỉ do `platform_admin` cấp, MFA, sổ chỉ ghi thêm.
- Cách gọi ở gốc lắp ráp: `accounts.Deps{AssignmentRules: extension.AssignmentRules()}`. Phần nối này do Codex làm (shared); gói này không sửa assembly.
- Thứ tự lỗi khi một đầu vào sai nhiều chỗ: khi đã cấu hình provider, accounts (#68) kiểm cú pháp chung trước (định dạng mã vai/phòng ban/xã, thời hạn), rồi mới gọi adapter. Với đầu vào sai cả cú pháp lẫn danh mục, lỗi báo ra có thể khác bridge. Ví dụ: vai rỗng báo "mã vai trò không hợp lệ" thay cho câu liệt kê vai. Mã HTTP vẫn là 422. Đây là thiết kế của #68, adapter không đổi được.

Bằng chứng parity:

- `internal/kn/extension/module_assignment_rules_test.go`: bảng lỗi (trường, câu chữ, thứ tự), loại Nút, danh mục khớp bridge, adapter không sửa đầu vào.
- `tests/knaccounts/assignment_rules_pg_test.go`: trên PostgreSQL thật, chạy ma trận tạo tài khoản KN (vai × Nút × phòng ban × xã × cấp quản lý, kèm `account_admin`), rồi thêm/sửa lần gán và đọc danh sách lần gán. Phản hồi đã chuẩn hóa (status, mã, trường, câu chữ, lần gán) giống hệt giữa module legacy (`AssignmentRules` nil) và module có adapter KN. Ma trận phủ đủ 7 luật của adapter.
