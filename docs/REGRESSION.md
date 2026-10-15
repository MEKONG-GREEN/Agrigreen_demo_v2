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

## pf/hrm — W11.24 (mốc dev fddf06a1)

Dependency evidence: `rg -l 'internal/platform/(hrm|evidence)/port|internal/vault/identity/client' internal cmd tests -g '*.go'`; đối chiếu `hrm/module.yaml`, `assembly/hrm_docs.go` và consumer dùng port HRM/evidence. W11.24 thêm mục đích `hr_records`, thêm nhóm quyền tài liệu, không đổi chữ ký cổng đã có.
Downstream consumers: party (menu), evidence (lưu/mở tệp), vault/identity (mục đích mở khóa), assembly API, kn77; web-ops (menu, OpenAPI, quyền, kho tài liệu).
Regression acceptance command: `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/hrm/... ./internal/platform/evidence/... ./internal/platform/party/... ./internal/vault/identity/... ./cmd/identity-vault ./cmd/api-platform/assembly ./tests/kn77` (PostgreSQL tạm).
Propagation rule: thay hợp đồng/quyền hiện hữu hoặc đường mở tệp → chạy đủ các bên trên; thay chỉ mục đích mới → vẫn chạy toàn bộ evidence và vault để bảo vệ mục đích cũ.
Exemptions: cooperative, mrv và extension tiêu thụ cổng evidence/vault hiện hữu nhưng không dùng `hr_records`; chữ ký, resolver, mục đích và điều kiện đồng ý của chúng giữ nguyên. Bằng chứng hành vi cũ là suite evidence/vault, bao gồm `TestOpenChecksSubjectAndLogs_W65`, `TestOpenLegacyFileWithoutSubject_W65` và ca MRV trong `TestOpenHRRecordsWithoutConsent_W1124`. Contact đọc `StaffDirectory`, không dùng quyền tài liệu; suite HRM và assembly giữ đường này. Nếu những hợp đồng đó đổi thì bỏ các miễn kiểm này và mở rộng lượt chạy.
Sau phát hiện dùng sai mục đích, chạy lại evidence + assembly và thêm `./internal/platform/sync/...`; mục đích riêng chuyển sang registry có ràng buộc module/loại chủ thể. Tất cả bên tiêu thụ registry được lắp ráp lại.
Related test points: HRM-DOC-PURPOSE-001 (`TestOpenHRRecordsWithoutConsent_W1124`), HRM-DOC-CRYPTO-001 (`TestHRRecordsUnwrapHTTP_W1124`), FN-14 AC-01/02/03 (`TestHRMDocsHTTP_W1124`).

Kết quả tiếp nhận W11.24 ngày 15/10/2026:

- HRM domain/app và full HTTP handler (danh mục, hồ sơ, báo cáo Sở, kho tài liệu, benchmark 1.000 hồ sơ) PASS trên PostgreSQL thật. Sau bổ sung HRM-DOC-FILE-001, `go test -v -count=1 -p 1 -timeout 55m -run 'TestHRMDocsHTTP_W1124|TestOpen|TestAttach|TestStorage|TestAssembly|TestWire|TestBootstrap|TestKN|TestContact' ./internal/platform/hrm/handler ./internal/platform/evidence/... ./cmd/api-platform/assembly` hoàn tất PASS cho HRM Docs, các ca evidence được chọn và `TestContactOverHRM_W1123`. Log có dấu dọn cụm; kiểm chứng log trả mã 0, không FAIL/SKIP. Không dùng các lượt trước bản sửa để chứng minh ca mới.
- Evidence full, assembly full và sync full PASS, mã thoát 0; bảo vệ đường mở tệp hiện trường và tệp cũ. Party full + golden menu, vault/identity full và cmd/identity-vault full PASS; ca `TestHRRecordsUnwrapHTTP_W1124` PASS. Lỗi trùng helper `recFixture.raw` đã được bảo vệ bằng việc biên dịch/chạy chung hai suite Reports/Docs; đổi helper Docs thành `rawDoc`, không sửa kỳ vọng nghiệp vụ.
- Cổng migration `SchemaIsolation|RLSExplain|ModuleGroup|LoginRoles|NoCrossModule|LedgerAddColumn` — mã thoát 0; RLS EXPLAIN kiểm 469 bảng, 0 bỏ qua, 0 ngoại lệ. Migration mới 20261015083926 chỉ đổi tên, SQL giữ nguyên SHA-256.
- Web: 60 tệp, 519 ca được kiểm tra. Lượt đầu 511 PASS, 8 ca menu đọc fixture cũ trước khi Go sinh xong; sau sinh lại, 37 ca menu/problem PASS, mã thoát 0. Typecheck, gen:api và build:mock PASS, mã thoát 0. Không đổi timeout hoặc ngưỡng kiểm tra.
- Build, vet, modgen-check, migratecheck-base, archlint, contractcheck, docscheck, mfaflagcheck và golangci-lint — mã thoát 0 trên cây nguồn đã bổ sung interface; lint 0 issues.
- KN77: lượt trước bị gián đoạn khi chạm hạn mức, không dùng làm bằng chứng. Lượt chạy lại riêng `go test -v -count=1 -p 1 -timeout 55m ./tests/kn77` đã PASS trên PostgreSQL thật (182,464 giây), không SKIP, mã thoát 0 đã lưu. Toàn bộ hợp lệnh hồi quy W11.24 đã đạt.

HRM-DOC-FILE-001: resolver của module chủ hiện thực `port.FileAuthorizer` (tùy chọn cho chủ thể vùng chứa); evidence gọi trước khi cấp URL/nội dung. HRM kiểm đúng tài liệu hiện hành theo `file_ref`, đúng thư mục và quyền của tài liệu. Registry mục đích `UsePurpose` ràng buộc loại chủ thể, không đưa luật KN vào evidence. Các miễn kiểm ở trên vẫn giữ phạm vi hẹp, không áp dụng nếu thay cổng/mục đích cũ.
## party/cooperative — ORGUI (mốc dev 4a2154e3)

Dependency evidence: `rg -n '"/v1/orgs"' internal cmd tests -g '*test.go'`,
`rg -l 'internal/platform/party/port' internal cmd tests -g '*.go'`, module.yaml và assembly/modules.go.
Đổi observable HTTP POST tổ chức, thêm cổng CooperativeRegistrar, giữ các chữ ký đọc/cấp phạm vi/profile cũ.
Downstream consumers: cooperative/anchor + member/plot, assembly, tests/e2e, tests/acceptance,
cmd/seed-synthetic, tests/kn77, web-ops (hợp đồng, mock, OrgCreateDialog, NewAnchorForm).
Regression acceptance command: `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/cooperative/... ./internal/platform/party/... ./cmd/api-platform/assembly ./tests/kn77`.
Regression acceptance command (scenarios): `go test -v -count=1 -p 1 -timeout 55m ./tests/e2e ./tests/acceptance ./cmd/seed-synthetic`.
Propagation rule: đổi quyền/đường tạo, phục hồi hoặc cổng registrar → chạy đủ các bên trên + web typecheck/full Vitest/build và Playwright org-create.
Exemptions: các module chỉ đọc Orgs/ScopeResolver không đổi hợp đồng, quy tắc đọc/phạm vi và hồ sơ vẫn được bảo vệ bởi party + cooperative full.
Các fixture cooperative/member ghi Nút trực tiếp từ trước (không gọi HTTP), mkg/hub và api-internal dùng nguồn cooperative UUID tổng hợp;
scan không tìm thấy public POST org tạo HTX tại các gói đó. Build/vet vẫn kiểm hợp đồng mọi module.
Related test points: ORG-CREATE-001, ORG-NEO-001, ORG-NEO-002, ORG-NEO-003.
Phát hiện khi chạy toàn tests/e2e: bộ quét tên cột PII G1 cũ đánh nhầm phone/address công vụ ở
pf_contact.unit (ADR-0054), có sẵn trên dev 4a2154e3. Chỉ miễn đúng hai cột của bảng unit và partition
thực sự qua pg_partition_tree, giữ quét giá trị toàn bộ bảng. ORG-PII-GUARD-001 tạo cột full_name
và bảng unit_person_probe.phone trong giao dịch rollback để chứng minh ngoại lệ không che PII khác.
Không bỏ ca hoặc giảm ngưỡng.

Kết quả tiếp nhận ORGUI (15/10/2026):

- Full cooperative + party + assembly + tests/kn77: mã thoát 0, PostgreSQL thật, không SKIP.
  KN77 hoàn tất 311,354 giây. ORG-NEO-001, ORG-CREATE-001, import/round-trip PASS.
- Web full: 551 ca / 63 tệp, gen:api/typecheck/build:mock mã thoát 0. Playwright 5 ca org-create mã thoát 0,
  gồm chuyển trang và tạo HTX ở Neo. Sau sửa metadata địa bàn mock: typecheck + 28 ca NewAnchorForm/orgs-create/features,
  build:mock mã cuối và riêng Playwright Neo đều mã thoát 0; ca chọn tỉnh khác mẫu gốc so tên tỉnh và tâm xã.
- Lượt scenarios ban đầu: tests/acceptance PASS (680,273 giây), cmd/seed-synthetic PASS (181,707 giây).
  tests/e2e có G3/G4/G5/ORG-NEO-003/erasure/bilateral PASS nhưng G1 đỏ vì quét nhầm công vụ đã nêu, nên cả lệnh trả 1.
  Sau sửa, `TestG1DongXuanCanTho|TestContactPublicColumnsDoNotHidePII_ORGUI` mã thoát 0, đủ 14 bước và đối chứng schema PASS.
  Quy trình tích hợp đã chạy lại full tests/e2e trên mã cuối: mã thoát 0 (310,322 giây); G1, PII guard, G3/G4/G5, Neo, erasure và bilateral đều PASS, không SKIP.
- Cổng migration mã thoát 0, RLS kiểm 469 bảng / 0 bỏ qua / 0 ngoại lệ. Không migration mới.
- Build/vet/modgen-check/migratecheck-base/archlint/contractcheck/docscheck/mfaflagcheck lần cuối mã thoát 0 có marker từng bước;
  golangci-lint mã thoát 0, 0 issues. Hai tệp gofmt testdata archlint cố ý giữ nguyên.

Quy trình `rebase-merge.ps1 -Pkg orgui` hoàn tất mã thoát 0, gộp dev tại 184bc09f; cổng migration tích hợp chạy lại PASS (362,831 giây). Worktree và nhánh giữ lại; main không thay đổi.

## extension/accounts/notify — W11.46 (mốc dev 66bb6c94)

Dependency evidence: `rg -l 'internal/platform/(extension|accounts|notify)/port' internal cmd -g '*.go'` (189 tệp) và `rg -l 'internal/shared/idempotency' internal cmd -g '*.go'` (104 tệp, gồm fixture); đối chiếu module.yaml, assembly/modules.go và cmd/worker/notify.go.
Downstream consumers: party/menu, accounts/XaScopes và middleware, extension/lịch/cảnh báo, notify/inbox/push, assembly, worker, tests/kn77; API dùng chung chống trùng được bảo vệ bởi tests/e2e G1/G3/G4/G5 và các ca acceptance chống trùng. Web-ops (menu, API, mock, lịch), field-app (lịch, check-in, outbox).
Regression acceptance command: `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/extension/... ./internal/platform/accounts/... ./internal/platform/party/... ./internal/platform/notify/... ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77` trên cụm PostgreSQL tạm.
Regression acceptance command (hạ tầng dùng chung): `go test -v ./internal/shared/idempotency`; `go test -v -count=1 -p 1 -timeout 55m ./tests/e2e ./tests/acceptance ./migrations -run 'TestG1|TestG3|TestG4|TestG5|TestPrototype|TestSchemaIsolation|TestRLSExplain|TestModuleGroup|TestLoginRoles|TestNoCrossModule|TestLedgerAddColumn'`.
Propagation rule: đổi phạm vi/phiên bản/lịch, giải ngữ cảnh hoặc chống trùng → chạy đủ các bên trên. Các module chỉ gọi notify qua sự kiện cũ không đổi payload; suite notify full bảo vệ các kênh cũ. Giá/dịch vụ HTX không đổi nghiệp vụ, build/vet + e2e G1–G5 bảo vệ đường tích hợp; acceptance kiểm phát lại phản hồi, khác người/tenant/payload/route và lỗi. Không miễn các cổng migration hoặc web/mobile.
Related test points: CNTS-ROSTER-SCOPE-001/002, CNTS-DUTY-NOTIFY-001, IDEM-SCOPE-001/002/003. Bằng chứng lỗi: ca hai tỉnh ban đầu lộ danh mục/cán bộ; ca gửi lại trên tháng/phiên/phạm vi khác ban đầu đều trả201 và dữ liệu tỉnh cũ. Sau sửa phải trả422 `idempotency_key_reused`, retry hợp lệ giữ201.

Kết quả đang tiếp nhận (16/10/2026):

- Full cntscal + notify: mã thoát0 trên PostgreSQL thật, không SKIP; lịch trực/tiêm/check-in, hai tỉnh cùng tenant, Admin/legacy/hash và inbox/push đổi ca PASS. Lượt này trước bổ sung IDEM-SCOPE, lượt consumers kế tiếp kiểm lại toàn bộ mã cuối.
- Golden Go `TestWebMenuFixture`: mã thoát0, sinh trực tiếp từ Go, fixture không đổi so với sau rebase.
- Web gen:api/typecheck + schedules: mã thoát0; 9 ca gồm Admin chọn tỉnh/chế độ cũ chỉ xem. Full web/build đang chạy.
- Flutter analyze + full test: mã thoát0, 119 ca PASS. Hai ca SQLite FFI trước đó fail khi chạy full nhưng đạt khi chạy riêng; đã chờ điều kiện kết quả xuất hiện (giới hạn5giây), giữ nguyên mọi assertion và kích thước màn, không tăng timeout toàn bộ.
- `TestRequestHashAuthorization`: mã thoát0; tài nguyên/query/org/vai/MFA/phạm vi đổi tạo dấu khác; đảo thứ tự query/vai và refresh AuthTime vẫn giữ dấu. Cơ chế binding chung không import hoặc diễn giải KN; accounts gắn dấu server. Lưu ý nâng cấp v1→v2 tại [runbook](runbooks/idempotency-scope-v2.md).
- Cổng migration tĩnh phát hiện thiếu lock_timeout/NOT VALID trên CHECK mở rộng; đã sửa đúng quy trình, giữ CHECK tương đương và VALIDATE riêng; chưa coi lượt cổng cũ là đạt.
