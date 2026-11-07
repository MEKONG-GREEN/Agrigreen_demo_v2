## KN-STANDALONE-2-WEB-GATE — phiên chỉ-KN (#263)

Căn cứ duy nhất: BRD 2.0, FN-CH-00.01 (Báo giá v2.0), BR-04 và AC-02/03; phạm vi gỡ S-2 trong kế hoạch KN độc lập đã duyệt 07/11/2026. Đây là hạ tầng điều hướng phục vụ phiên Vai trò + Phòng ban, không thêm chức năng nghiệp vụ.

- Phiên KN đang active chỉ hiển thị đường `/kn/…` được menu của ngữ cảnh cho phép và các trang dùng chung `/login`, `/login/…`, `/chon-vai-tro`, `/thong-bao`, `/khong-co-quyen`. Các tiền tố gần giống không được dùng để đi vòng.
- Gõ trực tiếp `/`, `/htx`, `/chi-dao` trong phiên active trả màn không có quyền, trước khi mount hoặc gọi dữ liệu của component legacy. Đăng nhập/chọn vai vẫn đi vào trang mặc định của vai theo BRD. Khi đổi vai, quyền được tính lại từ active context; có thêm realm role HTX/MKG hoặc platform_admin không mở thêm bề mặt trong phiên KN.
- Chín mục legacy trong `apps/web-ops/src/nav/nav.ts` bỏ TW/Tỉnh/Xã; giữ platform_admin theo quyền có sẵn, giữ `/bao-cao` cho Cục. Cờ ghép menu legacy và helper chỉ dùng bởi cờ đã gỡ. Các màn và dữ liệu HTX/MKG vẫn tồn tại, hoạt động theo quyền của phiên legacy.
- Shell là kiểm hiển thị của client OIDC hiện hữu. Đây không phải cổng phân quyền API: Go API vẫn kiểm token, ngữ cảnh, vai và phạm vi ở backend. Gói S-3 xử lý API legacy; S-1 xử lý các liên kết công cụ nền tảng trong menu KN; S-9 xử lý field-app. Không mở rộng ba phạm vi này trong S-2.
- Không đổi dependency, schema, migration, API, menu registry Go hoặc dữ liệu. Menu registry KN được dùng nguyên trạng; chạy kiểm sinh/parity. Tệp surface map được cập nhật sau khi gói S-10a vào dev.

### Tệp và thứ tự dựng

| Tệp | Bước | Căn cứ |
|---|---|---|
| Không thêm dependency hoặc dữ liệu | 1–2 | HẠ_TẦNG, FN-CH-00.01 |
| `apps/web-ops/src/components/AppShell.tsx`, `apps/web-ops/src/shell/menu.ts` | 3–4 | BR-04: gate hiển thị theo phiên, bỏ ghép menu cũ |
| `apps/web-ops/src/app/login/page.tsx`, `apps/web-ops/src/app/chon-vai-tro/page.tsx` | 4–5 | B3–B4, AC-02/03: về trang mặc định/ngữ cảnh |
| `apps/web-ops/src/nav/nav.ts`, `apps/web-ops/src/config.ts`, `README.md` | 5 | S-2: gỡ chín cấp quyền legacy và cờ |
| `apps/web-ops/src/kn/StandaloneWebGate.test.tsx`, `apps/web-ops/src/nav/nav.test.tsx`, `apps/web-ops/src/shell/shell.test.tsx`, `apps/web-ops/tests/e2e/kn-standalone-web-gate.spec.ts` | 4–5 | Negative URL/refresh/đổi vai; child không mount; HTX/MKG parity |

### Chạy và kiểm

Trong `apps/web-ops`: `npm ci`, `npm run check:api`, `npm run lint`, `npx tsc --noEmit`, `npx vitest run`, `npm run build:mock`. Chạy web bằng `npm run dev:mock`; kiểm trình duyệt bằng `npx playwright test tests/e2e/kn-standalone-web-gate.spec.ts`. Từ gốc worktree, đặt toolchain theo `go.mod` rồi chạy `./scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./tests/kn77 ./internal/kn/extension/menu/...`.
