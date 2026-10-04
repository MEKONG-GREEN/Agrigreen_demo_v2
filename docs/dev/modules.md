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

## Đồ thị module: `agmodgraph` (W1.2)

Analyzer `agmodgraph` trong `tools/archlint` ([modgraph.go](../../tools/archlint/modgraph.go)) đọc mọi `module.yaml`, mã Go không phải kiểm thử và `migrations/*/*.up.sql`. Chạy cùng các analyzer khác:

```powershell
go run ./tools/modgen -check     # module.yaml khớp mã (chạy trước)
go run ./tools/archlint ./...    # gồm agmodgraph
go test ./tools/archlint -run ModGraph
```

| Quy tắc | Kiểm | Có ngoại lệ |
|---|---|---|
| `cycle` | Đồ thị `depends` không chu trình. Mọi cạnh nằm trong một thành phần liên thông mạnh đều bị báo | có |
| `tier` | Chỉ đi xuống theo `kind`: kernel ← capability ← domain (R-MOD-05). Cùng tầng được phép nếu không chu trình | có |
| `depends` | Mã không phải kiểm thử import package của module khác (`port/`, vault `client/`) thì module đó phải có trong `depends` | **không** — chạy `modgen` |
| `version` | `depends` trỏ tới module có thật, ràng buộc thỏa `version` hiện có | có |
| `schema` | Schema trong `owns_schema` phải được migration `CREATE SCHEMA`; schema migration tạo phải có chủ (trừ cụm `shared` và `ops_schemas`); migration của module A không tạo/sửa/xóa đối tượng (bảng, view, chỉ mục, hàm, trigger, policy, chú thích) trong schema của module B | có |

Migration được gắn module theo tên tệp `<số>_<cụm>_<module>[_…].up.sql` (khớp dài nhất). Tệp không khớp tên mà chỉ đụng schema của **một** module thì gắn module đó (vd `pf 000010_pf_files` → `pf/sync`, `ts 000001_ts_weigh` → `mkg/measurement`). Tệp đụng schema của nhiều module là vi phạm `schema`: migration mới phải tách theo module.

Lỗi được báo ở `module.go` của module liên quan (với `depends`: ở dòng import). Vi phạm không gắn được module nào làm `archlint` dừng với lỗi chung.

### Ngoại lệ

Vi phạm có từ trước W1.2 được liệt kê trong [`tools/archlint/modgraph_exceptions.yaml`](../../tools/archlint/modgraph_exceptions.yaml), khóa `(rule, from, to)`, mỗi mục bắt buộc `reason`, `owner` (gói kế hoạch sẽ gỡ, dạng `W<n>.<m>`) và `until`. Mục `ops_schemas` liệt kê schema vận hành/dbadmin không thuộc module nào (`mkg_monitor`, `ts_monitor`).

- Vi phạm **mới** (không có trong tệp) làm hỏng cổng. Sửa mã; không thêm ngoại lệ để qua cổng.
- Ngoại lệ **lỗi thời** (vi phạm không còn xảy ra) cũng làm hỏng cổng: xóa mục trong cùng PR đã gỡ nó. Vì vậy danh sách chỉ co lại.
- Thêm mục mới chỉ khi có lý do kiến trúc, review như thay đổi hợp đồng (ADR-0027), ghi gói sẽ gỡ. Đổi `kind` của module để gỡ cạnh `tier` cần sửa ADR-0027 mục 2.
- Khi đóng một gói `W<n>.<m>`, mọi mục `owner: W<n>.<m>` phải đã biến mất. Nếu chưa, gói đó gỡ nốt hoặc ghi lại `owner` sang gói khác kèm lý do trong PR.

Tình trạng lúc W1.2 (04/10/2026), 118 mục:

| Quy tắc | Số mục | Gói gỡ |
|---|---|---|
| `cycle` | 77 — hai chu trình: cả 7 module `mkg/*`; 13 module pf (`accounts`, `apiclient`, `cropcycle`, `extension`, `geo`, `masterdata`, `mechanization`, `metering`, `metrics`, `party`, `reports`, `sync`, `waterway`) | W8.5 (mkg, 19), W3.2 (14), W5.2 (13), W3.3 (9), W6.3 (9), W6.2 (7), W7.2 (6) |
| `tier` | 24 cạnh đi lên | W5.2 (5), W9.1 (5, `pf/notify` → nghiệp vụ), W6.2 (4), W3.3 (3), W6.3 (3), W7.2 (3), W3.2 (1) |
| `schema` | 17 — migration tích hợp G3/G4/G5 nhiều schema (mkg 000010, 000014, 000020, 000021; pf 000080); pf 000034 (`pf/sync`) và 000045 (`pf/mrv`) sửa CHECK `pf_notify.notification`; pf 000078 (`pf/mechanization`) đụng `pf_party` | W7.1 (migration theo schema; tệp đã áp không sửa được, R-23) |

Gợi ý tầng (chưa áp, cần sửa ADR-0027): `pf/metering` (`dom/metering`) là điểm kiểm soát xuất dữ liệu duy nhất, được `party`, `apiclient`, `geo`, `notify`, `reports` gọi, còn nó chỉ phụ thuộc `apiclient` (kernel) và `masterdata` (capability). Chuyển sang `kind: capability` (`cap/metering`) gỡ 3 cạnh `tier` (`geo`, `notify`, `reports` → `metering`) mà không thêm cạnh đi lên nào; `kernel` gỡ cả 5 nhưng sinh cạnh đi lên mới `metering` → `masterdata`.
