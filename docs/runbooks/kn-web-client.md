# Client KN `kn-web` trong realm đã có

KNSPLIT-3-CLIENT (G1), theo [ADR-0055](../adr/0055-tach-he-thong-khuyen-nong.md). Tệp `deploy/compose/keycloak/agrigreen-realm.json` thêm một client; realm đã tồn tại không nhận lại tệp khi khởi động với `--import-realm`. Chỉ cập nhật client này trên realm hiện hữu, không nhập đè realm hoặc xóa volume. Các bước dưới đây dành cho người vận hành khi triển khai được duyệt; gói mã chưa áp dụng lên môi trường đang chạy.

## 1. Chuẩn bị

1. Sao lưu DB Keycloak và xuất cấu hình realm theo [backup.md](backup.md). Giữ bản trước thay đổi trong kho vận hành có kiểm soát truy cập, không đưa export chứa bí mật/người dùng vào Git hoặc log.
2. Xác nhận đúng realm `agrigreen`, phiên bản Keycloak 26.3 đang dùng trong Compose. Kiểm tra `Authentication` → Browser flow = `agrigreen browser` và thực hiện [runbook D24](keycloak-mfa-d24.md) mục 3 nếu realm cũ chưa có luồng này. Không chọn lại flow mặc định `browser` cho client mới.
3. Chốt URL của bản web KN dự định triển khai. Local/dev dùng localhost:3000; staging/production dùng chính xác origin HTTPS được duyệt. Cấu hình client là cấu hình môi trường, không phải quyền tenant: tenant/ngữ cảnh vẫn do backend kiểm.

## 2. Thêm hoặc đối chiếu đúng client

Admin Console → `Clients` → tìm **Client ID = kn-web**. Nếu chưa có, `Create client`, protocol **OpenID Connect**; nếu có, sửa đúng client đó, không tạo bản thứ hai. Đối chiếu với đối tượng `kn-web` trong realm JSON:

| Trường | Giá trị |
|---|---|
| Enabled | On |
| Client authentication | Off (public, không tạo/lưu client secret trong web) |
| Standard flow | On |
| Implicit flow / Direct access grants / Service accounts | Off / Off / Off |
| Valid redirect URIs (local/dev) | `http://localhost:3000/*`, `http://127.0.0.1:3000/*` |
| Web origins (local/dev) | `http://localhost:3000`, `http://127.0.0.1:3000` |
| Valid post logout redirect URIs (local/dev) | Hai redirect URI trên; export nối bằng `##` |
| Advanced → Proof Key for Code Exchange Code Challenge Method | **S256** (không để trống/plain) |
| Advanced → Client Session Idle / Client Session Max | **14400 / 36000 giây** (4 giờ / 10 giờ) |
| Advanced → Authentication flow overrides | Không override: kế thừa Browser flow của realm |
| Default client scopes | `basic`, `roles`, `web-origins`, `agrigreen-claims` |
| Optional client scopes | Rỗng; không thêm `offline_access` cho web KN |

Ở staging/production thay toàn bộ redirect, post-logout redirect và web origins của **kn-web** bằng URL HTTPS đã duyệt. Không giữ URL dev hoặc wildcard host/origin `*`/`+`. Đường dẫn callback phải nằm trên origin của bản web thực tế; không thêm origin của web MKG cổng 3001 hay custom scheme của app hiện trường.

Dùng các scope đã có trong realm, không tạo mapper riêng cho `tenant_id`, `org_ref`, `realm_access.roles`, `aud`, `sub`, `amr`. Giữ nguyên ngữ nghĩa vai realm, không đưa vai nghiệp vụ/phòng ban KN vào token và không tự cấp thêm vai. Không thay role mappings, policies, flow hoặc client khác. Realm SSO vẫn idle **14400**, max **86400** giây; client mobile và realm quản trị riêng giữ cấu hình cũ.

## 3. Nối web khi triển khai

G1 tiếp tục dùng mã `apps/web-ops`; frontend riêng sau G1. Bản web phục vụ KN có thể dùng cấu hình hiện hữu `NEXT_PUBLIC_OIDC_CLIENT_ID=kn-web`, `NEXT_PUBLIC_OIDC_ISSUER=<issuer agrigreen đã duyệt>`. Đây là biến build của Next.js: đặt ở lúc dựng image và cấu hình runtime tương ứng, rồi đối chiếu URL thực tế trước phát hành. Không đổi mặc định Compose/VPS dùng chung sang kn-web trong gói này. Không đổi client của HTX/MKG/field-app; MRV thuộc HTX/MKG.

Client riêng chưa chứng minh ranh giới tenant hoặc chuyển dữ liệu KN. API vẫn kiểm tenant, actor, MFA, vai và ngữ cảnh do server xác thực. Không đặt `AG_MFA_DISABLED` để kiểm thử hoặc xử lý lỗi đăng nhập.

## 4. Kiểm chứng trên môi trường triển khai

Dùng tài khoản thử, không đưa token/bí mật lên dịch vụ công cộng hoặc log. Lưu biên bản trong kho bằng chứng vận hành:

- Trong Client scopes → Evaluate, người dùng KN vẫn nhận `sub`, `tenant_id`, `org_ref`, `aud` có `agrigreen-api`, `realm_access.roles`; sau TOTP, `amr` phản ánh OTP. Token không chứa vai/phòng ban nghiệp vụ lấy từ trình duyệt.
- Mở bản web KN, yêu cầu authorize có `client_id=kn-web`, `response_type=code`, `code_challenge_method=S256`. Đổi mã với verifier đúng thành công; thiếu/sai verifier hoặc callback ngoài danh sách bị từ chối. Client không nhận password grant/client credentials/implicit flow.
- Cán bộ có `kn_user`/vai KN bắt đăng ký OTP nếu chưa cấu hình; đăng xuất rồi đăng nhập lại phải nhập OTP. Tài khoản thử chỉ có quyền HTX giữ hành vi D24 cũ trên client HTX. Không bỏ OTP để làm bài thử đạt.
- Chọn vai/phòng ban qua API, gọi chức năng KN và đổi ngữ cảnh trên cùng token; không được lấy ngữ cảnh tùy ý từ header/body. Cấu hình phiên web là 4 giờ/10 giờ; đo thời hạn token/refresh theo chính sách Keycloak, kể cả cửa sổ đệm idle của Keycloak.
- Đăng xuất quay lại đúng origin. Web-ops cũ, web-internal, client dịch vụ và app hiện trường vẫn dùng client/cấu hình cũ.

**Bằng chứng trong gói mã:** hai kiểm thử `TestKNWebClientCodePKCEAndClaims_KNSPLIT3`, `TestKNWebClientInheritsLoginTOTP_KNSPLIT3` và các kiểm thử realm/D24 hiện hữu. Đây là kiểm cấu hình; chưa có bằng chứng đăng nhập Keycloak thật hoặc áp dụng lên staging/production.

## 5. Lùi cấu hình

Nếu đã chuyển riêng bản web KN, đưa biến build/runtime của bản đó về client trước thay đổi đã lưu trong hồ sơ triển khai và dựng lại theo quy trình phát hành. Vô hiệu riêng client `kn-web` nếu cần ngừng đăng nhập mới, thu hồi phiên client theo quy trình vận hành. Không lùi Browser flow của realm, không tắt MFA và không nhập đè toàn realm. Không có migration dữ liệu trong gói này.

## Tài liệu nền

Các khóa PKCE và client-session của cấu hình đã đối chiếu [Server Administration Guide Keycloak 26.3.5](https://www.keycloak.org/docs/26.3.5/server_admin/index.html#_client-settings). Cơ chế import bỏ qua realm đã có được mô tả trong [Importing and exporting realms](https://www.keycloak.org/server/importExport). Các bước đăng nhập phía trên vẫn cần diễn tập thực tế trên môi trường triển khai.
