# Neo chuỗi hằng ngày

Mục 8.2 và mục 22 của tài liệu kiến trúc. Job `anchor` trong `cmd/worker`.

## Làm gì

Lúc 00:30 giờ Việt Nam, với **mỗi cụm**:

1. `SELECT shared.anchor_chains(<ngày>)`: chép đầu chuỗi (`last_seq`, `last_hash`) của mọi `(sổ, tenant, stream)` trong `shared.chain_head` vào `shared.chain_anchor`. Bảng này chỉ ghi thêm, có trigger chặn sửa/xóa.
2. Đọc lại các neo của ngày đó bằng `shared.chain_anchors_of(<ngày>)`.
3. Ký manifest bằng Ed25519 và ghi vào kho đối tượng tại `anchors/<cụm>/<YYYY-MM-DD>.json`. Kho ghi một lần, không ghi đè, không có Delete.

`<ngày>` là ngày nghiệp vụ vừa kết thúc (`opsjobs.AnchorDateFor`). Chạy lúc 00:30 ngày 27 thì neo ngày 26. Neo phản ánh đầu chuỗi **tại lúc chạy**, nên có cả các dòng ghi trong khoảng 00:00–00:30.

Job idempotent. Manifest đã có thì bỏ qua. Nếu lần trước ghi neo xong mà xuất manifest lỗi, lần sau ghi bổ sung neo còn thiếu rồi xuất. Worker khởi động (hoặc đổi leader) cũng chạy một lần để bắt kịp ngày bị lỡ.

## Cấu hình

| Biến | Ý nghĩa |
|---|---|
| `AG_ANCHOR_SIGNING_KEY` | Seed Ed25519 32 byte (hoặc khóa riêng 64 byte), base64 chuẩn. Lấy từ KMS/bí mật **trong lãnh thổ**. Thiếu biến này thì worker không khởi động (trừ khi `-jobs` không có `anchor`). |
| `AG_ANCHOR_KEY_ID` | Tùy chọn. Mặc định là 16 hex đầu của SHA-256 khóa công khai. |
| `AG_S3_ENDPOINT`, `AG_S3_BUCKET`, `AG_S3_ACCESS_KEY`, `AG_S3_SECRET_KEY`, `AG_S3_USE_TLS` | Kho S3 trong lãnh thổ (RB-7). Bucket bật versioning + object lock (COMPLIANCE). Credential chỉ có `s3:PutObject`, `s3:GetObject`. |
| `AG_OBJSTORE_DIR` | Chỉ Local/Dev: thư mục thay cho S3. |

Sinh khóa mới (máy quản trị, không lưu ra đĩa):

```bash
openssl genpkey -algorithm ed25519 -outform DER | tail -c 32 | base64
```

Công bố khóa công khai cho kiểm toán qua kênh riêng. **Không tin khóa ghi trong tệp.** Manifest chỉ ghi `key_id`.

## Định dạng manifest

```json
{
  "payload": { "format": "ag.chain-anchor.v1", "cluster": "mkg", "business_date": "2026-09-26",
               "key_id": "…", "count": 1,
               "entries": [{ "ledger": "mkg_fieldops.roll_count", "tenant_id": "…", "stream_key": "lot:…",
                             "last_seq": 3, "last_hash": "<hex>", "anchored_at": "…Z" }] },
  "alg": "Ed25519",
  "key_id": "…",
  "signature": "<base64>"
}
```

Chữ ký phủ dạng JSON gọn (compact) của `payload`. Kiểm bằng `opsjobs.VerifyManifest(tệp, khóa_công_khai)`. Manifest không chứa dữ liệu cá nhân: chỉ có mã tham chiếu và băm. `stream_key` theo quy ước là mã tham chiếu (vd `lot:<uuid>`), **không được** chứa họ tên, số điện thoại.

## Kiểm một ngày

```sql
-- trên cụm cần kiểm, bằng ag_migrator
SELECT count(*) FROM shared.chain_anchor WHERE business_date = '2026-09-26';
```

So từng `(ledger, tenant_id, stream_key, last_seq, last_hash)` trong manifest với `shared.chain_anchor`, rồi với dòng `seq = last_seq` của bảng sổ. Có bất kỳ khác biệt nào thì làm theo [verify-chains.md](verify-chains.md).

## Khi job lỗi

- Log `event=job.run job=anchor outcome=error`. Metric `ag.job.runs{job="anchor",outcome="error"}`.
- `permission denied for function anchor_chains`: chưa áp migration `shared/000002`. Chạy `migrate up`.
- `objstore: ErrExists`: đã có manifest khác nội dung dưới cùng khóa. **Không** xóa. Đây là dấu hiệu sự cố (P1). Giữ nguyên hai bản, báo an ninh.
- Lỡ nhiều ngày thì worker chỉ tự bắt kịp ngày gần nhất. Neo bù từng ngày bằng `AG_ANCHOR_DATE=YYYY-MM-DD migrate anchor` (cùng hàm `opsjobs.AnchorDaily`, vai `ag_ops_jobs`, cần `AG_DB_URL`, `AG_ANCHOR_SIGNING_KEY`, `AG_S3_*`), hoặc một bản worker tạm `-modules=none -jobs=anchor`. Diễn tập phục hồi cũng dùng `migrate anchor` để neo ngay trước sao lưu ([restore-drill.md](restore-drill.md)). Không thể neo "quá khứ" chính xác: neo bù phản ánh đầu chuỗi lúc chạy. Ghi chú việc này vào nhật ký vận hành.
