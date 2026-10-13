# ADR-0041: MFA theo nhóm cho cán bộ KN, phiên giữ theo D9, realm quản trị riêng, vô hiệu tài khoản 45 ngày

**Trạng thái:** Chấp nhận — quyết định CEO D24 (13/10/2026, theo khuyến nghị của nhóm phát triển) và D25; hiện thực ở gói MFA (W10.03, W10.05; pf 20261013134254, pf 20261013142347) · **Ngày:** 13/10/2026 · **Trả lời:** QĐ-4 phương án (c), Q-016…Q-019 ([đối chiếu BRD KN](../kn/doi-chieu-brd-kn-2026-10.md)); QĐ-3 phần "realm quản trị riêng" · **Ứng với:** v4.3 ADR-23, P-1…P-5 · **Sửa đổi:** [ADR-0010](0010-tai-khoan-nong-dan-kich-hoat.md) (MFA không còn chỉ cho vai [MFA]) · **Thành phần:** `deploy/compose/keycloak/agrigreen-realm.json`, `deploy/compose/keycloak/agrigreen-kn-admin-realm.json`, `internal/platform/accounts/{port,app,keycloak,store}`, `internal/platform/notify` (báo trước), `cmd/worker` (job `accounts_dormant`), `apps/web-ops` (quản trị tài khoản)

## Bối cảnh

- Cấu hình đăng nhập KN đã chốt với TTKN An Giang ở D9 (KN v7.7 N-09): phiên Web 4 giờ, app 24 giờ + ngoại tuyến 72 giờ, mật khẩu 12 ký tự/không trùng 5, MFA (TOTP) chỉ cho vai `[MFA]` ở Admin API. Kiểm thử `cmd/api-platform/realm_test.go` khóa các giá trị này.
- Kiến trúc v4.3 (P-1, P-3, P-4, P-5; ADR-23) đòi: MFA cho mọi người dùng không phải nông hộ; mật khẩu ≥ 14 ký tự, quản trị đổi 2 tháng, không trùng 10; vô hiệu sau 45 ngày; khóa phiên 5/15/5 phút — hoặc giữ giá trị khác kèm đánh giá rủi ro.
- Keycloak đặt chính sách mật khẩu **theo realm**, không theo vai.

## Quyết định (D24, D25)

1. **TOTP bắt buộc khi đăng nhập cho cán bộ KN từ G1** (và cho mọi vai `[MFA]`). Đăng ký ngay ở lần đăng nhập đầu.
   - Realm: vai đánh dấu `login_totp_required`. 17 vai trong `accounts/port.LoginTOTPRoles` (`kn_user`, `kn_national_admin`, `kn_province_admin`, `kn_officer` + 13 vai `[MFA]`) là vai tổng hợp chứa nó. Thêm vai vào nhóm = thêm composite, không sửa luồng.
   - Luồng browser `agrigreen browser`: mật khẩu → nhánh CONDITIONAL *Condition - user role = login_totp_required* → *OTP Form* REQUIRED. Chưa có TOTP thì Keycloak tự thêm *Configure OTP*. Nhánh thứ hai (vai đánh dấu phủ định + *Condition - user configured*) giữ hành vi cũ cho người tự cấu hình TOTP, không hỏi OTP hai lần.
   - `pf/accounts`: tạo tài khoản / cấp vai có vai trong nhóm → thêm `CONFIGURE_TOTP` vào liên kết kích hoạt (`NeedsLoginTOTP`). `MFARoles`/`NeedsMFA` giữ nghĩa cũ: vai mà Admin API kiểm `amr`.
   - Vai nông hộ, HTX, tài khoản dịch vụ **không** thuộc nhóm. App hiện trường: cán bộ KN nhập TOTP khi đăng nhập trực tuyến; thiết bị + PIN là hai yếu tố là W10.04.
2. **Phiên giữ 4 giờ / 24 giờ (D9)** kèm [đánh giá rủi ro](../security/danh-gia-rui-ro-phien-4h-24h.md). `realm_test.go` khóa **đúng** giá trị (trước chỉ khóa cận dưới).
3. **Mật khẩu 14 ký tự + độ phức tạp + không trùng 10 + đổi 60 ngày chỉ ở realm quản trị riêng** `agrigreen-kn-admin` (D25: realm quản trị riêng từng hệ thống thông tin; với hệ KN là realm này). Realm này bắt OTP mọi tài khoản, phiên 5 phút/8 giờ. Realm `agrigreen` giữ 12/5.
4. **Vô hiệu tài khoản không hoạt động 45 ngày, báo trước 7 ngày (ngày 38).**
   - Job `accounts_dormant` (worker, leader, 02:00 hằng ngày).
   - Mốc hoạt động là muộn nhất của: LOGIN của Keycloak, phiên gần nhất, lần tạo/mở khóa ở sổ `account_event`, lúc tạo.
   - Ngày 38: sổ ghi `dormant_warned`, phát `pf.accounts.user.dormant_warning` → `pf/notify` gửi thông báo khẩn + email cho chính tài khoản. Mỗi mốc chỉ báo một lần.
   - Ngày 45: Keycloak `enabled=false`, thu hồi phiên, sổ ghi `locked` với `lock_cause = dormant_45d`, actor là tác nhân hệ thống. Admin mở như mọi lần khóa.
   - Ngưỡng là tham số có phiên bản (`accounts.dormant_warn_days`, `accounts.dormant_disable_days`). Có chế độ chạy thử `AG_ACCOUNTS_DORMANT_DRY_RUN`.
   - **Nông hộ tự kích hoạt lại bằng OTP:** chưa làm, chờ kênh SMS (W10.06). Tới lúc đó tài khoản nông dân không vào job.
5. **Đặt lại TOTP có lý do** (`POST /v1/accounts/users/{id}/reset-totp`, lý do ≥ 20 ký tự):
   - xóa TOTP, thêm `CONFIGURE_TOTP`, thu hồi phiên, sổ ghi `totp_reset`;
   - cùng ma trận cấp vai/phạm vi như khóa, không tự đặt lại của mình;
   - màn quản trị hiện trạng thái `mfa` (bắt buộc / đã đăng ký / chờ đăng ký).

## Hệ quả

- Realm đã nhập ở môi trường thật không tự cập nhật: bước thủ công ở [runbook keycloak-mfa-d24](../runbooks/keycloak-mfa-d24.md).
- Admin API chưa nhận issuer của realm quản trị riêng. Ở G1 quản trị viên vẫn dùng realm `agrigreen`, giờ cũng bị bắt TOTP lúc đăng nhập. Chuyển hẳn sang realm riêng là W10.01/W10.03 sau G1.
- Chưa làm (W10.03 còn lại): `RequireMFA` cho Application API theo loại tài khoản, claim `acr`, passkey/WebAuthn, xác nhận đẩy.

## Hiện thực

Gói MFA (13/10/2026):

- realm dev + kiểm thử `TestRealmLoginTOTPByRole_D24`, `TestAdminRealmPasswordPolicy_D24`, phiên khóa đúng giá trị trong `TestKeycloakRealmCoversRolesAndClaims`;
- `accounts` (`app/dormant.go`, `app/mfa.go`, `keycloak/otp.go`, `store/dormant.go`), kiểm thử `TestDormantJobWarnsAt38AndDisablesAt45_D24`, `TestResetTOTPWithReason_D24`, `TestDormantPolicy_D24`;
- `notify` (`domain/accounts.go`, kiểm thử `TestDormantWarningToAccountItself_D24`);
- web-ops `/quan-tri/tai-khoan` và Admin KN `/kn/quan-tri/nguoi-dung/{id}`.
