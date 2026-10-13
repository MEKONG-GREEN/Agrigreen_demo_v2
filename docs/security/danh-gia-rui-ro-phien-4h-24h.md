# Đánh giá rủi ro: giữ thời gian phiên 4 giờ / 24 giờ cho cán bộ khuyến nông

**Quyết định:** D24 (CEO, 13/10/2026), trả lời QĐ-4 phương án (c) và Q-016 trong [đối chiếu BRD KN](../kn/doi-chieu-brd-kn-2026-10.md). **Gói:** MFA (W10.03, W10.05). **Liên quan:** [ADR-0041](../adr/0041-mfa-theo-nhom-d24.md), [runbook](../runbooks/keycloak-mfa-d24.md), D9 (KN v7.7 N-09, [kn-v7.7-gap](../brd/kn-v7.7-gap.md)).

**Trạng thái:** Chấp nhận rủi ro còn lại — CEO (D24). Cần thêm: chữ ký đơn vị vận hành hệ thống thông tin KN và xác nhận của TTKN An Giang khi gửi hồ sơ cấp độ. **Xem lại:** sau 6 tháng vận hành (trước 15/06/2027), hoặc ngay khi có một sự cố trong mục 7.

## 1. Việc cần quyết

Kiến trúc v4.3 P-5 và TCVN 14423:2026 mục 5.5.2.2 đặt khóa phiên: quản trị 5 phút, Web nghiệp vụ 15 phút, app di động khóa sau 5 phút mở bằng PIN. Cùng mục đó cho phép giữ giá trị khác **nếu có đánh giá rủi ro biện minh** cho từng giá trị. Tài liệu này là đánh giá đó cho các giá trị D9 đã chốt với TTKN An Giang và đang khóa bằng kiểm thử.

| Bề mặt | Giá trị giữ (D9) | Giá trị v4.3 P-5 | Nơi đặt (khóa bằng `cmd/api-platform/realm_test.go`) |
|---|---|---|---|
| Web vận hành `agrigreen-web-ops` (cán bộ KN) | 4 giờ không thao tác; phiên tối đa 10 giờ | 15 phút | client `client.session.idle.timeout` 14400, `client.session.max.lifespan` 36000 |
| Realm `agrigreen` (trần chung) | 4 giờ không thao tác; tối đa 24 giờ | — | `ssoSessionIdleTimeout` 14400, `ssoSessionMaxLifespan` 86400 |
| App hiện trường `agrigreen-field-app` | phiên 24 giờ; ngoại tuyến 72 giờ, mở lại bằng PIN 6 số | khóa sau 5 phút, mở bằng PIN | client 86400 / offline 259200 |
| Quản trị (realm riêng `agrigreen-kn-admin`) | **5 phút** không thao tác, tối đa 8 giờ | 5 phút | realm quản trị — theo P-5, không thuộc phạm vi giữ |
| Access token (mọi client) | 5 phút | — | `accessTokenLifespan` 300 |

Chỉ hai dòng đầu và dòng app là "giữ lệch P-5". Quản trị đã theo P-5 (D24, D25).

## 2. Vì sao giữ

- **Cách làm việc của cán bộ xã.** Cán bộ nhập phiếu, nhật ký, giá ngoài đồng và ở trụ sở xã. Mạng 3G/4G chập chờn. Phiên 15 phút làm mất dữ liệu đang nhập dở và buộc đăng nhập lại (mật khẩu + TOTP) nhiều lần mỗi buổi. TTKN An Giang đã chốt 4 giờ ở D9 vì lý do này.
- **Khung giờ khóa nhập giá 12:00.** Báo giá nông sản (W11.74) dồn thao tác vào buổi sáng. Đăng nhập lại giữa chừng làm trễ hạn khóa.
- **App hiện trường ngoại tuyến.** Thiết bị đồng bộ theo lô. Khóa 5 phút cần khóa thiết bị gắn phần cứng (W10.04, chờ G-5 máy Android giá rẻ) mới làm đúng được. Làm bằng phần mềm thuần thì yếu hơn PIN hiện có.

## 3. Mối đe dọa

| # | Kịch bản | Phiên dài làm tăng rủi ro thế nào |
|---|---|---|
| T1 | Máy tính ở trụ sở xã bị bỏ đi khi phiên còn mở; người khác dùng tiếp | Cửa sổ khai thác tới 4 giờ (thay vì 15 phút) |
| T2 | Đánh cắp phiên trình duyệt (XSS, tiện ích độc, mã độc trên máy) | Refresh token sống lâu hơn → dùng lại được lâu hơn |
| T3 | Mất hoặc bị lấy điện thoại có app hiện trường | Phiên app 24 giờ + ngoại tuyến 72 giờ |
| T4 | Máy dùng chung, cán bộ quên đăng xuất | Như T1 |
| T5 | Lộ mật khẩu (dò, lừa đảo, dùng lại mật khẩu) | Không phụ thuộc thời gian phiên — nêu để so với biện pháp bù |

## 4. Biện pháp bù trừ (đã có trong mã)

| Biện pháp | Chống | Ở đâu |
|---|---|---|
| **TOTP bắt buộc khi đăng nhập** cho mọi cán bộ KN và vai quản trị từ G1, đăng ký ngay lần đăng nhập đầu (D24) | T5; mỗi lần mở phiên mới cần thiết bị thứ hai | realm `agrigreen` — vai đánh dấu `login_totp_required`, luồng `agrigreen browser` |
| Access token 5 phút; refresh token chỉ dùng được trong phiên còn hạn; PKCE S256 cho mọi client công khai | T2 — token lấy được hết hạn nhanh; không có implicit flow | realm, client |
| Thu hồi mọi phiên khi khóa tài khoản, đặt lại TOTP, tổ chức bị vô hiệu hóa | T1–T4 sau khi phát hiện | `pf/accounts` (Logout ở Keycloak) |
| Admin thấy **phiên đang hoạt động** và **lịch sử đăng nhập** (IP, client) của từng tài khoản | phát hiện T1–T4 | màn Chi tiết tài khoản (FN-AD-04.04), sự kiện Keycloak giữ 365 ngày |
| Khóa tạm 15 phút sau 5 lần sai, khóa hẳn sau 3 lần khóa tạm/24 giờ (A3) | T5 | realm brute force |
| Vô hiệu tài khoản không hoạt động 45 ngày, báo trước 7 ngày (D24) | tài khoản bỏ quên bị chiếm | job `accounts_dormant` |
| Admin API đòi token có yếu tố thứ hai (amr) cho vai [MFA]; quản trị ở realm riêng, phiên 5 phút | thao tác quản trị không ăn theo phiên dài của cán bộ | `httpx.RequireMFA`, realm `agrigreen-kn-admin` |
| Dữ liệu định danh (họ tên, SĐT hộ) ở kho định danh, mở theo mục đích và ghi nhật ký; giao diện chỉ hiện nhãn đã che | giảm thiệt hại khi T1–T4 xảy ra | `vault/identity`, R-42 |
| App: PIN 6 số mở lại khi ngoại tuyến; thu hồi thiết bị từ Admin | T3 | `pf/sync` devices, `session_note` |
| Đăng xuất phía máy chủ (`post.logout.redirect.uris`), không "ghi nhớ đăng nhập" ở Keycloak | T4 | client `agrigreen-web-ops` |

Biện pháp tổ chức (đưa vào quy chế sử dụng, đơn vị vận hành chịu trách nhiệm):

- máy tính dùng cho hệ thống bật **khóa màn hình hệ điều hành sau ≤ 5 phút** không thao tác;
- máy dùng chung phải đăng xuất khi rời máy; không dùng chung tài khoản;
- báo mất điện thoại ngay để Admin thu hồi thiết bị và đặt lại TOTP.

## 5. Đánh giá

Thang 1–5 cho khả năng (K) và tác động (T). Rủi ro = K × T: thấp ≤ 6, trung bình 7–12, cao ≥ 13.

| # | Trước bù (phiên 4 giờ, chưa TOTP) | Sau bù (phiên 4 giờ + biện pháp mục 4) | Nếu theo P-5 (15 phút, có TOTP) |
|---|---|---|---|
| T1 | K3 × T3 = 9 | K2 × T3 = 6 (khóa màn hình OS 5 phút; dữ liệu nhạy cảm đã che) | K1 × T3 = 3 |
| T2 | K2 × T4 = 8 | K2 × T3 = 6 (token 5 phút, PKCE, thu hồi phiên) | K2 × T3 = 6 |
| T3 | K3 × T3 = 9 | K2 × T3 = 6 (PIN, thu hồi thiết bị) | K2 × T2 = 4 (cần W10.04) |
| T4 | K3 × T3 = 9 | K2 × T3 = 6 | K1 × T3 = 3 |
| T5 | K3 × T4 = 12 | K1 × T4 = 4 (TOTP) | K1 × T4 = 4 |

**Kết luận.** Mọi rủi ro sau bù ở mức **thấp** (≤ 6). Mức này cao hơn phương án P-5 ở T1, T3, T4 khoảng một bậc khả năng. Chênh lệch được chấp nhận vì:

- TOTP bắt buộc (D24) xử lý rủi ro lớn nhất trước bù là T5;
- phần chênh còn lại bị chặn bởi khóa màn hình OS và việc thu hồi phiên chủ động;
- giá vận hành của 15 phút (mất dữ liệu nhập dở, trễ hạn 12:00, phải đăng nhập lại kèm TOTP nhiều lần) cao hơn lợi ích tăng thêm.

## 6. Tham số và cách đổi

- Mọi giá trị ở mục 1 là cấu hình realm/client của Keycloak, khóa bằng `realm_test.go` (đúng 14400/86400/36000). Đổi phải có: quyết định mới, bản cập nhật tài liệu này, sửa realm dev + kiểm thử, và các bước thủ công ở realm đã nhập ([runbook](../runbooks/keycloak-mfa-d24.md) mục 3.6).
- Ngưỡng vô hiệu 38/45 ngày là tham số có phiên bản của `pf/masterdata` (`accounts.dormant_warn_days`, `accounts.dormant_disable_days`), đổi qua quy trình tham số (người đặt ≠ người duyệt).
- Khi có W10.04 (khóa ký thiết bị + PIN = hai yếu tố, app khóa sau 5 phút), dòng app ở mục 1 chuyển sang P-5 và T3 được tính lại.

## 7. Điều kiện xem lại ngay

- Có sự cố chiếm phiên hoặc tài khoản của cán bộ KN.
- Kiểm thử xâm nhập (v4.3 việc #8) có phát hiện về phiên hoặc token.
- Hồ sơ cấp độ bị cơ quan thẩm định yêu cầu theo P-5.
- Thêm nhóm người dùng mới (HTX, Cục KTHT, đối tác) vào realm `agrigreen` với dữ liệu nhạy cảm hơn.

## 8. Theo dõi

| Chỉ số | Nguồn | Ngưỡng cần xem |
|---|---|---|
| Số lần đặt lại TOTP / tháng | sổ `account_event` (action `totp_reset`) | tăng đột biến |
| Đăng nhập sai / khóa tạm | sự kiện Keycloak `LOGIN_ERROR` | > 3 lần khóa tạm/ngày một tài khoản |
| Tài khoản bị khóa vì không hoạt động | `account_event` (`locked`, `dormant_45d`), log `job.accounts_dormant` | > 10 %/tháng (đào tạo lại) |
| Phiên cùng tài khoản từ nhiều IP | tab "Phiên đang hoạt động" | phiên lạ |
