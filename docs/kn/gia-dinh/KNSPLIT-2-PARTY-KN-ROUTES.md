## KNSPLIT-2-PARTY-KN-ROUTES — Chuyển binding HTTP danh mục phòng ban từ party sang chủ KN (#118, phần đầu)

Nguồn: issue #118 (con #53, #47), ADR-0055, `tools/knboundary/boundary.yaml`. Nhánh từ dev 5e88b55c. Không làm trùng #76 (menu builder, đã gộp) và #67 (hồ sơ KN riêng, đã gộp); #160 đã gỡ route ghi HTX legacy nên `/v1/kn/cooperatives*` (chỉ còn GET) không thuộc gói này.

### Phân loại

| Nhóm | Thuộc | Lý do |
|---|---|---|
| Dữ liệu, sổ `pf_party.department_event`, luật đơn vị/trạng thái/mã, vai quản trị, nguồn đếm sử dụng | pf/party (giữ) | Schema `pf_party`; KN không được nhắc bảng schema khác (kn-sql-foreign). Không tạo schema hay framework trùng. |
| Binding route, đọc request, DTO `KNDepartment`/`KNDepartmentList`/`KNLogPage` của `/v1/departments*` | chủ KN `internal/kn/extension/departments` (chuyển) | `/v1/departments` thuộc bề mặt KN (`kn_shared_route_prefixes`). |
| `/v1/regions*`, `/v1/orgs/tree`, `/v1/orgs/{ref}/region`, `/region-log`, `/communes/ensure`, `/impact` | pf/party (giữ) | Primitive tổ chức của nền tảng, ngoài bề mặt KN; module KN phục vụ route ngoài bề mặt KN là ghép nối route-owner mới. Native platform_admin tạo xã giữ nguyên. |
| `/v1/menu`, `/v1/departments/{code}/modules`, `/v1/orgs/{ref}/departments/seed` | phần sau, issue con #222 | Bộ sinh `kn-menu.json` của cổng gộp chạy trong party/handler; `/v1/orgs/...` ngoài bề mặt KN, cần quyết định đường dẫn. |

### Mapping trước và sau

| Thao tác | Trước | Sau |
|---|---|---|
| Admin GET/POST /v1/departments, GET/PUT /v1/departments/{id}, POST /{id}/status, POST /{id}/retire, GET /{id}/log | `party/handler.KN.RegisterAdmin` | `extension/departments.Handler.RegisterAdmin` qua `extension.Module.RegisterAdmin` |
| Application GET /v1/departments | `party/handler.KN.RegisterApp` | `extension/departments.Handler.RegisterApp` qua `extension.Module.RegisterApp` |

Đường dẫn, tham số, thân JSON, mã HTTP (201/200 phát lại), header `Location`, mã lỗi `party.department_*` giữ nguyên; OpenAPI chỉ đổi tag `party` → `extension` (nguồn chuyển sang `contracts/openapi/modules/extension/departments/`), kiểu TypeScript sinh ra không đổi. Gốc lắp ráp: `kn.UseDepartmentAdmin(pty.DepartmentAdmin())` trong `cmd/api-platform/assembly/modules.go`; route đi dưới cùng chuỗi xác thực → phiên hiện hành của pf/accounts → Idempotency-Key như mọi route `/v1`. Chưa nối cổng → 503 `extension.dependency_unavailable` (đóng).

Cổng mới của shared, sửa tối thiểu: `party/port.DepartmentAdmin` cùng kiểu `DepartmentInput`, `DepartmentView`, `DepartmentStatusResult`, `ChangeLogEntry`, `FieldChange`, `DepartmentStatusLabel`, `ErrDepartmentMissing`. Kiểu cũ của `party/app` và `party/domain` thành bí danh của kiểu port, use case không đổi dòng nào; `*app.KN` hiện thực cổng.

Khác biệt có chủ đích, cần Codex/PO xác nhận: `/v1/departments*` nay qua cổng bật module `pf/extension` (per_tenant). Tenant tắt pf/extension nhận 403 `module_not_enabled` ở danh mục phòng ban; trước đây route ở pf/party (luôn bật). Tenant KN luôn bật pf/extension nên luồng hiện hành không đổi.

### Bằng chứng

- Route map: `party.TestDepartmentCatalogRoutesNotRegisteredByParty` (party không đăng ký `/v1/departments`, `/:id*`; menu, module phòng ban, khu vực, cây, seed còn nguyên) và `departments.TestRouteMap` (đúng 7 thao tác Admin + 1 Application).
- Binding/DTO: `departments.TestDTOAndStatusCodes`, `TestLogPage` so thân JSON từng byte; `TestUnwiredFailsClosed`.
- PG thật trên gốc lắp ráp đầy đủ: `tests/kndepartments` (tạo/gọi lặp cùng khóa, trùng mã, Cấp Tỉnh KN 403, Application không ghi, sửa/ngưng/xóa/Log, tenant khác 404, cổng module).
- Luật pf/party giữ kiểm ở `party/handler` qua cổng (`TestFN_AD_04_03_DepartmentLifecycleAuditLog`: 503 chưa nối nguồn đếm, 409 đang dùng, sổ chỉ ghi thêm, chuỗi băm).
- knboundary 305 → 299 (bỏ 6 ghép nối route-owner `/v1/departments*`), không thêm cạnh.
