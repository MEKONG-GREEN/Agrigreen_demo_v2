# Tệp khai báo module `module.yaml`

Nguồn: [ADR-0027](../adr/0027-bon-tang-module-tep-khai-bao.md), kiến trúc v4.1 §7.1, kế hoạch [W1.1](../kien-truc-v4.1-ke-hoach.md).

Mỗi module `internal/{platform,mkg,vault}/<module>` có `module.yaml` cạnh `module.go`. `internal/shared/*` là thư viện, không có tệp này. Gói `internal/shared/modreg` đọc, kiểm lược đồ và cung cấp registry (`modreg.Load`, `Registry.Get/Modules/ByKind/Dependents/SchemaOwner`) cho W1.2 (đồ thị), W1.3 (`app.yaml`), W1.4 (bật theo đơn vị).

## Trường

| Trường | Loại | Nguồn | Ý nghĩa |
|---|---|---|---|
| `name` | tay | đường dẫn | Tên repo: `pf/<m>`, `mkg/<m>`, `vault/<m>`; phải khớp thư mục |
| `aka` | tay | ADR-0027 mục 2 | Tên v4.1 (`k/*`, `cap/*`, `dom/*`, `mkg/*`), có thể nhiều tên hoặc rỗng |
| `hosts` | tay, tùy chọn | W6.6, R-MOD-09 | Năng lực `cap/*` **chưa tách** thành module mà module này đang cài (kiểm kê bên dùng trước quy tắc ba). Chỉ ghi khi khác rỗng; không trùng `aka` của chính module; năng lực đã có module mang `aka` đó thì khai `hosts` là lỗi (`modreg`) — tách xong phải gỡ. `Registry.Hosts(c)` trả danh sách. [Mục W6.6](#năng-lực-chưa-tách-capimport-captask-capexport-w66) |
| `kind` | tay | ADR-0027 | `kernel` \| `capability` \| `domain`. Đổi `kind` cần sửa ADR-0027 |
| `zones` | tay | — | Cụm dữ liệu module chạy: `pf`, `mkg`, `vault`, `ts`. Phải chứa cụm của mọi `owns_schema` |
| `version` | tay | — | Semver `MAJOR.MINOR.PATCH`, bắt đầu `1.0.0`. Tăng MAJOR khi port phá vỡ (R-MOD-06) |
| `depends` | khóa **sinh**, ràng buộc tay | import | Module khác mà mã không phải kiểm thử import (`port/`, vault `client/`) **và dùng ít nhất một định danh tạo phụ thuộc gọi** (interface, hàm, biến, kiểu thường). Import chỉ dùng hợp đồng dữ liệu — hằng (tên sự kiện, tên vai, khóa tham số, giá trị liệt kê) và kiểu payload `//ag:event`/`//ag:internal-event` — không tạo `depends`. Ràng buộc mặc định `">=MAJOR.MINOR <MAJOR+1"`; sửa tay được giữ |
| `consumes.events` | sinh | kiểu payload sự kiện, hằng `Event*` | Sự kiện của module khác mà mã nhắc tới (§7.1 tách `consumes` khỏi `depends`). Không thuộc đồ thị R-MOD-05; `modreg` kiểm mỗi sự kiện do module phát khai trong `provides`, sự kiện nội bộ MKG chỉ cho module vùng `mkg` |
| `provides.ports` | **sinh** | `port/` (vault: `client/`) | Interface xuất; gói con có tiền tố `sub.Name` |
| `provides.events` | **sinh** | `//ag:event <tên> v<N>` | Sự kiện công bố (`contracts/events`), dạng `<tên>/v<N>` |
| `provides.internal_events` | **sinh** | `//ag:internal-event` | Sự kiện nội bộ (`contracts/events-internal`) |
| `provides.publications` | **sinh** | `//ag:publication`, `//ag:pubview` | Van công bố (`contracts/publication`, tên snake) và view công bố `_pub` dạng `<schema>_pub.<tên>/v<N>` (W6.3, [mục dưới](#view-công-bố-_pub-w63)) |
| `provides.data_products` | **sinh** | `//ag:dataproduct` | Sản phẩm dữ liệu Public API (`contracts/dataproduct`) |
| `permissions` | **sinh** | hằng `Role*` trong `port/` | Vai realm module khai và kiểm. Mã quyền × lớp (R-MOD-07) đến ở W7.2 |
| `shares` | tay | — | Bảng chấp nhận đọc chéo đơn vị (ADR-0030), tên logic. Danh mục sinh `catalog_gen.go` mang `shares` (W4.1): cấp phép `intra_platform` chỉ nhận `module_table = <module>:<share>` khi `modreg.Shared` đúng |
| `owns_schema` | **sinh** | migration | Schema `CREATE SCHEMA` tên `<cụm>_<module>` hoặc `<cụm>_<module>_pub` (W6.3) (vault: `vault`; schema lõi `k_ref` → `pf/kref`), hoặc schema chỉ module này nhắc trong chuỗi SQL (vd `ts_weigh` → `mkg/measurement`, `pf_files` → `pf/evidence`). Schema lõi `k_*` có ở mọi cụm của `zones`, chỉ module `kernel` được sở hữu |
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
| `depends` | Mã không phải kiểm thử import package của module khác (`port/`, vault `client/`) thì module đó phải có trong `depends`, trừ import chỉ dùng hợp đồng dữ liệu (hằng, kiểu payload sự kiện — cùng quy tắc với `modgen`) | **không** — chạy `modgen` |
| `version` | `depends` trỏ tới module có thật, ràng buộc thỏa `version` hiện có | có |
| `schema` | Schema trong `owns_schema` phải được migration `CREATE SCHEMA`; schema migration tạo phải có chủ (trừ cụm `shared` và `ops_schemas`); migration của module A không tạo/sửa/xóa đối tượng (bảng, view, chỉ mục, hàm, trigger, policy, chú thích) trong schema của module B | có |

Migration được gắn module theo tên tệp `<số>_<cụm>_<module>[_…].up.sql` (khớp dài nhất). Tệp không khớp tên mà chỉ đụng schema của **một** module thì gắn module đó (vd `pf 000010_pf_files` → `pf/evidence`, `ts 000001_ts_weigh` → `mkg/measurement`). Tệp đụng schema của nhiều module là vi phạm `schema`: migration mới phải tách theo module.

Lỗi được báo ở `module.go` của module liên quan (với `depends`: ở dòng import). Vi phạm không gắn được module nào làm `archlint` dừng với lỗi chung.

### Ngoại lệ

Vi phạm có từ trước W1.2 được liệt kê trong [`tools/archlint/modgraph_exceptions.yaml`](../../tools/archlint/modgraph_exceptions.yaml), khóa `(rule, from, to)`, mỗi mục bắt buộc `reason`, `owner` (gói kế hoạch sẽ gỡ, dạng `W<n>.<m>`) và `until`. Mục `ops_schemas` liệt kê schema vận hành/dbadmin không thuộc module nào (`mkg_monitor`, `ts_monitor`). `k_ref` thuộc `pf/kref` từ W2.5.

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

## Ứng dụng: `app.yaml` (W1.3)

Ứng dụng (ADR-0027 mục 3, v4.1 §7.2) là tổ hợp module người dùng thấy, khai ở `app/<APP>/app.yaml`, viết tay. Đủ bảy ứng dụng: `KN`, `HTX`, `CGH`, `SIM`, `MKG`, `NH`, `PF`. Đơn vị triển khai là nhị phân `cmd/<đơn vị>`; một đơn vị phục vụ nhiều ứng dụng. Thư mục `app/` (khai báo) khác `apps/` (mã giao diện).

| Trường | Bắt buộc | Kiểm |
|---|---|---|
| `app` | có | thuộc bảy tên, trùng tên thư mục |
| `description` | có | không rỗng |
| `modules` | có | tên module repo → ràng buộc phiên bản; module có `module.yaml`, ràng buộc thỏa `version`. Không dùng ký tự đại diện (`k/*` của v4.1) |
| `roles` | có | vai realm (seed vai, R-MOD-07); phải có trong `deploy/compose/keycloak/agrigreen-realm.json` |
| `context_roles` | không | vai ngữ cảnh phiên không phải vai realm (KN P1: `kn_tw`, `kn_tinh`, `kn_xa`, `admin`) |
| `landing` | không | vai (thuộc `roles` hoặc `context_roles`) → trang đích, bắt đầu bằng `/` |
| `deployment_units` | có | có `cmd/<đơn vị>/main.go` |
| `frontends` | không | thư mục `apps/<x>` có thật |
| `clients` | không | client OIDC có trong realm |

`go run ./tools/modgen -check` (và `go test ./tools/modgen`, `./internal/shared/modreg`) kiểm thêm, không có ngoại lệ:

- mỗi `cmd/<đơn vị>` chỉ **ghép** module thuộc hợp `modules` của các ứng dụng liệt kê nó trong `deployment_units`. "Ghép" là mã không phải kiểm thử của `cmd/<đơn vị>` (kể cả `cmd/internal/*` nó import) import gói của module **trừ** `port/` và vault `client/`: port là hợp đồng, client của vault là gọi mạng sang cụm khác;
- mọi `cmd/<đơn vị>` có `main.go` phải được ít nhất một ứng dụng liệt kê (cả job vận hành: `migrate`, `objsync`, `drill`, …);
- mọi module có `module.yaml` thuộc ít nhất một ứng dụng;
- mỗi module của một ứng dụng được ít nhất một đơn vị của ứng dụng đó ghép.

Lúc W1.3 (04/10/2026):

| App | Module | Đơn vị | Giao diện |
|---|---|---|---|
| KN | 12 | `api-platform`, `worker`, `formimport` | `web-ops` (KN v7.7, gồm phòng ban Trồng trọt), `field-app` |
| HTX | 11 | `api-platform`, `worker` | `htx-app`, `web-ops` `/htx` |
| NH | 10 (gồm `vault/identity`) | `api-platform`, `worker`, `identity-vault` | `htx-app` (chế độ nông dân) |
| CGH | 6 | `api-platform`, `worker`, `drill` | `web-ops` `/cgh` |
| SIM | 6 | `api-platform`, `worker` | `web-ops` `/mo-phong`, `/quan-tri/thuy-he` |
| MKG | 7 (`mkg/*`) | `api-internal`, `worker-internal`, `ingest`, `edge-agent`, `rebuilddrill` | `web-internal`, `mkg-field-app` |
| PF | 15 | `api-platform`, `api-public`, `worker`, `identity-vault`, `outbox-publisher`, `migrate`, `objsync`, `drill`, `seed-synthetic` | `web-ops` `/quan-tri` |

Danh sách module lấy từ menu `web-ops` (`src/nav/nav.ts`), tiền tố API mà `web-ops`, `field-app`, `htx-app`, `mkg-field-app`, `web-internal` gọi, và import của `cmd/*`. `landing` KN lấy từ `accounts/domain.DefaultRoute` (trùng `party/domain` menu); các vai khác theo `homeFor` của `web-ops`. Phòng ban KN là cấu hình của `pf/extension`, không phải module (ADR-0027 mục 6). Bật module theo đơn vị là W1.4.

## Bật module theo đơn vị (W1.4)

R-MOD-08 (kiến trúc v4.1 §7.3): module chưa bật cho đơn vị thì route trả `module_not_enabled`, quyền liên quan không cấp được, menu không hiện. Migration **pf 000088**.

**Đơn vị = tenant.** "Đơn vị tham gia" của v4.1 là tenant (RB-2: cách ly theo đơn vị tham gia bằng `tenant_id`), khớp tên chính sách `enable: per_tenant`. Phòng ban KN (`pf_party.department`, ADR-0027 mục 6) là cấu hình riêng, lọc menu thêm sau bước này.

| `enable` | Hành vi |
|---|---|
| `always` | Luôn bật. Không ghi sổ được (`party.module_always_on`), không có middleware. Mọi `kernel`, `mkg/*`, `pf/extdata` |
| `per_tenant` | **Mặc định bật**: không có dòng chiếu = bật. Chỉ tắt khi Admin ghi sự kiện `disabled` |

**Lựa chọn nạp mặc định:** không nạp dữ liệu. Mọi tenant hiện có và tenant mới giữ nguyên hành vi trước W1.4; sổ chỉ có dòng khi quản trị tắt (hoặc bật lại) một module. Muốn tenant mới mặc định tắt một module thì cần gói sau (ghi sự kiện khi tạo tenant), không đổi nghĩa "không có dòng".

**Dữ liệu.** `pf_party.module_enablement_event` (sổ chỉ ghi thêm, seq, chuỗi băm, RLS; `status`, `prev_status`, lý do bắt buộc ≥ 10 ký tự khi tắt) và `pf_party.module_enablement_current` (chiếu, cùng giao dịch, R-21). Không phê duyệt hai người, cùng mức với trạng thái phòng ban: Admin API có MFA, vai `platform_admin` hoặc `registry_admin`.

**Danh mục lúc chạy.** Nhị phân không mang `module.yaml`, nên `modgen` sinh `internal/shared/modreg/catalog_gen.go` (tên, `kind`, `enable`, `permissions`). `modgen -check` hỏng khi tệp này lệch. `modreg.PerTenant`, `modreg.RoleBlocked` dùng nó.

**API.**

| Bề mặt | Route | Ghi chú |
|---|---|---|
| Admin (MFA) | `GET /v1/modules` | Mọi module của danh mục, trạng thái của tenant người gọi |
| Admin (MFA) | `POST /v1/modules/{zone}/{name}/enablement` | `{enabled, reason}`, Idempotency-Key; 201, gọi lặp 200 |
| Admin (MFA) | `GET /v1/modules/{zone}/{name}/log` | Khung Log: người, thời điểm, cũ → mới, lý do |
| App | `GET /v1/me/modules` | Mọi người đã xác thực; web-ops dùng để ẩn màn |

**Cổng route.** `cmd/api-platform/assembly` biết route nào thuộc module nào vì chính nó gọi `RegisterApp`/`RegisterAdmin` của từng module (`Modules.Named()`, kiểm thử khớp tên với gói và danh mục). Module `per_tenant` đăng ký route trong nhóm con mang `party.Module.ModuleGate(name)`: tenant đã tắt → 403 `module_not_enabled` (thân có `module`). Route công khai `/v1/public` và middleware phiên của module không qua cổng. Đọc trạng thái có đệm theo tenant 10 giây; ghi trong cùng tiến trình xóa đệm ngay, bản sao khác thấy sau tối đa 10 giây. Chỉ chặn HTTP; port trong tiến trình (lõi gọi masterdata, geo…) không bị chặn.

**Quyền.** `pf/accounts` từ chối vai **thêm mới** khi mọi module khai vai đó trong `permissions` đều `per_tenant` và đã tắt (`accounts.role_module_not_enabled`, 409). Vai có module lõi cùng khai (vd `coop_manager` của `pf/party`) không bị chặn. Gán vai trò KN (`kn_tw`/`kn_tinh`/`kn_xa`) bị chặn khi `pf/extension` tắt.

**Menu.** `GET /v1/menu` bỏ mục có màn thuộc module tắt (`party/domain.MenuModule`: tiền tố đường dẫn → module), mục cha không còn con và nhóm trở nên rỗng. `web-ops` `nav.ts` gắn `module` cho mục menu cũ và lọc theo `GET /v1/me/modules`; không đọc được thì không lọc (backend vẫn chặn).

## Vai DB theo module (W2.3)

ADR-0028, R-DB-02, R-DB-03. Mỗi module pf có vai nhóm `r_<module>` (NOLOGIN, tên = `modreg.DBRole`, vd `pf/content` → `r_content`). Mỗi đơn vị triển khai có một vai đăng nhập NOINHERIT (`app_platform`, `app_worker`, `app_public`, `app_sync`), là thành viên các `r_*` của module đơn vị ghép (`modreg/assembly_gen.go`, `modgen` sinh). Tự vai đăng nhập không có quyền bảng nào; giao dịch của module chạy `SET LOCAL ROLE r_<module>`, nên SQL của module chỉ có quyền của chính nó. `SET LOCAL` hết khi COMMIT/ROLLBACK, an toàn với PgBouncer transaction mode.

**Từ W2.6:**

- Mọi module có schema đã chạy dưới `r_<module>`: 19 pf, 7 mkg, vault/identity (`r_identity`); `r_measurement` có ở cả cụm mkg và ts.
- Mỗi đơn vị mở **một** pool vai đăng nhập:
  - pf: `app_platform`, `app_worker` (cả `drill`, `formimport`), `app_public`, `app_sync`; `app_agg` (pool nhỏ thứ hai của `worker -agg-run`, chỉ `r_agg` — ADR-0031 §3);
  - mkg: `app_internal`, `app_worker_internal` (cả `rebuilddrill`);
  - mkg + ts: `app_ingest`;
  - vault: `app_vault`.
- Idempotency HTTP và consumer không thuộc module chạy `db.Ops` (vai `r_ops`).
- Vai cũ `<module>_app` không còn quyền trực tiếp, chỉ còn qua membership `r_<module>`, và bị xóa ở bản phát hành sau.
- Thứ tự triển khai và lùi bản: [runbook](../runbooks/db-roles-w26.md).

**Chuyển một module sang `r_*`:**

1. Gốc module (`dbmodule.go` hoặc `module.go`): `var DBModule = db.Module("<vùng>/<module>")`. Tên phải có trong registry, không thì panic lúc khởi tạo. Cổng `TestEveryModuleWrapsPoolForModule_ADR0028` hỏng nếu module có `owns_schema` thiếu khai báo này hoặc thiếu `ForModule`.
2. Trong `New` (và mọi hàm xuất nhận `tenant.Beginner`), bọc pool một lần: `app.Service{DB: db.ForModule(b, DBModule), …}`. Mọi `tenant.WithTenantTx`/`FromCtxTx` của module từ đó chạy dưới `r_<module>`. Mã mới dùng `db.WithContextTx(ctx, pool, DBModule, fn)` (tenant lấy từ ctx), hoặc `db.WithModuleTenantTx` khi đã có tenant (consumer, job).
3. Consumer của worker: đặt `module: <pkg>.DBModule` trong `consumer` (`cmd/worker`), để giao dịch inbox cũng chạy dưới `r_<module>`. Ở chỗ khác, đưa `db.ForModule(pool, DBModule)` cho `inbox.Process`/`ConsumeWith`.
4. Grant: migration mới `GRANT … TO r_<module>`, không bao giờ cho vai đăng nhập. Module mới có `owns_schema` tự có vai `r_<module>` (danh sách suy từ `modreg.Catalog`, cụm theo `zones` — W2.6). Đơn vị ghép module (import trong `cmd/<đơn vị>`) thì vai đăng nhập của đơn vị tự thành thành viên sau `migrate bootstrap`.
5. Module pf mới (có `r_*`) phải được migration của nó `GRANT USAGE ON SCHEMA k_ref` + `SELECT` các quan hệ `k_ref` + `EXECUTE k_ref.ctx_*` (R-DB-05) — pf 000091/000092 chỉ cấp cho các `r_*` lúc đó.
6. Kiểm thử tích hợp: dựng module trên vai đăng nhập đơn vị — `env.Pool(t, migrations.PF, dbadmin.AppPlatform)`, hoặc `dbadmin.AppInternal` ở cụm mkg. Toàn bộ bộ kiểm thử phải xanh: vai đăng nhập NOINHERIT chỉ chạm bảng qua `r_*`. Kiểm trực tiếp DB dùng pool vai cũ trong kỳ chuyển cuối, sau đó dùng `env.AdminPool` hoặc `db.ForModule`.
7. Truy vấn ngoài giao dịch, như `pool.Query(…)` thẳng trên pool, sẽ bị `permission denied` khi pool là `app_*`. Chuyển chúng vào `db.WithContextTx`.

**Lint.** `agnosessionset` (archlint) cấm, trong chuỗi SQL của mã `internal/*` và `cmd/*` (trừ `*_test.go`):

- `SET ROLE`, `SET SESSION …`, `SET <biến> = …` và `SET TIME ZONE …` ở đầu câu lệnh;
- `RESET …`;
- `set_config(…, false)`;
- `SET LOCAL ROLE` ngoài `internal/shared/db`.

`migratecheck` cấm tương tự trong migration. Biến cục bộ dùng `SET LOCAL` hoặc `set_config(…, true)`.

**Triển khai:** chạy `migrate bootstrap` (tạo `r_*`, `app_*`, membership) trước `migrate up` (pf 000090/000119, mkg 000035, ts 000004, vault 000003 lỗi nếu thiếu vai hoặc membership). Mật khẩu `app_*` lấy từ `AG_DB_PASSWORD_APP_<VAI>` hoặc `AG_DB_APP_PASSWORD`; cỡ pool từ `AG_DB_MAX_CONNS` (mặc định 20).

**Cổng CI (W2.6):**

- `migrations/login_roles_gate_integration_test.go`, mọi cụm:
  - vai `app_*` không có quyền nào trên bảng;
  - truy vấn thẳng trên pool `app_*` trả 42501, cùng truy vấn trong giao dịch của module chạy được;
  - vai cũ không còn ACL trực tiếp.
- `internal/shared/db/modules_wired_test.go`:
  - mọi module có schema bọc `ForModule`;
  - `cmd/*` không tham chiếu vai đăng nhập cũ.

**Còn lại:**

- xóa vai cũ (bản phát hành sau);
- bỏ `tenant.WithTenantTx` gọi thẳng trong module;
- ~~W9.1 (cổng quyền chéo)~~ — xong, xem [Cổng kiến trúc W9.1](#cổng-kiến-trúc-w91).

## Biến ngữ cảnh trong DB (W2.4)

ADR-0028 §2, ADR-0029, kiến trúc v4.1 §14 và §17.2 (DB-13). Mọi giao dịch mở qua `db.ForModule` (nên cả `db.WithContextTx`, `db.WithModuleTenantTx`) đặt, **trong cùng một câu lệnh** với vai của module, các biến dưới đây bằng `set_config(…, true)`. Biến chỉ sống trong giao dịch, an toàn với PgBouncer transaction mode. Biến không có giá trị trong ctx được đặt rỗng, không bao giờ giữ giá trị của giao dịch trước (`internal/shared/db/ctxvars.go`).

| Biến | Nguồn | Dạng |
|---|---|---|
| `app.tenant_id` | `tenant.WithID` / tham số `WithModuleTenantTx` (đặt bởi `tenant.WithTenantTx`) | uuid |
| `app.node_id` | `Principal.KN.OrgRef` (Nút của ngữ cảnh phiên); không có KN → `Principal.OrgRef` (claim `org_ref`). Có KN mà không gắn Nút (Admin) → rỗng | uuid |
| `app.dept` | `Principal.KN.Department` | mã phòng ban |
| `app.xa_ids` | `Principal.KN.Communes` | literal mảng `{"30280","30283"}`; danh sách rỗng → rỗng |
| `app.purpose` | `authz.WithPurpose(ctx, mã)` — module đã kiểm mục đích (X-Purpose, thân request) thì gắn | mã mục đích |
| `app.actor_ref` | `Principal.ActorRef` (sub của token) | uuid |
| `app.on_behalf_of_ref` | `Principal.KN.Support.OnBehalfOf` (chế độ hỗ trợ, ADR-0037) | uuid |

Mã tham chiếu chỉ nhận `uuid.UUID`, nên không có đường nào đưa tên, SĐT hay email vào biến DB. Consumer và job không có Principal: mọi biến trừ tenant đều rỗng.

**Đọc trong SQL** chỉ qua `k_ref.ctx_uuid('app.…')` và `k_ref.ctx_text('app.…')` (pf 000091, R-DB-08). Cấm `current_setting(…)::uuid`. Hai hàm:

- trả NULL khi biến rỗng hoặc chưa đặt;
- `ctx_uuid` trả NULL, không ném lỗi, khi giá trị không phải UUID dạng chuẩn;
- chỉ nhận tên `app.<chữ thường|số|_>`, tên khác → lỗi 22023;
- được planner inline thành `current_setting(…)`;
- EXECUTE chỉ cấp cho `r_*` (vai cũ `<module>_app` có qua membership).

Mảng xã: `k_ref.ctx_text('app.xa_ids')::text[]`. `app.node_id` và `app.xa_ids` được chính sách `p_scope` của `pf_coop` đọc từ W3.4 (mục "Phạm vi Nút trên `pf_coop`" dưới); `app.purpose` được nhánh chia sẻ `p_share` đọc từ W4.2 (mục "Đọc tại chỗ qua cấp phép").

`db.WithoutNodeScope(ctx, tx, fn)` đặt tạm `app.node_id`, `app.xa_ids` rỗng cho `fn` rồi đặt lại — chỉ cho kiểm toàn vẹn phải nhìn cả tenant (vd chồng lấn thửa giữa HTX), không để trả dữ liệu ngoài phạm vi.

**Người dùng gắn Nút phải có Nút (FD).** `p_scope` coi `app.node_id` rỗng là "cả tenant" (consumer, job, Admin). Vì vậy `db.ForModule` **không mở giao dịch** khi Principal người dùng có vai/cấp gắn Nút mà Nút rỗng — lỗi `db.ErrNodeScopeRequired`, ra ngoài là 403 `node_scope_required` (docs/errors.md). Ánh xạ `db.RequiresNode` (`ctxvars.go`): có ngữ cảnh KN → chỉ `kn_xa` bắt buộc; không ngữ cảnh → `coop_manager`, `kn_officer` bắt buộc `org_ref`, trừ khi kèm `platform_admin`, `kn_province_admin`, `kn_national_admin`. Không Principal → không xét. Thêm vai mới có phạm vi theo Nút cấp HTX/xã: thêm vào `db.NodeBoundRoles`/`NodeBoundContextRoles` (kiểm thử `TestNodeBoundRolesDeclared_FD` đối chiếu `app/*/app.yaml`). Kiểm thử dựng Principal HTX/xã phải đặt `OrgRef` (hoặc `KN.OrgRef`).

## Bản chiếu `k_ref` (W2.5)

[ADR-0029](../adr/0029-k-ref-ban-chieu-loi-moi-cum.md#hiện-thực), kiến trúc v4.1 §17.1–17.3, R-DB-05. Module lõi `pf/kref` (`internal/platform/kref`, `k/ref`) sở hữu schema `k_ref` ở cụm pf (pf 000092) và mkg (mkg 000022, rỗng). `k_ref` là dữ liệu **dẫn xuất**: nguồn là sổ của `pf/party`, `pf/masterdata`.

| Quan hệ | Khóa | Nguồn | RLS |
|---|---|---|---|
| `org` | (tenant_id, ref) | `pf_party.org`: mã, loại, ĐVHC, cha, trạng thái, `source_seq` | theo tenant |
| `node_closure` | (tenant_id, ancestor, descendant) + `depth` | cây `org`; `(x, x, 0)` cho chính Nút | theo tenant |
| `admin_unit` | (tenant_id, code, valid_from) | `pf_masterdata.admin_unit` + `successor_codes` | theo tenant |
| `share_grant_active` | (tenant_id = bên cấp, grant_id) | `pf_party.share_grant` status `active` | bên cấp **hoặc** bên nhận |
| `share_scope` | (viewer_tenant, purpose, module_table, owner_tenant, node_id) | — (W4.2) | theo đơn vị xem |

**Đọc.** Mọi `r_*` có `SELECT` (đọc thẳng bảng, không qua hàm `SECURITY DEFINER` — DB-14). Chính sách RLS của module viết thẳng `EXISTS (SELECT 1 FROM k_ref.node_closure c WHERE c.ancestor = k_ref.ctx_uuid('app.node_id') AND c.descendant = <bảng>.node_id)` (W3.4). Port Go cho module khác đọc `k_ref` chưa có; đọc bằng SQL trong giao dịch của module.

**Ghi.** Chỉ `r_kref` (worker `pf.kref` trong `cmd/worker`, vai đăng nhập `app_worker`). Không module nào khác ghi.

**Sự kiện → `k_ref`.**

| Sự kiện | Chủ | Áp |
|---|---|---|
| `pf.party.org.created` | pf/party (tạo tổ chức) | thêm Nút + bao đóng |
| `pf.party.org.moved` | pf/party (`MoveOrg`) | cập nhật Nút; đổi cha → dựng lại bao đóng cả nhánh |
| `pf.party.org.deactivated`, `reactivated` | pf/party | trạng thái |
| `pf.masterdata.admin_unit.changed` | pf/masterdata (nhập danh mục, seed) | thêm phiên bản / đóng hiệu lực |
| `pf.party.share_grant.approved`, `revoked` | pf/party | thêm / bỏ cấp quyền |

Mọi ảnh chụp mang `seq` của sổ nguồn: bản cũ hơn bản đã áp thì bỏ qua (giao lại, sai thứ tự). Nút hoặc cha chưa có trong `k_ref` → `kref.ErrNotYetProjected`: giao lại, vẫn thiếu thì vào DLQ `pf.kref.dlq` — chạy dựng lại.

**Dựng lại.** `worker -kref-rebuild` dựng lại mọi tenant từ nguồn (vai `party_app`, `masterdata_app`, `app_worker`, `ag_ops_jobs` để tìm tenant). Chạy:

- **một lần sau khi triển khai W2.5** — tổ chức có trước W2.5 chưa từng phát `created`;
- sau mỗi lần phục hồi (thứ tự §34: nguồn trước, `k_ref` sau);
- khi `pf.kref.dlq` có sự kiện.

Diễn tập: `drill kref -confirm-copy` (chỉ trên bản sao, xem [restore-drill](../runbooks/restore-drill.md)).

**Chỉ số.** `ag.k_ref.lag` (histogram giây, nhãn `relation`), từ `recorded_at` của outbox tới lúc áp. Cảnh báo `AgKRefLagHigh`: p99 > 5 giây trong 5 phút (P2).

**Thêm quan hệ hoặc nguồn mới:**

1. Migration của `pf/kref` (tên `<số>_pf_kref_…`): tạo bảng, RLS, `SELECT` cho mọi `r_*`, ghi cho `r_kref`.
2. Thêm `Rel*`, phần chiếu trong `kref.Project`/`Digests`, `snapshotTx`, `rebuildTenantTx`.
3. Thêm interface nguồn trong `kref/port`; module nguồn cài bằng vai của nó.
4. Thêm sự kiện vào `kref.Events`.

## Duyệt bốn mắt `cap/workflow` (W6.1)

Kiến trúc v4.1 §26, R-56. Module năng lực `pf/workflow` (`internal/platform/workflow`, `cap/workflow`, `enable: always`) sở hữu schema `pf_workflow` (pf 000118): sổ `request` (một yêu cầu cho một đối tượng `(kind, subject_ref)`: người lập, người được hưởng) và `decision` (một quyết định cho một yêu cầu: `approved` | `rejected` | `cancelled`). Module không biết đối tượng là gì; `kind` dạng `<module>.<đối tượng>`.

| Bất biến | Ở app/ | Ở DB |
|---|---|---|
| Người duyệt ≠ người lập, ≠ người được hưởng (trừ `cancelled`) | `domain.FourEyes` → `self_approval_forbidden` | CHECK `decision_four_eyes`; người lập/người hưởng của `decision` do trigger `trg_bind_request` chép từ `request` |
| Quyết định phải có yêu cầu cùng tenant | — | trigger → 23503 |
| Một đối tượng một yêu cầu, một yêu cầu một quyết định | idempotent: gọi lặp trả lại bản cũ | `stream_key` cố định + CHECK `seq = 1` (`request_one_per_subject`, `decision_one_per_request`) |
| Từ chối/hủy có ghi chú | `domain.ValidateDecide` | CHECK `decision_note_required` |

**Dùng từ module khác.** Port `workflow/port.Approvals` (`Submit`, `Decide`) ghi **trong giao dịch của bên gọi**, để thay đổi nghiệp vụ và quyết định cùng commit: bên gọi gắn giao dịch đang mở vào ctx bằng `db.JoinTx(ctx, tx)` (hợp đồng `port/` không import driver DB — archlint mục 8), cài đặt lấy lại bằng `db.JoinedTx`; thiếu → `ErrNoCallerTx`. Giao dịch đó chạy dưới vai của bên gọi, nên lối ghi là hai hàm `SECURITY DEFINER` `pf_workflow.submit`/`decide` (thân cố định, `search_path` cố định, RLS vẫn áp vì `FORCE`). Bên gọi chỉ có `EXECUTE` + `USAGE` schema, không quyền bảng. Thêm bên gọi mới: migration mới `GRANT USAGE ON SCHEMA pf_workflow` + `GRANT EXECUTE` hai hàm cho `r_<module>` của nó. `Decide` lập bù yêu cầu nếu đối tượng chưa có (dữ liệu trước khi nối). `Requests.Status` đọc trong giao dịch riêng (`db.WithContextTx`, vai `r_workflow`, tenant từ ctx).

**Module lõi (kernel).** `agmodgraph` cấm kernel import capability. Module lõi cần duyệt bốn mắt khai interface của mình trong `port/` và gốc lắp ráp viết bộ chuyển sang `workflow/port.Approvals` (`pf/party`: `party/port.FourEyes`, `cmd/api-platform/assembly/workflow.go`).

**Bên dùng hiện có** (chạy song song, nguyên tắc 3 — cột/CHECK duyệt cũ giữ nguyên, API và mã lỗi không đổi; kiểm cũ chạy trước):

| Module | `kind` | Yêu cầu | Quyết định |
|---|---|---|---|
| `pf/masterdata` | `masterdata.param_version` | gửi duyệt (người lập = người đặt) | duyệt → `approved`, từ chối → `rejected` |
| `pf/party` | `party.share_grant` | đề xuất cấp quyền | duyệt → `approved`; thu hồi đề xuất chưa duyệt → `cancelled` (thu hồi cấp quyền đã duyệt không thêm quyết định) |
| `pf/forms` (W6.2) | `forms.submission` | nộp một phiên bản bài (người lập = người nộp) | rà soát chấp nhận → `approved`, trả lại → `rejected` (cùng giao dịch với dòng `pf_forms.review`) |

Chưa có route HTTP, sự kiện `pf.workflow.request.decided` (§26) chờ bên tiêu thụ (W6.4; `pf/forms` quyết định trong giao dịch của chính nó nên không cần sự kiện). Vai DB: `r_workflow` là vai nhóm không có vai cũ (`dbadmin.groupOnlyModules`); `cmd/api-platform` mở pool `app_platform` cho giao dịch riêng của module.

## Biểu theo kỳ `cap/forms` (W6.2)

Kiến trúc v4.1 §26, §7.2. Module năng lực `pf/forms` (`internal/platform/forms`, `cap/forms`, `enable: per_tenant`) sở hữu schema `pf_forms` (pf 000121–000122). Tách từ `pf/reports` (mẫu có phiên bản) và `extension/reportcycle` (vòng đời kỳ KN); hai module cũ chưa đổi hành vi.

**Sổ** (mọi bảng là sổ `shared.apply_ledger`; không bảng nào sửa được — trạng thái là phép chiếu):

| Sổ | Một dòng là | Bất biến ở DB |
|---|---|---|
| `template` | một phiên bản biểu (`profile`, `code`; phiên bản = `seq`), `fixture` = biểu mẫu thử | `stream_key = template:<profile>:<code>` |
| `period` | một kỳ đã phát hành (phiên bản biểu cố định, đơn vị nhận cấp lá, Nút chủ) | `trg_bind_template` chép profile/mã/phiên bản; một dòng mỗi kỳ |
| `period_event` | `published` / `closed` / `reopened` / `cancelled` (W11.05, lý do bắt buộc) (`origin` native hoặc mirror) | `trg_transition` (sau `trg_chain`, đầu chuỗi đã khóa): native published → closed ⇄ reopened, sai → AGF01; mirror không trộn với kỳ native |
| `submission` | một phiên bản bài của một đơn vị | `trg_bind_period`: kỳ tồn tại, đơn vị thuộc kỳ, kỳ đang mở hoặc đơn vị trong phạm vi mở lại — sai → AGF02 |
| `review` | rà soát duy nhất của một phiên bản (`accepted` / `returned`) | `trg_bind_submission` chép người nộp; CHECK `review_four_eyes` |
| `period_spec` (W11.05, pf 000140) | đặc tả một kỳ native: `unit_kind` (commune / facility / household / department), `unit_parents` (mã xã cha của cơ sở/hộ), `max_submissions`, `review_flow` (`single`), khóa kỳ W11.03 (`period_kind/key/from/to/rule`); kỳ cũ không có dòng = xã, không giới hạn | `trg_bind_spec`: cùng giao dịch phát hành (`period.recorded_at = now()`), đơn vị cha song song với đơn vị; `trg_submission_limit` trên `submission`: `seq > max_submissions` → AGF03 |
| `submission_meta` (W11.42, pf 000149) | thông tin một phiên bản bài nộp: `base_version` (khóa lạc quan), `nil_report`, `note`, `explanation`, `captured_at`, `late` | `trg_bind_meta`: cùng giao dịch nộp, `base_version = seq − 1` (sai → AGF04), DB tính `late` = giờ máy chủ ghi phiên bản (`submission.event_time`) > `due_at` của kỳ — D8, pf 20261013091503; `captured_at` (giờ thiết bị) chỉ để kiểm toán |
| `sync_feed` (W11.42, không phải sổ băm) | một thay đổi của kỳ native cho con trỏ đồng bộ (`feed_seq`) | trigger `trg_feed` AFTER INSERT trên các sổ; chặn UPDATE/DELETE/TRUNCATE, RLS tenant |
| `held_submission` (W11.05) | một bài offline đến sau khi kỳ đã chốt với đơn vị ("Chờ mở kỳ") | `trg_bind_held`: chỉ khi kỳ đã chốt với đơn vị (chốt, hoặc mở lại một phần không gồm đơn vị) — sai → AGF02 |
| `close_snapshot` | ảnh chụp đóng băng của một lần chốt | `trg_bind_close`: gắn sự kiện `closed` cùng kỳ, tính `content_hash = sha256(content::text)`; một ảnh mỗi lần chốt |

**Cấu hình theo bên dùng** (`forms/port.Profile`, tương đương `cap/forms: {hierarchy, publish_by, close_by}` của app.yaml §7.2 — app.yaml của repo chưa mang cấu hình module nên gốc lắp ráp truyền): `Hierarchy` (cấp trên → lá; lá nộp bài), `Roles` (vai ngữ cảnh KN hoặc vai realm → cấp), `PublishBy`, `CloseBy`, `ReviewBy` (mặc định `PublishBy`), `DefineBy` (vai định nghĩa biểu), `AllowFixtures`, `RequireReviewedBeforeClose`. Cấu hình sai → lỗi lúc khởi động (`domain.ValidateProfile`). KN: `cmd/api-platform/assembly.KNProfile` — `kn_tw`/`kn_tinh`/`kn_xa` → TW/TINH/XA, Tỉnh phát hành, rà soát, chốt và mở lại; Admin KN và `reports_admin` định nghĩa biểu; không cho biểu mẫu thử.

**Quyền.** Phạm vi đơn vị lấy từ ngữ cảnh KN (`KNContext.Scope`, `OrgRef` = Nút); vai realm không có ngữ cảnh chỉ có phạm vi toàn quốc ở cấp cao nhất. Phát hành: cấp `publish_by`, mọi đơn vị nhận trong phạm vi. Nộp: cấp lá, đơn vị trong phạm vi. Rà soát: cấp `review_by`, đơn vị trong phạm vi, người rà soát ≠ người nộp — kiểm ở app/, ở `cap/workflow` (`kind forms.submission`, CHECK `decision_four_eyes`) và CHECK `review_four_eyes`. Chốt/mở lại: cấp `close_by` của Nút phát hành (hoặc toàn quốc). Kỳ, bài nộp, rà soát, ảnh chụp ghi trong giao dịch `r_forms`; mọi thao tác trên một kỳ xếp hàng bằng `pg_advisory_xact_lock` để ảnh chụp không lỡ bài nộp chen giữa.

**Ghi song song (v1 cũ của KN).** `/v1/extension/report-*` giữ sổ `pf_extension.*`, API và mã lỗi. `extension/reportcycle` có `Freezer formsport.Freezer` (gốc lắp ráp nối `forms.Module.Freezer()`): chốt thủ công, tự chốt (job), chốt lại và mở lại kỳ gọi `Freezer.Record` trong CÙNG giao dịch (`db.JoinTx`) → hàm `SECURITY DEFINER pf_forms.mirror` (vai `r_extension` chỉ có EXECUTE). Chốt kèm ảnh chụp `kn.report_period/v1`: mọi công việc (xã × biểu) của kỳ, trạng thái, bài nộp đang tính (`submitted_submission_id` — dòng sổ bất biến), hạn mở lại, các lần tổng hợp của lần chốt. Idempotent theo khóa của bên dùng (`mirror:kn:<idem>`). Lỗi ghi song song làm hỏng thao tác cũ (R-21). Đọc ảnh chụp kỳ KN: `GET /v1/forms/periods/{mã kỳ KN}/snapshots?profile=kn` (cấp trên cấp lá).

**Biểu mẫu thử, Q-01.** 14 biểu thật của KN chưa có (câu hỏi Q-01): `domain.Fixtures()` (`FX-01` tiến độ gieo trồng, `FX-02` sâu bệnh) cài bằng `POST /v1/forms/templates/fixtures`, dòng sổ `fixture = true`. Profile có `AllowFixtures = false` (KN) trả `forms.fixture_template` khi phát hành kỳ trên biểu mẫu thử — bật production chờ Q-01 (định nghĩa biểu thật bằng `POST /v1/forms/templates`).

**Chưa làm:** chuyển kỳ KN sang kỳ native và web-ops sang `/v1/forms`; báo cáo Chi cục đọc ảnh chụp; gộp engine mẫu dạng lưới của `pf/reports` (bố cục, công thức) làm kiểu trường của `cap/forms` (điểm mở rộng `forms.field_types`); `publications` cho `cap/analytics`.

## Module nhân sự `dom/hrm` (W11.21)

[ADR-0050](../adr/0050-dom-hrm-pf-hrm.md). Module nghiệp vụ `pf/hrm` (`internal/platform/hrm`, `dom/hrm`, `kind: domain`, `enable: per_tenant`) sở hữu `pf_hrm` (pf 000147); vai `r_hrm` (không vai cũ, `app_platform` là thành viên). Toàn module cấm float (agnofloat): hệ số, PCCV, % là decimal/`numeric`.

| Thành phần | W11.21 | Gói mở rộng |
|---|---|---|
| `port.Access` | phòng ban chủ, vai ghi (`ManageBy`), vai xem trong phòng ban (`ReadBy`); KN ở `cmd/api-platform/assembly/hrm.go` | W11.22 ma trận quyền hồ sơ (FN-20) |
| `port.Catalog` | `Items(kind)`, `ScaleAt(grade, at)`, `RuleAt(key, at)` (tenant từ ctx) | W11.23 Contact đọc chức vụ |
| Sổ `pf_hrm.catalog_item` | 16 loại danh mục FN-19, phiên bản theo `(kind, code)`, `attrs` kiểm ở domain, `placeholder` | W11.22 hồ sơ tham chiếu `code` |
| Sổ `pf_hrm.grade_scale`, `rule_param` | bảng hệ số `numeric(5,2)[]` + giữ bậc, tham số `promotion`/`achievement_conversion`, `valid_from` | W11.25 tra as-of ngày hưởng, chụp hệ số vào quyết định |
| HTTP | `/v1/hrm/status`, `/defaults`, `/catalogs/{kind}[/{code}/history|revise]`, `/grade-scales[/{grade}[/history]]`, `/rules/{key}` | W11.22–W11.28 thêm route |

Bảng mới của các gói sau: migration riêng tên `<số>_pf_hrm_<việc>.up.sql`, sổ `shared.apply_ledger`, `GRANT SELECT, INSERT … TO r_hrm`. PII (hồ sơ) chỉ qua `vault/identity` (RB-3).

**W11.22 hồ sơ nhân sự (pf 000150, vault 000004).** Sổ `pf_hrm.staff` (phiên bản đầu hồ sơ: mã, loại, `subject_ref`, `account_ref`, tên trường đã đổi), `staff_event` (quá trình có hiệu lực `valid_from` + mức chính xác, thay/hủy bằng `supersedes_id` + lý do), `record_access` (nhật ký xem/xem thẻ TK/xuất, chuỗi theo người xem), `perm_rule` (ma trận FN-20). `domain.Project` chiếu tại một ngày (trạng thái FN-08, đơn vị, chức vụ, ngạch – bậc); hệ số tra `grade_scale` as-of. Ma trận: nhóm vai suy từ `KNContext` (admin / hcth / manager / tinh / xa / tw) × phạm vi none/self/unit/all; "đơn vị" = `KNContext.InternalUnit` khớp đơn vị công tác hoặc đơn vị quản lý cán bộ xã (HS-37); "bản thân" = `account_ref`. PII: định danh (họ tên, SĐT, địa chỉ) + ngăn `hrm.profile` (ngày sinh, giới tính, email, ghi chú) với mục đích `hrm.profile`; ngăn `hrm.sensitive` (TK-01..06) với mục đích `hrm.sensitive` — `vault/identity/client.Profiles` (Amend, PutSection, GetSection; `client.AsProfiles`). Gốc lắp ráp: `hr.UseRecords(ext.Vault, assembly.KNHRMUnits{acc.InternalUnits()}, hrm.RecordsEnabled(os.Getenv, production))` — cờ `AG_HRM_RECORDS` ("1"/"0"; vắng = tắt ở production). Danh sách đọc nhãn che bớt (Display) từng dòng; chi tiết Resolve + GetSection; người gọi cần vai realm `vault_registrar` (Admin cấp) — chưa có vai kho riêng cho HCTH.

**W11.23 Contact.** Port mới `port.Directory` (`StaffDirectory`): mọi hồ sơ chiếu tại hôm nay (mã chủ thể kho, tài khoản, loại, trạng thái, đơn vị, đơn vị quản lý, xã, chức vụ + tên) — KHÔNG áp ma trận FN-20 (Contact có quyền riêng), CÓ áp cổng `AG_HRM_RECORDS` (`hrm.records_disabled`). Gốc lắp ráp nối vào `pf/contact` (`assembly.KNContactStaff`).

## Danh bạ dùng chung `cap/directory` (W11.23)

[ADR-0054](../adr/0054-cap-directory-pf-contact.md). Module năng lực `pf/contact` (`internal/platform/contact`, `kind: capability`, `enable: always`) sở hữu `pf_contact` (pf 20261013230912; sổ `person` — liên hệ ngoài tỉnh, `unit` — thẻ Đơn vị; không PII); vai `r_contact` (`app_platform` là thành viên). Port: `Access` (bộ trường theo vai ngữ cảnh/realm, vai cập nhật — KN ở `cmd/api-platform/assembly/contact.go`), `Staff` (nguồn cán bộ thuộc tỉnh), `Labels` (tên đơn vị nội bộ, tên xã), `Names` (họ tên theo tài khoản — cùng chữ ký `party/port.ActorNames`; `pf/extension` dùng qua `UseContactNames`, Q-072). PII đọc bằng thẻ danh bạ của kho (`client.Directory.Cards`, mục đích `contact.directory`, vai `vault_directory`); ghi liên hệ ngoài tỉnh bằng `Create`/`Amend`/ngăn `contact.card` (vai `vault_registrar`). HTTP `/v1/contacts/access|persons[/{id}[/revise]]|units[/{id}/revise]`; bộ "basic" có DTO riêng — thêm trường vào `basicPersonDTO` là vi phạm BR-03 (kiểm thử `TestContact_FieldFiltering` so khóa và byte).

## Kế hoạch năm KHTC trong `dom/extension` (W11.61)

Gói con `internal/platform/extension/khtc` (schema `pf_extension`, migration pf 20261013070305), phòng ban `ke_hoach_tai_chinh`:

- Sổ `khtc_catalog_item` (Danh mục KHTC, 7 loại), `khtc_program` (danh sách CT/DA chủ — quyết định D20), `khtc_plan_version` (phiên bản theo QĐ, `entries_hash`) + bảng chỉ ghi thêm `khtc_plan_entry` (cả cây mỗi phiên bản).
- Cổng cho module khác: `extension/port.KHTCPrograms` (Program, Programs — CNTS W11.47 tham chiếu `program_ref`) và `extension/port.KHTCPlans` (PlanAt — phiên bản hiệu lực theo ngày, BR-DL-13; W11.62/63/65). Gốc lắp ráp lấy qua `Module.KHTCPrograms()`, `Module.KHTCPlans()`.
- Móc chờ nối: `khtc/app.Service.ClosedCutoff` (W11.62 — chặn ngày hiệu lực trước ngày chốt kỳ đã đóng) và `EntryUsage` (W11.63 — dòng đã có số liệu không xóa).
- `agnofloat` phủ cả gói con (kể cả kiểm thử).

## Khung giá `cap/price` (W11.71)

[ADR-0049](../adr/0049-cap-price-pf-price.md); kiến trúc v4.1 §26. Module năng lực `pf/price` (`internal/platform/price`, `cap/price`, `enable: per_tenant`) sở hữu `pf_price` và `pf_price_pub` (pf 000138); vai `r_price` (không vai cũ, `app_platform` là thành viên), `r_price_pub` (đọc view công bố — chưa có view, `v1_board_daily` ở W11.76). Toàn module cấm float (agnofloat `floatFreeZones`, R-CODE-01): giá, % là decimal/`numeric`.

| Thành phần | Khung W11.71 | Gói mở rộng |
|---|---|---|
| `port.Profile` | tên, phòng ban chủ, vai → cấp (TW/TINH/XA), `ManageBy`; KN ở `cmd/api-platform/assembly/price.go` | W11.72 `StewardDuty` (cờ QTNV); W11.74 giờ khóa, hạn phản hồi |
| `port.Catalog` | `Units`, `Groups` có hiệu lực tại ngày (tenant từ ctx) | W11.73 mặt hàng; KHTC đọc (Q-082) |
| `port.Calendar`, `port.Thresholds`, `port.ReminderTargets` | W11.74: kế hoạch ngày báo giá (`DayPlan.Locked`), ngưỡng mặt hàng → nhóm → mặc định, bộ chọn người nhận nhắc | W11.72/W11.75 cài `ReminderTargets`; W11.75 đọc `Calendar`, `Thresholds` |
| Sổ `pf_price.unit`, `pf_price.commodity_group` | mỗi dòng một phiên bản của một mã (`seq`), `valid_from` (date giờ VN), `status` `active`/`retired` | W11.73 tham chiếu `code` |
| HTTP | `GET /v1/price/status`, `GET /v1/price/catalog?at=`, `POST /v1/price/catalog/defaults` (5 nhóm BRD + 8 ĐVT, chỉ mã chưa có) | W11.72–W11.76 thêm route |
| Sổ W11.74 (pf 000139) | `calendar_rule`, `calendar_exception`, `alert_threshold`, `calendar_run`; job `price_calendar` (`cmd/worker`, mỗi phút, leader) ghi mốc khóa ±1 phút + nhắc → `pf.price.reminder.issued` → `pf.notify.price` | `/v1/price/calendar*`, `/v1/price/thresholds*` |
| Xã báo giá (W11.72, pf 000145) | sổ `pf_price.region_group` (nhóm vùng), `collection_point` (mã `XBG####` hệ thống sinh; ↔ ĐVHC cấp xã có hiệu lực đọc từ `k_ref.admin_unit`), `point_officer` (`xa` / `tinh` theo `valid_from`); `port.Points` (`PointsAt`, `PointAt`, `Point.HandledBy`), `port.Officers` (gốc lắp ráp: `assembly.PriceOfficers` ← `pf/accounts`); `Profile.StewardDuty` = cờ QTNV; `app.PointReminders` = `ReminderTargets` mặc định; `app.PointHooks.Submitted` ← W11.75; HTTP `/v1/price/regions*`, `points*`, `admin-units`, `officers` (`handler/point.go`) | W11.75 nối `Submitted`, đọc `Points` để kiểm Cấp Xã/Cấp Tỉnh xử lý xã |
| Phiếu báo giá (W11.75, pf 000148) | sổ `pf_price.ticket` (một phiếu còn hiệu lực / xã / ngày, khóa lạc quan theo `seq`), `ticket_line` (min/max `numeric`, "Không có hàng", giá hiệu chỉnh `adj_*`), `price_alert` (Tạm tính → xác minh → chốt; trao đổi = lịch sử phiên bản); trạng thái "Không nộp", "Đã khóa", "Chờ rà soát", "Quá hạn phản hồi" chiếu theo `DayPlan.Locked` / hạn phản hồi lúc đọc (không job); `app.TicketService` (`port.Tickets.Day` cho W11.76/W11.77), `ItemHooks.HasPrice/HasSheets` + `PointHooks.Submitted` nối trong `price.New`; `app.TicketHooks.Board` (W11.76); `port.TablePDF` ← `pf/reports`; HTTP `/v1/price/tickets*`, `/v1/price/alerts*` (`handler/ticket.go`) | W11.76 nối `Board`, đọc `Tickets.Day` cho bảng giá ngày |
| Bảng giá ngày (W11.76, pf 000151/000152) | sổ `pf_price.board` (Chưa duyệt / Đang duyệt — khóa 10 phút một người / Đã duyệt vN / Đang mở duyệt; khóa tuần + `days_scheduled` lưu lúc duyệt), `board_line` (CT-04 toàn tỉnh của một phiên bản), `board_ticket` (phiếu của phiên bản); `port.Boards.Published`, `port.Weeks` (← `pf/extension.ReportWeekOf`, `assembly.PriceWeeks`); sự kiện `pf.price.board.published` v1; `TicketHooks.Board`/`Approved` nối trong `price.New`; view `pf_price_pub.v1_board_daily`, `v1_ticket_week`; HTTP `/v1/price/boards*` (`handler/board.go`) | W11.77 đọc `Boards`; W11.81 nghe sự kiện |
| Mặt hàng (W11.73, pf 000141) | sổ `pf_price.item` (mã `MH#####` hệ thống sinh, khóa cố vấn theo tenant), `item_assignment` (`point_code` = mã xã báo giá, lỏng), `item_proposal`, `item_set`; `port.Items` (`ItemsAt`, `PointItems`); `app.ItemService` + `app.ItemHooks` (`HasPrice`, `HasSheets` ← W11.75; `NextDay` ← W11.74; `Steward` ← W11.72), nối bằng `price.Module.SetItemHooks` ở gốc lắp ráp; HTTP `/v1/price/items*`, `groups*`, `assignments*`, `point-items`, `item-sets*`, `proposals*` (`handler/item.go`) | W11.72 nối `point_code` ↔ xã báo giá, `Steward`; W11.75 `HasPrice`/`HasSheets` |

Bảng mới của các gói sau: migration riêng tên `<số>_pf_price_<việc>.up.sql`, sổ `shared.apply_ledger`, `GRANT SELECT, INSERT … TO r_price`; view công bố mới khai `//ag:pubview pf_price_pub.<tên> v1` trong `price/port` rồi `GRANT SELECT … TO r_price_pub` (kiểm thử `TestPubViewsDeclaredAndPrivileges_W63` đếm số view).

## Cấp phép chia sẻ v2 (W4.1)

ADR-0030 mục 3, kiến trúc v4.1 §17.1, R-SEC-04. pf 000120. Mã ở `internal/platform/party/{app,store,handler}/sharing.go`.

| Việc | Cách làm |
|---|---|
| Điều khoản v2 | `grant_class` (`intra_platform` \| `partner`), `module_table`, `scope_node_ref`, `purpose_code`, `basis_ref` ghi ở **sổ phụ** `pf_party.share_grant_terms` (một dòng mỗi cấp phép, CHECK `seq = 1`, cùng giao dịch với `proposed`) và cột NULL được của bảng chiếu `share_grant`. Sổ `share_grant_event` **không** thêm cột: chuỗi băm (`shared.fn_chain`/`verify_chain`) tính trên `to_jsonb` toàn dòng, cột mới đổi băm của mọi dòng cũ. Quy tắc chung: không `ADD COLUMN` vào bảng sổ đã có dữ liệu — dùng sổ phụ |
| Cấp phép cũ | Dòng chiếu không có `grant_class` = `partner` kế thừa (`legacy_terms = true` ở API) |
| Ai tạo | `platform_admin` của tenant bên cấp (đơn vị chủ). `intra_platform`: `scope_node_ref` phải là Nút đang hoạt động trong `pf_party.org` của chính tenant đó; không `kinds`; đủ mục đích + căn cứ. Duyệt bốn mắt như ADR-0020 (+ `cap/workflow`) |
| `module_table` | Tên logic `<module>:<share>`, vd `pf/cooperative:household`; sai → 422 `party.share_table_not_shared`. W4.2 ánh xạ sang bảng vật lý của `k_ref.share_scope` (`pf_coop.household`, [cooperative.md §7.3](../design/cooperative.md)) |
| Đường đọc | `partner`: như cũ (`share_grant_inbound`, `/v1/partner/*`, Public API, van). `intra_platform`: không ghi `share_grant_inbound`; đọc tại chỗ qua `share_scope` từ W4.2 |
| Rà soát định kỳ | Sổ `pf_party.share_grant_review` (`retain` \| `revoke`, chỉ ghi thêm). Đến hạn khi lần rà soát cuối (hoặc lúc duyệt) + 90 ngày ≤ hiện tại + `within_days`. `GET /v1/share-grant-reviews/due`, `GET\|POST /v1/share-grants/{id}/reviews` (Idempotency-Key). `revoke` thu hồi cùng giao dịch |
| Epoch ngữ cảnh | Trước W4.1 repo chưa có. Bản tối thiểu: `pf_party.context_epoch` (một dòng mỗi tenant đơn vị xem). Thu hồi cấp phép **đang hiệu lực** (thường hoặc khi rà soát) tăng epoch của tenant bên nhận trong cùng giao dịch (`tenant.Within` với bằng chứng bên cấp → bên nhận) và phát `pf.party.context_epoch.bumped` (tenant phong bì = đơn vị xem). Đọc: `party/port.ContextEpochs`. W7.2 (`k/access`) hợp nhất với epoch ở Valkey định danh |
| Sự kiện | `pf.party.share_grant.approved`/`revoked` v1 thêm trường tùy chọn `grant_class`, `module_table`, `scope_node_ref`, `purpose_code`, `basis_ref` (tương thích ngược). `k_ref.share_grant_active` chưa có cột v2 — W4.2 thêm ở migration của `pf/kref` (agmodgraph cấm một migration sửa schema của hai module) |
| Web-ops | Chưa có màn cấp phép để mở rộng — chỉ API (`apps/web-ops/src/api/generated/admin.ts` đã sinh) |

Còn mở: C-10 (thỏa thuận chia sẻ mẫu, chu kỳ rà soát), H-1 (ai là đơn vị chủ HTX/hộ/thửa) — chưa bật production.

## Cổng kiến trúc (W9.1)

Bốn cổng giữ ranh giới module sau W2.6. Hai cổng DB dùng tệp ngoại lệ **chỉ được co lại**: mỗi mục có lý do và gói kế hoạch gỡ (`owner: W<n>.<m>`), hoặc `owner: ADR-0028-write-port` cho điểm vào ghi chéo module lâu dài. Cổng hỏng khi có vi phạm mới, hoặc khi một mục ngoại lệ không còn xảy ra (xóa mục đó).

**1. `agnoappname` (archlint).** Module `kind: capability` và `kind: kernel` không rẽ nhánh theo danh tính ứng dụng:

- Cấm: `==`/`!=`, nhãn `case` của `switch`, `strings.EqualFold`/`bytes.EqualFold` với hằng chuỗi (literal hoặc hằng có tên) trùng mã trong `app/*/app.yaml` (KN, HTX, CGH, SIM, MKG, NH, PF). So khớp không phân biệt hoa thường.
- Miễn: hằng có kiểu đặt tên mà tên kiểu không chứa `app` (enum miền, vd `party/port.OrgKind` `"mkg"` là loại tổ chức); tệp `_test.go`; module `kind: domain`.
- Khác biệt giữa ứng dụng đi vào cấu hình: tham số (`masterdata`), cờ module theo đơn vị (W1.4), cấp quyền, `hierarchy`/`publish_by` (W6.2).
- Lúc thêm W9.1: không vi phạm.

**2. Quyền chéo schema** — `migrations/schema_isolation_integration_test.go`, mọi cụm (pf, mkg, ts, vault) sau khi migrate tới head:

- Không `r_<module>` nào có quyền hiệu lực trên đối tượng thuộc schema của module khác (`owns_schema` trong `modreg.Catalog`), trừ `k_ref` và `*_pub`. Xét USAGE/CREATE schema, mọi quyền bảng/khung nhìn/chuỗi số, quyền cột riêng, EXECUTE hàm.
- Quyền có được qua PUBLIC không tính cho `r_*`. Riêng PUBLIC không được có USAGE/CREATE schema hoặc quyền bảng trên schema module (ngoài `k_ref`, `*_pub`).
- Schema không thuộc module (`shared`, `public`, schema vận hành) ngoài phạm vi.
- Ngoại lệ: `migrations/schema_isolation_exceptions.yaml`. Lúc thêm W9.1: `r_masterdata`, `r_party` → `pf_workflow` (USAGE + EXECUTE `submit`/`decide`, hàm `SECURITY DEFINER` của W6.1). W6.3 không gỡ mà xếp chúng thành điểm vào ghi lâu dài (`owner: ADR-0028-write-port`, xem "Điểm vào ghi chéo module" ở mục `_pub`); W6.2 thêm `r_forms` → `pf_workflow`, `r_extension` → `pf_forms`, W6.4 thêm `r_metrics` → `pf_workflow`.
- Module mới cần quyền chéo: công bố view/hàm trong `pf_<module>_pub` (R-DB-10), không cấp thẳng.

**3. `EXPLAIN` chung cho bảng RLS (DB-14)** — `migrations/rls_explain_integration_test.go`:

- Mọi bảng bật RLS ở mọi cụm (bảng gốc, không phân vùng con): `EXPLAIN (FORMAT JSON, VERBOSE) SELECT * FROM t LIMIT 50` dưới vai đọc được bảng (ưu tiên `r_<module chủ>`, không superuser, không BYPASSRLS), ngữ cảnh mẫu `app.tenant_id`, `app.node_id`, `app.dept`, …
- Hỏng khi hàm do repo định nghĩa xuất hiện trong `Filter`/`Join Filter` của bất kỳ nút nào, hoặc ở bất kỳ biểu thức nào dưới một SubPlan (chạy theo hàng ngoài). Hàm SQL `STABLE` inline được (`shared.current_tenant`, `k_ref.ctx_*`) biến mất khỏi kế hoạch; InitPlan được phép.
- `TestRLSExplainGateDetects_W91` dựng chính sách mẫu (plpgsql trong Filter, EXISTS gọi hàm trong SubPlan, chính sách inline) để chứng minh cổng bắt được.
- Ngoại lệ: `migrations/rls_explain_exceptions.yaml`. Lúc thêm W9.1: 360 bảng, không vi phạm, danh sách rỗng.
- Chính sách mới (W3.4 trở đi): viết thẳng điều kiện, `EXISTS (SELECT 1 FROM k_ref.node_closure …)` không gọi hàm không inline được.

**4. `agnofloat` cho `mkg/fieldops`, `pf/mechanization`.** Hai module trong `floatFreeProdZones`: cấm float trong mã không phải kiểm thử. Tệp `_test.go` miễn vì kiểm thử HTTP giải mã JSON bằng `encoding/json` (số → `float64`) và cố ý gửi số lẻ để kiểm từ chối. Không còn float trong mã sản phẩm của hai module.

**Chạy:** `go run ./tools/archlint ./...`; `go test -count=1 -p 1 -run W91 ./migrations/` (cần `AG_TEST_PG_ADMIN_URL`).

## Tệp minh chứng `cap/evidence` (W6.5)

Kiến trúc v4.1 §18 (R-DB-07), §26; ADR-0027 mục 2. **Chọn module mới** `pf/evidence` (`internal/platform/evidence`, `cap/evidence`, `kind: capability`, `enable: always`) thay vì mở rộng `pf/sync`: bảng ánh xạ ADR-0027 gán schema `pf_files` cho `cap/evidence`, v4.1 tách `cap/evidence` khỏi `cap/sync`, và `Open(purpose)` là năng lực dùng chung (MRV, khuyến nông), không phải việc của động cơ đồng bộ. `pf/evidence` sở hữu `pf_files` (mã SQL chỉ ở `evidence/store`, modgen suy `owns_schema`); `pf/sync` giữ route `/v1/files` (kiểm thiết bị `X-Device-Id`, multipart) và gọi `evidence/port.Files`. Hợp đồng HTTP và mã lỗi `sync.file_*`, `sync.purpose_*`, `sync.consent_missing` giữ nguyên.

| Việc | Quy tắc |
|---|---|
| Bộ ba chủ thể | `(subject_module, subject_type, subject_id)` trên `pf_files.file` (pf 000123), NULL được (tệp cũ — không UPDATE điền ngược; tệp tải trước khi biết thực thể). CHECK `file_subject_triple`: đủ cả ba hoặc không có. Không khóa ngoại xuyên schema |
| `Attach` | Kiểm phong bì, băm bản mã; bộ ba (multipart `subject_module`, `subject_type`, `subject_id`) phải phân giải được qua `Resolver` của module chủ trong tenant người gọi → không thì `evidence.subject_unresolved` (422) |
| `Open(purpose)` / `OpenContent(purpose)` | Vai `vault_evidence_viewer`, mục đích bắt buộc + được phép (R-42), người gọi thấy chủ thể (`Resolver` → mã xã; phạm vi người gọi — ngữ cảnh KN hoặc `ScopeResolver` — chứa xã, hoặc `ViaShareGrant`) → không thì `evidence.subject_forbidden` (403); đồng ý của hộ. Tệp chưa có bộ ba: chỉ vai + mục đích + đồng ý như trước |
| Nhật ký | **Mọi** quyết định sau khi thấy tệp — `granted`, `denied_subject`, `denied_scope`, `denied_consent` — ghi một dòng sổ `pf_files.access_log` (`shared.apply_ledger`, chuỗi `file:<ref>`, chỉ mã tham chiếu). Ghi sổ lỗi → không cấp (fail-closed) |

**Thêm module chủ.** Module chủ có cổng phân giải riêng (vd `cooperativeport.Resolve`); `pf/evidence` khai `evidence/port.Resolver` (interface bên tiêu thụ, mẫu `pf/workflow` ↔ `pf/party`) vì module năng lực không import module domain. Gốc lắp ráp viết bộ chuyển và nối bằng `evidence.Module.UseResolver(<tên module.yaml>, r)` — hiện có `pf/cooperative` (`cmd/api-platform/assembly/evidence.go`: `cooperative`, `household`, `plot`, `diary_entry`; `season`, `harvest` qua store vụ từ W3.3). Phạm vi và đồng ý cũng là interface tự khai (`app.ScopeResolver`, `app.ConsentChecker`) để không thêm cạnh vào chu trình `pf/party ↔ pf/sync`.

Vai DB: `r_evidence` là vai nhóm không có vai cũ (suy từ `owns_schema` trong `modreg.Catalog`, W2.6), `app_platform` là thành viên; `SELECT, INSERT` trên `pf_files.file`, `pf_files.access_log`. pf 000123 thu quyền `pf_files` của `r_sync` (và `sync_app` — sau W2.6 không còn quyền trực tiếp, lệnh vô hại).

## View công bố `_pub` (W6.3)

R-DB-10, ADR-0031 §2, kiến trúc v4.1 §13/§14/§22.1. `cap/analytics` = `pf/metrics` (`aka: [cap/analytics]` trong `module.yaml`, giữ tên repo theo ADR-0027).

- **Hợp đồng:** module chủ công bố view trong schema `pf_<module>_pub`, tên `v<N>_<tên>`, khai trong `port/pub.go` bằng `//ag:pubview pf_<module>_pub.<tên> v<N>` → `modgen` ghi `provides.publications: [pf_<module>_pub.<tên>/v<N>]` và `owns_schema: [..., pf_<module>_pub]`. View là hợp đồng như port: đổi bảng nội bộ phải giữ cột/ý nghĩa view; đổi hợp đồng = view `v<N+1>_` mới, view cũ giữ tới khi không còn bên đọc. Chỉ VIEW (không chép dữ liệu), không PII.
- **Migration:** view tạo trong migration công bố `<cụm>_pub_*` (pf 000124 `pf_pub_v1`) — `agmodgraph` cho phép nó tạo/sửa đối tượng trong schema `_pub` của nhiều module, không đụng schema nội bộ. Module tự thêm view của mình trong migration `<cụm>_<module>_*` cũng được.
- **Quyền:** vai NOLOGIN `r_<module>_pub` (`dbadmin`, tạo khi module sở hữu schema `_pub`) có USAGE schema + SELECT view; `r_metrics` là thành viên (`dbadmin.AnalyticsModule`); W6.4 thêm `r_agg` (xem [Vai tổng hợp `r_agg`](#vai-tổng-hợp-r_agg-w64)). `r_metrics` không có quyền nào trên schema nội bộ của module nguồn (pf 000124 REVOKE chốt; kiểm `TestPubViewsDeclaredAndPrivileges_W63`). Chủ view là `ag_migrator` (NOBYPASSRLS), mọi bảng gốc FORCE RLS → policy tenant áp qua view.
- **pf 000124:** `pf_cropcycle_pub.v1_season_event`, `v1_harvest_declaration`; `pf_extension_pub.v1_field_report`, `v1_task_event`, `v1_task`, `v1_directive_target_ack`; `pf_geo_pub.v1_plot_event`, `v1_plot_boundary`, `v1_layer_geometry` (GeoJSON 7 chữ số); `pf_mechanization_pub.v1_balance_run`, `v1_coop_coverage` (mức đáp ứng tổng thể mỗi HTX — quy tắc `domain.Summarize`/QT-02 viết bằng SQL, do mechanization sở hữu).
- **pf 000144 (W11.10):** `pf_extension_pub.v1_consolidation_closed` — nguồn module cho KHTC: một dòng mỗi kỳ × biểu có khóa kỳ đã lưu (`report_period_key`), số liệu = lần tổng hợp các xã mới nhất ghi không muộn hơn lần chốt gần nhất chạm biểu, cờ `form_closed`/`period_closed`/`key_closed`. Hợp đồng cột ở [`contracts/pubview`](../../contracts/pubview/README.md) (tên, kiểu, thứ tự — kiểm ở `TestPubViewsDeclaredAndPrivileges_W63`); view có hợp đồng `published` không bắt buộc `pf/metrics` đọc. Hai view giá dựng ở W11.76 (pf 000152): `pf_price_pub.v1_board_daily`, `v1_ticket_week` — `published`.
- **pf 20261013150108, 000154 (PFPUB, [ADR-0053](../adr/0053-hop-dong-so-lieu-da-chot-closed-figures.md)):** họ hợp đồng `closed_figures/v1` — `pf_forms_pub.v1_closed_figures` (kỳ native `cap/forms`, vai `r_forms_pub`) và `pf_extension_pub.v1_closed_figures` (nguồn vòng đời báo cáo cũ), cột giống hệt. **Bên đọc `_pub` ngoài `cap/analytics`:** `dbadmin.PubReaders` (rà soát như hợp đồng) cho vai nhóm `r_<module>` làm thành viên `r_<chủ>_pub` lúc bootstrap — không cấp thẳng quyền view; hiện có `r_extension` → `r_extension_pub`, `r_forms_pub`, `r_price_pub` (KHTC, port `extension/port.KHTCSources`).
- **Đọc:** `pf/metrics` mặc định dùng nguồn `_pub` (`app/pubsource.go`, `metrics.Deps.Sources` nil); gốc lắp ráp không còn nối `MetricsSource()`/`LayerGeometry()` của module chủ — các nguồn cũ giữ một kỳ làm **đối chứng**: kiểm thử `metrics_integration_test.go` của cropcycle/extension/geo/mechanization bọc chúng bằng `tests/pubgolden.Compare`, so từng giá trị, watermark, lỗi, phạm vi, hình học. Gỡ nguồn cũ khi bỏ đối chứng. `TestMetricsReadsOnlyDeclaredPubViews_W63`: mã `pf/metrics` chỉ nhắc `pf_metrics.*` và view `_pub` đã khai.
- **Bản sao chỉ đọc:** `AG_DB_REPLICA_URL` (URL gốc như `AG_PF_DB_URL`/`AG_DB_URL`; vai/mật khẩu ghép như primary) cho `api-platform` và worker (`metrics_snapshot`, `-metrics-backfill`) → `assembly.External.Replica` → `metrics.Deps.Replica`. Mỗi lần đọc kiểm `pg_is_in_recovery()`/`pg_last_xact_replay_timestamp()`: bản sao chưa phát lại tới mốc cutoff (hoặc lỗi) → primary, để giá trị ngày D ghi sổ không thiếu dòng ghi nhận trước Cutoff(D). Ghi (sổ chỉ số, nhật ký lớp) luôn ở primary. Không đặt → primary.
- **Chưa làm:** `pf/reports` vẫn đọc qua `port/` của module chủ (không đọc thẳng schema khác — DB-09); chuyển sang `_pub` khi tách `cap/forms`/`cap/export`.
- **Điểm vào ghi chéo module** (không phải `_pub`): hàm `SECURITY DEFINER` của module chủ, EXECUTE cấp cho `r_*` bên gọi (USAGE schema, không quyền bảng), liệt kê trong `migrations/schema_isolation_exceptions.yaml` với `owner: ADR-0028-write-port`; **mỗi điểm vào mới phải rà soát**. Hiện có `pf_workflow.submit/decide` (`r_masterdata`, `r_party`, `r_forms`, `r_metrics` — W6.4) và `pf_forms.mirror` (`r_extension`) — [ADR-0028](../adr/0028-vai-dang-nhap-theo-don-vi-trien-khai.md#điểm-vào-ghi-chéo-module-w63-09102026).

## Vai tổng hợp `r_agg` (W6.4)

R-DB-11, R-SEC-03, ADR-0031 §3–§7; [runbook](../runbooks/agg-jobs.md). Module chủ: `pf/metrics` (`cap/analytics`) — danh mục job, nhật ký chạy, bộ chạy; `pf/workflow` chỉ giữ yêu cầu/quyết định bốn mắt (kind `agg.job`).

- **Vai:** `r_agg` NOLOGIN **BYPASSRLS**, thành viên mọi `r_<module>_pub` (bootstrap, `dbadmin.AggRole`), pf 000129 cấp thêm USAGE `k_ref` + SELECT cột `k_ref.org.tenant_id`; pf 000137 (gói FC) cấp EXECUTE `k_ref.ctx_uuid/ctx_text/ctx_reject` — chính sách RLS dưới view `_pub` gọi chúng và quyền hàm kiểm theo `current_user`. **Chính sách mới gọi hàm khác trên bảng nguồn của `_pub` phải cấp EXECUTE cho `r_agg`.** Thành viên duy nhất: vai đăng nhập riêng `app_agg` (gói FC, `dbadmin.AppAgg`, NOINHERIT, không `r_*` nào khác, mặc định chỉ đọc + `statement_timeout`; bootstrap thu hồi thành viên khác — `dbadmin.ExclusiveMembers`). `cmd/migrate bootstrap` phải chạy trước `migrate up` (000129 dừng nếu thiếu `r_agg`).
- **Đường vào duy nhất:** `db.WithAggTx(ctx, pool, owner, governance, aggPool, authorize, fn)` (`internal/shared/db/agg.go`): giao dịch chỉ đọc trên `pool` (`app_worker`) → vai module chủ + tenant quản trị → `authorize` (bộ chạy kiểm job + phiên bản + băm là phiên bản đã duyệt mới nhất ở `pf_metrics.agg_job`) → chỉ khi nil mới mở giao dịch chỉ đọc thứ hai trên `aggPool` (`app_agg`, không bọc `ForModule`) và `SET LOCAL ROLE r_agg` (xóa `app.*`, `transaction_read_only`, `statement_timeout` 10 phút) → `fn(db.AggTx)`; `AggTx.InTenant` đặt `app.tenant_id` cục bộ để đọc lát một tenant qua view `_pub`. Vai hết khi COMMIT/ROLLBACK.
- **Lint `agnosessionset`:** ngoài `internal/shared/db` không có `SET LOCAL ROLE` hay `set_config('role', …)`; tên `r_agg` chỉ trong `shared/db`, `shared/dbadmin`; `db.WithAggTx` chỉ được gọi từ `internal/platform/metrics/app`.
- **Danh mục job** (`domain.AggJob`, `app/aggjobs.go`): id, phiên bản, view `_pub` đọc, SQL (chỉ tham chiếu view đã khai — kiểm lúc đề xuất), chiều, số đo, mục đích, k. `Agg.Propose` → `Agg.Decide` (người quyết định ≠ người đề xuất: app + CHECK `decision_four_eyes` + CHECK `agg_job_four_eyes`); một phiên bản một đề xuất một quyết định; phiên bản chỉ tăng; dòng quyết định chép định nghĩa từ đề xuất (trigger `trg_bind_proposal`).
- **Chạy:** `Agg.Run` — k áp = max(k job, k sàn `publicdata.k_min`, mặc định 10); k thấp hơn, job chưa khai/chưa duyệt/cũ/sửa → `refused`. Liệt kê tenant qua `k_ref.org` (vượt RLS), đọc từng lát tenant qua view `_pub` (view W6.3 chạy bằng quyền chủ có FORCE RLS — BYPASSRLS không xuyên view), gộp + ẩn nhóm < k trong tiến trình; mỗi lần chạy một dòng `pf_metrics.agg_run`, không ghi được → không trả đầu ra.
- **k sàn:** `app.KFloorSource` (interface của bên dùng, gốc lắp ráp nối tham số masterdata của tenant quản trị); nil → 10. Bật production chờ C-4; bộ chạy chưa nối `cmd/worker`.
- **Ghi chú W5.3:** bản dựng tập dữ liệu tổng hợp của cổng tích hợp (`pf/apiclient` feedbuild, W5.3) dùng danh sách tenant bên góp chứ không dùng `r_agg`; khi cần tổng hợp toàn nền tảng có k trên nhóm sau gộp, chuyển thành một job đã khai ở đây (vd `cc.harvest_by_commune`) — đi qua bốn mắt như mọi job.

## Năng lực chưa tách `cap/import`, `cap/task`, `cap/export` (W6.6)

R-MOD-09 (quy tắc ba), kiến trúc v4.1 §5.1, §26; ADR-0027 mục 2. **Quyết định: chưa tách module nào**, khai `hosts` ở module đang cài (bảng dưới = `Registry.Hosts`). Không migration, không dời mã. `pf/reports` bỏ `aka: [cap/export, cap/import]` (nó không phải *là* hai năng lực đó — chỉ cài một phần) sang `hosts`.

**Kiểm kê (09/10/2026)** — đếm theo cơ chế, không theo tên hàm:

| Năng lực | Cơ chế | Cài ở (module → luồng) | Số |
|---|---|---|---|
| `cap/import` | Đọc tệp CSV/.xlsx **đã tách** thành thư viện `internal/shared/tabular` (giới hạn dung lượng/zip bomb/dòng/cột, từ chối công thức, ghi .xlsx toàn chuỗi). Còn lại là ánh xạ cột + kiểm từng dòng + ghi, **đồng bộ** trong một request (R-52), kết quả từng dòng `ok/rejected` + lý do không PII, khóa idempotency | `pf/cooperative` (hộ, sản lượng; thửa GeoJSON/KML/SHP; ranh giới HTX), `pf/party` (HTX hàng loạt, cây tổ chức), `pf/accounts` (nông dân qua `tabular`; người dùng CSV/XLSX tự đọc bằng `excelize` — `MaxImportBytes`), `pf/mechanization` (máy), `pf/masterdata` (danh mục hàng loạt — có sổ `catalog_import`; đơn vị hành chính JSON `agrigreen.admin_units/v1`), `pf/geo` (ranh giới hành chính GeoJSON, cơ sở, công suất JSON), `pf/reports` (định nghĩa biểu từ .xlsx — `formimport`, `cmd/formimport`; sổ `form_import`), `mkg/measurement` (số cân khách — sổ `customer_weight_import`, sai một dòng → từ chối cả lần) | 8 module, ≥ 14 luồng |
| `cap/export` | (a) **Việc xuất bất đồng bộ**: sổ job + sự kiện `*.export.requested` (outbox) → worker dựng tệp tất định → `objstore.SHA256Hex` + `Put` (`ErrExists` = idempotent) → tải. Khác nhau ở phần lõi: `pf/reports` có hạn mức/ngày, sổ `export_audit`, cảnh báo `export.anomaly` (DLP), URL ký + gửi thư; `pf/mrv` lưu băm ảnh chụp vào sổ, `VerifyExport`, gói nhiều tệp; `pf/apiclient` ngưỡng k, hợp đồng dataproduct, **không** URL ký (ADR-0022 §8) + ảnh chụp tập dữ liệu W5.3. (b) **Tải đồng bộ** không ghi lần xuất: dựng tệp trong request | (a) `pf/reports`, `pf/mrv`, `pf/apiclient`. (b) `pf/extension` (lịch công tác Excel/PDF), `pf/party` (cây tổ chức .xlsx khứ hồi), `pf/cooperative` (thửa KN GeoJSON/KML), `pf/metering` (bảng kê CSV), `mkg/acctbridge` (bút toán csv/xml, có SHA-256) | (a) 3, (b) 5 |
| `cap/task` | Nhiệm vụ có vòng đời tạo → giao → cập nhật → đóng, SLA mặc định + leo thang (`task_event`/`task_current`, `BulkAssign`, `enqueueEscalated`) | `pf/extension` (nhiệm vụ KN US-TASK-*). **Không tính:** `extension/reportcycle` (công việc nộp biểu theo kỳ — thuộc `cap/forms`), `mkg/fieldops` `crew_assignment` (xếp tổ máy cho đơn — phân bổ nguồn lực vùng mkg, không vòng đời nhiệm vụ/kết quả), `pf/reports` `form_assignment` (gán biểu cho loại báo cáo), `pf/accounts` `role_assignment` | 1 |

Không tính vào `cap/import`/`cap/export`: nhận lô đối tác `pf/apiclient` `ingest_batch` (cổng tích hợp, R-INT), van công bố `valve_export` của `mkg/*` (`contracts/publication`), tệp chứng từ `shared/docfile` (`cap/evidence`).

**Vì sao chưa tách:**
- `cap/import`: phần chung đáng tách (đọc tệp an toàn) **đã** là thư viện `shared/tabular` — thư viện, không cần `module.yaml`/schema (ADR-0027 mục 1). Phần còn lại theo §26 là "bộ ánh xạ cột theo module chủ" — vốn ở module chủ. Mô hình §26 (lô nhập có dòng lỗi lưu lại, hai bước `Validate`/`Commit`, sự kiện `import.committed`) **chưa có ở luồng nào** (mọi luồng một bước, đồng bộ); dựng nó cần schema mới (migration) và đổi hợp đồng HTTP của 14 luồng — không thấp rủi ro.
- `cap/export`: ba việc xuất bất đồng bộ dùng chung khung, nhưng khung đã nằm ở thư viện (`shared/outbox`, `shared/objstore`, `shared/idempotency`); phần khác nhau (DLP, băm vào sổ, ngưỡng k, cách tải) là lõi nghiệp vụ. Gom sổ job về một schema `cap/export` cần migration và dời ba sổ chỉ ghi thêm (chuỗi băm) — đúng điều kiện **dừng** của gói. Mẫu văn bản có phiên bản + Render PDF của §26 chưa có bên dùng thứ hai.
- `cap/task`: một nơi cài — chưa đạt quy tắc ba (khớp ghi chú bản đối chiếu: "tách khi có app thứ hai").

**Điều kiện kích hoạt tách** (gói mới, cấp số migration riêng; xóa `hosts` tương ứng trong cùng PR — `modreg` báo lỗi nếu còn):
- `cap/import` → module `pf/import` khi một luồng cần **lô nhập lưu dòng lỗi / hai bước xem trước–ghi** hoặc nhập lớn hơn ngưỡng đồng bộ R-52 (job), và ứng dụng thứ hai có lịch — dự kiến G1 KN (kiến trúc v4.1 Phụ lục A, cổng G1: "App KN chạy thật trên `cap/import`"). Giữ `tabular` làm bộ đọc; module chủ khai bộ ánh xạ cột qua interface của `cap/import` (interface bên tiêu thụ, không import domain). Việc nhỏ làm trước được: chuyển nhập người dùng `pf/accounts` sang `tabular`.
- `cap/export` → module `pf/export` khi (i) mẫu văn bản có phiên bản/PDF cần cho ứng dụng thứ hai, hoặc (ii) có việc xuất bất đồng bộ thứ tư, hoặc (iii) điểm kiểm soát chống thất thoát chung cho mọi đường xuất (§30: nhật ký, hạn mức, cảnh báo khối lượng bất thường) được yêu cầu — lúc đó các tải đồng bộ (b) cũng đi qua nó. Hiện chỉ `pf/reports` có hạn mức + cảnh báo khối lượng.
- `cap/task` → module `pf/task` khi ứng dụng thứ hai (HTX/CGH, v4.1 G2 "tổng quát `cap/task`") cần nhiệm vụ có SLA + kết quả bắt buộc khi đóng; tách từ `pf/extension` (sổ `task_event`), `dom/extension` dùng qua port.

## Module `pf/cooperative` — mặt tiền (W3.2)

[Thiết kế](../design/cooperative.md) (W3.1), ADR-0027, ADR-0030. `internal/platform/cooperative` (`dom/cooperative`, `kind: domain`, `enable: per_tenant`, `shares: [household, plot, season, diary_entry]`). W3.2 **không đổi DB**: module chưa có schema, vai hay route; `owns_schema` rỗng đến W3.3 (`pf_coop`, pf 000131 — mục dưới).

**Cổng** (`cooperativeport`): `Cooperatives`, `Households`, `Plots`, `Seasons`, `Diary`, `Resolve` và hai interface module khác hiện thực `MachineCounts` (pf/mechanization), `FarmerHouseholds`. DTO giữ nguyên trường của DTO cũ (partyport, geoport, ccport). Nguồn chưa nối → `cooperativeport.ErrUnavailable` (bên gọi coi như phụ thuộc lỗi, 503). Bộ kiểm hợp đồng `cooperative/port/porttest.RunContract(t, tenant.WithID, mk)` + bản giả `porttest.Memory` cho kiểm thử của module khác.

**Đỉnh lá của đồ thị.** `pf/cooperative` không import port cũ. Gốc lắp ráp dựng nguồn (`cooperative.Sources`, store tạm) từ port của `pf/party`, `pf/geo`, `pf/cropcycle` bằng bộ chuyển [`cmd/internal/coopwire`](../../cmd/internal/coopwire/coopwire.go). Lý do: `pf/cropcycle` đọc thửa qua `cooperativeport`, còn mặt tiền đọc vụ từ cropcycle — import trực tiếp sẽ thành chu trình `cropcycle ↔ cooperative`. W3.3 (vụ vào `pf/cooperative`) gộp bộ chuyển vào `cooperative/store`.

**Ai dùng `cooperativeport`.** Chỉ module **domain** được import (R-MOD-05):

| Bên tiêu thụ | Dùng |
|---|---|
| `pf/mechanization` | hiện thực `MachineCounts` (sự kiện diện tích vụ vẫn đọc `ccport` — chỉ là `consumes.events`) |
| `pf/extension` | `Cooperatives.ByUnit`, `Plots.DigitizedByCommune` (bảng điều hành gộp G-02) |
| `pf/cropcycle` (vụ + van collection) | `Plots.Get`, `DistanceM`, `Facts` |
| Gốc lắp ráp | `SupplySource` (simulation), `AreaFarmers` (notify), `SeasonActivity` (party) đọc qua `coop.*` |

Bên tiêu thụ tầng dưới (`pf/accounts` kernel; `pf/party`; `pf/reports`, `pf/notify`, `pf/metrics` capability) không import `cooperativeport`: chúng khai interface của mình (`partyport.PlotStats`, `reports/port/cropcyclesrc`…), gốc lắp ráp nối. Port cũ do module chủ khai có ghi chú "Thay thế (W3.2)" (không ghi `Deprecated:` để staticcheck không chặn chỗ dùng hợp lệ). Sự kiện vẫn khai `//ag:event` ở port cũ đến W3.3.

**Thêm bên tiêu thụ:** chỉ từ module domain; module tầng dưới khai interface của mình (port của nó) và gốc lắp ráp nối `coop.*` vào.

**W3.3 (đã làm, mục dưới)** thay `store.Legacy` bằng store SQL `pf_coop`, chuyển handler/consumer/sync và xóa `cmd/internal/coopwire` cùng port cũ. Vai giữ `r_cooperative` (không ghi đè `r_coop`).

## Module `pf/cooperative` — chủ schema `pf_coop` (W3.3)

[Ghi chú hiện thực](../design/cooperative.md#17-ghi-chú-hiện-thực-w33), [runbook chuyển schema](../runbooks/cooperative-schema-move.md). Từ pf 000131–000133, 22 bảng HTX/hộ/thửa/vụ (cùng phân vùng, 5 chuỗi số, 4 hàm trigger) ở `pf_coop`; mọi giao dịch của module chạy `SET LOCAL ROLE r_cooperative` (`cooperative.DBModule`). `r_party`, `r_geo`, `r_cropcycle` không còn quyền trên các bảng này, `r_cooperative` không có quyền trên `pf_party`/`pf_geo`/`pf_cropcycle`.

**Gói.** `cooperative/season` (vụ, nhật ký, SOP, sản lượng — từ `pf/cropcycle`), `cooperative/plot` (thửa, ranh giới, ranh giới/vùng HTX — từ `pf/geo`), `cooperative/member` (nông hộ, xã viên, hồ sơ HTX, sổ hồ sơ KN — từ `pf/party`), `cooperative/app` (mặt tiền `cooperativeport` trên `store.Sources`). Đường dẫn HTTP, mã lỗi (`cropcycle.*`, `geo.*`, `party.*`), tên sự kiện v1 và tên thực thể đồng bộ giữ nguyên; route gắn trong nhóm `pf/cooperative` ở gốc lắp ráp.

**Nối ở gốc lắp ráp** (`cmd/api-platform/assembly`, `cmd/api-public/assembly`, `cmd/worker`):

```go
coop := cooperative.New(unit, clk, cooperative.Deps{Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Dir: pty.OrgDirectory(),
	Consents: pty.ConsentStates(), AdminUnits: md.AdminUnits(), AdminGeom: g.AdminGeometry(), Params: md.Params(),
	Catalogs: md.Catalogs(), GpsLog: g.GpsLog(), Households: farmerid.Households(ext.Vault), Farmers: farmerid.New(ext.Vault)})
pty.UseCoopProfiles(coop.CoopProfiles()) // tạo HTX hai bước (party điều phối)
pty.UsePlotStats(coop.PlotStats())
g.UsePlots(coop.PlotGeometry())    // log GPS: khoảng cách tới lô
g.UseMapLayers(coop.MapLayers())   // lớp HTX, lớp thửa của bản đồ gộp
cc.UseCollectionVolume(coop.Plots(), coop.Seasons(), md.AdminUnits())
```

`coop.SyncSources()`/`SyncSinks()` vào `pf/sync`; consumer `pf.cropcycle.gps_log` chạy dưới `cooperative.DBModule`. Gốc module không kéo kho định danh (kho qua `cooperativeport.HouseholdIdentities`/`FarmerIdentity`, bộ điều hợp `cooperative/port/farmerid`), nên `cmd/integration-gateway` và job feed của `cmd/worker` dựng `cooperative.New(pool, clk, cooperative.Deps{})` cho `Plots().Facts`, `Seasons().Codes/Progress`. View công bố W6.3 trên bảng vụ/thửa (`pf_cropcycle_pub.*`, `pf_geo_pub.*`) giữ tên và chủ cũ, đọc bảng `pf_coop` (bám OID).

**Tên sự kiện ≠ module phát.** Sự kiện `pf.geo.plot.*`, `pf.cropcycle.{season,farm_log,harvest,sop,coop_season_area}.*`, `pf.party.household.*` do `pf/cooperative` khai `//ag:event` và phát (D-8). `modreg` lấy module phát theo tiền tố tên nếu module đó khai sự kiện, không thì module duy nhất khai nó trong `provides.events`.

**Migration chuyển schema.** Quy tắc `schema` của `agmodgraph` cho phép đúng một dạng sửa đối tượng của schema khác: `ALTER {TABLE|SEQUENCE|FUNCTION} <schema khác>.<x> SET SCHEMA <schema của module gắn migration>`. `migratecheck` coi sổ đã chuyển (`ALTER TABLE … SET SCHEMA`) vẫn là sổ dưới tên mới (quy tắc ADD COLUMN, ADR-0012). Tên sổ (`TG_ARGV`, `chain_head.ledger`) không đổi khi chuyển schema; `shared.ledger_added_column` khóa theo tên bảng thật nên phải đăng ký bù dưới tên mới.

## Đọc tại chỗ qua cấp phép (W4.2)

ADR-0030 (mục "W4.2 — hình chính sách"), kiến trúc v4.1 §17.3, pf 000136, shared 000014, [thiết kế §19](../design/cooperative.md#19-ghi-chú-hiện-thực-w42). Bật production chờ C-10/H-1.

| Việc | Cách làm |
|---|---|
| Chia sẻ một bảng mới | Khai share trong `shares` của `module.yaml` (`go run ./tools/modgen`); khóa `share_scope` là `modreg.ShareTable("<module>:<share>")` = `<schema đầu của owns_schema>.<share>`. Trong migration của module gọi `shared.apply_share_rls('<schema>.<bảng>', <biểu thức USING>)` với biểu thức như pf 000136 (`EXISTS k_ref.share_scope` theo đơn vị xem, mục đích, khóa, đơn vị chủ, Nút, hiệu lực) — không viết `CREATE POLICY p_share` tay. Thêm bảng vào `shareExpectedTables` của `migrations/share_rls_integration_test.go` |
| Chính sách | `p_share` PERMISSIVE **FOR SELECT**, không `WITH CHECK` (cấp phép là quyền đọc); `shared.v_missing_rls` báo nếu sai hình. `apply_share_rls` nới `p_scope` cho dòng khác tenant |
| Dựng `share_scope` | Worker `pf.kref`: sự kiện `share_grant.approved`/`revoked` → dựng lại dòng của cấp phép; `org.created`/`moved` → mọi cấp phép của đơn vị chủ; `Rebuild` → cuối giao dịch tenant. Chỉ cấp phép `intra_platform` có mục đích và `module_table` khai `shares` |
| Mục đích | Client gửi `X-Purpose: <mã>` (vd `khuyen-nong`) — middleware `shareread` (do `pf/party` cài) đặt `authz.WithPurpose` → `app.purpose`. Không có mục đích → không bao giờ thấy dòng chia sẻ |
| Port | Store đọc `tenant_id` của dòng; port/mặt tiền đặt `ViaShareGrant = shareread.Via(ctx, tenant dòng)` và gọi `shareread.Mark`/`Note` **trước** khi handler ghi phản hồi (middleware chỉ đệm phản hồi khi đã có bản ghi chia sẻ lúc bắt đầu ghi) |
| Nhật ký | Một sự kiện `pf.party.share_read.recorded` mỗi yêu cầu (tenant = đơn vị xem; bảng × đơn vị chủ × số bản ghi). Không ghi được → 503 `share.read_log_unavailable`, không trả dữ liệu. Chỉ số `ag.share.reads` (`viewer_tenant`, `module_table`) |
| Thu hồi | Worker xóa dòng `share_scope` của cấp phép cùng giao dịch; biến `app.*` là SET LOCAL nên kết nối PgBouncer tái sử dụng không giữ quyền. Trễ ≤ 5 s (p99) sau thu hồi |

## Phạm vi Nút trên `pf_coop` (W3.4)

[Ghi chú hiện thực](../design/cooperative.md#18-ghi-chú-hiện-thực-w34), kiến trúc v4.1 §17.2, pf 000134, shared 000012. Ngoài `p_tenant` (PERMISSIVE), 19 bảng `pf_coop` có `p_scope` **RESTRICTIVE** (`sop_template`, `sop_version`, `variety_sop_binding` chỉ `p_tenant`). Hàng thấy/ghi được dưới `r_cooperative` = cùng tenant **và** một trong các nhánh: không Nút (`app.node_id` rỗng — consumer, job, Admin không gắn Nút); tenant chưa có bản chiếu `k_ref`; Nút của dòng là hậu duệ của `app.node_id` theo `k_ref.node_closure`; xã của dòng thuộc `app.xa_ids`; dòng chưa gắn Nút (thửa nháp…) khi người gọi không có xã phụ trách. `WITH CHECK` dùng cùng biểu thức (trừ `household`) → ghi dòng ngoài phạm vi trả 42501, sửa dòng ngoài phạm vi ảnh hưởng 0 dòng.

- Kiểm phạm vi ở ứng dụng **giữ nguyên** (`authz.Scope`, `coopOnly`) — RLS là lớp thứ hai.
- Nút của người dùng HTX là `OrgRef` của token: kiểm thử dựng `k_ref` cho tenant (vd `kref.RebuildTenant`) thì người dùng chỉ thấy cây con của Nút; không dựng thì chính sách lùi về `p_tenant`.
- Bảng mới có `tenant_id` trong `pf_coop` (hay schema khác muốn phạm vi Nút): thêm `p_scope AS RESTRICTIVE` viết thẳng `EXISTS k_ref.node_closure` (không hàm do repo định nghĩa) — `shared.v_missing_rls` báo nếu `p_scope` PERMISSIVE hay có chính sách thứ ba; cổng EXPLAIN W9.1 báo nếu gọi hàm theo hàng.

## Module `mkg/engine` — kho quan sát (W8.2)

[Thiết kế](../design/mkg.md#59-mkgengine--kho-quan-sát-của-engine-điều-phối-w82), [ADR-0036](../adr/0036-engine-ba-che-do-vung-noi-bo-optcore.md#hiện-thực). Module mới `internal/mkg/engine` (`kind: domain`, `zones: [mkg]`, `enable: always`, ứng dụng MKG). Sở hữu `mkg_engine` (mkg 000037): sổ `observation` + bảng đệm `observation_feed_state`. Vai nhóm `r_engine` suy từ `modreg.Catalog` (không vai cũ `engine_app`); `cmd/api-internal` ghép module nên `app_internal` là thành viên. Giao dịch chạy `SET LOCAL ROLE r_engine` (`engine.DBModule`).

**Port.** `engine/port.Observations` (`Latest(ctx, tid, Query{Kind, EntityKeys})` → `Snapshot{Fresh, Stale, Missing}`) cho engine W8.3. Hằng `Kind*`, `Source*`, `Unit*`, `JobObservationFeed`.

**Nối ở gốc lắp ráp** (`cmd/api-internal/assembly`): `engine.New(unit, clk, EngineOptionsFromEnv(fo.DirectoryFeed))` — bộ điều hợp feed của `mkg/fieldops` (W5.5, `AG_INTEGRATION_GATEWAY_URL` + `AG_FIELDOPS_FEED_API_KEY`); `AG_ENGINE_FEEDS` (danh sách feed), `AG_ENGINE_FEED_ALLOW_PROPOSED=1` (dev/test, bỏ qua ở production), `AG_ENGINE_FEED_INTERVAL`; job `engine_observation_feed` trong `AG_MKG_JOBS` (cần `AG_MKG_TENANT_ID`). Module không có route HTTP (không trong `Modules.List`).

**Cấm float** (`agnofloat`, `floatFreeZones`): giá trị và cận bất định là `decimal`.

## Migration theo module (W7.1)

Bộ chạy v2 (`internal/shared/migrator`, [ADR-0038](../adr/0038-bo-chay-migration-theo-module.md)) gắn mỗi tệp `migrations/<cụm>/NNNNNN_*.up.sql` cho module bằng đúng quy tắc của `agmodgraph` ở trên (tên `<cụm>_<module>`, nếu không thì schema đụng; tệp nhiều module → module giả `<cụm>`), ghi lịch sử theo module (cột `module` của `shared.schema_migrations`; dời vào schema module là bước 2) và áp mọi tệp vắng trong sổ theo thứ tự topo `depends` — **số nhỏ hơn số đã áp vẫn được áp**. Từ 13/10/2026 (WH-B) tệp mới không cấp số: tên mốc UTC `YYYYMMDDHHMMSS_<cụm>_<module>_….up.sql` (`go run ./tools/migratecheck -new …`), xếp sau mọi số cũ ở cả hai bộ chạy; `migratecheck -base origin/dev` (CI) chặn tệp xếp trước phiên bản lớn nhất của nhánh gốc vì golang-migrate (mặc định) vẫn bỏ qua im lặng — [parallel-packages.md](parallel-packages.md).

- Đặt tên tệp mới `<mốc UTC>_<cụm>_<module>_….up.sql` (`go run ./tools/migratecheck -new <cụm>_<module>_<tên>`), chỉ đụng schema của module đó (agmodgraph đã bắt).
- `go run ./tools/migratecheck -modules migrations`: bảng module → số tệp, khoảng số, schema. `go run ./tools/migratecheck -module pf/party migrations`: thứ tự áp và kiểm riêng các tệp của module.
- Đổi `depends` trong `module.yaml` → `go run ./tools/modgen` (danh mục sinh mang `depends` cho bộ chạy lúc chạy, `modreg.Depends`).
- Kiểm thử cả bộ chạy v2 cho một gói: `AG_MIGRATE_RUNNER=v2 go test -count=1 -p 1 ./<gói>/` (`pgtest` dùng `dbadmin.Migrate`).
- Vai `own_<module>` (NOLOGIN) đã dựng; chạy migration bằng nó và thư mục `migrations/<cụm>/<module>/` là bước 2.
