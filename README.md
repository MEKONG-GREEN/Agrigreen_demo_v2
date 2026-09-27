# Agrigreen_demo_v2

Nền tảng AgriGreen được viết lại từ đầu theo **Thiết kế kiến trúc hệ thống v3.0** (15/09/2026). Hệ thống là một Go modular monolith, chạy trên PostgreSQL 17 + PostGIS.

**Trạng thái:** đang ở cổng **G0**: khung repo, `shared/*`, mẫu bảng sổ, `WithTenantTx`, kiểm thử rò rỉ tenant và CI với các cổng ở mục 20. Kế hoạch G1–G6 nằm ở [docs/roadmap.md](docs/roadmap.md).

## Đã có trong G0

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
cmd/            api-platform, api-internal, identity-vault, worker, outbox-publisher, migrate, ingest*, edge-agent*
internal/
  platform/     party (G0) · masterdata, geo, waterway, simulation, mrv, metering, metrics (G1+)
  mkg/          fieldops, measurement, hub, logistics, orders, costing, acctbridge (G2+)
  vault/        identity (G1)
  shared/       ledger, outbox, inbox, idempotency, tenant, money, mass, clock, authz, problem, telemetry, httpx, db, dbadmin, ids, pgtest
contracts/      openapi/, events/, publication/
migrations/     shared/ (áp cho mọi database), pf/, mkg/, vault/
tools/          archlint, migratecheck, contractcheck
deploy/         compose/ (ĐTK 1), policies/
docs/           adr/, requirements/, roadmap.md, dev-setup.md, errors.md
apps/           web-ops, web-sim, field-app (G1)
optimizer/      Python (G2)
```
`*` là khung giữ chỗ cho G3.

## Chạy

Cài môi trường xem [docs/dev-setup.md](docs/dev-setup.md). Cần Go 1.25+ và PostgreSQL 17.

```bash
go test ./...
```

```bash
go run ./tools/archlint ./... && go run ./tools/migratecheck migrations && go run ./tools/contractcheck
```

Kiểm thử tích hợp cần `AG_TEST_PG_ADMIN_URL` và `AG_DB_APP_PASSWORD`. Nếu thiếu, trên máy dev các kiểm thử này được bỏ qua; khi `CI=true` thì build hỏng.

## Tài liệu nguồn

Các tài liệu gốc nằm ở `G:\My Drive\Development`: kiến trúc v3.0, BRD/Backlog GIS, Bản đồ CGH, App HTX, App Khuyến nông. Bản tổng hợp và các xung đột yêu cầu nằm ở [docs/requirements/tong-hop.md](docs/requirements/tong-hop.md).
