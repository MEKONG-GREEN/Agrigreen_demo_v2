## KN-STANDALONE-10B-PROFILE-CI — Profile chỉ-KN, realm `kn`, job CI `kn-standalone`

Gói [#272](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/272), Đợt 4 của kế hoạch "KN chỉ theo BRD 2.0 và
chạy độc lập" (đã duyệt 07/11/2026). Người dùng quyết 11/11/2026 làm gói này **trước** các gói gỡ S-1…S-9, nên lần này
kiểm "bề mặt KN ⊆ danh sách FN" chỉ ở chế độ báo cáo; phần còn lại ở
[#310](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/310) `KN-STANDALONE-10B-PROFILE-CI-GATE`. Gói không
thêm, bớt hay đổi Function nghiệp vụ nào của BRD 2.0: nó đo nguyên tắc 3 ("App Khuyến nông hoạt động độc lập được theo
BRD").

### Thay đổi theo trình tự triển khai

| Bước | Tệp | Nội dung |
|---|---|---|
| 1 — Khởi tạo/cấu trúc | `deploy/compose/docker-compose.kn.yml` | Tệp ghi đè = profile `kn`. Dự án Compose `agrigreen-kn`. Tắt `api-internal`, `worker-internal`, `ingest`, `edge-sim*`, `optimizer`, `web-internal`; `api-public`, `integration-gateway` vốn tắt. Tệp gốc không sửa |
| 1 | `deploy/compose/keycloak-kn/kn-realm.json` | Realm `kn` (Q8). Không sửa realm `agrigreen` |
| 2 — Data | Không migration, không SQL | Tenant KN mới dựng qua Admin API (`tools/knsmoke`) |
| 3 — Service | `tools/knsmoke/{smoke,keycloak,main}.go` | Seed + kiểm khói; `Identity` tách nguồn token (Keycloak thật / issuer giả của kiểm thử) |
| 4 — API | Không đổi API | Chỉ gọi API hiện có |
| 5 — Kiểm chứng | `tools/knsmoke/smoke_integration_test.go`, `cmd/api-platform/realm_kn_standalone_test.go`, `.github/workflows/ci.yml` job `kn-standalone` | Xem "Kiểm chứng" |

### Profile `kn`

- Chạy: `postgres`, `pgbouncer`, `keycloak`, `rabbitmq`, `valkey`, `seaweedfs`, `identity-vault`, `migrate-bootstrap`,
  `migrate-up`, `api-platform`, `worker`, `outbox-publisher`, `web-ops`.
- Giữ thêm ba dịch vụ phụ trong mạng compose vì dịch vụ trên phụ thuộc: `rabbitmq-init` (`outbox-publisher` chờ nó),
  `s3-init` (tạo bucket), `otel-collector` (đích OTLP). Không cái nào nối ra hệ ngoài.
- Vì sao là tệp ghi đè chứ không phải `--profile kn`: Compose chỉ dùng profile để **bật thêm** dịch vụ. Muốn
  `--profile kn` chọn đúng 12 dịch vụ thì phải gắn profile vào chính các dịch vụ đó trong tệp gốc, khi ấy
  `docker compose up` mặc định không còn dựng gì — trái yêu cầu "không đổi cấu hình mặc định". Cùng cách
  `docker-compose.vps.yml` đang làm.
- `api-platform` vẫn là một tiến trình dựng đủ mọi module (`assembly.Build`); "chỉ-KN" ở mức tiến trình và tenant, chưa
  ở mức mã nạp. Cổng 8084 (Internal, mTLS cho optimizer) vẫn mở trong mạng compose nhưng không có client.

### Realm `kn`

- Client: `kn-web` (web, PKCE) và `kn-field` (app hiện trường, PKCE, redirect `vn.agrigreen.field:/oauth2redirect`) là
  hai client **đăng nhập** duy nhất. Hai client kỹ thuật không đăng nhập người dùng: `kn-accounts` (tài khoản dịch vụ
  của `pf/accounts` gọi Keycloak Admin REST — thiếu nó thì tạo tài khoản, khóa/mở, đặt lại mật khẩu ADR-0057 không chạy;
  bí mật là placeholder `${AG_KC_CLIENT_SECRET}` như realm dev) và `kn-seed` (ROPC, **chỉ Local/CI**, cho
  `tools/knsmoke`; không tạo ở staging/production — cùng quy ước `agrigreen-seed`).
- Không có client `agrigreen-api`: claim `aud` = `agrigreen-api` sinh bằng audience tùy biến của scope
  `agrigreen-claims`, nên `AG_OIDC_AUDIENCE` không đổi.
- `resetPasswordAllowed = false`, bỏ ba luồng `agrigreen … recovery`: trang "Quên mật khẩu" gốc của Keycloak không giới
  hạn số yêu cầu theo tài khoản; người dùng đi luồng của hệ thống (ADR-0057: 5 yêu cầu mỗi giờ cho một tài khoản, liên
  kết 30 phút, dùng một lần).
- Vai: 23 vai của KN và nền (`kn_*`, `platform_admin`, `account_admin*`, `masterdata_admin`, `registry_admin`,
  `reports_admin`, `param_*`, `geo_admin`, `metrics_viewer`, `sync_conflict_resolver`, `vault_*`,
  `login_totp_required`, `offline_access`), định nghĩa giống hệt realm `agrigreen`. Không có vai HTX, CGH, MRV, MKG,
  Public API, mô phỏng, thủy hệ, nội dung.
- Luồng đăng nhập D24 (TOTP theo vai), khóa tạm 5 lần/15 phút, phiên 4 giờ/24 giờ, chính sách mật khẩu, hồ sơ người
  dùng: giữ nguyên; kiểm thử so từng khóa với realm `agrigreen` để hai realm không lệch.
- Realm quản trị riêng `agrigreen-kn-admin` (D24/D25) chưa đưa vào profile `kn`: web-ops của profile này dùng một realm.
- App hiện trường muốn dùng profile này phải build với `oidcClientId = kn-field` và issuer realm `kn`
  (`packages/agrigreen_mobile/lib/core/config.dart`); gói này không đổi mặc định của app.

### Tenant KN mới và module

`tools/knsmoke` tạo một tenant mới (UUID ngẫu nhiên) chỉ qua Admin API: danh mục nền, TTKN Quốc gia → TTKN tỉnh, 5 phòng
ban ở trạng thái "Đang áp dụng", một tài khoản Cấp Tỉnh thuộc cả 5 phòng (tạo bằng `POST /v1/accounts/kn-users`, tức
`pf/accounts` ghi vào realm `kn`), và **tắt 8 module ngoài KN**: `pf/cooperative`, `pf/cropcycle`, `pf/mechanization`,
`pf/mrv`, `pf/waterway`, `pf/simulation`, `pf/metering`, `pf/content`.

**Chưa tắt được** (ghi nhận, không sửa trong gói này):

| Module | Lý do |
|---|---|
| `pf/apiclient`, `pf/extdata`, `pf/kref`, `mkg/*` | `module.yaml` khai `enable: always`; `POST /v1/modules/{zone}/{name}/enablement` trả `409 party.module_always_on`. Đổi sang `per_tenant` là đổi hợp đồng module của hệ khác (ADR-0027, W1.4) — ngoài phạm vi gói. Trong profile `kn` các module `mkg/*` không có tiến trình (`api-internal`, `worker-internal`, `ingest` không chạy); `pf/apiclient` không có `api-public`/`integration-gateway` phục vụ |
| `pf/guestlink` | `per_tenant` nhưng đang "dormant" (không route, không chủ), không nằm trong danh sách tắt của kế hoạch — để nguyên |

Các module `per_tenant` còn lại (`pf/extension`, `forms`, `geo`, `hrm`, `masterdata`, `metrics`, `notify`, `price`,
`reports`, `sync`) là hạ tầng KN theo Q2.

### Kiểm khói (cổng thật)

Bằng tài khoản Cấp Tỉnh của tenant mới, token thật của realm `kn`, trong lúc container bị chặn mạng ra ngoài:

- `GET /v1/me/modules`: đúng 8 module trên đang tắt; mỗi module một route trả `403 module_not_enabled`.
- Mỗi phòng: `POST /v1/me/context` → `GET /v1/menu` trả menu của phòng → FN đại diện đọc được (200):

| Phòng | FN BRD 2.0 | API |
|---|---|---|
| Trồng trọt v7.7 | FN-TI-02.01 Dashboard trồng trọt toàn tỉnh | `GET /v1/extension/kn-dashboard` |
| | FN-TI-02.02/02.03 Phát hành biểu mẫu, theo dõi nộp | `GET /v1/extension/report-batches` |
| | FN-XA-03.03 Quản lý HTX & nông hộ | `GET /v1/kn/producer-records?kind=cooperative` |
| CNTS v4.0 | FN-13 Theo dõi công việc cấp xã | `GET /v1/extension/cnts/period-tracking` |
| | FN-11 Báo cáo dịch bệnh & báo nghi dịch | `GET /v1/extension/cnts/outbreaks` |
| | FN-18 Bản đồ vùng nuôi, vùng dịch | `GET /v1/extension/cnts/map/layers` |
| KHTC v2.3 | FN-TI-02.01 Trang chủ tổng hợp | `GET /v1/extension/khtc/dashboard` |
| | FN-TI-02.03 Kỳ báo cáo và tiến độ nộp | `GET /v1/extension/khtc/periods` |
| | FN-AD-04.01 Danh mục KHTC | `GET /v1/kn/khtc/catalogs` |
| Báo giá v2.0 | FN-AD-04.03 Danh mục mặt hàng, nhóm hàng | `GET /v1/price/items` |
| | FN-AD-04.02 Danh mục xã báo giá | `GET /v1/price/points` |
| | FN-TI-02.05 Duyệt bảng giá | `GET /v1/price/boards` |
| HCTH v4.0 | FN-06 Dashboard nhân sự | `GET /v1/hrm/dashboard` |
| | FN-07 Quản lý hồ sơ nhân sự | `GET /v1/hrm/staff` |
| | FN-13 Contact | `GET /v1/contacts/units` |

Kiểm âm 404/403 cho bề mặt đã gỡ: lúc gói này gộp `origin/dev` **chưa có** #264 (KN-STANDALONE-1-ADMIN-TOOLS) và #271
(KN-STANDALONE-5-TT-DATA) nên chưa thêm; ghi ở #310.

### Giới hạn đã biết

- Kiểm khói đọc (GET) với tenant rỗng; chưa chạy luồng ghi của từng FN và chưa mở trình duyệt (đăng nhập authorization
  code qua `kn-web`) — #310.
- Trồng trọt FN-XA-03.04 (máy móc) và FN-XA-03.05 (thửa ruộng) còn dựa `pf/mechanization`, `pf/cooperative` cho tới khi
  #271 gộp; với tenant KN mới hai FN này chưa chạy. Thêm vào kiểm khói ở #310.
- Chặn mạng ra ngoài làm ở runner CI bằng `iptables` chuỗi `DOCKER-USER`; chạy cục bộ không tự chặn.
- Tài khoản do `pf/accounts` tạo cần email kích hoạt; profile `kn` không có SMTP nên kiểm khói đặt mật khẩu qua Keycloak
  Admin REST thay bước bấm liên kết (chỉ Local/CI).

### Kiểm chứng và cách chạy

- `go test ./cmd/api-platform -run TestKNStandaloneRealm` — realm `kn`.
- `.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -v ./tools/knsmoke/...` — cùng mã kiểm khói trên gốc lắp ráp
  thật và PostgreSQL thật (issuer giả).
- Phép làm sai có kiểm soát: bỏ `"pf/mrv"` khỏi `seedDisable` → kiểm thử đỏ ở hai mục (tập module đã tắt,
  `GET /v1/mrv/catalog`); khôi phục → xanh.
- CI: job `kn-standalone`; job toàn stack `compose-smoke` không đổi.
- Cục bộ: [docs/dev/kn-standalone.md](../../dev/kn-standalone.md).
