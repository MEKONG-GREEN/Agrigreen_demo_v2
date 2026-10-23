# Quy tắc kiểm lần gán theo chủ ứng dụng (#68)

`accounts.Deps.AssignmentRules` là cấu hình máy chủ theo instance; nil giữ nguyên đường legacy, kể cả thứ tự lỗi. Khi có cấu hình, accounts chuẩn hóa mã và danh sách, bỏ trùng, giữ thời hạn UTC/microsecond và kiểm valid_to trước khi gọi provider. Provider chỉ nhận Role, OrgRef, Departments, MgmtLevel, Communes; không nhận actor/tenant/thời hạn hay metadata ghi sổ, không trả giá trị thay thế. Các slice được sao chép nên thay đổi của callback không tác động dữ liệu lưu.

`ValidateAssignment` kiểm danh mục vai/cấp quản lý và tổ hợp thuộc tính; `ValidateNodeKind` kiểm loại node đã tra được. Provider lỗi là kết quả cuối, không rơi về legacy. Adapter có thể trả `port.AssignmentRuleError{Field, Msg}` để dùng lỗi HTTP validation hiện hành; không cần import domain. Callback được bỏ khỏi JSON.

Consumer chung là checkAssignment của luồng tạo người dùng, cấp và sửa lần gán. Accounts vẫn kiểm quyền người gọi, tenant, phạm vi/node đang hoạt động, phòng ban không bị xóa, xã nằm trong node, Admin chỉ do platform_admin cấp cho platform_admin, MFA/Keycloak, chống trùng và transaction sổ chỉ ghi thêm. Node-kind provider cũng được gọi cho Admin có node tường minh sau kiểm quyền Admin. Không đổi RuleVersion, HTTP, DDL hoặc dữ liệu sổ.

Gói này chưa chuyển attrs, delegation, danh mục HTTP/nhãn/CSV, quyền chỉ đọc của ngữ cảnh hoặc chính sách grant CNTS. Chưa gỡ bridge KN. Chủ KN cần adapter riêng và assembly inject, có kiểm thử/cross-review rồi mới gỡ luật legacy; không dùng port này để đưa quy tắc KN mới vào shared. Các bộ danh mục và cách đọc ngữ cảnh phải chuyển đồng bộ trước khi thay mã/ý nghĩa quyền hiện hành.

Kiểm thử thuần kiểm bridge/error order, cấu trúc/thời hạn, sao chép dữ liệu và lỗi provider không fallback. Kiểm thử HTTP trên PG thật kiểm create/grant/change, từ chối của chủ, tenant/node/phòng/xã, scope, MFA và quyền Admin không bị bỏ qua.

Review 23/10: lõi luôn kiểm tập con xã với mọi lần gán có commune_codes, không chỉ KNXa; Admin không có phòng ban/xã ngay cả khi provider cho qua. Typed-nil AssignmentRuleError được coi là lỗi cấu hình 5xx an toàn khi log, không 422. Regression PG kiểm cả ledger và projection không tăng khi từ chối.

Khi tích hợp cùng #78, phải giữ kiểm AttributeCatalog().LevelLabel(in.MgmtLevel) ngay sau normalizeAssignment trong checkAssignment. Callback AssignmentRules chỉ chốt tổ hợp thuộc tính, không thay kiểm danh mục hiện hành này. Không bỏ mất Service/Deps/New wiring của một trong hai gói khi giải xung đột.