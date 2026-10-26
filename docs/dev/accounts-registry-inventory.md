# KNSPLIT-2-ACCOUNTS-REGISTRY (#52): kiểm kê và thứ tự chuyển

Kiểm kê trên dev 407831bf. Tài liệu này chưa xác nhận đã đảo phụ thuộc hoặc gỡ bridge. #51 (SessionPolicies), #49 (grant scope) và #63 (ScopedDutyHolders) là các gói riêng, không làm trùng.

| Nhóm | Điểm hiện tại | Bước shared cần làm | Bước chủ ứng dụng cần làm |
| --- | --- | --- | --- |
| Vai realm, MFA, loại tổ chức | port/grants.go; domain/account.go; app/service.go, users.go, mfa.go, orgstatus.go; handler/http.go catalog | Catalog bất biến theo instance; cả catalog HTTP và kiểm quyền ghi dùng cùng snapshot; giữ quyền nền tảng, HTX và MKG; không sửa map toàn cục khi đăng ký | KN cung cấp các định nghĩa vai và yêu cầu TOTP D24 qua adapter; không lấy quyền từ SelectedSession để cộng vào realm |
| Assignment, node, thuộc tính | domain/kn.go, kn_attrs.go; app/kn.go, kn_attrs.go | Port chính sách kiểm/chuẩn hóa theo chủ ứng dụng; sổ, transaction tenant, kiểm node/phòng hiện hành vẫn do accounts kiểm; callback lỗi không fallback | KN sở hữu danh mục vai, nhóm, duty, cấp quản lý và luật phạm vi; chuyển trước khi gỡ luật legacy |
| Đơn vị nội bộ, delegation | app/internal_unit.go, kn_attrs.go; store/kn_attrs.go | Giữ hiệu lực/thu hồi, cùng assignment/node/department; không biến delegated power thành duty | Adapter chủ ứng dụng định nghĩa allowed powers và ngữ nghĩa quản lý; scope CNTS thuộc #49 |
| Job khóa không hoạt động | app/dormant.go; store/dormant.go | Đã dùng tenant tường minh, tham số theo tenant, không đọc Principal.KN; không thêm registry chỉ để đổi tên | Kiểm khi chuyển tenant: account projection, activity, reactivation và idempotency đi theo đúng tenant; thuộc các gói 4b |

## Các ràng buộc chuyển hai bước

1. Thêm cấu hình generic và consumer trước, giữ bridge khi chưa cấu hình. Cấu hình theo instance, không đăng ký KN vào global map hoặc làm nhiễm instance native khác.
2. Không đồng nhất MFARoles (kiểm Admin API) với LoginTOTPRoles (yêu cầu đăng ký TOTP lúc đăng nhập). Các vai KN phải tiếp tục cần TOTP trong giai đoạn chuyển; không gỡ trước khi owner adapter và realm contract đã được kiểm.
3. Không xóa mọi tham chiếu ttkn_province: account_admin_province và content_editor_province đang dùng loại tổ chức đó cho quy tắc nền tảng. Phải phân loại theo chủ hành vi, không thay chuỗi hàng loạt.
4. Kiểm role catalog phải đồng bộ NormalizeRoles, ManagedOf, RolesFitOrg, Grantable, RequiredActions, MFAStatus, thay vai và catalog HTTP. Chỉ chuyển endpoint catalog sẽ tạo lệch giữa giao diện và quyền ghi.
5. Callback không được tự cấp quyền quản trị nền tảng, bỏ MFA, đổi tenant hoặc ghép quyền từ các phiên khác. Các ca âm thu hồi/hết hạn/node/tenant cần chạy DB thật sau mỗi bước.
6. Chỉ gỡ bridge sau khi chủ KN có adapter, assembly đã inject và Claude Review OK. Không tự sửa lane KN. Không thay HTTP/DDL hoặc ledger bytes nếu chưa có gói hợp đồng riêng.

## Trạng thái

#52 đã claim và tạo worktree riêng; hiện mới kiểm kê. Chưa có thay đổi production, chưa có kiểm thử cho #52 và chưa ready. Nguồn dev hiện tại đã có Navigation #44; các thay đổi module.go/Service phải giữ trường đó cùng SessionPolicies #51 khi gộp sau này.
