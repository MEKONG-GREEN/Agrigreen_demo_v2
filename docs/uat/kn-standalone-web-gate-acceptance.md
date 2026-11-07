# KN độc lập — nghiệm thu S-2 web gate (#263)

Căn cứ BRD 2.0: Báo giá v2.0 FN-CH-00.01 B3–B4, BR-04, AC-02/03; kế hoạch KN độc lập S-2 đã duyệt 07/11/2026. Đây là gate hiển thị của phiên client; quyền API vẫn được backend kiểm riêng.

| Ca | Thao tác | Kết quả cần đạt |
|---|---|---|
| S2-01 | Admin KN/TW/Tỉnh/Xã có active context mở trực tiếp `/`, `/htx`, `/chi-dao`; tải lại | Màn không có quyền, không mount/gọi dữ liệu component legacy; không tự đưa `/` về dashboard |
| S2-02 | Nhập URL `/ban-do`, `/ban-do-gis`, `/nhiem-vu`, `/danh-ba`, `/bao-cao`, `/thu-vien`, `/htx/dashboard`, `/mrv`, đường lạ | Active KN từ chối; không phụ thuộc realm role bổ sung |
| S2-03 | Nhập `/login-evil`, `/chon-vai-tro-evil`, `/kn-evil` | Không bypass nhờ tiền tố; shell giữ kiểm hiển thị |
| S2-04 | KN mở `/login`, nhánh callback/login, `/chon-vai-tro`, `/thong-bao`, `/khong-co-quyen` | Utility đúng allowlist vẫn dùng được; nhánh KN phải được menu active cho phép |
| S2-05 | Đăng nhập tài khoản nhiều vai với returnTo legacy; chọn Tỉnh, đổi sang Xã | Về trang mặc định đúng vai; URL legacy vẫn bị từ chối sau đổi vai |
| S2-06 | Cùng principal có realm role KN + HTX/MKG, đổi active KN ↔ legacy | KN chặn legacy; phiên legacy tính quyền hiện hữu, không khóa chỉ vì có realm role KN |
| S2-07 | Phiên HTX mở bảng điều hành sản xuất/phê duyệt; platform_admin legacy mở chỉ đạo; Cục mở báo cáo/CGH | Các quyền đã có vẫn hoạt động; không xóa màn dùng chung |
| S2-08 | Chín nav entries ngoài BRD | Không còn cấp TW/Tỉnh/Xã; cấu hình legacy cũ không nối menu trở lại |

Tự động: `src/kn/StandaloneWebGate.test.tsx`, các test nav/shell và `tests/e2e/kn-standalone-web-gate.spec.ts` trong `apps/web-ops`. Backend/menu parity: từ gốc repo dùng pg-ephemeral với `./tests/kn77 ./internal/kn/extension/menu/...`. Các cổng tĩnh, web đầy đủ và CI đúng SHA vẫn bắt buộc trước gộp; kết quả local không thay bằng chứng CI.
