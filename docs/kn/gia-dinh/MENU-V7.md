## MENU-V7 — Nâng phiên bản menu KN lên `kn.menu/v7` (#146)

- **Lý do:** chồng gói W (#26, #28, #17, #30, #20, #27, #35, #36) gộp vào dev 25/10/2026 đã đổi nội dung menu (mục Nhật ký, Bản đồ vùng dịch, Mẫu văn bản, Trang chủ phòng, Quy trình văn bản, Kế hoạch nhân lực, Chiến dịch điều tra, Giá thành) nhưng giữ `kn.menu/v6`. Quy ước tại `internal/platform/party/domain/menu.go` và [KNSPLIT-2-MENU-KN](KNSPLIT-2-MENU-KN.md): đổi menu thì tăng phiên bản. Người dùng đồng ý 25/10/2026.
- **Phạm vi:** chỉ đổi chuỗi phiên bản: `domain.MenuVersion`, `menu.Version` (`internal/kn/extension/menu/catalog.go`), `MENU_VERSION` của mock web, kỳ vọng test, ca vàng `kn-menu.json` và ảnh chụp `compat-menus.json`. **Không đổi nội dung menu.**
- **Tác động:** web và app di động không lưu đệm theo phiên bản (web chỉ chép giá trị; app lưu theo khóa cố định và luôn tải lại khi có mạng), nên không cần thao tác phía người dùng.
- Chú thích lịch sử ghi `kn.menu/v6` (thời điểm #98 đưa vào cấu trúc `deptMenus`) giữ nguyên.
