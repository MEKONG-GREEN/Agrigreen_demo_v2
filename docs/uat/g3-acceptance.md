# Nghiệm thu cổng G3: yêu cầu → kiểm thử

Lập ngày 30/09/2026 trên `main` tại `371175c` (sau mọi nhánh G3-2), thêm kịch bản đầu–cuối `TestG3FieldToCustomer` (`tests/e2e/g3_scenario_test.go`). Mỗi yêu cầu có đúng một dòng. Tên kiểm thử trong bảng đều đã tra trong mã. Khi lập bảng, kiểm thử Go được chạy lại trên máy dev (PostgreSQL 17 + PostGIS + pgRouting, cụm `pf`, `mkg`, `ts`): `go test -count=1 -p 2 ./...`, **tất cả đạt**. Kiểm thử Flutter (App HTX, App hiện trường MKG), Vitest và Playwright (web-ops, web-internal) **không chạy lại trong lần rà này**; chúng được liệt kê theo tên tệp và tên ca trong mã nguồn.

Yêu cầu lấy từ:
- [docs/roadmap.md](../roadmap.md), mục G3 (G3-0, G3-1, G3-2), và [g3-conventions.md](../dev/g3-conventions.md);
- kiến trúc v3.0: Phụ lục A dòng G3, mục 13 (đo lường, ba điểm cân), 14 (agent biên), 16 (chỉ số, MRV), 17 (vùng), 19 (quan trắc, cảnh báo), 21 (R-95 phần số đo), RB-1..RB-7 mà G3 chạm tới;
- các dòng **"ngoài G2 → G3"** của [g2-acceptance.md](g2-acceptance.md);
- **Backlog App Khuyến nông**: KN US-MRV-01..06; **BRD Khuyến nông**: KN FR-07, FR-08;
- **BRD GIS v1.3**: FN-18..21 (GIS US-EVL-01, US-SNAP-01, US-REPLAY-01, US-RETAIN-01);
- quy tắc của prototype `mekong-green` (`src/erp/field/service.ts` FM-01..10, cân nhà máy) — prototype là đặc tả chạy được, không chép mã;
- quyết định: [ADR-0017](../adr/0017-kho-chuoi-thoi-gian.md), [ADR-0018](../adr/0018-su-kien-noi-bo-mkg.md), [ADR-0019](../adr/0019-dinh-danh-thiet-bi-va-dong-ho.md), [docs/design/mkg.md](../design/mkg.md).

Ba trạng thái:
- **đạt**: có kiểm thử tự động chứng minh tiêu chí chấp nhận. Cột kiểm thử liệt kê kiểm thử Go (`Test…`), bước của kịch bản **E2E-G3-xx** (mục 2), **WEB-E2E** `*.spec.ts` (Playwright của `apps/web-ops` hoặc `apps/web-internal`), Vitest, kiểm thử Flutter (`apps/htx-app/test`, `apps/mkg-field-app/test`, `integration_test/`). Khi tham số là **tạm, chờ khách**, dòng vẫn **đạt** nếu cơ chế (phiên bản, người đặt ≠ người duyệt, ghim `rule_version`) đã chứng minh; câu hỏi của khách nằm ở dòng riêng;
- **một phần**: ghi rõ phần còn thiếu và loại — **mã**, **chờ khách**, **hạ tầng**. Hạng mục **chưa có mã** cũng xếp vào đây và ghi "chưa làm";
- **ngoài G3 → G4/…**: chỉ dùng cho hạng mục mà kiến trúc hoặc lộ trình đặt ở cổng sau.

## Tóm tắt

| Mục | Số yêu cầu | đạt | một phần | ngoài G3 → G4/… |
|---|---|---|---|---|
| 3. Phụ lục A (G3) | 3 | 2 | 1 | 0 |
| 4. Chuyển từ G2 và GIS FN-18..21 | 7 | 5 | 2 | 0 |
| 5. Mục 13 — đo lường, ba điểm cân | 9 | 8 | 1 | 0 |
| 6. Mục 14 — agent biên | 6 | 6 | 0 | 0 |
| 7. Cổng kiến trúc G3 (mục 16, 17, 19; RB; ADR) | 22 | 18 | 4 | 0 |
| 8. MRV (KN US-MRV-01..06, FR-07, FR-08) | 8 | 5 | 3 | 0 |
| 9. Quy tắc prototype (FM-01..10, cân nhà máy) | 12 | 10 | 0 | 2 |
| 10. Lộ trình G3-2 (client, liên thông) | 8 | 7 | 1 | 0 |
| 11. Câu hỏi khách và hạ tầng còn lại | 3 | 0 | 3 | 0 |
| **Tổng** | **78** | **61** | **15** | **2** |

**Ghi chú môi trường (không tính theo dòng):** CI trên GitHub **chưa được xác nhận** cho các commit G3, vì GitHub Actions vẫn bị chặn do lỗi thanh toán. Các hạng mục chỉ kiểm được trên runner **chưa được xác minh**:
- `compose-smoke` bản G3: dịch vụ `ingest` + agent mô phỏng `edge-sim`, `scripts/dev-edge-bootstrap.sh` (đề xuất + duyệt thiết bị hai người qua Admin API), lô tới `ts_weigh.reading_batch`, consumer trên vhost `mkg.internal`, tài khoản nền tảng **không** có quyền trên `mkg.internal`;
- job `test` qua **PgBouncer** chế độ transaction cho các bảng G3 (R-32). Máy dev chỉ mô phỏng chế độ thực thi của PgBouncer (`QueryExecModeExec`); các kiểm thử rò tenant song song (`Test…TenantLeakParallel`) chạy trên máy dev nhưng chưa qua PgBouncer thật;
- kiểm thử App hiện trường MKG trên **emulator** (`apps/mkg-field-app/integration_test/mkg_flow_test.dart`) và App HTX;
- job `images` (Docker `ingest`, `worker-internal`, `web-internal`), job `edge-agent` (nhị phân tĩnh linux/amd64 + arm64, kèm SHA-256), `promtool check rules` cho `deploy/observability/`.

Trạng thái trong bảng dựa trên lần chạy kiểm thử ở máy dev.

**Cập nhật 30/09/2026 (bù khoảng trống fieldops, mkg 000012):** proto FM-01, FM-02, FM-06, FM-07, FM-08 chuyển từ "một phần" sang "đạt" (mục 9); kiểm thử Go chạy lại trên máy dev, tất cả đạt.

**15 dòng "một phần"**: phần còn thiếu và loại của từng dòng.

| Yêu cầu | Còn thiếu | Loại |
|---|---|---|
| A-3 Agent biên theo mẫu cân đã chốt | Agent, WAL, ký lô, mTLS, bộ điều hợp `modbus-tcp`/`serial-ascii` và bộ mô phỏng đã có. **Mẫu cân và giao thức chưa chốt (C-6, hạn Q1/2027, có thời gian dẫn phần cứng)**; thuật toán tích phân `weigh.integrate.v1` là tạm (trung vị số đo ổn định − bì) | chờ khách |
| GIS US-RETAIN-01 (FN-21) chính sách lưu | Nhật ký lớp và ảnh chụp chỉ ghi thêm; "dọn" chỉ được là chuyển tầng (RB-1, X1). **Số ngày lưu, nén, lưu trữ chưa chốt** (prototype HTML: 365 ngày sự kiện, 90 ngày ảnh chụp); chưa có job chuyển tầng | chờ khách |
| C-4 Bảng dung sai cân, trạng thái ẩm | Dung sai là tham số có phiên bản (người đặt ≠ người duyệt), mặc định prototype: cờ > 5 %, nghiêm trọng > 15 %. Phiếu ghi `moisture_state` (`as_received`/`not_measured`) nhưng **không khấu trừ ẩm**: bảng dung sai thật và quy tắc ẩm chờ khách | chờ khách |
| R-71 Số đo thô không xóa — tầng lạnh | Chỉ ghi thêm (trigger chặn cả owner), phân vùng tháng đã có. **Cửa sổ kiểm toán chưa có (C-7)**; xuất tầng lạnh có ký mới là runbook thủ công hai người ([ingest.md](../runbooks/ingest.md) §8), chưa có job | chờ khách + hạ tầng |
| Mục 16 — ước tính tCO2e chính thức | Ước tính luôn **TẠM**; chỉ chính thức khi phương pháp được duyệt bởi người khác và ghim tham số đã duyệt. **Phạm vi, phương pháp tCO2e (baseline, leakage, chất khô) và danh mục trường MRV chính thức (C-1) chờ khách**; hệ số 1460 kgCO2e/t, 0,062 / 0,021 kgCO2e/t·km là tạm | chờ khách |
| RB-2 Cấp quyền chia sẻ nền tảng → MKG | Tài khoản dịch vụ tenant MKG gọi Application API công khai (client credentials) để đọc HTX/thửa và luồng phản hồi HTX. **Chưa có cấp quyền chia sẻ tenant chương trình → tenant MKG (Q-MKG-12)**: kịch bản E2E dùng một tenant cho cả hai vùng | chờ khách |
| Mục 19 — luật cảnh báo Prometheus | `deploy/observability/prometheus/agrigreen-mkg-ingest.rules.yml` (đệm agent, ingest từ chối/khoảng trống/lệch đồng hồ/kẹt tích phân, DLQ `mkg.internal`). **`promtool check rules` chỉ chạy ở CI**, chưa xác minh; chưa nối Alertmanager | hạ tầng |
| Mục 19 — cảnh báo nghiệp vụ theo SQL | Collector `deploy/observability/sql-exporter/mkg-measurement.collector.yml` (tỷ lệ phiếu vượt dung sai, tranh chấp mở lâu) là đặc tả: **cần vai giám sát chỉ đọc xuyên tenant `mkg_monitor`/`ts_monitor`** (hoặc view SECURITY DEFINER) — chưa cấp | hạ tầng |
| KN US-MRV-04 Ghi nhận sâu bệnh/thiên tai | Báo cáo sâu bệnh có ở `pf/extension` (G1, FR-02). **Chưa làm** nhãn "Cảnh báo khẩn" tự bật khi vượt ngưỡng; ngưỡng nguy hiểm chưa có | mã + chờ khách |
| KN US-MRV-06 Leo thang hồ sơ quá hạn | **Chưa làm** job leo thang hồ sơ MRV chờ duyệt quá hạn (leo thang SLA hiện chỉ có cho nhiệm vụ `pf/extension`); thời hạn xử lý theo BRD chưa có số | mã + chờ khách |
| KN FR-08 Cảnh báo bất thường nhật ký HTX | Cờ "không có bản ghi mới N ngày khi vụ đang mở" đã đạt. **Chưa làm** cờ "ghi AWD nhưng vùng chưa đủ hạ tầng" (chưa có dữ liệu hạ tầng thủy lợi) và thông báo tự động tới KNV phụ trách | mã + chờ khách |
| B7 Compose và CI cho ingest + edge-sim | Dịch vụ, bootstrap và job đã có trong Compose/workflow. **Chưa chạy trên runner** (GitHub Actions bị chặn) | hạ tầng |
| C-5 Đường nhận số cân của khách | G3 nhận bằng **nhập tay kèm bằng chứng** (`POST /v1/measurement/lots/{id}/tickets`, điểm `CUSTOMER_IN`) và **nhập lô** (`POST /v1/measurement/customer-weight-imports`, kênh `api`). **Tích hợp hệ cân của khách chờ C-5** | chờ khách |
| Định mức hao hụt sấy (HUB_IN → HUB_OUT) | Định mức là tham số có phiên bản, tách hao hụt dự kiến và bất thường; **số định mức thật chưa có** (đang là mặc định tạm) | chờ khách |
| CA thiết bị riêng | Dev dùng chung CA nội bộ với Internal API (`scripts/gen-dev-certs`); production cần **CA thiết bị riêng** + quy trình ký CSR và thu hồi ([ingest.md](../runbooks/ingest.md)) — chưa dựng | hạ tầng |

**2 dòng "ngoài G3"**:
- **proto FM-05** (mỗi lượt xuống ghe sinh một chuyến vận tải đường thủy): `mkg/logistics`, G4;
- **Phiếu nhận hàng ↔ đơn mua, cảnh báo > 3 %** (prototype, chỉ cảnh báo): `mkg/orders`, G4.

## 1. Cách chạy

```powershell
. .\scripts\dev-env.ps1
go test -count=1 -p 2 -timeout 60m ./...                    # gồm tests/e2e (TestG1DongXuanCanTho, TestG3FieldToCustomer)
go test -count=1 -run TestG3FieldToCustomer -v ./tests/e2e/  # chỉ kịch bản G3 (≈ 15 s sau khi dựng DB)
go run ./tools/archlint ./... ; go run ./tools/migratecheck migrations ; go run ./tools/contractcheck
```

`pgtest` tự dựng ba database mỗi gói (`ag_pf_*`, `ag_mkg_*`, `ag_ts_*`). Nếu chạy song song mọi gói mà gặp `out of shared memory` (SQLSTATE 53200), đặt `max_locks_per_transaction=256` hoặc chạy lại gói lỗi riêng (g3-conventions §7).

Web vận hành (`apps/web-ops`) và web nội bộ MKG (`apps/web-internal`), backend giả lập theo hợp đồng:

```powershell
cd apps/web-ops; npm ci; npm test; $env:CI='1'; npm run e2e       # G3: metrics-history, gis-replay, mrv, htx-collection
cd apps/web-internal; npm ci; npm test; $env:CI='1'; npm run e2e  # measurement, fieldops, hub, sod, auth
```

Flutter: `flutter test` trong `apps/htx-app` (`collection_test.dart`) và `apps/mkg-field-app` (`domain_test.dart`, `field_service_test.dart`, `http_mkg_sync_test.dart`, `app_flow_test.dart`).

## 2. Kịch bản đầu–cuối `TestG3FieldToCustomer` — "từ ruộng tới nhà máy khách"

Dựng trong tiến trình, trên PostgreSQL thật, bằng **chính gốc lắp ráp** của các tiến trình thật:
- nền tảng: `tests/e2e/harness` (`cmd/api-platform/assembly`, kho định danh thật, issuer OIDC giả);
- MKG: `cmd/api-internal/assembly` (hub + measurement + fieldops, adapter `mkgwire`). mkg/fieldops đọc HTX/thửa và luồng phản hồi HTX qua **Application API công khai** của nền tảng bằng tài khoản dịch vụ (endpoint client credentials giả);
- ingest: `measurement.IngestReceiver` sau listener TLS cấu hình bằng `measurement.IngestServerTLS` như `cmd/ingest` (`RequireAndVerifyClientCert`), CA thiết bị kiểu `scripts/gen-dev-certs` (CA → chứng thư máy chủ + chứng thư client có SAN = mã thiết bị); vòng tích phân `IngestLoop.RunOnce`;
- thay outbox-publisher + RabbitMQ: một "bơm" đọc `shared.outbox` của hai cụm theo thứ tự id. Sự kiện `kind = internal` → consumer của `cmd/worker-internal/assembly`; van MKG và sự kiện pf → consumer liên module của `cmd/api-platform/assembly` (như `cmd/worker`). Mỗi consumer chạy qua `shared.inbox` trên vai DB của module nhận (R-61).

Số liệu golden của prototype: 5 800 → 5 700 → 4 400 cuộn; HUB_IN 138 400 − 41 600 = 96 800 kg so với 4 400 × 20 = 88 000 kg → +10 % cờ; HUB_OUT 95 348 kg (hao hụt 1,5 %); khách cân 86 767 kg (−9 %).

| Bước | Nội dung | Kiểm |
|---|---|---|
| E2E-G3-01 | Nền tảng: dữ liệu chủ, cây tổ chức, HTX Ninh Kiều, nông hộ (vault), 2 thửa (dùng lại bước G1); SOP soạn ≠ phát hành; quản lý HTX mở vụ cho thửa 1 | nhật ký lớp GIS nhận `plot.created` + `season.opened` |
| E2E-G3-02 | api-internal + ingest mTLS; tài khoản dịch vụ tenant MKG | `/readyz` vùng mkg |
| E2E-G3-03 | Cơ sở hub và hai cân (HUB_IN, HUB_OUT) trong sổ thiết bị: khóa Ed25519 + SHA-256 chứng thư | tự duyệt bị chặn (R-56); lô ký sai khóa bị ingest từ chối |
| E2E-G3-04 | Lệnh gom: người lập ≠ người duyệt | thửa không có trên nền tảng bị từ chối; tự duyệt bị chặn |
| E2E-G3-05 | Đếm cuộn bale → gather → boat_load (proto FM-04) | ít hơn mà không có lý do → 422, không ghi |
| E2E-G3-06 | Van `htx_collection_statement` + `collection_schedule` sang nền tảng; `roll_count.recorded` → hub, measurement | quản lý HTX thấy bảng kê 4 400 cuộn; không có khối lượng, kg/cuộn, giá |
| E2E-G3-07 | Quản lý HTX xác nhận trên nền tảng → job MKG kéo phản hồi (RB-6 chiều mua) | 1 xác nhận kênh `platform`; kéo lặp không ghi trùng; đối soát `match` |
| E2E-G3-08 | Phiên cân HUB_IN: 9 số đo ký Ed25519 + bộ đếm qua mTLS (có phát lại) → **chỉ ingest** tích phân phiếu (R-70) | phiếu 96 800,000 kg; liên kết lô ↔ lệnh gom; `ts_weigh.raw_reading` đúng 9 dòng |
| E2E-G3-09 | Nhận hub từ phiếu (Q-MKG-04), nguồn = thửa 1 | lô hub 96 800 kg theo phiếu; phiếu dùng hai lần → `hub.weigh_ticket_used` |
| E2E-G3-10 | Xuất hub + cân HUB_OUT qua ingest | phiếu 95 348 kg gắn phiếu xuất (`dispatch_linked`) |
| E2E-G3-11 | CUSTOMER_IN nhập tay kèm bằng chứng (C-5 tạm), vượt dung sai (RB-6 chiều bán) | thiếu bằng chứng → 422; HUB_IN tay khi đã có phiếu → 422; hao hụt nội bộ 1 452 kg; cuộn ↔ khối lượng +10,00 % cờ; mở tranh chấp; hub nhận kết quả đối soát |
| E2E-G3-12 | Van `mrv_collection_evidence` → `pf/mrv` | bậc `10000+`, căn cứ HUB_IN, gắn vụ; không khối lượng chính xác/phiếu/khách; độ đầy đủ `collection_evidence present`; ước tính không chính thức |
| E2E-G3-13 | Job `metrics_snapshot` ngày D−1; ghi sổ ngày D chưa kết thúc bị từ chối ở app **và** DB; `AsOf(D)` = 2 thửa; phát lại GIS ngày D = ảnh chụp D−1 + sự kiện sau watermark | thửa 1 thuộc HTX Ninh Kiều, `active`; bằng dựng thẳng từ nhật ký; ngày D−1 rỗng |
| E2E-G3-14 | RB-1, R-71: 17 sổ của cả ba cụm | UPDATE/DELETE bị chặn (vai ứng dụng; `ingest_ts` với ts; owner với `raw_reading`, `weigh_ticket`) |
| E2E-G3-15 | R-61, RB-2, RB-3 | giao lặp mọi sự kiện → inbox bỏ qua; tenant khác không thấy lệnh gom, phiếu, tranh chấp, lô hub, bảng kê, MRV; không PII trong outbox MKG và log |

R-03 (mkg ↔ pf không import nhau) do `archlint` (`TestBoundary`) và `TestNoPlatformDependency` kiểm; kịch bản chỉ nối hai vùng qua van và Application API.

## 3. Phụ lục A (G3)

| Mã | Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|---|
| A-1 | `pf/metrics` có lịch sử | `TestKNDash02RecordIdempotentAndRecomputeSupersedes`, `TestKNDash01MetricsAsOfExcludesLateRecords`, `TestRecordDayNotClosed`, `TestDefinitionPinDetectsEditedFile`, `TestMetricsScopeTenantIsolationAndRoles`, `TestCGHFN10SeasonDimension`, `TestCGHFN11MetricsAsOfLatestStoredRun`, `TestMetricsSeasonAreaYieldAsOf`, `TestMetricsPlotsDigitisedAsOf`, E2E-G3-13; WEB-E2E `metrics-history.spec.ts`; Vitest `metrics.test.ts` | **đạt** |
| A-2 | `mkg/measurement` có đường nhận số liệu cân của khách | `TestHubReconciliationDisputeLifecycle`, `TestCustomerWeightImportIdempotent`, `TestNormalizeDraftRules`, `TestMKGSeedEndToEnd` (kênh `api`), E2E-G3-11; WEB-E2E `measurement.spec.ts` (web-internal) | **đạt**. Đường G3: nhập tay kèm bằng chứng + nhập lô; tích hợp hệ cân khách là C-5 (mục 11) |
| A-3 | Agent biên theo mẫu cân đã chốt | `TestModbusTCPReadsWeightAndStable`, `TestModbusSoftwareStability`, `TestSerialASCIIOverTCPBridge`, `TestNormalizeKg`, `TestSimulatorDeterministicCycles`, `TestAgentEndToEndSimulatorToIngest`, `TestIntegrateV1Golden` | **một phần**: mẫu cân chưa chốt (C-6); `weigh.integrate.v1` tạm |

## 4. Chuyển từ G2 và GIS FN-18..21

| Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|
| GIS US-EVL-01 (FN-18) nhật ký thay đổi lớp, trước/sau + nguồn | `TestGISLayerLogSnapshotReplay`, `TestGISPlotRetireRestoreLayerLog` (ngừng dùng/khôi phục, giao lặp không ghi thêm), `TestPlotRetireRestoreEmitEvents`, `TestLayerLogQueueBindsPlotRetireRestore`, E2E-G3-01; WEB-E2E `gis-replay.spec.ts` (bảng trước/sau) | **đạt** |
| GIS US-SNAP-01 (FN-19) ảnh chụp cuối ngày + watermark | `TestGISLayerLogSnapshotReplay`, `TestFoldSnapshotPlusTailEqualsFull`, `TestGISPlotRetireRestoreLayerLog`, E2E-G3-13 | **đạt**. Lớp thời tiết ("giữ cảnh báo lớn nhất ngày") chưa có nguồn (RB-7) |
| GIS US-REPLAY-01 (FN-20) xem lại mọi lớp theo ngày | `TestGISLayerLogSnapshotReplay` (phát lại = ảnh chụp + sự kiện sau watermark = dựng thẳng), `TestGISPlotRetireRestoreLayerLog`, E2E-G3-13; WEB-E2E `gis-replay.spec.ts` (ngày tương lai bị chặn, vai không đủ không gọi) | **đạt** |
| GIS US-RETAIN-01 (FN-21) chính sách lưu và dọn | `TestGISLayerLogSnapshotReplay`, E2E-G3-14 (`pf_metrics.layer_event`, `daily_value` không sửa/xóa được) | **một phần**: số ngày lưu chờ khách; chưa có job chuyển tầng |
| RB-6 đối soát chéo số đo | chiều mua: `TestHTXResponsesFromPlatform_RB6_BuySide`, `TestHTXRespondStatement_RB6_BuySide`, `TestBuySideReconciliation`, `TestCheckResponse_RB6_BuySide`, `TestMKGSyncResponseFeed_RB6_RB2`, `TestCollectionNotifications_RB6_BuySide`; chiều bán: `TestHubReconciliationDisputeLifecycle`, `TestGoldenHubRouteInternalAndCommercial`, `TestDisputeLifecycleAndBucket`; cuộn ↔ khối lượng: `TestGoldenPrototypeSeedRollVsMass`, `TestCollectionLinkDrivesRollCheck`, `TestRollCountConsumerReconciles`; E2E-G3-07, E2E-G3-11 | **đạt** |
| R-95 phần số đo (cân, cuộn, độ ẩm) | `TestMKGPlanDeterministic`, `TestMKGPlanGoldenPrototypeLot`, `TestMKGPlanShares`, `TestMKGPlanBatchesVerify`, `TestMKGRefusesProduction`, `TestMKGSeedEndToEnd` (api-internal + ingest thật, chạy lại không ghi mới) | **đạt** (`cmd/seed-synthetic -plan mkg`) |
| C-4 bảng dung sai cân, trạng thái ẩm khi cân | `TestToleranceVersionsSoDAndEventTime`, `TestGoldenPrototypeSeedRollVsMass`, `TestNormalizeDraftRules` (`moisture_state`) | **một phần**: số thật chờ khách |

## 5. Mục 13 — đo lường, ba điểm cân

| Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|
| Ba điểm cân HUB_IN / HUB_OUT / CUSTOMER_IN; khối lượng chuẩn của lô theo điểm cân (`LotMass`) | `TestGoldenHubRouteInternalAndCommercial`, `TestHubReconciliationDisputeLifecycle`, `TestIngestEndToEndIntoTicketLedger`, `TestReconcileIncompleteAndDirect`, E2E-G3-08/10/11 | **đạt** |
| Hao hụt nội bộ HUB_IN → HUB_OUT, tách dự kiến (định mức) và bất thường | `TestGoldenHubRouteInternalAndCommercial`, `TestLossNormBands`, `TestReconciliationRollCountAndTenantIsolation` (hub quy kết theo cơ sở), E2E-G3-11 (1 452 kg) | **đạt**. Định mức tạm (mục 11) |
| Chênh thương mại HUB_OUT → CUSTOMER_IN theo dung sai có phiên bản; vượt → hồ sơ tranh chấp | `TestToleranceVersionsSoDAndEventTime` (người đặt ≠ người duyệt, theo thời điểm sự việc), `TestHubReconciliationDisputeLifecycle`, `TestDisputeLifecycleAndBucket`, E2E-G3-11; WEB-E2E `measurement.spec.ts`, `sod.spec.ts` (web-internal) | **đạt** |
| Tuyến DIRECT: không qua hub, chỉ CUSTOMER_IN | `TestDirectRouteOnlyCustomerIn`, `TestReconcileIncompleteAndDirect`, `TestMRVDirectRouteAggregated` (MRV căn cứ `CUSTOMER_IN_AGGREGATED`), `TestMKGSeedEndToEnd` | **đạt** |
| R-70 phiếu cân chỉ do ingest tích phân từ luồng thô, ghi `rule_version` + `reading_batch` đã dùng; sửa = phiếu bù | `TestIngestTicketIdempotencyCorrectionAndGrants`, `TestIngestIntegrationLoopEndToEnd`, `TestIngestEndToEndIntoTicketLedger`, `TestIntegrateV1Golden`, `TestIntegrateV1Deterministic`, `TestNormalizeDraftRules`, E2E-G3-08, E2E-G3-11 (HUB_IN tay khi đã có phiếu → 422) | **đạt** |
| R-71 số đo thô không bao giờ bị xóa | `TestTSWeighAppendOnly`, `TestTSGateDetectsMissingImmutability`, `TestTSWeighReceivedAtIsSystemTime`, `TestTSPartitionMaintenance`, E2E-G3-14 | **một phần**: cửa sổ kiểm toán C-7, tầng lạnh chỉ có runbook thủ công |
| Kiểm chéo số cuộn ↔ khối lượng (hợp đồng theo tấn; theo cuộn chỉ tham khảo) | `TestGoldenPrototypeSeedRollVsMass`, `TestCollectionLinkDrivesRollCheck`, `TestRollCountConsumerReconciles`, `TestMKGSeedEndToEnd` (`per_bale` → `informational`), `TestRollsAdapter`, `TestSharesSumToOne`, E2E-G3-11 | **đạt** |
| Sổ thiết bị: đề xuất ≠ duyệt, kỳ đăng ký, thu hồi | `TestDeviceRegistrySoDResolveEpochRevoke`, `TestIngestDeviceAuthentication`, E2E-G3-03; WEB-E2E `measurement.spec.ts`, `sod.spec.ts` | **đạt** |
| Xác nhận của HTX (mục 13.3, `htx_confirmation_ref`) | `TestBuySideReconciliation`, `TestHTXResponsesFromPlatform_RB6_BuySide`, `TestPlatformHTTPHTXResponses_RB6`, E2E-G3-07; Flutter `collection_test.dart` (App HTX); WEB-E2E `htx-collection.spec.ts` | **đạt** |

## 6. Mục 14 — agent biên

| Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|
| Đệm đĩa ≥ 72 giờ; đầy thì ngừng nhận và báo động, không ghi đè; không tiêu bộ đếm | `TestConfigBufferSizing72h`, `TestWALFullRefusesNeverOverwrites`, `TestBufferFullDoesNotConsumeCounter`, `TestWALSegmentsReadAcrossReopen`, `TestWALCrashTruncatedTail`, `TestWALTornSegmentHeader`, `TestWALCorruptMiddleSegmentRefused`, `TestWALMissingMiddleSegmentRefused`, `TestHealthzAndMetrics` (`edge_buffer_fill_ratio`) | **đạt** |
| Ký nguồn Ed25519 + bộ đếm đơn điệu; idempotent theo (thiết bị, kỳ, bộ đếm); mất đĩa → kỳ mới | `TestVectors`, `TestSignatureMatchesEdgewireVectors`, `TestIngestEdgeVectorsEndToEnd`, `TestCounterPersistsAcrossCrash`, `TestDiskLossRequiresNewEpoch`, `TestWALForeignEpochRefused`, `TestIngestReplayIdempotent`, `TestIngestConflictRegressionAndGap`, `TestPushRetryIdempotentAndAckTrim`, `TestPendingResentAfterRestart`, `TestPushRejectedRaisesAlarmKeepsBuffer`, E2E-G3-03/08 | **đạt** |
| Đồng hồ: ghi giờ thiết bị + độ lệch NTP, không dùng làm thứ tự; lệch → gắn cờ | `TestSNTPOffset`, `TestIngestClockSkewFlagged` | **đạt** |
| mTLS chỉ đi ra, chứng thư client bắt buộc | `TestMTLSRequiresClientCert`, `TestIngestMTLSListener` (không chứng thư / CA khác → thất bại), `TestIngestDeviceAuthentication`, `TestIngestBodyAndRateLimit`, E2E-G3-02/08 | **đạt** |
| Cập nhật có ký, lùi lại được | `TestUpdateVerifyApplyRollbackCommit` (chữ ký, SHA-256, phiên bản, khe A/B, lùi khi không khỏe) | **đạt**. Kho phát hành và khóa ký phát hành thật theo [edge-agent.md](../runbooks/edge-agent.md) |
| Nhị phân tĩnh, import giới hạn (ADR-0019) | archlint `TestBoundary` (`cmd/edge-agent` chỉ thư viện chuẩn, `shared/{clock,ids,mass}`, `edgewire`), `TestCLIEnrollInfoVersion`, `TestEnrollRules` | **đạt**. Build tĩnh amd64/arm64 là job CI (ghi chú môi trường) |

## 7. Cổng kiến trúc G3 (mục 16, 17, 19; RB; ADR)

| Mã | Nội dung | Kiểm thử | Trạng thái |
|---|---|---|---|
| 16-1 | Định nghĩa chỉ số khai báo có phiên bản; sửa tệp đã dùng bị phát hiện | `TestRepoDefinitionsLoad`, `TestDefinitionValidation`, `TestDefinitionPinDetectsEditedFile`; WEB-E2E `metrics-history.spec.ts` (định nghĩa chỉ xem) | **đạt** |
| 16-2 | `AsOf`/`Cutoff`: giá trị "như hệ biết lúc hết ngày D"; bản ghi muộn không đổi `AsOf` | `TestKNDash01MetricsAsOfExcludesLateRecords`, `TestMetricsSeasonAreaYieldAsOf`, `TestMetricsPlotsDigitisedAsOf`, `TestCGHFN11MetricsAsOfLatestStoredRun`, `TestInputsHashDeterministic`, E2E-G3-13 | **đạt** |
| 16-3 | Sổ giá trị theo ngày: idempotent, tính lại = bản ghi bù; chỉ ghi ngày đã kết thúc (app + CHECK DB) | `TestKNDash02RecordIdempotentAndRecomputeSupersedes`, `TestRecordDayNotClosed`, E2E-G3-13 | **đạt** |
| 16-4 | KPI theo vụ, xu hướng nhiều năm (CGH FN-09..11) | `TestCGHFN10SeasonDimension`, `TestCGHFN11MetricsAsOfLatestStoredRun`, `TestParseSeason`; WEB-E2E `metrics-history.spec.ts` (xu hướng theo vụ) | **đạt** |
| 16-5 | MRV nhận van bằng chứng thu gom: khối lượng theo bậc, không khối lượng chính xác | `TestG3CollectionValveContractAndIdempotency`, `TestMRVValveOnlyContractFields`, `TestMRVDirectRouteAggregated`, `TestBucketLowerKg`, E2E-G3-12 | **đạt** |
| 16-6 | Ước tính CO2: tạm ↔ chính thức theo phương pháp đã duyệt | `TestG3ProvisionalVsOfficialMethodApproval`, `TestMethodCoversProvisionalVsOfficial`, `TestComputeEstimateTransportLegs`, `TestComputeEstimateOnlyApprovedCountsKNUSMRV05`; WEB-E2E `mrv.spec.ts` (nhãn TẠM) | **một phần**: phương pháp tCO2e + C-1 chờ khách |
| 16-7 | Gói xuất MRV có liên kết, bằng chứng, ước tính kèm `rule_version`; băm dựng lại khớp | `TestG3ExportIncludesLinksAndEstimates`, `TestSnapshotG3SectionsOmittedKeepHash`, `TestExportEndToEnd` | **đạt** |
| 17-1 | mkg ↔ pf chỉ qua van công bố / Application API; R-03 ở gốc lắp ráp | archlint `TestBoundary`, `TestNoPlatformDependency`, `TestPlatformHTTPClientCredentials`, `TestOrderValidatesPlatformDirectory`, E2E-G3-04/06/07 | **đạt** |
| 17-2 | Sự kiện nội bộ MKG chỉ trên vhost `mkg.internal`, không bao giờ ra exchange nền tảng | `TestInternalEventNeverOnPlatformExchange`, `TestOutboxKindRoutesToOwnBroker`, `TestOutboxInternalKindMustBeMKG`, `TestConsumerRegistry`, `TestFrozenInternalSchemasExist`, `TestG32ConsumersRegistered`, `TestHubG3ConsumersRegistered`, E2E-G3-06 (bơm theo `kind`) | **đạt**. Quyền RabbitMQ thật: `compose-smoke` (ghi chú môi trường) |
| 17-3 | Van mặc định từ chối; danh sách "không bao giờ qua van" (số đo thô, phiếu, kg/cuộn, dung sai, tranh chấp, giá, khách) | `TestValidateDefaultDeny`, `TestEmbeddedContractsApproved`, `TestLegacyFieldsContract`, `TestPublicationDefaultDeny`, `TestCollectionValveDefaultDeny_RB6`, `TestValvePayloadContractFieldsOnly`, E2E-G3-06/12 | **đạt**. Người đề xuất/duyệt thật của ba hợp đồng dự thảo: MKG điền (Q-MKG-08) |
| 17-4 | RB-2 cấp quyền chia sẻ tenant chương trình → tenant MKG | `TestMKGSyncResponseFeed_RB6_RB2`, `TestPlatformHTTPClientCredentials`, `TestPlatformHTTPHTXResponses_RB6` | **một phần**: chờ khách (Q-MKG-12) |
| 19-1 | Chỉ số vận hành agent/ingest (độ đầy đệm, lô từ chối, khoảng trống bộ đếm, lệch đồng hồ, phiên kẹt) | `TestHealthzAndMetrics`, `TestIngestClockSkewFlagged`, `TestIngestIntegrationLoopEndToEnd` (báo động một lần sau ngưỡng), `TestIngestConflictRegressionAndGap`, `TestHealthcheckExitCode`, `TestOpsRouterNotConfigured` | **đạt** |
| 19-2 | Luật cảnh báo Prometheus | `deploy/observability/prometheus/agrigreen-mkg-ingest.rules.yml` (17 luật) | **một phần**: `promtool` chỉ ở CI |
| 19-3 | Cảnh báo nghiệp vụ theo SQL (vượt dung sai, tranh chấp mở lâu) | `deploy/observability/sql-exporter/mkg-measurement.collector.yml` | **một phần**: thiếu vai `mkg_monitor` |
| RB-1 | Chỉ ghi thêm, sửa bằng bản ghi bù (cả cụm ts) | E2E-G3-14 (17 sổ), `TestTSWeighAppendOnly`, `TestRollCountCorrectionByCompensation`, `TestWeighTicketConsumerIdempotentCompensating`, `TestIngestTicketIdempotencyCorrectionAndGrants`, `TestKNDash02RecordIdempotentAndRecomputeSupersedes`, `TestHTXRespondStatement_RB6_BuySide` (đổi ý = bản thay có lý do) | **đạt** |
| RB-2 | Cách ly tenant (song song, xen kẽ kết nối) | `TestFieldopsTenantLeakParallel`, `TestMeasurementTenantLeakParallel`, `TestHubTenantLeakParallel`, `TestTSWeighTenantLeakParallel`, `TestMetricsTenantLeakParallel`, `TestMRVTenantLeakParallel`, `TestIngestTenantIsolationTS`, `TestTSWeighTenantIsolation`, `TestG3TenantIsolationAndScope`, E2E-G3-15 | **đạt**. Chưa qua PgBouncer thật (ghi chú môi trường) |
| RB-3 | PII chỉ ở vault; không PII qua van, outbox, log | `TestHTXRespondStatement_RB6_BuySide` (lý do chứa SĐT bị chặn), `TestCollectionValveDefaultDeny_RB6`, `TestMKGSyncResponseFeed_RB6_RB2` (luồng không mang người phản hồi), E2E-G3-15 | **đạt** |
| RB-5 | Ranh giới module; nối qua port/ và sự kiện | archlint `TestBoundary`, `TestRollsAdapter`, `TestG32ConsumersRegistered`, `TestWorkerConsumersDeclaredByPublisher` | **đạt** |
| RB-7 | Dữ liệu trong lãnh thổ | ADR-0017 (cụm `ts` trên PostgreSQL trong lãnh thổ, không TimescaleDB TSL), tầng lạnh chỉ tới kho đối tượng trong lãnh thổ có khóa đối tượng ([ingest.md](../runbooks/ingest.md) §8); G3 không thêm nhà cung cấp ngoài | **đạt** |
| ADR-0017 | Cụm `ts`: phân vùng tháng theo `received_at`, chỉ ghi thêm, R-07, cấm timescaledb | `TestTSWeighAppendOnly`, `TestTSWeighCounterMonotonicAndIdempotent`, `TestTSWeighReceivedAtIsSystemTime`, `TestTSPartitionMaintenance`, `TestTSClusterRules`, `TestClusterDirMustBeEmbedded`, `TestTSGateDetectsMissingImmutability` | **đạt** |
| ADR-0018 | vhost `mkg.internal`, `shared.outbox.kind`, `//ag:internal-event` | `TestOutboxKindRoutesToOwnBroker`, `TestOutboxInternalKindMustBeMKG`, `TestInternalEventNeverOnPlatformExchange`, `TestValidName`, `TestConsumerRegistry` | **đạt** |
| ADR-0019 | Định danh thiết bị (mTLS + Ed25519), lô = dải bộ đếm, đồng hồ không làm thứ tự | `TestVectors`, `TestCanonicalCoversEveryField`, `TestVerifyRejectsOtherKey`, `TestMaxReadings`, `TestSchemaMatchesStructs`, `TestIngestDeviceAuthentication`, `TestDiskLossRequiresNewEpoch`, `TestEnrollSkipIfEnrolledIsIdempotent` | **đạt**. CA thiết bị riêng: mục 11 |

RB-4 (giấy phép) không đổi so với G2: ADR-0016 vẫn chưa ký (không tính dòng ở G3).

## 8. MRV (KN US-MRV-01..06, FR-07, FR-08)

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| KN US-MRV-01 ghi nhật ký AWD (ảnh + toạ độ, trong địa bàn) | `TestEvidenceValidationAndScope`, `TestConsentRequired`, `TestCorrectionChain` (G1) | **đạt** (G1) |
| KN US-MRV-02 đối chiếu nhật ký HTX ↔ KN (Khớp/Khác biệt + ghi chú) | `TestG3FarmLogLinkIdempotentPinnedKNUSMRV02`, `TestG3ComparisonRecordKNUSMRV02`, `TestSuggestMatchKNUSMRV02`; Vitest `mrv.test.ts`; WEB-E2E `mrv.spec.ts` | **đạt** |
| KN US-MRV-03 kiểm tra thực địa theo danh mục chuẩn | `TestCatalogSoDAndVersionPinning`, `TestCatalogDefinitionValidation` (G1, loại `field_checklist`) | **đạt** (G1). Danh mục tạm, chờ C-1 |
| KN US-MRV-04 ghi nhận sâu bệnh/thiên tai, tự gắn "Cảnh báo khẩn" | — | **một phần**: chưa làm nhãn tự động; ngưỡng chờ khách |
| KN US-MRV-05 duyệt hồ sơ MRV; chỉ hồ sơ đạt chuẩn tính vào giảm phát thải | `TestReviewerIsNotCapturer`, `TestG3OnlyApprovedEvidenceCountsKNUSMRV05`, `TestComputeEstimateOnlyApprovedCountsKNUSMRV05`; Vitest `mrv.test.ts`; WEB-E2E `mrv.spec.ts` | **đạt** |
| KN US-MRV-06 leo thang hồ sơ quá hạn | — | **một phần**: chưa làm |
| KN FR-07 ghi nhận xử lý rơm rạ (bằng chứng MRV) | `TestG3OnlyApprovedEvidenceCountsKNUSMRV05` (loại `straw_handling`), `TestG3CollectionValveContractAndIdempotency` (bằng chứng thu gom từ MKG), E2E-G3-12 | **đạt**. Trường chi tiết theo danh mục tạm (C-1) |
| KN FR-08 cảnh báo bất thường nhật ký HTX | `TestG3NoRecordAnomalyKNFR08`, `TestNoRecordAnomalyKNFR08`; WEB-E2E `mrv.spec.ts` ("Bất thường (FR-08)") | **một phần**: thiếu cờ AWD/hạ tầng và thông báo KNV |

## 9. Quy tắc prototype (FM-01..10, cân nhà máy)

| Quy tắc | Kiểm thử | Trạng thái |
|---|---|---|
| proto FM-01 không cuộn trước ngày gặt | `TestBaleBeforeHarvestRejectedFM01` (trước ngày gặt → 422 `fieldops.before_harvest`, cả khi nền tảng lỗi nhờ bảng đệm; chưa biết ngày gặt → ghi được, gắn cờ `unknown`/`unavailable` + cảnh báo; push đồng bộ từ chối theo bản ghi; CHECK DB lớp hai), `TestSelectSeasonAndBaleDateFM01`, `TestPlatformHTTPSeasonHarvestDatesFM01` | **đạt**. Ngày gặt đọc qua cổng `fieldops/port.HarvestDates` (bản giả + bộ điều hợp HTTP `GET /v1/partner/season-harvest-dates?plot_ref=`); bề mặt partner của nền tảng (ADR-0020) do người tích hợp nối |
| proto FM-02 cuộn xong trong N ngày sau gặt | `TestBalingDeadlineOverdueFM02` (quá hạn chưa cuộn → cờ trên thửa, lệnh gom, danh sách `GET /v1/fieldops/baling-deadlines`; cuộn trễ ghi được nhưng `baled_late`; N có phiên bản, người đề xuất ≠ người duyệt, CHECK DB), `TestBalingStateFM02` | **đạt**. N = `baling_deadline_days`, mặc định 3 ngày (prototype `MAX_DAYS_AFTER_HARVEST`) — **tạm, chờ khách**; job `fieldops_harvest_dates` làm mới ngày gặt |
| proto FM-03 công đoạn sau chỉ ghi khi công đoạn trước có số | `TestStageEqualityFM04` (ca FM-03), `TestStageCountsFM04` | **đạt** |
| proto FM-04 số cuộn không tăng qua công đoạn; ít hơn cần lý do; sửa = bản ghi bù | `TestStageEqualityFM04`, `TestStageCountsFM04`, `TestRollCountCorrectionByCompensation`, `TestSyncPushIdempotentAndPull`, E2E-G3-05; Flutter `domain_test.dart`, `field_service_test.dart` (App hiện trường MKG) | **đạt**. G3 chốt "bằng hoặc ít hơn kèm lý do" (g3-conventions §1.1) |
| proto FM-05 mỗi lượt xuống ghe một chuyến vận tải thủy | — | **ngoài G3 → G4** (`mkg/logistics`) |
| proto FM-06 phân công không vượt năng lực tổ | `TestAssignCapacityCheckFM06` (vượt → 409 `fieldops.crew_over_capacity`; giám sát giao tay kèm lý do → gắn cờ `over_capacity`; đơn vị cuộn/ha; lệnh đóng trả năng lực; CHECK DB), `TestCheckCapacityFM06`, `TestOrderApprovalSeparationOfDuties` | **đạt** |
| proto FM-07 chỉ tổ đang hoạt động nhận việc | `TestCrewDirectoryVersionedFM07` (sổ tổ gom có phiên bản, không xóa cứng; tổ ngừng → 409 `fieldops.crew_inactive`, tổ lạ → 422 `fieldops.crew_unknown`; RLS), `TestNormalizeCrew`, `TestSyncPullFiltersByCrewMembership` (pull của tổ gom lọc theo thành viên tổ) | **đạt** |
| proto FM-08 phân công tự động không trùng | `TestAutoAssignSuggestionFM08` (gợi ý tất định: đang hoạt động, vừa năng lực, gần nhất rồi ít tải nhất; giám sát xác nhận `mode = suggested`; xác nhận lặp → 200, không sinh phân công mới), `TestSuggestDeterministicFM08` | **đạt**. Gợi ý theo từng lệnh gom (chỉ đọc); "gần nhất" = xã của lệnh thuộc xã phục vụ của tổ (chưa có tọa độ tổ/thửa) |
| proto FM-09 ở ruộng đếm cuộn; tấn chỉ khi không có cuộn; kg/cuộn học theo HTX | `TestBaleCountMandatoryFM09`, `TestKgPerRollFallbackChain`, `TestKgPerRollLearningFallback`, `TestWeighTicketConsumerLearnsKgPerRoll`, `TestRollCountGPSOptionalNeverBlocks` | **đạt**. kg/cuộn không bao giờ qua van |
| proto FM-10 cân nhà máy lệch > 5 % so với ước tính theo cuộn → cờ; > 15 % nghiêm trọng | `TestGoldenPrototypeSeedRollVsMass` (+10 % cờ; +15,11 % nghiêm trọng), `TestCollectionLinkDrivesRollCheck`, `TestMKGPlanGoldenPrototypeLot`, E2E-G3-11 | **đạt** |
| proto plant-weighing: chỉ ghi thêm, tịnh = tổng − bì > 0, số cuộn nhà máy đếm lại | `TestNormalizeDraftRules`, `TestIntegrateV1Golden` (bì ≥ tổng → từ chối), `TestIngestTicketIdempotencyCorrectionAndGrants` | **đạt** |
| proto phiếu nhận hàng ↔ đơn mua, cảnh báo > 3 % | — | **ngoài G3 → G4** (`mkg/orders`) |

## 10. Lộ trình G3-2 (client, liên thông)

| Đợt | Nội dung | Kiểm thử | Trạng thái |
|---|---|---|---|
| B1 | App hiện trường MKG: đếm cuộn ngoại tuyến theo công đoạn, lệnh gom được giao, GPS không chặn | Flutter `domain_test.dart`, `field_service_test.dart`, `http_mkg_sync_test.dart`, `app_flow_test.dart`, `integration_test/mkg_flow_test.dart`; `TestSyncPushIdempotentAndPull`, `TestRollCountGPSOptionalNeverBlocks`, `TestParseLocation` | **đạt**. Emulator: CI |
| B2 | App HTX + web-ops: lịch gom, xác nhận/tranh chấp bảng kê | Flutter `collection_test.dart`; Vitest `collection.test.ts`; WEB-E2E `htx-collection.spec.ts`; `TestCollectionSyncPushPull_RB6`, `TestCollectionValveScheduleLatestRevision_RB6_BuySide` | **đạt** |
| B3 | Web nội bộ MKG (`apps/web-internal`, chỉ gọi api-internal): thiết bị, phiếu, đối soát, tranh chấp, dung sai/định mức, hub, lệnh gom | WEB-E2E `measurement.spec.ts`, `fieldops.spec.ts`, `hub.spec.ts`, `sod.spec.ts`, `auth.spec.ts`; Vitest `idempotency.test.ts`, `problem.test.ts`, `mock.test.ts`, `nav.test.ts`, `lib.test.ts` | **đạt** |
| B4 | web-ops: bảng chỉ số có lịch sử, phát lại GIS theo ngày | WEB-E2E `metrics-history.spec.ts`, `gis-replay.spec.ts`; Vitest `metrics.test.ts` | **đạt** |
| B5 | web-ops: MRV — độ đầy đủ, đối chiếu HTX ↔ KN, bất thường, ước tính tạm | WEB-E2E `mrv.spec.ts`; Vitest `mrv.test.ts` | **đạt** |
| B6 | Công cụ đăng ký thiết bị: `scripts/gen-dev-certs`, CLI `edge-agent enroll`, bộ mô phỏng đầu cân | `TestCLIEnrollInfoVersion`, `TestEnrollSkipIfEnrolledIsIdempotent`, `TestEnrollRules`, `TestSimulatorDeterministicCycles`, E2E-G3-02 (CA dev) | **đạt** |
| B7 | Compose và CI: `ingest` + `edge-sim` trong `compose-smoke` | `scripts/dev-edge-bootstrap.sh`, workflow | **một phần**: chưa chạy trên runner |
| B8 | UAT G3: bảng nghiệm thu + kịch bản cân → phiếu → đối soát → MRV | `TestG3FieldToCustomer` (tài liệu này) | **đạt** |

## 11. Câu hỏi khách và hạ tầng còn lại

| Yêu cầu | Kiểm thử | Trạng thái |
|---|---|---|
| C-5 đường nhận số cân của khách (tích hợp hệ cân) | `TestCustomerWeightImportIdempotent`, `TestHubReconciliationDisputeLifecycle` (đường tạm) | **một phần**: chờ khách |
| Định mức hao hụt sấy HUB_IN → HUB_OUT | `TestLossNormBands`, `TestToleranceVersionsSoDAndEventTime` (cơ chế phiên bản) | **một phần**: chờ khách |
| CA thiết bị riêng cho production | `TestIngestMTLSListener` (CA khác bị từ chối) | **một phần**: hạ tầng |

Các câu hỏi khách G3 khác nằm ở dòng tương ứng (C-4 mục 4, C-6 A-3, C-7 R-71, C-1 và phương pháp tCO2e 16-6, RB-2 17-4, FN-21 mục 4) và ở [tong-hop.md](../requirements/tong-hop.md#câu-hỏi-mở-g3-30092026).

## 12. Giới hạn đã biết của bộ kiểm thử

- **CI chưa chạy cho G3** (GitHub Actions bị chặn do thanh toán): xem ghi chú môi trường ở đầu tài liệu.
- **RabbitMQ.** Máy dev không có broker. Kịch bản E2E thay outbox-publisher + RabbitMQ bằng bơm outbox gọi đúng sổ consumer của `cmd/worker` và `cmd/worker-internal` qua `shared.inbox`. Định tuyến theo `kind` được kiểm bằng broker giả (`TestOutboxKindRoutesToOwnBroker`); vhost thật chỉ ở `compose-smoke`.
- **Một tenant cho hai vùng.** Tới khi có cấp quyền chia sẻ (RB-2, Q-MKG-12), kịch bản dùng cùng một tenant cho nền tảng và MKG.
- **Chỉ số ngày D.** CHECK DB `recorded_at >= cutoff` chặn ghi sổ trước khi ngày kết thúc (kể cả với đồng hồ giả). Kịch bản ghi sổ ngày D−1 và kiểm `AsOf(D)` bằng module dựng với đồng hồ "hai ngày sau".
- **Web E2E** chạy trên backend giả lập theo hợp đồng. Quy tắc nghiệp vụ do kiểm thử Go chứng minh.
- **Phần cứng cân** chưa có (C-6): agent được kiểm bằng bộ mô phỏng và cầu nối TCP cho giao thức modbus/serial.
- **Dữ liệu thật** (số cân khách, dung sai, định mức, phương pháp MRV) chưa có. Kiểm thử dùng dữ liệu tổng hợp và số golden của prototype.
