# Hợp đồng đồng bộ ngoại tuyến (mục 15, ADR-0006)

G1 đồng bộ qua **Application API** (pull/push có cursor và idempotency). Cách này không phụ thuộc engine logical replication, nên không bị chặn bởi câu hỏi 23.1. Máy chủ hiện thực trong module `internal/platform/sync`; app Flutter ở `apps/field-app`.

## Đăng ký thiết bị

`POST /v1/devices` (Idempotency-Key) → `201 {device_id, registered_at}`

Thân: `{ "device_id": "<UUIDv7 sinh trên máy>", "platform": "android", "app_version": "…" }`

- Thiết bị bị **thu hồi**: mọi lời gọi sync trả `403` với `code = sync.device_revoked`. App **xóa dữ liệu cục bộ** và khóa token ngay (mục 15).
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
  "server_time": "2026-12-03T02:10:00Z"
}
```

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
  - `consent`: đồng ý theo mục đích;
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

- `duplicate`: `idem_key` đã ghi. Máy coi là thành công và xóa khỏi hàng đợi.
- `conflict`: hai bản ghi cùng thực thể, ví dụ hai ranh giới cho cùng một thửa. Máy chủ **giữ cả hai**, đánh dấu chờ xử lý, có người phụ trách và SLA (C-8, tạm 72 giờ). Máy coi là đã gửi.
- `rejected`: dữ liệu sai. Máy giữ bản ghi, hiển thị lỗi để người dùng tạo bản ghi bù.
- Máy xóa bản ghi cục bộ sau khi máy chủ xác nhận **+ 48 giờ**.

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