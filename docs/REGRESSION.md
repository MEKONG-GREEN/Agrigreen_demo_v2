# Hồi quy giữa các module G1

Mốc kiểm toán W11.43: `42d20c08` (dev trước khi rebase). Sổ này ghi đường truyền
phụ thuộc và lệnh chấp nhận; các quy tắc nghiệp vụ và ca nghiệm thu nằm ở `docs/uat/`.
Chạy các lệnh DB bằng `scripts/pg-ephemeral.ps1 run go test -v -count=1 -p 1 -timeout 55m ...`.
Không coi kết quả SKIP là đạt.

## Bằng chứng phụ thuộc

- `rg -l 'internal/platform/(forms|notify|extension)/port' internal cmd tests -g '*.go'`.
- `internal/platform/{forms,extension,notify}/module.yaml`: depends, provides và consumes do modgen sinh.
- Các trường sự kiện mới: `pf.forms.submission.recorded/v1` → consumer cảnh báo extension;
  `pf.extension.cnts_alert.changed/v1`, `pf.extension.cnts_form.reminded/v1` → notify.
- Cập nhật lại scan khi đổi namespace/port/sự kiện. Không suy từ danh sách cũ để miễn kiểm tra.

## pf/forms

Downstream consumers: extension (reportcycle, khtcform, khtcperiod, cntsalert), reports,
assembly API và worker; tests/kn77, tests/e2e/g5_scenario là bên kiểm chứng liên module.
Regression acceptance command: `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/forms/... ./internal/platform/extension/cntsalert/... ./internal/platform/reports/... ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77`.
Propagation rule: thay port, vòng đời bài nộp hoặc sự kiện → chạy các bên tiêu thụ trên;
chỉ sửa nội bộ và giữ nguyên hợp đồng → ghi lý do miễn cụ thể, không miễn mặc định.

## pf/extension

Downstream consumers: notify (sự kiện), reports (port nguồn), assembly API, worker,
web-ops (OpenAPI/mock/menu), tests/kn77. Party cung cấp menu bị sửa trong W11.43.
Regression acceptance command: `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/extension/cntsalert/... ./internal/platform/notify/... ./internal/platform/party/... ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77`.
Propagation rule: thay cảnh báo/sự kiện/menu → chạy notify, party, assembly, worker,
kn77 và web: `npm run gen:api`, `npm run typecheck`, `npm test -- --maxWorkers=1 --pool=threads`, `npm run build:mock`.

## pf/notify

Downstream consumers: assembly và worker nối consumer/job; đồng bộ hộp thư hiện trường
qua hợp đồng notify hiện hữu. W11.43 thêm loại thông báo, giữ nguyên hình dạng inbox.
Regression acceptance command: `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/notify/... ./cmd/api-platform/assembly ./cmd/worker`.
Regression acceptance command (G5): `go test -v -count=1 -p 1 -timeout 55m -run '^TestG5ThirdPartyToStatement$' ./tests/e2e`.
Propagation rule: đổi payload inbox hoặc port → bổ sung kiểm thử client/mobile;
thêm template sự kiện nhưng giữ inbox/port → kiểm cả template cũ trong notify và gốc lắp ráp.

## Cổng dùng chung

`go build ./...`, `go vet ./...`, `go run ./tools/modgen -check`,
`go run ./tools/migratecheck -base origin/dev migrations`, `go run ./tools/archlint ./...`,
`go run ./tools/contractcheck`, `go run ./tools/docscheck`, `go run ./tools/mfaflagcheck`,
`golangci-lint run ./...`; thêm `go run ./tools/knboundary` sau KNSPLIT-0.
Migration chạy nhóm `SchemaIsolation|RLSExplain|ModuleGroup|LoginRoles|NoCrossModule|LedgerAddColumn`.

## Kết quả W11.43

- Core: `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/forms/... ./internal/platform/extension/cntsalert/... ./internal/platform/notify/...` — mã thoát 0, PostgreSQL thật, không SKIP. Có `TestFormsSubmissionFeed_W1143`, `TestCNTSAlerts_W1143`, `TestCNTSNotifications_W1143` PASS.
- Sau sửa CNTS-SCOPE-001: chạy lại `./internal/platform/extension/cntsalert/...` với cùng tham số và cụm PostgreSQL tạm — mã thoát 0; `TestProvinceAlertScope_W1143` và HTTP `TestCNTSAlerts_W1143` PASS. Thay đổi chỉ thu hẹp điều kiện đọc cảnh báo theo tỉnh; không đổi port/sự kiện/payload.
- Downstream: `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/platform/party/... ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77` — mã thoát 0, PostgreSQL thật, không SKIP; bao gồm các kịch bản KN77, CNTS tổng hợp, phiếu KHTC và cập nhật nguồn KHTC. Toàn bộ hợp lệnh hồi quy đã đạt.
- `TestWebMenuFixture` (AG_UPDATE_WEB_FIXTURES=1) rồi `TestG5ThirdPartyToStatement` — mã thoát 0; golden menu không thay đổi byte.
- Cổng migration `SchemaIsolation|RLSExplain|ModuleGroup|LoginRoles|NoCrossModule|LedgerAddColumn` — mã thoát 0, PostgreSQL thật, không SKIP; RLS EXPLAIN kiểm 466 bảng, 0 bỏ qua.
- Web: gen:api, typecheck, 513 test / 58 tệp (`--maxWorkers=1 --pool=threads`), build:mock — mã thoát 0. Không đổi ngưỡng timeout.
- Build, vet, modgen-check, migratecheck-base, archlint, contractcheck, docscheck, mfaflagcheck, golangci-lint — mã thoát 0. Sau sửa phạm vi chạy lại các cổng tĩnh (ngoại trừ build toàn repo đã đạt trước đó; package sửa được biên dịch lại trong Go test).
- Giới hạn nghiệp vụ/diễn tập thực tế: [ma trận CNTS](uat/cnts-v3.7-acceptance.md). Không miễn bên tiêu thụ nào trong hợp lệnh đã chọn.
