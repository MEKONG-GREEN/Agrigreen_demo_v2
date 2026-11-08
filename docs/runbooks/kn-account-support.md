# Tiếp nhận hỗ trợ tài khoản KN (Q10, #274)

Public endpoint của Application API: `POST /v1/public/contacts/support-requests`, JSON `{name, contact, message}` và `Idempotency-Key` UUID.
Không gửi tenant/actor/role; không đăng nhập và không cần assignment. Mỗi UUID là một thao tác, thử lại dùng cùng UUID và nội dung.
202 chỉ sau transaction commit. Giới hạn 12 KiB, không nhận file; họ tên 100, contact 254, nội dung 2000 ký tự. Không ghi password/OTP.

## Bootstrap chủ thể giữ khóa

Operator có quyền cấp phát vault (`vault_registrar`) tạo **một chủ thể kỹ thuật không chứa thông tin của người gửi**, trong đúng tenant KN.
Token operator là token hiện hữu, server vault xác minh bình thường; không giả principal, không dùng token dịch vụ đặc quyền thay người dùng.
Trong PowerShell, sau khi đặt `AG_ACCESS_TOKEN` và `AG_VAULT_URL` cho phiên vận hành được phép (không ghi token vào lịch sử/ảnh/log):

```powershell
$operation = [Guid]::NewGuid().ToString()
$body = @{purpose='registry.admin'; kind='person'; full_name='Kho hỗ trợ tài khoản KN'; area_code='kn_support'} | ConvertTo-Json
$keyHolder = Invoke-RestMethod -Method Post -Uri ($env:AG_VAULT_URL.TrimEnd('/') + '/v1/subjects') -ContentType 'application/json' -Headers @{
  Authorization=('Bearer ' + $env:AG_ACCESS_TOKEN)
  'Idempotency-Key'=$operation
} -Body ([Text.Encoding]::UTF8.GetBytes($body))
```

Giữ `$operation` khi retry cùng bootstrap. Kết quả `ref` và `public_key` là dữ liệu công khai, không phải khóa bí mật.
Operator cấu hình Application API (S10/profile KN/deployment owner, không phải frontend) rồi khởi động lại theo quy trình deployment:

| Biến | Giá trị |
|---|---|
| `AG_KN_SUPPORT_TENANT_ID` | UUID tenant KN đã được vault xác minh cho token operator trên |
| `AG_KN_SUPPORT_SUBJECT_REF` | `$keyHolder.ref` |
| `AG_KN_SUPPORT_PUBLIC_KEY` | `$keyHolder.public_key` (base64 chuẩn, 32 byte X25519) |

Không đặt khóa bí mật/DEK/token vào cấu hình này. Ba giá trị lấy cùng lần bootstrap, cùng tenant. Không dùng chủ thể người dân/nhân sự.
Thiếu cả ba: route intake trả 503, không báo đã tiếp nhận. Cấu hình một phần/sai dạng: API không startup. Mỗi public deployment có một tenant
tiếp nhận; các tenant khác cần deployment/app binding riêng do operator quản lý. Không cho client chọn tenant qua Host tùy ý/header/body.
Native `setup274` trong `tests/support274` thực hiện chính POST bootstrap này trên HTTP vault và PostgreSQL thật, rồi gửi public request thành công.

## Admin xử lý

Admin có realm `platform_admin` thật + MFA và assignment Admin KN còn hiệu lực chọn phiên KN bình thường. Không cấp thêm vai vault để xử lý.
Mở Quản trị hệ thống → Yêu cầu hỗ trợ tài khoản (`/kn/quan-tri/yeu-cau-ho-tro`); queue chỉ metadata, bấm Xem mới giải mã.
Nội dung được mở bằng token Admin gốc, purpose `kn.account_support`; vault chỉ mở chủ thể area `kn_support` bằng purpose này.
Vault audit và KN audit commit rồi mới trả nội dung. Lỗi quyền, MFA, tenant, vault hoặc audit không trả plaintext.
Đánh dấu đã xử lý dùng `PUT /v1/contacts/support-requests/{id}/resolution`, `{expected_version}`, UUID thao tác; retry cùng UUID.
Phiên bản cũ/cạnh tranh → 409 và tải lại; mất quyền/MFA/phiên → từ chối trước replay. Không có API đọc công khai hay tra tài khoản theo contact.

## Chạy và kiểm

```powershell
go run ./cmd/migrate up
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/contact/... ./internal/vault/identity/... ./internal/shared/envelope ./tests/support274 ./migrations
```

Chọn toolchain từ `go.mod` và biến môi trường theo AGENTS/CONTEXT. Backend dùng gốc lắp ráp `cmd/api-platform` và cấu hình vault hiện hữu;
không gọi port/HTTP trong DB transaction. Web: `npm ci`, `npm run gen:api`, `npm run check:api`, `npm run lint`, `npx tsc --noEmit`,
`npx vitest run`, `npm run build:mock`, rồi `npm start -- -p <port-rieng>` (hoặc `npx next start -p <port-rieng>`).
Giữ `/login/lien-he` trong login-family allowlist S2/#263; công việc này không đổi reset/PIN/role-selection hoặc source của các nhánh chưa gộp.
