# Triển khai các ADR G1: 0006, 0007, 0009, 0010, 0011

Lập ngày 30/09/2026, khi chủ sản phẩm chấp nhận năm ADR trong phiên làm việc. Mỗi cam kết của ADR được đối chiếu với mã, cấu hình, kiểm thử và tài liệu trong repo.

Cột **Trạng thái**:
- **đã có**: có từ trước đợt này, đã có kiểm thử;
- **đợt này**: mã và kiểm thử thêm trong đợt này (tên kiểm thử mang mã ADR);
- **chờ khách**: cần khách hoặc nhà cung cấp quyết định. Đã có sẵn chỗ cắm và bản giả;
- **người tích hợp**: việc nằm trong tệp mà agent không được sửa (`deploy/**`, CI).

## Tóm tắt

| ADR | Cam kết | Đã có | Đợt này | Chờ khách | Người tích hợp |
|---|---|---|---|---|---|
| 0006 Engine đồng bộ ngoại tuyến | 12 | 9 | 3 | 1 | 1 (luật cảnh báo) |
| 0007 Nhà cung cấp trong lãnh thổ | 11 | 5 | 4 | 2 | — |
| 0009 Mô hình tenant | 7 | 6 | 0 (sửa lời văn; 1 cam kết thuộc ADR-0020) | — | — |
| 0010 Kênh nông hộ | 7 | 4 | 2 | 1 | 1 (phần mở rộng Keycloak) |
| 0011 Module nghiệp vụ các app | 8 | 6 | 1 | 1 (FN-21) | — |

Một cam kết có thể vừa thuộc cột "đợt này" vừa thuộc cột "chờ khách". Ví dụ: proxy thời tiết đã có, nhưng nhà cung cấp chưa chọn.

## ADR-0006: Engine đồng bộ ngoại tuyến

**Quyết định khi chấp nhận:** engine là **Application API**, gồm pull/push có cursor ký HMAC và Idempotency-Key ([sync-contract.md](../dev/sync-contract.md)). ĐTK 1 và G2 **không** dùng logical replication. Vì vậy câu hỏi 23.1 (slot `failover = true`) và sáu điều kiện ở mục 15 chỉ cần trả lời khi nào có đề xuất đổi engine. Việc đổi engine phải qua một ADR mới.

| # | Cam kết | Nơi hiện thực | Kiểm thử | Trạng thái |
|---|---|---|---|---|
| 6.1 | Chiều lên luôn qua Application API có Idempotency-Key, không ghi thẳng DB | `internal/platform/sync/app/push.go`; `idempotency.Middleware` cho mọi POST (`cmd/api-platform/assembly/router.go`) | `TestPushMixedBatchAndReplay` (gọi lặp → `Idempotent-Replayed`) | đã có |
| 6.2 | Chiều lên chỉ ghi thêm, UUIDv7 + `idem_key` sinh trên máy, `create` / `correct` (không update/delete) | `sync/domain/record.go`, sổ `pf_sync.inbound` | `TestParseRecord`, `TestPushMixedBatchAndReplay`; Flutter `outbox_test.dart` | đã có |
| 6.3 | Chiều xuống theo phạm vi phân công; phạm vi đổi → `reset` | `sync/app/pull.go`, `party/port.ScopeResolver` | `TestPullScopeChangeResets`, `TestPullEmptyScope`, `TestFarmerPullByLinkedHouseholds` | đã có |
| 6.4 | Cursor mờ ký HMAC, gắn tenant + thiết bị + phạm vi | `sync/domain/cursor.go` | `TestCursorRoundTripAndTamper`, `TestPullCursorTamperRejected` | đã có |
| 6.5 | Khóa HMAC cursor bắt buộc ở production. Khóa ngẫu nhiên theo tiến trình làm hỏng cursor khi khởi động lại | `sync.CheckCursorKey`, gọi ở `assembly.Build` | `TestCursorKeyRequiredInProduction_ADR0006` | **đợt này** |
| 6.6 | Thu hồi thiết bị → `403 sync.device_revoked`, máy xóa dữ liệu cục bộ | Admin `POST /v1/devices/{id}/revoke`; `packages/agrigreen_mobile/lib/data/auth/wipe_service.dart` | `TestDeviceRegisterRevoke`; Flutter `wipe_test.dart` | đã có |
| 6.7 | Cửa sổ ngoại tuyến 24–48 giờ (mục 15). Đăng nhập ngoại tuyến tối đa 72 giờ, có PIN | `packages/agrigreen_mobile/lib/data/auth/session_service.dart` | Flutter `auth_test.dart` (nhóm "phiên ngoại tuyến tối đa 72 giờ") | đã có |
| 6.8 | Lưu giữ: máy xóa bản ghi đã xác nhận sau 48 giờ; máy chủ giữ Idempotency-Key 48 giờ rồi dọn | `AppConfig.ackRetention`, job `idempotency` (`opsjobs.PurgeIdempotency`) | Flutter `sync_push_test.dart` ("giữ bản ghi đã xác nhận 48 giờ…"); `TestPurgeIdempotency` | đã có |
| 6.9 | Xung đột: giữ cả hai bản, có người phụ trách, SLA C-8 (tạm 72 giờ, tham số `sync.conflict_sla_hours`) | `sync/app/conflicts.go`, sổ `pf_sync.conflict`, Admin `/v1/sync/conflicts` | `TestConflictSLA`, `TestConflictResolutionUI` | đã có. **Chờ khách:** C-8 chốt số giờ; đổi bằng tham số, không đổi mã |
| 6.10 | Gauge `ag.sync.conflict.overdue` (mục 19) phải được đo thật. Trước đợt này hàm có nhưng chưa nơi nào đăng ký | `sync.RegisterConflictSLAMetrics`, `sync.ConflictTenants`; `cmd/worker/gauges.go`; migration **pf 000043** (quyền đọc hai cột cho `ag_outbox_publisher`) | `TestConflictTenantsForSLAGauge_ADR0006` | **đợt này** |
| 6.11 | Chỉ số `standby_replication_lag_seconds` có cảnh báo | `db.StandbyLag`, `db.RegisterStandbyLag` (metric `ag.db.standby_replication_lag`, đơn vị giây → Prometheus `ag_db_standby_replication_lag_seconds`); worker đăng ký khi có `AG_DB_STANDBY_URL` | `TestStandbyLagOnPrimary_ADR0006` | **đợt này**. **Người tích hợp:** thêm luật Prometheus vào `deploy/observability/prometheus/` theo ngưỡng ở [runbooks/README.md](../runbooks/README.md) (standby lag > 60 giây trong 5 phút; `ag_sync_conflict_overdue` > 0 trong 1 giờ) |
| 6.12 | Tên engine, câu trả lời 23.1 | ADR này ghi engine = Application API | — | đã có (quyết định). 23.1 chỉ mở lại nếu đề xuất logical replication |

## ADR-0007: Danh mục nhà cung cấp trong lãnh thổ (RB-7)

| # | Cam kết | Nơi hiện thực | Kiểm thử | Trạng thái |
|---|---|---|---|---|
| 7.1 | Dữ liệu cá nhân, sao lưu, ảnh, nhật ký chứng cứ và nhật ký có định danh chỉ đi tới đích trong lãnh thổ. Ở production, guard kiểm lúc khởi động | `internal/shared/residency`; gọi đầu `run()` của mọi dịch vụ | `TestEachEndpointOutsideTerritoryFails`, `TestProductionRequiresAllowlist`, `TestServiceMainsCallGuard` | đã có |
| 7.2 | Kho đối tượng, sao lưu, KMS, OSRM trong lãnh thổ | `objstore`, [backup.md](../runbooks/backup.md), SeaweedFS ở dev (X11) | `TestEachEndpointOutsideTerritoryFails` (`AG_S3_ENDPOINT`, `AG_BACKUP_S3_ENDPOINT`) | đã có cho cấu hình. Chọn nhà cung cấp production là việc hồ sơ triển khai |
| 7.3 | Proxy **không gửi định danh** cho thời tiết, độ mặn, tile | Module mới `internal/platform/extdata`: yêu cầu đi ra dựng mới, chỉ có `User-Agent` cố định và `Accept`; context đã gỡ Principal, token và tenant (`app.Detached`); không theo chuyển hướng | `TestProviderSeesOnlyCoarseCellNoIdentity_ADR0007`, `TestHTTPSendsNoIdentifiers_ADR0007`, `TestHTTPRejectsRedirectAndBadBodies_ADR0007`, `TestDetachedDropsValuesKeepsCancel` | **đợt này** |
| 7.4 | Tọa độ làm thô theo lưới | `extdata/domain.Snap`: ô mặc định 0,1°, tối thiểu 0,05°; tile có trần zoom (mặc định 16) và phải giao vùng phục vụ | `TestSnapToCoarseGrid_ADR0007`, `TestSnapRejectsFineGridAndOutsideTerritory_ADR0007`, `TestCheckTileZoomAndTerritory_ADR0007` | **đợt này** |
| 7.5 | Danh mục nguồn theo từng nguồn, mặc định chặn; bộ nhớ đệm | `AG_EXTDATA_SOURCES` + `AG_EXTDATA_<ID>_*` (`extdata/app.LoadConfig`); LRU theo ô/tile, dùng chung giữa người gọi (`app.Cache`) | `TestLoadConfigDefaultsAndValidation_ADR0007`, `TestSourceAllowlistAndTerritory_ADR0007`, `TestCacheTTLAndProviderFailure_ADR0007`, `TestTileProxyCached_ADR0007`, `TestExtDataHTTP_ADR0007` | **đợt này** |
| 7.6 | "Cần ADR riêng cho từng nguồn nước ngoài". Guard RB-7 bao cả nguồn ngoài | Production: nguồn ngoài lãnh thổ cần thuộc `AG_NONPII_ALLOWLIST` **và** có `AG_EXTDATA_<ID>_ADR`; chỉ https; cấm nhà cung cấp giả. `residency` nhận tiền tố `AG_EXTDATA_` cho `AG_NONPII_ALLOWLIST` | `TestLoadConfigProductionResidency_ADR0007`, `TestExtDataForeignSourceOnlyViaNonPIIList_ADR0007` | **đợt này** |
| 7.7 | Yêu cầu API thời tiết nước ngoài trong BRD (OpenWeather, Open-Meteo; HTX US-WEATHER-01, GIS US-WX-01/02, US-NAT-01) phải đi qua proxy | API `GET /v1/extdata/{source}/point` (application.yaml). Client chưa nối, thẻ thời tiết App HTX vẫn hiện "chờ nguồn" | — | **chờ khách:** chọn nhà cung cấp (Q2-10). Khi đã chọn, chỉ cần cấu hình mẫu URL và nối client |
| 7.8 | Tile vệ tinh (HTX US-PLOT-03, GIS FN-07) | API `GET /v1/extdata/{source}/tiles/{z}/{x}/{y}`. App vẫn dùng `AG_TILE_URL` trỏ máy chủ tile trong lãnh thổ | `TestTileProxyCached_ADR0007` | **chờ khách:** chọn nhà cung cấp tile vệ tinh. OSM `tile.openstreetmap.org` chỉ dùng cho dev |
| 7.9 | Đẩy thông báo chỉ dữ liệu, không định danh (FCM) | ADR-0015, `pf/notify` | `TestPushDataOnlyAndPerDeviceFailures`, `TestNonPIIAllowlistOnlyForPushChannels` | đã có |
| 7.10 | Optimizer chỉ nhận dữ liệu không định danh | `pf/simulation` ảnh chụp | `TestSnapshotIsDeidentifiedAndCarriesRoutingResults`; pytest `test_rb7_identifying_fields_are_rejected` | đã có |
| 7.11 | Web không gọi tile hay API bên ngoài | `apps/web-ops` `MapView`/`LayerMap` (nền trơn hoặc `NEXT_PUBLIC_MAP_STYLE_URL`) | WEB-E2E `gis-map.spec.ts` | đã có |

## ADR-0009: Mô hình tenant theo tổ chức sở hữu dữ liệu

Lời văn đã chỉnh cho khớp mã: `tenant_id` lấy từ claim của token, không lấy từ path, query hay body (R-16). Phần liên thông khác tenant (tài khoản dịch vụ MKG đọc thửa và HTX thuộc tenant chương trình, RB-2) — xem ADR-0020. Đợt này **không** có mã cấp quyền.

| # | Cam kết | Nơi hiện thực | Kiểm thử | Trạng thái |
|---|---|---|---|---|
| 9.1 | `tenant_id` là tổ chức sở hữu dữ liệu. RLS chỉ cách ly tenant (R-33), mỗi bảng một policy | `shared.apply_tenant_rls`, `tenant.WithTenantTx`; ADR-0005 | `TestTenantLeakParallel`, `TestCannotWriteForeignTenant`, `TestNoTenantContextSeesNothing`; `TestTenantIsolation` ở từng module | đã có |
| 9.2 | ĐTK 1 chỉ có một tenant (chương trình khuyến nông quốc gia) | Tài khoản do pf/accounts tạo mang `tenant_id` của người tạo (`accounts/app/users.go`, `farmer.go`), nên một triển khai ĐTK 1 chỉ sinh một tenant. Realm Keycloak giữ thuộc tính `tenant_id`, chỉ admin sửa được | `TestIdempotentCreate` (tìm người dùng theo `tenant_id`), `TestTenantIsolation` (accounts) | đã có |
| 9.3 | Phạm vi theo cây TW → Tỉnh → Xã và theo HTX là phân quyền tầng ứng dụng, gắn vào `authz.Principal` (`org_ref`) | `party/port.ScopeResolver`, `authz.Scope` | `TestScopeProvinceSeesOnlyItsCommunes`, `TestCommuneOfficerCannotCreateOutsideCommune`, `TestNoOrgSeesNothing`, `TestDescendantsCoverWholeProvince`; kiểm thử phạm vi của extension, geo, reports, mrv, metrics | đã có |
| 9.4 | Dữ liệu dùng chung (thửa, HTX) thuộc tenant sở hữu; ứng dụng khác đọc qua `port/` hoặc API, không chồng policy | archlint `agboundary` (R-02); `migratecheck` (không FK hay join xuyên schema) | `TestBoundary`, `TestRepositoryMigrationsPass` | đã có |
| 9.5 | Ranh giới thửa do cán bộ KN vẽ **trước** khi gán HTX | `geo/app/plots.go` `AssignInput`, `POST /v1/plots/{id}/assign` | `TestOverlapRules` (gán HTX sau khi vẽ) | đã có |
| 9.6 | Mã "duy nhất toàn hệ thống" (MAY-, CSH-) là nhãn hiển thị, không mang ý nghĩa địa lý bất biến | `mechanization/app/machines.go` | `TestCodesAssignedOnceAndStableOnProvinceMove` | đã có |
| 9.7 | Liên thông khác tenant | xem ADR-0020 (agent khác) | — | ngoài phạm vi đợt này |

## ADR-0010: Kênh nông hộ

| # | Cam kết | Nơi hiện thực | Kiểm thử | Trạng thái |
|---|---|---|---|---|
| 10.1 | Nông dân và cán bộ hiện trường dùng app Flutter cho luồng ngoại tuyến | `apps/field-app`, `apps/htx-app`, `packages/agrigreen_mobile` | Flutter: `auth_test.dart`, `outbox_test.dart`, `sync_push_test.dart`; App HTX `login_flow_test.dart` | đã có |
| 10.2 | Zalo Mini App chỉ dùng online. Ranh giới trong mã: chỉ thiết bị `android` hoặc `ios` mới đăng ký đồng bộ ngoại tuyến được, nên kênh khác không pull/push/tải tệp được | `sync/domain/device.go` (`platforms`) | `TestOnlyFlutterPlatformsRegisterForOfflineSync_ADR0010` | **đợt này** (kiểm thử khóa ranh giới). Bản thân Zalo Mini App để sau (g2-conventions §1) |
| 10.3 | Không gửi mật khẩu tạm qua email (GIS, CGH, HTX) | `accounts/app/users.go` (required actions + `execute-actions-email`) | `TestCreateUserUSADM01` (liên kết một lần, không mật khẩu tạm) | đã có |
| 10.4 | Kích hoạt tài khoản bằng liên kết một lần của Keycloak. Nông dân dùng QR hoặc liên kết do quản lý HTX phát | `accounts/app/farmer.go`, `port.ActionLinks`, `AG_KC_ACTION_LINK_URL` | `TestFarmerAccountCreateScopeHTXUSACC01`, `TestActivationReissueRevokesPrevious`, `TestActivationTokenNeverLoggedOrStored` | đã có. **Người tích hợp:** phần mở rộng Keycloak phát liên kết hành động (`AG_KC_ACTION_LINK_URL`) ở môi trường thật |
| 10.5 | MFA cho vai quản trị: backend chặn token không có yếu tố thứ hai ở Admin API; tạo tài khoản vai [MFA] kèm `CONFIGURE_TOTP` | `httpx.RequireMFA`, `accounts/port.MFARoles` | `TestAdminRequiresMFA` | đã có |
| 10.6 | Danh sách vai MFA của backend khớp đúng các vai đánh dấu `[MFA]` trong realm; vai hiện trường (`farmer`, `coop_manager`, `kn_officer`) không bị đòi MFA | `cmd/api-platform/adr_g1_test.go` | `TestMFARolesMatchRealm_ADR0010` | **đợt này** |
| 10.7 | Thời tiết trên kênh online (Zalo, App HTX) | proxy ADR-0007 | — | **chờ khách** (7.7) |

## ADR-0011: Module nghiệp vụ cho các app

| # | Cam kết | Nơi hiện thực | Kiểm thử | Trạng thái |
|---|---|---|---|---|
| 11.1 | `extension`: nhiệm vụ hỗ trợ + SLA, chỉ đạo khẩn, lớp tập huấn, danh bạ trực (App KN) | `internal/kn/extension` (pf 000006, 000016, 000026) | kiểm thử `extension/handler` (KN US-TASK-*, US-DIR-*) | đã có |
| 11.2 | `mechanization`: máy, chủ sở hữu, định mức có hiệu lực, cân đối cung–cầu (CGH QT-01/02/03) | `internal/platform/mechanization` (pf 000007, 000015, 000035) | `TestBalanceUsesNormAtSeasonAndPilotStages`, `TestSavedResultsImmutableAndChainVerifies` | đã có |
| 11.3 | `cropcycle`: mùa vụ theo SOP, nhật ký canh tác dạng sổ, sản lượng decimal. Sửa nhật ký là bản ghi bù | `internal/platform/cropcycle` (pf 000020, 000021, 000040) | `TestFarmLogCorrection_HTX_US_LOG_01_AC3`, `TestSeasonCorrectionAndCancel`, `TestHarvestDeclare_HTX_US_YIELD_01_03` | đã có |
| 11.4 | `content`: thư viện quy trình, tin tức, CMS (KN + HTX) | `internal/platform/content` (pf 000005, 000014) | kiểm thử `content/handler` | đã có |
| 11.5 | HTX US-LOG-02/05, US-YIELD-02: đổi "xóa" hoặc "ghi đè" thành phiên bản mới hoặc bản ghi bù | cropcycle `op: correct` + `supersedes_id` | `TestFarmLogRecord_HTX_US_LOG_01_02`, `TestFarmLogReview_HTX_US_LOG_05`, `TestHarvestImport_HTX_US_YIELD_02` | đã có |
| 11.6 | CGH US-CFG-01 AC-4 và GIS US-CFG-01: cấu hình có phiên bản, khôi phục mặc định là phiên bản nháp mới | `masterdata` tham số có phiên bản, `masterdata/app/display.go` | `TestParamCorrectionSupersedes`, `TestCatalogDeactivateKeepsHistory`, `TestDisplayPaletteHexValidation_GIS_US_CFG_01` | đã có |
| 11.7 | Không xóa cứng: cổng máy chặn câu SQL `DELETE FROM` / `TRUNCATE` trong `internal/platform/**` và `internal/mkg/**`. Chỉ hàm `Rebuild*`/`Clear*` được xóa, và chỉ để dựng lại bảng chiếu từ sổ (R-22). Migration đã cấm từ trước (R-25) | `tools/archlint/noharddelete.go` (analyzer `agnoharddelete`) | `TestNoHardDelete_ADR0011`; `go run ./tools/archlint ./...` sạch trên repo | **đợt này** |
| 11.8 | GIS FN-21: "dọn" dữ liệu cũ | nhật ký lớp và ảnh chụp chỉ ghi thêm (`pf/metrics`); "dọn" chỉ được là chuyển tầng | `TestGISLayerLogSnapshotReplay` | **chờ khách:** số ngày lưu (g3-acceptance, GIS US-RETAIN-01) |

## Migration và thay đổi dùng chung

- **pf 000043** `pf_sync_conflict_sla_gauge`: `GRANT SELECT (tenant_id, status) ON pf_sync.conflict TO ag_outbox_publisher`. Không tạo bảng. **pf 000044**: không dùng, để trống.
- `internal/shared/residency`: thêm tiền tố `AG_EXTDATA_` vào danh sách được dùng `AG_NONPII_ALLOWLIST`. Thay đổi tương thích ngược.
- `internal/shared/db/standby.go`: thêm `StandbyLag`, `RegisterStandbyLag`, `EnvStandbyURL`. `internal/shared/telemetry`: thêm hằng `MetricStandbyLag`.
- `tools/archlint`: thêm analyzer `agnoharddelete`.
- Gốc lắp ráp: nối `pf/extdata` và gọi `sync.CheckCursorKey` (`cmd/api-platform/assembly/modules.go`). Worker đăng ký hai gauge (`cmd/worker/gauges.go`).
- Hợp đồng: `contracts/openapi/application.yaml` có thêm `/v1/extdata/*` và schema `ExtDataSource`. `apps/web-ops` đã sinh lại `src/api/generated/application.ts`.
