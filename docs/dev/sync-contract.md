# Hợp đồng đồng bộ ngoại tuyến (mục 15, ADR-0006)

G1 đồng bộ qua **Application API** (pull/push có cursor và idempotency). Cách này không phụ thuộc engine logical replication, nên không bị chặn bởi câu hỏi 23.1. Máy chủ hiện thực trong module `internal/platform/sync`; app Flutter ở `apps/field-app`.

## Đăng ký thiết bị

`POST /v1/devices` (Idempotency-Key) → `201 {device_id, registered_at, platform, app_version, revoked}`

Thân: `{ "device_id": "<UUIDv7 sinh trên máy>", "platform": "android" | "ios", "app_version": "…" }`

- `actor_ref` và `org_ref` của thiết bị lấy từ **token**, không từ thân (R-16).
- Cùng người đăng ký lại cùng mã (cài lại, nâng cấp app) → `200`, cập nhật `app_version`. Mã của người khác → `409 sync.device_taken`.
- **Mọi** lời gọi đồng bộ (`/v1/sync/pull`, `/v1/sync/push`, `POST /v1/files`) mang header `X-Device-Id`. Thiếu header, thiết bị chưa đăng ký trong tenant, hoặc đăng ký bởi tài khoản khác → `403 sync.device_unregistered`. Máy chủ ghi `last_seen_at` (tối đa mỗi phút một lần).
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
- `cursor` là chuỗi **mờ, có chữ ký HMAC** (khóa `AG_SYNC_CURSOR_KEY`), gắn với tenant + thiết bị + phạm vi. Sửa, cắt, hoặc dùng con trỏ của thiết bị khác → `400 sync.cursor_invalid`; máy pull lại không có `cursor`. Không có `cursor` = từ đầu.
- Các entity chia đều hạn mức trang theo vòng (round-robin); phần entity dùng không hết chia lại cho entity còn dữ liệu. `has_more = true` có thể theo sau bởi một trang rỗng `has_more = false`.
- `reset = true`: phạm vi của người gọi đã đổi (điều chuyển, sáp nhập xã) so với lúc phát con trỏ. Trang này bắt đầu lại từ đầu; máy thay toàn bộ dữ liệu tham chiếu bằng kết quả của lượt mới.
- Phạm vi rỗng (không có đơn vị công tác, tổ chức bị vô hiệu hóa) → `changes: []`, `has_more: false`.
- `pubkey` (do pf/sync cung cấp từ kho định danh): `data = {ref, area_code, public_key}` (base64 32 byte). Kho không có cursor cho khóa, nên mỗi khi tập khóa của phạm vi đổi, máy chủ gửi lại **toàn bộ** tập (một "lượt"); mọi khóa của lượt mang cùng `version`. Lượt kết thúc bằng bản ghi đánh dấu `{entity: "pubkey", id: "00000000-0000-0000-0000-000000000000", op: "tombstone", version: V}`: máy **xóa mọi pubkey có version < V** (hộ đã xóa định danh hoặc ra khỏi địa bàn). Phạm vi toàn quốc (`All`) không nhận khóa. Người gọi không có vai `vault_pubkey_sync` (hoặc vai đọc khóa khác của kho) thì không nhận khóa, các entity khác vẫn đồng bộ.

- Dữ liệu tham chiếu theo **phạm vi phân công** của người gọi (`party/port.ScopeResolver`):
  - danh mục dùng chung, đơn vị hành chính;
  - HTX, nông hộ (chỉ nhãn hiển thị tối thiểu, **không** họ tên đầy đủ, SĐT, CCCD);
  - thửa, nhiệm vụ được giao, quy trình kỹ thuật;
  - khóa công khai của hộ thuộc địa bàn.
- `entity` G1: `catalog_item`, `admin_unit`, `cooperative`, `household`, `plot`, `task`, `directive`, `document`, `pubkey`.
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
| `geometry` | có | GeoJSON Polygon/MultiPolygon WGS84 (EPSG:4326); ≥ 4 đỉnh phân biệt không thẳng hàng; vòng chưa khép được tự khép |
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
