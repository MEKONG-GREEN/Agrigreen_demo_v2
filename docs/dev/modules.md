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

Vi phạm có từ trước W1.2 được liệt kê trong [`tools/archlint/modgraph_exceptions.yaml`](../../tools/archlint/modgraph_exceptions.yaml), khóa `(rule, from, to)`, mỗi mục bắt buộc `reason`, `owner` (gói kế hoạch sẽ gỡ, dạng `W<n>.<m>`) và `until`. Mục `ops_schemas` liệt kê schema vận hành/dbadmin không thuộc module nào (`mkg_monitor`, `ts_monitor`; `k_ref` tạm từ W2.4 tới khi W2.5 gán chủ).

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
4. Grant: migration mới `GRANT … TO r_<module>`, không grant cho `<module>_app`; vai cũ thừa hưởng qua membership. Module chưa từng có vai cũ thì thêm vai vào `ModuleGroups` (W2.6 đổi nguồn danh sách) và grant cho `r_<module>`.
5. Kiểm thử tích hợp: dựng module trên `env.Pool(t, migrations.PF, dbadmin.AppPlatform)`. Toàn bộ bộ kiểm thử phải xanh: vai đăng nhập NOINHERIT chỉ chạm bảng qua `r_*`. Kiểm trực tiếp DB vẫn dùng pool vai cũ.
6. Truy vấn ngoài giao dịch, như `pool.Query(…)` thẳng trên pool, sẽ bị `permission denied` khi pool là `app_*`. Chuyển chúng vào `db.WithContextTx`.

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