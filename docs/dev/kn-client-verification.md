# Kiểm chứng client KN trên dev (#59)

Nền kiểm `c6ce02b5` ngày 21/10/2026. Commit cấu hình `b5f9c898` và bàn giao `2e82dfb2` đã có trong lịch sử; gói này chỉ kiểm chứng, không tạo lại client hoặc đổi realm đang chạy. Phạm vi G1 theo ADR-0055: client `kn-web` trong realm `agrigreen`, tiếp tục dùng web-ops; frontend/realm/binary riêng sau G1 khi được chốt.

## Đối chiếu cấu hình hiện có

| Điểm kiểm | Bằng chứng trong repo | Giới hạn |
|---|---|---|
| Client | Một `kn-web`, public OIDC, authorization code bật; implicit/password/service account tắt rõ; không secret | Chưa đọc cấu hình realm đã nhập |
| PKCE | `pkce.code.challenge.method=S256` | Chưa thử trao đổi code/verifier thật |
| Callback/origin | Chỉ localhost/127.0.0.1:3000 trong dev, post logout cùng phạm vi | Staging/production cần HTTPS được duyệt theo runbook |
| Phiên | Client idle 14400s/max 36000s; realm SSO 14400s/86400s; không offline scope | Chưa đo refresh/logout/expiry trên Keycloak |
| Claims | Client dùng basic, roles, web-origins, agrigreen-claims; mapper access-token có sub, tenant_id, org_ref, realm_access.roles, aud agrigreen-api, amr | Chưa nhận token thật; không log token/PII |
| MFA | Kế thừa agrigreen browser; composite KN login roles chứa login_totp_required và nhánh OTP bắt buộc | Chưa diễn tập enrollment/TOTP/HTX regression trực tiếp |
| Web | OIDC response_type=code, cấu hình issuer/clientId qua biến NEXT_PUBLIC lúc build | Compose vẫn mặc định agrigreen-web-ops; không tự chuyển tất cả web sang client KN |

Nguồn: `deploy/compose/keycloak/agrigreen-realm.json`, `cmd/api-platform/realm_kn_client_test.go`, `apps/web-ops/src/auth/oidc.ts`, [runbook client](../runbooks/kn-web-client.md), [ADR-0041](../adr/0041-mfa-theo-nhom-d24.md), [ADR-0055](../adr/0055-tach-he-thong-khuyen-nong.md). Test config có sẵn chưa kiểm hợp đồng request scope của web.

## Hai điểm owner cần xử lý

Web đang gửi scope `openid profile`, nhưng JSON realm không khai báo profile và kn-web không gán profile vào default/optional scopes. Đây cũng là hiện trạng client web cũ. [TokenManager Keycloak 26.3.5](https://github.com/keycloak/keycloak/blob/26.3.5/services/src/main/java/org/keycloak/protocol/oidc/TokenManager.java#L616-L669) loại openid rồi kiểm các scope còn lại thuộc client; [AuthorizationEndpoint](https://github.com/keycloak/keycloak/blob/26.3.5/services/src/main/java/org/keycloak/protocol/oidc/endpoints/AuthorizationEndpoint.java#L168-L175) thực hiện kiểm scope trước PKCE. Suy luận từ cấu hình: request profile chưa được cấp sẽ không hợp lệ; chưa có tái hiện trên realm chạy thật. Giao owner KN đối chiếu request scope với scope đã gán, giữ các mapper claim/audience/MFA cần thiết, thêm test hợp đồng và kiểm authorize thực tế; không tự thêm profile/PII vào token để bỏ qua lỗi.

Compose đặt issuer `http://localhost:8180/realms/agrigreen`, cổng 8180→Keycloak8080. Nhưng `apps/web-ops/src/config.ts` fallback và `.env.example` còn 8081; Compose dùng 8081 cho API nội bộ MKG. Bản web phải đặt issuer đúng khi build. Sửa ví dụ/default frontend thuộc owner KN, không sửa tại gói shared này.

## Tenant và phần còn lại của #56

`authz.Verifier` kiểm chữ ký/issuer/audience/hạn, lấy actor từ sub và tenant từ tenant_id; audience API là agrigreen-api. Client ID kn-web không tự chọn tenant Trung tâm tỉnh và không phải quyền đọc/ghi dữ liệu. `accounts.ResolveContext` đọc trong tenant của token, kiểm đúng actor, hạn context, tài khoản/phân công/phòng ban còn hiệu lực; middleware không nhận tenant thay thế từ header. Phiên chủ quản vẫn giữ identity, realm roles và MFA của Principal.

#56 IDENTITY-GRANTS còn phải chuyển danh tính/phân công/ủy quyền/job khóa và định tuyến sang tenant đúng Trung tâm, thu hồi phiên/quyền nguồn sau cutover, cấp share_grant dựa thỏa thuận D2 thật. Cần bằng chứng token mới vào tenant đích; token/context cũ không ghi KN; đổi Trung tâm/copy header không vượt tenant; HTX/MKG và MRV vẫn đúng hệ; thu hồi/hết hạn mất quyền trên kết nối tái sử dụng. Không suy ra hoàn tất việc chuyển tenant từ client JSON hoặc digest tenant map.

## Kiểm runtime và trạng thái

GET chỉ đọc `http://localhost:8180/realms/agrigreen/.well-known/openid-configuration` lúc 22:28:27 +07 ngày 21/10/2026 bị connection refused. Không thấy listener 8180 hay tiến trình Java/Keycloak/Docker trong lần kiểm. Không đăng nhập, không bật container, không import/sửa realm, không dùng hay in credential/token. Bằng chứng ngoài repo: `%TEMP%/agrigreen-codex-ticket59-discovery.json`.

Live OIDC/PKCE/MFA/refresh/logout/tenant-routing **chưa chạy**. Realm đã nhập không tự cập nhật khi thay JSON; người vận hành cần thực hiện runbook trên môi trường được duyệt rồi lưu bằng chứng không chứa bí mật. Chỉ GET discovery thành công cũng chưa chứng minh PKCE, OTP hoặc tenant isolation.

Các bài thử cấu hình thuần và kiểm bộ xác minh token hiện có sẽ được ghi sau khi kết thúc. Không có SQL/Go/frontend/deploy thay đổi; không lấy kiểm cấu hình làm bằng chứng PG hoặc live Keycloak. #59 và G1 còn mở tới khi owner xử lý hợp đồng web và có chứng cứ vận hành/phần #56.