# Catalog thuộc tính tài khoản theo instance

Ticket shared #78, con #52/#47; adapter KN thuộc #79. Không thay đổi schema, API hay lịch sử sổ.

accounts.Deps.Attributes *accounts/port.AttributeCatalog là snapshot bất biến: NewAttributeCatalog(AttributeCatalogSpec) kiểm mã, nhãn, phiên bản, vai và xung đột mã/nhãn CSV bằng tabular.NormalizeHeader; getter và Clone trả bản sao. AttributeCatalogSpec gồm Version, Provisional, Levels, Groups, Duties, Roles. AttributeRole tách GroupAndDuties, ScopedTargets, ManagementPermissions. AttributeGroup có Manager, ReadsAllDepartments, ReadOnly; mã/nhãn không tự sinh quyền. Capabilities(role, level, group) từ chối mã không biết và nhóm không thuộc vai; ReadOnly vẫn hạn chế vai không có quyền quản lý.

Nối đồng bộ catalog HTTP, nhãn assignment/attributes, CSV quản lý, chuẩn hóa thuộc tính, chọn/giải context, đọc chéo phòng và phạm vi nhân sự. Cờ Manager không suy ra ReadsAllDepartments. Đọc chéo phòng luôn chỉ xem; chỉ xem bỏ quyền ủy quyền. Thuộc tính, lần gán và trạng thái phòng ban hiện hành được đọc lại; catalog không thay guard tenant/Nút/phòng/MFA/hiệu lực/thu hồi/UUID/giới hạn 500. Các tổ hợp role/group không hợp lệ trong dữ liệu lưu bị từ chối.

nil tại accounts.New chọn một bản chụp bridge cũ riêng cho module; snapshot được cấu hình nhưng không hợp lệ gây lỗi lúc khởi động và không rơi về bridge. Service trực tiếp với cấu hình lỗi từ chối trước phụ thuộc. Hai instance không chia sẻ slice hay trạng thái.

## Ghép với owner

#79 cung cấp catalog KN, assembly và kiểm parity. Chưa gỡ bridge KN và chưa hoàn tất #52. Cổng ghi assignment vẫn do #68/#72 sở hữu; trước khi nối đồng bộ #68+#72+#79, mã cấp quản lý riêng có thể đọc/chuẩn hóa CSV nhưng ghi vẫn bị normalizer native từ chối. Không triển khai thay thế hoặc normalizer thứ hai trong #78. Thứ tự lỗi kiểm cấu trúc native được giữ trước kiểm hạn chế catalog.

Danh mục quyền ủy quyền và KNContext.Can vẫn dùng bridge cũ. Không thêm riêng mã power: manager legacy được phép mọi power, cần gói owner đầy đủ để đổi nhất quán.

## Kiểm tra

Có ca thuần cho snapshot/clone/hai instance, cấu hình lỗi, xung đột CSV, mã lưu không còn hợp lệ, valid role × level × group × own/other/empty department parity, tổ hợp sai role/group bị từ chối, read-only không phụ thuộc quyền quản lý, chuẩn hóa UUID/giới hạn/khử trùng lặp, thứ tự lỗi native. Ca HTTP PostgreSQL kiểm catalog/nhãn riêng, thay thuộc tính có hiệu lực ngay với context cũ, chỉ xem ở phòng mình/phòng khác, ngừng phòng và thu hồi lần gán. Kết quả thực tế 21/10/2026 (Go 1.26.9):
- Lượt đầy đủ accounts/... + api-platform/assembly ban đầu exit 1 chỉ vì ba lỗi fixture mới: assertion CSV coi mã chuẩn hóa là fallback nhãn, thiếu Clock trong test thứ tự lỗi, HasDuty được kỳ vọng true ở context chỉ xem. Log thất bại được giữ nguyên.
- Sau khi tất cả runner kết thúc, chỉ sửa hai tệp test; production giữ nguyên commit 27190e2d. Full app kiểm lại exit 0/2.023s; ca HTTP PostgreSQL kiểm lại exit 0/92.508s, test 0.46s, không SKIP/INFRA.
- Production không đổi: các ca handler cũ đã PASS trong lượt 114.997s; domain 1.896s, keycloak 1.213s, port 1.010s, api-platform/assembly 86.707s PASS. Không chạy lại các gói đã đạt.
- build/vet/modgen-check/migratecheck/archlint/contractcheck/docscheck/knboundary đều exit 0; vet accounts và lint toàn repo sau sửa fixture đều 0 (0 issues). Boundary giữ 314, gofmt chỉ hai testdata archlint có sẵn; 35 golden KHTC không đổi.
- Hai cụm PG riêng 52913/fcb100e07162 và 56767/b65c73023271 đã dừng và dọn; không chạm PG chung.

Ghép với #68: kiểm LevelLabel chạy ngay sau normalizeAssignment cả khi có AssignmentRules; provider cho phép mã không thuộc catalog vẫn bị từ chối, còn lỗi provider giữ thứ tự trước kiểm catalog. Rebase giữ đồng thời Roles, SessionPolicies, Navigation, AssignmentRules và Attributes; không rollout owner catalog qua assembly trong gói này.
