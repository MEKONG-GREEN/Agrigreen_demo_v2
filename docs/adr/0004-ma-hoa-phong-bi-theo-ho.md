# ADR-0004: Mã hóa phong bì X25519 theo hộ

**Trạng thái:** Chấp nhận — hiện thực ở G1 · **Ngày:** 27/09/2026

## Quyết định
- Mỗi hộ một cặp khóa X25519.
  - Khóa công khai đồng bộ xuống thiết bị như dữ liệu tham chiếu.
  - Khóa bí mật nằm trong `vault`, được bọc bởi KMS trong lãnh thổ.
- Ảnh và tệp: AES-256-GCM với DEK ngẫu nhiên; DEK được seal bằng khóa công khai của hộ (mục 10.2).
- Xóa hộ = hủy khóa bí mật. Mọi ảnh của hộ, kể cả trong bản sao lưu, không còn đọc được (RB-3).
- Tra cứu theo SĐT hoặc họ tên (App HTX, CGH, KN) dùng **blind index** HMAC-SHA256 với khóa riêng trong vault. Mã tham chiếu không suy ra từ thuộc tính định danh (R-41).

## Trạng thái G0
Đã có cụm `ag_vault`, vai `vault_app` và `cmd/identity-vault` (health). Bảng và API ở mục 10.3 làm ở G1.

## Hiện thực G1
- Migration `migrations/vault/000002_vault_identity.up.sql`:
  - `vault.subject` (dấu mộ, `erased_at`), `vault.identity` (bản mã), `vault.subject_key` (X25519 + khóa dữ liệu, bọc KEK), `vault.blind_index`: **không phải sổ**, xóa được theo RB-3;
  - `vault.access_log`: sổ chỉ ghi thêm (mẫu 8.1), chỉ chứa mã tham chiếu, mục đích, người yêu cầu, kết quả.
- Trường định danh mã hóa AES-256-GCM bằng khóa dữ liệu riêng của chủ thể, AAD = tenant/ref/tên trường. Khóa dữ liệu và khóa bí mật X25519 bọc bằng KEK qua `internal/vault/identity/kms` (dev: `AG_VAULT_KEK`; production cắm KMS/HSM trong lãnh thổ).
- Chỉ mục mù: HMAC-SHA256 bằng `AG_VAULT_BLIND_KEY` (khóa riêng, khác KEK), đầu vào gồm tenant nên không liên kết chéo tenant. SĐT chuẩn hóa về `84xxxxxxxxx`.
- API đổi tên từ `/v1/households…` thành `/v1/subjects…` vì chủ thể gồm cả người (đại diện HTX, chủ máy). Xem `contracts/openapi/identity-vault.yaml`.
- Phong bì mã hóa: `internal/shared/envelope`, vector cố định `internal/shared/envelope/testdata/vectors.json`.
