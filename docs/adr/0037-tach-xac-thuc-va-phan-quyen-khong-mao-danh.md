# ADR-0037: Tách xác thực và phân quyền, không mạo danh

**Trạng thái:** Đề xuất — chờ MKG ký (A7) · **Ngày:** 04/10/2026 (gói W0.3) · **Phụ lục B v4.1:** mục 14 · **Xung đột:** X-14 · **Thành phần:** Keycloak (realm chung), `pf/accounts` (`pf_accounts.role_assignment_*`, `session_context`, `support_session*`, `support_action_log` — pf 000060, 000061), `internal/shared/authz` (middleware `X-AG-Context`), mọi module KN v7.7 · **Liên quan:** [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0029](0029-k-ref-ban-chieu-loi-moi-cum.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), [kn-v7.7-gap.md §4.5](../brd/kn-v7.7-gap.md), gói W7.2 (`k/access` hợp nhất)

## Bối cảnh

- Phụ lục B v4.1 mục 14 yêu cầu một ADR "tách xác thực và phân quyền; không impersonation", đặc tả theo SAD định danh v1.1. Ở v4.1, `k/access` cung cấp `ResolveContext` (§25), cùng token ngữ cảnh có epoch và biến DB `node/dept/xa_ids/purpose`.
- KN v7.7 gói P1 đã hiện thực việc này cho app KN, theo **phương án A** của [kn-v7.7-gap.md §4.5](../brd/kn-v7.7-gap.md):
  - Keycloak chỉ xác thực.
  - Vai trò lưu ở sổ gán vai `pf_accounts.role_assignment_event` (vai × Nút × phòng ban × cấp × xã).
  - Client chọn ngữ cảnh qua `POST /v1/me/context` rồi gửi header `X-AG-Context`. Server tra `pf_accounts.session_context` ở mỗi request.
- Chế độ hỗ trợ Admin (FN-AD-04.07, pf 000061) cho Admin thao tác **thay cho** một tài khoản hoặc xã. Danh tính người thao tác không đổi.
- Bản đối chiếu §2.4 và X-14 kết luận: repo đúng tinh thần mục 14, nhưng còn hai cơ chế song song (KN context và `ScopeResolver` cho module khác) và chưa có epoch.

## Quyết định

### 1. Keycloak chỉ xác thực

- Keycloak giữ danh tính, MFA, khóa tài khoản và phiên. Token **không** mang vai nghiệp vụ theo Nút, phòng ban hay xã.
- Phân quyền lấy từ sổ gán vai ở phía server. Gán hay gỡ vai có hiệu lực ở request kế tiếp mà không cần cấp lại token.
- Realm roles hiện có (`kn_*`, `platform_admin`…) giữ cho module chưa chuyển, cho đến W7.2.

### 2. Ngữ cảnh phiên là hiện thực ĐTK 1 của `ResolveContext`

- `GET /v1/me/contexts` trả các cặp (vai trò, phòng ban) của người dùng. `POST /v1/me/context` trả `context_id`, và mọi request sau gửi `X-AG-Context: <context_id>`.
- Middleware kiểm từng request: context thuộc đúng người dùng, còn hiệu lực, phòng ban đang áp dụng. Sau đó dựng `Principal{Roles, Department, Scope}`. Đổi ngữ cảnh chỉ là đổi header.
- Đây là **hiện thực ĐTK 1** của `ResolveContext` v4.1. Khác v4.1 ở hai điểm, cả hai do W7.2 đóng:
  - chưa có **epoch ngữ cảnh** (Valkey định danh `noeviction`) để thu hồi mà không cần tra DB;
  - chỉ app KN dùng; module khác còn dùng `ScopeResolver`.
- Biến DB của ngữ cảnh (`app.node_id`, …) được đặt trong `WithContextTx` (ADR-0028, W2.3). RLS đọc biến qua `k_ref.ctx_uuid()` (ADR-0029). Chính sách đọc tại chỗ dùng chính các biến này (ADR-0030).

### 3. Không mạo danh: chế độ hỗ trợ có lý do và nhật ký

- **Không** có cơ chế đăng nhập thành người khác, đổi `sub` trong token hay token exchange để mạo danh.
- Chế độ hỗ trợ: `POST /v1/accounts/support-sessions` mở một phiên hỗ trợ trên (vai trò, tài khoản hoặc xã) và trả về một context đặc biệt có `on_behalf_of`.
  - Người thao tác vẫn là Admin (`actor`). Người được hỗ trợ chỉ được ghi ở `on_behalf_of`.
  - Bắt buộc MFA: claim `amr` phải có yếu tố thứ hai, nếu không trả `mfa_required`.
  - Mọi thao tác ghi trong phiên hỗ trợ bắt buộc có header `X-AG-Reason` (≥ 10 ký tự). Thiếu lý do thì trả `<module>.support_reason_required` (422).
  - Mỗi thao tác ghi một dòng vào `pf_accounts.support_action_log` (chỉ ghi thêm) gồm actor, `on_behalf_of`, lý do và thao tác. Tra cứu qua `GET /v1/accounts/support-log`.
  - Thoát bằng `POST /v1/accounts/support-sessions/{id}/end`. Phiên đã thoát không dùng lại được.
- Giao diện hiện dải cảnh báo "đang hỗ trợ" suốt phiên (BRD KN v7.7 FN-AD-04.07).

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| B: mỗi ngữ cảnh một token (Keycloak token exchange, "organizations", claim `ctx`) | Phụ thuộc tính năng Keycloak bản mới. Khó dùng ngoại tuyến 72 giờ. Thu hồi vai phải đợi token hết hạn ([kn-v7.7-gap.md §4.5](../brd/kn-v7.7-gap.md)) |
| Đưa vai × Nút × phòng ban vào claim token | Token phình to. Đổi vai phải đăng nhập lại. Trái yêu cầu đổi ngữ cảnh không đăng nhập lại |
| Impersonation (Admin đăng nhập thành người dùng) | Nhật ký ghi sai người thao tác. Trái Phụ lục B mục 14 và SAD v1.1 |

## Hệ quả

- Repo đáp ứng Phụ lục B mục 14 cho app KN ngay từ ĐTK 1. Các phần còn lại (epoch, một cơ chế cho mọi app, quyền module × lớp R-MOD-07, khóa API về `pf/accounts`) nằm ở W7.2.
- Mỗi request có `X-AG-Context` tốn thêm một lần tra `session_context`. Epoch ở W7.2 thay lần tra này bằng so khớp epoch.
- Thu hồi tức thời: gỡ vai hoặc tắt phòng ban có hiệu lực ở request kế tiếp. Khóa tài khoản thì Keycloak đăng xuất mọi phiên.
- Nhật ký hỗ trợ là bằng chứng kiểm toán (RB-1) và không xóa được.

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| KN P1 (đã có) | Sổ gán vai, `session_context`, middleware `X-AG-Context`, chế độ hỗ trợ, nhật ký | pf 000060, 000061 |
| W2.3 | `WithContextTx` đặt biến ngữ cảnh cùng `SET LOCAL ROLE` | pf 000090, shared 000007 |
| W2.5 | Bảng chiếu `k_ref` mà RLS đọc theo ngữ cảnh | pf 000091–000092, mkg 000022 |
| W7.2 | `k/access` hợp nhất: một ngữ cảnh cho mọi app (thay `ScopeResolver`), epoch, quyền module × lớp | pf 000111–000112 |

## Liên quan

- Kiến trúc v4.1: §25 (`k/access`, `ResolveContext`), §30, Phụ lục B mục 14, Phụ lục D (SAD định danh v1.1, R-ID-01…46).
- Bản đối chiếu: §2.4, X-14, dòng 14 của bảng Phụ lục B.
- [docs/errors.md](../errors.md) (mã `accounts.*`, `*.support_reason_required`, `mfa_required`), [docs/brd/kn-v7.7-plan.md](../brd/kn-v7.7-plan.md) gói P1.
