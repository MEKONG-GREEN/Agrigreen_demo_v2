## BF-ACC-KN-MENU — Gỡ menu tài khoản native cũ khỏi Admin KN (#120)

Con của #114 BF-ACC-VERIFY (TT-13, PRD `kn-bugfix-brd-alignment`). Làn KN.

| Mục | Quyết định | Bằng chứng |
|---|---|---|
| Nguồn menu | Bỏ mục `admin.qt.cong_cu.tai_khoan` ("Tài khoản cán bộ (G1)", route `/quan-tri/tai-khoan`) khỏi nhóm "Công cụ nền tảng (tạm)" trong `internal/kn/extension/menus/base.json`. Các mục công cụ khác (Tham số, Cây tổ chức, Mẫu xuất, Xung đột…) giữ nguyên | `TestAdminMenuDropsNativeAccountLink` |
| Đường mới | Admin KN quản lý người dùng qua "Quản trị hệ thống › Người dùng" (`/kn/quan-tri/nguoi-dung`, đã có). Đường `/kn/*` mở theo menu ngữ cảnh (`menuAllows`) nên vẫn vào được | `shell.test.tsx`, `kn_integration_test.go` (GET `/v1/menu` Admin) |
| Màn native | Không xóa màn/API `/quan-tri/tai-khoan`. Đường ngoài `/kn` vẫn mở theo vai realm (`nav.ts`: `platform_admin`, `account_admin`, `account_admin_province`) và máy chủ vẫn kiểm quyền/MFA. Không thêm `account_admin` vào `kn_tinh` | `nav.test.tsx`, #114 |
| Tệp sinh | `node scripts/gen-module-menus.mjs` (`menu_departments.go`, `kn.ts`), ca vàng `kn-menu.json` (`AG_UPDATE_WEB_FIXTURES=1 … TestWebMenuFixture`, PG tạm), ảnh chụp `compat-menus.json` theo quy trình KNSPLIT-2-MENU-KN. Ảnh chụp đổi mã băm ở mọi ca vì MENU-V7 chỉ đổi chuỗi phiên bản mà không băm lại; nội dung chỉ khác ở 12 ca Admin (mất đúng mục này) | `TestBuilderMatchesCompatibilitySnapshot`, `TestBuilderMatchesWebGoldenFixture` |
| Phiên bản | Giữ `kn.menu/v7`. Quy ước trong `party/domain/menu.go` nói đổi menu thì tăng phiên bản; thực tế các gói W đổi nội dung rồi tăng gộp (#146, người dùng duyệt). Chưa tăng ở gói này, chờ Tech Lead quyết | — |
