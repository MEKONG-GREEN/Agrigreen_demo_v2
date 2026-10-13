# Hợp đồng đồng bộ ngoại tuyến (mục 15, ADR-0006)

G1 đồng bộ qua **Application API** (pull/push có cursor và idempotency). Cách này không phụ thuộc engine logical replication, nên không bị chặn bởi câu hỏi 23.1. Máy chủ hiện thực trong module `internal/platform/sync`; app Flutter ở `apps/field-app`.

## Đăng ký thiết bị

`POST /v1/devices` (Idempotency-Key) → `201 {device_id, registered_at, platform, app_version, revoked}`

Thân: `{ "device_id": "<UUIDv7 sinh trên máy>", "platform": "android" | "ios", "app_version": "…" }`

- `actor_ref` và `org_ref` của thiết bị lấy từ **token**, không từ thân (R-16).
- Cùng người đăng ký lại cùng mã (cài lại, nâng cấp app) → `200`, cập nhật `app_version`. Mã của người khác → `409 sync.device_taken`.
- **Mọi** lời gọi đồng bộ (`/v1/sync/pull`, `/v1/sync/push`, `POST /v1/files`) mang header `X-Device-Id`. Thiếu header, thiết bị chưa đăng ký trong tenant, hoặc đăng ký bởi tài khoản khác → `403 sync.device_unregistered`. Máy chủ ghi `last_seen_at` (tối đa mỗi phút một lần).
- `POST /v1/files` (W6.5): lưu trữ và `GET /v1/files/{ref}[/content]` do `pf/evidence` (`cap/evidence`) thực hiện sau khi pf/sync kiểm thiết bị. Multipart nhận thêm bộ ba tùy chọn `subject_module`, `subject_type`, `subject_id` (R-DB-07: đủ cả ba hoặc không; module chủ phải phân giải được trong tenant → không thì `422 evidence.subject_unresolved`). Tệp tải trước khi biết thực thể (bản ghi ngoại tuyến tham chiếu `file_ref` sau) bỏ trống bộ ba. Mở tệp có chủ thể: người xem phải thấy chủ thể (`403 evidence.subject_forbidden`); mọi quyết định ghi sổ `pf_files.access_log`.
- Thiết bị bị **thu hồi** (Admin API `POST /v1/devices/{id}/revoke {reason}`, vai `device_admin`, phát `pf.sync.device.revoked`): mọi lời gọi sync trả `403` với `code = sync.device_revoked`. App **xóa dữ liệu cục bộ** và khóa token ngay (mục 15). Mã đã thu hồi không đăng ký lại được — app sinh mã mới.
- Đăng nhập ngoại tuyến tối đa **72 giờ** kể từ lần xác thực trực tuyến cuối. Mở khóa bằng PIN 6 số (App KN US-ADM).

## Chiều xuống: pull

`GET /v1/sync/pull?cursor=<opaque>&limit=500` → `200`

```json
{
  "changes": [
    { "entity": "plot", "id": "0191…", "version": 3, "op": "upsert", "data": { } },
    { "entity": "task", "id": "0191…", "version": 1, "op": "tombstone" }
  ],
  "next_cursor": "<opaque>",
  "has_more": false,
  "server_time": "2026-12-03T02:10:00Z",
  "reset": false
}
```

- `limit` mặc định 500, tối đa 1000. `data` không có khi `op = tombstone`.
- `entities` (G2, KN US-SYNC-02, tùy chọn): chỉ đọc các entity liệt kê — `?entities=plot,task` hoặc lặp `?entities=plot&entities=task`. Con trỏ giữ nguyên vị trí của entity không đọc, nên lượt pull đầy đủ sau đó không mất thay đổi. Entity không có → `422 validation_failed` (trường `entities`). Máy dùng khi thông điệp `sync_now` có khóa `entities` (`packages/agrigreen_mobile`: `PushMessage.entities` → `SyncService.syncOnce(entities:)`; chiều lên vẫn gửi hết; tên sai dạng / quá 30 → pull toàn bộ; trang pull một phần mang `reset = true` bị bỏ và máy pull đầy đủ từ con trỏ cũ, vì áp `reset` sẽ thay toàn bộ dữ liệu tham chiếu).
- Source trả `403` cho người gọi (không có vai đọc entity, vd nông dân với `task`) → entity đó bị bỏ qua trong trang, các entity khác vẫn đồng bộ (như `pubkey`).
- `cursor` là chuỗi **mờ, có chữ ký HMAC** (khóa `AG_SYNC_CURSOR_KEY`), gắn với tenant + thiết bị + phạm vi. Sửa, cắt, hoặc dùng con trỏ của thiết bị khác → `400 sync.cursor_invalid`; máy pull lại không có `cursor`. Không có `cursor` = từ đầu.
- Các entity chia đều hạn mức trang theo vòng (round-robin); phần entity dùng không hết chia lại cho entity còn dữ liệu. `has_more = true` có thể theo sau bởi một trang rỗng `has_more = false`. Điểm bắt đầu vòng xoay theo trang (lưu trong con trỏ): với `limit` nhỏ hơn số entity (vd `limit=1`), mỗi trang bắt đầu từ entity kế tiếp nên không entity nào bị bỏ đói khi entity đầu luôn còn dữ liệu.
- `cursor` là chuỗi **mờ, có chữ ký HMAC** (khóa `AG_SYNC_CURSOR_KEY`), gắn với tenant + thiết bị + phạm vi. Sửa, cắt, hoặc dùng con trỏ của thiết bị khác → `400 sync.cursor_invalid`; máy pull lại không có `cursor`. Không có `cursor` = từ đầu. `AG_SYNC_CURSOR_KEY` (≥ 32 byte) **bắt buộc ở production** — thiếu thì api-platform dừng lúc khởi động (`sync.CheckCursorKey`, ADR-0006); Local/Dev thiếu khóa thì sinh ngẫu nhiên theo tiến trình.
- Các entity chia đều hạn mức trang theo vòng (round-robin); phần entity dùng không hết chia lại cho entity còn dữ liệu. `has_more = true` có thể theo sau bởi một trang rỗng `has_more = false`.
- `reset = true`: phạm vi của người gọi đã đổi (điều chuyển, sáp nhập xã) so với lúc phát con trỏ. Trang này bắt đầu lại từ đầu; máy thay toàn bộ dữ liệu tham chiếu bằng kết quả của lượt mới.
- Phạm vi rỗng (không có đơn vị công tác, tổ chức bị vô hiệu hóa) → `changes: []`, `has_more: false`.
- **Tài khoản nông dân** (vai `farmer`, App HTX — token không có `org_ref` nên phạm vi pf/party rỗng): pf/sync đi **đường riêng** theo liên kết tài khoản ↔ nông hộ (`accounts/port.FarmerHouseholds`, nối bằng `sync.Module.UseFarmers`). Phạm vi nông dân = `sync/port.FarmerScope{Households, Cooperatives, Communes}` (nông hộ đang liên kết, HTX và xã của chúng). CHỈ các Source hiện thực `sync/port.FarmerSource` được gọi, qua `FarmerChanges`, cho các entity khai trong `FarmerEntities()` — mọi entity khác đóng với nông dân (mặc định chặn, không bao giờ gọi `Changes` với phạm vi suy từ xã):
  - `plot` (pf/geo): thửa hiện thuộc hoặc từng thuộc nông hộ liên kết — đã chuyển sang hộ khác hoặc ngừng dùng → tombstone; thửa hộ khác cùng xã không đi xuống;
  - `season`, `farm_log`, `sop_version` (pf/cropcycle): vụ/nhật ký của thửa thuộc nông hộ; SOP đã phát hành của hệ thống và của HTX các nông hộ;
  - `support_request` (pf/extension): yêu cầu của nông hộ liên kết;
  - `catalog_item` (dữ liệu công khai), `admin_unit` (các xã của nông hộ) (pf/masterdata); `document`, `library_item` (pf/content: tài liệu toàn quốc, của tỉnh/xã nông hộ; thư viện của chính người gọi);
  - `notification` (pf/notify): chỉ thông báo **cá nhân** gửi tới chính tài khoản (không nhận thông báo chủ đề theo vai/đơn vị/toàn quốc);
  - `pubkey`: khóa công khai của **chính các nông hộ liên kết** (để máy mã hóa ảnh nhật ký/yêu cầu hỗ trợ bằng khóa hộ mình, R-40) — pf/sync gọi `client.HouseholdPubKeys` (kho: `GET /v1/pubkeys/households?ref=…`, vai `farmer` được phép theo ref, không liệt kê theo địa bàn), không cần vai `vault_pubkey_sync`. Cùng cơ chế "lượt" + bản ghi đánh dấu như dưới; không có nông hộ → lượt rỗng chỉ có bản ghi đánh dấu.
  - Không có entity `task`, `directive`, `mrv_catalog` (vai cán bộ). Liên kết đổi (thêm/hủy nông hộ) → dấu vân tay phạm vi đổi → `reset = true`. Chưa nối pf/accounts → nông dân nhận `changes: []` như phạm vi rỗng.
- `pubkey` (do pf/sync cung cấp từ kho định danh): `data = {ref, area_code, public_key}` (base64 32 byte). Kho không có cursor cho khóa, nên mỗi khi tập khóa của phạm vi đổi, máy chủ gửi lại **toàn bộ** tập (một "lượt"); mọi khóa của lượt mang cùng `version`. Lượt kết thúc bằng bản ghi đánh dấu `{entity: "pubkey", id: "00000000-0000-0000-0000-000000000000", op: "tombstone", version: V}`: máy **xóa mọi pubkey có version < V** (hộ đã xóa định danh hoặc ra khỏi địa bàn). Phạm vi toàn quốc (`All`) không nhận khóa. Người gọi không có vai `vault_pubkey_sync` (hoặc vai đọc khóa khác của kho) thì không nhận khóa, các entity khác vẫn đồng bộ.

- Dữ liệu tham chiếu theo **phạm vi phân công** của người gọi (`party/port.ScopeResolver`):
  - danh mục dùng chung, đơn vị hành chính;
  - HTX, nông hộ (chỉ nhãn hiển thị tối thiểu, **không** họ tên đầy đủ, SĐT, CCCD);
  - thửa, nhiệm vụ được giao, quy trình kỹ thuật;
  - khóa công khai của hộ thuộc địa bàn.
- `entity` G1: `catalog_item`, `admin_unit`, `mrv_catalog`, `cooperative`, `household`, `plot`, `task`, `directive`, `document`, `library_item`, `pubkey`, `notification`. G2 (pf/cropcycle): `sop_version`, `season`, `farm_log` — xem mục pf/cropcycle.
- `catalog_item` (pf/masterdata): mọi mục **đang dùng** của các danh mục dùng chung `rice_variety`, `farm_stage`, `machine_type`, `season`, `pest` — không lọc theo địa bàn. `data = {catalog, code, name, attrs, revision, updated_at}` (cùng trường với `GET /v1/masterdata/catalogs/{catalog}`). `id` = UUIDv5 cố định theo `(catalog, code)` (`masterdata/port.CatalogItemSyncID`); `version` = `sync_seq` (luôn tăng, kể cả khi dùng lại mục). Ngừng dùng → tombstone cùng `id`; dùng lại → upsert `version` mới.
- `admin_unit` (pf/masterdata): **phiên bản** hiệu lực của đơn vị có mã thuộc phạm vi người gọi (`All`: mọi đơn vị). `data = {code, name, level, parent_code, valid_from, valid_to, successor_codes}`. `id` = UUIDv5 theo `(code, valid_from)` (`masterdata/port.AdminUnitSyncID`) vì mã có thể được dùng lại sau sắp xếp (vd `91`); `version` = `sync_seq`. Phiên bản đã hết hiệu lực (`valid_to` ≤ hôm nay theo giờ Việt Nam) → tombstone — gồm cả đơn vị cũ có đơn vị kế thừa trong phạm vi (đơn vị đã sáp nhập vào địa bàn). Đóng hiệu lực trong tương lai → upsert có `valid_to`; máy coi đơn vị hết hiệu lực từ ngày đó (người gọi có phạm vi cụ thể sẽ nhận `reset` khi phạm vi đổi vào ngày đó). Đơn vị có hiệu lực từ ngày tương lai cũng đi xuống (upsert, `valid_from` > hôm nay).
- `mrv_catalog` (pf/mrv): bản danh mục trường **đã duyệt** mới nhất của mỗi loại minh chứng, để app dựng và kiểm biểu mẫu khi ngoại tuyến. `data` = đúng thân `GET /v1/mrv/catalog` (`MrvCatalogVersion`, `status = approved`). `id` = UUIDv5 cố định theo `evidence_type` (`mrv/port.CatalogSyncID`); `version` = số phiên bản danh mục — duyệt phiên bản mới → upsert cùng `id`, máy thay bản cũ. Nháp, chờ duyệt, bị từ chối không đi xuống; không có tombstone ở G1. Cần vai đọc danh mục (`mrv_collector`, `mrv_reviewer`, `mrv_viewer`, `mrv_exporter`, `param_setter`, `param_approver`); không có vai → không nhận entity này, entity khác vẫn đồng bộ. Danh sách loại có bản đã duyệt: `GET /v1/mrv/evidence-types` → `{items: [{evidence_type, catalog_id, version, provisional, source_note, field_count, approved_at}]}`.- `notification` (pf/notify, ADR-0015): hộp thư của người gọi trong 30 ngày gần nhất, `data = {id, kind, severity, title, body, deep_link, subject_ref, created_at, read_at}`; luôn `upsert`; đánh dấu đã đọc ở nơi khác → đi xuống lại với `read_at` và `version` mới. Máy tự bỏ bản cũ hơn 30 ngày.
- **Thông điệp đẩy** (FCM, chỉ dữ liệu): `{kind, notification_id}` → app gọi `GET /v1/notifications` (hoặc pull) để lấy nội dung rồi hiện thông báo cục bộ; `{kind: "sync_now"}` → app chạy pull/push ngay. G2 (KN US-SYNC-02): `POST /v1/sync/nudge` nhận `entities[]` tùy chọn; khi có, thông điệp là `{kind: "sync_now", entities: "plot,task"}` (tên entity nối bằng dấu phẩy, không phải dữ liệu cá nhân — RB-7) → app gọi `GET /v1/sync/pull?entities=plot,task` rồi push. App cũ bỏ qua khóa lạ và pull toàn bộ — tương thích ngược. Token gắn bằng `POST /v1/devices/{device_id}/push-token {provider: "fcm", token}` sau khi đăng ký thiết bị, mỗi lần khởi động và khi FCM đổi token.
- `library_item` (pf/content, KN US-LIB-05): danh mục cá nhân của CHÍNH người gọi; `id` = `document_ref`, `data = {document_ref, saved_at, note, title, available}`; bỏ lưu → tombstone. Tài liệu đã lưu luôn nằm trong entity `document` của người đó (chỉ lưu được tài liệu đang hiển thị với họ); `available = false` khi tài liệu đã gỡ — máy giữ bản đã tải, nhãn "đã gỡ".
- `support_request` (G2, pf/extension, chiều xuống cho vai `farmer`): yêu cầu hỗ trợ của nông hộ liên kết với tài khoản, `data = {id, household_ref, plot_ref, category, urgency, description, photo_refs, gps_missing, labels, event_time, received_at, task_id, task_status, assigned, last_note, task_created_at}`; `version` = `sync_seq` của nhiệm vụ — đi xuống lại mỗi khi cán bộ tiếp nhận/xử lý/đóng. Vai khác nhận rỗng. Cán bộ (quản lý HTX, cán bộ KN) xem danh sách qua REST `GET /v1/extension/support-requests` (không qua pull).
- `htx_collection_schedule`, `htx_collection_statement` (G3-2, pf/cropcycle, chiều xuống cho `coop_manager` của HTX mình và `platform_admin`; vai khác nhận rỗng): lịch gom và bảng kê số cuộn MKG gửi qua van công bố. Lịch: `data = {schedule_ref, cooperative_ref, commune_code, window_from, window_to, status, revision, published_at, updated_at}`. Bảng kê: `data = {statement_ref, cooperative_ref, plot_ref, plot_code, season_ref, collected_on, rolls, revision, pending, response: {id, revision, outcome, htx_rolls, reason, responded_at} | null, published_at, updated_at}`; `version` = `sync_seq` (đổi khi MKG gửi bản sửa hoặc khi có phản hồi).
- `op: tombstone` nghĩa là ngừng hiển thị trên máy (hết phạm vi, ngừng dùng). Không có xóa cứng ở máy chủ.

## Chiều lên: push

`POST /v1/sync/push` (Idempotency-Key = mã lô do máy sinh)

```json
{
  "device_id": "0191…",
  "records": [
    {
      "id": "0191…",
      "idem_key": "dev:<device_id>:<seq>",
      "entity": "field_report",
      "op": "create",
      "event_time": "2026-12-02T08:15:00+07:00",
      "supersedes_id": null,
      "correction_reason": null,
      "payload": { }
    }
  ]
}
```

- `entity` G1:
  - `plot_boundary`: ranh giới thửa vẽ ngoại tuyến;
  - `field_report`: báo cáo hiện trường;
  - `task_update`: cập nhật nhiệm vụ hỗ trợ;
  - `consent`: đồng ý theo mục đích (Sink của pf/party, dùng lại use case ghi đồng ý của `POST /v1/consents`; payload `SyncConsentPayload` = `{subject_ref, purpose, text_version, status, channel?}`, `channel` mặc định `field_app`). Chỉ `op: create` — rút lại đồng ý là bản ghi **mới** `status: withdrawn`. `idem_key` của máy là khóa chống trùng của sổ đồng ý: gửi lại → `duplicate`. Cần vai `consent_writer`;
  - `evidence`: minh chứng, tham chiếu `file_ref`.
  - `support_request` (G2, pf/extension, HTX US-SUPPORT-01): yêu cầu hỗ trợ của **nông dân** (vai `farmer`), gửi được khi ngoại tuyến. Chỉ `op: create`. Payload `SupportPayload` = `{household_ref?, plot_ref?, category, urgency?, description, photo_refs[], location?: {lat, lon}, gps_accuracy_m?}` — `category` ∈ `dich_hai|benh_lua|thoi_tiet|ky_thuat|may_moc|khac`, `urgency` ∈ `binh_thuong|khan` (mặc định `binh_thuong`), `description` 10–2000 ký tự không chứa SĐT/CCCD, tối đa 5 `photo_refs` (tệp đã tải qua `/v1/files`), toạ độ decimal (chuỗi hoặc số). Không có `location` → vẫn nhận, nhãn "Thiếu vị trí GPS". `household_ref` bỏ trống khi tài khoản chỉ có một nông hộ. Khi máy chủ nhận (giờ máy chủ), cùng giao dịch tạo nhiệm vụ cho cán bộ khuyến nông (KN US-TASK-01); `event_time` là lúc bấm Gửi. Gửi lại cùng `idem_key` → `duplicate`.
  - `htx_collection_response` (G3-2, pf/cropcycle, RB-6 chiều mua): quản lý HTX xác nhận hoặc tranh chấp bảng kê số cuộn MKG, gửi được khi ngoại tuyến. Chỉ `op: create`; `id` (UUIDv7 máy sinh) là id phản hồi. Payload `{statement_ref, revision, outcome: confirmed|disputed, htx_rolls?, reason?}` — `revision` là bản bảng kê người dùng đã xem; MKG đã gửi bản sửa → `rejected` `cropcycle.collection_statement_stale` (máy hiện "Bị từ chối", người dùng phản hồi lại theo bản mới). `disputed` cần `htx_rolls` ≠ số MKG và `reason` 10–500 ký tự không chứa SĐT/CCCD; phản hồi thứ hai cho cùng revision cần `reason` và thay bản trước. Gửi lại cùng `idem_key` → `duplicate`.
- `op`:
  - `create`: bản ghi mới;
  - `correct`: bản ghi bù, bắt buộc có `supersedes_id` và `correction_reason`.
  - **Không có update hay delete.** Ghi thêm thắng ghi đè.

Phản hồi `200` trả kết quả **theo từng bản ghi**. Một bản ghi lỗi không làm hỏng cả lô:

```json
{ "results": [ { "id": "0191…", "status": "accepted" },
               { "id": "0191…", "status": "duplicate" },
               { "id": "0191…", "status": "conflict", "conflict_id": "0191…" },
               { "id": "0191…", "status": "rejected", "code": "validation_failed", "errors": [ ] } ] }
```

- Kiểm ở pf/sync trước khi chuyển cho module nhận: `device_id` trùng header `X-Device-Id`; tối đa 500 bản ghi mỗi lô; `id` là UUIDv7; `idem_key` đúng dạng và tiền tố là mã của **chính** thiết bị gửi; `op` đúng quy tắc (`create` không có `supersedes_id`/`correction_reason`, `correct` bắt buộc cả hai, lý do 10–500 ký tự); `event_time` có múi giờ và không vượt quá giờ máy chủ + 5 phút; `payload` là đối tượng JSON ≤ 256 KiB. Sai → `rejected`, `code = validation_failed`, `errors[]` theo trường.
- `id` trong kết quả là chuỗi máy gửi, nguyên văn (kể cả khi sai dạng), theo đúng thứ tự bản ghi.
- `rejected` với `code = sync.unknown_entity`: không có module nhận entity đó. `code = sync.sink_failed`: module nhận lỗi hệ thống — máy **giữ và tự gửi lại** sau, không bắt người dùng tạo bản ghi bù.
- Gọi lặp cùng Idempotency-Key trả đúng phản hồi cũ (header `Idempotent-Replayed: true`). Bản ghi gửi lại trong lô khác được nhận ra theo `idem_key` và trả `duplicate`.
- Mọi bản ghi nhận được ghi vào sổ chỉ ghi thêm `pf_sync.inbound` (kiểm toán): id, idem_key, entity, thiết bị, trạng thái, conflict_id và **SHA-256 của payload** — không lưu payload.
- `duplicate`: `idem_key` đã ghi. Máy coi là thành công và xóa khỏi hàng đợi.
- `conflict`: hai bản ghi cùng thực thể, ví dụ hai ranh giới cho cùng một thửa. Máy chủ **giữ cả hai**, đánh dấu chờ xử lý, có người phụ trách và SLA (C-8, tạm 72 giờ). Máy coi là đã gửi.
  - Sổ đăng ký `pf_sync.conflict`: đơn vị phụ trách = đơn vị công tác của người gửi, `due_at = opened_at + sync.conflict_sla_hours` (tham số masterdata; chưa duyệt → 72). Phát `pf.sync.conflict.opened`.
  - Admin API (vai `sync_conflict_resolver`): `GET /v1/sync/conflicts?status=&overdue=true`, `POST /v1/sync/conflicts/{id}/resolve {note, resolved_record_id?}` → phát `pf.sync.conflict.resolved`; module sở hữu entity áp quyết định bằng bản ghi bù.
  - Số xung đột mở quá hạn là chỉ số nghiệp vụ bắt buộc có cảnh báo (mục 19): gauge `ag.sync.conflict.overdue`.
- `rejected`: dữ liệu sai. Máy giữ bản ghi, hiển thị lỗi để người dùng tạo bản ghi bù.
- Máy xóa bản ghi cục bộ sau khi máy chủ xác nhận **+ 48 giờ**.

### Payload `plot_boundary` (pf/geo, `app.BoundaryPayload`)

`id` của bản ghi là `boundary_id` (UUIDv7 máy sinh) của phiên bản ranh giới.

Vai được đẩy: `plot_editor`, `coop_manager`, `platform_admin` (vai khác → `rejected forbidden`). Quản lý HTX (App HTX, HTX US-PLOT-01/02) không cần `plot_editor`: chỉ tạo/sửa thửa của HTX mình — thửa mới bỏ trống `cooperative_ref` nhận HTX của người vẽ, ghi HTX khác → `rejected geo.out_of_scope`, sửa thửa HTX khác → `rejected geo.plot_not_found`.

```json
{
  "plot_ref": "0191…",
  "name": "Thửa bờ kênh",
  "commune_code": "31117",
  "cooperative_ref": "0191…",
  "household_ref": null,
  "geometry": { "type": "Polygon", "coordinates": [[[105.60, 10.05], [105.601, 10.05], [105.601, 10.051], [105.60, 10.051]]] },
  "gps_accuracy_m": "3.5",
  "declared_area_ha": "1.20",
  "source": "kn_field_app",
  "confirm_overlap": false
}
```

| Trường | Bắt buộc | Ghi chú |
|---|---|---|
| `plot_ref` | `create` | UUIDv7 máy sinh cho thửa mới. Với `correct` bỏ qua — thửa suy ra từ `supersedes_id` |
| `commune_code` | `create` | Mã xã hiện hành, phải trong phạm vi người gửi |
| `name`, `cooperative_ref`, `household_ref` | không | Quản lý HTX bỏ trống `cooperative_ref` → HTX của mình. `household_ref` cần `cooperative_ref` |
| `geometry` | có | GeoJSON Polygon/MultiPolygon WGS84 (EPSG:4326); ≥ 3 đỉnh phân biệt không thẳng hàng; vòng chưa khép được tự khép |
| `gps_accuracy_m`, `declared_area_ha` | không | Số thập phân **dạng chuỗi** |
| `source` | không | `kn_field_app` (mặc định) hoặc `htx_app` |
| `confirm_overlap` | không | Người vẽ đã xác nhận giữ nguyên chồng lấn cùng HTX / thửa nháp trên máy (HTX US-PLOT-04 AC-2) |

`op: correct`: `supersedes_id` = `boundary_id` của phiên bản đang sửa, `correction_reason` ≥ 10 ký tự.

Kết quả (máy ngoại tuyến không thấy nhau nên máy chủ **không chặn** vì chồng lấn — mục 15):

| Tình huống | `status` |
|---|---|
| Hợp lệ, không chồng lấn vượt ngưỡng `geo.overlap_tolerance_pct` | `accepted` |
| Chồng lấn thửa cùng HTX / thửa nháp, `confirm_overlap: true` | `accepted` (xung đột `same_coop_overlap` vẫn mở để xem xét) |
| Chồng lấn thửa cùng HTX / thửa nháp, chưa xác nhận | `conflict` (`same_coop_overlap`, phụ trách = HTX) |
| Chồng lấn thửa HTX khác | `conflict` (`cross_coop_overlap`, phụ trách = cán bộ KN của xã) — trực tuyến thì bị chặn `geo.overlap_cross_coop` |
| `correct` dựa trên phiên bản không còn hiện hành | `conflict` (`concurrent_edit`); bản ghi được giữ nhưng không thành hiện hành |
| Lệch diện tích so với `declared_area_ha` > 5 % | không chặn; ghi `area_deviation_pct` vào `pf.geo.plot_boundary.recorded` |
| Hình sai, ngoài phạm vi, ngoài địa giới xã | `rejected` với mã `geo.*` (docs/errors.md) |

Chiều xuống entity `plot`: `data = {ref, code, name, commune_code, cooperative_ref, household_ref, status, version, boundary_id, area_ha, source, needs_review, geometry, updated_at}`; `version` của thay đổi là `revision` của thửa. Tombstone khi thửa ngừng dùng, hoặc (quản lý HTX) thửa chuyển sang HTX khác.

#### KN v7.7 (FN-XA-03.05) — người gửi có ngữ cảnh phiên KN (`X-AG-Context`)

- Người gọi có `X-AG-Context` (Principal.KN ≠ nil): KHÔNG cần vai realm `plot_editor`. Được đẩy khi vai ngữ cảnh là `kn_xa` hoặc `admin`; `kn_tw` → `rejected geo.read_only`; `kn_tinh` → `rejected forbidden`; Admin chế độ hỗ trợ thiếu lý do → `rejected geo.support_reason_required`. Phạm vi = đối số scope do pf/sync truyền (= `KN.Scope`: Cấp Xã chỉ các xã phụ trách) — xã ngoài phạm vi → `rejected geo.out_of_scope`; sửa thửa ngoài phạm vi hoặc của phòng ban khác → `rejected geo.plot_not_found`.
- Payload `plot_boundary` thêm (đều tùy chọn):

| Trường | Ghi chú |
|---|---|
| `lot_name` | Tên lô (≤ 200 ký tự, không SĐT/CCCD). Vắng → dùng `name`; KN cần một trong hai (`rejected validation_failed` trường `lot_name`) |
| `note` | Ghi chú (≤ 1000 ký tự, không SĐT/CCCD) |
| `farmer_name` | Tên nông dân — máy chủ tạo chủ thể "người" trong kho định danh → `farmer_ref`; KHÔNG lưu tên ở pf_geo, không ghi log |
| `shape` | `polygon` (mặc định) \| `circle` \| `square` — máy đổi hình tròn/vuông thành đa giác trước khi gửi; `geometry` luôn là Polygon/MultiPolygon |

- Với `op: correct` chỉ `shape` được dùng (thuộc tính khác sửa qua `PUT /v1/kn/plots/{ref}`). Thửa mới của KN mang `department_code` = phòng ban của ngữ cảnh (Admin: `trong_trot`).
- Chồng lấn khi đồng bộ: GIỮ ngữ nghĩa hiện có (máy ngoại tuyến không thấy nhau) — chồng lấn → `conflict` (bản ghi được giữ, xung đột `same_coop_overlap` / `cross_coop_overlap` mở để cán bộ xử lý), sửa trên phiên bản cũ → `conflict` (`concurrent_edit`). Chỉ API trực tuyến `/v1/kn/plots` mới chặn (409 `geo.plot_overlap`).
- Chiều xuống `plot` thêm vào `data` (chỉ khi có giá trị, khóa vắng = null): `lot_name`, `note`, `declared_area_ha` (chuỗi decimal), `shape`, `department_code`, và `farmer_label` (nhãn đã che, vd "Trần T. B." — chỉ với người gọi có ngữ cảnh KN; không bao giờ có tên đầy đủ). Với người gọi KN, thửa mang `department_code` của phòng ban khác → tombstone; thửa không mang phòng ban (có trước v7.7) đi xuống mọi phòng ban.

### Payload `field_report` (pf/extension, `app.ReportPayload`)

Nguồn: `internal/platform/extension/app/payload.go` (`ReportPayload.ToReport`), `domain/report.go` (`Report.Validate`), `app/reports.go` (`SubmitReport`). `id` của bản ghi là mã báo cáo; `event_time` lấy từ phong bì. Trường lạ bị bỏ qua (app **không** gửi GPS).

```json
{
  "kind": "sowing_progress",
  "commune_code": "31117",
  "cooperative_ref": "0192…",
  "period": "2026-12",
  "season_code": "DX",
  "variety_code": "OM5451",
  "sown_area_ha": "12.5",
  "sown_on": "2026-11-20",
  "growth_stage": "",
  "pest_code": "",
  "severity": "",
  "affected_area_ha": null,
  "urgent": false,
  "note": "Sạ lan, đủ nước"
}
```

| Trường | Quy tắc |
|---|---|
| `kind` | `sowing_progress` (FR-01) hoặc `growth_pest` (FR-02) |
| `commune_code` | bắt buộc; mã **xã** (cấp commune) trong phạm vi người gửi |
| `period` | `YYYY-MM`, không ở tương lai theo giờ Việt Nam (R-12); kỳ đã chốt ở tỉnh → chỉ nhận bản ghi bù |
| `cooperative_ref` | UUID; **bắt buộc** với `sowing_progress`, HTX phải thuộc xã |
| `season_code`, `variety_code` | `sowing_progress`: bắt buộc; `season_code` `[A-Za-z0-9_]{1,40}`; tra danh mục masterdata `season`, `rice_variety` |
| `sown_area_ha` | `sowing_progress`: bắt buộc, chuỗi decimal > 0, ≤ 100 000, ≤ 4 chữ số lẻ |
| `sown_on` | tùy chọn, `YYYY-MM-DD` giờ Việt Nam, không ở tương lai |
| `growth_stage` | `growth_pest`: bắt buộc, `[A-Za-z0-9_]{1,40}` |
| `pest_code` | `growth_pest`: bắt buộc, tra danh mục `pest` |
| `severity` | `growth_pest`: `nhe` \| `trung_binh` \| `nang` |
| `affected_area_ha` | `growth_pest`: tùy chọn, chuỗi decimal ≥ 0, ≤ 100 000, ≤ 4 chữ số lẻ |
| `urgent`, `note` | `note` ≤ 2000 ký tự, **không** chứa SĐT hay số CCCD 12 số (`ErrPIIInText`) |

Trường của loại kia bị máy chủ xóa khi kiểm; app gửi chuỗi rỗng/null. `op: correct` cần lý do ≥ 10 ký tự và phải thay phiên bản **hiện hành**; loại, xã, HTX, kỳ (và vụ, giống với `sowing_progress`) lấy từ bản bị thay — app khóa các trường này khi tạo bản ghi bù. Fixture: `apps/field-app/test/fixtures/contract/field_report_*.json`.

### Payload `evidence` (pf/mrv, `app.CapturePayload`)

Nguồn: `internal/platform/mrv/app/evidence.go` (`CapturePayload.Parse`, `Capture`), `domain/evidence.go` (`ValidateEnvelope`), `domain/catalog.go` (`ValidateRecord`, `checkValue`). `id` (UUIDv7), `event_time`, `device_id`, `supersedes_id`, `correction_reason` lấy từ phong bì bản ghi đồng bộ, không từ payload.

```json
{
  "evidence_type": "awd_log",
  "catalog_version": 1,
  "admin_unit_code": "31117",
  "plot_ref": "0192…",
  "household_ref": "0192…",
  "season_code": "DX",
  "values": {"stage": "de_nhanh", "awd_water_level_cm": "-12.5", "awd_dry_days": 5},
  "file_refs": [{"file_ref": "0192…", "sha256": "<64 hex thường của ciphertext>"}],
  "gps": {"lat": "10.0452", "lon": "105.7701", "accuracy_m": "4.5"}
}
```

| Trường | Quy tắc |
|---|---|
| `evidence_type` | `^[a-z][a-z0-9_]{2,39}$`; G1: `awd_log`, `field_checklist` |
| `catalog_version` | số nguyên dương — phiên bản danh mục **đã duyệt** app dùng để dựng biểu mẫu; máy chủ kiểm và **ghim** đúng phiên bản này (bỏ trống = bản mới nhất lúc ghi; app luôn gửi) |
| `admin_unit_code` | `^[0-9A-Z][0-9A-Z.-]{0,39}$`, trong phạm vi người gửi |
| `plot_ref`, `household_ref` | UUID; có `household_ref` → bắt buộc đồng ý `mrv.evidence` (`rejected mrv.consent_missing`) |
| `season_code` | 1–40 ký tự |
| `values` | chỉ khóa **không dành riêng** của danh mục; khóa lạ hoặc khóa dành riêng → lỗi `values.<khóa>`. Kiểu: `decimal` = **chuỗi** `^-?\d{1,16}(\.\d{1,8})?$`, ≤ `max_scale` chữ số lẻ, trong `[min, max]`; `integer` = số JSON hoặc chuỗi chữ số; `boolean`; `enum` ∈ `enum_values`; `date` `YYYY-MM-DD`; `text` ≤ 2000 và trong `[min, max]` ký tự; `photo` = danh sách `file_ref` có trong `file_refs`, không trùng |
| `file_refs` | ≤ 20, `file_ref` UUID không trùng, `sha256` 64 hex **thường** (băm ciphertext). Khóa dành riêng `photo` kiểm số ảnh theo `min`/`max` |
| `gps` | chuỗi decimal, lat ∈ [-90, 90], lon ∈ [-180, 180], `accuracy_m` ≥ 0; dữ liệu cá nhân khi gắn với hộ. Khóa dành riêng `gps` |

Khóa dành riêng của danh mục lấy giá trị từ cột riêng: `plot_ref` → `plot_ref`, `season` → `season_code`, `gps` → `gps`, `photo` → `file_refs`. App chỉ tạo bản ghi `evidence` khi **mọi** ảnh của biểu mẫu đã `POST /v1/files` xong (bảng `evidence_form`), rồi điền `file_refs` bằng `file_ref` máy chủ trả và SHA-256 ciphertext. Fixture: `apps/field-app/test/fixtures/contract/evidence_payloads.json`, `mrv_catalogs_g1.json`.

### Danh mục cần cho biểu mẫu

- **Danh mục trường MRV:** entity pull `mrv_catalog` (bản đã duyệt mới nhất theo loại; xem chiều xuống). `GET /v1/mrv/evidence-types` liệt kê loại có bản đã duyệt; `GET /v1/mrv/catalog?evidence_type=&version=` vẫn dùng được khi cần một phiên bản cụ thể.
- **Danh mục masterdata** (`rice_variety`, `season`, `pest` cho `field_report`; `season` cho `evidence`): entity pull `catalog_item`. Đơn vị hành chính: entity pull `admin_unit`. `cooperative`, `household` chưa có nguồn pull — mã xã và HTX lấy từ `plot`/`task` đã đồng bộ.
## pf/cropcycle (App HTX, G2)

Nguồn: `internal/platform/cropcycle/app/sync.go`. Vai: `farmer` (thửa của các hộ liên kết với tài khoản), `coop_manager` (HTX của mình), `platform_admin`; cán bộ KN (`kn_officer`, `kn_province_admin`, `kn_national_admin`) chỉ nhận chiều xuống theo phạm vi xã. Người gọi không có vai nào kể trên không nhận các entity này (các entity khác vẫn đồng bộ).

### Chiều xuống

- `sop_version`: phiên bản SOP **đã phát hành** của mẫu hệ thống + mẫu riêng của HTX người gọi (quản trị/KN: mọi mẫu). `id` = id phiên bản; `version` = `sync_seq` (cấp lúc phát hành). `data = {id, template_ref, code, name, crop, scope, cooperative_ref, template_status, version_no, steps[], harvest_day_offset, harvest_window_days, published_at}`; `steps[] = {seq, name, stage_code, activity_code, day_offset, window_days, mandatory, evidence_kinds[], phi_days?, instruction?}`. Phiên bản đã phát hành bất biến; mẫu ngừng dùng vẫn upsert với `template_status = retired` (vụ đã ghim vẫn cần). Không có tombstone.
- `season`: vụ trong phạm vi. `id` = `season_ref`; `version` = `sync_seq`. `data = {season_ref, plot_ref, plot_code, household_ref, cooperative_ref, commune_code, season_code, season_year, variety_code, sop_version_id, sowing_date, expected_harvest_date, harvest_open_date, area_ha, status, current_event_id, harvest_id, updated_at}`. Vụ bị hủy → tombstone. `current_event_id` là `supersedes_id` khi sửa/hủy vụ.
- `farm_log`: phiên bản nhật ký kèm trạng thái duyệt. `id` = id phiên bản; `data = {id, log_ref, version, season_ref, plot_ref, activity_code, sop_step_seq, event_time, gps_source, location_status, machine_codes[], inputs[{name, qty, unit}], note, file_refs[], batch_ref, on_behalf, status (pending|approved|needs_supplement), review_note, reviewed_at, updated_at}`. **Không** gửi toạ độ (chỉ nhãn vị trí). Phiên bản bị thay (`superseded`) → tombstone; máy giữ phiên bản mới cùng `log_ref`. Duyệt/yêu cầu bổ sung → upsert lại với `version` mới.

### Chiều lên

Mọi entity chỉ `create` / `correct`; `event_time` là lúc xảy ra tại ruộng (R-13); `id` là UUIDv7 máy sinh; `idem_key` chống trùng qua sổ của module → gửi lại trả `duplicate`. Lỗi nghiệp vụ trả `rejected` với mã `cropcycle.*` (docs/errors.md). GPS luôn dạng `gps: {lon, lat, accuracy_m, source}` (decimal **dạng chuỗi**, `source` ∈ `device | photo_exif | manual_pick | denied | no_device`; bỏ trống `gps` = `denied`). Thiếu/lệch GPS **không** chặn: máy chủ gắn `location_status` = `match` (≤ `cropcycle.gps_match_tolerance_m`, mặc định 50 m) | `mismatch` | `missing`.

| entity | op | payload | ghi chú |
|---|---|---|---|
| `season_open` | `create` | `{season_ref?, plot_ref, season_code, season_year, variety_code, sowing_date, sop_version_id?, copy_previous?, gps?}` | `id` = id sự kiện mở vụ; `season_ref` bỏ trống = `id`. Ngày sạ trong [hôm nay − 15, + 30] theo giờ Việt Nam **lúc máy chủ nhận**; SOP tự gán theo giống; thửa đã có vụ mở → `rejected cropcycle.season_already_open` |
| `season_open` | `correct` | `{season_ref, variety_code?, sowing_date?, sop_version_id?, cancel?}` | `supersedes_id` = `current_event_id` của vụ; không phải hiện hành → `rejected cropcycle.concurrent_edit`. `cancel: true` = hủy vụ mở nhầm |
| `farm_log` | `create` | `{season_ref? \| plot_ref?, activity_code, sop_step_seq?, machine_codes[], inputs[{name, qty, unit}], note, file_refs[], gps?}` | Cần vụ đang mở (`cropcycle.season_not_open`); bản ghi tới muộn có `event_time` trước lúc vụ hoàn thành vẫn được nhận. `file_refs` là `file_ref` đã `POST /v1/files` |
| `farm_log` | `correct` | `{activity_code, sop_step_seq?, machine_codes[], inputs[], note, file_refs[]}` | `supersedes_id` = id phiên bản đang sửa. `event_time` và GPS lấy từ bản bị thay (HTX US-LOG-01 AC-3). Dựa trên phiên bản không còn hiện hành (hai máy cùng sửa) → **giữ cả hai**: `conflict` (bản ghi `became_current = false`), pf/sync mở xung đột |
| `farm_log_batch` | `create` | `{plot_refs[], activity_code, sop_step_seq?, machine_codes[], inputs[], note, gps?}` | Một đầu lô + một dòng mỗi thửa (HTX US-LOG-03). Thửa chưa mở vụ / ngoài phạm vi bị loại, ghi lý do trong đầu lô; không còn thửa nào → `rejected cropcycle.batch_empty` |
| `harvest` | `create` | `{season_ref? \| plot_ref?, harvest_date?, mass_kg, moisture_pct?, confirm_anomaly?, gps?}` | `mass_kg` chuỗi decimal ≤ 3 chữ số lẻ. Trước `harvest_open_date` → `cropcycle.harvest_too_early`; 0 / vượt ngưỡng chưa xác nhận → `cropcycle.yield_anomaly` (máy hỏi lại người dùng rồi gửi bản ghi **mới** với `confirm_anomaly: true`). Đóng vụ cùng giao dịch |
| `harvest` | `correct` | `{mass_kg, harvest_date?, moisture_pct?, confirm_anomaly?}` | `supersedes_id` = khai báo hiện hành của vụ (`harvest_id`) |

## Báo cáo KN v7.7 (P7, field-app Cán bộ xã — FN-XA-03.01/03.02, N-07)

Nguồn: `internal/platform/extension/reportcycle/app/sync.go` (`ReportSync`, `ReportSink`), `store/sync.go`; migration pf 000077 (`report_task.sync_seq` tăng ở mọi UPDATE qua trigger, `report_task.created_seq` cấp một lần). Kiểm thử: `tests/kn77/p7_sync_integration_test.go` (`TestFN_XA_03_02_OfflineReportSync`).

**Ngữ cảnh phiên.** Request mang `X-AG-Context` (P1) → pf/sync lấy phạm vi pull/push = `Principal.KN.Scope` (Cấp Xã: các xã phụ trách), KHÔNG theo `org_ref` của token; dấu vân tay con trỏ có thêm `kn:<vai>:<phòng ban>:` → đổi vai trò/phòng ban/xã phụ trách = `reset: true`. Đơn vị phụ trách xung đột = Nút của ngữ cảnh. Không có header → đường cũ (`ScopeResolver`), entity báo cáo bị bỏ qua (Source trả 403), các entity khác vẫn đồng bộ. Chỉ vai `kn_xa` nhận/gửi entity báo cáo; dữ liệu lọc theo phòng ban của ngữ cảnh.

### Chiều xuống

- `report_task`: `id` = id công việc, `version` = `sync_seq`. `data = {id, batch_id, period_id, template_id, version_id, form_code, form_name, commune_code, commune_name, department_code, report_type, period_type, slot, period_block, period_label, period_range_label, period_labels[], season_label, period_from, period_to, due_at, auto_close_at, period_closed, reopened_until, status, display_status, open, open_slots[], adjustment_reason, latest_version, latest_submission_id, submitted_submission_id, submitted_at, provincial{chữ cái: giá trị}, updated_at}`. `display_status`/`open` tính lúc máy chủ trả — máy tự tính lại "Trễ hạn" theo `due_at`. Chốt kỳ, mở lại, nhắc, yêu cầu điều chỉnh, sửa hạn đều làm công việc lấy `sync_seq` mới. Thu hồi đợt → tombstone. Giá trị khe kỳ khác (chỉ xem) máy tự ghép từ `report_submission` của các công việc cùng đợt × mẫu × xã.
- `form_layout`: `id` = `version_id`, `version` = `created_seq` lớn nhất của công việc dùng phiên bản. `data = {version_id, template_id, code, name, version, definition}` — định nghĩa lưới CHƯA thay nhãn kỳ; máy áp nhãn kỳ và vụ như `reports/form.Layout` (cột theo kỳ rộng 1 → tiêu đề = nhãn kỳ; ô tiêu đề có `period_slot` → nhãn kỳ; khe vượt số kỳ của đợt → `inactive`; `{season}` → `season_label`). Phiên bản bất biến nên chỉ đi xuống khi có công việc mới dùng nó.
- `report_submission`: bài nộp mới nhất của công việc. `id` = id công việc, `version` = `sync_seq` của công việc. `data = {task_id, submission_id, version_no, kind, rows[{key, indicator, input{}, cells{}}], issues[CellIssue], event_time, recorded_at}`. `cells` gồm ô công thức do MÁY CHỦ tính (nguồn đúng); ngoại tuyến máy hiện "chưa tính".

### Chiều lên `report_submission`

Chỉ `op: create`; `id` = UUIDv7 máy sinh = id bài nộp (P4 nhận gửi lại cùng id → `duplicate`). `payload = {task_id, kind: draft|submitted, rows[{indicator?, input{chữ cái: chuỗi}}], base_version}` (`base_version` = `latest_version` máy đã thấy qua pull). Đi qua đúng use case `Submit` của REST (kiểm ô, khe kỳ đang mở, ràng buộc mẫu, chốt kỳ, chế độ hỗ trợ).

| Tình huống | Kết quả |
|---|---|
| Hợp lệ | `accepted` |
| Gửi lại cùng `idem_key` hoặc cùng `id` | `duplicate` (không thêm phiên bản) |
| Phiên bản liền trước do người/máy KHÁC ghi và mới hơn `base_version` | `conflict` — bản vẫn được lưu thành phiên bản mới (RB-1, giữ cả hai), pf/sync mở xung đột |
| Vi phạm yêu cầu mẫu khi Gửi | `rejected` `extension.submission_invalid`, `errors[{field: "rows.<row_key>.<chữ cái>", reason}]` |
| Kỳ đã chốt | `rejected` `extension.period_closed` |
| Công việc ngoài phạm vi / không có | `rejected` `extension.report_task_not_found` |

## Biểu kỳ cap/forms (W11.42, field-app Cấp Xã — CNTS BM-01/04/05/17)

Nguồn: `internal/platform/forms/app/sync.go` (`SyncSource`, `SyncSink`), `store/meta.go`; migration pf 000149 (`pf_forms.sync_feed`: trigger AFTER INSERT trên `period`, `period_event` native, `submission`, `review`, `held_submission` thêm một dòng `feed_seq` tăng dần — `unit_code '*'` = cả kỳ). Kiểm thử: `internal/platform/forms/app/cnts_integration_test.go` (`TestFormsCNTSPeriods_W1142`), field-app `test/cnts_period_test.dart`.

Chỉ người gọi là CẤP LÁ của một profile (`kn_cnts`: vai `kn_xa` phòng CNTS) nhận/gửi; không có → Source trả 403 (pf/sync bỏ qua entity). Phạm vi = ngữ cảnh phiên (xã phụ trách; cơ sở/hộ lọc theo xã cha và giới hạn `facility_refs`/`household_refs` W11.07).

- Chiều xuống `forms_task`: `id` = UUIDv5 cố định theo (kỳ, đơn vị) (`forms/app.TaskID`), `version` = `feed_seq`. `data` = `FormsTask` của OpenAPI (`/v1/forms/tasks`): kỳ, đơn vị, loại đơn vị, xã cha, biểu (mã, tên, phiên bản, `fields[]`), khóa/khoảng kỳ, `due_at`, `period_status`, `open`, `status` (kể cả `overdue`, `missed`, `awaiting_reopen`), phiên bản cuối `version` + `values`, `nil_report`, `note`, `explanation`, `late`, `return_note`, `held_at`. Kỳ hủy → tombstone. Máy tự tính lại "Trễ hạn" theo `due_at`.
- Chiều lên `forms_submission`: chỉ `op: create`; `id` = UUIDv7 máy sinh = mã phiên bản (gửi lại cùng `id` → `duplicate`). `payload = {profile, period_id, unit, values{}, base_version, nil_report, note, explanation}`; `event_time` = `captured_at` (giờ thiết bị — chỉ lưu để kiểm toán). Đi qua `SubmitWith` của REST. **Trễ hạn tính theo giờ MÁY CHỦ nhận bài** (D8, pf 20261013091503): bản ghi trên máy trước hạn nhưng đồng bộ sau hạn vẫn là trễ (`late = true`) và cần `explanation`; máy chỉ kiểm sớm theo đồng hồ máy để gợi ý.

| Tình huống | Kết quả |
|---|---|
| Hợp lệ, kỳ còn nhận bài | `accepted` |
| Kỳ đã chốt với đơn vị | `accepted` + `code: forms.awaiting_reopen` — bài GIỮ "Chờ mở kỳ" (`held_submission`), lượt pull sau trả `status: awaiting_reopen` |
| `base_version` khác phiên bản hiện tại | `rejected` `forms.submission_conflict` — KHÔNG ghi đè; máy báo tải bản mới |
| Ô sai / quy tắc chặn | `rejected` `forms.values_invalid`, `errors[{field: <khóa trường>, reason}]` |
| Máy chủ nhận sau hạn (kể cả bản ghi trên máy trước hạn) thiếu `explanation` | `rejected` `forms.late_reason_required` — người nhập ghi lý do trễ rồi gửi lại (bản mới, `id` mới) |
| Kỳ đã hủy | `rejected` `forms.period_cancelled` |

`catalog_item` (pf/masterdata) từ W11.42 gồm cả 12 danh mục CNTS `cnts_*` (giá trị hiện hành; `attrs` như `GET /v1/masterdata/catalogs/{catalog}`).

## Tệp minh chứng

`POST /v1/files` (multipart, Idempotency-Key):
- phần `meta`: JSON theo `envelope-format.md`;
- phần `blob`: ciphertext.

Trả về `201 {file_ref}`. Giới hạn 15 MB mỗi tệp. Máy chủ kiểm `sha256_ciphertext`. Máy chủ không bao giờ thấy bản rõ, trừ khi vault mở DEK để hiển thị có mục đích.

## Làm rõ (27/09/2026, sau khi ráp app Flutter với vault)

1. **Khóa công khai** (`entity: pubkey`): `data = {"ref": "<uuid>", "area_code": "<mã xã>", "public_key": "<base64 chuẩn, 32 byte>"}`. Đây là mã hóa `[]byte` mặc định của JSON trong Go.
2. **`POST /v1/files`**: phần `meta` là **trường văn bản** multipart, chứa JSON; phần `blob` là tệp nhị phân.
3. **Idempotency-Key** do app đặt:
   - `devreg:<device_id>` cho đăng ký thiết bị;
   - `file:<file_ref>` cho tải tệp;
   - `batch:<uuid>` cho một lô push.
   - Máy chủ giữ khóa 48 giờ. Lô gửi lại sau 48 giờ vẫn an toàn vì từng bản ghi được chống trùng theo `idem_key`.
4. **Bản ghi bị `rejected`** không được lưu ở máy chủ. Muốn sửa, người dùng tạo bản ghi **mới** (`op: create`), không dùng `correct`. `correct` chỉ trỏ tới bản ghi máy chủ đã nhận (`accepted` hoặc `conflict`).
5. **Schema payload** theo từng entity nằm trong `contracts/openapi/application.yaml`, mục `pf/sync`. Mỗi module công bố schema của entity nó nhận.
6. **Bộ vector** `internal/shared/envelope/testdata/vectors.json` dùng tên trường `recipient_secret_key_hex`, `recipient_public_key_hex`, `plaintext_hex`, `dek_hex`, `nonce_hex`, `ciphertext_hex` (ciphertext ‖ tag), `wrapped_dek_hex`, `sha256_ciphertext_hex`.
- phần `file_ref` (tùy chọn): UUIDv7 máy sinh, để bản ghi `evidence` tạo ngoại tuyến tham chiếu tệp trước khi tải. Bỏ trống → máy chủ sinh.

Trả về `201 {file_ref, sha256_ciphertext, size}`. Giới hạn 15 MB mỗi tệp. Máy chủ kiểm `sha256_ciphertext` (sai → `422 sync.file_checksum_mismatch`), `v = 1`, `alg`, `nonce` 12 byte, `wrapped_dek` 80 byte, `subject_ref`, `content_type` (image/jpeg, image/png, image/webp, image/heic, application/pdf, video/mp4). Gửi lại cùng `file_ref` + cùng bản mã → `200`; khác bản mã → `409 sync.file_ref_taken`. Kho đối tượng chỉ giữ bản mã tại `files/<tenant>/<file_ref>`. Máy chủ không bao giờ thấy bản rõ, trừ khi vault mở DEK để hiển thị có mục đích.

Đọc tệp (vai `vault_evidence_viewer`, header `X-Purpose` = `mrv.evidence` hoặc `extension.field_support`, chủ thể phải đã đồng ý cho mục đích đó):
- `GET /v1/files/{ref}` → siêu dữ liệu + `download_url` tải **bản mã**, hết hạn sau 5 phút.
- `GET /v1/files/{ref}/content` → bản rõ để hiển thị: kho định danh mở DEK (ghi sổ truy cập có mục đích), giải mã trong bộ nhớ request, `Cache-Control: no-store`. Mỗi lần đọc ghi một dòng log `files.access` (chỉ mã tham chiếu và mục đích, không toạ độ).
