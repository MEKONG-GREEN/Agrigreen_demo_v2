## MFA — (CEO 13/10, D24/D25) TOTP cho cán bộ KN, phiên 4 giờ/24 giờ, realm quản trị riêng, vô hiệu 45 ngày (ADR-0041, pf 20261013134254, pf 20261013142347)

Quyết định CEO 13/10/2026 (D24, theo khuyến nghị nhóm phát triển): TOTP bắt buộc cho cán bộ KN từ G1; phiên giữ 4 giờ/24 giờ (D9) kèm đánh giá rủi ro; mật khẩu 14 ký tự + độ phức tạp + không trùng 10 chỉ ở realm quản trị riêng; vô hiệu sau 45 ngày không hoạt động, báo trước 7 ngày; nông hộ tự kích hoạt lại bằng OTP (ngoài phạm vi tới khi có SMS). D25: realm quản trị riêng từng hệ thống thông tin.

| Câu hỏi / mục | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| "Cán bộ KN" gồm vai nào | `kn_user` (cổng KN v7.7), `kn_national_admin`, `kn_province_admin`, `kn_officer`; cộng mọi vai `[MFA]` (quản trị) | `accounts/port.KNOfficerRoles`, `LoginTOTPRoles`; realm (vai tổng hợp chứa `login_totp_required`) | Thêm/bớt vai ở `LoginTOTPRoles` + composite realm (kiểm thử giữ khớp); realm đã nhập: runbook mục 3.1 |
| HTX (`coop_manager`), Cục KTHT (`cgh_viewer`) | **Không** bắt TOTP (D24 chỉ nói cán bộ KN; v4.3 P-1 đòi cả HTX, Cục KTHT — Q-017 phần còn lại) | như trên | Thêm vai vào `LoginTOTPRoles` |
| App hiện trường | Cán bộ KN cũng nhập TOTP khi đăng nhập trực tuyến (cùng luồng browser); PIN ngoại tuyến giữ nguyên | realm `browserFlow` | W10.04 (thiết bị + PIN = hai yếu tố) có thể gắn luồng riêng cho client app |
| Phiên quản trị ở realm riêng | 5 phút không thao tác, tối đa 8 giờ (v4.3 P-5, Q-016 đề xuất) — D24 không nói | `agrigreen-kn-admin-realm.json` | Sửa realm + `TestAdminRealmPasswordPolicy_D24` (trần 15 phút/8 giờ) |
| Realm quản trị dùng cho gì ở G1 | Đã dựng (chính sách, OTP, client `agrigreen-kn-admin-web`); Admin API **chưa** nhận issuer này — quản trị viên vẫn ở realm `agrigreen` (có TOTP) | ADR-0041 Hệ quả | W10.01/W10.03: thêm issuer thứ hai cho Admin API, chuyển tài khoản quản trị |
| "Hoạt động" để tính 45 ngày | Muộn nhất của: LOGIN Keycloak, phiên gần nhất, tạo/mở khóa ở `account_event`, lúc tạo | `app/dormant.go` `lastActivity` | Bật thêm `REFRESH_TOKEN` ở `enabledEventTypes` nếu cần tính cả làm mới token |
| Ngưỡng | 38 / 45 ngày, tham số có phiên bản `accounts.dormant_warn_days`, `accounts.dormant_disable_days`; sai miền → mặc định | `masterdata/seeddata/params.json`, `app.dormantPolicy` | Quy trình tham số (người đặt ≠ người duyệt) |
| Nông hộ | **Không** vào job tới W10.06 (tự kích hoạt lại bằng OTP cần SMS) | `store.DormantCandidates` | Bỏ điều kiện loại `farmer` khi có W10.06 |
| Kênh báo trước | Thông báo cá nhân khẩn trong ứng dụng + email (`pf.notify.accounts`, kind `account_dormant`) | `notify/domain/accounts.go` | SMS khi có W10.06 |
| Tài khoản tạo ngoài nền tảng (console) | Không vào job (không có ở `pf_accounts.account`) | runbook mục 5.8 | Nhập vào `pf/accounts` |
| Đặt lại TOTP khi tài khoản không bắt TOTP và chưa có | 409 `accounts.state_conflict` | `app/mfa.go` | — |
