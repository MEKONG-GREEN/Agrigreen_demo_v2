# Van công bố (MKG → nền tảng)

Mục 5 của tài liệu kiến trúc: dữ liệu từ vùng nội bộ MKG sang nền tảng chỉ đi qua các hợp đồng trong thư mục này. **Mặc định là từ chối.** Thiết kế đầy đủ: [`docs/design/mkg.md`](../../docs/design/mkg.md) mục 3.

## Hợp đồng hiện có

| Hợp đồng | Phiên bản | Sự kiện | Chủ | Người nhận |
|---|---|---|---|---|
| [`hub_facility`](hub_facility.json) | 1 | `mkg.hub.facility.published v1` | `mkg/hub` | `pf/geo` (mô hình đọc hub — đợt 2) |
| [`hub_capacity`](hub_capacity.json) | 1 | `mkg.hub.capacity.published v1` | `mkg/hub` | `pf/geo` (mô hình đọc hub — đợt 2) |

## Dạng tệp `<tên>.json`

```json
{
  "name": "<tên, trùng tên tệp>",
  "version": 1,
  "owner": "mkg/<module>",
  "proposed_by": "<người đề xuất>",
  "approved_by": "<người phê duyệt — KHÁC proposed_by (R-56)>",
  "approved_at": "YYYY-MM-DD",
  "event": "<vùng>.<module>.<thực thể>.<động từ> v<N>",
  "consumers": ["pf/<module>"],
  "purpose": "vì sao dữ liệu này được phép đi qua van",
  "type": "object",
  "required": ["..."],
  "properties": { "<trường>": { "type": "string", "format": "uuid" } }
}
```

- `owner`, `proposed_by`, `approved_by`, `approved_at` **bắt buộc**; `approved_by ≠ proposed_by` (so không phân biệt hoa thường). Hợp đồng thiếu các trường này bị coi như **không có** (mặc định từ chối).
- `properties` theo tập con JSON Schema: `type` (bắt buộc cho mọi trường; `["x", "null"]` cho trường có thể rỗng), `format` (`uuid`, `date`, `date-time`), `pattern`, `enum`, `required`, `properties` lồng nhau, `items` cho mảng. Object lồng có `properties` cũng **mặc định từ chối** khóa lạ.
- Dạng cũ `"fields": ["a", "b"]` (không kiểu) vẫn được nhận để tương thích, chỉ kiểm tên trường cấp một; hợp đồng mới phải dùng `properties`.
- `event` (tùy chọn): sự kiện mang payload qua hàng đợi; schema `contracts/events/<event>.v<N>.json` phải tồn tại và **không** được có trường ngoài hợp đồng van.

## Cưỡng chế

**Lúc build — `go run ./tools/contractcheck`** hỏng khi:
- hợp đồng thiếu `owner` / `proposed_by` / `approved_by` / `approved_at`, tự phê duyệt, ngày sai dạng, `name` khác tên tệp;
- struct `//ag:publication <tên>` có trường JSON ngoài hợp đồng, thiếu trường `required`, hoặc **sai kiểu** — kiểm đệ quy vào struct lồng, mảng, struct nhúng, kiểu có tên (`uuid.UUID`, `time.Time`, `decimal.Decimal`, `mass.Kg` → string; `json.Number` → number);
- struct được chú thích mà không có hợp đồng đã phê duyệt;
- **vùng mkg**: struct `//ag:event` trong `internal/mkg/**` không kèm `//ag:publication`; payload `outbox.Event` trong `internal/mkg/**` không chứng minh được là hợp đồng van (phải là literal / biến / tham số kiểu `//ag:publication`, hoặc tham số kiểu interface `Publication` của `internal/mkg/*/port`); kiểu cài đặt `PublicationName()` mà không có chú thích hoặc trả sai tên; `//ag:publication` dùng ngoài `internal/mkg`.

**Lúc chạy — package Go `contracts/publication`** (hợp đồng nhúng vào nhị phân): module mkg gọi `publication.Get(name)` rồi `Contract.Validate(json)` trước khi ghi sổ xuất `mkg_<module>.valve_export` + `shared.outbox`. Khóa lạ ở bất kỳ cấp nào, sai kiểu, thiếu trường bắt buộc → không xuất gì.

## Thay đổi hợp đồng

- Thêm trường tùy chọn: PR riêng, `proposed_by` và `approved_by` là hai người khác nhau (R-56), cập nhật `approved_at`, đồng thời thêm vào schema sự kiện (R-64: chỉ thêm trường tùy chọn).
- Xóa / đổi kiểu / thêm trường bắt buộc: hợp đồng mới `version + 1` và sự kiện `v<N+1>`, giữ bản cũ tới khi consumer chuyển xong.
