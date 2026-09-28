# Nghiệm thu cổng G1: yêu cầu → kiểm thử

Lập ngày 28/09/2026. Bảng dưới đây đối chiếu từng yêu cầu G1 với kiểm thử tự động chứng minh nó. Yêu cầu lấy từ:
- [docs/roadmap.md](../roadmap.md), dòng G1;
- các story chính của BRD/Backlog;
- các cổng kiến trúc RB-1..RB-7 và R-xx của tài liệu kiến trúc v3.0.

Ô ghi **chưa có** là chưa có kiểm thử tự động, kèm lý do. Ô ghi **một phần** là kiểm thử chỉ phủ phần máy chủ hoặc phần lõi.

## 1. Cách chạy

```powershell
. .\scripts\dev-env.ps1
go test -count=1 ./tests/e2e/ ./cmd/seed-synthetic/
```

Kiểm thử cần PostgreSQL 17 + PostGIS như mọi kiểm thử tích hợp. Mỗi gói kiểm thử dùng database riêng. Nếu không đặt `AG_TEST_PG_ADMIN_URL`, kiểm thử bị bỏ qua trên máy dev và hỏng khi `CI=true`.

### Kịch bản đầu–cuối `TestG1DongXuanCanTho`

Kịch bản "G1 — một vụ Đông Xuân ở Cần Thơ" nằm ở [tests/e2e/g1_scenario_test.go](../../tests/e2e/g1_scenario_test.go). Nó dựng toàn bộ nền tảng trong tiến trình bằng [tests/e2e/harness](../../tests/e2e/harness):
- **Application API và Admin API** dựng bằng chính gốc lắp ráp `cmd/api-platform/assembly` (cùng mã với tiến trình thật). Mỗi module có vai DB riêng.
- **Kho định danh là bản thật.** Nó chạy trên database vault riêng, sau `httptest.Server` ([cmd/identity-vault/vaulttest](../../cmd/identity-vault/vaulttest)). Module gọi kho qua `client.NewHTTP` và chuyển tiếp token người dùng.
- **Issuer OIDC là bản giả.** Kho đối tượng là một thư mục cục bộ.

Kịch bản chỉ gọi qua HTTP. Có hai ngoại lệ, đều là việc mà ở production do worker làm:
- job leo thang SLA, chạy bằng `extension.Module.Jobs` với đồng hồ giả;
- handler inbox xuất gói MRV, chạy bằng `inbox.Process` + `mrv.Module.ExportHandler`.

Mọi POST đều mang `Idempotency-Key`. Mọi đường lỗi đều được kiểm là `application/problem+json`, có `code` và `trace_id`.

Kịch bản có 14 bước, được nhắc tới trong bảng ở dạng **E2E-nn**:

| Bước | Nội dung |
|---|---|
| E2E-01 | Seed masterdata qua Admin API. Kiểm: thiếu Idempotency-Key → 400; không MFA → 403; gọi lặp → phản hồi cũ |
| E2E-02 | Cây TTKN QG → TTKN tỉnh 92 → tổ KN Ninh Kiều (và nhánh An Giang). Tạo HTX, người đại diện lưu ở vault. Kiểm vi phạm cây → 422 |
| E2E-03 | 3 nông hộ qua vault; SĐT trùng → `duplicate` + cùng ref. Tỉnh khác ghi hộ → 403. Danh sách chỉ có nhãn đã che |
| E2E-04 | Đồng ý `mrv.evidence` cho hộ 1, kèm sự kiện outbox |
| E2E-05 | Cán bộ xã vẽ 2 thửa; kiểm diện tích geodesic. TTKN tỉnh thấy 2 thửa. Cán bộ tỉnh khác: 0 thửa, 404 khi đọc, 403 khi vẽ. Tam giác → 422 |
| E2E-06 | Quy trình quốc gia: người soạn tự công bố → 403 `self_approval_forbidden`; người duyệt khác công bố được. Tiếp theo: hướng dẫn bổ sung tỉnh, cảnh báo cấp xã, phạm vi đọc giữa hai tỉnh |
| E2E-07 | Nhiệm vụ SLA 24 giờ; nơi nhận leo thang là TTKN tỉnh. Giao việc. Job leo thang chạy hai lần với đồng hồ quá hạn → đúng 1 sự kiện `pf.extension.task.escalated` |
| E2E-08 | Báo cáo gieo sạ ở hai tỉnh. Dashboard toàn quốc: mọi KPI bằng tổng các tỉnh, tổng diện tích 20,75 ha. Dashboard tỉnh khớp ô tỉnh của dashboard toàn quốc |
| E2E-09 | CGH: chủ máy lưu ở vault (trùng SĐT → 409), 3 máy MLD, diện tích vụ Đông Xuân 2026 nhập tay, chạy cân đối. Kết quả lưu: 72 % → "Cần chú ý". Các khâu không có máy → Thiếu |
| E2E-10 | Danh mục trường MRV: seed → gửi → người khác duyệt |
| E2E-11 | Đồng bộ. Đăng ký thiết bị. Pull đủ `plot`/`document`/`task`/`pubkey`. Ảnh mã hóa bằng khóa công khai hộ 1 (envelope v1) → `POST /v1/files`. Kho chỉ giữ bản mã; vault mở DEK đọc bản rõ khi có mục đích. Push lô hỗn hợp 7 bản ghi → kết quả theo từng bản ghi (accepted, consent_missing, too_few_points, unknown_entity). Gọi lặp → phản hồi cũ; gửi lại ở lô khác → `duplicate` |
| E2E-12 | Thu hồi thiết bị. Sau đó pull, push và đăng ký lại đều → 403 `sync.device_revoked`, có sự kiện outbox |
| E2E-13 | Xuất gói MRV: 202 + job. Handler inbox dựng gói; giao lặp bị bỏ qua. `verify.match = true`. SHA-256 của đối tượng = `package_sha256` |
| E2E-14 | Không có PII ngoài vault. Kiểm bốn chỗ: không có cột mang tên PII trong `pf_*`/`shared`; quét giá trị mọi bảng `pf_*`/`shared` (có đối chứng dương); log của cả ba server; tệp trong kho đối tượng. Đối chứng ngược: vault tra được hộ theo SĐT |

## 2. Lộ trình G1 (docs/roadmap.md)

| Sprint | Hạng mục | Kiểm thử | Trạng thái |
|---|---|---|---|
| S1 | Đơn vị hành chính có hiệu lực theo sáp nhập 2025 | `TestSuccessorResolutionAfterMerger`, `TestDescendantsAtTime`, `TestResolveCurrentMergerSplitAndReuse`, E2E-01 | có |
| S1 | Giống lúa, danh mục | `TestCatalogDeactivateKeepsHistory`, E2E-01, E2E-08 | có |
| S1 | Tham số có phiên bản, setter ≠ approver | `TestSelfApprovalBlocked`, `TestParamLookupAtEventTime`, `TestParamCorrectionSupersedes` | có |
| S1 | Vault: API mục 10.3, X25519 theo hộ, blind index | `TestCreateResolveDisplayUnwrap`, `TestBlindIndexDuplicatePerTenant`, `TestHTTPClientEndToEnd`, E2E-03, E2E-11 | có |
| S1 | Bề mặt Admin với MFA | `TestAdminRequiresMFA`, E2E-01 | có |
| S2 | Thửa PostGIS, chồng lấn, diện tích geodesic | `TestGeodesicAreaNearCanTho`, `TestOverlapSameCoopRejectedCrossCoopFlagged`, E2E-05 | có. E2E chỉ dùng thửa không chồng lấn vì quy tắc chồng lấn đang được sửa |
| S2 | Cây phân cấp KN | `TestScopeProvinceSeesOnlyItsCommunes`, `TestOrgMoveHistoryAndChain`, E2E-02, E2E-05 | có |
| S2 | HTX và nông hộ, định danh ở vault | `TestHouseholdDedupeViaVault`, `TestNoPIIColumns`, E2E-03, E2E-14 | có |
| S2 | `apps/web-ops` | — | **chưa có** trong bộ này. Giao diện chưa có kiểm thử đầu–cuối qua trình duyệt |
| S2 | Chốt engine đồng bộ (23.1) | ADR-0006: đồng bộ qua Application API; E2E-11 | có (quyết định) |
| S3 | `apps/field-app`: offline 72 giờ, SQLCipher, hàng đợi, vẽ offline, mã hóa ảnh, hộp thư/đẩy, thư viện "Đã lưu", đồng ý, watermark, bản đồ ngoại tuyến | `apps/field-app/test/*.dart` (auth, outbox, boundary, photo_pipeline, envelope_vectors, wipe, inbox_library_directive, push, consent_watermark, tiles_pull, http_contract_g1), `offline_flow_http_test.dart` (HTTP thật tới máy chủ giả `dart:io`), `integration_test/app_flow_test.dart` (emulator API 30, CI `field-app-emulator`) | **có** (28/09). Emulator chạy SQLCipher + Keystore + giao diện thật với máy chủ API giả trong tiến trình; chưa chạy với máy chủ Go thật (E2E-11 vẫn mô phỏng phía thiết bị bằng Go). Job emulator chỉ chạy khi push `main`/chạy tay — chưa chạy trên máy dựng (không có Android SDK) |
| S4 | `platform/extension`: nhiệm vụ SLA 24 giờ, leo thang | `TestTaskSLAFromParamAtCreation`, `TestEscalationJobOnlyOverdueAndIdempotent`, E2E-07 | có |
| S4 | Thư viện quy trình | `TestLocalSupplementScope`, `TestAuthorCannotSelfApprove`, E2E-06 | có |
| S4 | Dashboard 4 KPI | `TestNationalKPIEqualsSumOfProvinces`, E2E-08 | có |
| S4 | CGH: cân đối QT-01/02 | `TestComputeBalanceQT01`, `TestClassifyBoundariesQT02`, `TestBalanceUsesNormAtSeasonAndPilotStages`, E2E-09 | có |
| S4 | Danh mục trường MRV (C-1) | `TestCatalogSoDAndVersionPinning`, E2E-10 | có. Danh mục vẫn là **tạm**, chờ khách chốt C-1 |
| S4 | OTLP (ADR-0013) | `TestTraceWithOTel`, `TestTracePropagatesOutboxToInbox` | có |
| S5 | Bộ sinh dữ liệu tổng hợp (R-95) | `TestRefusesProduction`, `TestPlanDeterministicAndPlausible`, `TestSeedAgainstInProcessStack` ([cmd/seed-synthetic](../../cmd/seed-synthetic)) | có |
| S5 | UAT theo ca kiểm thử trích từ prototype `mekong-green` | 39 kiểm thử ở [tests/acceptance](../../tests/acceptance) (HTX, thửa/chồng lấn, định mức/ngưỡng/cân đối CGH, nhiệm vụ KN + SLA, báo cáo hiện trường, danh mục, idempotency); mỗi ca ghi `// Nguồn:` tệp/ca prototype; bảng đối chiếu trong `doc.go` | **có**. 5 ca giữ nguyên quy tắc, 34 ca viết theo quy tắc MỚI có ghi `// Khác prototype:` (PII chỉ ở vault, chỉ ghi thêm thay vì xóa, chồng lấn theo BRD, Idempotency-Key bắt buộc). Năm phát hiện đã SỬA (ca tương ứng nay kiểm quy tắc đúng): gán thửa cho nông hộ của HTX khác → 422 `geo.household_invalid` (port `party.Households`); mốc ngưỡng CGH phải tăng dần Cần chú ý < Đủ < Thừa, kiểm khi đề xuất và khi duyệt → 422 `masterdata.param_threshold_order`; khôi phục thửa đã ngừng dùng `POST /v1/plots/{ref}/restore` (sự kiện `restored`, migration pf/000018, kiểm lại chồng lấn); mặt nạ SĐT theo DEF-CGH-08 `0912•••678`; mã mùa vụ chuẩn hóa chữ HOA (trùng không phân biệt hoa/thường). Nạp Shapefile .zip WGS84 được nhận, hệ toạ độ khác → `geo.crs_unsupported` |
| S5 | Diễn tập sao lưu và phục hồi (mục 22) | `TestDrillEndToEndAndDetectsCorruption` ([cmd/drill](../../cmd/drill): số dòng, băm bộ tham số, `verify_chain`, tính lại cân đối CGH, checksum kho đối tượng, neo ký Ed25519 — và phát hiện từng kiểu hỏng), `TestBackupRestoreVerify` ([internal/shared/objsync](../../internal/shared/objsync)); workflow `restore-drill` (hằng tuần + chạy tay: pgBackRest full → `down -v` → restore → `drill check -strict`) | **có** (kiểm thử Go). Phần Docker (pgBackRest, kho S3 thứ hai, kcadm) chỉ chạy trong CI — chưa chạy trên máy dựng. Xem [restore-drill.md](../runbooks/restore-drill.md) |

## 3. App Khuyến nông (Backlog v1.0)

### A. Quản trị hệ thống và Master Data

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-ADM-01/02/03 tài khoản, nhập hàng loạt, thao tác loạt | `TestCreateUserUSADM01`, `TestProvinceAdminGrantMatrix`, `TestLockUnlockNeedsReason`, `TestImportXLSXMixedRows`, `TestBulkActions`, `TestAssignmentChange`, `TestIdempotentCreate`, `TestTenantIsolation`, `TestContractHTTPAgainstEmulator` ([internal/platform/accounts](../../internal/platform/accounts)) | **một phần**. pf/accounts gọi Keycloak Admin REST (bản HTTP kiểm bằng emulator, không có Keycloak thật trong kiểm thử). Kích hoạt bằng liên kết một lần thay cho mật khẩu tạm (ADR-0010) |
| HTX epic J (US-HTXSTATUS-01 AC-2, 02 AC-2, 03): vô hiệu hóa/kích hoạt lại HTX khóa/mở tài khoản | `TestOrgDeactivationLocksOnlyThatOrgAndReactivationRestoresOnlyThose` (consumer `pf.accounts.org_status` qua `inbox.Process`), `TestLockUnlockNeedsReason` (sổ chỉ ghi thêm, chuỗi băm) | có ở mức module. E2E chưa chạy qua RabbitMQ |
| US-ADM-04 đăng nhập, điều hướng theo vai trò và phạm vi | `TestVerifyRejects`, `TestVerifyBuildsPrincipal`, `TestNoOrgSeesNothing`, E2E-05 | **một phần**. Có xác minh token và phạm vi theo `org_ref`; phần điều hướng giao diện chưa có |
| US-ADM-05/06 PIN 6 số, offline 72 giờ | `apps/field-app/test/auth_test.dart`, `integration_test/app_flow_test.dart` (đăng nhập chế độ kiểm thử → tạo PIN → mở khóa CSDL SQLCipher bằng khóa Keystore) | **một phần**. Có Dart + emulator; OIDC ở chế độ kiểm thử (chưa có Keycloak thật trên emulator) |
| US-ADM-07 cây phân cấp 3 cấp | E2E-02, `TestAllowedParent`, `TestScopeProvinceSeesOnlyItsCommunes` | có |
| US-ADM-08 di chuyển nhánh; nhập/xuất Excel | `TestOrgMoveHistoryAndChain`, `TestOrgTreeExportImportRoundTrip`, `TestOrgTreeExportScoped` | có. `GET /v1/orgs/export.xlsx` → sửa → `POST /v1/orgs/import`; nhập đi qua CreateOrg/MoveOrg/RenameOrg (sổ ghi lý do). Không vô hiệu hóa tổ chức vắng mặt trong tệp |

### B. Quản lý HTX và nông hộ

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-HTX-01 danh sách HTX theo phạm vi | E2E-02, E2E-03, `TestScopeProvinceSeesOnlyItsCommunes`, `TestDeactivateReactivateCooperative` | có |
| US-HTX-02 hồ sơ nông hộ, chống trùng | E2E-03, `TestHouseholdDedupeViaVault` | có |
| US-HTX-03 nhập hàng loạt | `TestImportHouseholdsCSV`, `TestImportHouseholdsXLSX`, `TestImportHouseholdsXLSXFormulaRejected` | có. CSV UTF-8 và Excel .xlsx (tiêu đề có dấu); ô công thức bị từ chối cả tệp |
| US-HTX-04 khai báo máy cơ giới hóa | E2E-09, `TestSerialUniqueAndStageDerived`, `TestImportUpdateAndReplace` | có (qua pf/mechanization) |
| US-HTX-05 đồng bộ nông hộ từ App HTX | — | **chưa có**. App HTX thuộc G2 |

### C. Vẽ ranh giới thửa

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-PLOT-01 vẽ ranh giới | E2E-05, `TestBoundaryValidationRules`, `TestCorrectionCreatesNewVersion` | có |
| US-PLOT-02 tải tệp ranh giới | `TestImportGeoJSONAndKML`, `TestImportShapefileZip`, `TestParseShapefileZip`, `TestParseShapefileRejects` | có. GeoJSON, KML, Shapefile (.zip: .shp/.dbf/.prj). Chỉ WGS84; VN-2000/hệ phẳng trả `geo.crs_unsupported` — chưa tự chuyển VN-2000 (người dùng chuyển sang EPSG:4326 trước) |
| US-PLOT-03 cảnh báo chồng lấn | `TestOverlapSameCoopRejectedCrossCoopFlagged`, `TestClassifyOverlaps`; app: `boundary_test.dart` (chặn HTX khác, xác nhận cùng HTX, lệch diện tích khai báo > 5 %) | có ở mức module + app. E2E không kiểm vì quy tắc đang được sửa song song |
| US-PLOT-04 vẽ offline, tự đồng bộ | E2E-11 (`plot_boundary` accepted; tam giác → rejected), `TestSyncSinkIdempotentAndConflict`; app: `boundary_test.dart` (payload `BoundaryPayload`, `declared_area_ha` chuỗi, `correct` theo `boundary_id`, `confirm_overlap` sau `conflict`/`rejected geo.*_needs_confirm`), `offline_flow_http_test.dart`, `integration_test/app_flow_test.dart` (vẽ khi mất mạng → có mạng + `sync_now` → accepted) | có (máy chủ + app). Trạng thái xung đột hiện ở danh sách thửa và hàng đợi |

### E. Liên thông dữ liệu (một phần)

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-SYNC-01 hai chiều với App HTX | — | **chưa có**, thuộc G2. G1 chỉ có đồng bộ app hiện trường ↔ máy chủ: E2E-11, `TestPushMixedBatchAndReplay`, `TestPullPaginationRoundRobin`, `TestPullScopeChangeResets` |
| US-SYNC-02 đồng bộ ngay theo yêu cầu | `TestNudgeScopeAndRateLimit` (`POST /v1/sync/nudge`, thông điệp dữ liệu `sync_now`, giới hạn 5 phút/đích, 30 lần/giờ); app: `push_test.dart` (`sync_now` → đồng bộ ngay; token `POST /v1/devices/{id}/push-token` sau đăng ký, mỗi lần khởi động, khi đổi), `integration_test/app_flow_test.dart` | **có** (28/09). FCM thật cần cấu hình `AG_FCM_*`; kiểm thử dùng kênh đẩy giả sau lớp trừu tượng. App tắt/đang khóa: `sync_now` chờ tới khi mở khóa (chưa có WorkManager) |
| US-SYNC-03 đối soát xung đột | `TestConflictSLA`, `TestSyncSinkIdempotentAndConflict` | **một phần**. Có sổ xung đột và SLA 72 giờ (C-8 tạm), có API resolve; chưa có màn hình, E2E không tạo xung đột |
| US-SYNC-02 đồng bộ ngay theo yêu cầu | xem dòng trên | **có** (28/09) |
| US-SYNC-03 đối soát xung đột | `TestConflictSLA`, `TestSyncSinkIdempotentAndConflict`, `TestConflictResolutionUI`, `TestConflictDetailBothVersions` | **đạt API** (28/09). Lọc theo entity/SLA (`sla_status`), chi tiết giữ cả hai phiên bản không PII, xử lý chọn phiên bản hoặc bản ghi bù; chưa có màn hình web |

### G. Thư viện kỹ thuật và tin tức

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-LIB-01 soạn và xuất bản theo vùng | E2E-06, `TestPublishNewVersionKeepsOld`, `TestProvinceEditorScope`, `TestAuthorCannotSelfApprove` | có |
| US-LIB-02 hướng dẫn đặc thù địa phương | E2E-06, `TestLocalSupplementScope` | có |
| US-LIB-03 trích nội dung từ tệp | `TestExtract`, `TestExtractPDFVietnamese`, `TestExtractPDFRejects`, `TestParseToUnicode` | có. DOCX và PDF có lớp chữ (tiếng Việt có dấu). PDF ảnh scan/mã hóa trả `content.extract_unsupported` kèm lý do (không OCR) |
| US-LIB-04 tra cứu | `TestSearchAccentInsensitive`, E2E-06 | có |
| US-LIB-05 lưu và tải offline | E2E-11 (pull `document`), `TestSyncRetireTombstone`, `TestPersonalLibraryUSLIB05`; app: `inbox_library_directive_test.dart` | **có** (28/09) — xem dòng dưới |
| US-LIB-06 thông báo nội dung mới | E2E-06, `TestAlertPublishedEvent`, `TestAlertFanOutRespectsScope`, `TestDuplicateEventDeliveryNoDuplicates`, `TestFCMPayloadIsDataOnly`; app: `inbox_library_directive_test.dart`, `push_test.dart`, `integration_test/app_flow_test.dart` | **một phần**. Flutter: hộp thư từ entity `notification` (30 ngày), chuông + số chưa đọc, đọc/đọc hết xếp hàng khi ngoại tuyến, `{kind, notification_id}` → pull → thông báo cục bộ (màn hình khóa chỉ "Có thông báo mới"), mở đúng màn hình theo `deep_link`. Chưa ráp app web |
| US-LIB-05 lưu và tải offline | E2E-11 (pull `document`), `TestSyncRetireTombstone`, `TestPersonalLibraryUSLIB05`; app: `inbox_library_directive_test.dart` (lưu/bỏ lưu ngoại tuyến → phát lại theo thứ tự, tài liệu đã gỡ giữ nhãn "đã gỡ", tombstone) | **có** (28/09). Danh mục cá nhân (migration 000014) + entity `library_item`; app có màn "Thư viện · Đã lưu" |
| US-LIB-06 thông báo nội dung mới | xem dòng US-LIB-06 ở trên | **một phần** (28/09): có kênh đẩy tới app Flutter; chưa có app web |

### I. Dashboard và báo cáo

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-DASH-01 4 KPI theo vai trò | E2E-08, `TestNationalKPIEqualsSumOfProvinces`, `TestClosedPeriodNeedsCorrectionReason` | có |
| US-DASH-02 xuất theo mẫu Bộ NN&PTNT | `TestExportXLSXAsyncViaInboxOnce`, `TestXLSXContent`, `TestPDFVietnamese`, `TestReportDataSowingAndSummary`, `TestProgramProgressFR03`, `TestQuotaAndAnomaly`, `TestExportScope` | **một phần**. Xuất Excel/PDF bất đồng bộ từ mẫu có phiên bản (FR-01/02/03/05, nhiệm vụ); nhãn "Số liệu tạm tính"; DLP hạn mức + cảnh báo. Bố cục **tạm**: BRD chưa có mẫu biểu chính thức của Bộ |
| US-DASH-03 lịch gửi báo cáo email | `TestScheduleFiresOncePerPeriodAndDelivers`, `TestDeliverySkippedWithoutDirectory`, `TestScheduleSlot` | **một phần**. Lịch tuần/tháng, mỗi mốc đúng một lần, email liên kết 15 phút qua SMTP TLS; địa chỉ email tra qua cổng `MailDirectory` (pf/accounts nối sau — thiếu thì bỏ gửi kèm cảnh báo) |

### J. Nhiệm vụ hỗ trợ

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-TASK-01 tự sinh nhiệm vụ từ yêu cầu App HTX | `TestCoopManagerCreatesForOwnCooperative` | **một phần**. Quản lý HTX tạo được nhiệm vụ qua API; luồng tự động từ App HTX thuộc G2 |
| US-TASK-02 tiếp nhận, xử lý, chuyển tiếp, leo thang SLA | E2E-07 (SLA 24 giờ, leo thang lên tỉnh), E2E-11 (`task_update` accepted), `TestEscalationJobOnlyOverdueAndIdempotent`; app: `consent_watermark_test.dart` (đếm ngược SLA: còn / sắp hết < 4 giờ / quá hạn; `action` theo pf/extension), `photo_pipeline_test.dart` | có. App hiển thị đếm ngược SLA theo `due_at`, mở đúng nhiệm vụ từ thông báo leo thang |
| US-TASK-03 giám sát và gửi chỉ đạo | `TestDirectiveAckRate`, `TestOfficerOutsideCommuneCannotSeeOrAssign`; app: `inbox_library_directive_test.dart` (xác nhận đã đọc ngoại tuyến → `POST /v1/extension/directives/{id}/ack`, chỉ người nhận) | có ở mức module + app. E2E không gửi chỉ đạo |
| US-TASK-04 phân công hàng loạt | `TestBulkAssign` | có |

### K. Danh bạ trực

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-DIR-01 cập nhật trạng thái trực | `TestRosterDutyStatus`, `TestDutyAt` | có |
| US-DIR-02 tra cứu danh bạ | `TestRosterDutyStatus`, E2E-07 (đăng ký danh bạ) | **một phần**. "Gọi/nhắn 1 chạm" là giao diện |
| US-DIR-03 lịch phân ca | `TestRosterDutyStatus` (`/roster/shifts`) | có |

## 4. Bản đồ CGH (BRD v1.3)

| FN | Nội dung | Kiểm thử | Trạng thái |
|---|---|---|---|
| FN-01 | Tài khoản và phân quyền | `TestViewerReadsAllButCannotWrite`, `TestCreateUserUSADM01`, `TestLockUnlockNeedsReason`, `TestGrantMatrix` | **một phần**. Quản trị tài khoản qua pf/accounts (Keycloak Admin REST); "mật khẩu tạm" của BRD thay bằng liên kết UPDATE_PASSWORD một lần (ADR-0010) |
| FN-02 | Danh mục và cấu hình | E2E-01, E2E-09 (ngưỡng 85/60 đọc từ tham số đã duyệt), `TestSelfApprovalBlocked` | có |
| FN-03 | Hồ sơ HTX | E2E-02, `TestTaxCodeUniquePerTenant` | có |
| FN-04 | Chủ sở hữu máy | E2E-09 (vault, trùng SĐT → 409), `TestCodesAssignedOnceAndStableOnProvinceMove` | có |
| FN-05 | Hồ sơ máy | E2E-09, `TestSerialUniqueAndStageDerived`, `TestMachineCountAtT`, `TestImportUpdateAndReplace`, `TestImportMachinesXLSXReplace` | có. Nhập CSV hoặc Excel .xlsx |
| FN-06 | Lịch mùa vụ và diện tích theo vụ | E2E-09 (nhập tay), `TestLockedManualAreaNotOverwritten`, `TestValidateCalendar` | có |
| FN-07 | Tích hợp App HTX | `TestResolveAreaQT03` (chỉ port `AreaFeed`) | **chưa có** luồng thật, thuộc G2 |
| FN-08 | Bản đồ và trực quan hóa | `TestSummarizeBottleneck` (API coverage theo cấp) | **một phần**. Có API đọc; chưa có kiểm thử giao diện bản đồ |
| FN-09 | Cân đối và dự báo | E2E-09, `TestBalanceUsesNormAtSeasonAndPilotStages`, `TestSavedResultsImmutableAndChainVerifies`, `TestForecastFN09AndCoopCodeG10`, `TestProjectAreaFN09` | **đạt, phương pháp dự báo tạm** (`cgh.forecast/v1`, docs/dev/cgh-forecast.md) — **cần DCRD xác nhận phương pháp** |
| FN-10 | Báo cáo và xuất dữ liệu | `TestReportDataEqualsSavedResults`, `TestCGHExportEqualsSavedResults`, `TestTemplateVersionsAdmin` | có (API). 3 tab Tổng hợp / Thiếu hụt theo HTX / Đối chiếu vụ A–B, xuất Excel/PDF từ kết quả cân đối **đã lưu**, nhãn nguồn, Admin quản lý mẫu |
| FN-11 | Dashboard tổng quan | E2E-09 (`total_machines`) | có |

## 5. GIS nền (Backlog v1.1)

| FN | Nội dung | Kiểm thử | Trạng thái |
|---|---|---|---|
| FN-04 | Ranh giới HTX/Hub | `TestAdminBoundaryAndCommuneContainment` | **một phần**. Có ranh giới hành chính và toạ độ điểm của HTX; chưa có polygon HTX và Hub (thuộc mkg/hub, G2+) |
| FN-05 | Thửa ruộng: vẽ, sửa, nhập | E2E-05, `TestImportGeoJSONAndKML`, `TestImportShapefileZip`, `TestCorrectionCreatesNewVersion`, `TestBBoxLimitAndScope` | có. "Xóa" là `retire`, không xóa cứng (X1) |
| FN-06 | Kho/Hub | — | **chưa có**, thuộc G2+ |
| FN-07 | Dữ liệu nền: ranh giới hành chính theo zoom | `TestAdminBoundaryAndCommuneContainment` | **một phần**. Chưa có giao thông, tự nhiên, tile |

## 6. Cổng kiến trúc

| Mã | Nội dung | Kiểm thử | Trạng thái |
|---|---|---|---|
| RB-1 | Chỉ ghi thêm, sửa bằng bản ghi bù | `TestAppendOnlySixWays`, `TestCatalogGates`, `TestChainSequenceAndTamperDetection`, `TestCorrectionCreatesNewVersion`, `TestCorrectionChain` | có |
| RB-2 | Cách ly tenant | `TestTenantLeakParallel` (R-32, qua PgBouncer trong CI), `TestTenantIsolation` của từng module | có. E2E dùng một tenant |
| RB-3 | PII xóa được | `TestErasure` (vault) | **một phần**. E2E không xóa định danh rồi kiểm ảnh không mở được. Bản sao lưu (pgBackRest, `objsync`) chỉ chứa bản mã; xóa trong bản sao dựa vào hủy khóa hộ (mục 22) — diễn tập chưa kiểm "phục hồi rồi ảnh đã xóa vẫn không mở được" |
| RB-3 | PII xóa được | `TestErasure` (vault), E2E `TestRB3ErasureEvidenceUnreadable` | **đạt**. Xóa định danh → `GET /v1/files/{ref}/content` trả 410 `vault.erased`; sổ pf (đồng ý, inbound, org_event) còn nguyên, chuỗi băm xác minh được |
| RB-4 | Giấy phép | CI `go-licenses` + `deploy/policies/allowed-licenses.txt` | có (cổng CI, không phải kiểm thử Go) |
| RB-5 | Ranh giới module | `go run ./tools/archlint ./...`, `TestAssemblyMatchesAPIPlatform` | có |
| RB-6 | Đối soát chéo số đo ở biên | — | **chưa có**. Module đối soát (mục 13) chưa thuộc G1 |
| RB-7 | Dữ liệu trong lãnh thổ | `TestEachEndpointOutsideTerritoryFails`, `TestProductionRequiresAllowlist`, `TestNonPIIAllowlistOnlyForPushChannels`, `TestServiceMainsCallGuard` ([internal/shared/residency](../../internal/shared/residency)) | **có** cho cấu hình: `AG_ENV=production` → mọi đích DB/S3/OTLP/SMTP/FCM/Keycloak/vault/AMQP phải thuộc `AG_RESIDENCY_ALLOWLIST` (ngoại lệ không định danh qua `AG_NONPII_ALLOWLIST` chỉ cho kênh đẩy), sai thì dừng khởi động. Pháp nhân/hạ tầng của nhà cung cấp vẫn là rà soát hồ sơ. Xem [residency.md](../runbooks/residency.md) |
| R-04 | PII chỉ ở vault | E2E-14, `TestNoPIIColumns`, archlint `agboundary` | có |
| R-10 | Decimal cho số đo | archlint `agnofloat`, E2E-08 (tổng decimal), E2E-09 | có |
| R-11 | UUIDv7 | `TestParseRequiresV7`, E2E-11 (id do máy sinh) | có |
| R-12 | Giờ nghiệp vụ Việt Nam | `TestBusinessDateUsesVietnamTime`, `TestDayOfUsesVietnamTime`, E2E-08 | có. E2E-08 bắt được lỗi `sown_on`, xem mục 7 |
| R-14 | Không PII trong log | E2E-14 (log cả ba server), archlint `agpiilog`, `TestSeedAgainstInProcessStack` | có |
| R-15, R-63 | RFC 9457 + trace_id | `TestWriteRFC9457`, mọi `wantProblem` của E2E | có |
| R-16 | Tenant và người gọi chỉ lấy từ token | E2E (không có tenant trong thân request), `TestRawTokenForwarding` | có |
| R-40 | Kho chỉ giữ bản mã | E2E-11, E2E-14 | có |
| R-42 | Đọc PII phải có mục đích | E2E-11 (`sync.purpose_required`), `TestPurposeRequired` | có |
| R-51 | Idempotency-Key 48 giờ | E2E-01, E2E-11, `TestIdempotent*` của từng module, `TestSeedAgainstInProcessStack` (chạy lại) | có |
| R-52 | Việc dài: 202 + job | E2E-13 | có |
| R-53 | Phân trang cursor | `TestCursorPagination`, E2E-11 (pull nhiều trang) | có |
| R-54 | Tự xác minh token | `TestVerifyRejects`; vault xác minh token chuyển tiếp trong E2E | có |
| R-56 | Người duyệt ≠ người đặt | E2E-06, E2E-10, `TestSelfApprovalBlocked`, `TestCatalogSoDAndVersionPinning` | có |
| R-61 | Hiệu quả đúng một lần | E2E-13 (giao lặp bị bỏ qua), `TestOutboxToInboxExactlyOnceEffect` | có |
| R-62 | DLQ | `TestDLQDepth` | có |
| R-64 | Hợp đồng sự kiện | `go run ./tools/contractcheck` | có |
| R-95 | Dữ liệu tổng hợp, không có người thật | `cmd/seed-synthetic`: từ chối `AG_ENV=production`, tất định theo `-seed`, thửa 0,3–3 ha không chồng lấn quanh tâm xã ĐBSCL, SĐT 09990/09991xxxxx | **một phần**. Chưa sinh số cuộn, sản lượng quy đổi, độ ẩm vì G1 chưa có module đo lường (G3) |

## 7. Lỗi phát hiện khi dựng kiểm thử (đã sửa tối thiểu)

1. **pf/content: không dùng được mã xã của bộ seed cho phạm vi cấp xã.**
   - Regex `^[0-9A-Za-z]{2,10}$` loại mã `92-X-NINH-KIEU`, nên tài liệu cấp xã không tạo được với dữ liệu chủ thật.
   - Đã đổi regex cho khớp quy tắc mã của pf/masterdata. E2E-06 tạo cảnh báo cấp xã.
   - Cập nhật: bộ seed nay dùng mã xã chính thức 5 chữ số (QĐ 19/2025/QĐ-TTg), E2E dùng `31135` (Phường Ninh Kiều) và `30307` (Phường Long Xuyên) — xem [danh-muc-hanh-chinh.md](../requirements/danh-muc-hanh-chinh.md).
2. **pf/extension: `sown_on` bị coi là "ở tương lai" trước 07:00 giờ Việt Nam.**
   - Ngày được parse theo UTC.
   - Đã parse theo `clock.Zone()` (R-12).
3. **pf/party: kho định danh từ chối dữ liệu (4xx, ví dụ SĐT sai dạng) nhưng API trả 503 `party.dependency_unavailable`.**
   - Client sẽ thử lại một yêu cầu sai mãi mãi.
   - Đã sửa: trả nguyên problem 4xx của kho. Lỗi hệ thống vẫn trả 503.

## 8. Giới hạn đã biết của bộ kiểm thử

- **Gốc lắp ráp.** `cmd/api-platform` là package main, nên harness lặp lại `buildModules` và router. `TestAssemblyMatchesAPIPlatform` báo lệch khi `modules.go` đổi danh sách module, Sources hoặc Sinks. Ví dụ: khi Sink `consent` của pf/party được nối, cần cập nhật harness.
- **Router kho định danh.** Router của identity-vault cũng được lặp lại trong `vaulttest`. Kiểm thử không phủ mTLS (có `TestMTLS` riêng).
- **Worker.** E2E gọi thẳng job và handler inbox, không qua RabbitMQ. Đường RabbitMQ có `TestOutboxToInboxExactlyOnceEffect`.
- **Token của bộ sinh dữ liệu.** Token lấy từ Keycloak cần mang MFA (`amr`) vì Admin API bắt buộc MFA. Realm Local/Dev cần cấu hình mapper cho tài khoản dịch vụ; việc này chưa làm trong `deploy/`.
