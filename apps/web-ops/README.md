# Web vận hành AgriGreen (`apps/web-ops`)

Next.js 16 (App Router, TypeScript strict). Người dùng: cán bộ khuyến nông TW / Tỉnh / Xã, Cục Kinh tế hợp tác (cổng Bản đồ CGH, chỉ xem) và quản trị nền tảng.

## Chạy nhanh

Cần Node 24 + npm.

```powershell
cd apps/web-ops
npm ci
npm run dev:mock      # dữ liệu giả lập, không cần backend/Keycloak → http://localhost:3000
```

Ở chế độ giả lập, trang `/login` cho chọn vai thử: TTKN Quốc gia, TTKN tỉnh Đồng Tháp, KNV xã, Cục KTHT, quản trị (đặt & duyệt tham số), người phê duyệt.

Chạy với backend thật và Keycloak (`deploy/compose`):

```powershell
Copy-Item .env.example .env.local   # sửa URL nếu cần
npm run dev
```

| Lệnh | Việc |
|---|---|
| `npm run lint` | ESLint (`eslint-config-next`, có quy tắc a11y và React hooks) |
| `npm run typecheck` | `tsc --noEmit` |
| `npm test` | Vitest + Testing Library |
| `npm run build` | Build production (`output: standalone`) |
| `npm run gen:api` | Sinh lại `src/api/generated/application.ts` từ `contracts/openapi/application.yaml` |
| `npm run check:api` | Báo lỗi nếu kiểu sinh ra lệch với hợp đồng (dùng trong CI) |

## Biến môi trường

Mọi biến `NEXT_PUBLIC_*` được **nhúng lúc build**. Đổi môi trường thì build lại image.

| Biến | Mặc định | Ý nghĩa |
|---|---|---|
| `NEXT_PUBLIC_API_MOCK` | `0` | `1` = backend giả lập trong trình duyệt (dữ liệu tổng hợp, không phải dữ liệu thật) |
| `NEXT_PUBLIC_API_BASE_URL` | `http://localhost:8080` | Application API |
| `NEXT_PUBLIC_ADMIN_API_BASE_URL` | = API_BASE_URL | Admin API (bắt buộc MFA): đề xuất/phê duyệt tham số |
| `NEXT_PUBLIC_OIDC_ISSUER` | `http://localhost:8081/realms/agrigreen` | Issuer Keycloak |
| `NEXT_PUBLIC_OIDC_CLIENT_ID` | `agrigreen-web-ops` | Client công khai, PKCE S256 |
| `NEXT_PUBLIC_MAP_STYLE_URL` | rỗng | Style MapLibre của nhà cung cấp tile trong lãnh thổ (ADR-0007). Rỗng = nền trơn, không gọi tile ngoài |

## Đăng nhập

- OIDC Authorization Code + PKCE (S256) qua `oidc-client-ts` (Apache-2.0).
- Access/ID/refresh token chỉ nằm trong **bộ nhớ** của tab. Không dùng localStorage hay cookie đọc được bằng JS.
- `sessionStorage` chỉ giữ `state` và `code_verifier` tạm thời của một lượt chuyển hướng.
- Tải lại trang thì đăng nhập im lặng (`prompt=none`, trang `/login/silent`) qua phiên SSO của Keycloak. Token được gia hạn tự động.
- `redirect_uri` = `<origin>/login/callback`, `post_logout_redirect_uri` = `<origin>/login`.
- Vai lấy từ `realm_access.roles`, `tenant_id` và `org_ref` từ claim của access token. Cấp phạm vi (TW / Tỉnh / Xã / Cục) và danh sách đơn vị lấy từ `GET /v1/me`.
- Menu chỉ **ẩn/hiện** theo vai và phạm vi. Mọi quyền nghiệp vụ do backend kiểm (R-55).

## Gọi API

- Kiểu sinh từ OpenAPI (`openapi-typescript`), client `openapi-fetch` (MIT). Xem `src/api/client.ts`.
- Mọi POST tự có `Idempotency-Key` (UUIDv7). Form giữ nguyên khóa khi thử lại cùng một thao tác và sinh khóa mới khi dữ liệu đổi.
- Lỗi `application/problem+json` được parse thành `ApiProblem` (`src/api/problem.ts`). Giao diện hiện thông báo tiếng Việt theo `code`, kèm `trace_id`.
- Phân trang cursor: `usePagedList` (nút "Tải thêm") và `collectPages`.
- Route **chưa có** trong `application.yaml` được khai tạm ở `src/api/pending/*.ts`. Tệp `paths.ts` viết đúng dạng openapi-typescript để vẫn kiểm được kiểu. Khi backend đưa route vào OpenAPI: chạy `npm run gen:api`, rồi xoá dòng tương ứng trong `pending/paths.ts`.

## Màn hình

| Đường dẫn | Nội dung | Ai thấy |
|---|---|---|
| `/login`, `/login/callback`, `/login/silent` | Đăng nhập OIDC | mọi người |
| `/` | 4 KPI theo phạm vi, huy hiệu "Số liệu tạm tính", tiến độ theo đơn vị | TW, Tỉnh, Xã |
| `/htx`, `/htx/[id]` | Danh sách, tìm kiếm, chi tiết HTX; tab nông hộ chỉ hiện nhãn đã che | TW, Tỉnh, Xã |
| `/ban-do` | MapLibre: ranh giới hành chính, lớp HTX; lớp thửa để chỗ | TW, Tỉnh, Xã |
| `/nhiem-vu` | Nhiệm vụ hỗ trợ, đếm ngược SLA | TW, Tỉnh, Xã |
| `/thu-vien` | Thư viện số | TW, Tỉnh, Xã |
| `/cgh` | Cổng Cục: bản đồ mức đáp ứng, chú giải, 4 thẻ chỉ số, bảng HTX cần ưu tiên. Chỉ xem | Cục KTHT, quản trị |
| `/quan-tri/danh-muc` | Danh mục dùng chung | `platform_admin` |
| `/quan-tri/tham-so` | Đề xuất và phê duyệt tham số; người duyệt phải khác người đặt (R-56) | `platform_admin`, `param_setter`, `param_approver` |
| `/quan-tri/to-chuc` | Cây tổ chức TW → Tỉnh → Xã | `platform_admin` |

Ngưỡng màu CGH nằm ở `src/cgh/coverage.ts`: Đủ ≥ 85%, Cần chú ý 60–<85%, Thiếu < 60%, Thừa ≥ 120%, Chưa có dữ liệu (xám). Phép so sánh dùng số thập phân dạng chuỗi, không qua float.

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

- MapLibre v6 nạp web worker từ tệp riêng. `scripts/copy-maplibre-worker.mjs` (chạy trong `predev`/`prebuild`) chép worker vào `public/maplibre/`. Thư mục này không commit.
- Dữ liệu giả lập ở `src/api/mock/` là **dữ liệu tổng hợp**. Tên tỉnh theo đơn vị hành chính sau sắp xếp 2025. Xã, HTX, mã số và ranh giới lục giác chỉ để minh hoạ. Không có dữ liệu cá nhân thật.
