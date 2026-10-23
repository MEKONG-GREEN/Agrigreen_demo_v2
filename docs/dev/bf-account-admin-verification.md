# TT-13: kiểm quyền tài khoản cán bộ xã

BF-ACC-VERIFY (#114), theo PRD kn-bugfix-brd-alignment ngày23/10/2026.

Phần server hiện đã có guard quản trị. Gói này bổ sung regression, không đổi vai, quyền native, cấu hình realm, API hay schema:

- Vai nghiệp vụ KN và các vai realm đầu vào KN không có quyền cấp tài khoản. Đồ thị composite role của realm không được dẫn tới platform_admin, account_admin hoặc account_admin_province.
- Tạo tài khoản Cấp Tỉnh qua API KN không được tự gắn vai quản trị tài khoản. Chọn phiên kn_tinh không thêm quyền realm vào Principal.
- Cán bộ tỉnh có MFA và phiên hợp lệ nhận403 khi tạo tài khoản xã, gán/sửa/thu hồi lần gán hoặc sửa hồ sơ. Sổ account_event, role_assignment_event, bảng chiếu role_assignment_current và danh tính trong Directory phải giữ nguyên sau các lần bị từ chối.
- Quản trị hợp lệ vẫn đọc/quản lý như trước; thiếu MFA vẫn403. Các quyền native được cấp rõ ràng không bị cắt chỉ vì một người có thêm vai nghiệp vụ ở ứng dụng khác.

## Menu KN còn chờ owner

Route /quan-tri/tai-khoan vẫn có trong nguồn JSON KN internal/kn/extension/menus/base.json. Thay đổi nguồn đó và sinh lại menu thuộc ticket con [#120](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/120), phối hợp #76/#98. Không sửa tay menu_departments.go, không xóa route/API quản trị native, không nhận là toàn #114 xong khi menu KN chưa bỏ liên kết cũ.

Không thêm điểm ghép KN vào production shared; không đổi fail-closed hay Principal để làm test đạt. Kiểm HTTP dùng phiên do server tạo và JWT thử nghiệm hợp lệ, cùng PostgreSQL thật. Kết quả thực tế và commit ghi trên issue trước ready/review.
