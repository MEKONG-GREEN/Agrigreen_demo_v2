# Nghiệm thu cổng G2: yêu cầu → kiểm thử

Lập ngày 29/09/2026 trên `main` tại `d86f958`. Mỗi yêu cầu có đúng một dòng. Tên kiểm thử trong bảng đều đã tra trong mã. Khi lập bảng, kiểm thử Go được chạy lại trên máy dev (PostgreSQL 17 + PostGIS + pgRouting): 44 gói gồm mọi module pf/mkg có kiểm thử, `cmd/api-platform`, `cmd/worker`, `contracts`, `tools`, `tests/acceptance`, `tests/e2e`, và **tất cả đạt**. Kiểm thử Flutter, Vitest, Playwright và pytest **không chạy lại trong lần rà này**; số ca ở mục 1 đếm trong mã nguồn. Cập nhật 30/09/2026 (App HTX: bản đồ và vẽ ranh giới lô, tin theo chuyên mục, gọi nhanh nông hộ): `flutter test` của lõi, App HTX và app hiện trường được chạy lại, **tất cả đạt**; số ca Flutter ở mục 1 là số của lần chạy này. Cập nhật 30/09/2026 (hoàn thiện App HTX trên backend và web: nhập/sao chép hồ sơ HTX, tìm/gọi/sao chép nông hộ, bản đồ lô ruộng, lịch báo cáo của Quản lý HTX, nhóm dữ liệu "Đồng bộ ngay", xem trước mobile): chạy lại toàn bộ `go test ./...`, Vitest (176 ca) và Playwright (94 ca), **tất cả đạt**.

Yêu cầu lấy từ:
- [docs/roadmap.md](../roadmap.md), dòng G2, và [g2-conventions.md](../dev/g2-conventions.md);
- **Backlog App HTX v4.0**: đủ 48 story, mã có tiền tố `HTX`;
- **Backlog GIS v1.1**: 38 story, mã có tiền tố `GIS`, kèm số FN của BRD GIS v1.3 (FN-04 ranh giới HTX/Hub, FN-06 kho, FN-08 đường bộ, FN-09 thủy hệ, FN-13 bản đồ nhiệt giai đoạn, FN-14 cảnh báo chín rộ, FN-15 widget Hub…);
- kiến trúc v3.0: Phụ lục A dòng G2 (`mkg/*`, `pf/waterway`, `pf/simulation`), mục 12 và 16, RB-1..RB-7, các quy tắc R-xx mà G2 chạm tới;
- các dòng **"ngoài G1 → G2"** của [g1-acceptance.md](g1-acceptance.md);
- giả định và quyết định: [g2-conventions.md](../dev/g2-conventions.md), [docs/design/mkg.md](../design/mkg.md), [ADR-0016](../adr/0016-giay-phep-python.md).

Ba trạng thái:
- **đạt**: có kiểm thử tự động chứng minh tiêu chí chấp nhận. Cột kiểm thử liệt kê kiểm thử Go (`Test…`), **WEB-E2E** `*.spec.ts` (Playwright), Vitest, kiểm thử Flutter của App HTX (`apps/htx-app/test`, `integration_test/`), của lõi di động (`packages/agrigreen_mobile/test`) và của app hiện trường, cùng pytest của `optimizer/`;
- **một phần**: ghi rõ AC còn thiếu. Story **chưa có mã** cũng xếp vào đây và ghi "chưa làm";
- **ngoài G2 → G3/…**: chỉ dùng cho hạng mục mà kiến trúc hoặc lộ trình đặt ở cổng sau, hoặc hạng mục cần nhà cung cấp chưa được chọn (RB-7).

## Tóm tắt

| Mục | Số yêu cầu | đạt | một phần | ngoài G2 → G3/… |
|---|---|---|---|---|
| 2. Lộ trình G2 | 11 | 11 | 0 | 0 |
| 3. Chuyển từ G1 | 10 | 5 | 3 | 2 |
| 4. App HTX (Backlog v4.0) | 48 | 43 | 5 | 0 |
| 5. GIS (Backlog v1.1) | 38 | 23 | 9 | 6 |
| 6. Cổng kiến trúc G2 | 30 | 25 | 3 | 2 |
| **Tổng** | **137** | **107** | **20** | **10** |

**Ghi chú môi trường (không tính theo dòng):** CI trên GitHub **chưa được xác nhận** cho các commit G2, vì GitHub Actions đang bị chặn do lỗi thanh toán. Các job mới (`optimizer`, `agrigreen-mobile`, `htx-app`, kiểm thử App HTX trên emulator trong `field-app-emulator`) và `compose-smoke` có api-internal, optimizer, mTLS chưa chạy trên runner. Trạng thái trong bảng dựa trên lần chạy kiểm thử tại máy dev.

**20 dòng "một phần"**: phần còn thiếu và loại của từng dòng.

| Yêu cầu | Còn thiếu | Loại |
|---|---|---|
| CGH FN-07 Tích hợp App HTX | Diện tích theo vụ đã đồng bộ tự động (QT-03). Còn thiếu: **tình trạng máy và lịch mùa vụ từ App HTX** (App HTX không thu dữ liệu này, cần khách chốt nguồn); nhật ký đồng bộ và banner cảnh báo Admin (BR-04, FN-12); fallback theo ngưỡng độ tươi (AC-03) | chờ khách + mã |
| GIS FN-06 Kho/Hub | Danh sách, sức chứa và nhập tay giữ số (BR-52) đã đạt. **Chưa có job chụp và công bố sức chứa định kỳ** từ `mkg/hub` (Q-MKG-02). Hiện chỉ công bố khi duyệt và khi chụp tay | mã + chờ khách |
| GIS FN-07 Dữ liệu nền | Đường bộ và thủy hệ đã đạt. **Nhà cung cấp tile vệ tinh trong lãnh thổ chưa chọn** (RB-7); lớp tự nhiên chuyển sau (xem GIS US-NAT-01) | chờ khách |
| HTX US-ACC-01 Tạo tài khoản | Tài khoản nông dân (tên đăng nhập là SĐT) và QR/liên kết một lần đã đạt với Keycloak giả. **Keycloak thật cần phần mở rộng realm phát liên kết hành động** (`AG_KC_ACTION_LINK_URL`, gắn token với lượt và thu hồi lượt cũ). Phần mở rộng này chưa có trong repo hay Compose | hạ tầng |
| HTX US-ACC-02 Nhập tài khoản hàng loạt | Như US-ACC-01: liên kết kích hoạt cho từng dòng cần phần mở rộng Keycloak | hạ tầng |
| HTX US-HH-01 Hồ sơ nông hộ | Tạo, chống trùng, phạm vi và **tìm theo SĐT** (chỉ mục mù của kho định danh, `GET /v1/cooperatives/{ref}/households?phone=`) đã đạt. **Chỉ còn tìm theo họ tên**: họ tên chủ hộ chỉ nằm ở kho định danh, mã hóa, không có chỉ mục (R-04), nên pf không lọc được. Muốn tìm theo tên cần khách quyết định PII (vd chỉ mục mù theo họ tên chuẩn hóa ở kho, chấp nhận rủi ro dò tên) | quyết định PII |
| HTX US-PLOT-03 Chọn ranh giới trên ảnh vệ tinh | Chế độ chạm chọn góc trên bản đồ đã có trên App HTX, dùng cùng ràng buộc với đo GPS. Nền bản đồ lấy theo `AG_TILE_URL`. **Nhà cung cấp tile vệ tinh chưa chọn** (RB-7) | chờ khách |
| HTX US-WEATHER-01 Thời tiết vùng | **Nhà cung cấp thời tiết trong lãnh thổ chưa chọn** (RB-7, ADR-0007). Thẻ thời tiết hiện "chờ nguồn" | chờ khách |
| GIS US-CFG-01 Thêm cấu hình | Ngưỡng có phiên bản và phê duyệt (`geo.hub_capacity_warn_pct/full_pct`, `geo.plot_layer_min_zoom`) đã đạt. **Chưa có tham số dải màu heatmap, chưa kiểm HEX**. Kiểm "ngưỡng tăng dần 0–100 %" mới có cho ngưỡng CGH | mã |
| GIS US-CFG-02 Sửa/xóa/khôi phục mặc định | Sửa bằng phiên bản mới đã đạt. **Chưa có "khôi phục mặc định"**. Ghi Snapshot ngày chuyển sau (US-SNAP-01) | mã |
| GIS US-LOG-01 Đăng nhập | Đăng nhập bằng email, phân quyền và thông báo lỗi đã đạt. **AC-3: realm khóa tạm 15 phút** (`maxFailureWaitSeconds`), trong khi AC yêu cầu khóa tới khi Admin mở. Cần khách chọn (`permanentLockout`) | chờ khách |
| GIS US-WH-01 Dung lượng kho | Như FN-06: chưa có job đồng bộ định kỳ (AC-2) | mã |
| GIS US-BASE-01 Ranh giới đa cấp & polygon theo zoom | Theo cấp và zoom, ≤ 1.000 polygon/khung nhìn, nhập GeoJSON ranh giới hành chính đã đạt. **Chưa đo ≤ 1,5 s**; polygon **địa chính (VN-2000) chờ khách** | chờ khách + đo |
| GIS US-INT-01 Đồng bộ App HTX, CGH, ERP | App HTX (cùng nền tảng, qua sự kiện/bảng chiếu) và ERP (van `hub_capacity`) đã đạt; nhập tay có xác nhận trước khi ghi đè đạt. **Chưa có lớp CGH** (số máy, thiếu/thừa) trên bản đồ GIS | mã |
| GIS US-INT-02 Bản đồ chuẩn hóa cho TMS | Có lớp gộp theo khung nhìn (HTX, Hub + widget, đường). **Chưa có bề mặt API cho TMS** (xác thực dịch vụ, nền bản đồ trong cùng lần trả). **Chưa đo ≤ 500 ms**. Phạm vi và độ trễ cần khách chốt | chờ khách + mã |
| GIS US-SYNC-01 Giám sát đồng bộ | Mỗi sự kiện đã ghi inbox/outbox, có metric độ sâu DLQ. **Chưa có màn giám sát theo nguồn/thời gian/trạng thái** | mã |
| GIS US-SYNC-02 Retry & dead-letter | Retry, giới hạn giao 5 lần và DLQ đã đạt. **"Phát cảnh báo cho Admin" mới là metric**: chưa có luật cảnh báo hay thông báo | mã + vận hành |
| Bộ tham số mô phỏng chính thức | Cơ chế đạt: 49 tham số BRD + #50–#57, chặn "Chính thức" khi tham số chưa duyệt. **Giá trị và phê duyệt của bộ 57 tham số chờ khách**; #48/#49 chưa chốt. Chế độ chọn Hub MILP còn là thử nghiệm (chờ khách chọn MILP hay đánh giá kịch bản) | chờ khách |
| RB-4 Giấy phép | Cổng CI có cho Go/npm/Python. **ADR-0016 (Đề xuất) chưa ký**: `typing-extensions` (PSF-2.0), `numpy` (biểu thức gộp và thư viện nhị phân gói kèm) chưa vào danh sách cho phép; CI chỉ in ra | chờ ký |
| C-3 Ngưỡng k của van dữ liệu công | Chưa có van dữ liệu công. Hai hợp đồng G2 (`hub_facility`, `hub_capacity`) là dữ liệu vận hành của MKG, không phải dữ liệu gom nhóm. Ngưỡng k chờ MKG (Q-MKG-11) | chờ khách |

**10 dòng "ngoài G2"**:
- **GIS US-NAT-01** (lớp tự nhiên, dữ liệu khí hậu qua API), **GIS US-WX-01/02** (dự báo và cảnh báo thời tiết): chờ nhà cung cấp trong lãnh thổ (RB-7), cùng HTX US-WEATHER-01;
- **GIS US-SNAP-01/REPLAY-01/RETAIN-01** (snapshot ngày, replay, chính sách lưu và dọn): cần lớp lịch sử của `pf/metrics` (G3) và chính sách lưu FN-21 mà khách chưa chốt. Chính sách này xung đột với RB-1 (X1);
- **RB-6** đối soát số đo ở biên, **R-95** phần số đo, **C-4** bảng dung sai cân: G3, cùng `mkg/measurement`.

## 1. Cách chạy

```powershell
. .\scripts\dev-env.ps1
go test -count=1 ./...                                   # gồm tests/e2e, tests/acceptance (có TestFarmerDevicePullsOwnHouseholdOnly)
go run ./tools/archlint ./... ; go run ./tools/migratecheck migrations ; go run ./tools/contractcheck
```

Kiểm thử `pf/waterway` cần pgRouting ở cụm pf: máy dev Windows dùng 4.0.1; CI và Compose dùng image `deploy/compose/postgres`. Chỉ dùng `pgr_dijkstra`/`pgr_ksp`.

Web vận hành (`apps/web-ops`, backend giả lập theo hợp đồng):

```powershell
cd apps/web-ops; npm ci
npm test                 # Vitest: 176 ca (G2 thêm src/gis, src/htx, deepLink, g2gaps, …)
$env:CI='1'; npm run e2e # Playwright: 94 ca; G2 thêm htx-* (gồm htx-g2-gaps), gis-map, gis-admin, waterway, simulation, sop, catalog-import, coop-deactivate
```

Di động (Flutter 3.47.5):

```powershell
cd packages/agrigreen_mobile; cmd /c "flutter test"      # lõi chung: 101 ca (g2_entities_test, sync_nudge_entities_test, boundary_service_test, …)
cd apps/htx-app;            cmd /c "flutter test"      # App HTX: 64 ca (plot_boundary_test, news_category_call_test, …)
flutter test integration_test -d emulator-5554           # htx_flow_test: đăng nhập → PIN → mở vụ, nhật ký có ảnh, yêu cầu hỗ trợ ngoại tuyến
cd apps/field-app;          cmd /c "flutter test"      # app hiện trường: 55 ca (sau khi tách lõi)
```

Optimizer (`optimizer/`, Python 3.12):

```powershell
cd optimizer; pip install ".[dev,milp]"; python -m pytest -q   # 36 ca: golden prototype, hợp đồng contracts/optimizer, RB-7, vòng nhận RabbitMQ
```

CI (`.github/workflows/ci.yml`): `lint`, `test`, `web-ops`, `web-e2e`, `optimizer`, `agrigreen-mobile`, `field-app`, `htx-app`, `field-app-emulator`, `security`, `images` (thêm image optimizer), `compose-smoke` (thêm api-internal, optimizer, mTLS nội bộ). **Chưa chạy trên GitHub cho G2**, xem ghi chú môi trường.

## 2. Lộ trình G2 (docs/roadmap.md)

| Hạng mục | Kiểm thử | Trạng thái |
|---|---|---|
| Hạ tầng: pgRouting ở cụm pf, vai module G2, số migration | `TestGISUSWater01BuildPublishRoute` (định tuyến `pgr_dijkstra` trên DB thật), `TestBuildWiresG2Consumers`, `TestKeycloakRealmCoversRolesAndClaims` (đủ vai `farmer`, `sop_admin`, `waterway_*`, `simulation_analyst`, `hub_*`), `go run ./tools/migratecheck` | **đạt** |
| `platform/cropcycle`: nhật ký canh tác dạng sổ | `TestFarmLogRecord_HTX_US_LOG_01_02`, `TestFarmLogCorrection_HTX_US_LOG_01_AC3` (bản ghi bù, sổ chỉ ghi thêm), `TestSeasonCorrectionAndCancel`, `TestRebuildProjections_R22` | **đạt** |
| `platform/cropcycle`: sản lượng decimal | `TestHarvestDeclare_HTX_US_YIELD_01_03`, `TestReportData_Production_HTX_US_DASH_02` (kg → tấn chính xác), `TestXLSX_HTXProduction_HTX_US_DASH_02`; app: `domain_test.dart` (kg dạng chuỗi decimal, không số thực) | **đạt**. Nhập theo kg (`shared/mass`), hiển thị t/ha |
| Liên thông KN ↔ HTX bằng sự kiện | `TestHouseholdEventsKNUSHTX05`, `TestSupportRequestCreatesTaskWithDutyFallback`, `TestCGHFN07AreaSyncConsumerIdempotentAndRespectsLock`, `TestFarmLogNotificationsHTXUSLOG05`, `TestAlertReachesFarmersAndCoopManagersInAreaHTXUSNEWS03`, `TestWorkerConsumersDeclaredByPublisher` | **đạt** |
| `pf/waterway` (pgRouting, có phiên bản) | Xem mục 6 (A-5) | **đạt** |
| `pf/simulation` | Xem mục 6 (A-6..A-9) | **đạt** |
| `optimizer/` (Python) | pytest 36 ca; `TestExampleSnapshotIsUpToDate` (hợp đồng liên ngôn ngữ); `TestSimulationAsyncFlowEndToEnd` | **đạt**. Chọn Hub MILP là chế độ thử nghiệm (mục 6) |
| Thiết kế chi tiết `mkg/*` + hợp đồng van đầu tiên | Xem mục 6 (A-1..A-4) | **đạt** |
| `packages/agrigreen_mobile` + `apps/htx-app` | Lõi 92 ca, App HTX 53 ca, `integration_test/htx_flow_test.dart` | **đạt** (tái cấu trúc giữ đủ 143 ca của app hiện trường) |
| web-ops: màn App HTX (vụ, nhật ký, duyệt, sản lượng, dashboard, tài khoản nông dân, yêu cầu hỗ trợ, SOP) | WEB-E2E `htx-seasons`, `htx-logs`, `htx-review`, `htx-yield`, `htx-dashboard`, `htx-farmers`, `htx-support`, `sop`, `catalog-import`, `coop-deactivate` `.spec.ts`; Vitest `src/htx/rules.test.ts`, `production.test.ts`, `support.test.ts` | **đạt** |
| web-ops: GIS nhiều lớp, biên tập bản đồ, kho/Hub, thủy hệ, mô phỏng | WEB-E2E `gis-map`, `gis-admin`, `waterway`, `simulation` `.spec.ts`; Vitest `src/gis/gis.test.ts` | **đạt** |

## 3. Chuyển từ G1 (các dòng "ngoài G1 → G2" và phần G2 của US-SYNC-02)

| Yêu cầu G1 | Kiểm thử G2 | Trạng thái G2 |
|---|---|---|
| KN US-HTX-05 đồng bộ nông hộ từ App HTX | `TestHouseholdEventsKNUSHTX05` (`pf.party.household.registered` và `.membership_changed` cùng giao dịch, không PII, không phát lại khi trùng), `TestHouseholdEventsNotifyCommuneOfficerKNUSHTX05` (cán bộ xã nhận thông báo; nhập tệp không báo), `TestCoopStatsEndpoint` | **đạt** |
| KN US-SYNC-01 hai chiều với App HTX | App HTX và app KN dùng chung bản ghi pf. `TestFarmerDevicePullsOwnHouseholdOnly` (stack thật: nông dân chỉ nhận dữ liệu hộ mình, không nhận entity của cán bộ), `TestFarmerPullByLinkedHouseholds`, `TestFarmerPlotSyncByHousehold_HTX_US_SEASON_01`, `TestSyncPushPull_HTX_US_LOG_04`, `TestSupportRequestViaSync`, `TestHouseholdPubKeysFarmer` | **đạt** |
| KN US-SYNC-02 chọn nhóm/loại dữ liệu | `TestNudgeEntitiesFilterKNUSSYNC02` (`entities[]` trong `sync_now`), `TestPullEntitiesFilterKNUSSYNC02` (`?entities=`, entity lạ → 422); lõi: `sync_nudge_entities_test.dart`; App HTX: `nudge_deeplink_test.dart`; WEB-E2E `htx-g2-gaps.spec.ts` (KN US-SYNC-02: `/dong-bo` chọn nhóm dữ liệu → "Đồng bộ ngay" gửi `entities[]`, kết quả nêu đúng nhóm); Vitest `src/htx/g2gaps.test.tsx` (gộp entity theo nhóm, không trùng, rỗng = tất cả) | **đạt**. Nhóm dữ liệu trên web ánh xạ tới entity chiều xuống của pf/sync (`src/lib/syncEntities.ts`) |
| KN US-TASK-01 tự sinh nhiệm vụ từ yêu cầu App HTX | `TestSupportRequestCreatesTaskWithDutyFallback` (cùng giao dịch, mốc giờ máy chủ nhận ≤ 1 phút, chuỗi trực xã → tỉnh → quản trị), `TestTaskCreatedNotifiesAssigneeKNUSTASK01`, `TestStaffSupportListScopeAndFields`; WEB-E2E `htx-support.spec.ts` | **đạt** |
| CGH FN-07 Tích hợp App HTX | `TestCGHFN07AreaSyncConsumerIdempotentAndRespectsLock` (QT-03 trong giao dịch inbox, giữ bản Admin đã khóa, bỏ sự kiện cũ hơn, cách ly tenant), `TestResolveAreaQT03`, `TestStatsPort` | **một phần**: diện tích đạt; thiếu tình trạng máy, lịch mùa vụ, nhật ký đồng bộ + cảnh báo Admin, fallback theo độ tươi |
| GIS FN-04 ranh giới HTX/Hub | Mục 5, GIS US-BND-01..08 | **đạt** |
| GIS FN-06 kho/Hub | Mục 5, GIS US-WH-01 | **một phần** (đồng bộ định kỳ) |
| GIS FN-07 giao thông, thủy hệ, tự nhiên, tile | Đường bộ và thủy hệ: mục 5 (FN-08/09). WEB-E2E `gis-map.spec.ts` kiểm nền bản đồ chỉ lấy từ biến môi trường, không gọi dịch vụ ngoài | **một phần**: tile vệ tinh chờ nhà cung cấp; lớp tự nhiên ngoài G2 |
| RB-6 đối soát số đo ở biên | — | **ngoài G2 → G3** (`mkg/measurement`) |
| R-95 phần số đo (số cuộn, sản lượng quy đổi, độ ẩm) | — | **ngoài G2 → G3** |

## 4. App HTX (Backlog v4.0, 48 story)

### A. Tài khoản và đăng nhập

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| HTX US-ACC-01 tạo & quản lý tài khoản | Cán bộ: `TestCreateUserUSADM01`, `TestAssignmentChange` (AC-2), `TestLockUnlockNeedsReason` (AC-4..6) (G1). Nông dân: `TestFarmerAccountCreateScopeHTXUSACC01` (tên đăng nhập = SĐT chuẩn hóa, vai `farmer`, không mật khẩu, HTX khác bị chặn), `TestActivationReissueRevokesPrevious`, `TestActivationTokenNeverLoggedOrStored`; WEB-E2E `htx-farmers.spec.ts` (QR + liên kết một lần kèm cảnh báo không chia sẻ, phát lại thu hồi lượt cũ) | **một phần**: cần phần mở rộng Keycloak phát liên kết hành động. "Mật khẩu tạm qua email" được thay bằng liên kết một lần (X2) |
| HTX US-ACC-02 nhập tài khoản hàng loạt | `TestImportXLSXMixedRows` (G1), `TestFarmerImportPerRowHTXUSACC02` (dòng lỗi có lý do, dòng hợp lệ có liên kết riêng); WEB-E2E `htx-farmers.spec.ts` | **một phần**: như US-ACC-01 |
| HTX US-LOGIN-01 đăng nhập, điều hướng theo vai | App: `login_flow_test.dart` (nông dân → PIN → trang chủ nông dân; quản lý HTX → trang chủ quản lý; vai từ access token khi ngoại tuyến; màn kích hoạt chỉ mở liên kết https đúng máy chủ); `TestKeycloakRealmCoversRolesAndClaims` (không tự đăng ký, AC-2); web `navigation.spec.ts` | **đạt**. AC-3 "đổi mật khẩu tạm lần đầu" = required action UPDATE_PASSWORD trong liên kết kích hoạt; với nông dân, đường này phụ thuộc phần mở rộng ở US-ACC-01 |
| HTX US-LOGIN-02 đăng nhập & làm việc offline | Lõi: `auth_test.dart` (phiên 72 giờ, PIN băm, token trong kho bí mật), SQLCipher; `integration_test/htx_flow_test.dart` (mất mạng → có mạng lại → push accepted, AC-3) | **đạt**. Lần đăng nhập đầu bắt buộc có mạng (AC-2) |

### B. Danh mục và cấu hình

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| HTX US-CAT-01 danh mục & SOP | `TestSOPAuthoringAndPublish_HTX_US_CAT_01` (bước liên tục, tên trùng, phát hành cần `param_approver` khác người soạn, phải có bước thu hoạch, bản đã phát hành bất biến), `TestNormalizeStepsContiguousSeq`, `TestCatalogDeactivateKeepsHistory` (AC-2 ẩn, không xóa cứng), `TestCatalogNormalize`; WEB-E2E `sop.spec.ts` (gồm gắn giống ↔ SOP) | **đạt**. AC-3..5: danh mục xuống máy qua entity `catalog_item` |
| HTX US-CAT-02 nhập danh mục hàng loạt | `TestCatalogImportPerRowErrors`; WEB-E2E `catalog-import.spec.ts` | **đạt** |
| HTX US-CAT-03 nhân bản SOP | `TestSOPClone_HTX_US_CAT_03`; WEB-E2E `sop.spec.ts` | **đạt** |

### C. Hồ sơ HTX và nông hộ

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| HTX US-HTX-01 hồ sơ HTX | E2E-02 (G1), `TestDeactivateReactivateCooperative` (AC-2), `TestDeactivationPreviewHTXUSHTXSTATUS01` + `TestCoopStatsEndpoint` (AC-3: số nông hộ, vụ chưa xong, nhật ký chờ duyệt); WEB-E2E `coop-deactivate.spec.ts` | **đạt** |
| HTX US-HTX-02 nhập HTX hàng loạt | `TestCooperativeImport_HTX_US_HTX_02` (tệp mẫu .xlsx có đủ cột; tạo mọi dòng hợp lệ trong một lần, Mã HTX hệ thống sinh, địa chỉ + MST + cấp trên theo mã; dòng thiếu ô/thiếu hẳn cột → "Thiếu cột bắt buộc: Người đại diện"; tên trùng → thông điệp US-HTX-03 AC-3; xã ngoài tỉnh bị loại; tệp không phải bảng tính → 422 "Sai định dạng file"; người đại diện + SĐT chỉ ở kho định danh, không có trong sổ/bảng pf; gọi lặp cùng khóa phát lại, khóa mới không tạo trùng; quản lý HTX → 403; cách ly tenant); WEB-E2E `htx-g2-gaps.spec.ts` (tải tệp mẫu, nhập, số dòng thành công/lỗi và lý do từng dòng trên màn hình); Vitest `g2gaps.test.tsx` | **đạt**. Giả định: nhập HTX là thao tác quản trị dữ liệu chủ (Admin API: `registry_admin`, `kn_province_admin` trong tỉnh, `platform_admin`, MFA) — Quản lý HTX chỉ quản lý HTX của mình nên không tạo HTX khác. Cột "Mã xã" bắt buộc thêm vì HTX phải gắn xã để tính phạm vi (ADR-0009). Địa chỉ trụ sở lưu ở `pf_party.cooperative.head_office` (migration pf 000032) |
| HTX US-HTX-03 sao chép hồ sơ HTX | `TestCooperativeCopy_HTX_US_HTX_03` (giữ xã, địa chỉ, cấp trên, diện tích, ngày thành lập; không chép MST, toạ độ, Mã HTX; người đại diện mới vào kho định danh, KHÔNG kế thừa của nguồn; tên trùng không phân biệt hoa thường → 409 "Tên HTX này đã tồn tại, vui lòng chọn tên khác."; gọi lặp; tỉnh khác/tenant khác → 404, quản lý HTX → 403); WEB-E2E `htx-g2-gaps.spec.ts` (`/htx/[id]` "Sao chép hồ sơ": sửa Tên + Người đại diện, tên trùng bị chặn, mở bản sao) | **đạt**. Kiểm tên trùng dưới khóa tư vấn trong giao dịch (không đặt UNIQUE vì dữ liệu G1 có thể đã trùng tên) |
| HTX US-HH-01 hồ sơ nông hộ | `TestHouseholdDedupeViaVault` (AC-3 SĐT trùng), `TestCommuneOfficerCannotCreateOutsideCommune`, E2E-03 (G1); `TestHouseholdSearchByPhone_HTX_US_HH_01` (AC-2 theo SĐT: đúng một hộ qua chỉ mục mù, có mục đích; hộ HTX khác/SĐT lạ → rỗng; không phải SĐT → 422; quản lý HTX khác → 404); WEB-E2E `htx-g2-gaps.spec.ts` (`/htx/nong-ho` tìm theo SĐT; nhập họ tên → giải thích vì sao không tìm được) | **một phần**: chỉ thiếu tìm theo họ tên — họ tên chỉ nằm ở kho định danh (R-04), cần khách quyết định PII |
| HTX US-HH-02 nhập/sao chép nông hộ | `TestImportHouseholdsCSV`, `TestImportHouseholdsXLSX` (G1); `TestHouseholdBulkCopy_HTX_US_HH_02` (AC-2 sửa riêng họ tên/SĐT từng hồ sơ, giữ HTX/xã/diện tích của hộ nguồn; AC-4 "Thiếu họ tên", "SĐT đã tồn tại", hộ HTX khác "Bạn không có quyền truy cập nông hộ này.", không chặn hồ sơ hợp lệ; gọi lặp phát lại đúng kết quả; > 50 hồ sơ → 422; cách ly tenant); WEB-E2E `htx-g2-gaps.spec.ts` (chọn nhiều hộ → sửa từng hồ sơ → "1 hồ sơ thành công · 1 hồ sơ lỗi" kèm lý do trên màn hình, AC-3); Vitest `g2gaps.test.tsx` | **đạt**. Định danh hộ nguồn không được đọc hay chép — hồ sơ mới có định danh riêng trong kho |
| HTX US-HH-03 gọi nhanh nông hộ | `TestHouseholdQuickCall_HTX_US_HH_03` (`GET /v1/households/{ref}/contact` → `{household_ref, phone}`, `Cache-Control: no-store`, mục đích `extension.contact`; HTX khác cùng xã → 403 "Bạn không có quyền truy cập nông hộ này."; không có/xóa định danh → 404 `party.household_contact_unavailable`; tenant khác, `cgh_viewer` bị chặn); App: `news_category_call_test.dart` (`tel:` với SĐT đọc lúc bấm, máy không gọi được → "Không thể khởi tạo cuộc gọi trên thiết bị này."); WEB-E2E `htx-g2-gaps.spec.ts` (nút gọi cạnh từng hộ, liên kết `tel:`, máy tính không gọi được → thông báo AC-2, ẩn số) | **đạt**. Quản lý HTX cần vai kho `vault_registrar` (như khi tạo nông hộ) |

### D. Lô ruộng và GPS

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| HTX US-PLOT-01 xem & sửa ranh giới lô | `TestOverlapRules`, `TestCorrectionCreatesNewVersion` (fixture có vai `coop_manager`), `TestFarmerPlotSyncByHousehold_HTX_US_SEASON_01`, tests/acceptance `TestPrototypeGanThuaChoHTXVaNongHo` (AC-3), `TestCoopManagerSyncPush_HTX_US_PLOT_01_02` (quản lý HTX đẩy `plot_boundary` không cần `plot_editor`, chỉ lô HTX mình); WEB-E2E `htx-g2-gaps.spec.ts` (`/htx/lo-ruong`: bản đồ lô của HTX kèm lô KN vẽ trước "Nháp - KN vẽ trước", chọn lô → vụ và nhật ký, lô nháp không yêu cầu vẽ lại); App HTX: `plot_boundary_test.dart` (HTX US-PLOT-01: màn "Lô ruộng & Ranh giới GPS" hiện lô KN vẽ trước với nhãn "Nháp - KN vẽ trước", lô nháp chưa gán HTX; lô HTX khác không hiện, sửa bị chặn "Bạn không có quyền truy cập lô ruộng này."; sửa lô KN tạo bản bù `correct` cùng `plot_ref`, `supersedes_id` là phiên bản KN, `source: htx_app`); lõi: `boundary_service_test.dart` | **đạt**. "Chia nhỏ ranh giới" (một lô thành hai phiên bản mới, có lý do, chặn khi đang có vụ mở) **chưa có** ở G2: pf/geo chưa có phép cắt hình và kiểm vụ mở của pf/cropcycle — hiện sửa bằng bản ghi bù và vẽ thêm lô |
| HTX US-PLOT-02 đánh dấu ranh giới bằng GPS | App HTX: `plot_boundary_test.dart` (HTX US-PLOT-02: < 4 điểm hoặc thẳng hàng bị chặn với thông báo AC-2; GPS yếu thì cảnh báo "±35 m", có nút "Đánh dấu lại" và "Chọn trên bản đồ"; lệch diện tích vượt tham số `geo.area_deviation_max_pct` phải xác nhận; khép vùng tạo lô mới của HTX mình, `method: gps`); lõi: `boundary_service_test.dart` (ngưỡng theo tham số); app hiện trường: `boundary_test.dart` (G1) | **đạt**. Màn vẽ dùng chung ở lõi (`BoundaryEditorScreen`, `BoundaryService`) với app hiện trường. Máy chủ nhận `plot_boundary` từ `coop_manager` cho lô HTX mình (`TestCoopManagerSyncPush_HTX_US_PLOT_01_02`) |
| HTX US-PLOT-03 chọn trên ảnh vệ tinh | App HTX: `plot_boundary_test.dart` (HTX US-PLOT-03: chế độ "Chọn trên bản đồ" chạm góc; ở chế độ đi GPS chạm bản đồ không thêm điểm; cùng ràng buộc ≥ 4 điểm không thẳng hàng; `method: map`) | **một phần**: nền theo `AG_TILE_URL`; tile vệ tinh chờ nhà cung cấp (RB-7) |
| HTX US-PLOT-04 cảnh báo chồng lấn | `TestOverlapRules`, `TestClassifyOverlaps`; WEB-E2E `htx.spec.ts`, `conflicts.spec.ts` (G1) | **đạt**. Quy tắc X10 áp dụng cho mọi client |
| HTX US-PLOT-05 vẽ ranh giới offline | App HTX: `plot_boundary_test.dart` (HTX US-PLOT-05: mất sóng giữa lúc đi ranh giới, điểm lưu tạm trong CSDL mã hóa và khôi phục khi mở lại màn; lưu vào hàng đợi; có mạng thì đồng bộ đúng một bản ghi kể cả khi mất phản hồi; máy chủ báo chồng lấn thì bản ghi thành xung đột trong hàng đợi, xác nhận tạo bản bù `confirm_overlap`; nền bản đồ đọc gói tile trong CSDL, không cần mạng); lõi: `boundary_service_test.dart` (nháp còn sau khi app tắt đột ngột); app hiện trường: `boundary_test.dart`, `integration_test/app_flow_test.dart` (G1) | **đạt** |
| HTX US-GPS-01 xác thực vị trí khai báo | `TestGPSNormalizeAndStatus`, `TestFarmLogRecord_HTX_US_LOG_01_02` (nhãn không chặn lưu), `TestOpenSeasonFarmer_HTX_US_SEASON_01`, `TestGpsFixLog_HTX_US_GPS_03` (khoảng cách trắc địa tới lô); app: `domain_test.dart` (từ chối quyền → `denied`, không chặn), `farm_log_flow_test.dart` (AC-4); WEB-E2E `htx-logs.spec.ts` ("Vị trí không khớp", "Thiếu GPS") | **đạt**. Ngưỡng 50 m là tham số `cropcycle.gps_match_tolerance_m` |
| HTX US-GPS-02 xác thực khi GPS yếu | App: `domain_test.dart` (nguồn `photo_exif`, nhãn "Không có GPS thiết bị", nguồn luôn có nhãn), `season_fix_test.dart` (GPS-02) | **đạt** |
| HTX US-GPS-03 log lịch sử toạ độ | `TestGpsFixLog_HTX_US_GPS_03` (chỉ đọc, sửa/xóa → 405 `geo.gps_log_read_only` kể cả Admin), `TestGpsLogConsumerAfterCommit_HTX_US_GPS_03` (ghi sau commit, lỗi geo tạm thời → giao lại, nghiệp vụ không bị chặn), `TestSupportRequestGpsLogConsumer_HTX_US_GPS_03`, `TestFarmerReadsOwnGpsLog_HTX_US_GPS_03` | **đạt**. Sổ phân vùng tháng, không xóa (≥ 12 tháng, AC-2). GPS là dữ liệu cá nhân hay không, thời hạn lưu: chờ khách |

### E. Thời tiết

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| HTX US-WEATHER-01 thời tiết vùng | — (thẻ "chờ nguồn") | **một phần**: chờ nhà cung cấp (RB-7) |
| HTX US-WEATHER-02 lời khuyên nông vụ | App: `login_flow_test.dart` (AC-1 có vụ → thẻ lời khuyên theo bước SOP hiện tại; AC-2 chưa có vụ → ẩn thẻ, thẻ thời tiết vẫn hiện), `domain_test.dart` (bước hiện tại theo ngày sau sạ) | **đạt**. Lời khuyên lấy từ bước SOP. Phần kết hợp thời tiết chờ US-WEATHER-01 |

### F. Mùa vụ và nhật ký

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| HTX US-SEASON-01 nông dân tự mở vụ | `TestOpenSeasonFarmer_HTX_US_SEASON_01` (SOP tự gán theo giống, ngày sạ trong [−15, +30]), `TestCheckSowingDate`, `TestSeasonOpenedByFarmerNotifiesManagerHTXUSSEASON01`; app: `season_flow_test.dart` (mở vụ 4 bước ngoại tuyến, cửa sổ ngày theo giờ Việt Nam), `season_fix_test.dart` (sửa vụ bằng bản ghi bù); WEB-E2E `htx-seasons.spec.ts` | **đạt** |
| HTX US-SEASON-02 mở vụ hộ | `TestBulkOpen_HTX_US_SEASON_02`; app: `season_flow_test.dart`, `manager_test.dart`, `season_fix_test.dart` (hủy vụ mở nhầm); WEB-E2E `htx-seasons.spec.ts` | **đạt** |
| HTX US-SEASON-03 sao chép cấu hình vụ trước | `TestCopyPreviousSeason_HTX_US_SEASON_03`; app: `season_fix_test.dart` (có mạng và ngoại tuyến); WEB-E2E `htx-seasons.spec.ts` | **đạt** |
| HTX US-SEASON-04 chặn mở vụ trùng | `TestOneOpenSeasonPerPlot_HTX_US_SEASON_04` (kể cả hai yêu cầu song song), `TestBulkOpen_HTX_US_SEASON_02` (AC-2); app: `season_fix_test.dart` (đúng văn bản AC-1) | **đạt** |
| HTX US-LOG-01 ghi nhật ký | `TestFarmLogRecord_HTX_US_LOG_01_02` (GPS + thời gian, máy móc), `TestFarmLogCorrection_HTX_US_LOG_01_AC3` (giữ GPS/thời gian, `concurrent_edit`); app: `farm_log_flow_test.dart`; lõi: `photo_pipeline_test.dart` (watermark, AC-4) | **đạt** |
| HTX US-LOG-02 ghi hộ | `TestFarmLogRecord_HTX_US_LOG_01_02` (AC-2 chặn khi chưa mở vụ); app: `farm_log_flow_test.dart`; WEB-E2E `htx-logs.spec.ts` | **đạt** |
| HTX US-LOG-03 ghi hàng loạt | `TestFarmLogBatch_HTX_US_LOG_03` (một đầu lô + một dòng mỗi thửa); app: `batch_log_test.dart` (3 ca); WEB-E2E `htx-logs.spec.ts` | **đạt**. "Một bản ghi duy nhất" = một đầu lô, giữ minh chứng từng thửa cho MRV (giả định g2-conventions) |
| HTX US-LOG-04 ghi offline | `TestSyncPushPull_HTX_US_LOG_04`; app: `news_queue_screen_test.dart` (chờ gửi, lùi thử lại, lý do từ chối), `integration_test/htx_flow_test.dart`; lõi: `g2_entities_test.dart`, `outbox_test.dart` | **đạt** |
| HTX US-LOG-05 duyệt nhật ký | `TestFarmLogReview_HTX_US_LOG_05` (người ghi không tự duyệt; chỉ nhật ký đã duyệt vào tiến độ SOP), `TestFarmLogNotificationsHTXUSLOG05` (AC-3 thông báo người ghi); app: `manager_test.dart`, `farm_log_flow_test.dart` (bổ sung = bản ghi bù), `nudge_deeplink_test.dart`; WEB-E2E `htx-review.spec.ts` | **đạt**. Quy tắc duyệt (ai duyệt, có tự duyệt cho hộ nhỏ không): chờ khách |

### G. Sản lượng

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| HTX US-YIELD-01 khai sản lượng | `TestHarvestDeclare_HTX_US_YIELD_01_03` (AC-1 chặn trước bước thu hoạch của SOP đã ghim, AC-2 đóng vụ), `TestDatesForHarvestGate`, `TestHarvestPHI`, `TestDashboard_HTX_US_DASH_01`, `TestSyncPushPull_HTX_US_LOG_04` (AC-3); app: `harvest_flow_test.dart`, `domain_test.dart` | **đạt**. Cổng thu hoạch = offset − cửa sổ của bước SOP; thời gian cách ly thuốc (PHI) theo nguyên mẫu. Cả hai là giả định chờ khách |
| HTX US-YIELD-02 nhập sản lượng hàng loạt | `TestHarvestImport_HTX_US_YIELD_02`; WEB-E2E `htx-yield.spec.ts` (bảng nhập nhanh, CSV) | **đạt** |
| HTX US-YIELD-03 cảnh báo bất thường | `TestYieldAndAnomaly`, `TestHarvestDeclare_HTX_US_YIELD_01_03` (âm → từ chối; 0 hoặc vượt ngưỡng theo giống → cần xác nhận); app: `harvest_flow_test.dart` (xác nhận trong hàng đợi → bản ghi mới); WEB-E2E `htx-yield.spec.ts` | **đạt**. Đơn vị và ngưỡng theo giống là tham số tạm, chờ khách |

### H. Hỗ trợ

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| HTX US-SUPPORT-01 gửi yêu cầu hỗ trợ | `TestSupportRequestValidationAndReplay` (thiếu GPS vẫn gửi, thông điệp AC-3, ≤ 5 ảnh), `TestSupportRequestViaSync` (AC-2), `TestSupportRequestCreatesTaskWithDutyFallback`; app: `support_staff_test.dart`, `domain_test.dart`; WEB-E2E `htx-support.spec.ts` | **đạt** |
| HTX US-SUPPORT-02 danh bạ & gọi nhanh | `TestCoopStaffDirectoryHTXUSSUPPORT02` (SĐT đọc lúc bấm, `no-store`; không có số → ẩn gọi); app: `support_staff_test.dart` (gọi `tel:`, chép số AC-1, mất mạng → thông báo) | **đạt** |

### I. Tin tức và nội dung

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| HTX US-CMS-01 soạn bài viết | `TestPublishNewVersionKeepsOld`, `TestBodySanitisedOnSave`, `TestAuthorCannotSelfApprove` (G1); WEB-E2E `htx-g2-gaps.spec.ts` (`/quan-tri/bai-viet`: tiêu đề, chuyên mục, nội dung, ảnh bìa; "Xem trước trên điện thoại" khung 360 × 720 có ảnh bìa, chuyên mục, tiêu đề, nội dung; xem trước bản đã lưu lấy đúng phiên bản máy chủ đã lọc qua `GET /v1/documents/{ref}/versions/{no}`; thiếu chuyên mục → "Vui lòng nhập tiêu đề và chọn chuyên mục trước khi xuất bản."; gửi duyệt); Vitest `g2gaps.test.tsx` (Markdown tắt HTML thô, liên kết chỉ http/https/mailto) | **đạt**. AC-2: bài xuất bản xuống máy ở lượt đồng bộ hoặc thông báo đẩy kế tiếp, không cần người dùng làm mới (như US-NEWS-01) |
| HTX US-CMS-02 nhân bản & trích xuất | `TestAlertPublishedEvent` (nhân bản thành bản nháp giữ nội dung, AC-1), `TestExtract`, `TestExtractPDFRejects` (AC-2: trích thất bại → soạn tay) | **đạt** |
| HTX US-NEWS-01 xem tin tức | `TestSearchAccentInsensitive` (lọc `category`), `TestSyncRetireTombstone`; app: `news_queue_screen_test.dart`, `news_category_call_test.dart` (HTX US-NEWS-01: danh sách chia nhóm theo `document.category`, chip lọc chỉ hiện đúng chuyên mục, không trộn; cảnh báo khẩn vẫn trên cùng) | **đạt**. Bài mới xuống máy theo lượt đồng bộ hoặc đẩy (như US-CMS-01 AC-2) |
| HTX US-NEWS-02 lưu tin đọc offline | `TestSyncRetireTombstone`, `TestPersonalLibraryUSLIB05`; app: `news_queue_screen_test.dart` (lưu ngoại tuyến, bài đã gỡ vẫn đọc được kèm nhãn) | **đạt** |
| HTX US-NEWS-03 cảnh báo khẩn cấp | `TestAlertPublishedEvent`, `TestAlertReachesFarmersAndCoopManagersInAreaHTXUSNEWS03` (nông dân có thửa trong vùng, không tới ngoài vùng), `TestAreaFarmersAndSeasonActivityAdapters`, `TestPushDataOnlyAndPerDeviceFailures` (AC-3); app: `nudge_deeplink_test.dart` | **đạt**. FCM thật cần `AG_FCM_*` |

### J. Trạng thái HTX

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| HTX US-HTXSTATUS-01 vô hiệu hóa HTX | `TestDeactivationPreviewHTXUSHTXSTATUS01` (AC-1: số nông hộ, vụ, nhật ký chờ duyệt), `TestOrgDeactivationLocksOnlyThatOrgAndReactivationRestoresOnlyThose` (AC-2); WEB-E2E `coop-deactivate.spec.ts` | **đạt** (G1 AC-2, G2 AC-1) |
| HTX US-HTXSTATUS-02 kích hoạt lại | `TestOrgDeactivationLocksOnlyThatOrgAndReactivationRestoresOnlyThose` | **đạt** (G1) |
| HTX US-HTXSTATUS-03 log vô hiệu hóa/kích hoạt | `TestDeactivateReactivateCooperative`, `TestLockUnlockNeedsReason` | **đạt** (G1) |

### K. Dashboard và báo cáo

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| HTX US-DASH-01 dashboard sản lượng | `TestDashboard_HTX_US_DASH_01` (bộ lọc, AC-3 chỉ HTX mình, AC-4 quản trị); app: `manager_test.dart` (mất mạng → bản gần nhất); WEB-E2E `htx-dashboard.spec.ts` | **đạt** |
| HTX US-DASH-02 xuất báo cáo sản lượng | `TestReportData_Production_HTX_US_DASH_02`, `TestReportData_Scope_HTX_US_DASH_03`, `TestHTXProductionExport_HTX_US_DASH_02` (202 + việc nền, khớp bộ lọc), `TestHTXProductionScope_HTX_US_DASH_02`, `TestXLSX_HTXProduction_HTX_US_DASH_02`, `TestPDF_HTXProduction_HTX_US_DASH_02`; WEB-E2E `htx-dashboard.spec.ts` | **đạt**. Xuất trên web; App HTX chỉ xem |
| HTX US-DASH-03 lập lịch gửi báo cáo | `TestHTXProductionSchedule_HTX_US_DASH_03`, `TestScheduleFiresOncePerPeriodAndDelivers`, `TestDeliverySkippedWithoutDirectory`, `TestSMTPRefusesInsecureOrRejected` (G1); `TestCoopScheduleAppSurface_HTX_US_DASH_03` (Application API cho Quản lý HTX, không cần MFA Admin: người nhận chỉ từ `GET /v1/reports/recipients`, người ngoài HTX → 422 kèm thông điệp AC-2; gọi lặp; HTX khác/tenant khác không thấy, cán bộ KN → 403; đến mốc gửi đúng hòm thư đã chọn; ngừng lịch cần lý do, không xóa), `TestOrgRecipients_HTX_US_DASH_03` (chỉ tài khoản đang bật, email đã xác minh, đúng HTX; email đã che); WEB-E2E `htx-g2-gaps.spec.ts` (`/htx/lich-bao-cao`: chọn người nhận, lập lịch, lần gửi thiếu email → "Email đăng ký nhận báo cáo không hợp lệ hoặc gửi thất bại, vui lòng kiểm tra lại.", ngừng lịch) | **đạt**. Giả định (chờ khách xác nhận): người nhận chỉ là tài khoản đã đăng ký của HTX có email đã xác minh, chọn từ danh sách — không nhập email tự do |

## 5. GIS (Backlog v1.1, 38 story)

| Story (FN) | Kiểm thử | Trạng thái |
|---|---|---|
| GIS US-ACC-01 (FN-01) tài khoản | `TestCreateUserUSADM01`, `TestLockUnlockNeedsReason`, `TestBulkActions` (G1); WEB-E2E `accounts.spec.ts` | **đạt**. Liên kết một lần thay mật khẩu tạm (X2) |
| GIS US-LOG-01 (FN-02) đăng nhập | `TestKeycloakRealmCoversRolesAndClaims` (5 lần/15 phút, không tự đăng ký; realm bật `loginWithEmailAllowed`), `TestVerifyBuildsPrincipal`; WEB-E2E `navigation.spec.ts`, `gis-admin.spec.ts` (KNV xã không vào màn biên tập) | **một phần**: khóa tạm 15 phút thay vì tới khi Admin mở |
| GIS US-CFG-01 (FN-03) thêm cấu hình | `TestHubCapacityWidgetThresholds_GIS_US_HUB_01`, `TestLayersZoomGate_GIS_BR24`, `TestG2ParamsSeeded`, `TestSelfApprovalBlocked` | **một phần**: thiếu dải màu + kiểm HEX |
| GIS US-CFG-02 (FN-03) sửa/khôi phục | `TestParamCorrectionSupersedes`, `TestParamLookupAtEventTime` | **một phần**: thiếu khôi phục mặc định |
| GIS US-BND-01 (FN-04) danh sách HTX | `TestCoopMarkersFallback_GIS_US_BND_02`, `TestCoopMarkersAndSites`; WEB-E2E `gis-map.spec.ts` | **đạt**. Toạ độ HTX nằm trong nền tảng (pf/party, pf/geo), không cần gọi "API HTX" theo ngày |
| GIS US-BND-02 (FN-04) thêm ranh giới HTX | `TestCoopBoundaryLedger_GIS_US_BND_02_03`, `TestCoopBoundaryScope_GIS_US_BND_02`, `TestCoopMarkersFallback_GIS_US_BND_02`; WEB-E2E `gis-admin.spec.ts` | **đạt** |
| GIS US-BND-03 (FN-04) sửa/xóa mềm HTX | `TestCoopBoundaryLedger_GIS_US_BND_02_03` (bản ghi bù có lý do, ẩn khỏi bản đồ nhưng giữ lịch sử, chuỗi băm); WEB-E2E `gis-admin.spec.ts` | **đạt** |
| GIS US-BND-04 (FN-04) đồng bộ Hub | `TestHubValveConsumerValidatesContractAndIsIdempotent` (van `hub_facility` → `pf_geo.hub_view`), `TestFacilityMasterDataApprovalAndValve` | **đạt** |
| GIS US-BND-05 (FN-04) thêm Hub | `TestFacilityLedger_GIS_US_BND_05_06` (thông báo đúng câu chữ); WEB-E2E `gis-admin.spec.ts` | **đạt**. "Địa chỉ" = mã tỉnh + xã (không có địa chỉ chữ ngoài vault) |
| GIS US-BND-06 (FN-04) sửa/xóa mềm Hub | `TestFacilityLedger_GIS_US_BND_05_06` | **đạt** |
| GIS US-BND-07 (FN-04) nhập ranh giới HTX | `TestCoopBoundaryImport_GIS_US_BND_07` (GeoJSON, Shapefile; hệ khác WGS84 → `geo.crs_unsupported`); WEB-E2E `gis-admin.spec.ts` | **đạt**. Chuyển VN-2000: chờ khách (X9) |
| GIS US-BND-08 (FN-04) nhập vị trí Hub | `TestFacilityImport_GIS_US_BND_08`, `TestParseFeaturesPoints_GIS_US_BND_08`; WEB-E2E `gis-admin.spec.ts` (KML) | **đạt** |
| GIS US-PLOT-01 (FN-05) đồng bộ thửa từ App HTX | `TestFarmerPlotSyncByHousehold_HTX_US_SEASON_01`, `TestSyncSinkIdempotentAndConflict` | **đạt**. App HTX và GIS dùng chung bản ghi thửa `pf/geo` (không có bản sao để đồng bộ) |
| GIS US-PLOT-02 (FN-05) vẽ thửa | E2E-05, `TestBoundaryValidationRules`, `TestOverlapRules` (G1) | **đạt** |
| GIS US-PLOT-03 (FN-05) sửa/xóa mềm thửa | `TestCorrectionCreatesNewVersion`, tests/acceptance `TestPrototypeNgungDungThuaCoLyDo` (G1) | **đạt** |
| GIS US-PLOT-04 (FN-05) nhập thửa | `TestImportGeoJSONAndKML`, `TestImportShapefileZip` (G1) | **đạt** |
| GIS US-WH-01 (FN-06) dung lượng kho | `TestCapacityManualNotOverwritten_BR52_GIS_US_WH_01`, `TestCapacitySnapshotValveOnlyContractFields`; WEB-E2E `gis-admin.spec.ts` | **một phần**: thiếu đồng bộ định kỳ |
| GIS US-BASE-01 (FN-07) ranh giới đa cấp & thửa theo zoom | `TestAdminBoundaryAndCommuneContainment`, `TestAdminBoundariesByParentG05`, `TestLayersZoomGate_GIS_BR24`, `TestBBoxLimitAndScope`; WEB-E2E `gis-map.spec.ts` | **một phần**: chưa đo 1,5 s; dữ liệu địa chính chờ khách |
| GIS US-ROAD-01 (FN-08) tra tuyến đường bộ theo tải trọng | `TestRoadLedger_GIS_US_ROAD_01_02_03`; WEB-E2E `gis-map.spec.ts` | **đạt**. Lọc theo tải trọng, không định tuyến đường bộ (mô phỏng dùng Haversine hiệu chỉnh) |
| GIS US-ROAD-02 (FN-08) vẽ tuyến đường bộ | `TestRoadLedger_GIS_US_ROAD_01_02_03` (chiều dài trắc địa tự tính, thông báo đúng câu chữ); WEB-E2E `gis-admin.spec.ts` | **đạt** |
| GIS US-ROAD-03 (FN-08) sửa tuyến đường bộ | `TestRoadLedger_GIS_US_ROAD_01_02_03` (sửa là phiên bản mới) | **đạt** |
| GIS US-WATER-01 (FN-09) tra tuyến thủy theo tải trọng sà lan | `TestGISUSWater01BuildPublishRoute`, `TestStructureConstrainsRoute`, `TestDeriveCapacityFromDimensions`, `TestSafetyMargins`; WEB-E2E `gis-map.spec.ts`, `waterway.spec.ts` | **đạt**. Nguồn dữ liệu thủy hệ và người duyệt: chờ khách |
| GIS US-WATER-02 (FN-09) vẽ tuyến sông/kênh | `TestGISUSWater02CreateSegmentValidation`, `TestValidateSegmentMessages`, `TestBuildTJoin` (nhánh rẽ); WEB-E2E `waterway.spec.ts` | **đạt** |
| GIS US-WATER-03 (FN-09) sửa tuyến sông/kênh | `TestGISUSWater03ReviseByCompensatingRecord` | **đạt** |
| GIS US-NAT-01 (FN-10) lớp điều kiện tự nhiên | — | **ngoài G2**: chờ nguồn dữ liệu và nhà cung cấp khí hậu (RB-7) |
| GIS US-WX-01 (FN-11) dự báo thời tiết | — | **ngoài G2**: chờ nhà cung cấp (RB-7) |
| GIS US-WX-02 (FN-12) cảnh báo thời tiết nguy hiểm | — | **ngoài G2**: như WX-01 |
| GIS US-SEASON-01 (FN-13) bản đồ nhiệt giai đoạn | `TestSeasonStageFollowsSeasonDates`, `TestStageMap_GIS_US_SEASON_01_US_YIELD_01` (giai đoạn trội theo HTX tô polygon); WEB-E2E `gis-map.spec.ts` | **đạt**. Bảng màu cố định; cấu hình màu thuộc GIS US-CFG-01 |
| GIS US-YIELD-01 (FN-14) cảnh báo vùng chín rộ | `TestRipeningWindowOneToThreeDays`, `TestStageMap_GIS_US_SEASON_01_US_YIELD_01` (1–3 ngày tới, kèm sản lượng dự kiến của App HTX, không tính lại); WEB-E2E `gis-map.spec.ts` | **đạt** |
| GIS US-HUB-01 (FN-15) widget dung lượng Hub | `TestCapacityClass_GIS_US_HUB_01`, `TestHubCapacityWidgetThresholds_GIS_US_HUB_01`; WEB-E2E `gis-map.spec.ts`, `gis-admin.spec.ts` | **đạt** |
| GIS US-INT-01 (FN-16) đồng bộ App HTX, CGH, ERP | `TestStageMap_GIS_US_SEASON_01_US_YIELD_01`, `TestHubValveConsumerValidatesContractAndIsIdempotent`, `TestCapacityManualNotOverwritten_BR52_GIS_US_WH_01` (AC-2) | **một phần**: thiếu lớp CGH |
| GIS US-INT-02 (FN-17) bản đồ chuẩn hóa cho TMS | `TestLayersZoomGate_GIS_BR24` (lớp gộp theo khung nhìn) | **một phần**: chưa có API cho TMS, chưa đo 500 ms |
| GIS US-EVL-01 (FN-18) nhật ký sự kiện thay đổi | `TestCoopBoundaryLedger_GIS_US_BND_02_03`, `TestFacilityLedger_GIS_US_BND_05_06`, `TestGISUSWater03ReviseByCompensatingRecord`, `TestAppendOnlySixWays`, `TestChainSequenceAndTamperDetection` | **đạt**. Sổ chỉ ghi thêm theo từng lớp (thời điểm nghiệp vụ và thời điểm ghi, người ghi, `supersedes_id`, chuỗi băm) |
| GIS US-SNAP-01 (FN-19) snapshot theo ngày | — | **ngoài G2 → G3** (lịch sử `pf/metrics`) |
| GIS US-REPLAY-01 (FN-20) xem lại theo ngày | — | **ngoài G2 → G3**. Sổ đã đủ để tái dựng; chưa có màn và API |
| GIS US-RETAIN-01 (FN-21) chính sách lưu và dọn | — | **ngoài G2**: chờ khách chốt chính sách lưu; "dọn" phải là chuyển tầng, không xóa (RB-1, X1) |
| GIS US-SYNC-01 (FN-22) giám sát đồng bộ | `TestOutboxToInboxExactlyOnceEffect`, `TestDLQDepth`, `TestConflictSLA` | **một phần**: thiếu màn giám sát |
| GIS US-SYNC-02 (FN-23) retry & dead-letter | `TestDLQDepth`, `TestWorkerConsumersDeclaredByPublisher`; pytest `test_permanent_errors_go_to_dlq`, `test_post_5xx_requeues_with_backoff_not_ack`, `test_topology_matches_go_inbox`; [rabbitmq-queues.md](../runbooks/rabbitmq-queues.md) | **một phần**: cảnh báo Admin mới là metric |

## 6. Cổng kiến trúc G2

| Mã | Nội dung | Kiểm thử | Trạng thái |
|---|---|---|---|
| A-1 | Thiết kế chi tiết `mkg/*` chứng minh bảy ràng buộc | [docs/design/mkg.md](../design/mkg.md) §6 (bảng RB-1..7 theo từng module, đối chiếu R-xx), §8 câu hỏi Q-MKG-01..11 | **đạt** (tài liệu). Hiện thực: `mkg/hub` (A-2) |
| A-2 | `mkg/hub`: mẻ ghi thành phần đầu vào; lô N nguồn với tỷ lệ Σ = 1 kể cả N = 1; số tầng biến đổi là dữ liệu | `TestRatiosFromMassesSumExactlyOne`, `TestLotRatioInvariant` (app + CHECK DB), `TestTransformLossProvenanceAndInventory`, `TestTotalsLossNonNegative`, `TestDispatchByDegradationRisk`, `TestRiskRankingNotFIFO` | **đạt** |
| A-3 | Hợp đồng van công bố đầu tiên `hub_facility` v1, `hub_capacity` v1 | `TestFacilityMasterDataApprovalAndValve`, `TestCapacitySnapshotValveOnlyContractFields` (chỉ trường hợp đồng, băm SHA-256 khớp byte), `TestValveRuntimeDefaultDeny`, `TestHubValveConsumerValidatesContractAndIsIdempotent` (phía pf kiểm lại lúc chạy) | **đạt**. Người đề xuất và phê duyệt thật của hợp đồng do MKG điền khi ký (Q-MKG-08) |
| A-4 | Siết `contractcheck` và `archlint` cho vùng mkg | `TestPublicationDefaultDeny`, `TestPublicationRequiresApproval`, `TestTestdataBad`, `TestTestdataOK`, `TestRepositoryAnnotationsAllSeen`, `TestRepositoryContractsPass`; archlint `TestBoundary` (R-03 ở gốc lắp ráp), `TestNoFloat` | **đạt** |
| A-5 | `pf/waterway`: đồ thị pgRouting có phiên bản; tuyến nháp không vào đồ thị; truy vấn trên bản sao đọc | `TestGISUSWater01BuildPublishRoute` (202 + job, giao lặp an toàn, tuyến nháp không vào đồ thị, `waterway_publisher` khác người dựng), `TestVersionPinningAfterNewPublish` (một phiên bản hiệu lực, ghim bản cũ vẫn định tuyến), `TestBuildDeterministic`, `TestBuildSnap`, `TestBuildCrossingIsNoded`, `TestBuildGaps`, `TestTenantIsolation` | **đạt**. Bản sao đọc là cấu hình (`Read` pool; nil → pool chính, giao dịch `ReadOnly`) |
| A-6 | `pf/simulation` theo mẫu mục 12 (202 + job, hàng đợi, kết quả idempotent, Internal API mTLS) | `TestSimulationAsyncFlowEndToEnd`, `TestSubmitIdempotentReplay`, `TestResultPostingIsIdempotent`, `TestOptimizerFailureIsRecorded`, `TestMissingSupplySourceFailsJob`, `TestInternalAPIRequiresMTLS`, `TestInternalListenerRequiresClientCert`, `TestInternalListenerRejectsWrongCN`, `TestInternalListenerBodyLimit`; pytest `test_ack_only_after_result_posted`, `test_replayed_result_is_acked`, `test_worker_refuses_tampered_snapshot` | **đạt** |
| A-7 | Ảnh chụp đầu vào bất biến, có băm | `TestSnapshotIsImmutableAndHashed` (UPDATE/DELETE bị chặn cả với quản trị; băm = sha256 byte Internal API trả), `TestSnapshotCanonicalHashSurvivesJSONBRoundTrip`; pytest `test_snapshot_sha256_is_over_received_bytes`, `test_same_snapshot_gives_identical_bytes` | **đạt** |
| A-8 | Ảnh chụp không định danh (RB-7: optimizer chỉ nhận dữ liệu không định danh) | `TestSnapshotIsDeidentifiedAndCarriesRoutingResults` (chỉ mã tham chiếu, không tên, không toạ độ); pytest `test_rb7_identifying_fields_are_rejected`, `test_result_contains_no_names_or_coordinates` | **đạt** |
| A-9 | Ảnh chụp có phiên bản đồ thị, **kết quả định tuyến đã dùng** lưu bền vững, `algorithm_version` | `TestWaterwayRouterPinsVersionAndLabels`, `TestSnapshotIsDeidentifiedAndCarriesRoutingResults` (định tuyến kèm nguồn), `TestSupplySourceFromCropcyclePartyGeo`, `TestExampleSnapshotIsUpToDate`; pytest `test_golden_snapshots_match_input_schema`, `test_platform_example_snapshot_is_accepted` (`algorithm_version`, `network_version` bắt buộc trong `snapshot.v1.schema.json`) | **đạt** |
| A-10 | Optimizer tái lập engine của prototype | pytest `test_golden_scenario_result_matches_prototype`, `test_golden_harvest_calendar_matches_prototype`, `test_golden_sensitivity_matches_prototype`, `test_js_rounding_and_summation_match_v8`, `test_fn05_ac03_each_cooperative_counted_once`, `test_fn11_ac01_ac02_cost_per_ton_is_total_over_delivered`, `test_flow_hub_inventory_never_exceeds_capacity_and_mass_balance`; `TestCompareScenarios`, `TestCompareRules`, `TestPlanRoutesRadiusAndLegs` | **đạt** (5 ca golden) |
| A-11 | Bộ tham số mô phỏng chính thức, "Chính thức" chỉ khi mọi tham số đã duyệt (CGH FN-01 BR-04) | `TestCatalogIntegrityFN01AC03`, `TestResolveParamsApproval`, `TestOfficialOnlyWhenAllParamsApproved`; pytest `test_fn01_br04_official_only_when_all_params_approved`, `test_select_hubs_respects_max_hubs_and_reevaluates`; WEB-E2E `simulation.spec.ts` | **một phần**: giá trị 57 tham số chờ khách; MILP thử nghiệm |
| RB-1 | Chỉ ghi thêm, sửa bằng bản ghi bù | `TestFarmLogCorrection_HTX_US_LOG_01_AC3`, `TestSeasonCorrectionAndCancel`, `TestCoopBoundaryLedger_GIS_US_BND_02_03`, `TestGISUSWater03ReviseByCompensatingRecord`, `TestMovementsImmutableCorrectionByCompensation`, `TestDetachHubIsCompensatingRecord`, `TestSnapshotIsImmutableAndHashed`, `TestGpsFixLog_HTX_US_GPS_03` | **đạt** |
| RB-2 | Cách ly tenant | `TestTenantIsolationAndHierarchyScope` (cropcycle), `TestG2TenantIsolation` (geo), `TestTenantIsolation` của waterway, simulation (kể cả Internal API), mkg/hub; `TestReportData_Scope_HTX_US_DASH_03`; `TestFarmerDevicePullsOwnHouseholdOnly` (cùng `sub` ở tenant khác không thấy gì) | **đạt**. Dữ liệu HTX thuộc tenant chương trình (giả định ADR-0009, chờ khách) |
| RB-3 | PII xóa được, chỉ ở vault | `TestNotePII` (ghi chú nhật ký), `TestNoPIIColumns` (accounts, party), `TestHouseholdEventsKNUSHTX05` (sự kiện không PII), `TestActivationTokenNeverLoggedOrStored`, `TestHouseholdPubKeysFarmer`, `TestSnapshotIsDeidentifiedAndCarriesRoutingResults`; G1 `TestErasure`, `TestRB3ErasureEvidenceUnreadable` | **đạt** |
| RB-4 | Giấy phép | CI `go-licenses`, `license-checker` (npm, có `qrcode-generator` MIT), `pip-licenses` (job `optimizer`) | **một phần**: ADR-0016 chưa ký |
| RB-5 | Ranh giới module, chuyển giai đoạn không sửa mã | archlint `TestBoundary` (R-02, R-03 cả ở `cmd/api-platform` ↔ mkg và `cmd/api-internal` ↔ platform); `TestBuildWiresG2Consumers` | **đạt** |
| RB-6 | Đối soát chéo số đo ở biên | — | **ngoài G2 → G3** |
| RB-7 | Dữ liệu trong lãnh thổ | `TestSnapshotIsDeidentifiedAndCarriesRoutingResults`; WEB-E2E `gis-map.spec.ts` (không gọi dịch vụ ngoài; nền bản đồ chỉ từ biến môi trường); `TestPushDataOnlyAndPerDeviceFailures` (FCM chỉ dữ liệu); G1 `TestProductionRequiresAllowlist` | **đạt**. Không dùng nhà cung cấp ngoài lãnh thổ nào. Thời tiết, độ mặn, tile vệ tinh để trống tới khi khách chọn |
| R-03 | mkg ↔ pf chỉ qua van công bố | archlint `TestBoundary`; `TestHubValveConsumerValidatesContractAndIsIdempotent` | **đạt** |
| R-04, R-14 | PII chỉ ở vault; không PII trong log | `TestNotePII`, `TestSupportRequestValidationAndReplay` (mô tả chứa SĐT/CCCD bị chặn), `TestCoopStaffDirectoryHTXUSSUPPORT02` (SĐT đọc lúc bấm, `no-store`), archlint `TestPIILog` | **đạt** |
| R-10 | Decimal cho số đo | archlint `TestNoFloat` (cả `mkg/hub`), `TestXLSX_HTXProduction_HTX_US_DASH_02`, `TestLotRatioInvariant`; app: `domain_test.dart` | **đạt** |
| R-22 | Dựng lại bảng chiếu cho cùng kết quả | `TestRebuildProjections_R22` | **đạt** |
| R-51 | Idempotency-Key | `TestOpenSeasonFarmer_HTX_US_SEASON_01`, `TestG2IdempotentReplay`, `TestIdempotentPostReplay` (waterway), `TestSubmitIdempotentReplay`, `TestIdempotentReceiptReplay` (mkg, cả khi mất khóa), `TestCatalogImportPerRowErrors` | **đạt** |
| R-52 | Việc dài: 202 + job | `TestGISUSWater01BuildPublishRoute`, `TestSimulationAsyncFlowEndToEnd`, `TestHTXProductionExport_HTX_US_DASH_02` | **đạt** |
| R-56 | Người duyệt ≠ người đặt | `TestSOPAuthoringAndPublish_HTX_US_CAT_01`, `TestFarmLogReview_HTX_US_LOG_05`, `TestGISUSWater01BuildPublishRoute`, `TestFacilityMasterDataApprovalAndValve`, `TestPublicationRequiresApproval`; WEB-E2E `sop.spec.ts`, `waterway.spec.ts`, `htx-review.spec.ts` | **đạt** |
| R-61, R-62 | Hiệu quả đúng một lần; DLQ | `TestCGHFN07AreaSyncConsumerIdempotentAndRespectsLock`, `TestGpsLogConsumerAfterCommit_HTX_US_GPS_03`, `TestFarmLogNotificationsHTXUSLOG05` (giao lặp không trùng), `TestResultPostingIsIdempotent`, `TestWorkerConsumersDeclaredByPublisher`; pytest `test_permanent_errors_go_to_dlq` | **đạt** |
| R-64 | Hợp đồng sự kiện | `go run ./tools/contractcheck`, `TestEventStructMustMatchSchema`, `TestEventFieldTypeMustMatchSchema` | **đạt** |
| Mục 11 | Internal API chỉ qua mTLS (cổng 8084, không publish) | `TestInternalListenerRequiresClientCert`, `TestInternalListenerRejectsWrongCN`, `TestInternalConfigFromEnv`, `TestInternalAPIRequiresMTLS` | **đạt** |
| C-3 | Ngưỡng k của van dữ liệu công | — | **một phần**: chờ MKG; chưa có van dữ liệu công |
| C-4 | Bảng dung sai cân, trạng thái ẩm khi cân | — | **ngoài G2 → G3** (`mkg/measurement`) |

## 7. Giới hạn đã biết của bộ kiểm thử

- **CI chưa chạy cho G2** (GitHub Actions bị chặn do thanh toán). Riêng emulator App HTX và `compose-smoke` bản G2 (api-internal, optimizer, mTLS) mới chỉ có trong workflow.
- **Keycloak.** Tài khoản nông dân và liên kết kích hoạt được kiểm bằng Keycloak giả trong bộ nhớ (`keycloak.Memory`, harness e2e). Keycloak thật cần phần mở rộng realm cho `AG_KC_ACTION_LINK_URL`; phần mở rộng này chưa có.
- **Web E2E** chạy trên backend giả lập theo hợp đồng (`src/api/mock/`). Quy tắc nghiệp vụ do kiểm thử Go chứng minh.
- **App HTX** kiểm bằng widget test và máy chủ giả (`FakeServer`). `integration_test/htx_flow_test.dart` dùng máy chủ API giả trong tiến trình; phía máy chủ của cùng luồng do `TestFarmerDevicePullsOwnHouseholdOnly` và các kiểm thử cropcycle/extension chứng minh.
- **Optimizer ↔ nền tảng.** Kiểm bằng hợp đồng JSON Schema (`contracts/optimizer`) ở cả hai phía và broker giả trong pytest. Đường RabbitMQ thật tới optimizer chỉ có ở `compose-smoke`.
- **Hiệu năng** (GIS ≤ 1,5 s cho 1.000 polygon, TMS ≤ 500 ms) chưa đo.
- **Dữ liệu thật** (thủy hệ VIWA, địa chính VN-2000, tile, thời tiết, bộ 57 tham số) chưa có. Kiểm thử dùng dữ liệu tổng hợp.
