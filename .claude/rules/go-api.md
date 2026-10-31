---
paths:
  - "internal/**/handler/**/*.go"
  - "internal/**/port/**/*.go"
  - "internal/**/app/**/*.go"
  - "contracts/**/*"
  - "docs/errors/**/*.md"
---
# Go: API, quyền, lỗi, sự kiện

Nguồn: [g1-conventions §4](../../docs/dev/g1-conventions.md), [docs/errors.md](../../docs/errors.md), [docs/errors/README.md](../../docs/errors/README.md), [idempotency.md](../../docs/conventions/idempotency.md). Khi lệch, nguồn thắng.

## Route
- Application API gắn qua `RegisterApp(rg)` (nhóm `/v1` đã có OIDC + `Idempotency-Key` cho mọi POST). Admin API qua `RegisterAdmin(rg)` (bắt buộc MFA): dữ liệu chủ, tham số, phê duyệt.
- Route mới → cập nhật `contracts/openapi/application.yaml` hoặc `admin.yaml`, rồi `npm run gen:api` trong `apps/web-ops` (và `apps/web-internal` nếu đổi `mkg-internal.yaml`).

## Quyền và phạm vi
- Kiểm vai bằng `authz.Require(ctx, vai...)` (KN: `authz.RequireKN`). Tên vai của module khai trong `port/`.
- Phạm vi TW → Tỉnh → Xã lấy từ `party/port.ScopeResolver`: lọc đọc theo `scope.Units`, kiểm ghi bằng `scope.Allows(unitCode)`.
- Thay đổi có phê duyệt: người duyệt khác người đặt — `authz.EnsureDistinctApprover` ở `app/` (R-56).

## Lỗi (`application/problem+json`, RFC 9457)
- Trả lỗi qua `internal/shared/problem`; lỗi thô không lộ ra client (`problem.From` che lỗi lạ thành `internal_error`).
- Mã riêng của module dạng `<module>.<lỗi>`. **Mã là hợp đồng:** không đổi nghĩa, không tái sử dụng.
- Mã mới ghi vào `docs/errors/<cụm>-<module>.md` (tiêu đề `### <cụm>/<module> (…)` + bảng `HTTP | code | Khi nào`), **không** thêm vào `docs/errors.md`. Thêm nhãn tiếng Việt trong `apps/*/src/api/problem.ts`. `docscheck` chặn mã trùng.

## Hành vi API
- POST ghi sổ dùng `idempotency.KeyFromContext(ctx)` làm `idem_key`.
- Phân trang bằng cursor (R-53), theo mẫu `ListBySubject` và handler `list` của party.
- Việc chạy > 2 giây trả `202` + `/v1/jobs/{id}` (ghi `job` + outbox, worker xử lý) (R-52).
- Kiểm dữ liệu vào ở server; lỗi theo trường trả `422 validation_failed` kèm `errors[]`.

## Sự kiện
- Tên `pf.<module>.<thực thể>.<động từ quá khứ>`; schema `contracts/events/<tên>.v1.json`; struct payload trong `port/` có chú thích `//ag:event <tên> v1`. Rồi chạy `modgen`, `contractcheck`.
- View công bố: schema `pf_<module>_pub`, tên `v<N>_<tên>`, khai `//ag:pubview` trong `port/pub.go`, hợp đồng cột ở `contracts/pubview/`. Đổi hợp đồng = view `v<N+1>_` mới; không PII.
