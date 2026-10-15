## ORGUI — tạo tổ chức và chuyển tạo HTX sang Neo D2

Phạm vi CODEX-G1 §4.1, quyết định PO 14/10/2026: tạo tổ chức trên web, chỉ Admin nền tảng
(`platform_admin`, `registry_admin`). Quản trị KN không tạo tổ chức. Không có migration.

| Nội dung | Hành vi bàn giao | Điểm hiện thực |
|---|---|---|
| Cơ cấu tổ chức | `/quan-tri/to-chuc`, hộp thoại theo loại, mã/tên, cấp trên đúng loại đang hoạt động, địa bàn còn hiệu lực theo ngày VN | `OrgCreateDialog.tsx`, `orgCreate.ts` |
| Quyền tạo | `POST /v1/orgs` yêu cầu platform_admin hoặc registry_admin cho mọi loại; KN nhận 403 trước khi ghi | `party/handler/registry.go`, `TestOrgNodeCreateOnlyPlatformAdmin` |
| HTX trong tổ chức chung | Tất cả Admin nhận 422 `party.cooperative_requires_anchor`, `redirect=/quan-tri/neo-htx` | HTTP + mock + OpenAPI |
| HTX trong nhập cây | Dòng HTX mới bị loại theo dòng, kể cả mã trống/chưa tồn tại; lý do chỉ sang Neo. HTX cũ vẫn di chuyển/đổi tên | `importOrgRow`, `TestOrgImportCreateRulesORGUI`, `TestOrgTreeExportImportRoundTrip` |
| Tạo HTX trong Neo | `POST /v1/htx-anchors` chọn đúng một trong `org_ref` đã có hoặc `new_cooperative`. Nhánh mới chỉ platform_admin/registry_admin | `anchor/app.Service.Create`, `party/port.CooperativeRegistrar`, assembly |
| Dữ liệu HTX mới | Tên, xã, cấp trên tùy chọn; MST chuẩn hóa lấy từ `mst` bên ngoài. Có thể kèm hồ sơ diện tích/ngày thành lập/trụ sở/tọa độ và người đại diện. Người đại diện đi thẳng kho định danh; không nằm trong sổ Neo | `party.RegisterCooperative`, quy tắc hồ sơ hiện hữu |
| Web Neo | Form chọn tỉnh/xã hiệu lực, tên, MST tùy chọn và căn cứ thỏa thuận. Chỉ Admin nền tảng thấy form; sau thành công chọn neo mới | `NewAnchorForm.tsx`, `/quan-tri/neo-htx` |
| Chống trùng | MST có neo bị chặn trước khi tạo Nút; tên HTX mới không trùng tên hiện hữu trong tenant; profile MST và neo cùng MST | dịch vụ Neo + Registry |
| Thử lại | Khóa tạo Nút `anchor-org:<key>`, khóa Neo `anchor-create:<key>` ổn định. Gửi lại hoàn tất phần thiếu; không tạo Nút thứ hai. Mất phản hồi/lỗi máy chủ thì web giữ nguyên dữ liệu và khóa, khóa sửa trường tới khi hoàn tất | ORG-NEO-001/002/003 |
| Sau tạo tổ chức | Tải lại cây, chọn Nút mới và mở các cấp trên; lỗi mã/cấp trên/địa bàn hiển thị tại trường tương ứng | OrgCreateDialog + Playwright |

### Ranh giới và phục hồi

Tạo Nút, hồ sơ và Neo là các bước qua module, **không phải một giao dịch phân tán nguyên tử**.
Nếu lỗi sau bước tạo Nút hoặc hồ sơ, gửi lại cùng yêu cầu và Idempotency-Key để hoàn tất.
Nếu một yêu cầu khác thắng cuộc đua MST, lỗi 409 chỉ neo đã có; không tự gộp dữ liệu, không tự xóa Nút.
Quản trị có thể chọn Nút cũ để lập neo hoặc đối chiếu neo thắng cuộc. Không sửa sổ chỉ ghi thêm.

D2 về đồng sở hữu MKG/HTX, nhận quyền MST, nhãn “Chưa xác nhận”, bí danh và kiểm toán giữ theo ADR-0052.
Phần KN tạo/nhập dữ liệu HTX trong mô hình cũ phải đối chiếu ở ADR-0055 khi tách KN; ORGUI không tự quyết thay CEO D2.
Các thao tác đổi tên/di chuyển Nút cũ giữ quyền và phạm vi hiện hữu. Các đường nhập hồ sơ HTX chuyên dụng không thuộc
phạm vi chặn `/v1/orgs` và `/v1/orgs/import`; sẽ được rà soát cùng ranh giới KN.

### Kiểm thử và mặc định

- ORG-CREATE-001: Admin bị chặn tạo HTX qua tổ chức chung; KN bị chặn mọi loại.
- ORG-NEO-001: PostgreSQL thật, giả lập lỗi sau tạo Nút, cùng khóa chỉ một Nút và một Neo.
- ORG-NEO-002: giao diện + HTTP client/mock thật; mất phản hồi sau ghi, thử lại cùng khóa không trùng.
- ORG-NEO-003: assembly + HTTP + PostgreSQL + vault thật; profile đầy đủ, phát lại, MST trùng, quyền/MFA/tenant/PII.
- Các kịch bản G1, acceptance và bộ sinh dữ liệu tổng hợp tạo HTX qua Neo; các fixture lõi Registry ghi rõ là setup nội bộ,
  không giả phản hồi HTTP thành công cho API đã bị đóng. Sổ hồi quy: [REGRESSION](../../REGRESSION.md).

Mặc định còn giữ: vị trí hộp thoại ở trang quản trị nền tảng; mọi loại tổ chức đã hỗ trợ trừ HTX dùng hộp thoại đó;
chọn ngày hiệu lực là hôm nay tại Việt Nam; không mở luồng tạo trên mobile.
