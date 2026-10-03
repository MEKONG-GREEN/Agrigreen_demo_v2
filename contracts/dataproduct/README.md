# Hợp đồng sản phẩm dữ liệu (Public API → bên thứ ba)

Thiết kế: [ADR-0022](../../docs/adr/0022-public-api-ben-thu-ba.md) mục 9 và [g5-conventions.md](../../docs/dev/g5-conventions.md) §6. Thư mục này giữ dữ liệu mà Public API (`partner.<domain>`, `cmd/api-public`) được trả cho bên thứ ba. **Mặc định là từ chối**: trường không có trong hợp đồng thì không được trả. Hợp đồng nào thiếu người phê duyệt, tự phê duyệt, khai `personal_data` khác `false`, hoặc chưa `approved` ở production thì coi như không có.

Khác với [van công bố](../publication/README.md): van là cửa MKG → nền tảng, còn hợp đồng ở đây là cửa nền tảng → bên thứ ba. Hai loại dùng chung một tập con JSON Schema và một hàm `Validate` (`contracts/publication.Node`).

## Sản phẩm hiện có (G5-0)

Cả ba đều là **dự thảo**, chờ khách chốt bên thứ ba đầu tiên.

| Sản phẩm | Phiên bản | Route | Loại cấp quyền | Scope | Nguồn | Ghi chú |
|---|---|---|---|---|---|---|
| [`collection_volume`](collection_volume.json) | 1 | `GET /v1/data/collection-volumes` | `dp_collection_volume` | read, export | `pf/cropcycle`, `pf/geo`, `pf/masterdata` | Số cuộn và diện tích đã gom theo xã × vụ × tháng. Ngưỡng k = số nông hộ phân biệt (`publicdata.k_min`, tạm 10, C-3) |
| [`hub_capacity`](hub_capacity.json) | 1 | `GET /v1/data/hub-capacities` | `dp_hub_capacity` | read | `pf/geo` | Hub MKG đã công bố qua van, sức chứa danh định, bậc sử dụng. Không có tấn tồn |
| [`mrv_package_status`](mrv_package_status.json) | 1 | `GET /v1/data/mrv-packages` | `dp_mrv_package_status` | read (export **chưa bật**) | `pf/mrv` | Cho đơn vị thẩm định: trạng thái gói, băm, kết quả kiểm lại |

## Dạng tệp `<tên>.json`

```json
{
  "name": "<tên, trùng tên tệp>",
  "version": 1,
  "owner": "pf/<module phục vụ>",
  "sources": ["pf/<module chủ dữ liệu>"],
  "data_owner": "<tenant chủ dữ liệu — bên cấp>",
  "grant_kind": "dp_<tên>",
  "scopes": ["dp:<tên>:read", "dp:<tên>:export"],
  "route": "GET /v1/data/<...>",
  "status": "draft | approved | retired",
  "proposed_by": "<người đề xuất>", "approved_by": "<người phê duyệt — KHÁC>", "approved_at": "YYYY-MM-DD",
  "purpose": "vì sao bên thứ ba được nhận dữ liệu này",
  "personal_data": false,
  "aggregation": { "k_param": "publicdata.k_min", "k_default": 10, "unit": "household", "suppression": "omit" },
  "export": { "enabled": true, "formats": ["csv", "json"] },
  "type": "object", "required": ["..."], "properties": { "...": { "type": "string" } }
}
```

`properties` mô tả **một phần tử** của danh sách mà route trả về. Danh sách đặt trong phong bì `{items, next_cursor, meta}` của `public.yaml`.

## Cưỡng chế

- **`go run ./tools/contractcheck`** hỏng build khi:
  - siêu dữ liệu sai;
  - struct `//ag:dataproduct <tên>` có trường ngoài hợp đồng, sai kiểu, hoặc thiếu trường `required`;
  - struct được chú thích mà không có hợp đồng;
  - chú thích nằm ngoài `internal/platform/**`;
  - sản phẩm không có tag cùng tên trong `contracts/openapi/public.yaml`.
- **Lúc chạy (`pf/apiclient`):**
  - `dataproduct.Get(tên)`, rồi `Servable(residency.Production(os.Getenv))`;
  - `Validate` **từng phần tử** trước khi trả;
  - header `AG-Data-Contract: <tên>.v<N>`.

## Thay đổi hợp đồng

- Thêm trường tùy chọn: PR riêng, người đề xuất ≠ người phê duyệt, cập nhật `approved_at`.
- Xóa trường, đổi kiểu, thêm trường bắt buộc, đổi ngưỡng gom nhóm: tạo tệp sản phẩm mới `<tên>_v2.json` (`name: <tên>_v2`, `version: 2`, `grant_kind: dp_<tên>_v2`) và phục vụ song song ít nhất một kỳ phát hành (R-50). Bản cũ trả thêm `Deprecation` / `Sunset`. Vì `grant_kind` mới, chủ dữ liệu phải duyệt lại cấp quyền: bên thứ ba không tự động nhận trường mới.
- Chuyển `draft → approved` cần chủ dữ liệu (bên cấp) **và** đơn vị vận hành ký. Với `hub_capacity` cần thêm MKG, vì MKG là chủ gốc của dữ liệu hub.
