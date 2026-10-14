## ORGUI — Hộp thoại "Tạo tổ chức" trên web-ops (không migration)

Bối cảnh: backend `POST /v1/orgs` (`partyCreateOrg`, Admin API, MFA) đã có và đã kiểm thử, nhưng web-ops chưa gọi.
Trang `/quan-tri/to-chuc` ("Cơ cấu tổ chức") chỉ có cây, đổi tên, xuất/nhập tệp. Gói này thêm hộp thoại tạo. Không đổi
OpenAPI, không đổi hành vi backend. Mặc định dưới đây do nhóm rà soát chốt. Dòng "Ai tạo được" là **quyết định PO
(14/10/2026)**, không còn là giả định; các dòng khác vẫn là mặc định chờ PO.

| Câu hỏi | Phương án đã chọn | Ở đâu | Đổi thế nào nếu PO chọn khác |
|---|---|---|---|
| Đặt hộp thoại ở đâu | Trang quản trị nền tảng `/quan-tri/to-chuc`, không đặt trong màn KN, vì KN đang tách riêng | `apps/web-ops/src/app/quan-tri/to-chuc/page.tsx`, `src/admin/OrgCreateDialog.tsx` | Gắn `OrgCreateDialog` vào màn KN; logic nằm ở `src/admin/orgCreate.ts`, dùng lại được |
| Ai tạo được — **QUYẾT ĐỊNH PO 14/10/2026** | Xã (và các Nút hành chính / TTKN khác) là đơn vị hành chính. **Chỉ Admin nền tảng tạo**, trên trang quản trị nền tảng "Cơ cấu tổ chức" `/quan-tri/to-chuc`. Vai được tạo: `platform_admin`, `registry_admin`. **KN admin (`kn_national_admin`, `kn_province_admin`, …) không có quyền tạo.** Từ 14/10/2026 backend cũng chặn: `CreateOrg` gọi `requireNodeCreator`, Nút không phải HTX cần `orgNodeRoles` (`registry_admin`, `platform_admin`), vai khác → 403 `forbidden` kèm `detail` | `ORG_CREATE_ROLES` (`orgCreate.ts`); `app.orgNodeRoles`, `requireNodeCreator` (`internal/platform/party/app/registry.go`); `TestOrgNodeCreateOnlyPlatformAdmin` | Đã chốt và đã áp ở API |
| Loại nào tạo được | Mọi loại trong `PartyOrgInput` trừ HTX: `ttkn_province`, `extension_team`, `ttkn_national`, `dcrd`, `mkg`, `partner`. Trường hiển thị theo loại (`KIND_RULES`): TTKN tỉnh chọn tỉnh; Tổ KN xã chọn xã thuộc tỉnh của cấp trên; TTKN QG/Cục KTHT/MKG không gắn ĐVHC, không cấp trên; Đối tác nhập tay mã ĐVHC (tuỳ chọn) | `KIND_RULES`, `CREATE_KIND_ORDER` | Bớt loại khỏi `CREATE_KIND_ORDER`, ví dụ khi `mkg` chỉ có một bản ghi mỗi tenant |
| HTX (`cooperative`) | Không tạo trong hộp thoại. Chọn loại HTX thì hiện liên kết sang `/quan-tri/neo-htx` (luồng HTX tạm theo MST, ADR-0052 / D2) | `KIND_RULES.cooperative.redirect` | Bỏ `redirect` và thêm trường hồ sơ HTX (MST, địa chỉ, diện tích); backend đã nhận `cooperative` |
| Quy tắc cấp trên | Theo `domain.AllowedParent`: TTKN tỉnh dưới TTKN QG hoặc ở gốc; Tổ KN xã bắt buộc dưới TTKN tỉnh; các loại còn lại chỉ ở gốc. Ô chọn chỉ liệt kê tổ chức **đang hoạt động** đúng loại. Server trả `party.invalid_hierarchy` (422) thì lỗi hiện trên ô Cấp trên, kèm `detail` | `parentOptions`, `allowedParent`, `fieldErrorsFromProblem` | Sửa `KIND_RULES[*].parents/root` cùng lúc với `domain.AllowedParent` |
| ĐVHC và ngày hiệu lực | Danh sách tra `GET /v1/masterdata/admin-units?at=<hôm nay giờ VN>`, sau đó lọc lại ở client: `valid_from ≤ hôm nay < valid_to` (`valid_to` không bao gồm) và nằm trong địa bàn cấp trên (`app.unitWithin`). Mã sai hoặc hết hiệu lực mà server trả 422 `validation_failed` thì lỗi hiện trên ô ĐVHC | `unitOptions`, `unitValidOn`, `vnToday` | Đổi `vnToday()` nếu cần cho chọn ngày hiệu lực trong quá khứ (tạo bù) |
| Kiểm mã ở client | `^[A-Za-z0-9._-]{1,40}$` (giống `domain.codeRe`), tên 1–200 ký tự. 409 `party.org_code_taken` hiện trên ô Mã | `codeError`, `nameError` | — |
| Idempotency-Key | Sinh **một lần mỗi lần mở hộp thoại**. Khóa được dùng lại khi gửi lại, kể cả sau khi sửa mã vì 409 hoặc sửa ĐVHC vì 422. An toàn vì backend chỉ lưu khóa khi đã ghi sổ (`org_event.idem_key`); lỗi nghiệp vụ không chiếm khóa. Lỗi mạng sau khi server đã ghi thì gửi lại sẽ nhận bản cũ, không tạo trùng | `OrgCreateDialog` (`useState(newIdempotencyKey)`) | Nếu sau này có kho idempotency HTTP chung lưu cả phản hồi lỗi và so dấu thân, phải sinh khóa mới mỗi khi thân đổi |
| Sau khi tạo | Đóng hộp thoại, tải lại cây, chọn nút mới, mở mọi cấp trên của nút để nút hiện ra. Khung chi tiết cho đổi tên ngay | `onCreated` trong `page.tsx` | — |
| Giả lập | `src/api/mock/orgs-create.ts`: 201 kèm `Location`; 409 `party.org_code_taken`; 422 `validation_failed` (theo trường), `party.invalid_hierarchy`; 403 `forbidden` / `party.out_of_scope` / `mfa_required`. Vai giả lập giống backend: Nút không phải HTX chỉ `registry_admin`/`platform_admin`; `kn_province_admin` chỉ tạo HTX. Phát lại khóa do `mockFetch` lo | `orgs-create.ts`, `orgs-create.test.ts` | — |

### Phạm vi của quyết định PO với các thao tác khác (backend, 14/10/2026)

| Thao tác | Trước | Sau | Lý do |
|---|---|---|---|
| `POST /v1/orgs` (Nút không phải HTX) | `registry_admin`, `kn_province_admin`, `platform_admin` | Chỉ `registry_admin`, `platform_admin`; vai KN → 403 `forbidden` | Đúng quyết định PO: tạo Nút hành chính / TTKN là việc của Admin nền tảng |
| `POST /v1/orgs` (`cooperative`) | như trên | Không đổi: `kn_province_admin` vẫn tạo HTX trong phạm vi | HTX không phải Nút hành chính; quyết định PO không nói tới HTX |
| `POST /v1/orgs/{ref}/rename` | `adminRoles` + phạm vi | **Không đổi** | Đổi tên không tạo Nút; KN tỉnh cần sửa tên tổ/xã sau sáp nhập |
| `POST /v1/orgs/{ref}/move` | `adminRoles` + phạm vi | **Không đổi** | Luồng "di chuyển Nút xã" của KN admin (sáp nhập, US-ADM-07/08) giữ nguyên; di chuyển không tạo Nút. Quyết định PO chỉ nói về tạo |
| `POST /v1/orgs/import` | Dòng mới gọi `CreateOrg` với vai người nhập | Dòng tạo Nút mới bởi vai KN bị loại **theo dòng** (lý do "chỉ Admin nền tảng…"); dòng có sẵn vẫn di chuyển/đổi tên | Kế thừa quy tắc của `CreateOrg`; tệp vẫn nhập được cho phần không phải tạo |
| `POST /v1/orgs/{ref}/communes/ensure` (KN) | `knAdminRoles` = `platform_admin`, `registry_admin` | Không đổi | Đã chỉ Admin nền tảng |

Kiểm thử DB: `TestOrgNodeCreateOnlyPlatformAdmin` (403 cho KN tỉnh với xã/TTKN tỉnh/đối tác, 403 cho KN QG; 201 cho
`registry_admin` riêng và `platform_admin` riêng; KN tỉnh vẫn đổi tên, di chuyển Nút trong tỉnh và tạo HTX),
`TestOrgImportCreateRulesORGUI` (nhập bởi KN tỉnh: dòng xã mới và dòng HTX mới bị loại theo dòng; Admin nền tảng: xã tạo được,
HTX mới vẫn bị loại).

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

**Đã làm (14/10/2026), cách A.** Nhập cây không tạo HTX mới: dòng `cooperative` có mã trống hoặc mã chưa có bị loại theo dòng
với lý do "… tạo HTX qua màn Neo HTX theo mã số thuế (/quan-tri/neo-htx, D2) …" (`app.CoopImportRejectReason`,
`importOrgRow`). Dòng HTX đã có vẫn di chuyển/đổi tên. `TestOrgTreeExportImportRoundTrip` đổi kỳ vọng dòng "HTX Xã C Mới" thành
`rejected` (tổng: 1 tạo, 5 loại; xuất lại 8 dòng). Lý do loại là văn bản theo dòng, không phải mã lỗi mới, nên không thêm
`docs/errors/…`. OpenAPI chỉ sửa mô tả (`gen:api` đã chạy).

**Chưa làm: chặn `POST /v1/orgs` với `kind = cooperative`** (422 `party.cooperative_requires_anchor`). Quyết định có điều kiện của
điều phối: "luồng neo D2 vẫn phải tạo được Nút HTX nó cần". Kết quả kiểm tra:

- `POST /v1/htx-anchors` (`internal/platform/cooperative/anchor/app/service.go`) **không** tạo Nút. Nó nhận `org_ref` của một Nút HTX
  đã có hồ sơ; ADR-0052 §3 ghi "tạo bằng luồng tạo HTX hiện có". Với Admin nền tảng, luồng tạo một HTX đơn lẻ có hồ sơ (MST) chính là
  `POST /v1/orgs` `kind = cooperative`. Chặn nó thì màn Neo HTX mất bước tạo Nút cho MKG; chỉ còn nhập hàng loạt
  `/v1/cooperatives/import`, sao chép hồ sơ, hoặc luồng KN `/v1/kn/cooperatives`.
- 15 tệp kiểm thử ngoài gói (mkg/hub, cooperative/member, party, tests/acceptance, tests/e2e G1, cmd/api-internal) tạo HTX qua
  `POST /v1/orgs`; `seedTree` của party dùng nó cho mọi kiểm thử handler.

Đề xuất gói sau (cần sửa OpenAPI, cổng giữa module, web Neo HTX): `POST /v1/htx-anchors` nhận thêm `new_cooperative`
(tên, xã, cấp trên, MST, …), tạo Nút qua đường nội bộ `Registry.createOrg` và neo trong cùng thao tác. Khi đó mới chặn
`POST /v1/orgs` `kind = cooperative` bằng 422 `party.cooperative_requires_anchor` (ghi vào `docs/errors/…`), và chuyển các kiểm thử
trên sang đường mới hoặc sang đường nội bộ.
