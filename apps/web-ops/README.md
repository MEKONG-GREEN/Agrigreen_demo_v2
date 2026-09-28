# Web vận hành AgriGreen (`apps/web-ops`)

Next.js 16 (App Router, TypeScript strict). Người dùng: cán bộ khuyến nông TW / Tỉnh / Xã, Cục Kinh tế hợp tác (cổng Bản đồ CGH, chỉ xem; xuất báo cáo CGH) và quản trị nền tảng.

## Chạy nhanh

Cần Node 24 + npm.

```powershell
cd apps/web-ops
npm ci
npm run dev:mock      # dữ liệu giả lập, không cần backend/Keycloak → http://localhost:3000
```

Ở chế độ giả lập, trang `/login` cho chọn vai thử:
- TTKN Quốc gia, TTKN tỉnh Đồng Tháp, KNV xã, Cục KTHT;
- quản trị nền tảng đã qua MFA (đặt & duyệt tham số, tài khoản, mẫu báo cáo, xung đột đồng bộ, dự báo CGH);
- người phê duyệt tham số; quản trị tài khoản tỉnh (chỉ cấp `kn_officer`, `coop_manager`);
- quản trị chưa qua MFA (để thử `mfa_required`).

Backend giả lập (`src/api/mock/`) trả đúng hình dạng của hợp đồng thật, tách hai bề mặt Application và Admin, và mô phỏng quy tắc backend (phạm vi, Idempotency-Key, R-56, MFA, 429 kèm `Retry-After`, ma trận cấp vai). Trạng thái giả lập nằm trong bộ nhớ tab: tải lại trang là dữ liệu mới.

Chạy với backend thật và Keycloak (`deploy/compose`):

```powershell
Copy-Item .env.example .env.local   # sửa URL nếu cần
npm run dev
```

| Lệnh | Việc |
|---|---|
| `npm run lint` | ESLint (`eslint-config-next`, có quy tắc a11y và React hooks) |
| `npm run typecheck` | `tsc --noEmit` (gồm cả `tests/e2e`) |
| `npm test` | Vitest + Testing Library (`src/**/*.test.ts(x)`) |
| `npm run build` | Build production (`output: standalone`) |
| `npm run build:mock` | Build production ở chế độ giả lập (`NEXT_PUBLIC_API_MOCK=1`) — dùng cho E2E |
| `npm run e2e:install` | Tải Chromium cho Playwright (một lần, ~150 MB từ CDN chính thức của Playwright) |
| `npm run e2e` | Playwright: `next build` (giả lập) + `next start -p 3100` + chạy `tests/e2e/*.spec.ts`. `E2E_SKIP_BUILD=1` dùng lại bản build có sẵn |
| `npm run gen:api` | Sinh lại `src/api/generated/application.ts` và `admin.ts` từ `contracts/openapi/application.yaml`, `admin.yaml` |
| `npm run check:api` | Báo lỗi nếu kiểu sinh ra (cả hai tệp) lệch với hợp đồng (dùng trong CI) |

## Biến môi trường

Mọi biến `NEXT_PUBLIC_*` được **nhúng lúc build**. Đổi môi trường thì build lại image.

| Biến | Mặc định | Ý nghĩa |
|---|---|---|
| `NEXT_PUBLIC_API_MOCK` | `0` | `1` = backend giả lập trong trình duyệt (dữ liệu tổng hợp, không phải dữ liệu thật) |
| `NEXT_PUBLIC_API_BASE_URL` | `http://localhost:8080` | Application API |
| `NEXT_PUBLIC_ADMIN_API_BASE` | `http://localhost:8082` | Admin API: host riêng (admin.<domain>), bắt buộc MFA. Dùng cho danh mục, tham số, cây tổ chức, nhập tệp, tài khoản, mẫu/lịch báo cáo, xung đột đồng bộ, thiết bị, chạy dự báo CGH |
| `NEXT_PUBLIC_OIDC_ISSUER` | `http://localhost:8081/realms/agrigreen` | Issuer Keycloak |
| `NEXT_PUBLIC_OIDC_CLIENT_ID` | `agrigreen-web-ops` | Client công khai, PKCE S256 |
| `NEXT_PUBLIC_MAP_STYLE_URL` | rỗng | Style MapLibre của nhà cung cấp tile trong lãnh thổ (ADR-0007). Rỗng = nền trơn, không gọi tile ngoài |

## Đăng nhập

- OIDC Authorization Code + PKCE (S256) qua `oidc-client-ts` (Apache-2.0).
- Access/ID/refresh token chỉ nằm trong **bộ nhớ** của tab. Không dùng localStorage hay cookie đọc được bằng JS.
- `sessionStorage` chỉ giữ `state` và `code_verifier` tạm thời của một lượt chuyển hướng (và, ở chế độ giả lập, mã vai thử). Lịch sử xuất ở trang Báo cáo đọc từ `GET /v1/reports/exports`.
- Tải lại trang thì đăng nhập im lặng (`prompt=none`, trang `/login/silent`) qua phiên SSO của Keycloak. Token được gia hạn tự động.
- `redirect_uri` = `<origin>/login/callback`, `post_logout_redirect_uri` = `<origin>/login`.
- Vai lấy từ `realm_access.roles`, `tenant_id` và `org_ref` từ claim của access token. Cấp phạm vi (TW / Tỉnh / Xã / Cục) và danh sách đơn vị lấy từ `GET /v1/me`. Tên hiển thị lấy từ claim `name` (Me không chứa tên người). Tổ chức `dcrd` là Cục KTHT, dùng cổng CGH. Nếu `/v1/me` lỗi, web hiện cảnh báo kèm mã tra cứu và menu chỉ lọc theo vai.
- Menu chỉ **ẩn/hiện** theo vai và phạm vi. Mọi quyền nghiệp vụ do backend kiểm (R-55). Màn hình Admin API hiện hướng dẫn "đăng nhập lại có OTP" khi phiên chưa qua MFA (`MfaNotice`) và khi máy chủ trả `mfa_required`.

## Gọi API

- Kiểu sinh từ OpenAPI (`openapi-typescript`), client `openapi-fetch` (MIT). Có hai client, xem `src/api/client.ts`:
  - `api`: Application API, `generated/application.ts`;
  - `adminApi`: Admin API trên host riêng, `generated/admin.ts`.
- Tên kiểu gọn nằm ở `src/api/types.ts`. Adapter ở `src/api/adapters/` (HTX, ranh giới, bảng điều hành); nghiệp vụ quản trị ở `src/admin/`; báo cáo ở `src/reports/`; thông báo ở `src/notify/`.
- Mọi POST có `Idempotency-Key` (UUIDv7):
  - nơi gọi truyền khóa (kiểu sinh bắt buộc header này);
  - middleware tự thêm khóa nếu nơi gọi thiếu;
  - thử lại cùng thao tác thì giữ khóa, dữ liệu đổi thì sinh khóa mới.
- Số thập phân (diện tích, %, định mức, toạ độ) giữ **chuỗi** từ API tới UI. `formatDecimal` và `compareDecimal` chỉ dùng để hiển thị/so sánh; tiến độ so với mục tiêu tính trên số nguyên đã nhân tỉ lệ (`progressPct`), không gửi lại số float.
- Lỗi `application/problem+json` được parse thành `ApiProblem` (`src/api/problem.ts`). Giao diện hiện thông báo tiếng Việt theo `code`, kèm `trace_id`.
  - `mfa_required` (Admin API): hướng dẫn đăng nhập lại có OTP.
  - `geo.overlap_needs_confirm` và `geo.area_deviation_needs_confirm`: có nút "Xác nhận vẫn lưu", gửi lại với `confirm_overlap: true` và khóa mới.
  - `geo.overlap_cross_coop`: không bỏ qua được.
  - 429 (`notify.nudge_rate_limited`, `reports.export_quota_exceeded`): `ApiProblem.retryAfterS` đọc từ `retry_after_s` hoặc header `Retry-After`; nút "Đồng bộ ngay" khóa và đếm ngược.
- Phân trang cursor theo `next_cursor`: `usePagedList` (nút "Tải thêm") và `collectPages`.
- Tệp tải lên (`src/lib/files.ts`) được nhận dạng theo chữ ký nội dung (.xlsx/.zip, chặn .xls cũ) và gửi **nguyên văn** (CSV/KML/GeoJSON là chuỗi, .xlsx/.zip là byte) với đúng `Content-Type`. Tải xuống: báo cáo qua URL có hạn của `/download`; cây tổ chức qua `GET /v1/orgs/export.xlsx` (`parseAs: "blob"`).
- Web không khai route viết tay. Mọi khoảng trống G-02…G-10 trong `docs/dev/web-backend-gaps.md` đã chuyển sang route/trường thật, phần ghép tạm đã xoá (tra tên đơn vị từng mã, gọi ranh giới từng đơn vị, bảng tên tham số/danh mục viết cứng, cây tổ chức qua Application API).
- Thông báo: chuông ở thanh trên hỏi `GET /v1/notifications/unread-count` mỗi 60 giây (không websocket, tab ẩn thì bỏ lượt). `deep_link` được ánh xạ cố định (`src/lib/deepLink.ts`): `/extension/directives/{id}` → `/chi-dao?id=`, `/documents/{ref}` → `/thu-vien?doc=`, `/extension/tasks/{id}` → `/nhiem-vu?task=`, `/sync/conflicts/{id}` → `/quan-tri/xung-dot?id=`, `/orgs/{ref}` → `/quan-tri/to-chuc?org=`; đường dẫn lạ không điều hướng.
- Báo cáo: `POST /v1/reports/exports` → 202 → hỏi `GET /v1/reports/exports/{id}` mỗi 1,5 giây tới `completed`/`failed` → `GET …/download`. Lịch sử: `GET /v1/reports/exports` — việc xuất của chính người dùng, mới nhất trước, kèm cờ `downloadable`.

## Màn hình

| Đường dẫn | Nội dung | Ai thấy |
|---|---|---|
| `/login`, `/login/callback`, `/login/silent` | Đăng nhập OIDC | mọi người |
| `/` | Bảng điều hành gộp `GET /v1/dashboard`: diện tích gieo sạ so với mục tiêu, HTX/hộ, thửa đã số hoá, nhiệm vụ quá hạn; huy hiệu "Số liệu tạm tính"; tiến độ theo đơn vị con | TW, Tỉnh, Xã |
| `/htx`, `/htx/[id]` | Danh sách (lọc tỉnh/xã, tên địa bàn, số hộ, thửa số hoá), chi tiết HTX; tab nông hộ: nhãn đã che + huy hiệu đồng ý theo mục đích; tab thửa: thửa của HTX và gán thửa nháp trong xã | TW, Tỉnh, Xã |
| `/ban-do` | MapLibre: ranh giới theo cấp (một lời gọi mỗi tỉnh), lớp HTX (toạ độ trụ sở) | TW, Tỉnh, Xã |
| `/nhiem-vu` | Nhiệm vụ hỗ trợ (tên cán bộ/HTX/xã do backend trả), đếm ngược SLA; `?task=` mở chi tiết + lịch sử | TW, Tỉnh, Xã |
| `/chi-dao` | Chỉ đạo khẩn: danh sách, tỷ lệ xác nhận, người chưa xác nhận theo đơn vị; ban hành tới vùng chọn; xác nhận đã đọc | TW, Tỉnh, Xã (ban hành: `kn_province_admin`, `kn_national_admin`) |
| `/bao-cao` | Xuất Excel/PDF: mẫu, phạm vi, kỳ (tháng/quý/năm) hoặc vụ (CGH, đối chiếu vụ A–B), định dạng → việc xuất → tải; nhãn "Số liệu tạm tính"; thông báo hạn mức 429 | TW, Tỉnh, Xã, Cục |
| `/thong-bao` | Hộp thư: chưa đọc/tất cả, đọc, đọc tất cả, mở theo `deep_link` (mở từ chuông) | mọi người đã đăng nhập |
| `/dong-bo` | "Đồng bộ ngay" theo đơn vị hoặc thiết bị (`POST /v1/sync/nudge`), khóa nút + đếm ngược khi 429 | `kn_province_admin`, `kn_national_admin`, `device_admin`, `platform_admin` |
| `/thu-vien` | Thư viện số; lưu/bỏ lưu đọc ngoại tuyến, tab "Đã lưu" (`GET /v1/library`); `?doc=` mở tài liệu | TW, Tỉnh, Xã |
| `/cgh` | Cổng Cục: bản đồ mức đáp ứng theo kết quả cân đối **đã lưu**, chú giải, 4 thẻ chỉ số, bảng HTX cần ưu tiên (Mã HTX, nhãn nguồn); dự báo nhu cầu FN-09 (đọc; chạy bằng Admin API với `cgh_admin`) kèm cảnh báo phương pháp tạm | Cục KTHT, quản trị |
| `/quan-tri/tai-khoan` | Tài khoản cán bộ: danh sách trong phạm vi, tạo (vai theo ma trận `GET /v1/accounts/grants`), khóa/mở khóa (lý do ≥ 20 ký tự), gửi lại liên kết kích hoạt, đổi đơn vị/vai, nhập CSV/XLSX theo dòng, nhật ký | `platform_admin`, `account_admin`, `account_admin_province` |
| `/quan-tri/danh-muc` | Danh mục dùng chung (`GET /v1/masterdata/catalogs` + mục) | `platform_admin`, `masterdata_admin` |
| `/quan-tri/tham-so` | Sổ đăng ký tham số (tên, đơn vị, khoảng hợp lệ) + phiên bản: tạo nháp → gửi duyệt → duyệt/từ chối; tên người đặt/duyệt; người duyệt phải khác người đặt (R-56) | `platform_admin`, `param_setter`, `param_approver` |
| `/quan-tri/to-chuc` | Cây tổ chức (Admin `GET /v1/orgs`), xuất Excel, nhập .xlsx/CSV theo dòng, nhật ký biến động, đổi tên; `?org=` chọn tổ chức | `platform_admin`, `registry_admin` |
| `/quan-tri/nhap-nong-ho` | Nhập nông hộ từ CSV hoặc .xlsx, kết quả theo dòng | `platform_admin`, `registry_admin` |
| `/quan-tri/nhap-thua` | Nhập ranh giới thửa GeoJSON/KML/Shapefile (.zip), xã mặc định, nhận chồng lấn cùng HTX, kết quả theo đối tượng | `platform_admin`, `geo_admin` |
| `/quan-tri/xung-dot` | Xung đột đồng bộ (Admin): lọc SLA/loại/trạng thái, chi tiết hai phiên bản (bản đồ xem trước ranh giới), xử lý giữ phiên bản/bản ghi bù; tab chồng lấn ranh giới (pf/geo) | `platform_admin`, `sync_conflict_resolver` |
| `/quan-tri/mau-bao-cao` | Mẫu báo cáo có phiên bản (tạo phiên bản mới, ngừng dùng) và lịch gửi tuần/tháng, người nhận là tài khoản cán bộ, lịch sử chạy | `platform_admin`, `reports_admin`, `masterdata_admin` |

Mức đáp ứng CGH do backend phân loại và lưu theo vụ. Web hiện đúng `status` và `color` được trả về, không tính lại. `src/cgh/coverage.ts` ánh xạ trạng thái sang nhãn/màu chip và dựng chú giải từ ngưỡng của lần cân đối. Mặc định: Đủ ≥ 85%, Cần chú ý 60–<85%, Thiếu < 60%, Thừa ≥ 120%. Phép so sánh dùng số thập phân dạng chuỗi, không qua float.

## Kiểm thử đầu–cuối (Playwright)

`tests/e2e/*.spec.ts` chạy trên bản build production ở chế độ giả lập, mỗi ca đăng nhập bằng một vai thử (bỏ qua Keycloak):

| Tệp | Ca |
|---|---|
| `navigation.spec.ts` | Đăng nhập qua `/login`; menu theo vai/phạm vi (TW, tỉnh, xã, Cục → `/cgh`, chặn khối vận hành); quản trị chưa MFA → `mfa_required` |
| `dashboard.spec.ts` | Huy hiệu "Số liệu tạm tính", 4 KPI gộp, tiến độ so với mục tiêu |
| `htx.spec.ts` | Chi tiết HTX (tên địa bàn, huy hiệu đồng ý); gán thửa: chồng lấn cùng HTX → "Xác nhận vẫn lưu", chồng lấn HTX khác → không bỏ qua được |
| `params.spec.ts` | Đề xuất tham số, chặn tự duyệt (R-56), người khác phê duyệt |
| `reports.spec.ts` | Xuất báo cáo → việc xuất → tải tệp; mẫu CGH chọn vụ, định dạng theo mẫu |
| `notifications.spec.ts` | Chuông → deep link chỉ đạo/tài liệu/nhiệm vụ; xác nhận đã đọc; đọc tất cả |
| `conflicts.spec.ts` | Xung đột từ thông báo, hai phiên bản + bản đồ, giữ phiên bản; lọc SLA |
| `accounts.spec.ts` | Lý do khóa < 20 ký tự bị chặn, khóa + nhật ký; ma trận cấp vai tỉnh; lỗi máy chủ `accounts.user_exists`; chưa MFA |

CI chạy job `web-e2e` (`.github/workflows/ci.yml`): `npx playwright install --with-deps chromium` rồi `npm run e2e`; khi lỗi tải lên `playwright-report/` và `test-results/`.

## Docker

```powershell
docker build -t agrigreen/web-ops `
  --build-arg NEXT_PUBLIC_API_BASE_URL=https://api.agrigreen.vn `
  --build-arg NEXT_PUBLIC_OIDC_ISSUER=https://auth.agrigreen.vn/realms/agrigreen `
  apps/web-ops
docker run --rm -p 3000:3000 agrigreen/web-ops
```

Image build bằng `node:24-alpine`. Runtime chỉ chứa bản standalone của Next và chạy bằng user `node`, không phải root.

## Ghi chú

- MapLibre v6 nạp web worker từ tệp riêng. `scripts/copy-maplibre-worker.mjs` (chạy trong `predev`/`prebuild`/`build:mock`) chép worker vào `public/maplibre/`. Thư mục này không commit.
- Dữ liệu giả lập ở `src/api/mock/` là **dữ liệu tổng hợp**. Tên tỉnh theo đơn vị hành chính sau sắp xếp 2025. Xã, HTX, mã số, tài khoản cán bộ và ranh giới lục giác chỉ để minh hoạ. Không có dữ liệu cá nhân thật. Giả lập không đóng gói .xlsx: tệp xuất Excel/cây tổ chức ở chế độ giả lập là CSV (tên tệp ghi "gia-lap"); tệp .xlsx tải lên được nhận theo chữ ký và trả kết quả minh hoạ.
- Playwright (`@playwright/test`, Apache-2.0) là devDependency — không vào bản runtime.
