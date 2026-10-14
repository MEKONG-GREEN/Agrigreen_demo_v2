# Nghiệm thu ORGUI — hộp thoại "Tạo tổ chức" (`/quan-tri/to-chuc`)

Lập ngày 14/10/2026 cho gói ORGUI ([giả định và quyết định](../kn/gia-dinh/ORGUI.md)). Ma trận có một dòng cho mỗi cặp
loại tổ chức × vai. Hợp đồng: Admin `POST /v1/orgs` (`partyCreateOrg`, `contracts/openapi/admin.yaml`).

**Quyết định PO (14/10/2026), không còn là giả định:** xã và các Nút hành chính / TTKN khác là đơn vị hành chính. Chỉ Admin
nền tảng (`platform_admin`, `registry_admin`) tạo, và chỉ trên trang quản trị nền tảng "Cơ cấu tổ chức". KN admin
(`kn_national_admin`, `kn_province_admin`) không có quyền tạo.

Cách chạy:
- Vitest: `npx vitest run src/admin/orgCreate.test.ts src/api/mock/orgs-create.test.ts` (trong `apps/web-ops`);
- WEB-E2E: `npx playwright test tests/e2e/org-create.spec.ts` (bản build giả lập).

Trạng thái **đạt** nghĩa là có kiểm thử tự động trên giao diện + giả lập. Backend `POST /v1/orgs` đã có kiểm thử Go riêng
(`internal/platform/party`); gói này không đổi backend.

## Ma trận loại × vai

| Mã | Loại | Vai | Kỳ vọng | Kiểm thử | Trạng thái |
|---|---|---|---|---|---|
| TC-01 | `ttkn_province` (TTKN tỉnh) | platform_admin (đã MFA) | Thấy nút "Tạo tổ chức". Mã, Tên, Tỉnh (ĐVHC cấp tỉnh còn hiệu lực), Cấp trên = TTKN QG hoặc để trống. Tạo được → 201, nút mới hiện trong cây và được chọn. | Vitest `orgCreate.test.ts` (cấp trên, ĐVHC tỉnh); mock `orgs-create.test.ts` | đạt |
| TC-02 | `ttkn_province` (TTKN tỉnh) | registry_admin (đã MFA) | Thấy nút "Tạo tổ chức". Mã, Tên, Tỉnh (ĐVHC cấp tỉnh còn hiệu lực), Cấp trên = TTKN QG hoặc để trống. Tạo được → 201, nút mới hiện trong cây và được chọn. | Vitest `orgCreate.test.ts` (cấp trên, ĐVHC tỉnh); mock `orgs-create.test.ts` | đạt (E2E dùng persona `setter` có cả platform_admin + registry_admin; registry_admin riêng: Vitest `canCreateOrg`) |
| TC-03 | `ttkn_province` (TTKN tỉnh) | kn_national_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | Vitest `canCreateOrg` (kn_national_admin → false); vai menu `nav.ts` (`nav.test.tsx`) | đạt |
| TC-04 | `ttkn_province` (TTKN tỉnh) | kn_province_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | WEB-E2E `org-create.spec.ts` (vai không có quyền, persona `tinh` = kn_province_admin); Vitest `canCreateOrg` | đạt |
| TC-05 | `extension_team` (Tổ KN / KNV xã) | platform_admin (đã MFA) | Thấy nút "Tạo tổ chức". Mã, Tên, Cấp trên bắt buộc = TTKN tỉnh đang hoạt động, Xã thuộc tỉnh của cấp trên và còn hiệu lực. Tạo được → 201, nút mới hiện trong cây và được chọn. | WEB-E2E `org-create.spec.ts` (tạo → cây → đổi tên; 409 trên ô Mã; thiếu cấp trên); Vitest; mock 201/409/422 | đạt |
| TC-06 | `extension_team` (Tổ KN / KNV xã) | registry_admin (đã MFA) | Thấy nút "Tạo tổ chức". Mã, Tên, Cấp trên bắt buộc = TTKN tỉnh đang hoạt động, Xã thuộc tỉnh của cấp trên và còn hiệu lực. Tạo được → 201, nút mới hiện trong cây và được chọn. | WEB-E2E `org-create.spec.ts` (tạo → cây → đổi tên; 409 trên ô Mã; thiếu cấp trên); Vitest; mock 201/409/422 | đạt (E2E dùng persona `setter` có cả platform_admin + registry_admin; registry_admin riêng: Vitest `canCreateOrg`) |
| TC-07 | `extension_team` (Tổ KN / KNV xã) | kn_national_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | Vitest `canCreateOrg` (kn_national_admin → false); vai menu `nav.ts` (`nav.test.tsx`) | đạt |
| TC-08 | `extension_team` (Tổ KN / KNV xã) | kn_province_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | WEB-E2E `org-create.spec.ts` (vai không có quyền, persona `tinh` = kn_province_admin); Vitest `canCreateOrg` | đạt |
| TC-09 | `ttkn_national` (TTKN Quốc gia) | platform_admin (đã MFA) | Thấy nút "Tạo tổ chức". Mã, Tên; không ĐVHC, không cấp trên. Tạo được → 201, nút mới hiện trong cây và được chọn. | Vitest `allowedParent`, `buildOrgInput` | đạt |
| TC-10 | `ttkn_national` (TTKN Quốc gia) | registry_admin (đã MFA) | Thấy nút "Tạo tổ chức". Mã, Tên; không ĐVHC, không cấp trên. Tạo được → 201, nút mới hiện trong cây và được chọn. | Vitest `allowedParent`, `buildOrgInput` | đạt (E2E dùng persona `setter` có cả platform_admin + registry_admin; registry_admin riêng: Vitest `canCreateOrg`) |
| TC-11 | `ttkn_national` (TTKN Quốc gia) | kn_national_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | Vitest `canCreateOrg` (kn_national_admin → false); vai menu `nav.ts` (`nav.test.tsx`) | đạt |
| TC-12 | `ttkn_national` (TTKN Quốc gia) | kn_province_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | WEB-E2E `org-create.spec.ts` (vai không có quyền, persona `tinh` = kn_province_admin); Vitest `canCreateOrg` | đạt |
| TC-13 | `dcrd` (Cục KTHT) | platform_admin (đã MFA) | Thấy nút "Tạo tổ chức". Mã, Tên; không ĐVHC, không cấp trên. Tạo được → 201, nút mới hiện trong cây và được chọn. | Vitest `validateForm`, `buildOrgInput`; mock 422 khi gửi ĐVHC | đạt |
| TC-14 | `dcrd` (Cục KTHT) | registry_admin (đã MFA) | Thấy nút "Tạo tổ chức". Mã, Tên; không ĐVHC, không cấp trên. Tạo được → 201, nút mới hiện trong cây và được chọn. | Vitest `validateForm`, `buildOrgInput`; mock 422 khi gửi ĐVHC | đạt (E2E dùng persona `setter` có cả platform_admin + registry_admin; registry_admin riêng: Vitest `canCreateOrg`) |
| TC-15 | `dcrd` (Cục KTHT) | kn_national_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | Vitest `canCreateOrg` (kn_national_admin → false); vai menu `nav.ts` (`nav.test.tsx`) | đạt |
| TC-16 | `dcrd` (Cục KTHT) | kn_province_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | WEB-E2E `org-create.spec.ts` (vai không có quyền, persona `tinh` = kn_province_admin); Vitest `canCreateOrg` | đạt |
| TC-17 | `mkg` (Mekong Green) | platform_admin (đã MFA) | Thấy nút "Tạo tổ chức". Mã, Tên; không ĐVHC, không cấp trên. Tạo được → 201, nút mới hiện trong cây và được chọn. | Vitest `allowedParent` (quy tắc gốc) | đạt |
| TC-18 | `mkg` (Mekong Green) | registry_admin (đã MFA) | Thấy nút "Tạo tổ chức". Mã, Tên; không ĐVHC, không cấp trên. Tạo được → 201, nút mới hiện trong cây và được chọn. | Vitest `allowedParent` (quy tắc gốc) | đạt (E2E dùng persona `setter` có cả platform_admin + registry_admin; registry_admin riêng: Vitest `canCreateOrg`) |
| TC-19 | `mkg` (Mekong Green) | kn_national_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | Vitest `canCreateOrg` (kn_national_admin → false); vai menu `nav.ts` (`nav.test.tsx`) | đạt |
| TC-20 | `mkg` (Mekong Green) | kn_province_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | WEB-E2E `org-create.spec.ts` (vai không có quyền, persona `tinh` = kn_province_admin); Vitest `canCreateOrg` | đạt |
| TC-21 | `partner` (Đối tác) | platform_admin (đã MFA) | Thấy nút "Tạo tổ chức". Mã, Tên, Mã ĐVHC tuỳ chọn (tỉnh hoặc xã); không cấp trên. Tạo được → 201, nút mới hiện trong cây và được chọn. | WEB-E2E `org-create.spec.ts` (422 ĐVHC hết hiệu lực hiện trên ô) | đạt |
| TC-22 | `partner` (Đối tác) | registry_admin (đã MFA) | Thấy nút "Tạo tổ chức". Mã, Tên, Mã ĐVHC tuỳ chọn (tỉnh hoặc xã); không cấp trên. Tạo được → 201, nút mới hiện trong cây và được chọn. | WEB-E2E `org-create.spec.ts` (422 ĐVHC hết hiệu lực hiện trên ô) | đạt (E2E dùng persona `setter` có cả platform_admin + registry_admin; registry_admin riêng: Vitest `canCreateOrg`) |
| TC-23 | `partner` (Đối tác) | kn_national_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | Vitest `canCreateOrg` (kn_national_admin → false); vai menu `nav.ts` (`nav.test.tsx`) | đạt |
| TC-24 | `partner` (Đối tác) | kn_province_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | WEB-E2E `org-create.spec.ts` (vai không có quyền, persona `tinh` = kn_province_admin); Vitest `canCreateOrg` | đạt |
| TC-25 | `cooperative` (HTX / THT) | platform_admin (đã MFA) | Thấy nút "Tạo tổ chức". Không có trường nhập; hiện liên kết "Mở Neo HTX theo mã số thuế" → `/quan-tri/neo-htx`; không có nút Tạo. | WEB-E2E `org-create.spec.ts` (HTX chuyển Neo HTX); Vitest `validateForm` HTX | đạt |
| TC-26 | `cooperative` (HTX / THT) | registry_admin (đã MFA) | Thấy nút "Tạo tổ chức". Không có trường nhập; hiện liên kết "Mở Neo HTX theo mã số thuế" → `/quan-tri/neo-htx`; không có nút Tạo. | WEB-E2E `org-create.spec.ts` (HTX chuyển Neo HTX); Vitest `validateForm` HTX | đạt (E2E dùng persona `setter` có cả platform_admin + registry_admin; registry_admin riêng: Vitest `canCreateOrg`) |
| TC-27 | `cooperative` (HTX / THT) | kn_national_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | Vitest `canCreateOrg` (kn_national_admin → false); vai menu `nav.ts` (`nav.test.tsx`) | đạt |
| TC-28 | `cooperative` (HTX / THT) | kn_province_admin | Không vào được trang ("Không có quyền truy cập"), không có nút "Tạo tổ chức". KN admin không có quyền tạo (quyết định PO 14/10/2026). | WEB-E2E `org-create.spec.ts` (vai không có quyền, persona `tinh` = kn_province_admin); Vitest `canCreateOrg` | đạt |

## Tiêu chí chung (mọi loại tạo được)

| Mã | Tiêu chí | Kiểm thử | Trạng thái |
|---|---|---|---|
| TC-G1 | Mã kiểm ở client theo `^[A-Za-z0-9._-]{1,40}$`; tên 1–200 ký tự | Vitest `codeError`; WEB-E2E (mã có dấu) | đạt |
| TC-G2 | 409 `party.org_code_taken` hiện trên ô Mã (`aria-invalid`), hộp thoại không đóng | WEB-E2E; Vitest `fieldErrorsFromProblem`; mock 409 | đạt |
| TC-G3 | Ô cấp trên chỉ liệt kê tổ chức đang hoạt động thuộc loại cha hợp lệ; 422 `party.invalid_hierarchy` hiện trên ô Cấp trên kèm `detail` | Vitest `parentOptions`, `fieldErrorsFromProblem`; WEB-E2E (không có TTKN QG khi tạo Tổ KN xã); mock 422 | đạt |
| TC-G4 | ĐVHC chỉ gồm đơn vị còn hiệu lực hôm nay (giờ VN), đúng cấp, trong địa bàn cấp trên; 422 `validation_failed` hiện trên ô ĐVHC | Vitest `unitOptions`, `unitValidOn`, `vnToday`; WEB-E2E (lọc xã theo tỉnh, 422 mã 99999) | đạt |
| TC-G5 | Idempotency-Key sinh một lần mỗi lần mở hộp thoại; sửa sau 409 rồi gửi lại cùng khóa → 201; phát lại cùng khóa không tạo bản thứ hai | Mock `orgs-create.test.ts` (phát lại, sửa sau 409); WEB-E2E (409 → sửa → tạo) | đạt |
| TC-G6 | Sau khi tạo: cây tải lại, nút mới được chọn, các cấp trên tự mở; đổi tên được ngay | WEB-E2E (tạo → cây → đổi tên) | đạt |
| TC-G7 | Admin chưa MFA → 403 `mfa_required` | Mock `orgs-create.test.ts` | đạt |

## Ngoài phạm vi gói

- Backend vẫn cho `kn_province_admin` gọi `POST /v1/orgs` trực tiếp. Muốn chặn ở API thì cần một gói backend có kiểm thử
  (xem giả định ORGUI).
- `POST /v1/orgs/import` vẫn tạo HTX mới mà không có neo D2 (xem đề xuất trong giả định ORGUI).
