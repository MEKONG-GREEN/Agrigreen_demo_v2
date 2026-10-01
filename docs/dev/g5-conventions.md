# Quy ước phát triển G5

Tài liệu này chỉ ghi phần **thêm** cho G5. Đọc cùng các tài liệu trước:
- [g1-conventions.md](g1-conventions.md)
- [g2-conventions.md](g2-conventions.md)
- [g3-conventions.md](g3-conventions.md)
- [g4-conventions.md](g4-conventions.md)

Mọi quy ước của các cổng trước vẫn áp dụng: cấu trúc module, RLS, sổ chỉ ghi thêm, PII chỉ ở vault, kiểm thử bắt buộc, tiền tố mã story, lệnh kiểm tra trước khi báo xong.

Cổng G5 (kiến trúc Phụ lục A, Q4/2028): "**Phase K; `pf/metering` và Public API bật với bên thứ ba đầu tiên**". Các điều khoản kiến trúc liên quan:

| Mục kiến trúc | Nội dung cho G5 |
|---|---|
| §2 | Người dùng ĐTK 2 có "bên thứ ba, đơn vị thẩm định"; Phase K là trạng thái cấp phát, không phải điểm thiết kế |
| §11 | Bề mặt Public `partner.<domain>`: khóa API lưu băm, scope, quota, **tắt ở ĐTK 1**, mọi lời gọi ghi sổ đo lường |
| §16 | `pf/metering` là chứng từ theo mẫu sổ mục 8, mỗi lời gọi Public API một dòng |
| §18 | Mọi đường xuất (van, Public API, gói, báo cáo) đi qua **một** điểm kiểm soát có nhật ký, hạn mức, cảnh báo |
| R-01 | Chuyển giai đoạn chỉ là đổi cấu hình và số bản sao |

**Không nhầm** Public API với `/v1/partner/*` của [ADR-0020](../adr/0020-lien-thong-platform-mkg-khac-tenant.md). Bề mặt ADR-0020 nằm trên Application API (vai `mkg_sync`), là cửa nền tảng → MKG. Public API là cửa nền tảng → bên thứ ba.

G5-0 (01/10/2026, nhánh `sub/g5`) dựng nền và **đóng băng hợp đồng** để agent module (`pf/apiclient`, `pf/metering`, tích hợp DLP), hạ tầng (Phase K), web và E2E làm song song. Bảng nghiệm thu: [g5-acceptance.md](../uat/g5-acceptance.md).

ADR của G5:

| ADR | Nội dung |
|---|---|
| [ADR-0022](../adr/0022-public-api-ben-thu-ba.md) | Public API |
| [ADR-0023](../adr/0023-so-do-luong-su-dung-metering.md) | `pf/metering` |
| [ADR-0024](../adr/0024-diem-kiem-soat-xuat-du-lieu-dlp.md) | Điểm kiểm soát xuất dữ liệu (DLP) |
| [ADR-0025](../adr/0025-ho-so-cap-phat-phase-k.md) | Hồ sơ Phase K |

Cả bốn ở trạng thái **Đề xuất**.

## 1. Quyết định đã chốt ở G5-0 (01/10/2026)

| Chủ đề | Quyết định | Căn cứ |
|---|---|---|
| Đơn vị triển khai | Public API là **tiến trình riêng `cmd/api-public`** (`:8085`, Deployment `api-public`, namespace `agrigreen-pf`), không phải listener thứ ba của `api-platform`. Nhị phân chỉ có route Public. `AG_PUBLIC_API_ENABLED` ≠ `1` → thoát lỗi khi khởi động. ĐTK 1: 0 bản sao, không có host ingress | ADR-0022 §1, R-01 |
| Ranh giới mã | archlint: `cmd/api-public` không import `internal/mkg/**` (R-03) và **không import kho định danh, kể cả client** (RB-3). Đã cưỡng chế ở G5-0 | ADR-0022 §1 |
| Bề mặt quản trị | Đăng ký khách hàng, khóa, đăng ký sản phẩm, gói hạn mức, bảng kê, nhật ký xuất: **Admin API** của `api-platform` (MFA). Không có cổng tự phục vụ cho bên thứ ba ở G5 | mục 11 |
| Module | `pf/apiclient` (khách hàng, khóa, scope, đăng ký, gói hạn mức, việc xuất, handler sản phẩm) · `pf/metering` (sổ sử dụng, bộ đếm, điểm kiểm soát xuất, bảng kê) | ADR-0022 §2 |
| Tenant | Bên thứ ba = **tenant riêng**. Khách hàng API, khóa, sổ sử dụng, bảng kê thuộc tenant **đơn vị vận hành** (`AG_PUBLIC_OPERATOR_TENANT`). Dữ liệu phục vụ thuộc tenant chủ dữ liệu, và chỉ đọc được khi có cấp quyền ADR-0020 loại `dp_<sản phẩm>` (bên cấp → tenant bên thứ ba). Tenant bên gọi lấy từ bản ghi khóa | ADR-0009, ADR-0020, ADR-0022 §3 |
| Khóa API | Dạng `agk_<prefix 12 base32>_<32 byte base64url>`, header `X-API-Key`. Lưu `key_prefix` + HMAC-SHA-256(pepper theo phiên bản, khóa), không argon2id. Bí mật hiện một lần, tag `pii:"true"`. Vòng đời là sổ (`issued`/`rotated`/`revoked`/`expired`). Tối đa 2 khóa hiệu lực, hạn 365 ngày, chồng lấn khi xoay 7 ngày | ADR-0022 §4, RB-1, R-92 |
| Xác thực mạnh hơn | `oauth_client_credentials` (Keycloak, R-54) tùy chọn cho đơn vị thẩm định. `mtls` để sau | ADR-0022 §5 |
| Truy cập hiệu lực | khóa hiệu lực ∧ scope (đăng ký sản phẩm được duyệt, R-56) ∧ cấp quyền `dp_*` đang hiệu lực ∧ sản phẩm phục vụ được (production chỉ `approved`). Thiếu một điều kiện → 403 `apiclient.forbidden` | ADR-0022 §6 |
| Giới hạn | Gateway: giới hạn thô theo IP, không thẩm quyền (R-55). Backend: token bucket theo khách hàng (Valkey nếu có, không thì trong tiến trình). **Hạn mức thẩm quyền** = bộ đếm `pf_metering` cập nhật cùng giao dịch với sổ | ADR-0022 §7, mục 4 |
| R-50/51/52/53 | `/v1`, header `AG-Data-Contract`; `Idempotency-Key` bắt buộc với POST, gọi lặp → 0 đơn vị; xuất → 202 + `/v1/jobs/{id}`, tệp phát luồng qua API (**không** URL có chữ ký); cursor | ADR-0022 §8 |
| Hợp đồng dữ liệu | `contracts/dataproduct/<tên>.json` (gói Go `contracts/dataproduct`, dùng lại `publication.Node` / `Validate`), mặc định từ chối, `personal_data: false`. `contractcheck` kiểm struct `//ag:dataproduct` và tag OpenAPI. Lúc chạy `Validate` từng phần tử. **Đã cưỡng chế ở G5-0** | ADR-0022 §9 |
| Sổ sử dụng | Một dòng mỗi lời gọi **đã xác thực**, kể cả lỗi sau xác thực. `stream_key = client:<id>:<YYYY-MM>`. Ghi **trước** khi gửi byte, lỗi → 503, không trả dữ liệu | ADR-0023 §1 |
| Đơn vị | `metering.units.v1` (`port.UnitsV1`): đọc 2xx = 1; tải tệp = ceil(dòng/1000); còn lại 0 | ADR-0023 §2 |
| Bảng kê | Kỳ tháng, 202 + job, ảnh chụp có băm + đầu chuỗi. `billing_officer` yêu cầu ≠ `billing_approver` duyệt. Giá **tùy chọn**, chờ khách. Lập hóa đơn ngoài hệ thống (CSV tải qua kênh `report_export`) | ADR-0023 §4 |
| DLP | `meteringport.Checkpoint.Pass` là **điểm kiểm soát duy nhất**, sổ `pf_metering.egress_event` (tenant = chủ dữ liệu). `AG_EGRESS_MODE=observe` (ĐTK 1, không từ chối, lỗi chỉ log) / `enforce` (Phase K). `port.Guard` áp ngữ nghĩa thống nhất; checkpoint nil → hành vi cũ. Van kiểm ở **phía nhận** (consumer van của nền tảng) | ADR-0024 |
| Báo cáo | `pf/reports` **giữ** điểm kiểm soát cục bộ (`app/dlp.go`: hạn mức người/ngày, `pf.reports.export.anomaly`) và **thêm** `Guard` kênh `report_export`. Không đổi mã lỗi hiện có | ADR-0024 §3 |
| Phase K | Lớp giá trị `values-phasek.yaml` chỉ gồm cấu hình và số bản sao. Thủ tục cấp tenant và khách hàng theo runbook | ADR-0025 |

### 1.1 Căn cứ yêu cầu (rà 01/10/2026)

BRD và backlog **không có** yêu cầu nào về Public API, khóa API, đo lường, thu phí, VVB, bên mua tín chỉ hay ngưỡng k. Danh sách tài liệu đã rà:
- BRD KN v1.0, BRD GIS v1.3, BRD CGH v1.3;
- backlog KN, GIS, CGH, HTX;
- `docs/requirements/tong-hop.md`.

Mọi yêu cầu G5 đến từ ba nguồn: kiến trúc v3.0, một vài dòng gần nhất (bảng dưới), và prototype `mekong-green` dùng làm đặc tả chạy được (đọc quy tắc, không chép mã).

| Nguồn | Nội dung | Dùng cho |
|---|---|---|
| KN US-MRV-05 (FN-08) AC | Chuyên gia thẩm định chỉ xem, không sửa/xóa dữ liệu gốc, mọi lần truy cập có nhật ký | `mrv_package_status`, ADR-0025 §3 |
| GIS FN-17 / BR-59, US-INT-02 | TMS dùng bản đồ chuẩn hóa — tiêu thụ máy duy nhất có tên. Đã phục vụ qua vai `map_consumer` (Application API), **không** chuyển sang Public API | — |
| prototype `rbac.ts`, `seed.ts` | Vai `vvb_auditor` chỉ đọc MRV/GIS/báo cáo; đối tác loại `customer`/`vendor`/`vvb`/`buyer` (nhà máy VFT, EVN, VVB-SGS, quỹ mua tín chỉ) | loại bên thứ ba |
| prototype `finance/service.ts` `revenue_rules` | `SUB-DATA` "Data Licensing" dự báo sản lượng cho nhà máy, giá cố định 450 000 000 đ (kỳ hạn không ghi) | đề xuất bên thứ ba đầu tiên = nhà máy; giá **không** làm mặc định |
| prototype `mrv/sync-lakehouse` | Đẩy tới 1 000 bản ghi MRV thô ra "Data Lakehouse", không ngưỡng, không ẩn danh | **phản mẫu**: G5 không xuất bản ghi thô |
| prototype `rateLimit.ts` | Bộ đếm cửa sổ cố định chỉ cho đăng nhập | — |
| `pf/reports/app/dlp.go` (G1) | Điểm kiểm soát xuất cục bộ: hạn mức 50/người/ngày, ngưỡng bất thường 30 | giữ, nối vào checkpoint chung |

### 1.2 Tham số tạm (tạm, chờ khách)

Tham số vùng nền tảng nằm ở `pf/masterdata` (có phiên bản, setter ≠ approver, áp theo thời điểm sự việc). Gói hạn mức của khách hàng là bản ghi có phiên bản ở `pf_apiclient`. Khóa tham số là hằng trong `port/`.

| Khóa | Mặc định | Ghi chú |
|---|---|---|
| `publicdata.k_min` (`apiclientport.ParamKMin`) | **10** nông hộ phân biệt | C-3 / Q-MKG-11, chờ MKG ký |
| Gói hạn mức: `requests_per_minute` / `daily_units` / `monthly_units` / `max_export_rows` | 60 / 10 000 / 200 000 / 100 000 | `apiclientport.Default*`; `api_client_admin` đặt ≠ `api_client_approver` duyệt |
| Hạn khóa / chồng lấn khi xoay | 365 ngày / 7 ngày | `DefaultKeyLifetime`, `DefaultRotateGrace` |
| `metering.egress.public_api.daily_records_quota` | 0 (không giới hạn riêng; hạn mức gói là thẩm quyền) | |
| `metering.egress.mrv_package.daily_records_quota` | 50 000 bằng chứng / tenant / bên nhận / ngày | |
| `metering.egress.report_export.daily_records_quota` | 0 (hạn mức người/ngày của reports đã có) | |
| `metering.egress.valve.daily_records_quota` | 0 (chỉ cảnh báo — chờ khách: có chặn van không) | |
| `metering.egress.partner_share.daily_records_quota` | 0 | |
| `metering.egress.<kênh>.anomaly_factor` | 3 (× trung vị 28 ngày) | |
| `metering.egress.<kênh>.anomaly_records` | public_api 50 000 · mrv_package 10 000 · report_export 20 000 · valve 20 000 · partner_share 50 000 | ngưỡng tuyệt đối khi < 7 ngày mẫu |
| `metering.price.<plan>.<product>.per_unit_vnd`, `metering.price.<plan>.subscription_vnd_per_month` | **không đặt** (bảng kê chỉ có đơn vị) | chờ khách; prototype `SUB-DATA` 450 000 000 đ chưa rõ kỳ |
| Lưu chứng từ sổ sử dụng / bảng kê | 10 năm, chuyển tầng | chờ pháp chế |

## 2. Số migration

Chỉ dùng số đã cấp. golang-migrate **không** áp số nhỏ hơn số đã chạy. Database dev/Compose đã chạy số cao hơn phải dựng lại. Người tích hợp merge theo thứ tự số.

| Cụm | Số | Nội dung | Schema | Vai DB |
|---|---|---|---|---|
| pf | **000047** | G5-0 (**đã có**). `CREATE SCHEMA pf_apiclient`, `pf_metering` + `USAGE` cho đúng vai, không bảng. Loại cấp quyền `dp_collection_volume`, `dp_hub_capacity`, `dp_mrv_package_status` (CHECK `share_grant_event_kinds_check`). Số dự phòng G4 (`road_routes`) không dùng: nếu sau này có OSRM thì xin số mới | `pf_apiclient`, `pf_metering`, `pf_party` | `apiclient_app`, `metering_app`, `party_app` |
| pf | 000048 | **apiclient**. Sổ `client_event` (`client:<id>`) + bảng chiếu `client_current`. Sổ `api_key_event` (`key_prefix` UNIQUE, `key_hmac` bytea, `pepper_version`, không bí mật) + bảng chiếu `api_key_current`. Sổ `subscription_event` (sản phẩm, scope, R-56 CHECK) + bảng chiếu. Sổ `quota_plan` có phiên bản (R-56 CHECK). `export_job` + ảnh chụp đầu vào bất biến (mẫu mục 12) + `export_file` (SHA-256, khóa đối tượng) | `pf_apiclient` | `apiclient_app` |
| pf | 000049 | **metering**. Sổ `usage_event` (mẫu §8.1, phân vùng tháng, `UNIQUE (tenant_id, idem_key)`, chuỗi `client:<id>:<YYYY-MM>`) + `usage_counter_current`. Sổ `egress_event` (tenant = chủ dữ liệu, `UNIQUE (tenant_id, idem_key)` với `idem_key = <kênh>:<source_id>:<counterparty_ref>`, CHECK kênh ∈ `port.Channels`) + `egress_counter_current` + `egress_anomaly` (một dòng/ngày/khóa). `statement_job`, `statement_snapshot` (bất biến, băm), sổ `statement` (R-56 CHECK, `supersedes_id`), `numeric(18,4)` / `numeric(18,2)` cho tiền | `pf_metering` | `metering_app` |
| pf | 000050 | **dự phòng tích hợp DLP** (nếu `pf/reports`, `pf/mrv`, consumer van cần cột/bảng phụ). Xin người tích hợp | | |
| pf | 000051 | dự phòng G5 (E2E / sửa sai) | | |
| mkg | 000022 | **không dùng ở G5-0**. G5 không có migration vùng mkg (van kiểm ở phía nhận, ADR-0024). Vẫn là số dự phòng G4 | | |

Migration cụm `pf` **không** tham chiếu `mkg_*`, `ts_*`, `vault` (R-07).

## 3. Vai

### 3.1 Vai DB

Đã khai ở G5-0 trong `internal/shared/dbadmin/dbadmin.go` và userlist PgBouncer (CI `.github/workflows/ci.yml` + `deploy/compose/pgbouncer/entrypoint.sh`). Mật khẩu: `AG_DB_APP_PASSWORD` hoặc `AG_DB_PASSWORD_<VAI>`. Agent module **không** sửa các tệp này.

| Vai DB | Cụm | Của | Quyền |
|---|---|---|---|
| `apiclient_app` | pf | `pf/apiclient` | `USAGE pf_apiclient` (000047); bảng do 000048 |
| `metering_app` | pf | `pf/metering` | `USAGE pf_metering` (000047); bảng do 000049 |
| `public_api` | pf | tiến trình `cmd/api-public` | chỉ bảng vận hành schema `shared` qua `ag_shared_user` (idempotency, outbox), như `platform_api`; không quyền nghiệp vụ |

Cả ba là `LOGIN`, thành viên `ag_shared_user`, chỉ `CONNECT ag_pf`. **Không** có quyền xuyên schema. `api-public` đọc module chủ dữ liệu qua `port/`, bằng pool vai của module đó: `cropcycle_app`, `geo_app`, `mrv_app`, `masterdata_app` (tham số k), `party_app` (`GrantLookup`, `RecordRead`). `metering_app` đọc gói hạn mức qua `apiclientport.Plans`, bằng pool `apiclient_app`.

### 3.2 Vai nghiệp vụ

Hằng trong `port/`, đã có trong realm Keycloak (`TestKeycloakRealmCoversRolesAndClaims`). Mọi vai đều là tài khoản cá nhân của **tenant đơn vị vận hành**, trừ `metering_viewer`. Vai `metering_viewer` có thể ở mọi tenant chủ dữ liệu và chỉ thấy nhật ký xuất của tenant mình.

| Vai | Hằng | Làm gì | R-56 |
|---|---|---|---|
| `api_client_admin` | `apiclientport.RoleAPIClientAdmin` | đăng ký khách hàng, đề xuất đăng ký sản phẩm + scope + gói hạn mức, phát hành/xoay/thu hồi khóa | đề xuất ≠ duyệt |
| `api_client_approver` | `apiclientport.RoleAPIClientApprover` | duyệt khách hàng, đăng ký sản phẩm, gói hạn mức | không duyệt việc của mình (`app/` + CHECK DB) |
| `metering_viewer` | `meteringport.RoleMeteringViewer` | xem sổ sử dụng, bảng kê, nhật ký xuất, cảnh báo | — |
| `billing_officer` | `meteringport.RoleBillingOfficer` | yêu cầu lập bảng kê (202 + job), tải bảng kê, đề xuất tham số giá | yêu cầu ≠ duyệt |
| `billing_approver` | `meteringport.RoleBillingApprover` | duyệt bảng kê và tham số giá | không duyệt bảng kê do mình yêu cầu |

Cấp quyền `dp_*` vẫn do `platform_admin` của **bên cấp** đề xuất và một `platform_admin` khác duyệt (ADR-0020). Bên thứ ba **không** có vai OIDC nào ở G5. Khi bên thứ ba gọi bằng khóa, principal là `apiclientport.Caller`, không phải `authz.Principal`.

## 4. Sự kiện G5

Sự kiện **nền tảng** (`contracts/events/<tên>.v1.json` + struct `//ag:event` trong `port/`), phát qua outbox lên exchange nền tảng.

| Sự kiện | Phát | Nhận (hàng đợi) | Nội dung chính |
|---|---|---|---|
| `pf.apiclient.export.requested` v1 | apiclient (`api-public`, schema `pf_apiclient`) | apiclient (`pf.apiclient.export`, `cmd/worker`) | `job_id`, `client_id`, `product`, `contract_version`, `data_tenant_id`, `requested_at`. Worker nạp bộ lọc từ job |
| `pf.metering.statement.requested` v1 | metering (Admin API) | metering (`pf.metering.statement`) | `job_id`, `period`, `client_id` (null = mọi khách hàng), `requested_by`, `requested_at` |
| `pf.metering.egress.anomaly_detected` v1 | metering (checkpoint) | notify (`pf.notify.egress_anomaly`) | lần **đầu** trong ngày vượt ngưỡng; kênh, sản phẩm, bên nhận, `daily_records`, `threshold`, `basis`, `mode` |

- Consumer idempotent qua `shared.inbox` (R-61; kiểm thử giao lặp). DLQ = tên + `.dlq` (R-62). Đăng ký trong `cmd/api-platform/assembly.Queues`.
- Không có sự kiện cho mỗi lời gọi Public API. Sổ `usage_event` là nguồn, không phát lên MQ, vì lưu lượng lớn và không có consumer.
- `pf.reports.export.anomaly` v1 (G1) giữ nguyên.

## 5. Vùng, cửa và đường xuất

| Chiều | G5 |
|---|---|
| Internet → `api-public` | `partner.<domain>` → Ingress → `api-public:8085` (luồng **F08**). Middleware giới hạn theo IP, giới hạn thân 1 MiB |
| `api-public` → dữ liệu | PgBouncer cụm pf 5432 (luồng **F19**). **Không** RabbitMQ (outbox ghi DB, `outbox-publisher-pf` phát), **không** `identity-vault`, **không** DB mkg/ts, **không** Internet. Valkey (tùy chọn): luồng riêng do hạ tầng thêm |
| Nền tảng → bên thứ ba | Chỉ qua Public API + hợp đồng `contracts/dataproduct` + cấp quyền `dp_*` + sổ đo lường + checkpoint |
| MKG → nền tảng | Không đổi (van). Consumer van gọi `Guard` kênh `valve` sau `ValveTargets`, mỗi bên cấp một lần |
| Đường xuất nội bộ của nền tảng | `pf/reports` (yêu cầu, lịch, tải), `pf/mrv` (yêu cầu xuất gói, `PackageURL`), `pf/party` `RecordRead` (`partner_share`, P2) đều gọi `meteringport.Guard` |

Mã luồng F08/F19 là đề xuất. Agent hạ tầng chọn mã khác nếu trùng và cập nhật `check_flows.py`, kể cả các luồng cấm của ADR-0025 §1.

## 6. Sản phẩm dữ liệu ban đầu (dự thảo, **chờ khách chốt bên thứ ba đầu tiên**)

Bộ tối thiểu đủ để bảo vệ trước khách. Chỉ gồm dữ liệu tổng hợp hoặc dữ liệu vận hành không định danh, đã có nguồn trong vùng nền tảng. Không sản phẩm nào cần dữ liệu mới từ MKG.

| Sản phẩm | Cho ai | Nguồn (`port/`) | Bảo vệ | Không bao giờ có |
|---|---|---|---|---|
| `collection_volume` v1 | nhà máy thu mua rơm/sinh khối, bên mua (đề xuất bên thứ ba đầu tiên — prototype `SUB-DATA`) | `pf/cropcycle` (bảng kê HTX qua van collection) + `pf/geo` (thửa ↔ nông hộ) + `pf/masterdata` (k) — `apiclientport.CollectionVolumeSource` | ô xã × vụ × tháng chỉ khi **≥ k nông hộ phân biệt** (`publicdata.k_min`, tạm 10). Ô dưới k bị bỏ, `meta.suppressed_cells` | thửa, nông hộ, HTX, khối lượng cân, giá, khách hàng MKG |
| `hub_capacity` v1 | nhà máy, bên mua | `pf/geo` (mô hình đọc hub từ van `hub_facility` / `hub_capacity`) — `HubCapacitySource` | bậc sử dụng (`normal`/`warning`/`full`) thay cho tấn tồn. **Cần MKG đồng ý** mục đích mới | tồn theo lô, nguồn gốc lô, giá |
| `mrv_package_status` v1 | đơn vị thẩm định (VVB) | `pf/mrv` (`export_job` + kiểm lại) — `MRVPackageSource` | chỉ xem (KN US-MRV-05). Tải gói (`export`) **chưa bật**: chờ C-1, bản cho VVB có bút danh theo VVB, DPA | ảnh, tọa độ, mã nông hộ, người thu/duyệt |

Không chọn ở G5-0:
- dự báo sản lượng (`pf/simulation`, cần mô hình được duyệt);
- tCO2e / tín chỉ (chờ C-1, phương pháp MRV);
- bản ghi MRV thô (phản mẫu prototype `sync-lakehouse`);
- lớp bản đồ cho TMS (đã có ở Application API).

## 7. Hợp đồng đóng băng ở G5-0

Đổi chữ ký hoặc ngữ nghĩa phải qua người tích hợp. Thêm phương thức → interface **mới**. Thêm trường DTO hoặc sự kiện chỉ khi tương thích ngược.

### 7.1 `internal/platform/apiclient/port`

```go
const DBRole = "apiclient_app"; const PublicDBRole = "public_api"
const RoleAPIClientAdmin, RoleAPIClientApprover
func ParseKey(presented string) (prefix string, ok bool)          // agk_<12 base32>_<43 base64url>
func ProductScope(product, action string) string                  // "dp:<product>:<read|export>"; ScopeUsageRead = "usage:read"
type Caller struct{ Client Client; KeyID uuid.UUID; KeyPrefix string; AuthMethod AuthMethod; RequestID uuid.UUID; ReceivedAt time.Time }
func WithCaller(ctx, Caller) context.Context; func CallerFrom(ctx) (Caller, bool)
type Authenticator interface{ Authenticate(ctx, presented string, receivedAt time.Time) (Caller, error) }
type TokenAuthenticator interface{ AuthenticateToken(ctx, bearer string, receivedAt time.Time) (Caller, error) }
type Plans interface{ PlanFor(ctx, operatorTenant, clientID uuid.UUID, at time.Time) (Plan, error) }
type CollectionVolumeSource interface{ CollectionCells(ctx, CollectionQuery) (CollectionPage, error) }      // pf/cropcycle hiện thực
type HubCapacitySource interface{ HubCapacities(ctx, dataTenant, provinceCode, after string, limit int) ([]HubCapacityV1, string, error) } // pf/geo
type MRVPackageSource interface{ Packages(...); Package(ctx, dataTenant, exportID) (MRVPackageStatusV1, error) } // pf/mrv
// DTO //ag:dataproduct: CollectionVolumeV1, HubCapacityV1 (LatLng json.Number), MRVPackageStatusV1
// IssuedKey{Secret pii:"true"}, Client, Plan, ClientStatus, KeyEventKind, SubscriptionStatus, ExportStatus, AuthMethod
// Header*: AG-Data-Contract, RateLimit-*, AG-Quota-Remaining, AG-Request-Id; Env*: AG_PUBLIC_*, AG_APIKEY_PEPPER_V<n>
// Lỗi: apiclient.unauthenticated|forbidden|unknown_product|rate_limited|contract_violation|self_approval|too_many_keys|export_too_large
```

### 7.2 `internal/platform/metering/port`

```go
const DBRole = "metering_app"; RoleMeteringViewer, RoleBillingOfficer, RoleBillingApprover; RuleUnitsV1 = "metering.units.v1"
type Ledger interface{ Record(ctx, operatorTenant uuid.UUID, c Call) (Recorded, error) }   // 1 tx: usage_event + bộ đếm + egress_event
type Quotas interface{ Remaining(ctx, operatorTenant, clientID uuid.UUID, product string, at time.Time) (Remaining, error) }
type Usage interface{ UsageOf(ctx, operatorTenant, clientID uuid.UUID, from, to time.Time) ([]UsageDay, error) }
func UnitsV1(c Call) int64
type Checkpoint interface{ Pass(ctx, e Egress) (Decision, error) }                       // ĐIỂM KIỂM SOÁT DUY NHẤT
func Guard(ctx, cp Checkpoint, mode Mode, e Egress, log *slog.Logger) error            // nil cp → nil; observe/enforce
func ParseMode(s string) (Mode, error)                                                 // AG_EGRESS_MODE: "" | observe | enforce
type Channel: public_api | mrv_package | report_export | valve | partner_share; CounterpartyKind: api_client | tenant | actor
// Statement, StatementLine (money.Price / money.Amount, nil khi chưa có giá), StatementStatus
// Param*: metering.egress.<kênh>.{daily_records_quota,anomaly_factor,anomaly_records}, metering.price.*
// Lỗi: metering.unavailable|quota_exceeded|egress_quota_exceeded|egress_unavailable|self_approval|statement_frozen|period_not_ended
```

### 7.3 `internal/platform/party/port` (thêm, tương thích ngược)

```go
const KindDPCollectionVolume, KindDPHubCapacity, KindDPMRVPackageStatus   // ShareKinds mở rộng (pf 000047)
func DataProductKind(product string) string                                  // "dp_" + product
type GrantLookup interface{ ActiveGrant(ctx, grantee, grantor uuid.UUID, kind string, at time.Time) (ShareGrant, error) }
```

Hiện thực `GrantLookup` bằng cùng truy vấn hai bảng như `Authorize`, nhưng không kiểm vai OIDC. Đó là việc của đợt G5-1 (P).

### 7.4 Hợp đồng tệp

| Hợp đồng | Nơi |
|---|---|
| Sản phẩm dữ liệu | `contracts/dataproduct/*.json` + gói `contracts/dataproduct` (`Get`, `All`, `Parse`, `Product.{Validate, Servable, HasScope, ContractHeader, Publication, MetaProblems}`) |
| OpenAPI Public | `contracts/openapi/public.yaml`: tag theo sản phẩm, `apiKey` (X-API-Key), `verifierClientCredentials`, problem+json, header giới hạn, 202 + Location |
| Sự kiện | ba schema ở §4 |

Kiểm thử chốt hợp đồng:
- `TestG5FrozenDataProducts` (apiclient/port): mỗi hợp đồng có hằng tên, loại `dp_*` hợp lệ, scope.
- `TestDataProductContracts_G5` (contractcheck).
- `TestEmbeddedProductsLoad_G5`, `TestValidateDefaultDeny_G5`, `TestMetaProblems_G5` (dataproduct).
- `TestUnitsV1_ADR0023`, `TestGuardModes_ADR0024`, `TestParseMode_ADR0024` (metering/port).
- `TestParseKey_ADR0022`.
- `TestShareGrantDataProductKinds_G5` (party, DB thật).
- `TestBoundary` (`cmd/api-public`).
- `TestNoFloat` (`metering`).

### 7.5 Gốc lắp ráp

Chưa nối module nào: G5-0 chưa có module. Nhiều agent cùng chạm các chỗ nối sau, mỗi agent chỉ thêm dòng của mình:

| Chỗ nối | Thêm gì |
|---|---|
| `cmd/api-platform/assembly.Build` | `pf/apiclient`, `pf/metering` (route Admin); `UseEgress(checkpoint, mode)` cho `pf/reports`, `pf/mrv`, `pf/geo`, `pf/cropcycle`, `pf/party` |
| `cmd/api-platform/assembly.Queues` / `Consumers` | ba hàng đợi §4 |
| `cmd/api-public` (mới, đợt A) | gốc lắp ráp riêng, chỉ mở pool của §3.1 |

## 8. Đợt G5-1 (song song, theo hợp đồng G5-0)

| Đợt | Nội dung | Migration |
|---|---|---|
| **A** `pf/apiclient` + `cmd/api-public` | Sổ khách hàng/khóa/đăng ký/gói hạn mức, Admin API (MFA, R-56); xác thực khóa HMAC + pepper theo phiên bản; giới hạn tần suất; handler ba sản phẩm (kiểm `GrantLookup`, `Validate`, `AG-Data-Contract`); xuất 202 + job + worker; `cmd/api-public` (thoát lỗi khi chưa bật); OpenAPI `admin.yaml` cho route quản trị + `gen:api` | pf 000048 |
| **M** `pf/metering` | `Ledger.Record` (đóng khi lỗi), bộ đếm, `Quotas`, `Usage`, `Checkpoint.Pass` + bất thường + sự kiện, bảng kê 202 + job + duyệt R-56 + CSV tất định, `RebuildTenant`, metric `ag_egress_*`, `ag_public_*` | pf 000049 |
| **S** nguồn sản phẩm | `pf/cropcycle` `CollectionVolumeSource` (k nông hộ qua `pf/geo`), `pf/geo` `HubCapacitySource`, `pf/mrv` `MRVPackageSource`, `pf/party` `GrantLookup` | — |
| **D** tích hợp DLP | `Guard` ở `pf/reports` (ba lối), `pf/mrv` (yêu cầu + `PackageURL`), ba consumer van, `pf/party` `RecordRead` (P2); consumer `pf.notify.egress_anomaly` | pf 000050 (nếu cần) |
| **H** hạ tầng Phase K | `values-phasek.yaml`, F08/F19 + luồng cấm trong `check_flows.py`, ExternalSecret pepper, middleware ingress, luật `agrigreen-egress.rules.yml`, Valkey (tùy chọn) | — |
| **W** web | `apps/web-ops`: màn khách hàng API, khóa (hiện bí mật một lần), đăng ký, gói hạn mức, bảng kê, nhật ký xuất / cảnh báo | — |
| **E** E2E | `TestG5ThirdPartyToStatement` (mục 2 của g5-acceptance), diễn tập bật Phase K bằng cấu hình | pf 000051 (nếu cần) |

## 9. Kiểm thử bắt buộc (thêm vào g1-conventions §5)

- **Mọi module G5:**
  - cách ly tenant, kể cả song song qua PgBouncer;
  - sáu phép thử UPDATE/DELETE/TRUNCATE trên mọi sổ mới, `verify_chain`;
  - idempotent POST;
  - giao lặp sự kiện (R-61);
  - sửa sai bằng bản ghi bù;
  - tự duyệt bị chặn ở `app/` **và** CHECK DB (R-56);
  - `agnofloat` (metering).
- **Public API:**
  - khóa sai, thu hồi, hết hạn → 401 và không có dòng sổ;
  - thiếu scope, thiếu cấp quyền, sản phẩm dự thảo ở production → 403 + một dòng sổ;
  - thu hồi cấp quyền → 403 ngay ở lời gọi kế tiếp;
  - ghi sổ lỗi → 503, **không** có byte dữ liệu;
  - phần tử lạ → `apiclient.contract_violation`;
  - ô dưới k không bao giờ xuất hiện (kể cả trong tệp xuất);
  - gọi lặp `Idempotency-Key` → cùng phản hồi, `units = 0`;
  - 429 kèm `Retry-After` + header `RateLimit-*`;
  - bí mật khóa không có trong log, lỗi, DB (quét chuỗi);
  - `api-public` thoát lỗi khi `AG_PUBLIC_API_ENABLED` ≠ `1`.
- **DLP:**
  - mỗi kênh một dòng `egress_event` ở tenant chủ dữ liệu;
  - giao lặp phong bì van → không thêm dòng;
  - `observe` không từ chối, kể cả khi checkpoint lỗi;
  - `enforce` từ chối đúng mã, van vào DLQ;
  - lần đầu vượt ngưỡng → đúng một sự kiện;
  - kiểm thử hiện có của reports/mrv/van **không đổi**.
- **Bảng kê:** cùng ảnh chụp → cùng băm, cùng CSV byte-cho-byte; duyệt ≠ yêu cầu; đã duyệt → 409; bảng kê bù.
- **Mã story:** ghi tiền tố, vd `ADR-0022`, `ADR-0023`, `ADR-0024`, `KN US-MRV-05`, `proto SUB-DATA`.

## 10. Câu hỏi khách và mục mở

| # | Câu hỏi | Chặn |
|---|---|---|
| Q5-1 | Bên thứ ba đầu tiên là ai (đề xuất: nhà máy sinh khối EVN/VFT)? Sản phẩm, độ chi tiết, kỳ? `SUB-DATA` 450 000 000 đ có thật không, kỳ nào? | chuyển hợp đồng dữ liệu sang `approved` |
| Q5-2 | Pháp nhân **đơn vị vận hành** Public API (ai đăng ký khách hàng, ai lập hóa đơn)? | `AG_PUBLIC_OPERATOR_TENANT`, bảng kê |
| Q5-3 | C-3 / Q-MKG-11: giá trị k. Có áp cho cả gói MRV và báo cáo không? | `publicdata.k_min` |
| Q5-4 | Mô hình tính phí: theo đơn vị, thuê bao cố định, bậc thang? Đơn giá? Hoàn đơn vị khi sự cố? | tham số giá |
| Q5-5 | Gói hạn mức và ngưỡng bất thường theo kênh. Có **chặn** van khi vượt hạn mức hay chỉ cảnh báo? Ai nhận cảnh báo DLP? | tham số DLP, `enforce` |
| Q5-6 | MKG có đồng ý mở dữ liệu hub (đã qua van cho `pf/geo`) cho bên thứ ba không? | `hub_capacity` |
| Q5-7 | Đơn vị thẩm định: là ai, có cần OAuth client credentials / mTLS không? Có nhận gói bằng chứng không (C-1, bút danh theo VVB, ảnh theo đồng ý `mrv.verification`, DPA)? | `mrv_package_status` export |
| Q5-8 | Mẫu hợp đồng dữ liệu / DPA với bên thứ ba; kênh giao bí mật khóa | ADR-0025 §2 |
| Q5-9 | Thời hạn lưu chứng từ sổ sử dụng / bảng kê (đề xuất 10 năm) | ADR-0023 §6 |
| Q5-10 | Nhà cung cấp Valkey trong lãnh thổ (giới hạn tần suất dùng chung) | ADR-0022 §7 |

Chênh tài liệu: `README.md` của bản rà soát ghi "metering, metrics (G3+)", còn kiến trúc Phụ lục A và `docs/roadmap.md` ghi metering ở G5. **Lấy G5** theo kiến trúc.

Mục rà lại khi đánh giá cấp độ (C-2): sự kiện nội bộ MKG (`mkg.internal`) và bút toán kế toán (`mkg/acctbridge`) **không** đi qua checkpoint nền tảng (ADR-0024 §3).

## 11. Quyết định đợt G5-1 D — nguồn sản phẩm, `GrantLookup`, nối DLP (01/10/2026)

Đợt D gộp phần **S** (nguồn sản phẩm, `GrantLookup`) và **D** (nối DLP) của §8. Không dùng migration (pf 000050 vẫn dự phòng). Không đổi hợp đồng đóng băng; chỉ thêm cổng mới `geoport.PlotFacts`.

### 11.1 `GrantLookup` và sổ đọc (ADR-0020, ADR-0022 §3)

| Chủ đề | Quyết định |
|---|---|
| `party.Module.GrantLookup().ActiveGrant` | Cùng truy vấn hai phía như `Authorize` (bảng chiếu bên nhận + bảng hiện hành bên cấp, `active`, `at ∈ [valid_from, valid_to)`). **Chỉ nhận loại `dp_*`** — cổng không vai OIDC không mở rộng sang loại khác (loại khác vẫn qua `Authorize` + vai `mkg_sync`). Thiếu / hết hạn / chưa hiệu lực / thu hồi / loại lạ → `party.share_grant_missing`; nhiều bên cấp + `grantor = Nil` → `party.share_grantor_ambiguous` |
| Sổ đọc của bên thứ ba | `PartnerAccess.RecordRead` nhận thêm người đọc là `apiclientport.Caller` trong context (không có `authz.Principal`): tenant bên nhận = `Caller.Client.ClientTenantID`, `actor_ref` = mã khách hàng API. Khác tenant → `party.partner_audit_unavailable`. Một dòng `partner_read_log` ở tenant bên cấp cho mỗi lần đọc (K-05) |
| Kênh `partner_share` | Chỉ cho người gọi OIDC của `/v1/partner/*`. Lần đọc của khách hàng Public API **không** qua `partner_share` — egress `public_api` đã ghi cùng sổ sử dụng (ADR-0023), không đếm hai lần |

### 11.2 Nguồn sản phẩm dữ liệu

| Sản phẩm | Module (gốc lắp ráp) | Quy tắc |
|---|---|---|
| `collection_volume` | `cropcycle.Module.CollectionVolumeSource()`; cần `UseCollectionVolume(geo.PlotFacts(), md.AdminUnits())` | Bảng kê hiện hành (`collection_statement_view`) + vụ `season_current` (mã vụ = `<season_year>-<season_code>`, vd `2026-DX`); thửa → xã, nông hộ, diện tích qua `geoport.PlotFacts` (pf/geo là chủ thửa ↔ nông hộ); xã → tỉnh qua `AdminUnits` (tại `AsOf`, không có thì tại ngày gom, đi lên qua cấp huyện lịch sử). Tháng = tháng của `collected_on` (ngày nghiệp vụ giờ VN). Số nông hộ = số `household_ref` khác nhau (thửa chưa gán nông hộ không tính vào k nhưng vẫn góp số cuộn/diện tích); diện tích = tổng diện tích **thửa phân biệt** của ô, 2 chữ số. Bị bỏ khỏi mọi ô (không tính `Suppressed`): bảng kê chưa gắn vụ / vụ không có, thửa không có ở pf/geo, xã không tra được tỉnh. `K < 2` → lỗi kiểm dữ liệu |
| `hub_capacity` | `geo.Module.HubCapacitySource()` | `pf_geo.hub_view` của tenant bên cấp. Bậc theo lớp màu widget GIS (BR-52, ngưỡng `geo.hub_capacity_warn_pct`/`_full_pct` của bên cấp): `available → normal`, `near_full → warning`, `full → full`, chưa có số → `unknown`. **Không** trả tấn đã dùng, % lấp đầy, nguồn số. `capacity_tons` = sức chứa danh định MKG công bố, chỉ khi đơn vị là tấn. Loại `hub → hub`, `kho → warehouse` (van đã gộp `yard` vào `kho`); bỏ `nha_may` và hub chưa có mã tỉnh. `retired → inactive`. Tọa độ làm tròn 6 chữ số, chuỗi thập phân (`json.Number`) |
| `mrv_package_status` | `mrv.Module.PackageSource()` | `pf_mrv.export_job` của tenant bên cấp. `catalog_version` = `domain.RuleVersion` của module (job không ghim một phiên bản danh mục; danh mục cụ thể nằm trong gói). `season_code` từ bộ lọc; `province_code` chỉ khi bộ lọc là đúng một mã tỉnh 2 chữ số. `last_verify_match`: `Package` dựng lại ảnh chụp và so băm **mỗi lần đọc** (không lưu); `Packages` → `null`. Không người yêu cầu, khóa đối tượng, URL |

Mọi nguồn đọc bằng pool vai của module chủ dữ liệu với `tenant.WithTenantTx(…, DataTenant, …)`, con trỏ là chuỗi base64url mờ do nguồn tự mã hóa, và chỉ điền trường của DTO `//ag:dataproduct` (kiểm thử `Validate` từng phần tử).

### 11.3 Ngưỡng k: quy tắc `publicdata.kanon.v1` (`cropcycle/domain.Suppress`)

1. Ô = (tỉnh × xã × vụ × tháng). **Ẩn sơ cấp:** ô có < k nông hộ phân biệt.
2. **Ẩn bổ sung (complementary):** tổng theo hai chiều được coi là có thể biết từ nguồn khác (báo cáo, số liệu công bố của bên cấp):
   - hàng (tỉnh × vụ × tháng) — mọi xã của một tỉnh trong tháng;
   - cột (xã × vụ) — mọi tháng của một xã trong vụ.

   Nhóm nào có **đúng một** ô bị ẩn và ít nhất một ô công bố thì ẩn thêm ô công bố nhỏ nhất của nhóm (ít nông hộ nhất, rồi ít cuộn nhất, rồi theo khóa). Lặp tới điểm bất động, duyệt nhóm theo khóa đã sắp nên kết quả tất định.
3. Ẩn tính trên **toàn bộ** dữ liệu của bên cấp rồi mới áp bộ lọc của lời gọi (vụ, tỉnh, khoảng tháng). Đổi bộ lọc không làm lộ ô bị ẩn.
4. `Suppressed` = số ô bị ẩn (sơ cấp + bổ sung) khớp bộ lọc, trên cả bộ lọc, không chỉ trang. Ô bị ẩn không bao giờ có trong phản hồi, kể cả tệp xuất (worker xuất của đợt A gọi cùng nguồn).

Rà lại khi khách chốt C-3 (Q5-3): giá trị k, và có áp cho gói MRV / báo cáo không.

### 11.4 Nối DLP (ADR-0024 §3)

Module nhận `UseEgress(cp, mode[, log])`; `cp == nil` → hành vi cũ, kiểm thử hiện có không đổi. Gốc lắp ráp: `assembly.External.Egress` (nil tới khi nối `pf/metering` của đợt M) + `AG_EGRESS_MODE` qua `assembly.EgressModeFromEnv` (giá trị lạ → lỗi khởi động). `cmd/worker`: biến `workerEgress` (nil) cho job lịch gửi báo cáo và `platformLazy`.

| Lối | `Egress` | Khi `enforce` từ chối |
|---|---|---|
| `pf/reports` yêu cầu / lịch / tải | kênh `report_export`; tenant = tenant người xuất; bên nhận `actor` = người xuất / người lập lịch; `product` = mã mẫu; `records` = 1; nguồn = id dòng `export_audit`. Gọi **sau** hạn mức người/ngày cục bộ và **trước** khi ghi `export_audit` | 429 / 503, không có dòng `export_audit`; lối lịch bỏ mốc (`skipped`), không lỗi job |
| `pf/mrv` yêu cầu xuất | kênh `mrv_package`; nguồn = `export_id`; `records` = số bằng chứng ước (ảnh chụp tại lúc yêu cầu — chỉ dựng khi đã nối checkpoint); bên nhận `actor` | 429 / 503, không tạo job |
| `pf/mrv` cấp URL tải gói | kênh `mrv_package`; nguồn **mới** mỗi lần cấp; `records` = số bằng chứng của gói, `bytes` = `size_bytes` của `export_package` | 429 / 503, không cấp URL |
| Ba consumer van | kênh `valve`, sau `ValveTargets`, **mỗi bên cấp một lần**, trước khi ghi; tenant dữ liệu = tenant phong bì (MKG), bên nhận `tenant` = bên cấp; nguồn = `event_id`; `product` = tên hợp đồng van; `records` = 1, `bytes` = cỡ payload | lỗi consumer → giao lại → DLQ, không ghi |
| `/v1/partner/*` (`RecordRead`) | kênh `partner_share`; tenant dữ liệu = bên cấp, bên nhận `tenant` = tenant người gọi; `product` = loại cấp quyền; `records` = số dòng trả; nguồn = id dòng `partner_read_log` | 429 / 503, không ghi sổ đọc, không trả dữ liệu |

## 12. Quyết định đợt tích hợp G5 (02/10/2026, nhánh `g5-int`)

| Chủ đề | Quyết định |
|---|---|
| `cmd/api-public` `integrationPorts` | Nối cổng **thật**, mỗi cổng một pool vai của module chủ (§3.1): `pf/metering` (`metering_app`: `Ledger`, `Quotas`, `Usage`; gói hạn mức qua `apiclient.Plans()`), `GrantLookup` (`party_app`), `CollectionVolumeSource` (`cropcycle_app` + `geo.PlotFacts` + `masterdata.AdminUnits`), `HubCapacitySource` (`geo_app`), `MRVPackageSource` (`mrv_app`), tham số (`masterdata_app`). Đúng 8 vai, kiểm ở `TestPublicAPIRealWiring_G5Integration`. `/readyz` ping thêm pool metering + party. `AG_EGRESS_MODE` đọc ở `api-public` (giá trị lạ → không khởi động) |
| `GrantLookup` không kéo vault | Gốc module `pf/party` và `party/app` import client kho định danh + handler. Gốc lắp ráp chỉ import gốc module hoặc `port/` (R-02), nên bộ điều hợp chỉ đọc nằm ở **`party/port/grantlookup`** (`grantlookup.New(pool, clk)`: `ActiveGrant` + `ActiveGrantors` cho danh mục). Truy vấn hai phía dùng chung `party/store.VerifiedActive`; `app.Sharing.ActiveGrant` gọi lại gói này (một ngữ nghĩa). Không nới archlint; `TestNoVaultTransitiveDeps_RB3` cấm vault / mkg / `party`, `party/app`, `party/handler` trong `go list -deps` của `cmd/api-public` |
| Kho tệp xuất | `api-public` đọc tệp xuất từ kho đối tượng để phát luồng: luồng **F29** `api-public` → `external.objectStorage` 443 (mọi hồ sơ, như F08/F19), `AG_S3_*` ở values gốc, khóa **chỉ đọc** riêng `pf-public-s3` (`<remotePrefix>/s3/pf-public`, bật ở `values-phasek.yaml`). `check_flows.py`: F29 phải mở, chỉ 443; khóa S3 chỉ từ `pf-public-s3` (không `pf-s3` ghi) |
| Consumer `pf.notify.egress_anomaly` | pf/notify, chủ đề toàn tenant **chủ dữ liệu** (tenant phong bì) theo vai `notifyport.EgressAnomalyRoles` = `metering_viewer`, `platform_admin` (**tạm, chờ khách Q5-5**). Loại thông báo `egress_anomaly` — migration **pf 000050** (dùng số dự phòng tích hợp DLP). Hàng đợi tự có trong `assembly.Queues` vì liệt kê từ `notify.Consumers()` |
| Job `apiclient_key_expiry` | `apiclientport.JobKeyExpiry`, `cmd/worker` (leader, mỗi giờ, chạy khi khởi động) chỉ khi đặt `AG_PUBLIC_OPERATOR_TENANT`. `Service.ExpireAllDue` ghi sự kiện `expired` (RB-1) cho khóa quá hạn và khóa `rotated` quá hạn chồng lấn của mọi khách hàng, idempotent |
| Valkey | **Không làm** adapter: máy dev không có Valkey để kiểm thử tích hợp, nhà cung cấp chờ Q5-10. Ứng viên client `valkey-go` (Apache-2.0) hoặc `go-redis` (BSD-2-Clause), đều trong allowlist. `AG_VALKEY_URL` vẫn chỉ log cảnh báo |
| Runbook | [public-api-onboarding.md](../runbooks/public-api-onboarding.md) (ADR-0025 §2–3) |

Mục mở sau tích hợp: `partner_read_log` cho lần đọc của khách hàng Public API (K-05) chưa được `api-public` gọi (`PartnerAccess.RecordRead` cần gốc `pf/party`); kiểm thử đặt `publicdata.k_min` khác mặc định qua `api-public`; `Deprecation`/`Sunset` (P-12); OAuth client credentials (P-19).
