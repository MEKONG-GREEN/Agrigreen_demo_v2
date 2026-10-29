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

## Danh mục và cờ quyền thuộc tính KN (#79)

Factory: `extension.AttributeCatalog() (*accport.AttributeCatalog, error)`. Mỗi lần gọi trả một snapshot bất biến riêng. Factory không nhận actor hay tenant và không đọc trạng thái toàn cục.

Phiên bản `accounts.kn-roles/2`, `Provisional=true` (bản tạm, chờ khách trả lời Q-059).

**Cấp quản lý.** Dùng chung danh mục với luật lần gán #72.

| Mã | Nhãn | Manager | ReadsAllDepartments |
|---|---|---|---|
| `chuyen_vien` | Chuyên viên - Cán bộ | | |
| `phu_trach` | Phụ trách | ✓ | |
| `pho_truong_phong` | Phó trưởng phòng | ✓ | |
| `truong_phong` | Trưởng phòng | ✓ | |
| `lanh_dao_don_vi` | Lãnh đạo đơn vị | ✓ | ✓ |
| `pho_giam_doc` | Phó Giám đốc | ✓ | |
| `giam_doc` | Giám đốc | ✓ | |

**Nhóm Cấp Tỉnh.**

- `vien_chuc`, `tong_hop`, `chu_nhiem`: không mang cờ quyền.
- `lanh_dao_phong`: Manager.
- `ban_giam_doc`: ReadsAllDepartments + ReadOnly, nghĩa là chỉ xem, kể cả ở phòng mình.

**Nhiệm vụ.** `qtnv_bao_gia`, `phu_trach_bieu`, `theo_doi_lich`.

**Vai.**

- `kn_tinh`: có nhóm và nhiệm vụ, có quyền quản lý.
- `kn_xa`: có giới hạn cơ sở và hộ.
- `kn_tw`, `admin`: không có thuộc tính.

**Quy tắc chung.**

- Manager không kéo theo đọc mọi phòng.
- Đọc chéo phòng luôn ở chế độ chỉ xem.
- Nhãn và mã không tự sinh quyền.
- Mã lạ bị từ chối, không fallback.
- Danh mục quyền ủy quyền (Powers) và cách ủy quyền **giữ nguyên bridge cũ**. Gói này không mở chúng.

**Nối ở gốc lắp ráp.** `attrs, err := extension.AttributeCatalog()`. Nếu có lỗi thì dừng khởi động, rồi truyền `accounts.Deps{Attributes: attrs}`. Theo #78, việc bật phải đồng bộ với `AssignmentRules` (#72) và `Roles` (#95), cùng một lần rollout (#57). Gói này không sửa assembly.

**Bằng chứng parity.**

- `internal/kn/extension/module_attribute_catalog_test.go`: snapshot đầy đủ của legacy bằng của KN và bằng golden `testdata/attribute_catalog.golden.json`. Snapshot gồm danh mục, nhãn, mã CSV (mã, nhãn, chữ hoa, khoảng trắng, mã lạ) và `Capabilities` của mọi tổ hợp vai × cấp × nhóm, kể cả rỗng và mã lạ. Kèm thêm: các quy tắc chủ, cách ly instance, nhất quán với #72.
- `tests/knaccounts/attribute_catalog_pg_test.go`, trên PostgreSQL thật:
  - Phạm vi kịch bản: danh mục HTTP, tạo tài khoản theo từng cấp quản lý, thuộc tính (nhóm × nhiệm vụ, mã lạ, sai vai), nhãn lần gán, ngữ cảnh phiên ở phòng mình / phòng khác / phòng chưa mở (`read_only`), nhập CSV theo nhãn cấp quản lý.
  - Kết quả của legacy giống hệt KN-attributes, và giống hệt khi bật cả ba adapter #95 + #72 + #79.
  - Catalog cấu hình sai bị từ chối ngay lúc khởi động.
- Khác biệt đã biết khi bật cả ba adapter: trong nhập CSV, ô cấp quản lý là nhãn lạ (ví dụ "Không có") báo "mã cấp quản lý không hợp lệ" thay cho câu danh mục. Lý do là #68 kiểm cú pháp trước khi gọi adapter. Dòng đó vẫn bị từ chối như cũ. Test chốt đúng 2 dòng khác biệt này; mọi phản hồi khác giống hệt. Cần Codex/PO xác nhận chấp nhận trước rollout #57.
