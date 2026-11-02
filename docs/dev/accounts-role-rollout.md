# Accounts registry: rollout vai và phần còn lại (#193)

## Lát hoàn chỉnh

`assembly.AccountRoleCatalog()` ghép `accounts/port.NativeRoleCatalog()` với actual `extension.RoleCatalog` / `RoleRegistrations` của #95. API và cả năm đường khởi tạo accounts của worker dùng cùng composition; lỗi cấu hình dừng đường khởi tạo, không fallback về policy legacy.

`WithRoleCatalogOrder(catalog, reference)` là primitive generic chỉ đổi thứ tự của snapshot mới: ManagedRoles, MFARoles, LoginTOTPRoles, AdminRoles và từng danh sách Grantable. Mọi danh sách phải là hoán vị đầy đủ, grantor keys phải bằng nhau. Thiếu/thêm/trùng/đổi vai hoặc grant bị từ chối nguyên tử. Policy OrgKinds và nội dung catalog luôn từ catalog owner; reference không thể nới quyền hoặc bỏ TOTP. Input/output là trusted server config, không lấy từ request/session.

Trong rollout này reference thứ tự vẫn là compatibility snapshot. Đây là phụ thuộc **thứ tự**, không nguồn policy. Nó còn được giữ cho #57 / #196 đến khi có hợp đồng ordering thay thế đã kiểm parity; không tuyên bố bridge đã retire. Giữ nguyên nil/default LegacyRoleCatalog cho module chưa được rollout và domain compatibility helpers. Không đổi realm/client sống hoặc default MFA D24.

## Kiểm kê production constructors

| Đường | Catalog mới | Năng lực dùng |
|---|---|---|
| `cmd/api-platform/assembly/modules.go` | AccountRoleCatalog | Admin API create/grant/change/import, MFA/required-actions, KN sessions |
| `cmd/worker/notify.go` | workerAccounts → AccountRoleCatalog | người nhận, đơn vị và mail directory |
| `cmd/worker/reports.go` | workerAccounts → AccountRoleCatalog | mail directory, không cần pool accounts |
| `cmd/worker/accounts.go` | workerAccounts → AccountRoleCatalog | org-status inbox, giữ handler/transaction hiện hành |
| `cmd/worker/main.go` job lịch công tác | workerAccounts → AccountRoleCatalog | context/directory/role holders |
| `cmd/worker/main.go` dormant | workerAccounts → AccountRoleCatalog | job dormant và policy MFA/managed roles |

Sau thay đổi, tìm `accounts.New(` ngoài test chỉ còn API constructor và helper worker. CLI dùng các đường worker trên; không tạo binary/registry mới. Không có HTTP/OpenAPI/menu/web/migration mới, không gọi port hoặc I/O trong transaction mới.

## Phân loại phần còn lại

| Nơi hiện hành | Generic kernel giữ shared | Luật owner còn cần chuyển |
|---|---|---|
| `app/internal_unit.go` | ledger, projection, tenant RLS, idempotency, log, phân trang, cập nhật trạng thái | `checkUnitRefs` giới hạn TTKN quốc gia/tỉnh; `SeedInternalUnits` + seeddata là định nghĩa KN; `knCaller` là quyền KN |
| `domain/internal_unit.go`, `port/internal_unit.go` | shape/code/name/sort validation và immutable snapshot | loại đơn vị, loại phòng và seed cụ thể |
| `app/kn_attrs.go` Create/RevokeDelegation/PeopleScope | ledger/projection, atomic idempotency, truy vấn thời hạn/thu hồi | RequireKN, vai Tỉnh/phòng, manager, read-only/support, quyền cụ thể và phạm vi người nhận |
| `domain/kn_attrs.go` NormalizeDelegation | kiểm kiểu, độ dài và thời gian an toàn | danh mục quyền và giới hạn thời hạn KN |
| `app/internal_unit.go` ListInternalUnits | truy vấn current tenant và fail-closed selected owner | `SessionPolicies.DefaultUnit` của #60 đã nối trong API; không cần registry thứ hai |
| `app/kn_support.go` | ghi audit hỗ trợ | `SessionPolicies.SupportAudit` của #60 đã nối trong API |
| assignment / attributes | shared kiểm tenant/node/current-state; catalog instance | #72 AssignmentRules và #79 AttributeCatalog đã nối API ở #52 |
| dormant / org-status / transfer writers | lifecycle và ledger generic, không tạo registry job giả | tenant application membership/activity/signed profile còn ở #87/#90–94; không giả Principal hoặc global tenant rewrite |

#195 (lane KN) chốt exact owner contract/adapter cho đơn vị và ủy quyền; #196 (shared) rollout sau #193 và hợp đồng owner đã vào dev. Không làm trùng #60/#72/#79 hoặc các gói tenant move #87–94. #57 chỉ retire sau rollout và direct consumer coverage đầy đủ, không tạo vòng phụ thuộc.

## Giới hạn bằng chứng

Catalog role rollout không chứng minh tenant cutover, realm OIDC sống, năm tenant-boundary cases hoặc G1 hoàn tất. Phiên hiện hành, MFA, native HTX/MKG, hỗ trợ, ủy quyền và RLS không được nới. Không đổi golden hoặc baseline để che ordering. Kết quả lệnh/PG/mutation/CI đúng SHA ở bình luận bàn giao #193; không coi unit test là PG/HTTP live.
