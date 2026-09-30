# Ứng dụng giao diện (G1)

| Thư mục | Công nghệ | Nội dung | Cổng |
|---|---|---|---|
| `web-ops/` | Next.js, TypeScript | Web vận hành: khuyến nông (TW/Tỉnh/Xã), Bản đồ CGH cho Cục, quản trị | G1 |
| `web-internal/` | Next.js, TypeScript | Web nội bộ vùng MKG: hub/kho, cân & đối soát, lệnh gom — chỉ gọi `api-internal`, client OIDC `agrigreen-web-internal`, cổng 3001 | G3 |
| `web-sim/` | Next.js, TypeScript | Bộ mô phỏng chọn hub, dạng web responsive, không app riêng ở V1 | G2 |
| `field-app/` | Flutter | App hiện trường ngoại tuyến 24–48 giờ: SQLCipher, mã hóa ảnh theo hộ, UUIDv7 + idem_key | G1 |
| `htx-app/` | Flutter | App Hợp tác xã (nông dân, quản lý HTX): mở vụ, nhật ký, sản lượng, hỗ trợ — ngoại tuyến, lõi `packages/agrigreen_mobile` | G2 |

Nguyên tắc chung:
- Giao diện dựng theo các màn hình trong prototype (`App_Khuyen_Nong_v5.0.html`, `Mekong_Green_GIS_v1.3.html`, `App Hợp tác xã_v5.1.html`) và prototype `mekong-green`.
- Mọi quyền nghiệp vụ kiểm ở backend (R-55). Client chỉ ẩn hoặc hiện theo vai.
- Đăng nhập bằng OIDC PKCE qua Keycloak (client `agrigreen-web-ops`, `agrigreen-web-internal`, `agrigreen-field-app`, `agrigreen-htx-app`).
