# Hợp đồng cổng tích hợp (`cmd/integration-gateway`)

Kiến trúc v4.1 §38–§42 (R-INT-01…11), [ADR-0033](../../docs/adr/0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), [ADR-0034](../../docs/adr/0034-luong-song-phuong.md), [ADR-0035](../../docs/adr/0035-cap-phep-tong-hop-theo-xa-nguong-k.md), [ADR-0032](../../docs/adr/0032-don-vi-hanh-chinh-co-hieu-luc-va-ke-thua.md). Mọi feed và hợp đồng nạp của cổng có lược đồ ở đây (R-INT-08). **Mặc định là từ chối**: trường không có trong hợp đồng thì không qua cổng; hợp đồng sai siêu dữ liệu coi như không có. MKG dùng đúng các hợp đồng này như mọi đối tác (R-INT-01).

Lược đồ dùng chung tập con JSON Schema của van công bố — `contracts/publication.Node` — và cùng hàm `Validate` (mặc định từ chối khóa lạ ở mọi cấp).

## Hợp đồng hiện có (W5.1)

Tất cả ở trạng thái **`proposed`**: ADR-0033/0034/0035 chờ MKG ký (A7). `proposed_by`/`approved_by` là **vai giữ chỗ**. `proposed` chỉ phục vụ ngoài production (`Servable`).

| Hợp đồng | Loại | Luồng §38.1 | Cấp phép | Khóa thực thể | `max_age` | Còn chờ | Thay cho |
|---|---|---|---|---|---|---|---|
| [`harvest_progress_by_commune`](feeds/harvest_progress_by_commune.v1.json) | feed | licensed | `aggregate` (k) | `area_code:season_code` | PT15M | A7, **C-4, K-8** | — |
| [`collection_volume_by_commune`](feeds/collection_volume_by_commune.v1.json) | feed | licensed | `aggregate` (k) | `area_code:season_code:period_month` | P1D | A7, C-4, K-8 | `dataproduct/collection_volume` (ADR-0035 §4) |
| [`season_harvest_dates`](feeds/season_harvest_dates.v1.json) | feed | licensed | `partner` / `season_harvest_dates` | `season_ref` | P1D | A7 | `GET /v1/partner/season-harvest-dates` |
| [`waterway_routes`](feeds/waterway_routes.v1.json) | feed | reference | `reference` | `network_version_ref` | PT1H | A7 | — (đồ thị ở ảnh chụp) |
| [`admin_units`](feeds/admin_units.v1.json) | feed | reference, có hiệu lực theo thời gian + kế thừa | `reference` | `code:valid_from` | P1D | A7, **C-9** | — |
| [`collection_schedule`](ingest/collection_schedule.v1.json) | nạp | **bilateral** (MKG ghi) | `bilateral` | `schedule_ref` | PT15M | A7, **C-12** | van `collection_schedule` (lược đồ dùng nguyên) |
| [`roll_count_statement`](ingest/roll_count_statement.v1.json) | nạp | **bilateral** (MKG ghi) | `bilateral` | `statement_ref` | PT15M | A7, **C-12** | van `htx_collection_statement` (X-04) |
| [`htx_roll_confirmation`](ingest/htx_roll_confirmation.v1.json) | nạp | **bilateral** (HTX ghi) | `bilateral` | `confirmation_ref` | PT1H | A7, **C-12** | `GET /v1/partner/htx-collection/responses` |

**Ngưỡng k (C-4/K-8 chưa ký).** Tập tổng hợp khai `aggregation.k_param` (tham số `pf/masterdata` có phiên bản, hiện `publicdata.k_min`) và `k_default` tạm = 10. Job dựng (W5.3) đọc tham số, **từ chối chạy** nếu nhỏ hơn k đã ký, ghi `dataset.k_used` vào mỗi phiên bản; `contractcheck` bắt mẫu có `k_used < k_default`. Đổi k **không** đổi hợp đồng (k là tham số, không phải trường), nhưng là thay đổi chính sách: đổi `k_default` cần người đề xuất ≠ người phê duyệt như mọi sửa hợp đồng. Quy tắc ẩn: ô dưới k bị bỏ (`op: retract` nếu đã phát), dòng `province_withheld` chỉ khi chính nó ≥ k, ẩn ô bù (`complementary_suppression`), trễ ẩn tới k + `hysteresis` (ADR-0035 §3).

**Song phương (ADR-0034, Đề xuất, chờ C-12).** Chỉ hai bên (`party_a` = MKG, `party_b` = HTX) thấy bản ghi; không bao giờ có `aggregation`, không vào `contracts/dataproduct`, `r_agg`, view `_pub`. Bên kia đọc qua feed của cặp `GET /v1/feeds/bilateral?pair=…` — trang dùng phong bì `feed_page` với `items[].contract` = `<tên>.v<N>`. Không bật production trước C-12; `household_payment` chỉ thêm khi C-12 chấp nhận.

## Bố cục

```
contracts/integration/
  envelope/feed_page.v1.json      phong bì trang feed (cursor, next_cursor, as_of, max_age, dataset, snapshot, items[])
  envelope/ingest_batch.v1.json   phong bì lô nạp (contract, batch_ref = Idempotency-Key, sent_at, as_of, items[])
  feeds/<tên>.v<N>.json           hợp đồng feed — lược đồ MỘT bản ghi (items[].record)
  ingest/<tên>.v<N>.json          hợp đồng nạp
  examples/<tên>.v<N>.json        một trang feed / một lô mẫu — CI kiểm khớp phong bì + hợp đồng
  integration.go                  Parse, MetaProblems, Resolve, ValidateRecord, CheckExample, Get, All
```

**Phong bì.** `items[].entity_key` = giá trị các trường `entity_key` của hợp đồng nối bằng `:` (trường phải bắt buộc, không null, string không date-time hoặc integer). `as_of` là mốc dữ liệu; `max_age` = `freshness.max_age` của hợp đồng — bên nhận (bộ điều hợp feed, §41.4) quá `max_age` thì gắn cờ dữ liệu cũ. Feed theo phiên bản có `dataset` (`version`, `admin_unit_as_of`, `k_used`, `contributors`, `checksum` R-INT-10). Ảnh chụp chỉ là `snapshot.href` tới `GET /v1/snapshots/{dataset}/{version}` — URL ký do cổng cấp sau đo lường, **không** nằm trong trang feed.

## Dạng hợp đồng

```json
{
  "name": "<tên, trùng tên tệp>", "version": 1,
  "kind": "feed | ingest", "flow": "licensed | published | bilateral | reference",
  "status": "draft | proposed | approved | retired", "pending": ["C-4"], "status_note": "…",
  "owner": "pf/<module>", "sources": ["pf/<module chủ dữ liệu>"], "consumers": ["…"],
  "proposed_by": "…", "approved_by": "<KHÁC proposed_by>", "approved_at": "YYYY-MM-DD",
  "purpose": "…", "personal_data": false,
  "grant_class": "aggregate | partner | reference | bilateral", "grant_kind": "<loại ADR-0020, với partner>",
  "entity_key": ["<trường>"],
  "freshness": { "max_age": "PT15M" },
  "delivery": { "cursor": true, "retention_days": 30, "dataset_versioned": true, "snapshot": "signed_url", "webhook": "thin", "channels": ["api", "file"] },
  "ingest": { "route": "POST /v1/batches/<tên>", "max_batch": 500, "business_key": ["…"], "target_port": "…", "channels": ["api", "file"] },
  "aggregation": { "k_param": "publicdata.k_min", "k_default": 10, "k_status": "…", "unit": "household", "suppression": "omit", "complementary_suppression": true, "hysteresis": 2, "grain": "…" },
  "bilateral": { "party_a": "…", "party_b": "…", "author_side": "a | b", "pair_key": ["htx_ref"], "pair_feed": "bilateral" },
  "effective_dating": { "valid_from": "valid_from", "valid_to": "valid_to", "interval": "[)", "succession": "successors" },
  "publication": "<tên hợp đồng van — dùng nguyên lược đồ, khi đó không có properties>",
  "replaces": ["publication/<tên>", "dataproduct/<tên>"],
  "type": "object", "required": ["…"], "properties": { "…": { "type": "string" } }
}
```

Khóa lạ ở cấp hợp đồng → lỗi (`DisallowUnknownFields`).

## Cưỡng chế

**`go run ./tools/contractcheck [-base origin/main]`** hỏng khi:
- siêu dữ liệu sai (`integration.MetaProblems`): thiếu/tự phê duyệt, `approved` còn `pending`, owner ngoài `pf/`, `personal_data = true` ngoài song phương, feed không cursor hoặc giữ < 30 ngày, webhook không mỏng, `grant_class` không khớp `flow`, `aggregate` ⇔ `aggregation`, k_default < 2, tắt ẩn ô bù, `bilateral` ⇔ khối `bilateral`, song phương có tổng hợp, route nạp khác `POST /v1/batches/<tên>`;
- tên/phiên bản khác tên tệp, `kind` khác thư mục, một tên dùng cho cả feed lẫn nạp;
- `publication` không trỏ hợp đồng van hợp lệ, hoặc kèm `properties`; `replaces` trỏ tệp không có;
- `entity_key` / `business_key` / `pair_key` / `effective_dating` trỏ trường không có hoặc sai dạng;
- thiếu mẫu, mẫu không khớp phong bì hoặc hợp đồng (khóa lạ, sai kiểu, `entity_key` lệch, `max_age` lệch, `k_used` dưới k), mẫu mồ côi;
- với `-base`: xóa trường, đổi kiểu, thêm `required` trong cùng tệp `v<N>`.

**Lúc chạy (W5.2–W5.4):** cổng gọi `integration.Get(tên, N)`, `Servable(production)`, rồi `ValidateRecord` từng bản ghi (feed trước khi ghi kho feed; nạp ở inbox) và `EnvelopeFor(kind).Validate` cho lô nhận. Header `AG-Data-Contract: <tên>.v<N>`.

## Thay đổi hợp đồng (R-INT-08)

- Thêm trường tùy chọn: sửa tệp `v<N>`, người đề xuất ≠ người phê duyệt, cập nhật `approved_at`, cập nhật mẫu.
- Xóa trường, đổi kiểu, thêm trường bắt buộc, đổi `entity_key`: tệp mới `<tên>.v<N+1>.json` + mẫu, phục vụ **song song ít nhất một kỳ phát hành**; bản cũ trả `Deprecation`/`Sunset` rồi `status: retired`.
- Đổi ngưỡng k: đổi tham số `k_param` (có phiên bản, bốn mắt); không cần phiên bản hợp đồng mới.
- Hợp đồng nạp khai `publication`: thay đổi theo quy tắc của [van công bố](../publication/README.md#thay-đổi-hợp-đồng).
