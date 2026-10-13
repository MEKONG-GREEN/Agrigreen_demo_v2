# Keycloak D24: TOTP cho cán bộ KN, realm quản trị riêng, vô hiệu tài khoản 45 ngày

Áp dụng cho môi trường **đã nhập realm** (staging, VPS, Kubernetes). Keycloak chỉ nhập `deploy/compose/keycloak/*.json` khi realm **chưa tồn tại** (`--import-realm` bỏ qua realm đã có), nên thay đổi trong tệp realm của gói MFA không tự vào môi trường đang chạy. Môi trường dev mới (xóa volume Keycloak) nhận đủ tự động.

Căn cứ: D24, D25 (CEO 13/10/2026), [ADR-0041](../adr/0041-mfa-theo-nhom-d24.md), [đánh giá rủi ro phiên](../security/danh-gia-rui-ro-phien-4h-24h.md).

## 1. Thay đổi gồm gì

| Phần | Thay đổi | Tự động? |
|---|---|---|
| Realm `agrigreen` | Vai đánh dấu `login_totp_required`; 17 vai (cán bộ KN + vai [MFA]) thành vai tổng hợp chứa nó; luồng browser `agrigreen browser` (OTP theo vai) và gắn làm Browser flow | **Thủ công** (mục 3) |
| Realm `agrigreen` — phiên, mật khẩu | **Không đổi** (4 giờ/24 giờ; 12 ký tự/không trùng 5) | — |
| Realm mới `agrigreen-kn-admin` | Mật khẩu 14 ký tự + độ phức tạp, không trùng 10, đổi 60 ngày; OTP bắt buộc; phiên 5 phút/8 giờ | Nhập tệp (mục 4) |
| DB cụm pf | `pf 20261013134254_pf_accounts_dormant_totp`, `pf 20261013142347_pf_notify_account_dormant` | `migrate up` |
| Worker | Job `accounts_dormant` (02:00 giờ VN hằng ngày, leader); consumer `pf.notify.accounts` | Cấu hình (mục 5) |
| Tham số | `accounts.dormant_warn_days` = 38, `accounts.dormant_disable_days` = 45 | Seed tham số (mục 5) |
| API / web | `POST /v1/accounts/users/{id}/reset-totp`, trạng thái `mfa` ở chi tiết tài khoản | Phát hành |

## 2. Chuẩn bị

1. Sao lưu DB Keycloak (database `keycloak`) theo [backup.md](backup.md). Xuất realm `agrigreen` để đối chiếu:
   `kc.sh export --realm agrigreen --file /tmp/agrigreen-before.json --users skip` (hoặc Realm settings → Action → Partial export).
2. Báo trước cho cán bộ KN **ít nhất 3 ngày**: cài ứng dụng xác thực (Google Authenticator, Microsoft Authenticator, FreeOTP…); lần đăng nhập đầu sau ngày X sẽ phải quét mã QR. Gửi kèm hướng dẫn một trang.
3. Chuẩn bị bàn hỗ trợ: Admin KN dùng nút **Đặt lại TOTP** (màn Chi tiết tài khoản) khi cán bộ mất/đổi điện thoại.
4. Kiểm tra `Authentication → Required actions`: **Configure OTP** đang bật (mặc định bật).

Lệnh `kcadm.sh` dưới đây chạy trong container Keycloak (VPS: xem [vps-deploy.md](vps-deploy.md) mục 5; Kubernetes: `kubectl -n agrigreen-iam exec deploy/keycloak -- /opt/keycloak/bin/kcadm.sh …`):

```sh
kc() { docker compose $C exec -T keycloak /opt/keycloak/bin/kcadm.sh "$@"; }
kc config credentials --server http://localhost:8080 --realm master --user admin --password "$KEYCLOAK_ADMIN_PASSWORD"
R=agrigreen
```

## 3. Realm `agrigreen` — TOTP theo vai

### 3.1 Vai đánh dấu và vai tổng hợp

```sh
kc create roles -r $R -s name=login_totp_required \
  -s 'description=D24 (CEO 13/10/2026): vai ĐÁNH DẤU, không cấp trực tiếp — luồng agrigreen browser bắt TOTP'
for role in kn_user kn_national_admin kn_province_admin kn_officer \
            platform_admin param_setter param_approver masterdata_admin registry_admin reports_admin \
            cgh_admin geo_admin device_admin sync_conflict_resolver vault_eraser account_admin account_admin_province; do
  kc add-roles -r $R --rname "$role" --rolename login_totp_required
done
kc get roles/login_totp_required/users -r $R | head   # rỗng — vai không gán trực tiếp
```

Danh sách 17 vai phải khớp `accounts/port.LoginTOTPRoles` (kiểm thử `TestRealmLoginTOTPByRole_D24` giữ realm dev khớp). Vai `farmer`, `coop_manager`, tài khoản dịch vụ **không** được thêm.

### 3.2 Luồng đăng nhập `agrigreen browser`

Làm trên Admin Console (`Authentication`), đặt tên **đúng** như realm dev:

1. **Create flow** `agrigreen browser` (Basic flow, top level). Thêm theo thứ tự:
   - step **Cookie** — Alternative;
   - step **Identity Provider Redirector** — Alternative;
   - sub-flow `agrigreen browser forms` (Generic) — Alternative.
2. Trong `agrigreen browser forms`:
   - step **Username Password Form** — Required;
   - sub-flow `agrigreen browser - TOTP theo vai` — **Conditional**;
   - sub-flow `agrigreen browser - TOTP da cau hinh` — **Conditional**.
3. Trong `agrigreen browser - TOTP theo vai`:
   - condition **Condition - user role** — Required; cấu hình alias `agrigreen-login-totp-role`, User role = `login_totp_required`, Negate output = **off**;
   - step **OTP Form** — Required.
4. Trong `agrigreen browser - TOTP da cau hinh` (giữ hành vi cũ: ai đã tự cấu hình TOTP vẫn bị hỏi):
   - condition **Condition - user role** — Required; alias `agrigreen-not-login-totp-role`, User role = `login_totp_required`, Negate output = **on**;
   - condition **Condition - user configured** — Required;
   - step **OTP Form** — Required.
5. Menu ⋮ của `agrigreen browser` → **Bind flow** → *Browser flow*.

Không sửa luồng `direct grant` (client chỉ-dev `agrigreen-seed` dùng mật khẩu + OTP) và `clients` (tài khoản dịch vụ).

### 3.3 Kiểm tra ngay (trước khi báo cán bộ)

| Thử | Kết quả đúng |
|---|---|
| Đăng nhập web-ops bằng cán bộ KN thử chưa có TOTP | Sau mật khẩu → màn **Mobile Authenticator Setup** (quét QR), rồi vào hệ thống |
| Đăng xuất, đăng nhập lại cán bộ đó | Hỏi mã OTP 6 số |
| Quản lý HTX (`coop_manager`) chưa cấu hình TOTP | Không hỏi OTP |
| Token của cán bộ (jwt.io trên máy quản trị, không dán token thật lên mạng) | `amr` có `otp`; `realm_access.roles` có `login_totp_required` |
| `client_credentials` của `agrigreen-accounts` (api-platform gọi Keycloak Admin) | Vẫn chạy — tạo thử một tài khoản ở web-ops |

### 3.4 Tài khoản cán bộ đã có

Không cần làm gì: lần đăng nhập kế tiếp Keycloak tự thêm *Configure OTP* (OTP Form Required + chưa có TOTP). Muốn gửi trước qua email: `POST /v1/accounts/users/{id}/resend-actions` với `{"actions": ["CONFIGURE_TOTP"]}` (Admin API).

### 3.5 Lùi lại

Bind lại **browser** (luồng có sẵn) làm Browser flow — một thao tác, có hiệu lực ngay; TOTP người dùng đã đăng ký vẫn được hỏi (luồng có sẵn hỏi khi đã cấu hình). Vai đánh dấu để nguyên cũng vô hại.

### 3.6 Không đổi phiên

Kiểm tra (không sửa) — phải giữ theo D9/D24:

- Realm settings → Sessions: *SSO Session Idle* 4 giờ, *SSO Session Max* 24 giờ, *Offline Session Idle/Max* 72 giờ.
- Client `agrigreen-web-ops` → Advanced: *Client Session Idle* 4 giờ, *Max* 10 giờ. Client `agrigreen-field-app`: 24 giờ, offline 72 giờ.
- Realm settings → Authentication → Policies → Password policy: `length(12) and notUsername and passwordHistory(5)`.

Đổi bất kỳ giá trị nào ở đây cần quyết định mới và cập nhật [đánh giá rủi ro](../security/danh-gia-rui-ro-phien-4h-24h.md).

## 4. Realm quản trị riêng `agrigreen-kn-admin` (D25)

1. VPS: `sh deploy/vps/prepare.sh …` sinh `deploy/compose/.vps/keycloak/agrigreen-kn-admin-realm.json` (redirect theo tên miền). Kubernetes/khác: thay `http://localhost:3000` bằng `https://app.<domain>` trong bản sao tệp.
2. Nhập realm mới (realm chưa tồn tại nên không đè gì): Admin Console → **Create realm** → *Resource file* = tệp trên → Create. Hoặc khởi động lại Keycloak có tệp trong thư mục import.
3. Kiểm tra: Realm settings → Policies → Password policy có `length(14)`, `upperCase(1)`, `lowerCase(1)`, `digits(1)`, `specialChars(1)`, `notUsername`, `notEmail`, `passwordHistory(10)`, `forceExpiredPasswordChange(60)`; Browser flow = `kn-admin browser` (OTP Form Required); Sessions 5 phút/8 giờ.
4. Tạo tài khoản quản trị trong realm này (Admin Console → Users), required action *Update Password*; lần đăng nhập đầu bắt đăng ký TOTP.

**Giới hạn ở G1:** Admin API của api-platform **chưa** nhận token của realm này (issuer khác). Quản trị viên vẫn dùng tài khoản realm `agrigreen` có vai [MFA] (giờ cũng bị bắt TOTP khi đăng nhập). Nối issuer thứ hai và chuyển tài khoản quản trị sang realm riêng là việc W10.01/W10.03 sau G1. Tới lúc đó realm này dùng cho quản trị console Keycloak của hệ KN và để thẩm định thấy chính sách mật khẩu quản trị.

## 5. Job vô hiệu tài khoản không hoạt động

1. `migrate up` (cụm pf) — hai migration ở mục 1.
2. Tham số cho tenant đã có: `POST /v1/masterdata/seed` (Admin API, quản trị nền tảng) — seed chạy lặp an toàn, chỉ thêm khóa mới (`accounts.dormant_warn_days` 38, `accounts.dormant_disable_days` 45, đã duyệt bởi tác nhân hệ thống). Chưa seed thì job dùng mặc định 38/45.
3. Worker cần `AG_KC_ADMIN_URL`, `AG_KC_REALM`, `AG_KC_CLIENT_ID`, `AG_KC_CLIENT_SECRET` (như consumer `pf.accounts.org_status`) và `-jobs`/`AG_WORKER_JOBS` gồm `accounts_dormant` (hoặc `all`). Thiếu `AG_KC_ADMIN_URL` → không có job (không lỗi khởi động).
4. **Lần đầu ở môi trường đã có người dùng: chạy thử.** Mốc hoạt động lấy từ sự kiện LOGIN của Keycloak (giữ 365 ngày), phiên, lần tạo/mở khóa ở pf. Tài khoản đã lâu không đăng nhập sẽ bị khóa **ngay lượt đầu**. Đặt `AG_ACCOUNTS_DORMANT_DRY_RUN=true`, khởi động worker, đọc log:
   `event=job.accounts_dormant checked=… warned=… disabled=… dry_run=true`.
   Nếu `disabled` lớn bất thường, chọn một trong hai:
   - tạm nâng `accounts.dormant_disable_days` (vd 60) qua quy trình tham số để mọi người được báo trước, rồi hạ về 45 sau 7 ngày;
   - gửi thông báo chung, chấp nhận khóa và mở lại theo yêu cầu.
   Sau đó bỏ biến `AG_ACCOUNTS_DORMANT_DRY_RUN`.
5. Báo trước: thông báo cá nhân khẩn trong ứng dụng + email (consumer `pf.notify.accounts`, cần SMTP như gói NOTIF). Không có email đã xác minh → chỉ trong ứng dụng.
6. Mở khóa: Admin mở như mọi lần khóa (lý do ≥ 20 ký tự); job tính lần mở khóa là hoạt động nên không khóa lại ngay.
7. **Nông hộ không vào job** cho tới khi có kênh SMS (W10.06): D24 cho nông hộ tự kích hoạt lại bằng OTP — chưa có kênh nên khóa tự động sẽ buộc mọi nông hộ phải nhờ HTX/Admin. Khi W10.06 xong: bỏ điều kiện loại `farmer` ở `store.DormantCandidates` và thêm luồng tự kích hoạt lại.
8. Tài khoản tạo thẳng trong console Keycloak (không qua `pf/accounts`) không có trong `pf_accounts.account` nên job không thấy. Rà định kỳ ở Admin Console (Users) hoặc tạo lại qua web-ops.

## 6. Sau triển khai

- [ ] Mục 3.3 đạt trên môi trường thật với một tài khoản thử.
- [ ] Log `job.accounts_dormant` xuất hiện sau 02:00, `outcome=ok`.
- [ ] Xuất realm sau thay đổi, lưu cùng hồ sơ cấp độ (bằng chứng TOTP + chính sách mật khẩu quản trị).
- [ ] Ghi ngày áp dụng vào [đánh giá rủi ro](../security/danh-gia-rui-ro-phien-4h-24h.md) và hồ sơ cấp độ.
