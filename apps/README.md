# Ứng dụng giao diện (G1)

| Thư mục | Công nghệ | Nội dung | Cổng |
|---|---|---|---|
| `web-ops/` | Next.js, TypeScript | Web vận hành: khuyến nông (TW/Tỉnh/Xã), Bản đồ CGH cho Cục, quản trị. Gồm cả bộ mô phỏng chọn hub (SIM, G2 — trang `/mo-phong`, mã `src/sim/`; bản đồ/GIS `src/gis/`), dạng web responsive, không app hay thư mục riêng ở V1 | G1, G2 |
| `web-internal/` | Next.js, TypeScript | Web nội bộ vùng MKG: hub/kho, cân & đối soát, lệnh gom — chỉ gọi `api-internal`, client OIDC `agrigreen-web-internal`, cổng 3001 | G3 |
| `field-app/` | Flutter | App hiện trường ngoại tuyến 24–48 giờ: SQLCipher, mã hóa ảnh theo hộ, UUIDv7 + idem_key | G1 |
| `htx-app/` | Flutter | App Hợp tác xã (nông dân, quản lý HTX): mở vụ, nhật ký, sản lượng, hỗ trợ — ngoại tuyến, lõi `packages/agrigreen_mobile` | G2 |
| `mkg-field-app/` | Flutter | App hiện trường MKG (tổ gom `field_collector`, giám sát gom `field_supervisor`): lệnh gom, đếm cuộn bale → gather → boat_load (FM-03/04/09), bản ghi bù, ca tổ gom, duyệt lệnh, đối soát chiều mua — ngoại tuyến 72 giờ + PIN, SQLCipher; đồng bộ với api-internal vùng mkg (`/v1/fieldops/sync/push|pull`), không qua pf/sync | G3 |

Nguyên tắc chung:
- Giao diện dựng theo các màn hình trong prototype (`App_Khuyen_Nong_v5.0.html`, `Mekong_Green_GIS_v1.3.html`, `App Hợp tác xã_v5.1.html`) và prototype `mekong-green`.
- Mọi quyền nghiệp vụ kiểm ở backend (R-55). Client chỉ ẩn hoặc hiện theo vai.
- Đăng nhập bằng OIDC PKCE qua Keycloak, mỗi app một public client: `agrigreen-web-ops` (web-ops), `agrigreen-web-internal` (web-internal), `agrigreen-field-app` (field-app), `agrigreen-htx-app` (htx-app), `agrigreen-mkg-field-app` (mkg-field-app).
- Web: biến `NEXT_PUBLIC_*` nhúng lúc build. Bản build production (`NEXT_PUBLIC_DEPLOY_ENV` mặc định `production`) bắt buộc đủ URL https và cấm `NEXT_PUBLIC_API_MOCK=1`; bản demo giả lập dùng `npm run build:mock` (đặt `NEXT_PUBLIC_DEPLOY_ENV=demo`); máy cục bộ/Compose dùng `NEXT_PUBLIC_DEPLOY_ENV=dev` (`src/deployEnv.ts`).
- App di động dùng chung lõi `packages/agrigreen_mobile`; lệnh xếp hàng ngoại tuyến của vùng mkg (`/v1/fieldops`, `/v1/logistics`) do `mkg-field-app` tự khai báo và đăng ký (`MkgActionKind`), lõi chỉ chứa lệnh của Application API nền tảng.
