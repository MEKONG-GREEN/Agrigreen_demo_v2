# Nghiệm thu cổng G1: yêu cầu → kiểm thử

Lập ngày 28/09/2026. **Rà soát lại toàn bộ ngày 29/09/2026** (chốt G1): bảng được dựng lại để mỗi yêu cầu có đúng một dòng, và mọi dòng được đối chiếu lại với mã và kiểm thử hiện có (tên kiểm thử đều đã tra trong mã; các kiểm thử mới được chạy lại).

Yêu cầu lấy từ:
- [docs/roadmap.md](../roadmap.md), dòng G1;
- các story của Backlog App Khuyến nông v1.0, BRD Bản đồ CGH v1.3, Backlog GIS v1.1;
- các cổng kiến trúc RB-1..RB-7 và R-xx của tài liệu kiến trúc v3.0.

Ba trạng thái:
- **đạt** — có kiểm thử tự động chứng minh; cột kiểm thử liệt kê kiểm thử Go, **WEB-E2E** `*.spec.ts` (Playwright), Vitest và kiểm thử của app hiện trường (`test/*.dart`, `integration_test/`);
- **một phần** — ghi rõ phần còn thiếu;
- **ngoài G1 → G2/G3** — chỉ dùng cho hạng mục mà lộ trình kiến trúc đặt ở cổng sau (App HTX, Hub/kho, lớp GIS nền, đối soát đo lường, số đo G3).

## Tóm tắt

| Mục | Số yêu cầu | đạt | một phần | ngoài G1 → G2/G3 |
|---|---|---|---|---|
| 2. Lộ trình G1 | 20 | 18 | 2 | 0 |
| 3. App Khuyến nông | 34 | 29 | 2 | 3 |
| 4. Bản đồ CGH | 11 | 9 | 1 | 1 |
| 5. GIS nền | 4 | 1 | 0 | 3 |
| 6. Cổng kiến trúc | 25 | 23 | 0 | 2 |
| **Tổng** | **94** | **80** | **5** | **9** |

**Năm dòng "một phần"** — phần còn thiếu:

| Yêu cầu | Còn thiếu | Loại |
|---|---|---|
| S4 Danh mục trường MRV (C-1) | Danh mục trường **chính thức** do khách chốt (C-1). Cơ chế (danh mục có phiên bản, người duyệt ≠ người soạn, ghim phiên bản) đã đạt; nội dung hiện là danh mục tạm | chờ khách |
| S5 Diễn tập sao lưu và phục hồi | Workflow `restore-drill` (pgBackRest → `down -v` → restore → `drill check -strict`) **chưa có lần chạy xanh trên GitHub**: hai lần chạy tay ngày 28/09 hỏng, lần thứ ba đang chạy khi lập bảng. Logic kiểm (`cmd/drill`, `objsync`) đã đạt bằng kiểm thử Go | hạ tầng CI |
| KN US-SYNC-02 Đồng bộ ngay | AC "chọn **nhóm/loại dữ liệu**": lệnh `sync_now` chọn đích theo đơn vị hoặc thiết bị, thiết bị kéo mọi entity. Chọn theo nhóm dữ liệu gắn với liên thông App HTX (FN-07 KN) → G2 | một phần thuộc G2 |
| KN US-DASH-02 Xuất theo mẫu Bộ | **Mẫu biểu chính thức của Bộ NN&PTNT**: BRD không kèm mẫu; bố cục FR-01/02/03/05 hiện là tạm (mẫu có phiên bản, thay được không sửa mã) | chờ khách |
| CGH FN-09 Cân đối và dự báo | **DCRD xác nhận phương pháp dự báo** `cgh.forecast/v1` ([cgh-forecast.md](../dev/cgh-forecast.md)). Cân đối QT-01/02 và lưu kết quả theo vụ đã đạt | chờ khách |

**Chín dòng "ngoài G1"**: KN US-HTX-05, US-SYNC-01, US-TASK-01 (luồng tự động) và CGH FN-07 — tích hợp App HTX (G2); GIS FN-04 (polygon HTX/Hub), FN-06 (kho/Hub), FN-07 (lớp giao thông, thủy hệ, tự nhiên, tile — G2 cùng `pf/waterway`); RB-6 đối soát đo lường ở biên và phần số đo của R-95 (G3).

Thay đổi so với bản 28/09: gộp các dòng trùng (US-ADM-04, US-ADM-05/06, US-PLOT-03/04, US-SYNC-02, US-LIB-05/06, US-TASK-02/03 xuất hiện 2–4 lần với trạng thái khác nhau); bỏ ghi chú cũ ("quy tắc chồng lấn đang được sửa", "chưa ráp Flutter/web", "harness lặp lại router"). Đóng khoảng trống trong G1 ngày 29/09: danh bạ trực trên web và app kèm gọi/nhắn một chạm (US-DIR-01/02/03), thao tác loạt tài khoản trên web (US-ADM-03), gửi email lịch báo cáo qua SMTP được kiểm với máy chủ SMTP giả + E2E web lập lịch (US-DASH-03), bản đồ CGH đủ FN-08 (chú giải cùng màu vùng tô; ghim HTX theo vị trí hồ sơ, "vị trí tạm" ở tâm xã; gom cụm theo zoom; panel HTX máy theo khâu; tra cứu nhanh bay tới ghim), chính sách đăng nhập realm và không khóa được quản trị nền tảng cuối cùng (CGH FN-01).

## 1. Cách chạy

```powershell
. .\scripts\dev-env.ps1
go test -count=1 ./...                                   # gồm tests/e2e (kịch bản G1) và tests/acceptance (39 ca từ prototype)
go run ./tools/archlint ./... ; go run ./tools/migratecheck migrations ; go run ./tools/contractcheck
```

Kiểm thử tích hợp cần PostgreSQL 17 + PostGIS. Mỗi gói kiểm thử dùng database riêng. Không đặt `AG_TEST_PG_ADMIN_URL` thì kiểm thử bị bỏ qua trên máy dev và hỏng khi `CI=true`.

Web vận hành (`apps/web-ops`), không cần backend:

```powershell
cd apps/web-ops
npm ci
npm run check:api; npm run lint; npm run typecheck
npm test                 # Vitest: 119 ca (src/**/*.test.ts(x))
npm run e2e:install      # một lần: Chromium
$env:CI='1'; npm run e2e # Playwright: next build (NEXT_PUBLIC_API_MOCK=1) + next start, 27 ca trong tests/e2e/
```

App hiện trường (`apps/field-app`):

```powershell
cmd /c "flutter analyze --fatal-infos"
cmd /c "dart format --output=none --set-exit-if-changed lib test integration_test"
cmd /c "flutter test"                                    # 143 ca
flutter test integration_test -d emulator-5554           # chỉ trên máy có Android SDK; CI: job field-app-emulator (API 29)
```

CI (`.github/workflows/ci.yml`, 14 job khi tính cả ma trận 6 image): `lint`, `test`, `web-ops`, `web-e2e`, `field-app`, `field-app-emulator`, `security`, `images` ×6, `compose-smoke`. Workflow riêng `restore-drill` (hằng tuần + chạy tay).

E2E web dùng backend giả lập (`src/api/mock/`), đúng hình dạng hợp đồng OpenAPI: chứng minh luồng giao diện và cách hiện lỗi theo hợp đồng. Quy tắc nghiệp vụ do kiểm thử Go chứng minh.

### Kịch bản đầu–cuối `TestG1DongXuanCanTho`

Kịch bản "G1 — một vụ Đông Xuân ở Cần Thơ" nằm ở [tests/e2e/g1_scenario_test.go](../../tests/e2e/g1_scenario_test.go). Nó dựng toàn bộ nền tảng trong tiến trình bằng [tests/e2e/harness](../../tests/e2e/harness):
- **Application API và Admin API** dựng bằng chính gốc lắp ráp `cmd/api-platform/assembly` (`assembly.Build`, `assembly.NewRouter`, `assembly.NewAdminRouter` — cùng mã với tiến trình thật). Mỗi module có vai DB riêng.
- **Kho định danh là bản thật**, chạy trên database vault riêng sau `httptest.Server` ([cmd/identity-vault/vaulttest](../../cmd/identity-vault/vaulttest)). Module gọi kho qua `client.NewHTTP` và chuyển tiếp token người dùng.
- **Issuer OIDC là bản giả.** Kho đối tượng là một thư mục cục bộ.

Kịch bản chỉ gọi qua HTTP, trừ hai việc mà ở production do worker làm: job leo thang SLA (`extension.Module.Jobs`, đồng hồ giả) và handler inbox xuất gói MRV (`inbox.Process` + `mrv.Module.ExportHandler`). Mọi POST mang `Idempotency-Key`; mọi đường lỗi được kiểm là `application/problem+json` có `code` và `trace_id`.

| Bước | Nội dung |
|---|---|
| E2E-01 | Seed masterdata qua Admin API. Kiểm: thiếu Idempotency-Key → 400; không MFA → 403; gọi lặp → phản hồi cũ |
| E2E-02 | Cây TTKN QG → TTKN tỉnh 92 → tổ KN Ninh Kiều (và nhánh An Giang). Tạo HTX, người đại diện lưu ở vault. Vi phạm cây → 422 |
| E2E-03 | 3 nông hộ qua vault; SĐT trùng → `duplicate` + cùng ref. Tỉnh khác ghi hộ → 403. Danh sách chỉ có nhãn đã che |
| E2E-04 | Đồng ý `mrv.evidence` cho hộ 1, kèm sự kiện outbox |
| E2E-05 | Cán bộ xã vẽ 2 thửa; kiểm diện tích geodesic. TTKN tỉnh thấy 2 thửa. Cán bộ tỉnh khác: 0 thửa, 404 khi đọc, 403 khi vẽ. Tam giác → 422 |
| E2E-06 | Quy trình quốc gia: người soạn tự công bố → 403 `self_approval_forbidden`; người duyệt khác công bố được. Hướng dẫn bổ sung tỉnh, cảnh báo cấp xã (mã xã chính thức `31135`), phạm vi đọc giữa hai tỉnh |
| E2E-07 | Nhiệm vụ SLA 24 giờ; nơi nhận leo thang là TTKN tỉnh. Job leo thang chạy hai lần với đồng hồ quá hạn → đúng 1 sự kiện `pf.extension.task.escalated` |
| E2E-08 | Báo cáo gieo sạ ở hai tỉnh. Dashboard toàn quốc: mọi KPI bằng tổng các tỉnh, tổng 20,75 ha. Dashboard tỉnh khớp ô tỉnh của toàn quốc |
| E2E-09 | CGH: chủ máy ở vault (trùng SĐT → 409), 3 máy MLD, diện tích vụ Đông Xuân 2026 nhập tay, cân đối: 72 % → "Cần chú ý"; khâu không có máy → Thiếu |
| E2E-10 | Danh mục trường MRV: seed → gửi → người khác duyệt |
| E2E-11 | Đồng bộ: đăng ký thiết bị; pull đủ `plot`/`document`/`task`/`pubkey`; ảnh mã hóa bằng khóa công khai hộ 1 (envelope v1) → `POST /v1/files`; kho chỉ giữ bản mã, vault mở DEK khi có mục đích. Push lô hỗn hợp 7 bản ghi → kết quả từng bản ghi (accepted, consent_missing, too_few_points, unknown_entity). Gọi lặp → phản hồi cũ; gửi lại ở lô khác → `duplicate` |
| E2E-12 | Thu hồi thiết bị: pull, push, đăng ký lại → 403 `sync.device_revoked`, có sự kiện outbox |
| E2E-13 | Xuất gói MRV: 202 + job; handler inbox dựng gói, giao lặp bị bỏ qua; `verify.match = true`; SHA-256 đối tượng = `package_sha256` |
| E2E-14 | Không có PII ngoài vault: không cột PII trong `pf_*`/`shared`; quét giá trị mọi bảng (có đối chứng dương); log cả ba server; tệp trong kho đối tượng. Đối chứng ngược: vault tra được hộ theo SĐT |

## 2. Lộ trình G1 (docs/roadmap.md)

| Sprint | Hạng mục | Kiểm thử | Trạng thái |
|---|---|---|---|
| S1 | Đơn vị hành chính có hiệu lực theo sáp nhập 2025 | `TestSuccessorResolutionAfterMerger`, `TestDescendantsAtTime`, `TestResolveCurrentMergerSplitAndReuse`, E2E-01 | **đạt**. Seed là danh mục **chính thức** (QĐ 19/2025/QĐ-TTg; 591 xã mới, 1.664 mã cũ có liên kết kế thừa) — [danh-muc-hanh-chinh.md](../requirements/danh-muc-hanh-chinh.md) |
| S1 | Giống lúa, danh mục | `TestCatalogDeactivateKeepsHistory`, E2E-01, E2E-08 | **đạt** |
| S1 | Tham số có phiên bản, setter ≠ approver | `TestSelfApprovalBlocked`, `TestParamLookupAtEventTime`, `TestParamCorrectionSupersedes`, WEB-E2E `params.spec.ts` | **đạt** |
| S1 | Vault: API mục 10.3, X25519 theo hộ, blind index | `TestCreateResolveDisplayUnwrap`, `TestBlindIndexDuplicatePerTenant`, `TestHTTPClientEndToEnd`, E2E-03, E2E-11 | **đạt** |
| S1 | Bề mặt Admin với MFA | `TestAdminRequiresMFA`, E2E-01, WEB-E2E `navigation.spec.ts` (quản trị chưa MFA → `mfa_required`) | **đạt** |
| S2 | Thửa PostGIS, chồng lấn, diện tích geodesic | `TestGeodesicAreaNearCanTho`, `TestOverlapRules`, `TestClassifyOverlaps`, E2E-05 | **đạt**. Quy tắc chồng lấn đã chốt theo BRD (xem US-PLOT-03). E2E-05 dùng thửa không chồng lấn; chồng lấn do kiểm thử module chứng minh |
| S2 | Cây phân cấp KN | `TestScopeProvinceSeesOnlyItsCommunes`, `TestOrgMoveHistoryAndChain`, E2E-02, E2E-05 | **đạt** |
| S2 | HTX và nông hộ, định danh ở vault | `TestHouseholdDedupeViaVault`, `TestNoPIIColumns`, E2E-03, E2E-14 | **đạt** |
| S2 | `apps/web-ops` | Vitest 119 ca; WEB-E2E 27 ca: `navigation`, `accounts`, `params`, `htx`, `dashboard`, `reports`, `notifications`, `conflicts`, `roster`, `cgh`, `cgh-pins` `.spec.ts` | **đạt**. Chạy trên bản build production ở chế độ giả lập (job `web-e2e`) |
| S2 | Chốt engine đồng bộ (23.1) | ADR-0006 (đồng bộ qua Application API); E2E-11 | **đạt** (quyết định + kiểm thử) |
| S3 | `apps/field-app`: offline 72 giờ, SQLCipher, hàng đợi UUIDv7 + idem_key, vẽ offline, mã hóa ảnh ngay khi chụp | `test/*.dart` (143 ca: auth, outbox, sync_push, sync_pull, boundary, photo_pipeline, envelope_vectors, wipe, inbox_library_directive, push, consent_watermark, tiles_pull, http_contract_g1, roster, …), `offline_flow_http_test.dart` (HTTP thật tới máy chủ giả `dart:io`), `integration_test/app_flow_test.dart` (emulator API 29: SQLCipher + Keystore + giao diện thật; job `field-app-emulator`) | **đạt**. Emulator dùng máy chủ API giả trong tiến trình; phía máy chủ của cùng luồng do E2E-11 chứng minh |
| S4 | `platform/extension`: nhiệm vụ SLA 24 giờ, leo thang | `TestTaskSLAFromParamAtCreation`, `TestEscalationJobOnlyOverdueAndIdempotent`, E2E-07 | **đạt** |
| S4 | Thư viện quy trình | `TestLocalSupplementScope`, `TestAuthorCannotSelfApprove`, E2E-06 | **đạt** |
| S4 | Dashboard 4 KPI | `TestNationalKPIEqualsSumOfProvinces`, E2E-08, WEB-E2E `dashboard.spec.ts` | **đạt** |
| S4 | CGH: cân đối QT-01/02 | `TestComputeBalanceQT01`, `TestClassifyBoundariesQT02`, `TestBalanceUsesNormAtSeasonAndPilotStages`, E2E-09 | **đạt** |
| S4 | Danh mục trường MRV (C-1) | `TestCatalogSoDAndVersionPinning`, E2E-10; app: `http_contract_g1_test.dart` (danh mục qua `GET /v1/mrv/evidence-types`) | **một phần** — cơ chế đạt; **thiếu danh mục chính thức C-1** của khách (hiện là danh mục tạm) |
| S4 | OTLP (ADR-0013) | `TestTraceWithOTel`, `TestTracePropagatesOutboxToInbox` | **đạt** |
| S5 | Bộ sinh dữ liệu tổng hợp (R-95) | `TestRefusesProduction`, `TestPlanDeterministicAndPlausible`, `TestSeedAgainstInProcessStack` ([cmd/seed-synthetic](../../cmd/seed-synthetic)) | **đạt** cho dữ liệu G1. Số đo G3 xem R-95 ở mục 6 |
| S5 | UAT theo ca kiểm thử trích từ prototype `mekong-green` | 39 kiểm thử ở [tests/acceptance](../../tests/acceptance); mỗi ca ghi `// Nguồn:` tệp/ca prototype, bảng đối chiếu trong `doc.go` | **đạt**. 5 ca giữ nguyên quy tắc, 34 ca theo quy tắc mới có ghi `// Khác prototype:` (PII chỉ ở vault, chỉ ghi thêm, chồng lấn theo BRD, Idempotency-Key bắt buộc). Năm phát hiện đã sửa: gán thửa cho nông hộ HTX khác → 422 `geo.household_invalid`; mốc ngưỡng CGH phải tăng dần → 422 `masterdata.param_threshold_order`; khôi phục thửa `POST /v1/plots/{ref}/restore`; mặt nạ SĐT `0912•••678`; mã mùa vụ chuẩn hóa chữ HOA |
| S5 | Diễn tập sao lưu và phục hồi (mục 22) | `TestDrillEndToEndAndDetectsCorruption` ([cmd/drill](../../cmd/drill)), `TestBackupRestoreVerify` ([internal/shared/objsync](../../internal/shared/objsync)); workflow `restore-drill` | **một phần** — logic kiểm đạt; **workflow `restore-drill` chưa có lần chạy xanh** (28/09: hai lần hỏng, một lần đang chạy). Xem [restore-drill.md](../runbooks/restore-drill.md) |

## 3. App Khuyến nông (Backlog v1.0)

### A. Quản trị hệ thống và Master Data

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-ADM-01/02/03 tài khoản, nhập hàng loạt, thao tác loạt | `TestCreateUserUSADM01`, `TestProvinceAdminGrantMatrix`, `TestLockUnlockNeedsReason`, `TestImportXLSXMixedRows`, `TestBulkActions`, `TestAssignmentChange`, `TestIdempotentCreate`, `TestTenantIsolation`, `TestContractHTTPAgainstEmulator` ([internal/platform/accounts](../../internal/platform/accounts)); WEB-E2E `accounts.spec.ts` (lý do khóa < 20 ký tự bị chặn, khóa + nhật ký, ma trận cấp vai tỉnh, `accounts.user_exists`, **thao tác loạt**: chọn nhiều → khóa theo đợt, gửi lại liên kết cho tài khoản đã khóa → lỗi từng tài khoản), chưa MFA | **đạt** (29/09: thêm thao tác loạt trên web `/quan-tri/tai-khoan`). Kích hoạt bằng liên kết một lần thay mật khẩu tạm (X2, ADR-0010). Keycloak Admin REST kiểm bằng emulator HTTP, không có Keycloak thật trong kiểm thử Go |
| HTX epic J (US-HTXSTATUS-01 AC-2, 02 AC-2, 03): vô hiệu hóa/kích hoạt lại HTX khóa/mở tài khoản | `TestOrgDeactivationLocksOnlyThatOrgAndReactivationRestoresOnlyThose` (consumer `pf.accounts.org_status` qua `inbox.Process`), `TestLockUnlockNeedsReason` (sổ chỉ ghi thêm, chuỗi băm), `TestOutboxToInboxExactlyOnceEffect` (đường RabbitMQ) | **đạt** |
| US-ADM-04 đăng nhập, điều hướng theo vai trò và phạm vi | `TestVerifyRejects`, `TestVerifyBuildsPrincipal`, `TestNoOrgSeesNothing`, E2E-05; Vitest `src/nav/nav.test.tsx`; WEB-E2E `navigation.spec.ts` (TW, tỉnh, xã, Cục → `/cgh` và bị chặn khối vận hành, quản trị mở mọi màn hình, chưa MFA → `mfa_required`) | **đạt**. Đăng nhập thật qua Keycloak không có E2E trình duyệt (giả lập OIDC) |
| US-ADM-05/06 PIN 6 số, offline 72 giờ | app: `auth_test.dart` (đúng 6 chữ số, lưu băm; sai 5 lần → khóa 15 phút; hợp lệ đến đúng 72 giờ; lùi đồng hồ bị phát hiện; token chỉ trong kho bí mật), `integration_test/app_flow_test.dart` (đăng nhập → tạo PIN → mở CSDL SQLCipher bằng khóa Keystore) | **đạt**. OIDC ở chế độ kiểm thử trên emulator |
| US-ADM-07 cây phân cấp 3 cấp | E2E-02, `TestAllowedParent`, `TestScopeProvinceSeesOnlyItsCommunes` | **đạt** |
| US-ADM-08 di chuyển nhánh; nhập/xuất Excel | `TestOrgMoveHistoryAndChain`, `TestOrgTreeExportImportRoundTrip`, `TestOrgTreeExportScoped` | **đạt**. `GET /v1/orgs/export.xlsx` → sửa → `POST /v1/orgs/import` (qua CreateOrg/MoveOrg/RenameOrg, sổ ghi lý do); không vô hiệu hóa tổ chức vắng mặt trong tệp. Web `/quan-tri/to-chuc` |

### B. Quản lý HTX và nông hộ

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-HTX-01 danh sách HTX theo phạm vi | E2E-02, E2E-03, `TestScopeProvinceSeesOnlyItsCommunes`, `TestDeactivateReactivateCooperative`, WEB-E2E `htx.spec.ts` | **đạt** |
| US-HTX-02 hồ sơ nông hộ, chống trùng | E2E-03, `TestHouseholdDedupeViaVault`, WEB-E2E `htx.spec.ts` (huy hiệu đồng ý) | **đạt** |
| US-HTX-03 nhập hàng loạt | `TestImportHouseholdsCSV`, `TestImportHouseholdsXLSX`, `TestImportHouseholdsXLSXFormulaRejected`, Vitest `features.test.ts` | **đạt**. CSV UTF-8 và .xlsx; ô công thức bị từ chối cả tệp; web chặn .xls cũ |
| US-HTX-04 khai báo máy cơ giới hóa | E2E-09, `TestSerialUniqueAndStageDerived`, `TestImportUpdateAndReplace` | **đạt** |
| US-HTX-05 đồng bộ nông hộ từ App HTX | — | **ngoài G1 → G2** (App HTX, liên thông KN ↔ HTX bằng sự kiện) |

### C. Vẽ ranh giới thửa

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-PLOT-01 vẽ ranh giới | E2E-05, `TestBoundaryValidationRules`, `TestCorrectionCreatesNewVersion`; app: `boundary_test.dart` (< 4 điểm, thẳng hàng, tự cắt, diện tích trắc địa) | **đạt** |
| US-PLOT-02 tải tệp ranh giới | `TestImportGeoJSONAndKML`, `TestImportShapefileZip`, `TestParseShapefileZip`, `TestParseShapefileRejects`, tests/acceptance `shapefile_test.go` | **đạt**. GeoJSON, KML, Shapefile (.zip) trên web `/quan-tri/nhap-thua`; app hiện trường không có tải tệp (vẽ tay). Hệ toạ độ khác WGS84 → `geo.crs_unsupported` (X9: chuyển VN-2000 cần khách chốt) |
| US-PLOT-03 cảnh báo chồng lấn | `TestOverlapRules`, `TestClassifyOverlaps`; app: `boundary_test.dart` (chặn HTX khác kể cả khi xác nhận, cùng HTX cần xác nhận, lệch diện tích khai báo > 5 %); WEB-E2E `htx.spec.ts` (cùng HTX → "Xác nhận vẫn lưu" với khóa Idempotency mới; HTX khác → không bỏ qua được), `conflicts.spec.ts` (bản đồ xem trước vùng chồng lấn) | **đạt**. Quy tắc chốt theo BRD: HTX khác vượt `geo.overlap_tolerance_pct` (mặc định 1 %, theo thửa nhỏ hơn) → 409 `geo.overlap_cross_coop`; cùng HTX/thửa nháp → 409 `geo.overlap_needs_confirm`; liền kề chung cạnh không tính |
| US-PLOT-04 vẽ offline, tự đồng bộ | E2E-11 (`plot_boundary` accepted; tam giác → rejected), `TestSyncSinkIdempotentAndConflict`; app: `boundary_test.dart` (payload, `declared_area_ha` chuỗi, `correct` theo `boundary_id`, `confirm_overlap` sau `conflict`), `offline_flow_http_test.dart`, `integration_test/app_flow_test.dart` (vẽ khi mất mạng → có mạng + `sync_now` → accepted) | **đạt**. Ngoại tuyến không chặn được máy: chồng lấn → `conflict` + xung đột chờ xử lý (docs/dev/sync-contract.md) |

### E. Liên thông dữ liệu

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-SYNC-01 hai chiều với App HTX | — (G1 có đồng bộ app hiện trường ↔ máy chủ: E2E-11, `TestPushMixedBatchAndReplay`, `TestPullPaginationRoundRobin`, `TestPullScopeChangeResets`) | **ngoài G1 → G2** |
| US-SYNC-02 đồng bộ ngay theo yêu cầu | `TestNudgeScopeAndRateLimit` (`POST /v1/sync/nudge`, thông điệp dữ liệu `sync_now`, 1 lần/5 phút mỗi đích, 30 lần/giờ mỗi người); Vitest `features.test.ts` (429 + `Retry-After`); app: `push_test.dart` (`sync_now` → đồng bộ ngay; khi đang khóa → đồng bộ lúc mở khóa; token `POST /v1/devices/{id}/push-token`), `integration_test/app_flow_test.dart` | **một phần** — đồng bộ ngay theo đơn vị/thiết bị đạt (web `/dong-bo`, app); **thiếu chọn nhóm/loại dữ liệu** (AC) — gắn với liên thông App HTX, G2. FCM thật cần `AG_FCM_*` |
| US-SYNC-03 đối soát xung đột | `TestConflictSLA`, `TestSyncSinkIdempotentAndConflict`, `TestConflictResolutionUI`, `TestConflictDetailBothVersions`, WEB-E2E `conflicts.spec.ts` | **đạt**. Lọc theo entity/SLA, hai phiên bản không PII, giữ phiên bản hoặc bản ghi bù; không tự ghi đè |

### G. Thư viện kỹ thuật và tin tức

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-LIB-01 soạn và xuất bản theo vùng | E2E-06, `TestPublishNewVersionKeepsOld`, `TestProvinceEditorScope`, `TestAuthorCannotSelfApprove` | **đạt** |
| US-LIB-02 hướng dẫn đặc thù địa phương | E2E-06, `TestLocalSupplementScope` | **đạt** |
| US-LIB-03 trích nội dung từ tệp | `TestExtract`, `TestExtractPDFVietnamese`, `TestExtractPDFRejects`, `TestParseToUnicode` | **đạt**. DOCX và PDF có lớp chữ; PDF scan/mã hóa → `content.extract_unsupported` (không OCR) |
| US-LIB-04 tra cứu | `TestSearchAccentInsensitive`, E2E-06 | **đạt** |
| US-LIB-05 lưu và tải offline | E2E-11 (pull `document`), `TestSyncRetireTombstone`, `TestPersonalLibraryUSLIB05`; Vitest `features.test.ts`; app: `inbox_library_directive_test.dart` (lưu/bỏ lưu ngoại tuyến → phát lại theo thứ tự, tài liệu đã gỡ giữ nhãn "đã gỡ", tombstone) | **đạt**. Danh mục cá nhân (migration 000014) + entity `library_item`; web `/thu-vien` tab "Đã lưu"; app "Thư viện · Đã lưu" |
| US-LIB-06 thông báo nội dung mới | E2E-06, `TestAlertPublishedEvent`, `TestAlertFanOutRespectsScope`, `TestDuplicateEventDeliveryNoDuplicates`, `TestFCMPayloadIsDataOnly`; WEB-E2E `notifications.spec.ts` (chuông số chưa đọc, tài liệu/nhiệm vụ mở đúng màn hình, đọc tất cả); app: `inbox_library_directive_test.dart`, `push_test.dart`, `integration_test/app_flow_test.dart` | **đạt**. Chỉ người thuộc vùng áp dụng nhận; FCM chỉ dữ liệu (ADR-0015); web hỏi lại 60 giây (không websocket) |

### I. Dashboard và báo cáo

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-DASH-01 4 KPI theo vai trò | E2E-08, `TestNationalKPIEqualsSumOfProvinces`, `TestClosedPeriodNeedsCorrectionReason`, WEB-E2E `dashboard.spec.ts` | **đạt**. `GET /v1/dashboard` (G-02), huy hiệu "Số liệu tạm tính" |
| US-DASH-02 xuất theo mẫu Bộ NN&PTNT | `TestExportXLSXAsyncViaInboxOnce`, `TestXLSXContent`, `TestPDFVietnamese`, `TestReportDataSowingAndSummary`, `TestProgramProgressFR03`, `TestQuotaAndAnomaly`, `TestExportScope`, `TestMyExportsHistory`, WEB-E2E `reports.spec.ts` | **một phần** — xuất Excel/PDF bất đồng bộ từ mẫu có phiên bản, "Số liệu tạm tính", hạn mức DLP đều đạt; **thiếu mẫu biểu chính thức của Bộ** (bố cục hiện là tạm) |
| US-DASH-03 lịch gửi báo cáo email | `TestScheduleFiresOncePerPeriodAndDelivers`, `TestDeliverySkippedWithoutDirectory`, `TestScheduleSlot`, `TestMailDirectoryEmailOf` (email tra từ Keycloak lúc gửi), **`TestSMTPDeliversOverSTARTTLS`, `TestSMTPImplicitTLS`, `TestSMTPRefusesInsecureOrRejected`** (máy chủ SMTP giả trong tiến trình: STARTTLS trước AUTH/MAIL, tiêu đề tiếng Việt, thân đúng; không gửi bản rõ, không nhận chứng chỉ lạ); WEB-E2E `reports.spec.ts` (lập lịch hằng tháng → đang chạy → ngừng có lý do) | **đạt** (29/09). Bản trước ghi "một phần" vì chưa kiểm đường SMTP và web; `MailDirectory` đã nối pf/accounts ở `assembly` và `cmd/worker` |

### J. Nhiệm vụ hỗ trợ

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-TASK-01 tự sinh nhiệm vụ từ yêu cầu App HTX | `TestCoopManagerCreatesForOwnCooperative` (quản lý HTX tạo nhiệm vụ qua API) | **ngoài G1 → G2** — luồng tự động từ App HTX (≤ 1 phút) thuộc G2 |
| US-TASK-02 tiếp nhận, xử lý, chuyển tiếp, leo thang SLA | E2E-07, E2E-11 (`task_update` accepted), `TestEscalationJobOnlyOverdueAndIdempotent`; app: `consent_watermark_test.dart` (đếm ngược SLA, `action` theo pf/extension), `photo_pipeline_test.dart`; WEB-E2E `notifications.spec.ts` (thông báo nhiệm vụ mở đúng màn hình) | **đạt** |
| US-TASK-03 giám sát và gửi chỉ đạo | `TestDirectiveAckRate`, `TestOfficerOutsideCommuneCannotSeeOrAssign`; WEB-E2E `notifications.spec.ts` (mở chỉ đạo qua deep link, xác nhận đã đọc); app: `inbox_library_directive_test.dart` (xác nhận ngoại tuyến → `POST /ack`, chỉ người nhận) | **đạt**. Web `/chi-dao`: ban hành tới vùng chọn, tỷ lệ xác nhận, người chưa xác nhận |
| US-TASK-04 phân công hàng loạt | `TestBulkAssign` | **đạt** |

### K. Danh bạ trực

| Story | Kiểm thử | Trạng thái |
|---|---|---|
| US-DIR-01 cập nhật trạng thái trực | `TestRosterDutyStatus`, `TestDutyAt`; WEB-E2E `roster.spec.ts` (KNV xã tự khai "Tạm nghỉ"); app: `roster_test.dart` (`POST /roster/status` có Idempotency-Key, trả hạn hiệu lực) | **đạt** (29/09: màn "Danh bạ trực" trên web `/danh-ba` và app) |
| US-DIR-02 tra cứu danh bạ, gọi/nhắn 1 chạm | `TestRosterDutyStatus` (không ai trực → cán bộ TTKN tỉnh dự phòng), **`TestRosterContactOneTap`**, **`TestStaffContactsPhoneOf`**; WEB-E2E `roster.spec.ts` (tra theo xã, "Liên hệ" → liên kết `tel:`/`sms:`, chưa khai số → `extension.contact_unavailable`, xã không ai trực → gọi cán bộ tỉnh); Vitest `src/lib/dial.test.ts`; app: `roster_test.dart` (tra số lúc bấm → mở `tel:`/`sms:` qua url_launcher, lỗi rõ khi chưa khai số/mất mạng/máy không có ứng dụng gọi) | **đạt** (29/09). Số điện thoại công vụ nằm ở tài khoản Keycloak (pf/accounts), đọc qua `GET /v1/extension/roster/{id}/contact` lúc bấm, `Cache-Control: no-store`, không lưu ở pf hay trên máy, không nạp sẵn cùng danh bạ (R-04, R-14). Chỉ cán bộ người gọi thấy được (kể cả cán bộ tỉnh dự phòng); ngoài phạm vi → 404 |
| US-DIR-03 lịch phân ca | `TestRosterDutyStatus` (`/roster/shifts`, cán bộ xã không phân ca được); WEB-E2E `roster.spec.ts` (TTKN tỉnh khai báo ca → trạng thái "Theo ca") | **đạt** |

## 4. Bản đồ CGH (BRD v1.3)

| FN | Nội dung | Kiểm thử | Trạng thái |
|---|---|---|---|
| FN-01 | Tài khoản và phân quyền | `TestViewerReadsAllButCannotWrite`, `TestCreateUserUSADM01`, `TestLockUnlockNeedsReason`, `TestGrantMatrix`, **`TestPlatformAdminCannotBeLockedByLowerAdminOrSelf`** (không khóa được quản trị cuối cùng: chỉ quản trị nền tảng thao tác trên quản trị nền tảng, không tự khóa), **`TestKeycloakRealmCoversRolesAndClaims`** (không đăng ký công khai; sai 5 lần trong 15 phút → khóa tạm; chính sách độ mạnh mật khẩu); WEB-E2E `navigation.spec.ts` (Cục chỉ thấy CGH + báo cáo, khối vận hành bị chặn) | **đạt**. "Mật khẩu tạm" thay bằng liên kết UPDATE_PASSWORD một lần (X2, ADR-0010) |
| FN-02 | Danh mục và cấu hình | E2E-01, E2E-09 (ngưỡng 85/60 đọc từ tham số đã duyệt), `TestSelfApprovalBlocked` | **đạt** |
| FN-03 | Hồ sơ HTX | E2E-02, `TestTaxCodeUniquePerTenant` | **đạt** |
| FN-04 | Chủ sở hữu máy | E2E-09 (vault, trùng SĐT → 409), `TestCodesAssignedOnceAndStableOnProvinceMove` | **đạt** |
| FN-05 | Hồ sơ máy | E2E-09, `TestSerialUniqueAndStageDerived`, `TestMachineCountAtT`, `TestImportUpdateAndReplace`, `TestImportMachinesXLSXReplace` | **đạt**. CSV hoặc .xlsx |
| FN-06 | Lịch mùa vụ và diện tích theo vụ | E2E-09 (nhập tay), `TestLockedManualAreaNotOverwritten`, `TestValidateCalendar` | **đạt** |
| FN-07 | Tích hợp App HTX | `TestResolveAreaQT03` (chỉ port `AreaFeed`, fallback nhập tay) | **ngoài G1 → G2** |
| FN-08 | Bản đồ và trực quan hóa | `TestSummarizeBottleneck` (coverage theo cấp); `TestBalanceUsesNormAtSeasonAndPilotStages` (panel HTX `GET /v1/mechanization/cooperatives/{ref}/panel` (máy, nhãn chủ sở hữu đã che, mức đáp ứng đã lưu); Vitest `src/cgh/pins.test.ts` (vị trí hồ sơ HTX / tâm xã "vị trí tạm", màu theo khâu lọc, tra cứu không dấu theo tên/Mã HTX/xã, **1.200 HTX dựng ghim + lọc < 1 giây**); **WEB-E2E `cgh.spec.ts`** (chú giải ngưỡng; tỉnh tô đúng màu chú giải; Tây Ninh chưa có dữ liệu → xám; lọc khâu áp dụng ngay; bấm tỉnh → chi tiết), **`cgh-pins.spec.ts`** (MapLibre gom cụm ở zoom toàn vùng, tách ghim khi phóng to, ghim tô màu mức đáp ứng; tra cứu nhanh → bay tới ghim → panel HTX: mức theo 4 khâu, máy theo khâu kèm tình trạng + [Nhập tay]/[App HTX], chủ sở hữu chỉ nhãn + SĐT che; HTX chưa có toạ độ → "vị trí tạm"; không có kết quả → "Không có kết quả phù hợp"); `navigation.spec.ts` (Cục vào `/cgh`) | **đạt** (29/09). Ghim từ kết quả cân đối **đã lưu** cấp HTX (`GET /v1/mechanization/coverage?level=cooperative`) + vị trí hồ sơ HTX (`GET /v1/cooperatives`); không thêm API. Vòng rỗng = vị trí tạm. Chỉ xem (BR-04) |
| FN-09 | Cân đối và dự báo | E2E-09, `TestBalanceUsesNormAtSeasonAndPilotStages`, `TestSavedResultsImmutableAndChainVerifies`, `TestForecastFN09AndCoopCodeG10`, `TestProjectAreaFN09`, Vitest `features.test.ts`, WEB-E2E `navigation.spec.ts` (nhãn "Phương pháp dự báo tạm thời") | **một phần** — cân đối và dự báo chạy, kết quả lưu bất biến; **thiếu xác nhận phương pháp dự báo của DCRD** (`cgh.forecast/v1` là tạm) |
| FN-10 | Báo cáo và xuất dữ liệu | `TestReportDataEqualsSavedResults`, `TestCGHExportEqualsSavedResults`, `TestTemplateVersionsAdmin`, WEB-E2E `reports.spec.ts` (mẫu CGH cho Cục) | **đạt**. Xuất từ kết quả cân đối **đã lưu**, nhãn nguồn, Admin quản lý mẫu |
| FN-11 | Dashboard tổng quan | E2E-09 (`total_machines`), WEB-E2E `navigation.spec.ts` (cổng Cục) | **đạt** |

## 5. GIS nền (Backlog v1.1)

| FN | Nội dung | Kiểm thử | Trạng thái |
|---|---|---|---|
| FN-04 | Ranh giới HTX/Hub | `TestAdminBoundaryAndCommuneContainment` (ranh giới hành chính, toạ độ điểm HTX) | **ngoài G1 → G2** — polygon HTX và Hub thuộc `mkg/hub` |
| FN-05 | Thửa ruộng: vẽ, sửa, nhập | E2E-05, `TestImportGeoJSONAndKML`, `TestImportShapefileZip`, `TestCorrectionCreatesNewVersion`, `TestBBoxLimitAndScope` | **đạt**. "Xóa" là `retire` + khôi phục, không xóa cứng (X1) |
| FN-06 | Kho/Hub | — | **ngoài G1 → G2** |
| FN-07 | Dữ liệu nền theo zoom | `TestAdminBoundaryAndCommuneContainment`, `TestAdminBoundariesByParentG05` (ranh giới hành chính theo cấp, `zoom`) | **ngoài G1 → G2** cho lớp giao thông, thủy hệ, tự nhiên và tile (cùng `pf/waterway`). Phần G1 — ranh giới hành chính theo cấp/zoom — đạt |

## 6. Cổng kiến trúc

| Mã | Nội dung | Kiểm thử | Trạng thái |
|---|---|---|---|
| RB-1 | Chỉ ghi thêm, sửa bằng bản ghi bù | `TestAppendOnlySixWays`, `TestCatalogGates`, `TestChainSequenceAndTamperDetection`, `TestCorrectionCreatesNewVersion`, `TestCorrectionChain` | **đạt** |
| RB-2 | Cách ly tenant | `TestTenantLeakParallel` (R-32, qua PgBouncer trong CI), `TestTenantIsolation` của từng module | **đạt**. E2E dùng một tenant |
| RB-3 | PII xóa được | `TestErasure` (vault), `TestRB3ErasureEvidenceUnreadable` (E2E) | **đạt**. Xóa định danh → `GET /v1/files/{ref}/content` 410 `vault.erased`; sổ pf còn nguyên, chuỗi băm xác minh được |
| RB-4 | Giấy phép | CI `go-licenses` + `deploy/policies/allowed-licenses.txt`; `license-checker` (npm, job `web-ops`); rà `pubspec.lock` ([apps/field-app/README.md](../../apps/field-app/README.md)) | **đạt** (cổng CI). Lưu ý FCM/Google Play services cần rà pháp lý trước phát hành |
| RB-5 | Ranh giới module | `go run ./tools/archlint ./...`; harness E2E dùng chính `assembly.Build` | **đạt** |
| RB-6 | Đối soát chéo số đo ở biên | — | **ngoài G1 → G3** (module đối soát mục 13, cùng measurement/edge-agent) |
| RB-7 | Dữ liệu trong lãnh thổ | `TestEachEndpointOutsideTerritoryFails`, `TestProductionRequiresAllowlist`, `TestNonPIIAllowlistOnlyForPushChannels`, `TestServiceMainsCallGuard` ([internal/shared/residency](../../internal/shared/residency)) | **đạt** cho cấu hình: `AG_ENV=production` → mọi đích DB/S3/OTLP/SMTP/FCM/Keycloak/vault/AMQP phải thuộc `AG_RESIDENCY_ALLOWLIST`. Pháp nhân/hạ tầng nhà cung cấp là rà soát hồ sơ ([residency.md](../runbooks/residency.md)) |
| R-04 | PII chỉ ở vault | E2E-14, `TestNoPIIColumns`, archlint `agboundary` | **đạt** |
| R-10 | Decimal cho số đo | archlint `agnofloat`, E2E-08, E2E-09 | **đạt** |
| R-11 | UUIDv7 | `TestParseRequiresV7`, E2E-11 | **đạt** |
| R-12 | Giờ nghiệp vụ Việt Nam | `TestBusinessDateUsesVietnamTime`, `TestDayOfUsesVietnamTime`, E2E-08 | **đạt** |
| R-14 | Không PII trong log | E2E-14 (log cả ba server), archlint `agpiilog`, `TestSeedAgainstInProcessStack` | **đạt** |
| R-15, R-63 | RFC 9457 + trace_id | `TestWriteRFC9457`, mọi `wantProblem` của E2E | **đạt** |
| R-16 | Tenant và người gọi chỉ lấy từ token | E2E (không có tenant trong thân request), `TestRawTokenForwarding` | **đạt** |
| R-40 | Kho chỉ giữ bản mã | E2E-11, E2E-14 | **đạt** |
| R-42 | Đọc PII phải có mục đích | E2E-11 (`sync.purpose_required`), `TestPurposeRequired` | **đạt** |
| R-51 | Idempotency-Key 48 giờ | E2E-01, E2E-11, `TestIdempotent*` của từng module, `TestSeedAgainstInProcessStack` | **đạt** |
| R-52 | Việc dài: 202 + job | E2E-13, `TestExportXLSXAsyncViaInboxOnce` | **đạt** |
| R-53 | Phân trang cursor | `TestCursorPagination`, E2E-11 | **đạt** |
| R-54 | Tự xác minh token | `TestVerifyRejects`; vault xác minh token chuyển tiếp trong E2E | **đạt** |
| R-56 | Người duyệt ≠ người đặt | E2E-06, E2E-10, `TestSelfApprovalBlocked`, `TestCatalogSoDAndVersionPinning`, WEB-E2E `params.spec.ts` | **đạt** |
| R-61 | Hiệu quả đúng một lần | E2E-13, `TestOutboxToInboxExactlyOnceEffect` | **đạt** |
| R-62 | DLQ | `TestDLQDepth` | **đạt** |
| R-64 | Hợp đồng sự kiện | `go run ./tools/contractcheck` | **đạt** |
| R-95 | Dữ liệu tổng hợp, không có người thật | `cmd/seed-synthetic`: từ chối `AG_ENV=production`, tất định theo `-seed`, thửa 0,3–3 ha không chồng lấn, SĐT 09990/09991xxxxx | **ngoài G1 → G3** cho số cuộn, sản lượng quy đổi, độ ẩm (cần module đo lường G3). Phần G1 đạt (S5) |

## 7. Lỗi phát hiện khi dựng kiểm thử (đã sửa tối thiểu)

1. **pf/content: mã xã của bộ seed không dùng được cho phạm vi cấp xã.** Regex loại mã kiểu `92-X-NINH-KIEU`. Đã đổi cho khớp quy tắc mã của pf/masterdata. Nay seed dùng mã xã chính thức 5 chữ số (QĐ 19/2025/QĐ-TTg); E2E dùng `31135` (Phường Ninh Kiều) và `30307` (Phường Long Xuyên).
2. **pf/extension: `sown_on` bị coi là "ở tương lai" trước 07:00 giờ Việt Nam** vì parse theo UTC. Đã parse theo `clock.Zone()` (R-12).
3. **pf/party: kho định danh từ chối dữ liệu (4xx) nhưng API trả 503** → client thử lại mãi. Đã trả nguyên problem 4xx; lỗi hệ thống vẫn 503.
4. **web-ops `/cgh` (29/09): chú giải mức đáp ứng khác màu vùng tô bản đồ.** Vùng tô dùng màu máy chủ (`domain.CoverageColors`, ví dụ Đủ `#1F7A38`), chú giải dùng bảng màu riêng của web (`#22A45D`) — người xem không đối chiếu được (FN-08 AC-01). Đã: chú giải dùng `legend[].color` máy chủ trả; bảng màu mặc định của web trùng màu máy chủ; `cgh.spec.ts` kiểm từng vùng.

## 8. Giới hạn đã biết của bộ kiểm thử

- **Router kho định danh.** `vaulttest` lặp lại router của `cmd/identity-vault` (NewRouter nằm trong package main). Kiểm thử E2E không phủ mTLS (có `TestMTLS` riêng). Application/Admin API thì harness dùng chính `assembly`.
- **Worker.** E2E gọi thẳng job và handler inbox, không qua RabbitMQ. Đường RabbitMQ có `TestOutboxToInboxExactlyOnceEffect`; Compose đầy đủ có job `compose-smoke`.
- **Keycloak.** Kiểm thử Go dùng issuer giả và emulator Keycloak Admin REST; web E2E và emulator Android dùng OIDC giả. Realm thật chỉ được dựng trong `compose-smoke`. `cmd/seed-synthetic` lấy token bằng mật khẩu + OTP (`kctoken.go`) để có `amr` cho Admin API.
- **Web E2E** chạy trên backend giả lập theo hợp đồng; không có E2E trình duyệt nối backend Go thật.
- **Email.** Đường SMTP kiểm bằng máy chủ SMTP giả trong tiến trình (có TLS); chưa gửi qua máy chủ SMTP thật trong lãnh thổ.
