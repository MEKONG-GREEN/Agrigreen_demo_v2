# Kiểm kê ghép nối của hệ thống Khuyến nông (KNSPLIT-0)

**Gói:** KNSPLIT-0 · **Ngày:** 14/10/2026 · **Quyết định:** [ADR-0055 — Tách hệ thống Khuyến nông](../../adr/0055-tach-he-thong-khuyen-nong.md) (Đề xuất) · **Cổng:** `go run ./tools/knboundary` ([cấu hình](../../../tools/knboundary/boundary.yaml), [baseline](../../../tools/knboundary/baseline.yaml)) · **Kiểm thử dữ liệu:** [`migrations/kn_split_boundary_integration_test.go`](../../../migrations/kn_split_boundary_integration_test.go)

Tài liệu liệt kê mọi chỗ hệ Khuyến nông (KN) đang dính với nền tảng và các ứng dụng khác. Gói này **không chuyển mã**. Nó chuẩn bị cho các gói KNSPLIT-1…5 ở ADR-0055.

**Trả lời của chủ sản phẩm ngày 14/10/2026** (ADR-0055, mục "Trả lời của chủ sản phẩm"):
- KN là **hệ độc lập**. KN chỉ trao đổi dữ liệu với hệ khác qua nền tảng dữ liệu dùng chung: `_pub`, data product, lớp tích hợp. KN **không gọi trong tiến trình** module của hệ khác, kể cả port HTX.
- NĐ 331 không đòi tách vật lý. Tách vật lý là tùy chọn, sau G1.
- Báo giá (`pf/price`, BRD Báo giá v1.3, Phòng TTTT) và Contact (`pf/contact`, FN-13 BRD HCTH v3.0) là **của KN**.

Phân loại dưới đây đã theo các trả lời đó.

> Các chi tiết/số đếm mục 1–7 là kiểm kê ban đầu14/10. Mục 8 và ADR-0055 là cập nhật16/10 trên dev `988febd9`; quyết định PO mới có ưu tiên. Không dùng đề xuất cũ để cấp quyền ghi HTX hoặc giữ tenant KN trong lưu ký.

## Cách đọc

Mỗi mục có một trong ba hướng xử lý:

| Ký hiệu | Nghĩa |
|---|---|
| **→ KN** | Chuyển sang module KN (`internal/kn/*`, app web KN). Nền tảng không còn biết tới nó. |
| **Nền tảng** | Ở lại nền tảng, là dịch vụ dùng chung. KN gọi qua `port/` như mọi ứng dụng khác. |
| **Port** | Nền tảng cần một điểm mở rộng: port, registry hoặc hồ sơ cấu hình. KN đăng ký vào đó, nền tảng không import KN. |
| **Nền tảng dữ liệu** | Dữ liệu của hệ khác (HTX, MKG). KN đọc qua view `_pub`/data product và ghi qua lớp tích hợp, không gọi trong tiến trình. |

- Danh sách đầy đủ, từng tệp và từng ký hiệu, nằm ở [baseline.yaml](../../../tools/knboundary/baseline.yaml), 380 mục. Lệnh sinh lại: `go run ./tools/knboundary -list`.
- Tài liệu này gom các mục theo cụm và ghi hướng xử lý. Khi hai nguồn lệch nhau, baseline là nguồn đúng, vì CI kiểm nó.
- Phương pháp:
  - **Go:** `go list -f '{{.Imports}} {{.TestImports}}' ./...` lọc theo module KN; phân tích import và chuỗi SQL bằng `tools/knboundary`; `grep` ký hiệu.
  - **DB:** `module.yaml` (`owns_schema`), migration cụm pf, `dbadmin.PubReaders`. Thành viên vai đọc từ `pg_auth_members` của cụm dev.
  - **HTTP:** tag OpenAPI của `contracts/openapi/application.yaml` và `admin.yaml`.
  - **Web:** import TypeScript của `apps/web-ops/src`.
  - **Mobile:** chuỗi route trong `apps/field-app` và `packages/agrigreen_mobile`.
  - **Realm:** `deploy/compose/keycloak/*.json`, `app/*/app.yaml`.

## 0. Hiện trạng tóm tắt

- **Module chỉ của KN:**
  - `pf/extension` (domain): nhiệm vụ, chỉ đạo, báo cáo, lịch công tác, KHTC, CNTS dịch bệnh, vật tư;
  - `pf/price`: Báo giá. Khai `capability`, nhưng theo PO 14/10 là chức năng của KN (Phòng TTTT), không dùng chung;
  - `pf/hrm` (domain): nhân sự;
  - `pf/contact`: Contact FN-13 của Phòng HCTH. Khai `capability`, nhưng theo PO 14/10 là của KN, không dùng chung.

  Bốn module này nằm ở `internal/platform/{extension,price,hrm,contact}`, chạy chung binary `api-platform`/`worker`, cụm `pg-pf` và realm `agrigreen`.
- **Hướng ra của KN gần sạch.**
  - Mã KN chỉ import `port/` của module khác. Mã KN không có chuỗi SQL nào tới bảng của module khác. Quy tắc `kn-import-internal` và `kn-sql-foreign` có 0 vi phạm.
  - Riêng `pf/extension` gọi trong tiến trình port của hệ HTX (`cooperative`): 4 tệp, quy tắc `kn-import-htx`. Phải thay bằng đọc qua nền tảng dữ liệu (§1.2).
- **Hướng vào KN thì chưa sạch.** Nền tảng biết KN theo bốn cách:
  - `notify` và `reports` import port của KN (22 tệp);
  - module dùng chung chứa phần riêng KN (`accounts`, `party`, `cooperative`, `masterdata`, `authz`; 212 ký hiệu và tệp);
  - route `/v1/kn/*` do `party`, `cooperative`, `mechanization` phục vụ (46 thao tác ở bề mặt KN có tag không phải KN);
  - vỏ web dùng chung import `src/kn` (59 import).
- **Dữ liệu HTX.** Vai module KN không có quyền gì trên schema nội bộ HTX. Kiểm thử `TestKNRolesNoHTXInternalAccess_KNSPLIT0` và `TestKNRolesCannotWriteHTXLedgers_KNSPLIT0` đều PASS.
  - Người dùng KN vẫn đọc và ghi dữ liệu HTX qua route KN do module HTX phục vụ (vai `r_cooperative`). Họ ở **tenant lưu ký**, nên đọc theo `p_tenant` + `p_scope`, không qua cấp phép. Thu hồi cấp phép không cắt được quyền này.
  - Kiểm thử `TestKNPrincipalHTXDataAccess_KNSPLIT0/hien_tai_tenant_luu_ky` đánh dấu 5 ca XFAIL. Gói gỡ là KNSPLIT-4b.

## 1. Go

### 1.1 Nền tảng import module KN (`import-into-kn`: 22 tệp, 0 ở gốc lắp ráp)

| Nơi | Import | Vì sao | Hướng |
|---|---|---|---|
| `notify/module.go`, `notify/app/consume.go`, `notify/domain/{templates,report_cycle,work_schedule}.go` (+ 2 test domain, 3 test handler) | `extension/port`: tên sự kiện `pf.extension.*`, payload nhắc lịch, chu kỳ báo cáo | Mẫu thông báo cho sự kiện KN nằm trong `pf/notify` | **Port.** `notify` mở registry "loại thông báo" (sự kiện → mẫu → người nhận). KN đăng ký ở gốc lắp ráp, như `assembly.KNProfiles` của `cap/forms` đã làm. Chuyển `notify/domain/{report_cycle,work_schedule,price}.go` sang KN. |
| `notify/app/consume.go`, `notify/domain/price.go` (+ test) | `price/port`: loại thông báo Báo giá | Như trên | **Port**, như trên. Gốc lắp ráp đã có `assembly.NotifyPriceDesk` (hướng đúng). |
| `reports/app/{dispatch,service}.go` (+ 5 test handler) | `extension/port` | Báo cáo tổng hợp KN đọc dữ liệu `pf/extension` qua `reports/port/extensionsrc` | **Port.** `reports` giữ engine mẫu và xuất bản. Nguồn dữ liệu KN (`extensionsrc`) thành nguồn đăng ký (`reports/port.SourceRegistry`). Gói `reports/port/extensionsrc` → KN. |
| `accounts/app/kn_extension_test.go` | `extension/port` | Kiểm thử chéo | **→ KN** cùng phần KN của accounts. |
| Gốc lắp ráp: `cmd/api-platform/assembly/{contact,hrm,price,notify_price,forms,modules}.go`, `cmd/worker` (job `extension.*`, `price` calendar), `cmd/formimport` | module KN | Lắp ráp | **Nền tảng** (binary chung là đích). Chỉ khi KNSPLIT-5 (tùy chọn, sau G1) được mở mới chuyển sang `cmd/api-kn`, `cmd/worker-kn`. |
| `tests/acceptance`, `tests/e2e`, `tests/kn77` | `extension`, `extension/port` | Kiểm thử đầu-cuối | Giữ. `tests/kn77` → KN cùng KNSPLIT-1. |

### 1.2 Module KN import nền tảng và hệ khác

- `pf/extension` import `port/` của 10 module. Chín module là dịch vụ nền tảng dùng chung, hợp lệ ở đích. Module thứ mười là `cooperative`, thuộc hệ HTX và là ghép nối phải gỡ.
  - Ký hiệu dùng nhiều nhất: `syncport.*` (nguồn/đích đồng bộ), `extensionsrc.*`, `partyport.{AdminUnit,ActorNames,Dept*,Org*,Level*}`, `metricsport.*`, `formsport.{Publisher,Validator,Freezer,PeriodControl}`, `accountsport.{KNContext,KNOfficer,Schedule*}`, `geoport.Gps*`, `notifyport.StaffDirectory`.
  - Đọc HTX: `coopport.{Plots,Cooperatives,PlotStat,UnitCooperatives,CoopCentroid,HTXConfirmations}`.
- `pf/contact` import `vault/identity/client`. `pf/price` và `pf/hrm` chỉ dùng `internal/shared`.
- **Đọc dữ liệu HTX trong tiến trình, đi vòng `_pub` (quy tắc `kn-import-htx`, 4 tệp trong baseline).**
  - `extension/app/service.go` và `extension/module.go` gọi `coopport.Plots` và `coopport.Cooperatives`.
  - `extension/port/closedfig.go` gọi `coopport.HTXConfirmations`.
  - `extension/handler/unified_integration_test.go` (kiểm thử) cũng import port này.
  - Đây là lời gọi trong tiến trình. Module `cooperative` đọc bảng `pf_coop` dưới vai `r_cooperative`, trong ngữ cảnh của người dùng KN, chứ không đọc view `_pub`.
  - Theo PO 14/10, cách này **không được phép ở đích**: hệ độc lập không gọi trong tiến trình vào hệ khác.
  - **Hướng: Nền tảng dữ liệu.** KNSPLIT-2 đổi nguồn:
    - số tổng hợp và trạng thái xác nhận HTX: view `pf_coop_pub`/`pf_cropcycle_pub` có phiên bản (thêm `pf/extension` vào `PubReaders`);
    - dòng chi tiết (thửa, HTX theo đơn vị): data product có cấp phép (`p_share`, KNSPLIT-4b).

### 1.3 Phần riêng KN trong module dùng chung (`kn-symbol-in-shared`: 212 mục)

| Module | Tệp / ký hiệu chính | Nội dung | Hướng |
|---|---|---|---|
| `internal/shared/authz` | `kn.go`: `KNContext`, `RequireKN`, `KNRoles`, `KNTW/KNTinh/KNXa/KNAdmin`, `KNSupport`, `ErrNoKNContext` | Ngữ cảnh phiên KN (header `X-AG-Context`), dùng ở 20 tệp ngoài KN: `accounts`, `cooperative`, `forms`, `mechanization`, `notify`, `party`, `reports`, `shared/db` | **Port.** `authz` giữ khái niệm chung "ngữ cảnh ứng dụng" (vai ngữ cảnh, đơn vị, phòng ban, phạm vi). `KN*` → `internal/kn/authz`. `shared/db.RequiresNode` đọc vai gắn Nút từ `app.yaml`, không hằng KN. |
| `pf/accounts` | `app/kn*.go` (tạo/nhập cán bộ KN, ngữ cảnh, phiên, hỗ trợ, vault xã), `domain/kn*.go` (luật vai × loại Nút), `handler/kn*.go` (`/v1/accounts/kn-users*`, `/v1/me/context*`), `port/kn*.go` (`KNContext`, `KNContextResolver`, `KNDirectory`, `KNPeople`, `KNOfficer`), `store/kn*.go`, `module.go` (`ExtensionKNContext`, `ExtensionKNDirectory`, `KNPeople`), `port/grants.go` (`KNOfficerRoles`); kiểm kê tay: `{app,domain,handler,port,store}/internal_unit.go` (đơn vị nội bộ), `{app,store}/dormant.go` (job vô hiệu 45 ngày, D24) | Tài khoản cán bộ, gán vai theo Nút/phòng ban, ủy quyền, phiên hỗ trợ | **Tách đôi.** Tài khoản, khóa, MFA, vô hiệu 45 ngày, phiên và ủy quyền chung là **Nền tảng** (dịch vụ dùng chung). Luật vai KN, nhập cán bộ KN, ngữ cảnh KN, đơn vị nội bộ là **→ KN**. Nền tảng cần **Port** "luật gán vai theo ứng dụng" (validator theo `app`) và "bộ phân giải ngữ cảnh" (thay `KNContextResolver`). |
| `pf/party` | `app/kn*.go` (menu KN, cây, vùng, thu thập HTX), `domain/kn.go`, `handler/kn*.go` (`/v1/kn/cooperatives*`, `/v1/menu`), `port/kn*.go`, `store/kn*.go` (`KNEvent`), `port/registry.go` (`RoleKN*`); kiểm kê tay: `domain/menu.go`, `domain/menu_departments.go` (`KHTCReportItem`, …) | Menu KN, phòng ban, cây tổ chức KN, danh sách HTX cho KN | **Nút/cây/vùng, phòng ban (dữ liệu):** Nền tảng. **Menu KN, mục menu phòng ban:** → KN. Nền tảng có **Port** "registry menu theo ứng dụng"; `/v1/menu` trả hợp của các registry. **Thu thập HTX cho KN** (`/v1/kn/cooperatives*`): → KN, đọc HTX qua **Nền tảng dữ liệu** (data product có cấp phép), không qua port `cooperative`. |
| `pf/cooperative` | `member/{app,handler,store}/kn_*.go` (`/v1/kn/cooperatives/{ref}/households`, `/v1/kn/households/*`, sổ `pf_coop.kn_profile_event`), `plot/{app,domain,handler,store}/kn*.go` (`/v1/kn/plots*`, `KN*Plot`, `SourceKN*`), `port/member.go` (`HouseholdSourceKNApp`), `anchor` (`CreatedByKN`, `roleKN*`), `season/app`, `cropcycle/app` (`accKN`); kiểm kê tay: `plot/app/department_usage.go` | Màn KN đọc/ghi dữ liệu HTX (vẽ thửa, hộ) | **Nền tảng dữ liệu.** Route `/v1/kn/*` chuyển sang KN. KN đọc qua data product/view `_pub` có cấp phép, ghi thay (ADR-0052 §7, nếu Q-KNS-04 giữ) qua lớp tích hợp. KN không gọi `cooperativeport` trong tiến trình. Nhãn nguồn `SourceKN*`, `CreatedByKN` giữ ở hệ HTX như giá trị dữ liệu. |
| `pf/mechanization` | `handler/machine_count.go` (`RegisterKNMachineCounts`: `/v1/kn/machine-counts*`); kiểm kê tay: `app/department_usage.go` | Đếm máy theo HTX cho KN | Như `cooperative`: route → KN, dữ liệu ở lại. |
| `pf/masterdata` | `{app,domain,port}/cnts.go` (`CNTSCatalogs`, 12 danh mục `CatalogCNTS*`, 12 tham số `ParamCNTS*`), `module.go` | Danh mục Chăn nuôi – Thủy sản (CNTS) | **Port.** `masterdata` giữ engine danh mục và tham số. Định nghĩa danh mục và tham số CNTS → KN, đăng ký qua registry danh mục (seed/manifest), như hồ sơ `cap/forms`. |
| `pf/forms` | `app/service.go`, `port/port.go` dùng `authz.KN*`. Hồ sơ `kn`, `kn_cnts`, `kn_khtc` **đã** ở gốc lắp ráp (`assembly.KNProfiles`) | Vai phát hành/nộp theo vai ngữ cảnh KN | **Port**, đã gần xong: thay `authz.KN*` trong `forms` bằng vai lấy từ `Profile`. |
| `pf/notify` | kiểm kê tay: `domain/{price,report_cycle,work_schedule}.go` | Loại thông báo Báo giá, kỳ báo cáo, lịch công tác | **→ KN**, qua registry ở §1.1. |
| `pf/reports` | kiểm kê tay: `port/extensionsrc/extensionsrc.go` | Nguồn báo cáo tổng hợp KN | **→ KN**, qua registry ở §1.1. |

### 1.4 Gốc lắp ráp và job

| Đơn vị | Phần KN | Hướng |
|---|---|---|
| `cmd/api-platform` | `assembly/{contact,hrm,price,notify_price,forms}.go`, đăng ký `extension`, `price`, `hrm`, `contact` trong `modules.go` | KNSPLIT-1: gọi `internal/kn/*`. KNSPLIT-5 (tùy chọn): tách `cmd/api-kn`. |
| `cmd/worker` | job `extension.JobEscalation`, `JobWorkSchedule`, `JobReportCycle`, `priceport.JobCalendar`, `accport.JobDormant` (chung), `notify.go`/`reports.go` dựng `extension.New` | Như trên: `cmd/worker-kn`. |
| `cmd/formimport` | nhập biểu mẫu KN | → KN. |
| Vai DB đăng nhập | `extension_app` (vai cũ, thành viên `r_extension`) | Gỡ khi có vai đăng nhập riêng; không bắt buộc trong đích logic. |

## 2. Cơ sở dữ liệu (cụm `pg-pf`)

### 2.1 Schema của KN

| Schema | Bảng/view | Module |
|---|---|---|
| `pf_extension` | 49 | `pf/extension` |
| `pf_extension_pub` | 6 view (`v1_task`, `v1_task_event`, `v1_field_report`, `v1_directive_target_ack`, `v1_consolidation_closed`, `v1_closed_figures`) | `pf/extension` |
| `pf_price`, `pf_price_pub` | 22, 2 | `pf/price` |
| `pf_hrm` | 7 | `pf/hrm` |
| `pf_contact` | 2 | `pf/contact` |

Tên schema vật lý giữ nguyên ở cả hai giai đoạn: chuỗi băm sổ, `shared.ledger_idem`, cấp quyền và hợp đồng `_pub` gắn với tên. Thêm hay đổi tiền tố `pf_` là việc của giai đoạn vật lý, nếu cần.

### 2.2 KN chạm vào ngoài schema của mình

| Vai | Đối tượng | Nguồn | Hướng |
|---|---|---|---|
| `r_extension` | thành viên `r_forms_pub`, `r_price_pub`, `r_extension_pub` (đọc `closed_figures/v1`) | `dbadmin.PubReaders["pf/extension"]` (ADR-0053) | **Nền tảng**, đúng mô hình `_pub` |
| `r_extension` | `USAGE` + `EXECUTE pf_forms.mirror` (SECURITY DEFINER) | pf 000122; ngoại lệ `schema_isolation_exceptions.yaml` (ADR-0028 write-port) | **Port** ghi lâu dài; bỏ khi KN dùng kỳ native |
| `r_price`, `r_hrm`, `r_contact` | `SELECT k_ref.{org,node_closure,admin_unit,share_grant_active,share_scope}`, `EXECUTE k_ref.ctx_*` | pf 000138, 000147, 20261013230912 | **Nền tảng** (k_ref mọi `r_*` đọc) |
| `r_extension`, `r_price`, `r_hrm`, `r_contact` | `ag_shared_user` (outbox, ledger qua `shared.*`) | bootstrap | **Nền tảng** |
| Bất kỳ vai KN nào | **không** có quyền trên `pf_coop`, `pf_cropcycle`, `pf_mechanization` (kể cả `_pub` HTX) | kiểm thử `TestKNRolesNoHTXInternalAccess_KNSPLIT0` | Đúng ở đích |

### 2.3 Ngoài KN chạm vào schema của KN (`sql-into-kn`: 20 mục)

| Nơi | Đối tượng | Hướng |
|---|---|---|
| `migrations/pf/000114_pf_ops_jobs_grants.up.sql` | `ag_ops_jobs` đọc cột `pf_extension.{task_current,report_period,work_schedule_current,staff}` (đếm hạn) | **Nền tảng** (vai vận hành). Mục vĩnh viễn: migration không sửa được. Đếm hạn mới của KN đi qua view `_pub` hoặc hàm của KN. |
| `migrations/pf/000124_pf_pub_v1.up.sql` | Dựng view `pf_extension_pub.*` từ `pf_extension.*` | Hợp đồng `_pub` của KN, mục vĩnh viễn. View mới của KN đặt trong migration `pf_extension_*`. |
| `internal/shared/opsjobs/opsjobs_integration_test.go`, `internal/platform/metrics/handler/pub_integration_test.go` | Kiểm thử seed bảng KN | **→ KN**: chuyển seed vào gói kiểm thử KN hoặc dùng port. |
| `r_metrics`, `r_agg` | thành viên `r_extension_pub`, `r_price_pub` | **Nền tảng** (`cap/analytics` đọc mọi `_pub`). |

### 2.4 Dữ liệu HTX: đường đọc/ghi của người dùng KN

| Đường | Hiện tại (ĐTK 1, tenant lưu ký) | Đích (KNSPLIT-4b) | Kiểm thử |
|---|---|---|---|
| Đọc bảng HTX không khai `shares` (`pf_coop.cooperative`) | Thấy cả cây (p_scope) | 0 dòng | XFAIL / PASS |
| Đọc thửa, hộ ngoài phạm vi cấp phép | Thấy | Chỉ phạm vi cấp phép | XFAIL / PASS |
| Ghi bảng, sổ HTX (`pf_coop.plot`, `pf_coop.plot_event`) | Ghi được (KN nhập thay theo thỏa thuận, ADR-0052 §7) | 42501 (RLS) | XFAIL / PASS |
| Thu hồi cấp phép | Không cắt | Mất quyền ngay | XFAIL / PASS |
| Cấp phép hết hạn | Không cắt | Mất quyền ngay | XFAIL / PASS |

Sổ HTX mà luồng KN ghi hôm nay:
- `pf_coop.plot_event` (vẽ và sửa thửa, `/v1/kn/plots*`);
- `pf_coop.kn_profile_event` và `pf_party.kn_profile_event` (hồ sơ hộ, HTX KN);
- `pf_mechanization.coop_machine_count_event` (`/v1/kn/machine-counts*`).

Ở đích:
- **Đọc** của KN đi qua nền tảng dữ liệu (view `_pub`, data product). Cùng cụm thì RLS `p_share` lọc theo cấp phép `intra_platform`, `purpose_code = khuyen-nong`. Không gọi port HTX trong tiến trình (PO 14/10).
- **Ghi**: KN chỉ tạo bản riêng KN và chia sẻ qua nền tảng dữ liệu, không ghi HTX kể cả qua lệnh tích hợp. D2-KN-01 chờ PO chốt bên tiếp nhận/tạo HTX tạm. RLS `p_share` chỉ cho SELECT.
- Q-KNS-04 đã chốt; chỉ đề xuất đối chiếu D2 tại mục Ảnh hưởng ADR-0055 còn chờ PO.

## 3. HTTP (OpenAPI)

### 3.1 Route của KN, do module KN phục vụ (đúng ở đích)

| Tiền tố | Số thao tác | Module |
|---|---|---|
| `/v1/extension/*` | 92 path (34 thao tác có tag, phần còn lại chưa tag) | `pf/extension` |
| `/v1/price/*` | 72 path, 81 thao tác | `pf/price` |
| `/v1/hrm/*` | 22 path, 27 thao tác | `pf/hrm` |
| `/v1/contacts/*` | 6 path, 8 thao tác | `pf/contact` |
| `/v1/kn/materials*`, `/v1/kn/khtc/*`, `/v1/support-requests` | 19 + 2 | `pf/extension` |

### 3.2 Bề mặt KN do module khác phục vụ (`route-owner`: 46 thao tác)

| Route | Module phục vụ | Hướng |
|---|---|---|
| `/v1/kn/cooperatives*`, `/v1/kn/households*` | `pf/party` + `pf/cooperative` | **→ KN**: đọc qua nền tảng dữ liệu, ghi qua lớp tích hợp |
| `/v1/kn/plots*` (tag `geo`, phục vụ bởi `pf/cooperative` từ W3.3) | `pf/cooperative` | **→ KN**: đọc qua nền tảng dữ liệu, ghi qua lớp tích hợp; sửa tag |
| `/v1/kn/machine-counts*` | `pf/mechanization` | **→ KN**: như trên (không gọi port HTX trong tiến trình) |
| `/v1/menu`, `/v1/departments*` (app + admin) | `pf/party` | **Port**: registry menu; phòng ban giữ ở nền tảng nếu ứng dụng khác dùng, còn không → KN |
| `/v1/me/context`, `/v1/me/contexts`, `/v1/me/delegations*`, `/v1/internal-units` | `pf/accounts` | **Port**: ngữ cảnh ứng dụng chung; đơn vị nội bộ → KN |
| Admin `/v1/accounts/kn-users*`, `/v1/accounts/internal-units*`, `/v1/accounts/users/{id}/assignments*`, `/v1/accounts/users/{id}/delegations`, `/v1/accounts/delegations/*`, `/v1/accounts/support-*` | `pf/accounts` | Nhập cán bộ KN, đơn vị nội bộ → KN. Gán vai, ủy quyền, phiên hỗ trợ giữ ở nền tảng (dùng chung) qua port luật vai |

Route nền tảng KN dùng như ứng dụng khác (**Nền tảng**):
- `/v1/forms/*`, `/v1/reports/*`, `/v1/masterdata/*`, `/v1/documents`, `/v1/library`, `/v1/notifications`, `/v1/sync/*`, `/v1/files`, `/v1/geo/*`, `/v1/gis`, `/v1/metrics/*`, `/v1/orgs/*`, `/v1/regions`, `/v1/me`, `/v1/me/modules`, `/v1/mrv/*`;
- admin `/v1/share-grants*`, `/v1/htx-anchors*`.

## 4. Web (`apps/web-ops`)

### 4.1 Vùng KN

- `src/kn/**`: 94 tệp.
- `src/app/kn/**`: 74 trang.
- Mock `src/api/mock/kn-*.ts` và `src/api/mock/kn-reports/**`.

Các màn nghiệp vụ là **→ KN**. G1 giữ apps/web-ops với client kn-web; apps/web-kn riêng sau G1.

### 4.2 Vỏ dùng chung import KN (`web-import-into-kn`: 59 mục)

| Tệp | Import từ KN | Hướng |
|---|---|---|
| `src/auth/AuthProvider.tsx` | `kn/client`, `kn/contract` (`KnApi`, `SessionContext`, `RoleAssignment`, `MenuTree`), `kn/contextStore`, `kn/session`, `kn/cycle/api` | **Port.** Hợp đồng phiên/ngữ cảnh/menu chung chuyển sang `src/shell/contract.ts` (hoặc `packages/web-shell`). API KN đăng ký vào provider. |
| `src/components/AppShell.tsx`, `src/shell/{Sidebar,menu,RoleContextPicker,AccountMenu}.tsx`, `src/shell/shell.test.tsx` | `kn/contract` (`MenuTree`, `MenuNode`, `MenuBadges`, `KN_ROLE_LABEL`), `kn/session`, `kn/SupportBanner` | **Port**, như trên. Nhãn vai KN do ứng dụng cung cấp. |
| `src/app/chon-vai-tro/page.tsx` | `kn/contract`, `kn/session` | Như trên. |
| `src/api/mock/handler.ts`, `src/api/mock/kn.ts`, `src/api/mock/personas.ts` | 20 mock KN | **Port**: registry mock theo ứng dụng. |
| `src/api/mock/forms/*` (8 tệp) | `kn/forms/types`, `kn/grid/{decimal,letters}`, `api/mock/kn-accounts` | **Nền tảng**: lưới và kiểu biểu mẫu là của `cap/forms`. Chuyển `kn/grid`, `kn/forms/types` xuống `src/lib/forms`. |

### 4.3 Vùng KN import ứng dụng khác (`web-kn-import-app`: 2 mục)

| Tệp | Import | Hướng |
|---|---|---|
| `src/kn/cnts/catalog/CntsCatalogScreen.tsx` | `htx/rules` (`toCsv`) | Chuyển `toCsv` xuống `src/lib`. |
| `src/kn/cycle/screens/AdminScreens.tsx` | `admin/accounts` (`mfaState`, `canResetTotp`) | Chuyển xuống `src/lib/accounts`, hoặc màn quản trị tài khoản KN dùng app quản trị realm `agrigreen-kn-admin`. |

KN dùng chung, hợp lệ: `@/api` (161 import, client sinh từ OpenAPI), `@/components` (108), `@/auth` (29), `@/lib` (26), `@/shell` (10), `@/nav` (2), `@/config`.

## 5. Mobile

| Thành phần | KN | Hướng |
|---|---|---|
| `apps/field-app` (client `agrigreen-field-app`) | App hiện trường KN. Gọi `/v1/kn/{cooperatives,plots,materials,machine-counts}`, `/v1/extension/{roster,work-schedules,report-tasks,report-badges,commune-reports,directives}`, `/v1/menu`, `/v1/me/context*`, cộng MRV (`/v1/mrv/catalog`), thư viện, `/v1/masterdata/catalogs` | **→ KN** (đã là app riêng). PO đã chốt thu thập MRV thuộc HTX/MKG (Q-KNS-06), không chuyển dữ liệu/quyền MRV vào KN. |
| `packages/agrigreen_mobile` (dùng chung với `htx-app`, `mkg-field-app`) | Chứa client `/v1/kn/{cooperatives,households,plots,materials,machine-counts}`, `/v1/extension/{work-schedules,directives}` | **Port**: tách thư viện KN (`packages/agrigreen_kn_mobile`); gói chung chỉ giữ đồng bộ, thiết bị, tệp, thông báo. |

## 6. Realm, client, vai

| Mục | Hiện tại | Đích (độc lập logic) | Tùy chọn sau G1, chỉ khi vận hành cần (NĐ 331 không đòi, PO 14/10) |
|---|---|---|---|
| Realm người dùng | `agrigreen`, chung cho mọi ứng dụng | Giữ `agrigreen` | Có thể `agrigreen-kn` |
| Realm quản trị | `agrigreen-kn-admin` (D24/D25, client `agrigreen-kn-admin-web`) | Giữ | Giữ |
| Client web | `agrigreen-web-ops` (KN, HTX, CGH, SIM, PF chung) | Thêm `kn-web` cho `apps/web-kn` | Như trái |
| Client mobile | `agrigreen-field-app` | Giữ | Như trái |
| Vai realm của KN | `kn_national_admin`, `kn_province_admin`, `kn_officer`, `kn_user`, `content_editor_national`, `content_editor_province`, `content_reviewer`, `mrv_collector`, `mrv_reviewer`, `mrv_viewer`, `mrv_exporter` (`app/KN/app.yaml`) | Giữ tên. Client `kn-web` chỉ nhận vai KN (client scope) | Chuyển sang realm KN |
| Vai ngữ cảnh | `kn_tw`, `kn_tinh`, `kn_xa`, `admin` (`authz/kn.go`, header `X-AG-Context`) | → `internal/kn/authz` | — |
| Vai dùng chung với ứng dụng khác | `platform_admin`, `account_admin(_province)`, `registry_admin`, `reports_admin`, `masterdata_admin` | Nền tảng | Tài khoản quản trị riêng từng hệ (ADR-22) |

## 7. Cổng `knboundary` và cách co danh sách

| Quy tắc | Mục hiện có | Gói gỡ |
|---|---:|---|
| `import-into-kn` | 22 | KNSPLIT-2 (registry `notify`, `reports`) |
| `kn-import-internal` | 0 | — (giữ 0) |
| `kn-import-htx` | 4 | KNSPLIT-2 (đọc qua `_pub`/data product); KNSPLIT-4 (dòng chi tiết dưới tenant KN) |
| `kn-sql-foreign` | 0 | — (giữ 0) |
| `sql-into-kn` | 20 | KNSPLIT-2 (kiểm thử). Mục migration là vĩnh viễn |
| `kn-symbol-in-shared` | 212 | KNSPLIT-2 |
| `route-owner` | 46 | KNSPLIT-2 |
| `web-import-into-kn` | 59 | KNSPLIT-3 |
| `web-kn-import-app` | 2 | KNSPLIT-3 |

- **Khi gỡ một ghép nối:** chạy `go run ./tools/knboundary -prune` trong cùng PR. Mục lỗi thời làm hỏng cổng.
- **Khi lỡ thêm ghép nối:** cổng hỏng với dòng `MỚI …`. Không thêm vào baseline: CI chạy `-base origin/<nhánh đích>` và chặn baseline lớn lên.
- **Khi đổi `kn_modules`:** KNSPLIT-1 chuyển `internal/platform/{extension,price,hrm,contact}` → `internal/kn/*`. Cấu hình đã có sẵn `internal/kn`. Baseline dùng canonical_paths cố định từ gói 0, nên ID ghép nối không đổi khi chuyển namespace; không sửa alias hoặc thêm mục baseline.

**Giới hạn của cổng:**
- Ký hiệu riêng KN không mang chữ KN/CNTS/KHTC (ví dụ hằng phòng ban `DeptTrongTrot`) chỉ bị bắt khi tệp có trong `kn_inventory_files`.
- Route không có tag chỉ được xét ở tiền tố bề mặt dùng chung.
- Mobile và realm không được cổng kiểm. Chúng nằm trong danh sách việc của KNSPLIT-3/4.


## 8. Cập nhật tiếp nhận 16/10/2026

Mốc dev `988febd9` sau W11.46. Baseline cổng ban đầu được sinh lại theo CODEX-G1; các số trong phần kiểm kê14/10ở trên là lịch sử. Sau tích hợp KNSPLIT-0 chỉ được giảm baseline.

| Quy tắc | Ghép nối hiện hành |
|---|---:|
| `import-into-kn` | 25 |
| `kn-import-htx` | 4 |
| `kn-sql-foreign` | 1 |
| `kn-symbol-in-shared` | 241 |
| `kn-write-htx` | 10 |
| `route-owner` | 46 |
| `sql-into-kn` | 20 |
| `web-import-into-kn` | 65 |
| `web-kn-import-app` | 2 |
| Tổng | 414 |

PO đã chốt tenant riêng mỗi Trung tâm tỉnh và chuyển dữ liệu KN cũ trong G1, KN không ghi HTX (chỉ tạo bản riêng), MRV thuộc HTX/MKG. Client G1 là kn-web trong realm agrigreen; web-kn/mobile KN riêng sau G1. Đối chiếu D2-KN-01 tại ADR-0055 là đề xuất chờ PO, chưa sửa ADR-0052.

KNSPLIT-GATE-001/002/003: chuyển thư mục và import cơ học giữ cùng baseline; import mới vẫn làm cổng đỏ. canonical_paths đóng băng khi so nhánh đích. Ref đích không tồn tại/fetch thiếu gây lỗi, chỉ tệp baseline/config chưa từng tồn tại ở ref hợp lệ mới là khởi tạo. Alias không miễn bất kỳ quy tắc nào.
