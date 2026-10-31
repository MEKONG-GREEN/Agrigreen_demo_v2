---
paths:
  - "apps/web-ops/**/*"
  - "apps/web-internal/**/*"
---
# Web (Next.js)

Nguồn: [apps/web-ops/AGENTS.md](../../apps/web-ops/AGENTS.md), [docs/kn/ui-kit.md](../../docs/kn/ui-kit.md), [idempotency.md](../../docs/conventions/idempotency.md), AGENTS.md §6, §8, §9. Khi lệch, nguồn thắng.

- **Next.js trong repo khác bản bạn biết** (bản 16.x, có thay đổi phá vỡ): đọc hướng dẫn trong `node_modules/next/dist/docs/` trước khi viết mã; tuân thủ cảnh báo deprecation.
- **API:** gọi qua client `openapi-fetch` (`src/api/client.ts`), kiểu từ `src/api/generated/*`. **Không sửa tay `src/api/generated/*`** — đổi `contracts/openapi/*.yaml` rồi `npm run gen:api`; kiểm `npm run check:api`. Menu/mock/OpenAPI gộp theo module sinh bằng `npm run gen:modules`.
- **Lệnh ghi:** dùng `useIdempotentAction` (`src/api/useIdempotentAction.ts`): một khóa cho mỗi thao tác người dùng — bấm lại/thử lại dùng **cùng** khóa, đổi khóa sau thành công hoặc khi dữ liệu form đổi. Không sinh khóa mới mỗi request.
- **Lỗi:** hiển thị qua `ProblemAlert` / nhãn tiếng Việt trong `src/api/problem.ts`; mã lỗi mới của server phải có nhãn ở đây.
- **UI kit bắt buộc cho màn mới:** `NumberField`, `TextField`, `SelectField`, `validateForm` (`components/form.tsx`); `DetailPage` (`components/patterns/detail.tsx`); `DataTable` (`components/patterns/list.tsx`).
  - Ô số không nhận số âm (mặc định); `allowNegative` phải có comment lý do. Giá trị gửi server là chuỗi đã chuẩn hóa từ `validateForm(...).values`.
  - Lưu: `validateForm` trước; không hợp lệ thì hiện lỗi theo trường, không gọi API.
  - Thêm/sửa/xem mở route riêng (`…/them-moi`, `…/[id]`, `…/[id]/sua`); `Dialog` chỉ cho xác nhận xóa, nhập lý do ngắn, thông báo.
- **TypeScript:** cấm `any`; dùng `import type` cho kiểu; không `console.log` (chỉ `console.warn/error` khi cần).
- **Văn bản giao diện tiếng Việt có dấu.**
- **Kiểm:** `npm run check:api`, `npm run lint`, `npx tsc --noEmit`, `npm test` (vitest), `npm run build:mock`. Lệnh ghi mới → test vitest "bấm lại cùng khóa".
