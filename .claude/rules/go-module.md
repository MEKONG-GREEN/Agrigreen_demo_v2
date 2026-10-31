---
paths:
  - "internal/**/*.go"
  - "cmd/**/*.go"
---
# Go: cấu trúc module và ranh giới

Nguồn: [g1-conventions §1, §6](../../docs/dev/g1-conventions.md), [modules.md](../../docs/dev/modules.md), [package-rules §3](../../docs/process/package-rules.md), AGENTS.md §1. Khi lệch, nguồn thắng.

## Lớp của một module (mẫu: `internal/platform/party`)

| Thư mục | Chứa | Không được |
|---|---|---|
| `port/` | interface + DTO công khai, hằng `Role*`, payload `//ag:event`, khai `//ag:pubview` | — (thứ DUY NHẤT module khác import, R-02) |
| `domain/` | thực thể, quy tắc nghiệp vụ thuần | phụ thuộc hạ tầng; `time.Now()/Since/Until` (nhận `clock.Clock` hoặc thời điểm qua tham số — `agnotimenow`) |
| `app/` | use case, mở giao dịch, kiểm vai và phạm vi | SQL trực tiếp |
| `store/` | SQL của schema module | gọi port module khác |
| `handler/` | HTTP (Gin): Application API, Admin API | logic nghiệp vụ |
| `module.go` | `New(...)`, `RegisterApp(rg)`, `RegisterAdmin(rg)` | — |
| `dbmodule.go` hoặc `module.go` | `var DBModule = db.Module("<vùng>/<module>")` | — |

## Truy cập DB

- Bọc pool **một lần** trong `New`: `db.ForModule(b, DBModule)` → mọi giao dịch chạy dưới vai `r_<module>`.
- Mã mới mở giao dịch bằng `db.WithContextTx(ctx, pool, DBModule, fn)` (tenant lấy từ ctx), hoặc `db.WithModuleTenantTx` khi đã có tenant (consumer, job).
- Không truy vấn thẳng trên pool ngoài giao dịch (vai `app_*` sẽ `permission denied`).
- Trong SQL của mã: cấm `SET ROLE`, `SET SESSION`, `SET <biến>`, `RESET`, `set_config(…, false)` (`agnosessionset`). Biến cục bộ dùng `SET LOCAL` / `set_config(…, true)`.
- Không `DELETE FROM` / `TRUNCATE` trong `internal/platform`, `internal/mkg` (`agnoharddelete`) — "xóa" là trạng thái ngừng dùng hoặc bản ghi bù, kèm lý do.

## Ranh giới (máy chặn: `archlint`, `golangci-lint depguard`, `knboundary`)

- Gọi chéo module chỉ qua `port/` của module kia; vault chỉ qua `internal/vault/identity/client` (R-04).
- `platform/`, `kn/` không import `mkg/`; `mkg/` không import `platform/`, `kn/` (R-03).
- Đọc dữ liệu module khác chỉ qua view `_pub` có hợp đồng trong `contracts/pubview/`, khai `//ag:readpubview <schema>_pub.<tên> v<N>`. Không join/khóa ngoại xuyên schema; tham chiếu module khác là UUID thuần (R-06).
- KN (`internal/kn/*`) độc lập logic: không gọi in-process vào module HTX, không ghi dữ liệu HTX; không thêm điểm dính KN mới vào module dùng chung — dùng port/registry.
- Module `kind: capability`/`kernel` không rẽ nhánh theo mã ứng dụng (`KN`, `HTX`, `CGH`, `SIM`, `MKG`, `NH`, `PF`) — khác biệt giữa ứng dụng đi vào cấu hình (`agnoappname`).
- Đổi import `port/`, sự kiện, vai `Role*`, schema → `go run ./tools/modgen` rồi commit `module.yaml` đã đổi.
- Gốc lắp ráp: `cmd/<đơn vị>/assembly` (api-platform: `cmd/api-platform/assembly/modules.go`). Không sửa `cmd/api-platform/main.go`.
