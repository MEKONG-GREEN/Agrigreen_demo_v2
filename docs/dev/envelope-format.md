# Định dạng phong bì mã hóa (mục 10.2, ADR-0004)

Áp dụng cho ảnh hiện trường và tệp có dữ liệu cá nhân. Thiết bị hiện trường mã hóa **ngay khi chụp**, kể cả khi đang ngoại tuyến. Thiết bị chỉ cầm **khóa công khai** của hộ, được đồng bộ xuống như dữ liệu tham chiếu. Khóa bí mật chỉ nằm trong vault.

## Thuật toán, phiên bản 1

| Bước | Thuật toán | Ghi chú |
|---|---|---|
| DEK | 32 byte ngẫu nhiên từ CSPRNG | Mỗi tệp một DEK |
| Mã hóa nội dung | AES-256-GCM, nonce 12 byte ngẫu nhiên, không có AAD | Đầu ra = ciphertext ‖ tag (16 byte) |
| Bọc DEK | `crypto_box_seal` (libsodium sealed box: X25519 + XSalsa20-Poly1305) bằng khóa công khai X25519 của chủ thể | Go: `golang.org/x/crypto/nacl/box.SealAnonymous`. Dart: `pinenacl` `SealedBox`. Đầu ra 32 + 16 + 32 = 80 byte |
| Mở DEK | `crypto_box_seal_open` bằng cặp khóa của chủ thể — **chỉ trong vault** (`POST /v1/subjects/{ref}/unwrap`, bắt buộc có mục đích) | |

## Dạng lưu và gửi

Metadata là JSON, nội dung là nhị phân:

```json
{
  "v": 1,
  "alg": "A256GCM+X25519-SEALEDBOX",
  "subject_ref": "0191…",
  "nonce": "<base64 12 byte>",
  "wrapped_dek": "<base64 80 byte>",
  "sha256_ciphertext": "<hex>",
  "content_type": "image/jpeg",
  "captured_at": "2026-12-03T07:14:22+07:00",
  "gps": { "lat": 10.03, "lon": 105.78, "accuracy_m": 6.5 }
}
```

- **Tọa độ GPS là dữ liệu cá nhân khi gắn với hộ.** Nó chỉ nằm trong metadata của minh chứng, không đi vào log.
- Kho đối tượng chỉ lưu **ciphertext** (R-40). Tên đối tượng là `files/<tenant>/<file_ref>`, không chứa định danh.

## Bộ vector kiểm thử

`internal/shared/envelope/testdata/vectors.json` là bộ vector cố định, cả hai phía Go và Dart phải mở được:
- khóa bí mật và công khai X25519 dạng hex;
- plaintext, DEK, nonce, ciphertext, wrapped_dek.

Kiểm thử phía Dart: `apps/field-app/test/envelope_vectors_test.dart`.
