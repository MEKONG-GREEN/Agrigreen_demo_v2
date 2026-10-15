# Thêm một gói (work package) — danh sách việc

Áp cho mọi gói chạy song song trên nhánh riêng, gộp vào `dev`. Mục tiêu: gói chỉ **thêm tệp mới**, không phải gộp tay tệp chung. Giải thích ở [parallel-packages.md](parallel-packages.md).

## Migration, ADR, tài liệu, kiểm thử DB (WH-B)

1. **Migration.** Đặt tên bằng `go run ./tools/migratecheck -new <cụm>_<module>_<tên>` → `migrations/<cụm>/<mốc UTC>_….up.sql`. Không xin số tuần tự. Không sửa tệp đã gộp (R-24).
2. **Trước khi bàn giao / sau mỗi lần rebase lên `origin/dev`:**
   - `go run ./tools/migratecheck -base origin/dev migrations` (hoặc `make gates-base`).
   - Báo "không lớn hơn phiên bản lớn nhất…" → đổi tên tệp của gói sang mốc mới, sửa chỗ tham chiếu tên tệp (kiểm thử, tài liệu).
3. **ADR.** Lấy số trống lớn nhất + 1, thêm dòng vào bảng chỉ mục `docs/adr/README.md`, xóa dòng ở mục "Số đã giữ chỗ" nếu số đó đang giữ cho chính ADR này. `go run ./tools/docscheck`.
4. **Giả định (đợt KN).** Tạo `docs/kn/gia-dinh/<gói>.md`, dòng đầu `## <gói> — <tên> (<migration>)`. Không sửa `docs/kn/gia-dinh-brd-2026-10.md`.
5. **Roadmap (đợt KN).** Tạo hoặc sửa `docs/roadmap.d/kn-2026-10/<gói>.md` (một dòng `- [x] <gói> (<ngày>, <migration>): …`, liên kết `../../…`). Không sửa `docs/roadmap.md`.
6. **Mã lỗi mới.** Ghi vào `docs/errors/<cụm>-<module>.md` (`### <cụm>/<module> …` + bảng `| HTTP | code | Khi nào |`), không thêm mục vào `docs/errors.md`. Nhãn web thêm như cũ.
7. **Kiểm thử DB.**
   - Cụm dùng chung: `. .\scripts\dev-env.ps1`.
   - Cụm riêng (khuyên dùng khi 5432 không ổn định): `.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 <gói>…`.
   - Dòng `INFRA: PostgreSQL unavailable` là lỗi hạ tầng, không phải lỗi mã. Chạy lại khi PostgreSQL sẵn sàng, hoặc dùng cụm tạm. Không dừng/khởi động lại cụm 5432.
8. **Cổng:**
   - `go run ./tools/migratecheck migrations`
   - `go run ./tools/docscheck`
   - `go run ./tools/modgen -check`
   - `go run ./tools/archlint ./...`
   - `go run ./tools/contractcheck`
   - golangci-lint


## Menu, mock và OpenAPI (WH-A)

1. Đọc [quy ước đăng ký module](module-registrations.md). Thêm tệp menu của module, provider mock + tệp registrations, fragment OpenAPI của module; không sửa tay vùng sinh trong party/menu, implementedMenu, mock/handler hay hai hợp đồng đã gộp.
2. Từ `apps/web-ops`: `npm run gen:api`, `npm run check:api`, `npm run typecheck`, `npm test`, `npm run build:mock`. Kiểm mục sẵn sàng có trang thật; ưu tiên mock không trùng và không đổi fallback.
3. Menu đổi có chủ đích: sinh lại golden bằng Go/PG thật; kiểm assertion theo khóa/đường dẫn thay vì tổng số mục. Gói chỉ tái cấu trúc như WH-A phải giữ nguyên từng byte golden và mã API đã sinh.
4. `go run ./tools/knboundary -base origin/dev`: baseline chỉ giảm sau khi đã gỡ ghép nối thực tế; không thêm alias hay đổi phân loại để che tăng. Chạy party/KN77/migration gates trên PostgreSQL thật và đủ cổng tĩnh trước tích hợp dev.
