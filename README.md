# Agrigreen_demo_v2

Nền tảng AgriGreen được viết lại từ đầu theo **Thiết kế kiến trúc hệ thống v3.0** (15/09/2026). Hệ thống là một Go modular monolith, chạy trên PostgreSQL 17 + PostGIS.

**Trạng thái (29/09/2026):** đã chốt mã cổng **G1** (ĐTK 1: cán bộ khuyến nông, TTKNQG, Cục Kinh tế hợp tác; một tenant). Đối chiếu yêu cầu → kiểm thử: [docs/uat/g1-acceptance.md](docs/uat/g1-acceptance.md) — 94 yêu cầu, 80 đạt, 5 một phần (chủ yếu chờ khách chốt), 9 thuộc G2/G3. Kế hoạch G1–G6: [docs/roadmap.md](docs/roadmap.md).

## Đã có trong G1

| Thành phần | Nội dung | Kiểm chứng |
|---|---|---|
| `pf/masterdata` | Đơn vị hành chính có hiệu lực theo sáp nhập 2025 (danh mục chính thức QĐ 19/2025), giống lúa, danh mục, tham số có phiên bản (người đặt ≠ người duyệt) | kiểm thử module, E2E-01 |
| `pf/party` | Cây TW → Tỉnh → Xã, HTX, nông hộ (định danh ở vault), đồng ý theo mục đích | E2E-02..04 |
| `pf/geo` | Thửa PostGIS, diện tích geodesic, chồng lấn theo BRD, nhập GeoJSON/KML/Shapefile, ranh giới hành chính theo zoom | E2E-05, `TestOverlapRules` |
| `pf/content` | Thư viện quy trình theo vùng, duyệt, trích DOCX/PDF, tra cứu không dấu, "Đã lưu" | E2E-06 |
| `pf/extension` | Nhiệm vụ SLA 24 giờ + leo thang, chỉ đạo khẩn, báo cáo hiện trường, dashboard 4 KPI, danh bạ trực (gọi/nhắn 1 chạm) | E2E-07/08 |
| `pf/mechanization` | CGH: chủ máy, máy, lịch mùa vụ, cân đối QT-01/02 lưu theo vụ, dự báo (phương pháp tạm) | E2E-09 |
| `pf/mrv` | Danh mục trường MRV (C-1 tạm), minh chứng, gói xuất có băm | E2E-10, E2E-13 |
| `pf/sync` | Đồng bộ app hiện trường qua Application API (ADR-0006), xung đột + đối soát | E2E-11/12 |
| `pf/notify` | Hộp thư theo phạm vi, đẩy FCM chỉ dữ liệu (ADR-0015), đồng bộ ngay | kiểm thử module |
| `pf/accounts` | Tài khoản cán bộ qua Keycloak Admin REST, ma trận cấp vai, khóa/mở, thao tác loạt, nhập tệp | kiểm thử + emulator Keycloak |
| `pf/reports` | Xuất Excel/PDF bất đồng bộ từ mẫu có phiên bản, lịch gửi email (SMTP TLS) | kiểm thử + SMTP giả |
| `vault/identity` | Kho định danh tách database: X25519 theo hộ, blind index, mở DEK có mục đích, xóa được (RB-3) | E2E-14, `TestErasure` |
| `apps/web-ops` | Next.js: vận hành KN (TW/tỉnh/xã), cổng CGH cho Cục (ghim HTX gom cụm, panel máy, tra cứu), 8 màn quản trị | Vitest 119, Playwright 27 |
| `apps/field-app` | Flutter Android: ngoại tuyến 72 giờ + PIN, SQLCipher, hàng đợi đúng một lần, vẽ ranh giới, ảnh mã hóa, hộp thư/đẩy, danh bạ trực | 143 kiểm thử + emulator |
| Công cụ | `cmd/seed-synthetic` (R-95), `cmd/drill` + `cmd/objsync` (diễn tập phục hồi), `cmd/worker` (job, consumer, lịch báo cáo) | kiểm thử của từng lệnh |

CI GitHub (`.github/workflows/ci.yml`, 14 job): cổng tĩnh (gofmt, vet, golangci-lint, archlint, migratecheck, contractcheck) → kiểm thử Go trên PostgreSQL + PgBouncer → web (check:api, lint, typecheck, Vitest, build, giấy phép npm) → Playwright → Flutter (format, analyze, test, build apk) → tích hợp trên emulator Android → bảo mật (govulncheck, go-licenses, gitleaks, trivy) → build/SBOM/quét/ký 6 image → `compose-smoke`. Workflow `restore-drill` chạy hằng tuần.

## Nền móng G0

| Hạng mục kiến trúc | Ở đâu | Kiểm chứng bằng |
|---|---|---|
| Sổ chỉ ghi thêm, 3 lớp (mục 8.2) | [migrations/shared/000001_baseline.up.sql](migrations/shared/000001_baseline.up.sql) | `TestAppendOnlySixWays`: 6 phép sửa/xóa + TRUNCATE phân vùng đều thất bại |
| Chuỗi băm, `verify_chain`, neo hằng ngày | `shared.fn_chain`, `shared.anchor_chains` | `TestChainSequenceAndTamperDetection`, `TestConcurrentAppendsSameStream` |
| RLS theo tenant, một policy (mục 9) | `shared.apply_tenant_rls`, [tenant.go](internal/shared/tenant/tenant.go) | `TestTenantLeakParallel` (R-32), `TestNoTenantContextSeesNothing` (R-31) |
| Cổng catalog (R-20, RB-2) | `shared.v_missing_immutability`, `shared.v_missing_rls` | `TestCatalogGates` + kiểm thử ngược |
| Outbox/inbox + RabbitMQ (mục 12) | [outbox](internal/shared/outbox), [inbox](internal/shared/inbox) | `TestOutboxToInboxExactlyOnceEffect` (R-61) |
| Idempotency-Key 48 giờ (R-51) | [idempotency](internal/shared/idempotency) | `TestIdempotentCreate` |
| RFC 9457, trace_id (R-15, R-63) | [problem](internal/shared/problem), [httpx](internal/shared/httpx) | `TestWriteRFC9457`, `TestFromHidesRawErrors` |
| OIDC: tự xác minh issuer/aud/chữ ký, tenant chỉ từ token (R-54, R-16) | [authz](internal/shared/authz) | `TestVerifyRejects` |
| Phân tách nhiệm vụ (R-56) | `authz.EnsureDistinctApprover` | `TestSeparationOfDuties` |
| Decimal cho tiền và khối lượng (R-10) | [money](internal/shared/money), [mass](internal/shared/mass) | kiểm thử đơn vị + `agnofloat` |
| UUIDv7 (R-11), giờ nghiệp vụ Việt Nam (R-12) | [ids](internal/shared/ids), [clock](internal/shared/clock) | kiểm thử đơn vị + `agnotimenow` |
| Mỗi module một vai DB (R-05), vault tách database (R-04) | [dbadmin](internal/shared/dbadmin/dbadmin.go) | CONNECT theo database |
| Module mẫu đủ 5 lớp | [internal/platform/party](internal/platform/party): đồng ý theo mục đích (mục 10.5) | API end-to-end: [router_test.go](cmd/api-platform/router_test.go) |
| Ranh giới module R-02/03/04, PII R-14 | [tools/archlint](tools/archlint) | `analysistest` |
| Migration R-23/R-25, hợp đồng R-64, van công bố | [tools/migratecheck](tools/migratecheck), [tools/contractcheck](tools/contractcheck) | kiểm thử của từng công cụ |

## Lệch so với tài liệu kiến trúc

Có hai chỗ khi dựng G0 phát hiện mẫu trong tài liệu v3.0 chạy không đúng, nên đã sửa và ghi ADR:

1. **Policy RLS mục 9:** `current_setting('app.tenant_id', true)::uuid` báo lỗi trên kết nối đã từng dùng SET LOCAL, vì hàm trả `''` thay vì NULL. Đã sửa bằng `nullif(...)`. Xem [ADR-0005](docs/adr/0005-rls-mot-policy-moi-bang.md).
2. **Mẫu bảng sổ mục 8.1:** trên bảng phân vùng không thể khai báo `UNIQUE (tenant_id, idem_key)`. Tính duy nhất được bảo đảm qua `shared.ledger_idem` và `shared.chain_head`. Xem [ADR-0012](docs/adr/0012-duy-nhat-tren-bang-so-phan-vung.md).

## Cấu trúc

```
cmd/            api-platform (+ assembly: gốc lắp ráp duy nhất), api-internal, identity-vault, worker, outbox-publisher,
                migrate, seed-synthetic, drill, objsync, ingest*, edge-agent*
internal/
  platform/     masterdata, party, geo, content, extension, mechanization, mrv, sync, notify, accounts, reports (G1)
                waterway, simulation, cropcycle (G2) · metering, metrics (G3+)
  mkg/          fieldops, measurement, hub, logistics, orders, costing, acctbridge (G2+, chưa có mã)
  vault/        identity
  shared/       ledger, outbox, inbox, idempotency, tenant, money, mass, clock, authz, problem, telemetry, httpx, db,
                dbadmin, ids, pgtest, envelope, objstore, objsync, residency, schedule, tabular, leader, opsjobs
contracts/      openapi/ (application.yaml, admin.yaml), events/, publication/
migrations/     shared/ (áp cho mọi database), pf/, mkg/, vault/
tests/          e2e/ (kịch bản G1 14 bước), acceptance/ (39 ca từ prototype)
tools/          archlint, migratecheck, contractcheck
deploy/         compose/ (ĐTK 1: PostgreSQL, PgBouncer, RabbitMQ, Keycloak, SeaweedFS, OTel), policies/
docs/           adr/, requirements/, uat/, runbooks/, dev/, roadmap.md, dev-setup.md, errors.md
apps/           web-ops (Next.js), field-app (Flutter)
optimizer/      Python (G2)
```
`*` là khung giữ chỗ cho G3.

## Chạy

Cài môi trường xem [docs/dev-setup.md](docs/dev-setup.md): Go 1.26, PostgreSQL 17 + PostGIS, Node 24, Flutter (Android SDK cho emulator). Trên Windows, chạy `. .\scripts\dev-env.ps1` trước.

```bash
go test -count=1 ./...
go run ./tools/archlint ./... && go run ./tools/migratecheck migrations && go run ./tools/contractcheck
```

Kiểm thử tích hợp cần `AG_TEST_PG_ADMIN_URL` và `AG_DB_APP_PASSWORD`. Nếu thiếu, trên máy dev các kiểm thử này được bỏ qua; khi `CI=true` thì build hỏng.

```bash
cd apps/web-ops && npm ci && npm run check:api && npm run lint && npm run typecheck && npm test && npm run build
CI=1 npm run e2e                  # Playwright trên bản build giả lập (npm run e2e:install một lần)
npm run dev:mock                  # giao diện với dữ liệu giả lập, không cần backend
```

```bash
cd apps/field-app && flutter pub get && flutter analyze --fatal-infos && flutter test
flutter test integration_test -d emulator-5554
```

Toàn bộ stack Local/Dev: `scripts/gen-dev-secrets` rồi `docker compose -f deploy/compose/docker-compose.yml up -d --build --wait` (xem [docs/dev-setup.md](docs/dev-setup.md)).

## Tài liệu nguồn

Các tài liệu gốc nằm ở `G:\My Drive\Development`: kiến trúc v3.0, BRD/Backlog GIS, Bản đồ CGH, App HTX, App Khuyến nông. Bản tổng hợp và các xung đột yêu cầu nằm ở [docs/requirements/tong-hop.md](docs/requirements/tong-hop.md).
