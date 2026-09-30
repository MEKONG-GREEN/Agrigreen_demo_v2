# Web nội bộ Mekong Green (`apps/web-internal`)

Ứng dụng web của **vùng MKG**: hub/kho, cân và đối soát, thu gom hiện trường; G4: vận tải, đơn hàng, giá thành &
quyết toán, cầu nối kế toán. Chỉ gọi `api-internal`
(`contracts/openapi/mkg-internal.yaml`), không gọi Application/Admin API của nền tảng và không dùng chung mã chạy
với `apps/web-ops` (chỉ chép cấu hình và vài thành phần giao diện chung). Cùng ngăn xếp với web-ops: Next.js 16
(App Router, `output: standalone`), React 19, `openapi-fetch` + kiểu sinh bằng `openapi-typescript`, Vitest, Playwright.

> Next.js bản này có thay đổi phá vỡ — đọc `node_modules/next/dist/docs/` trước khi viết mã (xem `AGENTS.md`).

## Chạy

```
npm ci
npm run dev:mock        # dữ liệu giả lập, http://localhost:3001 — chọn vai ở /login
npm run dev             # thật: api-internal :8081/:8083 + Keycloak :8180 (xem .env.example)
npm run lint && npm run typecheck && npm test
npm run build:mock && npx playwright test   # E2E trên bản build production giả lập (cổng 3101)
npm run gen:api         # sinh lại src/api/generated/mkg-internal.ts khi hợp đồng đổi (CI: check:api)
```

## Đăng nhập và hai bề mặt

- OIDC Authorization Code + PKCE, client công khai **`agrigreen-web-internal`** (realm `agrigreen`, redirect
  `http://localhost:3001/*`). Token chỉ trong bộ nhớ tab; tải lại trang → đăng nhập im lặng qua phiên SSO.
- api-internal không có `/v1/me`: vai và MFA đọc từ claim token (`realm_access.roles`, `amr`) — chỉ để ẩn/hiện,
  backend kiểm quyền (R-55).
- Hai client cùng kiểu sinh: `api` → bề mặt ứng dụng (`AG_HTTP_ADDR`), `adminApi` → bề mặt quản trị
  (`AG_ADMIN_HTTP_ADDR`, **MFA bắt buộc**). Màn hình quản trị (menu gắn `admin: true`: cơ sở & phiên bản, tham số
  rủi ro, dung sai & định mức, sổ thiết bị cân) hiện hướng dẫn đăng nhập lại có OTP khi phiên chưa qua MFA.
- Mọi POST mang `Idempotency-Key` (R-51); lỗi RFC 9457 hiện thông báo tiếng Việt theo mã + `trace_id`.

## Màn hình

| Khối | Đường dẫn | Bề mặt |
|---|---|---|
| Hub | `/hub/co-so` cơ sở + phiên bản + duyệt · `/hub/ton-kho` lô & tồn, bù sai · `/hub/xuat-hang` xuất theo rủi ro · `/hub/nhan-hang` hàng sắp đến (mã chuyến, FM-05) + nhận theo phiếu cân · `/hub/kiem-ke` · `/hub/chuyen-kho` · `/hub/tham-so` hub.risk.v2/hub.weigh.v1 · `/hub/hao-hut` · `/hub/van-cong-bo` | co-so, tham-so: quản trị |
| Cân | `/can/phien-can` · `/can/phieu-can` 3 điểm + phiếu tay CUSTOMER_IN · `/can/nhap-so-khach` · `/can/tham-so` dung sai & định mức · `/can/doi-soat` (nội bộ/thương mại/cuộn↔khối lượng RB-6) · `/can/tranh-chap` · `/can/thiet-bi` sổ thiết bị · `/can/suc-khoe` | tham-so, thiet-bi: quản trị |
| Thu gom | `/gom/lenh-gom` lập/sửa/duyệt/giao/đóng/huỷ · `/gom/dem-cuon` số cuộn theo công đoạn + xác nhận HTX · `/gom/doi-soat` chiều mua · `/gom/kg-cuon` | ứng dụng |
| Vận tải (G4) | `/van-tai/chuyen` chuyến (lọc, chặng + ảnh chụp định tuyến kiểm SHA-256, gán ghe, mốc tài xế, hoàn tất / bản bù, huỷ có lý do, chứng từ chuyến ePOD / e-bill / biên bản có người ký + tải tệp, sổ chuyến) · `/van-tai/phuong-tien` danh mục ghe + lịch sử phiên bản · `/van-tai/canh-bao` chưa cân 48 giờ, đăng kiểm · `/van-tai/tham-so` đề xuất (dispatcher) ≠ duyệt (chief_accountant) | ứng dụng |
| Đơn hàng (G4) | `/don-hang/hop-dong` hợp đồng mua rơm có phiên bản (CT-01..03) + tra điều khoản · `/don-hang/don-mua` đơn mua, phiếu nhận (lệch > 3 % → biên bản + tệp, không chặn), phải trả · `/don-hang/don-ban` đơn bán (duyệt giá/chiết khấu), giao nhận theo CUSTOMER_IN + tệp chứng từ · `/don-hang/tham-so` | ứng dụng |
| Giá thành (G4) | `/gia-thanh/ky` kỳ + đóng kỳ (202 + job) + danh sách job · `/gia-thanh/lo` danh sách lô có giá thành, sổ bút toán, điều chỉnh, phân bổ + tính lại · `/gia-thanh/quyet-toan` quyết toán HTX (duyệt ở bề mặt quản trị) · `/gia-thanh/carbon` chia + danh sách khoản đã ghi · `/gia-thanh/tham-so` | tham-so: quản trị; duyệt quyết toán: quản trị |
| Kế toán (G4) | `/ke-toan/but-toan` bút toán tổng hợp (dòng, Σ Nợ = Σ Có, source_hash, duyệt ở bề mặt quản trị, đẩy lại DLQ, tải CSV/XML kiểm X-Content-SHA256, danh sách job dựng / đẩy) · `/ke-toan/anh-xa` ánh xạ tài khoản có phiên bản | anh-xa, duyệt bút toán: quản trị |

Phê duyệt luôn do **người khác** người đề xuất (R-56): giao diện gợi ý, máy chủ chặn (`self_approval_forbidden`).

## Dữ liệu giả lập (`NEXT_PUBLIC_API_MOCK=1`)

`src/api/mock/*` thay `fetch` của openapi-fetch, theo đúng đường dẫn/hình dạng của hợp đồng và DTO backend
(`internal/mkg/*/handler/dto.go`), mô phỏng MFA bề mặt quản trị, Idempotency-Key, R-56 và quy tắc nghiệp vụ
(`src/lib/reconcile.ts` = measurement.reconcile.v1, `src/lib/rules.ts` FM-03/04, hub.risk.v2). Dữ liệu tất định theo
prototype: 5 800 → 5 700 cuộn, ghe 4 400 cuộn cân 96,8 t bị cờ +10 %; một lô theo tấn, một lô theo cuộn.

Dữ liệu G4 (`src/api/mock/{logistics,orders,costing,acctbridge}.ts`): chuyến FM-05 CH000101 chưa gán ghe (xuống ghe 60 giờ
trước → cảnh báo chưa cân), ghe sắp hết hạn / hết hạn đăng kiểm; hợp đồng Tân Hiệp theo tấn 650 000 đ/t, Vĩnh Bình theo cuộn,
Giồng Riềng chờ duyệt; phiếu nhận 96,8 t ↔ đơn mua 90 t (+7,56 %); đơn bán chiết khấu 5 % cần duyệt; ba kỳ: M-3 đã đóng (bút toán
đẩy lỗi → DLQ), M-2 đã ghi sổ, M-1 mở đã hết tháng (có bút toán điều chỉnh của Kế toán giá thành L → người đó không đóng được).
Danh sách G4 có con trỏ (`src/lib/usePaged.ts`, 5 dòng/trang + "Tải thêm"): 6 lô có giá thành, 6 khoản carbon, 2 job đóng kỳ,
6 job cầu nối, sà lan CT-06789 có 7 phiên bản; thông báo hàng đến sinh khi chuyến có chặng cuối tới cơ sở xuất phát / đến / hủy
(`src/api/mock/arrivals.ts`). Kho tệp giả lập (`src/api/mock/files.ts`) kiểm như server: ≤ 15 MB, PDF / JPEG / PNG / WebP theo
chữ ký nội dung, `sha256` khớp nội dung (422), sai mục đích → 422 `orders.file_purpose`; CH000103 có sẵn một ePOD.
Tiền là chuỗi thập phân, tính bằng BigInt (`src/lib/money.ts`), hiển thị vi-VN không qua float. Vai G4: `van-tai`, `tai-xe`,
`kinh-doanh`, `truong-kinh-doanh`, `ban-hang`, `ke-toan`, `ke-toan-truong` (xem `src/api/mock/personas.ts`).

## Khoảng trống hợp đồng (ghi cho người tích hợp)

- Không có route liệt kê lô (mọi cơ sở, kể cả đã xuất hết), phiên cân, phiếu chuyển, lần nhập số cân, đề xuất cơ sở
  mới chưa duyệt → giao diện nhớ mã vừa dùng trong tab (`src/lib/recent.ts`) và cho dán UUID.
- Một số phản hồi chưa khai lược đồ (tranh chấp, sản lượng, lần nhập, xuất hàng, thao tác tranh chấp) → kiểu ở
  `src/api/types.ts` mô tả DTO backend; nên bổ sung vào `mkg-internal.yaml`.
- Tệp chứng từ: orders (`POST /v1/orders/files` — biên bản chênh lệch phiếu nhận, chứng từ giao nhận) và logistics
  (`POST /v1/logistics/files` — chứng từ chuyến) đã tải thật (SHA-256 tính ở trình duyệt bằng Web Crypto, multipart +
  Idempotency-Key, `file.id` làm `file_ref`, tải về qua `/content` có kiểm X-Content-SHA256 — `src/lib/upload.ts`). Cân
  (`evidence_ref` của phiếu tay, nhập số cân khách, tranh chấp) chưa có API tải tệp trong mkg/measurement → vẫn nhập UUID.
- Tên HTX/thửa ở nền tảng: RB-2 đã chốt (ADR-0020 — bề mặt đối tác `/v1/partner/*` qua cấp quyền chia sẻ), nhưng api-internal chưa có route
  chuyển tiếp danh bạ cho web → giao diện vẫn hiện mã rút gọn.
- api-internal đã bật CORS (`httpx.CORS`, danh sách origin trong `AG_CORS_ORIGINS`, compose đặt `http://localhost:3001`); các màn hình G4 mới chỉ được kiểm với backend giả lập, chưa chạy với api-internal thật.
- G4: `GET /v1/acctbridge/jobs/{id}` và job đóng kỳ được hỏi lại định kỳ (không có đẩy thông báo); danh sách job / thông báo hàng
  đến chỉ làm mới khi thao tác hoặc bấm "Làm mới". Ảnh chụp định tuyến chỉ có mã (`route_snapshot_ref`) trên chặng — băm đọc qua
  route ảnh chụp. `GET /v1/costing/lots` chỉ trả `lot_ref` (không mã lô) → giao diện hiện mã lô đã biết trong tab
  (`src/lib/recent.ts`), còn lại mã rút gọn. Thông báo hàng đến có `facility_ref` / `lot_refs` là mã, tên cơ sở tra từ
  `GET /v1/hub/facilities`.
