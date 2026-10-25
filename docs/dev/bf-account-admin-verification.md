# TT-13: kiểm quyền tài khoản cán bộ xã

BF-ACC-VERIFY (#114), theo PRD kn-bugfix-brd-alignment ngày 23/10/2026.

Phần server hiện đã có guard quản trị. Gói này bổ sung regression, không đổi vai, quyền native, cấu hình realm, API hay schema:

- Vai nghiệp vụ KN và các vai realm đầu vào KN không có quyền cấp tài khoản. Đồ thị composite role của realm không được dẫn tới platform_admin, account_admin hoặc account_admin_province.
- Tạo tài khoản Cấp Tỉnh qua API KN không được tự gắn vai quản trị tài khoản. Chọn phiên kn_tinh không thêm quyền realm vào Principal.
- Cán bộ tỉnh có MFA và phiên hợp lệ nhận 403 khi tạo tài khoản xã, gán/sửa/thu hồi lần gán hoặc sửa hồ sơ. Sổ account_event, role_assignment_event, bảng chiếu role_assignment_current và danh tính trong Directory phải giữ nguyên sau các lần bị từ chối.
- Quản trị hợp lệ vẫn đọc/quản lý như trước; thiếu MFA vẫn 403. Các quyền native được cấp rõ ràng không bị cắt chỉ vì một người có thêm vai nghiệp vụ ở ứng dụng khác.

## Menu KN (#120)

Phần menu thuộc ticket con [#120](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/120) BF-ACC-KN-MENU (gộp trước gói này): bỏ mục "Công cụ nền tảng (tạm) › Tài khoản cán bộ (G1)" (`/quan-tri/tai-khoan`) khỏi nguồn `internal/kn/extension/menus/base.json`, sinh lại menu/ca vàng/ảnh chụp. Admin KN quản lý người dùng qua `/kn/quan-tri/nguoi-dung`. Màn/API quản trị tài khoản native giữ nguyên, chỉ mở theo vai realm (`platform_admin`, `account_admin`, `account_admin_province`). Xem [giả định BF-ACC-KN-MENU](../kn/gia-dinh/BF-ACC-KN-MENU.md).

Không thêm điểm ghép KN vào production shared; không đổi fail-closed hay Principal để làm test đạt. Kiểm HTTP dùng phiên do server tạo và JWT thử nghiệm hợp lệ, cùng PostgreSQL thật. Kết quả thực tế và commit ghi trên issue trước ready/review.
