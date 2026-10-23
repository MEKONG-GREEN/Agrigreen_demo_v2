# KNSPLIT-2-MENU-PARITY (#122)

Phụ thuộc owner #76, không sửa builder/tài liệu KN. Factory cần `extension.MenuCatalog(hrm.MenuSource(), price.MenuSource(), contact.MenuSource())`, xử lý mọi error; Registry + NavigationPolicy đưa vào party.Deps, UseFormAvailability nối sau reports. Không nối trước khi owner có review chéo và vào dev.

Phần shared đã chuẩn bị: registry presentation giữ nhóm mà owner chủ động trả rỗng ngay từ đầu; nhóm có mục nhưng bị module gating lọc hết vẫn bị loại. Không hard-code tên nhóm, không nới duplicate-key, không đổi kiểm phiên/tenant, không đổi policy lỗi thành compatibility fallback.

Regression generic `TestRegisteredMenuPreservesOwnerEmptyGroupAndPrunesFilteredGroup`: baseline thất bại vì mất nhóm chủ động rỗng; bản sửa phải giữ nhóm đó nhưng vẫn bỏ nhóm có module tắt và không đổi landing đang bật. Pure test này không thay kiểm HTTP/PG parity toàn gói.

Ca Admin không gắn Nút + hai đơn vị cùng Code: metadata hợp lệ hiện bị Catalog.Key chỉ dùng Code làm registry từ chối. Đã gửi owner76 finding builder.go:486/490 và regression registry thật (snapshot a978763b, pure9PASS nhưng ca multiunitFAIL). Owner cần namespace khóa cha theo OrgRef khi trùng, giữ ca không trùng/route/trạng thái; shared không xóa bớt metadata theo Code. Chờ cập nhật owner và review lại.

Còn lại trước ready: dependency76 vào dev; lắp ráp; parity trực tiếp `/v1/menu`, `/v1/departments/{code}/modules`, ContextNavigation ở Menus nil/configured (session/alias/traits/form availability/module gating/default); duy trì snapshot 657 ca từ nguồn compatibility cho tới53 gỡ bridge. Unknown profile hoặc generic/rỗng chưa map vẫn deny ở configured path theo75, không ép parity bằng fail-open. Chưa ready hoặc merge toàn122.
