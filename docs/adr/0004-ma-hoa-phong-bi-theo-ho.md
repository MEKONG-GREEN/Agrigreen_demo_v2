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
