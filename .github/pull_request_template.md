<!-- Quy tắc đầy đủ: AGENTS.md. PR chỉ vào `dev`, không vào `main`. Liên kết trong mẫu dùng URL đầy đủ (blob/dev) vì thân PR không hiểu đường dẫn tương đối theo tệp. -->

## Ticket

- Issue: #<số> `[<PKG>]` — lane: `kn` / `shared` (ngoại lệ làn: không / có — lý do)
- ADR / đặc tả liên quan:
- Migration mới (tên tệp) hoặc "không có migration":

## Thay đổi

-

## Bằng chứng kiểm thử

<!-- Dán lệnh và dòng kết quả. Test DB phải là `--- PASS`; SKIP tính là trượt. Gói sửa lỗi: kết quả đỏ trên dev trước khi sửa. -->

| Lệnh | Kết quả |
|---|---|
| `go version` (sau khi đặt `GOTOOLCHAIN` = `toolchain` trong `go.mod`) | |
| `gofmt -l` / `go build ./...` / `go vet ./...` | |
| `go run ./tools/archlint ./...` · `modgen -check` · `contractcheck` · `docscheck` | |
| `go run ./tools/migratecheck -base origin/dev migrations` | |
| `go run ./tools/knboundary -base origin/dev` | |
| `golangci-lint run ./...` | |
| `go test -v -count=1 -p 1 <gói>` (dev-env / pg-ephemeral) | `--- PASS` … |
| web: `check:api` · `lint` · `tsc --noEmit` · `vitest run` · `build:mock` | |
| flutter: `analyze` · `test` | |

## Checklist

- [ ] Mọi lệnh `go` (test, cổng, sinh lại) chạy với `GOTOOLCHAIN` = dòng `toolchain` trong `go.mod`, `go version` khớp — Go mới hơn của máy có thể gây lỗi giả (#144) ([AGENTS.md §8](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/blob/dev/AGENTS.md#toolchain-go-đặt-trước-mọi-lệnh-go))
- [ ] Nhánh từ `origin/dev`, không xếp chồng (nếu có: SHA gốc + `git rebase --onto` đã ghi trong issue)
- [ ] Commit tiếng Việt `<PKG>: …`; commit AI có `Co-Authored-By`
- [ ] Không gọi port module khác / I-O ngoài trong DB transaction ([quy ước](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/blob/dev/docs/conventions/transactions-and-io.md))
- [ ] Cấu hình kênh ngoài fail-closed; worker kiểm phụ thuộc; claim nguyên tử ([quy ước](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/blob/dev/docs/conventions/fail-closed-config.md))
- [ ] Lệnh ghi từ web dùng một `Idempotency-Key` cho mỗi thao tác ([quy ước](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/blob/dev/docs/conventions/idempotency.md))
- [ ] RLS fail-closed; số kiểm ở server
- [ ] Tệp sinh đã sinh lại (modgen, `gen:api`, golden menu), không sửa tay
- [ ] Migration forward-only, mốc UTC, xếp sau dev
- [ ] UI dùng UI kit chung
- [ ] Tài liệu gói (`docs/kn/gia-dinh/<PKG>.md`, `docs/roadmap.d/…/<PKG>.md`, mã lỗi) — `docscheck` xanh
- [ ] Không bí mật, không đường dẫn riêng của máy

## Review chéo

<!-- Bên khác chủ gói bình luận một dòng ở đầu dòng (không trong khối code), SHA phải trùng đầu PR; đầu PR đổi thì review lại. -->
`Review OK — <claude|codex|tên> (review chéo tại <sha đầu PR>)`
