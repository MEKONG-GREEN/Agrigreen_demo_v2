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
| `depends` | khóa **sinh**, ràng buộc tay | import | Module khác mà mã không phải kiểm thử import (`port/`, vault `client/`) **và dùng ít nhất một định danh tạo phụ thuộc gọi** (interface, hàm, biến, kiểu thường). Import chỉ dùng hợp đồng dữ liệu — hằng (tên sự kiện, tên vai, khóa tham số, giá trị liệt kê) và kiểu payload `//ag:event`/`//ag:internal-event` — không tạo `depends`. Ràng buộc mặc định `">=MAJOR.MINOR <MAJOR+1"`; sửa tay được giữ |
| `consumes.events` | sinh | kiểu payload sự kiện, hằng `Event*` | Sự kiện của module khác mà mã nhắc tới (§7.1 tách `consumes` khỏi `depends`). Không thuộc đồ thị R-MOD-05; `modreg` kiểm mỗi sự kiện do module phát khai trong `provides`, sự kiện nội bộ MKG chỉ cho module vùng `mkg` |
| `provides.ports` | **sinh** | `port/` (vault: `client/`) | Interface xuất; gói con có tiền tố `sub.Name` |
| `provides.events` | **sinh** | `//ag:event <tên> v<N>` | Sự kiện công bố (`contracts/events`), dạng `<tên>/v<N>` |
| `provides.internal_events` | **sinh** | `//ag:internal-event` | Sự kiện nội bộ (`contracts/events-internal`) |
| `provides.publications` | **sinh** | `//ag:publication` | Van công bố (`contracts/publication`) |
| `provides.data_products` | **sinh** | `//ag:dataproduct` | Sản phẩm dữ liệu Public API (`contracts/dataproduct`) |
| `permissions` | **sinh** | hằng `Role*` trong `port/` | Vai realm module khai và kiểm. Mã quyền × lớp (R-MOD-07) đến ở W7.2 |
| `shares` | tay | — | Bảng chấp nhận đọc chéo đơn vị (ADR-0030). Rỗng đến W4.1 |
| `owns_schema` | **sinh** | migration | Schema `CREATE SCHEMA` tên `<cụm>_<module>` (vault: `vault`; schema lõi `k_ref` → `pf/kref`), hoặc schema chỉ module này nhắc trong chuỗi SQL (vd `ts_weigh` → `mkg/measurement`, `pf_files` → `pf/sync`). Schema lõi `k_*` có ở mọi cụm của `zones`, chỉ module `kernel` được sở hữu |
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

Migration được gắn module theo tên tệp `<số>_<cụm>_<module>[_…].up.sql` (khớp dài nhất). Tệp không khớp tên mà chỉ đụng schema của **một** module thì gắn module đó (vd `pf 000010_pf_files` → `pf/sync`, `ts 000001_ts_weigh` → `mkg/measurement`). Tệp đụng schema của nhiều module là vi phạm `schema`: migration mới phải tách theo module.

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

**Kỳ chuyển (tới W2.6):** vai cũ `<module>_app` vẫn chạy, giữ quyền và là thành viên `r_<module>`. Module đã chuyển chạy được trên cả pool cũ lẫn pool `app_*`. Đã chuyển: `pf/content`, `pf/notify`.

**Chuyển một module sang `r_*`:**

1. Gốc module (`module.go`): `var DBModule = db.Module("pf/<module>")`. Tên phải có trong registry, không thì panic lúc khởi tạo.
2. Trong `New`, bọc pool một lần: `app.Service{DB: db.ForModule(b, DBModule), …}`. Mọi `tenant.WithTenantTx`/`FromCtxTx` của module từ đó chạy dưới `r_<module>`. Mã mới dùng `db.WithContextTx(ctx, pool, DBModule, fn)` (tenant lấy từ ctx), hoặc `db.WithModuleTenantTx` khi đã có tenant (consumer, job).
3. Consumer của worker: đặt `module: <pkg>.DBModule` trong `consumer` (`cmd/worker`), để giao dịch inbox cũng chạy dưới `r_<module>`. Ở chỗ khác, đưa `db.ForModule(pool, DBModule)` cho `inbox.Process`/`ConsumeWith`.
4. Grant: migration mới `GRANT … TO r_<module>`, không grant cho `<module>_app`; vai cũ thừa hưởng qua membership. Module chưa từng có vai cũ thì thêm vào `dbadmin.groupOnlyModules` (W2.5: `pf/kref`; W2.6 đổi nguồn danh sách) và grant cho `r_<module>`. Pool là vai đăng nhập của đơn vị (`app_worker`…), mật khẩu `AG_DB_PASSWORD_APP_*`.
5. Module pf mới (có `r_*`) phải được migration của nó `GRANT USAGE ON SCHEMA k_ref` + `SELECT` các quan hệ `k_ref` + `EXECUTE k_ref.ctx_*` (R-DB-05) — pf 000091/000092 chỉ cấp cho các `r_*` lúc đó.
6. Kiểm thử tích hợp: dựng module trên `env.Pool(t, migrations.PF, dbadmin.AppPlatform)`. Toàn bộ bộ kiểm thử phải xanh: vai đăng nhập NOINHERIT chỉ chạm bảng qua `r_*`. Kiểm trực tiếp DB vẫn dùng pool vai cũ.
7. Truy vấn ngoài giao dịch, như `pool.Query(…)` thẳng trên pool, sẽ bị `permission denied` khi pool là `app_*`. Chuyển chúng vào `db.WithContextTx`.

**Lint.** `agnosessionset` (archlint) cấm, trong chuỗi SQL của mã `internal/*` và `cmd/*` (trừ `*_test.go`):

- `SET ROLE`, `SET SESSION …`, `SET <biến> = …` và `SET TIME ZONE …` ở đầu câu lệnh;
- `RESET …`;
- `set_config(…, false)`;
- `SET LOCAL ROLE` ngoài `internal/shared/db`.

`migratecheck` cấm tương tự trong migration. Biến cục bộ dùng `SET LOCAL` hoặc `set_config(…, true)`.

**Triển khai:** chạy `migrate bootstrap` (tạo `r_*`, `app_*`, membership) trước `migrate up` (pf 000090 chép quyền và lỗi nếu thiếu vai). Mật khẩu `app_*` lấy từ `AG_DB_PASSWORD_APP_PLATFORM`, … hoặc `AG_DB_APP_PASSWORD`.

**Còn lại (W2.6):**

- 15 module pf: accounts, apiclient, cropcycle, extension, geo, masterdata, mechanization, metering, metrics, mrv, party, reports, simulation, sync, waterway;
- 7 module mkg, ts, vault;
- `cmd/*` đổi pool sang `app_*`;
- compose, chart, PgBouncer `userlist`;
- bỏ quyền bảng của `<module>_app`.

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

Mảng xã: `k_ref.ctx_text('app.xa_ids')::text[]`. Chưa chính sách RLS nào đọc các biến mới (W3.4, W4.x).
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

Chưa có route HTTP, sự kiện `pf.workflow.request.decided` (§26) chờ bên tiêu thụ (W6.2, W6.4). Vai DB: `r_workflow` là vai nhóm không có vai cũ (`dbadmin.groupOnlyModules`); `cmd/api-platform` mở pool `app_platform` cho giao dịch riêng của module.

## Module `pf/cooperative` — mặt tiền (W3.2)

[Thiết kế](../design/cooperative.md) (W3.1), ADR-0027, ADR-0030. `internal/platform/cooperative` (`dom/cooperative`, `kind: domain`, `enable: per_tenant`, `shares: [household, plot, season, diary_entry]`). W3.2 **không đổi DB**: module chưa có schema, vai hay route; `owns_schema` rỗng đến W3.3 (`pf_coop`, pf 000094).

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

**W3.3** thay `store.Legacy` bằng store SQL `pf_coop`, ghi đè `modreg.DBRole` → `r_coop`, thêm `dbadmin.groupOnlyModules`, chuyển handler/consumer/sync và xóa `cmd/internal/coopwire` cùng port cũ.
