# Template Sub-task

Trường Jira: **Issue type** `Sub-task` · **Parent** Story · **Summary** `[<LOẠI>] <việc cụ thể>` (≤ 80 ký tự) · **Priority** = priority của Story · **Labels** `brd-<mã-hệ>`. Không gán Story point cho Sub-task (điểm nằm ở Story); có thể ghi ước lượng giờ trong mô tả.

Loại: `DB` · `API` · `BE` · `WEB` · `MOBILE` · `TEST` · `DOC` · `OPS` — ý nghĩa ở [decomposition.md §4](../rules/decomposition.md#4-sub-task).

Mô tả (Description):

```markdown
## Việc cần làm
<2–5 dòng: làm gì, ở đâu (module/màn/route/bảng nếu biết)>

## Hoàn thành khi
- [ ] <kết quả kiểm được, gắn với AC của Story: "AC2 có test tương ứng PASS">
- [ ] <cổng kiểm của loại việc — xem bảng dưới>

## Ghi chú
- Ước lượng: <giờ>
- Tham chiếu: <AC/BR của Story, mục BRD>
```

Cổng "Hoàn thành khi" mặc định theo loại (chọn dòng áp dụng, theo AGENTS.md §7–§8):

| Loại | Dòng thêm vào "Hoàn thành khi" |
|---|---|
| `DB` | Migration tạo bằng `migratecheck -new`, forward-only, có RLS; `migratecheck -base origin/dev` xanh |
| `API` | OpenAPI cập nhật; `contractcheck` và `npm run check:api` xanh |
| `BE` | Kiểm số ở server; mã lỗi `problem.*`; test DB `--- PASS` (không SKIP); `archlint`, `golangci-lint` xanh |
| `WEB` | Dùng UI kit chung; lệnh ghi dùng một Idempotency-Key mỗi thao tác; `lint`, `tsc`, `vitest`, `build:mock` xanh |
| `MOBILE` | `flutter analyze --fatal-infos`, `flutter test` xanh; chạy được offline nếu Story yêu cầu |
| `TEST` | Kịch bản phủ mọi AC của Story; chạy được trong CI |
| `DOC` | Tài liệu/roadmap cập nhật; `docscheck` xanh |
| `OPS` | Cấu hình thiếu → fail-closed; không bí mật trong repo |

Ví dụ: `[DB] Bảng hồ sơ HTX và sổ lịch sử thay đổi`, `[BE] API cập nhật hồ sơ HTX, kiểm vốn điều lệ không âm`, `[WEB] Màn sửa hồ sơ HTX`, `[TEST] Test API hồ sơ HTX: quyền, dữ liệu sai`.
