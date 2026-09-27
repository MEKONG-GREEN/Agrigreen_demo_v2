# Web vận hành ↔ backend: phần còn thiếu

Web vận hành (`apps/web-ops`) gọi **đúng** các route có trong `contracts/openapi/application.yaml` và `contracts/openapi/admin.yaml` (kiểu sinh ở `src/api/generated/`). Tài liệu này ghi những gì web cần mà backend **chưa** cung cấp. Đây là đề xuất để các module cân nhắc, không phải thay đổi đã chốt. Web không tự bịa route. Khi thiếu, web tự ghép từ route có sẵn hoặc hiện "—". Hiện **không còn** route viết tay: thư mục `src/api/pending/` đã xoá, kể cả `GET /v1/me` và các route CGH, vì backend đã có.

Khi backend thêm route vào OpenAPI:

1. Chạy `npm run gen:api` trong `apps/web-ops`.
2. Chuyển nơi gọi sang kiểu sinh, bỏ phần ghép tạm (adapter trong `src/api/adapters/`, `src/admin/`).
3. Xoá mục đó khỏi tài liệu này.

Quy ước chung của mọi đề xuất (docs/dev/g1-conventions.md §3–4):
- số thập phân là chuỗi;
- phân trang cursor `{items, next_cursor}`;
- lỗi theo RFC 9457 có `trace_id`;
- phạm vi TW → Tỉnh → Xã (ADR-0009).

## Tóm tắt

| Mã | Route đề xuất | Bề mặt | Trang cần | Web đang làm tạm |
|---|---|---|---|---|
| G-02 | Mở rộng `GET /v1/extension/dashboard` (hoặc `GET /v1/dashboard/summary`) | Application | `/` | chỉ hiện 4 KPI của pf/extension |
| G-03 | Thêm trường và bộ lọc cho `GET /v1/cooperatives` | Application | `/htx`, `/htx/[id]`, `/ban-do` | tra tên xã/tỉnh từng mã qua masterdata |
| G-04 | Trạng thái đồng ý trong `GET /v1/cooperatives/{ref}/households` | Application | `/htx/[id]` tab Nông hộ | không hiện cột đồng ý |
| G-05 | `GET /v1/geo/admin-boundaries?level=&parent=` | Application | `/ban-do`, `/cgh` | gọi `GET /v1/geo/admin-boundaries/{code}` cho từng đơn vị |
| G-06 | `GET /v1/masterdata/catalogs` | Admin | `/quan-tri/danh-muc` | danh sách danh mục cứng theo enum, không có số mục |
| G-07 | `GET /v1/masterdata/params` (sổ đăng ký tham số) | Admin | `/quan-tri/tham-so` | tên tham số viết cứng ở `src/admin/params.ts`; người đặt/duyệt hiện mã |
| G-08 | `GET /v1/orgs` trên Admin API | Admin | `/quan-tri/to-chuc` | đọc cây qua Application API |
| G-09 | Tên hiển thị trong `GET /v1/extension/tasks` | Application | `/nhiem-vu` | chỉ hiện "Đã/Chưa phân công", link HTX không có tên |
| G-10 | Mã HTX và nguồn diện tích trong `MechCoverageItem` | Application | `/cgh` bảng "HTX cần ưu tiên" | chỉ hiện tên HTX |

Đã chuyển sang route thật:
- `GET /v1/me`: phạm vi, menu, tên đơn vị. `Me` không có tên người (R-04), nên tên hiển thị lấy từ claim `name` của token.
- Bản đồ CGH `/cgh`: `GET /v1/mechanization/balance-runs`, `/coverage`, `/dashboard`.

---

## G-02 — Bảng điều hành gộp nhiều module

**Cần cho:** trang `/` (US-DASH-01 và prototype KN: 4 thẻ "diện tích triển khai – HTX – thửa đã số hoá – nhiệm vụ quá hạn", biểu đồ tiến độ so với mục tiêu).

`GET /v1/extension/dashboard` chỉ có số liệu của pf/extension. Còn thiếu:
- **số thửa đã số hoá** theo đơn vị, từ pf/geo;
- **mục tiêu diện tích** theo đơn vị (tham số hoặc kế hoạch).

Hai phương án:

Phương án (a) là thêm trường vào `ExtKPI`:

```json
{ "digitized_plots": 18420, "target_area_ha": "62000.0000" }
```

Phương án (b) là route gộp mới `GET /v1/dashboard/summary?unit=82&period=2026-09`:

```json
{
  "unit": "82", "level": "province", "period": "2026-09", "as_of": "2026-09-27T09:20:00+07:00",
  "total": { "sown_area_ha": "3321.4500", "target_area_ha": "62000.0000", "cooperatives": 7, "digitized_plots": 1840, "overdue_tasks": 5, "provisional": true },
  "children": [ { "unit": "82101", "sown_area_ha": "812.1200", "target_area_ha": "4000.0000", "digitized_plots": 460, "provisional": false } ]
}
```

## G-03 — Danh sách HTX: tên địa bàn, số hộ, bộ lọc đơn vị

**Cần cho:** `/htx` (cột Địa bàn, Hộ thành viên, Thửa số hoá; bộ lọc tỉnh cho cán bộ TW), `/htx/[id]`, `/ban-do` (lọc HTX theo tỉnh đã chọn).

`Cooperative` chỉ có `admin_unit_code`. Web đang gọi `GET /v1/masterdata/admin-units/{code}` cho từng mã (có nhớ tạm) để hiện "Xã …, Tỉnh …". Đề xuất:

- **Method / path:** `GET /v1/cooperatives?q=&admin_unit=82&include_inactive=&cursor=&limit=`, với `admin_unit` là mã tỉnh hoặc xã.
- **Response:** thêm các trường sau vào mỗi phần tử `items[]`, cạnh các trường `Cooperative` hiện có:

```json
{
  "ref": "01927b3e-c00b-7000-8000-000000000001",
  "code": "HTX8200001",
  "admin_unit_code": "82101",
  "province_code": "82",
  "commune_name": "Xã Mỹ An",
  "province_name": "Đồng Tháp",
  "member_households": 128,
  "digitized_plots": 96
}
```

## G-04 — Trạng thái đồng ý của nông hộ trong danh sách

**Cần cho:** `/htx/[id]`, tab Nông hộ, cột "Đồng ý chia sẻ dữ liệu" (R-41/R-42).

`HouseholdListItem` không có trạng thái đồng ý. Nếu dùng `GET /v1/consents?subject_ref=` thì phải gọi riêng cho từng hộ. Đề xuất thêm trường vào mỗi phần tử của `GET /v1/cooperatives/{ref}/households`:

```json
{ "household_ref": "…", "label": "Nguyễn V. A.", "consents": [ { "purpose": "mrv.evidence", "status": "granted", "event_time": "2026-05-02T08:00:00+07:00" } ] }
```

## G-05 — Ranh giới hành chính theo cấp hoặc đơn vị cha

**Cần cho:** `/ban-do` (ranh giới tỉnh với TW, ranh giới xã với tỉnh/xã) và `/cgh` (tô màu mức đáp ứng theo tỉnh).

Backend chỉ có `GET /v1/geo/admin-boundaries/{code}`. Web phải liệt kê đơn vị qua masterdata, rồi gọi từng mã (song song tối đa 6 lời gọi). Đơn vị trả 404 `geo.admin_boundary_not_found` bị bỏ qua. Với tỉnh có khoảng 100 xã, cách này tốn khoảng 100 lời gọi.

- **Method / path:** `GET /v1/geo/admin-boundaries?level=commune&parent=82&zoom=10`
- **Response 200 (`application/geo+json`):**

```json
{
  "type": "FeatureCollection",
  "features": [
    { "type": "Feature", "id": "82101", "geometry": { "type": "MultiPolygon", "coordinates": [] },
      "properties": { "code": "82101", "level": "commune", "name": "Xã Mỹ An", "parent_code": "82", "source": "CloudMap 2026-09", "area_ha": "4120.2500", "imported_at": "2026-09-01T00:00:00+07:00" } }
  ],
  "missing": ["82104"]
}
```

## G-06 — Danh sách danh mục (Admin)

**Cần cho:** cột trái của `/quan-tri/danh-muc`, gồm tên tiếng Việt, số mục và số mục ngừng dùng.

Tên danh mục hiện là enum cố định (`components.parameters.Catalog`), nên web viết cứng tên tiếng Việt.

- **Method / path:** `GET /v1/masterdata/catalogs` (Admin API)
- **Response 200:**

```json
{ "items": [ { "catalog": "rice_variety", "name": "Giống lúa", "item_count": 6, "inactive_count": 1, "attrs_schema": ["growth_days", "expected_yield_t_per_ha"] } ] }
```

## G-07 — Sổ đăng ký tham số và tên người đặt/duyệt

**Cần cho:** `/quan-tri/tham-so`.

`GET /v1/masterdata/params/versions` trả danh sách phiên bản, nhưng không có tên tham số, đơn vị mặc định, kiểu (số/chữ), khoảng hợp lệ. Nó cũng không liệt kê được khóa chưa có phiên bản nào. Người đặt và người duyệt chỉ có `setter_ref`/`approver_ref` (UUID), nên web hiện mã. Hiện tại, web gom phiên bản theo khóa và lấy tên từ bảng viết cứng `src/admin/params.ts`.

- **Method / path:** `GET /v1/masterdata/params?prefix=cgh.` (Admin API)
- **Response 200:**

```json
{
  "items": [
    { "key": "cgh.coverage.sufficient_min_pct", "name": "Ngưỡng Đủ máy", "unit": "%", "kind": "decimal", "min": "0", "max": "500",
      "current": { "version": 1, "value": "85", "effective_from": "2026-01-01T00:00:00+07:00" }, "submitted_count": 0 }
  ],
  "next_cursor": null
}
```

Kèm theo: thêm `setter_name` và `approver_name` (tên cán bộ, không phải nông hộ) vào `ParamVersion`.

## G-08 — Liệt kê tổ chức trên Admin API

**Cần cho:** `/quan-tri/to-chuc`.

Admin API có tạo, di chuyển, đổi tên và nhật ký tổ chức, nhưng không có route liệt kê. Web đang đọc cây qua Application `GET /v1/orgs?include_inactive=true`. Route này lọc theo phạm vi của người gọi, nên quản trị có thể không thấy đủ cây nếu phạm vi không phải "All". Nhật ký và đổi tên thì web đã gọi qua Admin API.

- **Method / path:** `GET /v1/orgs?kind=&parent_ref=&include_inactive=true&cursor=&limit=` (Admin API)
- **Response 200:** `{ "items": [PartyOrg], "next_cursor": null }`. Mỗi `PartyOrg` có dạng:

```json
{ "ref": "…", "kind": "extension_team", "code": "TKN-82101", "name": "Tổ khuyến nông cộng đồng Xã Mỹ An", "admin_unit_code": "82101", "parent_ref": "…", "representative_ref": null, "active": true }
```

## G-09 — Tên hiển thị trong danh sách nhiệm vụ

**Cần cho:** `/nhiem-vu`, gồm cột "Phân công" (tên cán bộ) và "HTX / địa bàn" (tên HTX).

`ExtTask` có `assignee_ref`, `cooperative_ref` và `commune_code`, nhưng không có tên. Web đang tra tên xã qua masterdata; cán bộ và HTX thì chưa hiện tên. Đề xuất thêm vào mỗi phần tử của `GET /v1/extension/tasks`:

```json
{ "task_id": "…", "assignee_ref": "…", "assignee_name": "Trần Thị C", "cooperative_ref": "…", "cooperative_name": "HTX Nông nghiệp Mỹ An", "commune_name": "Xã Mỹ An" }
```

## G-10 — Mã HTX trong kết quả cân đối CGH

**Cần cho:** `/cgh`, bảng "HTX cần ưu tiên" (BRD CGH FN-11 hiện Mã HTX).

`MechCoverageItem` ở `level=cooperative` và `priority` của `GET /v1/mechanization/dashboard` có `cooperative_ref` và `name`, nhưng không có Mã HTX. Đề xuất thêm trường vào `MechCoverageItem`:

```json
{ "cooperative_ref": "…", "cooperative_code": "HTX8200001", "name": "HTX Nông nghiệp Mỹ An", "province_code": "82", "commune_code": "82101" }
```
