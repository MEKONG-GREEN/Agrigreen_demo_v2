## ORGUI — Hộp thoại "Tạo tổ chức" trên web-ops (không migration)

Bối cảnh: backend `POST /v1/orgs` (`partyCreateOrg`, Admin API, MFA) đã có và đã kiểm thử, nhưng web-ops chưa gọi.
Trang `/quan-tri/to-chuc` ("Cơ cấu tổ chức") chỉ có cây, đổi tên, xuất/nhập tệp. Gói này thêm hộp thoại tạo. Không đổi
OpenAPI, không đổi hành vi backend. Mặc định dưới đây do nhóm rà soát chốt. Dòng "Ai tạo được" là **quyết định PO
(14/10/2026)**, không còn là giả định; các dòng khác vẫn là mặc định chờ PO.

| Câu hỏi | Phương án đã chọn | Ở đâu | Đổi thế nào nếu PO chọn khác |
|---|---|---|---|
| Đặt hộp thoại ở đâu | Trang quản trị nền tảng `/quan-tri/to-chuc`, không đặt trong màn KN, vì KN đang tách riêng | `apps/web-ops/src/app/quan-tri/to-chuc/page.tsx`, `src/admin/OrgCreateDialog.tsx` | Gắn `OrgCreateDialog` vào màn KN; logic nằm ở `src/admin/orgCreate.ts`, dùng lại được |
| Ai tạo được — **QUYẾT ĐỊNH PO 14/10/2026** | Xã (và các Nút hành chính / TTKN khác) là đơn vị hành chính. **Chỉ Admin nền tảng tạo**, trên trang quản trị nền tảng "Cơ cấu tổ chức" `/quan-tri/to-chuc`. Vai thấy nút "Tạo tổ chức": `platform_admin`, `registry_admin`, tức đúng các vai của menu trang, như mặc định đã đề xuất. **KN admin (`kn_national_admin`, `kn_province_admin`, …) không có quyền tạo.** Lưu ý: backend hiện vẫn cho `kn_province_admin` gọi `POST /v1/orgs` trong phạm vi tỉnh (`app.adminRoles`) | `ORG_CREATE_ROLES` trong `orgCreate.ts`; e2e "vai không có quyền" | Đã chốt. Việc còn lại để chặn ở API: bỏ `kn_province_admin` khỏi vai của `CreateOrg` (giữ cho các thao tác khác nếu cần), kèm kiểm thử Go. Gói này không đổi backend |
| Loại nào tạo được | Mọi loại trong `PartyOrgInput` trừ HTX: `ttkn_province`, `extension_team`, `ttkn_national`, `dcrd`, `mkg`, `partner`. Trường hiển thị theo loại (`KIND_RULES`): TTKN tỉnh chọn tỉnh; Tổ KN xã chọn xã thuộc tỉnh của cấp trên; TTKN QG/Cục KTHT/MKG không gắn ĐVHC, không cấp trên; Đối tác nhập tay mã ĐVHC (tuỳ chọn) | `KIND_RULES`, `CREATE_KIND_ORDER` | Bớt loại khỏi `CREATE_KIND_ORDER`, ví dụ khi `mkg` chỉ có một bản ghi mỗi tenant |
| HTX (`cooperative`) | Không tạo trong hộp thoại. Chọn loại HTX thì hiện liên kết sang `/quan-tri/neo-htx` (luồng HTX tạm theo MST, ADR-0052 / D2) | `KIND_RULES.cooperative.redirect` | Bỏ `redirect` và thêm trường hồ sơ HTX (MST, địa chỉ, diện tích); backend đã nhận `cooperative` |
| Quy tắc cấp trên | Theo `domain.AllowedParent`: TTKN tỉnh dưới TTKN QG hoặc ở gốc; Tổ KN xã bắt buộc dưới TTKN tỉnh; các loại còn lại chỉ ở gốc. Ô chọn chỉ liệt kê tổ chức **đang hoạt động** đúng loại. Server trả `party.invalid_hierarchy` (422) thì lỗi hiện trên ô Cấp trên, kèm `detail` | `parentOptions`, `allowedParent`, `fieldErrorsFromProblem` | Sửa `KIND_RULES[*].parents/root` cùng lúc với `domain.AllowedParent` |
| ĐVHC và ngày hiệu lực | Danh sách tra `GET /v1/masterdata/admin-units?at=<hôm nay giờ VN>`, sau đó lọc lại ở client: `valid_from ≤ hôm nay < valid_to` (`valid_to` không bao gồm) và nằm trong địa bàn cấp trên (`app.unitWithin`). Mã sai hoặc hết hiệu lực mà server trả 422 `validation_failed` thì lỗi hiện trên ô ĐVHC | `unitOptions`, `unitValidOn`, `vnToday` | Đổi `vnToday()` nếu cần cho chọn ngày hiệu lực trong quá khứ (tạo bù) |
| Kiểm mã ở client | `^[A-Za-z0-9._-]{1,40}$` (giống `domain.codeRe`), tên 1–200 ký tự. 409 `party.org_code_taken` hiện trên ô Mã | `codeError`, `nameError` | — |
| Idempotency-Key | Sinh **một lần mỗi lần mở hộp thoại**. Khóa được dùng lại khi gửi lại, kể cả sau khi sửa mã vì 409 hoặc sửa ĐVHC vì 422. An toàn vì backend chỉ lưu khóa khi đã ghi sổ (`org_event.idem_key`); lỗi nghiệp vụ không chiếm khóa. Lỗi mạng sau khi server đã ghi thì gửi lại sẽ nhận bản cũ, không tạo trùng | `OrgCreateDialog` (`useState(newIdempotencyKey)`) | Nếu sau này có kho idempotency HTTP chung lưu cả phản hồi lỗi và so dấu thân, phải sinh khóa mới mỗi khi thân đổi |
| Sau khi tạo | Đóng hộp thoại, tải lại cây, chọn nút mới, mở mọi cấp trên của nút để nút hiện ra. Khung chi tiết cho đổi tên ngay | `onCreated` trong `page.tsx` | — |
| Giả lập | `src/api/mock/orgs-create.ts`: 201 kèm `Location`; 409 `party.org_code_taken`; 422 `validation_failed` (theo trường), `party.invalid_hierarchy`; 403 `forbidden` / `party.out_of_scope` / `mfa_required`. Vai giả lập giống backend (`registry_admin`, `kn_province_admin`, `platform_admin`). Phát lại khóa do `mockFetch` lo | `orgs-create.ts`, `orgs-create.test.ts` | — |

### Rà soát `POST /v1/orgs/import` với HTX (D2)

**Phát hiện.** Dòng có `kind = cooperative` và Mã tổ chức để trống sẽ gọi thẳng `CreateOrg`
(`internal/platform/party/app/orgtree.go`, `importOrgRow`). Kết quả là một Nút HTX:

- không có hồ sơ (không MST);
- không có neo `pf_coop.htx_anchor`, không ghi `created_by_kind`/`created_by`;
- không kiểm trùng tên (`UniqueName = false`).

Đây là đường tạo HTX vòng qua luồng D2 (ADR-0052 §3), cũng như `POST /v1/orgs` với `kind = cooperative` khi không có bước
`POST /v1/htx-anchors` theo sau. Nút vẫn hiện "Chưa xác nhận" (§5: không có neo nghĩa là chưa xác nhận). Nó chỉ vào mô hình
neo khi chạy `POST /v1/htx-anchors/backfill`, và khi đó bị gắn nhãn `legacy_backfilled`, `created_by = kn`, tức là sai nguồn
gốc. Kiểm thử `TestOrgTreeExportImportRoundTrip` (`internal/platform/party/handler/tabular_integration_test.go`, dòng
`"HTX Xã C Mới"`) đang **khẳng định** hành vi này. Vì vậy gói này không sửa backend.

**Đề xuất** (gói sau, cần PO chốt và sửa kiểm thử trên):

1. **Cách A, khuyến nghị.** Nhập tệp không tạo HTX mới. Dòng `cooperative` có mã trống bị loại với lý do "HTX mới tạo qua màn
   Neo HTX theo mã số thuế (ADR-0052)". Nhập tệp vẫn di chuyển và đổi tên HTX đã có. Sửa trong `importOrgRow`, đổi dòng
   `"HTX Xã C Mới"` của kiểm thử thành kỳ vọng `rejected`, thêm một dòng vào `docs/errors/…` nếu cần mã lý do riêng.
2. **Cách B.** Nhập tệp tạo HTX rồi tạo luôn neo tạm (`created_by = kn`, mã tạm `TMP-…`). Cách này cần `pf/party` gọi cổng
   `pf/cooperative/anchor`. Chiều phụ thuộc hiện là cooperative → party, nên phải có một cổng ngược hoặc một sự kiện outbox,
   và archlint/modgen phải đổi. Cách B lớn hơn cách A.
3. Cũng nên cân nhắc áp quy tắc của cách A cho `POST /v1/orgs` với `kind = cooperative`. Giao diện của gói này đã không gửi
   HTX.
