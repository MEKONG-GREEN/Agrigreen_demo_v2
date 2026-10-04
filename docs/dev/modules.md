# Tệp khai báo module `module.yaml`

Nguồn: [ADR-0027](../adr/0027-bon-tang-module-tep-khai-bao.md), kiến trúc v4.1 §7.1, kế hoạch [W1.1](../kien-truc-v4.1-ke-hoach.md).

Mỗi module `internal/{platform,mkg,vault}/<module>` có `module.yaml` cạnh `module.go`. `internal/shared/*` là thư viện, không có tệp này. Gói `internal/shared/modreg` đọc, kiểm lược đồ và cung cấp registry (`modreg.Load`, `Registry.Get/Modules/ByKind/Dependents/SchemaOwner`) cho W1.2 (đồ thị), W1.3 (`app.yaml`), W1.4 (bật theo đơn vị).

## Trường

| Trường | Loại | Nguồn | Ý nghĩa |
|---|---|---|---|
| `name` | tay | đường dẫn | Tên repo: `pf/<m>`, `mkg/<m>`, `vault/<m>`; phải khớp thư mục |
| `aka` | tay | ADR-0027 mục 2 | Tên v4.1 (`k/*`, `cap/*`, `dom/*`, `mkg/*`), có thể nhiều tên hoặc rỗng |
| `kind` | tay | ADR-0027 | `kernel` \| `capability` \| `domain`. Đổi `kind` cần sửa ADR-0027 |
| `zones` | tay | — | Cụm dữ liệu module chạy: `pf`, `mkg`, `vault`, `ts`. Phải chứa cụm của mọi `owns_schema` |
| `version` | tay | — | Semver `MAJOR.MINOR.PATCH`, bắt đầu `1.0.0`. Tăng MAJOR khi port phá vỡ (R-MOD-06) |
| `depends` | khóa **sinh**, ràng buộc tay | import | Module khác mà mã không phải kiểm thử import (`port/`, vault `client/`). Ràng buộc mặc định `">=MAJOR.MINOR <MAJOR+1"`; sửa tay được giữ |
| `provides.ports` | **sinh** | `port/` (vault: `client/`) | Interface xuất; gói con có tiền tố `sub.Name` |
| `provides.events` | **sinh** | `//ag:event <tên> v<N>` | Sự kiện công bố (`contracts/events`), dạng `<tên>/v<N>` |
| `provides.internal_events` | **sinh** | `//ag:internal-event` | Sự kiện nội bộ (`contracts/events-internal`) |
| `provides.publications` | **sinh** | `//ag:publication` | Van công bố (`contracts/publication`) |
| `provides.data_products` | **sinh** | `//ag:dataproduct` | Sản phẩm dữ liệu Public API (`contracts/dataproduct`) |
| `permissions` | **sinh** | hằng `Role*` trong `port/` | Vai realm module khai và kiểm. Mã quyền × lớp (R-MOD-07) đến ở W7.2 |
| `shares` | tay | — | Bảng chấp nhận đọc chéo đơn vị (ADR-0030). Rỗng đến W4.1 |
| `owns_schema` | **sinh** | migration | Schema `CREATE SCHEMA` tên `<cụm>_<module>` (vault: `vault`), hoặc schema chỉ module này nhắc trong chuỗi SQL (vd `ts_weigh` → `mkg/measurement`, `pf_files` → `pf/sync`) |
| `enable` | tay | — | `always` (lõi, mkg, `pf/extdata`) \| `per_tenant` (bật theo đơn vị, W1.4). `kernel` bắt buộc `always` |

Kiểm của `modreg`: trường lạ là lỗi, giá trị enum, semver, tên duy nhất, `depends` trỏ tới module có thật và ràng buộc thỏa phiên bản hiện có, mỗi schema một chủ, schema khớp `zones`. **Không** kiểm hướng tầng và chu trình (W1.2 `agmodgraph`).

## Sinh lại và kiểm

```powershell
go run ./tools/modgen          # tính lại trường sinh, ghi module.yaml
go run ./tools/modgen -check   # CI: lỗi nếu tệp nào lỗi thời hoặc không hợp lệ
```

`go test ./tools/modgen` chạy cùng phép kiểm. Sau khi thêm import `port/` của module khác, sự kiện, vai `Role*` hay migration `CREATE SCHEMA`, chạy `modgen` rồi commit tệp đã đổi. Module mới: chạy `modgen` để tạo khung, điền `kind`, `zones`, `enable`, `aka` rồi chạy lại.

## Quyết định W1.1

- Tên trường v4.1 là `module:`; repo dùng `name` (tên repo) + `aka` (tên v4.1) theo ADR-0027 mục 1. `owns_schema` là danh sách vì có module sở hữu hai schema.
- `pf/extdata` → `capability`, `aka` rỗng, `enable: always`: proxy khử định danh cho dữ liệu ngoài, không bảng, không dữ liệu đơn vị, dùng cho lớp bản đồ của nhiều ứng dụng.
- `vault/identity` → `kernel`, `enable: always`: kho định danh nằm dưới mọi module chạm tới người (party, accounts, sync, geo, mechanization), cụm riêng (ADR-0021).
- Module `mkg/*` → `enable: always`: vùng nội bộ chỉ phục vụ MKG.
- Phụ thuộc chỉ tính từ mã không phải kiểm thử; import chéo trong `_test.go` không vào `depends`.
