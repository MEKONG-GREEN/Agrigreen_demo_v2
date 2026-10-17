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
Regression acceptance command: `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/forms/... ./internal/kn/extension/cntsalert/... ./internal/platform/reports/... ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77`.
Propagation rule: thay port, vòng đời bài nộp hoặc sự kiện → chạy các bên tiêu thụ trên;
chỉ sửa nội bộ và giữ nguyên hợp đồng → ghi lý do miễn cụ thể, không miễn mặc định.

## pf/extension

Downstream consumers: notify (sự kiện), reports (port nguồn), assembly API, worker,
web-ops (OpenAPI/mock/menu), tests/kn77. Party cung cấp menu bị sửa trong W11.43.
Regression acceptance command: `go test -v -count=1 -p 1 -timeout 55m ./internal/kn/extension/cntsalert/... ./internal/platform/notify/... ./internal/platform/party/... ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77`.
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

- Core: `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/forms/... ./internal/kn/extension/cntsalert/... ./internal/platform/notify/...` — mã thoát 0, PostgreSQL thật, không SKIP. Có `TestFormsSubmissionFeed_W1143`, `TestCNTSAlerts_W1143`, `TestCNTSNotifications_W1143` PASS.
- Sau sửa CNTS-SCOPE-001: chạy lại `./internal/kn/extension/cntsalert/...` với cùng tham số và cụm PostgreSQL tạm — mã thoát 0; `TestProvinceAlertScope_W1143` và HTTP `TestCNTSAlerts_W1143` PASS. Thay đổi chỉ thu hẹp điều kiện đọc cảnh báo theo tỉnh; không đổi port/sự kiện/payload.
- Downstream: `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/platform/party/... ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77` — mã thoát 0, PostgreSQL thật, không SKIP; bao gồm các kịch bản KN77, CNTS tổng hợp, phiếu KHTC và cập nhật nguồn KHTC. Toàn bộ hợp lệnh hồi quy đã đạt.
- `TestWebMenuFixture` (AG_UPDATE_WEB_FIXTURES=1) rồi `TestG5ThirdPartyToStatement` — mã thoát 0; golden menu không thay đổi byte.
- Cổng migration `SchemaIsolation|RLSExplain|ModuleGroup|LoginRoles|NoCrossModule|LedgerAddColumn` — mã thoát 0, PostgreSQL thật, không SKIP; RLS EXPLAIN kiểm 466 bảng, 0 bỏ qua.
- Web: gen:api, typecheck, 513 test / 58 tệp (`--maxWorkers=1 --pool=threads`), build:mock — mã thoát 0. Không đổi ngưỡng timeout.
- Build, vet, modgen-check, migratecheck-base, archlint, contractcheck, docscheck, mfaflagcheck, golangci-lint — mã thoát 0. Sau sửa phạm vi chạy lại các cổng tĩnh (ngoại trừ build toàn repo đã đạt trước đó; package sửa được biên dịch lại trong Go test).
- Giới hạn nghiệp vụ/diễn tập thực tế: [ma trận CNTS](uat/cnts-v3.7-acceptance.md). Không miễn bên tiêu thụ nào trong hợp lệnh đã chọn.

## pf/hrm — W11.24 (mốc dev fddf06a1)

Dependency evidence: `rg -l 'internal/platform/(hrm|evidence)/port|internal/vault/identity/client' internal cmd tests -g '*.go'`; đối chiếu `hrm/module.yaml`, `assembly/hrm_docs.go` và consumer dùng port HRM/evidence. W11.24 thêm mục đích `hr_records`, thêm nhóm quyền tài liệu, không đổi chữ ký cổng đã có.
Downstream consumers: party (menu), evidence (lưu/mở tệp), vault/identity (mục đích mở khóa), assembly API, kn77; web-ops (menu, OpenAPI, quyền, kho tài liệu).
Regression acceptance command: `go test -v -count=1 -p 1 -timeout 55m ./internal/kn/hrm/... ./internal/platform/evidence/... ./internal/platform/party/... ./internal/vault/identity/... ./cmd/identity-vault ./cmd/api-platform/assembly ./tests/kn77` (PostgreSQL tạm).
Propagation rule: thay hợp đồng/quyền hiện hữu hoặc đường mở tệp → chạy đủ các bên trên; thay chỉ mục đích mới → vẫn chạy toàn bộ evidence và vault để bảo vệ mục đích cũ.
Exemptions: cooperative, mrv và extension tiêu thụ cổng evidence/vault hiện hữu nhưng không dùng `hr_records`; chữ ký, resolver, mục đích và điều kiện đồng ý của chúng giữ nguyên. Bằng chứng hành vi cũ là suite evidence/vault, bao gồm `TestOpenChecksSubjectAndLogs_W65`, `TestOpenLegacyFileWithoutSubject_W65` và ca MRV trong `TestOpenHRRecordsWithoutConsent_W1124`. Contact đọc `StaffDirectory`, không dùng quyền tài liệu; suite HRM và assembly giữ đường này. Nếu những hợp đồng đó đổi thì bỏ các miễn kiểm này và mở rộng lượt chạy.
Sau phát hiện dùng sai mục đích, chạy lại evidence + assembly và thêm `./internal/platform/sync/...`; mục đích riêng chuyển sang registry có ràng buộc module/loại chủ thể. Tất cả bên tiêu thụ registry được lắp ráp lại.
Related test points: HRM-DOC-PURPOSE-001 (`TestOpenHRRecordsWithoutConsent_W1124`), HRM-DOC-CRYPTO-001 (`TestHRRecordsUnwrapHTTP_W1124`), FN-14 AC-01/02/03 (`TestHRMDocsHTTP_W1124`).

Kết quả tiếp nhận W11.24 ngày 15/10/2026:

- HRM domain/app và full HTTP handler (danh mục, hồ sơ, báo cáo Sở, kho tài liệu, benchmark 1.000 hồ sơ) PASS trên PostgreSQL thật. Sau bổ sung HRM-DOC-FILE-001, `go test -v -count=1 -p 1 -timeout 55m -run 'TestHRMDocsHTTP_W1124|TestOpen|TestAttach|TestStorage|TestAssembly|TestWire|TestBootstrap|TestKN|TestContact' ./internal/kn/hrm/handler ./internal/platform/evidence/... ./cmd/api-platform/assembly` hoàn tất PASS cho HRM Docs, các ca evidence được chọn và `TestContactOverHRM_W1123`. Log có dấu dọn cụm; kiểm chứng log trả mã 0, không FAIL/SKIP. Không dùng các lượt trước bản sửa để chứng minh ca mới.
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
Regression acceptance command: `go test -v -count=1 -p 1 -timeout 55m ./internal/kn/extension/... ./internal/platform/accounts/... ./internal/platform/party/... ./internal/platform/notify/... ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77` trên cụm PostgreSQL tạm.
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

Bổ sung sau các lượt tiếp nhận:

- `scenarios-gates`: mã thoát0; e2e G1/G3/G4/G5 PASS (248,290 giây), acceptance `TestPrototype*` PASS (597,227 giây), migration gates PASS (315,089 giây), RLS474bảng/0bỏqua/0ngoạilệ. Đây là lượt rộng bảo vệ chống trùng sau thay shared middleware.
- `final-security-migrations`: mã thoát0 trên SQL cuối đã dùng lock_timeout/NOT VALID/VALIDATE; CNTS-ROSTER-SCOPE-001/002/003, IDEM-SCOPE-001/002 và CNTS-DUTY-NOTIFY-001 PASS. Ca notify dùng cùng actor ở tenant khác để khẳng định RLS độc lập người nhận. Cổng migration cuối287,136giây,474bảng/0bỏqua.
- CNTS-CHECKIN-PRIVACY-001 (`TestWorkScheduleCheckin`): lượt tái hiện cache GPS trả1 vì vị trí có trong cache ngoài bảng hạn lưu. Đã lược Point khỏi bản lưu bằng SetStoredBody cho check-in và writeDetail (lưu/duyệt sau check-in); phản hồi đầu giữ vị trí đúng quyền, GET đọc vị trí còn hiệu lực. Lượt sau sửa cntscal full PASS: check-in/gửi lại/duyệt sau check-in, cache SQL không chứa location.lat, purge tại ngày hết hạn không xóa sớm và sau hạn xóa đúng2 điểm tenant hiện hành, tenant khác giữ1 điểm, metadata/hash sổ bất biến. Full extension/handler đang chạy tiếp trên mã sửa.
- CNTS-ROSTER-UI-001: lượt Playwright đầu bị cổng menu chặn Admin ở URL nghiệp vụ chung (2 ca lịch công tác cũ PASS). Đã thêm2mục Admin dưới phòng CNTS và2trang /kn/phong-ban/{dept}/lich-truc, lich-tiem; giữ AppShell/menuAllows nguyên vẹn. Thêm ánh xạ module cho lịch và /kn/cnts (danh-mục vẫn ưu tiên pf/masterdata). Golden sinh lại từ Go (18dòng thêm), party domain + các TestFN/TestModule/TestWebMenuFixture chọn lọc mã thoát0. Web đầy đủ/build/browser đang chạy lại.
- HRM-DOC-STATUS-001: thông báo lưu quyền đặt ở màn cha để tồn tại khi editor đổi phiên bản. Full web sau sửa560/65PASS; phụ thuộc cài cũ16.3.6 không khớp lock16.3.8 nên đã npm ci và chạy lại trên16.3.8: gen/typecheck/full560/65PASS, buildPASS. Không đổi package.json/lockfile hoặc nâng phiên bản ngoài lock. Bộ web sau thêm menu/trang mới đang kiểm lại.

Kết quả mã W11.46 cuối trước tích hợp:

- Hợp lệnh consumers hoàn tất mã thoát0: extension full, accounts full, party full, notify full, assembly full, worker và KN77 (188,006giây), PostgreSQL thật, không FAIL/SKIP. Lượt này khởi động trước chỉnh cache GPS/menu Admin; các lượt bổ sung dưới đây bảo vệ hai chỉnh cuối.
- `gps-cache-fixed`: mã thoát0; full cntscal và full extension/handler trên mã lược cache GPS. CNTS-CHECKIN-PRIVACY-001 cùng toàn bộ lịch/cảnh báo/phiếu/chu kỳ của handler PASS (102,463giây); cntscal HTTP89,324giây.
- `admin-menu`: mã thoát0; golden sinh Go + party domain và các ca FN/module/menu chọn lọc PASS, handler104,004giây.
- `web-admin-final`: gen:api/typecheck/full560ca65tệp/build:mock đạt trên Next16.3.8 đúng lockfile. Hợp lệnh trả1 chỉ do ca Playwright mới chọn nhãn rút gọn của phòng đóng thay vì nhãn đầy đủ phòng đã mở; hai ca lịch công tác cũ PASS. Ca trình duyệt cuối đã sửa bộ chọn đúng nhãn quan sát và lọc đúng thông báo đổi trạng thái (không chọn loading), không thay timeout/tiêu chí nghiệp vụ: CNTS-ROSTER-UI-001 PASS8,5giây, mã thoát0, đi qua bật phòng bằng UI, đăng nhập lại trong SPA lấy menu, phân ca/lưu, đổi tỉnh/xem lại, CSV và legacy chỉ đọc.
- Flutter cuối119ca + analyze mã thoát0 (không thay mobile sau lượt đó). Runbook ghi ngoại lệ phản hồi gửi lại chỉ metadata vị trí.
- `static-last` build0; vet1 vì Playwright dọn thư mục test-results đúng lúc Go đang duyệt đường dẫn (lỗi filesystem tạm), không dùng lượt này làm verdict xanh. Chạy lại vet và các cổng còn lại sau khi trình duyệt kết thúc; mã thoát cuối ghi bổ sung sau.

Cổng tĩnh sau khi trình duyệt kết thúc: vet, modgen-check, migratecheck-base, archlint, contractcheck, docscheck, mfaflagcheck và golangci-lint đều mã thoát0, lint0issues. Build cuối trước đó mã0. Đủ cổng trước tích hợp; helper sẽ chạy lại build/vet/migrationcheck/archlint và các ca lịch/menu cùng migration gates trên commit cuối.

Tích hợp W11.46: helper rebase-merge.ps1 hoàn tất mã thoát0, remote dev449faf44. Lượt tích hợp chạy lại full cntscal/handler + full party/handler PASS, cổng migration PASS162,007giây, không SKIP. Worktree/nhánh giữ nguyên; main không đổi.

## tools/knboundary — KNSPLIT-0 (mốc dev `988febd9`)

Dependency evidence: `rg -n 'knboundary' Makefile .github/workflows/ci.yml docs/dev` và đường gọi run/Scan/Compare/Growth trong tools/knboundary. Không đổi API/web/schema/runtime nghiệp vụ so với dev.
Downstream consumers: make gates/gates-base, CI pull request; kiểm kê namespace/route/schema và các gói KN kế tiếp. Kiểm thử dữ liệu mới tiêu thụ danh sách module/vai/schema hiện hành.
Regression acceptance command: `go test -v -count=1 ./tools/knboundary`; `go run ./tools/knboundary -base origin/dev`; `go test -v -count=1 -p 1 -timeout 55m ./migrations -run KNSPLIT0` trên PostgreSQL tạm. Cổng build/vet/modgen/migrate/archlint/contract/docs/mfaflag/lint vẫn bắt buộc.
Propagation rule: đổi quy tắc/alias hoặc baseline → chạy đầy đủ scanner fixture+repo+chống tăng/đổi alias/ref sai; đổi ranh giới DB → chạy các ca quyền thật và cổng migration. Không chạy lại web/mobile vì không có thay đổi source/hợp đồng ở hai ứng dụng trong gói này; W11.46 đã kiểm bản nền.
Related test points: KNSPLIT-GATE-001/002/003; các ca quyền dữ liệu trong migrations/kn_split_boundary_integration_test.go (tên và kết quả được ghi sau lượt DB).

Baseline ban đầu414 ghép nối sinh trên dev `988febd9` theo CODEX-G1, không phải tăng baseline sau áp cổng. Canonical aliases giữ ID khi chuyển namespace, đóng băng bằng -base; các ca thêm import mới/đổi alias/ref sai đều bắt lỗi. Tool suite mã thoát0. Bộ DB đang chạy:5 ca XFAIL được ghi là nợ bắt buộc 4b, không phải đã đạt ranh giới G1. D2-KN-01 đang chờ PO, không thay quyết định CEO.

Kết quả PostgreSQL KNSPLIT-0: mã thoát 0 (68,755 giây), không SKIP. `TestKNRolesNoHTXInternalAccess_KNSPLIT0` và `TestKNRolesCannotWriteHTXLedgers_KNSPLIT0` PASS. `TestKNPrincipalHTXDataAccess_KNSPLIT0/dich_tenant_KN_rieng` PASS. Nhánh tenant lưu ký tái hiện đúng 5 XFAIL: đọc bảng ngoài công bố/cấp phép, đọc ngoài phạm vi cấp phép, ghi sổ HTX, thu hồi và hết hạn không cắt quyền. Đây là bằng chứng nợ để gỡ ở KNSPLIT-4b; không được tính là 5 tiêu chí ranh giới đã đạt.

Cổng build/vet/modgen-check/migratecheck-base/archlint/contractcheck/docscheck/mfaflagcheck/golangci-lint: mã thoát 0, lint 0 issues. Tool suite cuối và knboundary -base origin/dev mã thoát 0, 414 ghép nối. Helper tích hợp được bổ sung chạy knboundary sau rebase và lấy tên nhánh thực để hỗ trợ prefix codex/; vẫn từ chối main và chỉ đẩy refs/heads/dev.

KNSPLIT-0 đã tích hợp bằng helper: mã thoát 0, dev f2a9afec; chạy lại 3 ca DB (giữ 5 XFAIL đúng lý do) và cổng migration 219,226 giây. knboundary sau rebase khớp 414; build/vet/modgen/migrate/archlint đều 0. Không có migration mới, không sửa main; giữ worktree.


## WH-A — Registry menu/mock/OpenAPI (mốc dev b5906660)

Dependency evidence: `rg -n 'DepartmentMenu|AdminDepartmentItems|MenuModule' internal/platform/party`; `rg -n 'mockFetch|mockAdminFetch' apps/web-ops/src`; `rg -n 'generated/application|generated/admin' apps/web-ops/src`; `rg -n 'check:api' .github/workflows/ci.yml`. Nguồn mới module/menus và contracts/openapi/modules được bộ ghép quét tự động; scanner knboundary vẫn đọc YAML đầu ra nguyên cấu trúc.
Downstream consumers: party domain/app/handler, KN77, menu mock/khung web, mọi client API đã sinh và toàn bộ màn mock. Không đổi mobile/API runtime/schema/migration; mobile đã xanh trên nền W11.46 và không cần chạy lại cho gói này.
Regression acceptance command: `node --test scripts/module-registries.test.mjs`; `npm run check:api`, `npm run typecheck`, `npm test -- --maxWorkers=1`, `npm run build:mock` tại apps/web-ops; Go/PG thật `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/party/... ./tests/kn77`, cổng migration và toàn bộ cổng tĩnh theo CODEX-G1. Đối chiếu SHA256 với bản nền cho golden,2OpenAPI,2generatedTS và kiểm git diff migrations rỗng.
Propagation rule: mọi thay đổi bộ ghép/menu/mock → chạy các consumers trên; chặn tích hợp nếu bất kỳ ca thật/byte đối chiếu/cổng ranh giới đỏ. Các ca menu khẳng định khóa và đường dẫn, không khóa tổng số mục khiến module thêm mới phải sửa test cũ.
Related test points: WH-A-MENU-001/002/003, WH-A-MOCK-001/002, WH-A-OAS-001; TestWebMenuFixture và kn-menu.contract.test.ts.

Lượt menu-db/session44329 mã thoát0: full party và KN77 trên PG riêng; KN77 181,172s, không FAIL/SKIP; golden byte giữ nguyên. Node5ca registry PASS sau sửa ca thay phần tửarray0 (trước đó trả lỗi vì coi0làfalse). GenAPI/typecheck đầu và full web560/65PASS trước chuyển bảng menu mock sang nguồn chung; chưa dùng làm kết luận cuối cho phần sửa đó. Registry mock giữ ưu tiên từng batch cũ và trạng thái module; kiểm file/exports, trùng ưu tiên, route có trang thật, duplicate/stale replacement đều bắt lỗi.

Cổng ranh giới lúc gom qua kn.ts bắt coupling mới; đã bỏ cách đó và giữ nguyên cấu hình/scanner. Quét lại0MỚI,24LỖI THỜI; sau xét duyệt lại có bằng chứng, prune chỉ bỏ24mục đã gỡ, baseline414→390 và -baseorigin/dev mã0. Không thêm/sửa alias, không nới quy tắc. Các kiểm tra cuối đang chạy; chưa tích hợp.

Lượt web-final trên menu mock cùng nguồn: gen/check/lint/typecheck đạt, full562ca/66tệpPASS117,40s; build đang chạy. Lượt static đầu các cổng trước lint đạt, lint bắt3helper cũ không còn dùng (adminDeptRoute/soon/needs); đã gỡ dead code, chạy lại toàn bộ static session44419 cùng log (có lịch sử lượt đỏ). FullPG cuối session74971 đã qua party, đang KN77 rồi migration. Chưa tích hợp.


Kết quả WH-A cuối trên commit e1ed8b3c:

- `web-final` mã0: gen:api/check:api, ESLint, TypeScript,562ca/66tệp PASS (117,40s), build:mock thành công trên Next16.3.8.
- `db-final` mã0, PostgreSQL riêng,0FAIL/SKIP: full party (handler160,555s), KN77(207,184s), migration gates(213,465s); RLS474bảng/0bỏqua/0ngoạilệ. Golden TestWebMenuFixture PASS.
- `static` lượt cuối mã0: build/vet/modgen/migratecheck-base/archlint/contract/docs/mfaflag/knboundary/tooltests và golangci-lint0issues. Lượt đầu đỏ3helper không dùng được ghi nguyên trong log; lượt sau đã xóa dead code và chạy lại đủ lệnh.
- Đối chiếu cả5SHA256 với b5906660: golden menu,2OpenAPI,2generatedTS đều giữ nguyên. Migration tree không đổi; gofmt và diff-check đạt. Thêm `# @ag-end` ở2fragment để giữ blank YAML gốc và tránh blank-EOF ở nguồn mới; parser bỏ marker, bộ ghép và Node5ca được kiểm lại sau thay đổi này. Không đổi đầu ra hay hành vi.
- Chuẩn bị tích hợp bằng helper vào dev; ca KN77 đã chạy đầy đủ trên mã cuối, helper chạy lại party và các cổng migration trên cây sau rebase. Chưa có thay đổi Go nghiệp vụ sau các kết quả này.

Tích hợp WH-A: helper38351 mã0, dev a83b0b90; full party chạy lại (handler110,157s) và migration gates342,476s PASS. Các cổng sau rebase đều0, baseline390. Giữ nguyên worktree/nhánh và main27afd593.


## W11.66 — báo cáo KHTC và nguồn/phiếu/kỳ (đang kiểm chứng, nền 75f83976)

Dependency evidence: `rg -n 'KHTCPlanVersions|KHTCPlans|GuardCancel|khtcReportSvc|khtcSourceSvc' internal cmd tests`; `rg -n 'createReportApi|khtcReportInputs|khtcReportSource|khtcReportRoutes' apps/web-ops/src`; registry WH-A sinh menu/mock/OpenAPI.
Downstream consumers: extension KHTC plan/form/period/source/report, closedfig, cap/forms, platform assembly, party menu/domain/handler, tests/kn77, web API/mock/editor/shell/menu. Không đổi Flutter hoặc API MRV, miễn chạy lại mobile cho W11.66.
Regression acceptance command: PG thật `go test -v -count=1 -p 1 -timeout 55m ./internal/kn/extension/... ./internal/platform/forms/... ./cmd/api-platform/assembly ./internal/platform/party/... ./tests/kn77`; migration gates; các cổng tĩnh CODEX-G1, boundary-base. Web `npm run check:api`, typecheck/lint, full Vitest, build:mock và browser editor. Golden menu sinh bằng TestWebMenuFixture trên PG thật.
Propagation rule: thay đổi plan port, source Sum/YTD/ReportKeys, final/cancel guards phải kiểm toàn bộ consumers trên. Biên dịch không chạy test và DB SKIP không tính là bằng chứng nghiệp vụ.
Related test points: KH-FN-TI-02.05…02.09, TestKH_ReportSumYearToDateAndFrozenWeeks, TestKH_ReportKeysDoNotRequireProgramPlan, TestKH_ReportFinalAndCancelConcurrent, TestKH_ReportCancelBeforeFinalDoesNotWaitOnItself, TestKH_ReportClosedSnapshotKeepsPlanVersion.

- HTTP report-sixth exit0: ba ca report thật, Excel mở lại, phiếu đã gửi/resend, provisional/access, final/cancel và giữ byte tệp cũ; 128,276s, không SKIP.
- Pure-third exit0: mở lại Mẫu1–5, số trong kỳ20/điều chỉnh−5/lũy kế85/85%, hai ngày giá8000–13500 và8500–14000, phụ lục và không công thức; domain6ca và source goldens + discovery/sum-YTD đạt.
- Boundary-first0:390 couplings đúng baseline, không nới scanner/config.
- HTTP seventh: hủy thường, final/cancel đồng thời, closed-plan PASS; một assertion provenance dùng substring999 đã trúng UUID ngẫu nhiên, sửa kiểm khóa Sheet/cells không được serialize (số12 và phiên bản cũ đã được kiểm độc lập). Lượt kiểm lại/full consumers còn chờ. Không tích hợp khi chưa đủ xanh.
- Web report contract hai ca PASS sau sửa fixture thêm cong_tac_khac bắt buộc. TSC-third0, ESLint phạm vi mới0. Chưa kết luận full web/browser/build hoặc full PG/gates.

W11.66 kiểm chứng bổ sung 16/10:

- `report-ninth` exit0:8ca HTTP PG riêng,360,519s, không SKIP; gồm chép duy nhất Mẫu5II và giữ blank đã lưu. Lượt eighth dùng sai cú pháp timeout nên không chạy đúng package, exit1, không tính đạt.
- `pure-sixth` exit0: Excel SUM(F:H)=100 trên các ô numeric, phần trăm numeric85.0%, phụ lục đúng5cột +sheet chi tiết; truy nguyên kỳ nguồn/gốc đóng. `pure-seventh` exit0: thêm năng suất7.4=tổng740/tổng100ha, diện tích0→không tạo số, nguồn chưa chốt giữ Tạm tính.
- Static second-final và third-final exit0 trước trace/yield: build/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary390/lint0issues. Đã sửa2lint trước đó; cần kiểm lại sau trace/yield.
- Full-second đã dừng có chủ đích để sửa thiếu sót Excel, giữ log không tính đạt. `db-full-third` đang chạy trên Go đóng băng: toàn party/extension/forms/cooperative-anchor/cooperative/assembly/KN77; bao gồm lịch sử xác nhận HTX M6 ở consumeranchor. `web-final-third` đang chạy trên schema/UI mới nhất. Lượt web-second chặn preflight do schema vừa đổi, không tính đạt.

Bằng chứng cuối giao diện/cổng tĩnh W11.66:

- `static-fourth-final` exit0 trên mã có trace/yield: build/vet/modgen-check/migrate-base/archlint/contract/docs/MFA/boundary390, golangci-lint0issues, diff-check0.
- `web-final-third` đã checkAPI/lint/typecheck0; full test đỏ do worker lịch công tác không khởi động,552ca/67tệp đạt, không dùng làm full verdict.
- `web-final-fourth` exit0: nhóm lịch riêng14ca đạt; chạy lại đầy đủ **566ca/68tệp PASS137,54s**, registry5PASS, build:mock thành công; browser KH-REPORT-UI-0011PASS14,7s trên bản dựng mới, CI không dùng server cũ, không tăng timeout/không retry. Không sửa mã web sau lượt này.
- Full Go/PG consumers `db-full-third` còn chạy; chưa tích hợp. Migration riêng trước đó477bảng RLS,0SKIP, không thay SQL migration sau lượt đó; helper vẫn phải kiểm migration trên commit sau rebase.

Khôi phục 16/10 lúc13:54: full-third đã kết thúc tất cả package trước KN77 với PASS (party/extension/forms/cooperative-anchor/assembly), nhưng tiến trình bị gián đoạn trong KN77, không có mã thoát cuối. Không coi toàn lệnh đó là xanh. Cây mã không đổi (0a1093de); chỉ chạy lại `go test -v -count=1 -p 1 -timeout 55m ./tests/kn77` trong `kn77-final` với PostgreSQL riêng. Giữ bằng chứng package đã hoàn tất, không chạy lặp toàn suite không cần thiết.

Kết quả cuối backend W11.66: 31 package có kiểm thử bắt buộc đều có dòng hoàn tất PASS, không FAIL/SKIP, trên mã0a1093de. Package trước KN77 từ full-third; **kn77-final exit0,268,904s**, đủ tất cả ca báo cáo mới trên trace/yield cuối. Đối chiếu danh sách bằng `go list -f` với TestGoFiles/XTestGoFiles; proofJSON ở hostTEMP agrigreen-codex-w1166-consumer-proof.json. Whole full-third bị gián đoạn không được đổi thành exit0. Chuẩn bị helper trên cùng nền dev75f83976; helper chạy report component và migration gates thật, không chạy lại toàn bộ consumers không đổi.


W11.66 đã tích hợp dev b13ae196: helper exit0, migration gates thật PASS263,804s, build/vet/modgen/migrate-base/archlint/boundary390 đạt; giữ toàn bộ worktree và main.

## KNSPLIT-1 — Namespace KN (đang kiểm chứng, nền b13ae196)

Dependency evidence: tìm các import internal/kn/{extension,price,hrm,contact} trong internal/cmd/tests; consumers trực tiếp gồm accounts, notify, reports, assembly và worker. Party/forms tiêu thụ registry và hợp đồng đã chuyển; KN77/acceptance/e2e/golden dùng đường dẫn mới. modreg/modgen/archlint quản lý mọi module.

Acceptance: PostgreSQL riêng, go test -v -count=1 -p 1 -timeout 55m ./internal/kn/... ./internal/platform/accounts/... ./internal/platform/notify/... ./internal/platform/reports/... ./internal/platform/party/... ./internal/platform/forms/... ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77 ./tests/acceptance ./tests/e2e ./tests/golden/khtc; migration gates qua helper và các cổng CODEX-G1. Không sửa Go khi chạy hồi quy.

Tool-first exit1: kiểm thử mới phát hiện /extension/khtc không bao phủ các package cạnh nó. Đã bổ sung phạm vi cả namespace cũ/mới, không đổi kiểm thử để bỏ lỗi. Tool-second exit0: modreg, modgen, archlint, knboundary; gồm từ chối hai chủ schema cùng định danh, giữ wiring, R02/R03/shared/vault/port và chạy analyzer trên float/DELETE thật trong namespace mới.

Registry menu/mock/OpenAPI sinh lại thành công; năm SHA256 golden/menu/spec/TS giống hệt b13ae196, migrations không đổi. Không thay nguồn web/mobile nên miễn chạy lại suite giao diện trên bytes không đổi. Chưa có kết luận hồi quy dữ liệu/tĩnh hoặc tích hợp; cập nhật khi có mã thoát thật.


KNSPLIT-1 bằng chứng cuối 16/10:

- `db-full-second` exit1 chỉ vì TestNamedModulesMatchCatalog giả định mọi module ở platform. Tất cả 55 package còn lại PASS, không SKIP: gồm bốn chủ KN, accounts/notify/reports/party/forms, worker, KN77, acceptance, e2e, golden. Giữ log đỏ, không đổi thành exit0.
- Sau khi lệnh kết thúc, sửa assertion đối chiếu chính xác PkgPath với manifest.Dir, đồng thời bắt buộc bốn module nằm internal/kn. `assembly-final` exit0: assembly118,698s PostgreSQL riêng; modreg/modgen/archlint/knboundary đạt. Tổng 56/56 package bắt buộc có PASS; không chạy lặp 55 package không đổi.
- Đối chiếu2102 SHA Go xác nhận đóng băng mã trong suốt full PG. Sau đó gofmt383 tệp đã thay; so gofmt(index) với working tree chứng minh toàn bộ Go nghiệp vụ chỉ đổi định dạng, duy nhất assertion assembly đổi ngữ nghĩa. Hai fixture archlint chưa định dạng từ nền được giữ theo w-common. Proof consumers/format JSON lưu hostTEMP.
- `static-final` exit0 sau format: build/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary390/lint0issues. `.golangci.yml` giữ lớp dự phòng cấm KN↔MKG; không thêm ngoại lệ.
- Registry5ca và3gen-check PASS. Sau full PG,5SHA256 golden/menu/spec/generatedTS giữ nguyên;3fixtureJSON chỉ đổi ghi chú đường dẫn, payload đối chiếu bằng JSON hoàn toàn bằng nhau; migrations không đổi. Không có thay đổi mã TS/Dart/API hành vi cần chạy lại web/mobile.
- Chuẩn bị helper tích hợp dev trên mã này; phải kiểm các cổng migration PostgreSQL thật trước FF, giữ worktree và main.


KNSPLIT-1 đã tích hợp: helper7499 exit0; dev ddd2671f; migration8ca PASS304,766s/không SKIP, cổng/tooltests0 và baseline390. Main27afd593 sạch, worktree giữ nguyên.

## KNSPLIT-2-FORMS-A — Đăng ký phân giải ngữ cảnh (nền ddd2671f)

Dependency evidence: tìm formsport.Profile/forms.Deps/assembly.KNProfiles trong internal/cmd/tests. Consumer cấu hình thực tế: extension.FormProfiles, assembly, KN77 closedfig/CNTS/KHTC phiếu/kỳ/nguồn. Forms.resolve dùng chung mọi thao tác capability; KN77/harness E2E kiểm wiring thật. Không đổi số liệu/biểu mẫu API/mobile/web hoặc SQL.

Acceptance: PostgreSQL riêng go test -v -count=1 -p 1 -timeout 55m ./internal/platform/forms/... ./internal/kn/extension ./cmd/api-platform/assembly ./tests/kn77 ./tests/acceptance ./tests/e2e; các cổng CODEX-G1. Thay đổi resolver phải kiểm scope/target/tenant, lỗi không fallback về realm và JSON không mang callback. Cổng knboundary giữ390 trong bước A; không nới cấu hình. Bước B mới gỡ fallback.

Pure-first PASS: application FormProfiles policy (phòng ban/admin/KHTC cấp/giới hạn cơ sở+hộ/cấu hình mới từng lần), generic registered actor identity/scope/target, lỗi cấm fallback và tương thích realm. TestRegisteredActorDatabaseTenantIsolation mới chờ PG thật, không tính pure là DB proof.


Bổ sung bước A: static-first bắt báo nhầm R02 ở _testmain.go do Go tự sinh khi module root có test mới. Đã thêm nhận diện đúng driver (main + hậu tố .test + duy nhất AST mang dấu generated), chỉ bỏ kiểm self-import bắt buộc vào chính package đang thử. Các import khác vẫn kiểm đủ; ca âm driver sinh import module khác và driver viết tay import root đều bị R02. Tooltest14,054s PASS. Trước khi sửa công cụ riêng, go list -deps -test xác nhận tools/archlint không thuộc graph PG đang chạy; không đổi bất kỳ Go production/consumer nào sau khi bắt đầu PG.

TestRegisteredActorDatabaseTenantIsolation PASS0,16s trên PostgreSQL thật; toàn forms/app PASS200,033s và forms/handler PASS171,678s, không SKIP. Chưa kết luận whole consumers; đang chạy assembly/KN77/acceptance/e2e.


Siết nhận diện driver thêm dấu chính xác `Code generated by 'go test'. DO NOT EDIT.`; không áp dụng cho mã sinh bởi công cụ khác. Thêm ca âm othergen.test vẫn bị R02. Tools-final PASS14,256s và archlint-final exit0. Go production/consumer đối chiếu SHA không đổi trong khi PG chạy; các sửa công cụ nằm ngoài graph phụ thuộc PG đã xác minh. KN77 đã PASS484,170s; chờ acceptance/e2e.


Bằng chứng cuối KNSPLIT-2-FORMS-A:

- `db-full-first` exit0: 8/8 package bắt buộc PASS, không FAIL/SKIP. Forms/app200,033s (gồm ca registry DB cách ly tenant), forms/domain0,728s, forms/handler171,678s, extension root3,380s, assembly175,914s, KN77484,170s, acceptance671,369s, e2e242,864s. Các hash Go production/consumer khớp snapshot đến khi toàn bộ lệnh kết thúc.
- `static-final` exit0: build/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary390/lint0issues trên công cụ nhận diện driver cuối. Tool-final14,256s PASS, ca âm cho driver viết tay và công cụ sinh khác vẫn R02.
- Sau full PG, chỉ tăng độ mạnh TestRegisteredActorScopeAndIdentity: callback cho phép mục tiêu ngoài địa bàn, capability vẫn bắt buộc từ chối bằng Scope. `pure-final` exit0:3ca đăng ký/scope/error/realm PASS1,583s; không sửa production, không cần chạy lặp hồi quy dữ liệu.
- Không đổi OpenAPI/mã web/mobile/migrations. Profile.ResolveActor json:- đã được kiểm serialize; không lộ callback. Chuẩn bị helper chỉ FFdev sau migration gates thật. Bước B vẫn phải gỡ legacy, không tuyên bố tách KN hoàn tất ở bước A.


KNSPLIT-2-FORMS-A đã tích hợp dev75745d27: helper57920 exit0, migration8ca PASS251,305s/không SKIP; root profile và công cụ kiến trúc đạt, giữ main/worktree.

## KNSPLIT-2-FORMS-B — Gỡ nhánh KN trong cap/forms (nền75745d27)

Dependency evidence: tìm authz.KN/RequireKN/.KN trong forms trước/sau; các constructor cấu hình ở assembly và KN77 đều nhận extension.FormProfiles từ A. Adapter và assembly/forms.go không đổi trong B, nên đường production KN đã được A kiểm8/8 giữ nguyên. B đổi nhánh profile không đăng ký và các fixture của capability. Kiểm trực tiếp forms/... +extension root +assembly +KN77 trên PostgreSQL riêng, cổng CODEX-G1. Không chạy lặp acceptance/e2e trên cùng wiring/callback đã đạt ở A khi không có thay đổi ảnh hưởng; mở rộng nếu xuất hiện lỗi mới.

Ca âm trước sửa: negative-before lần đầu không hợp lệ vì fixture cấp viết thường, không tính là bằng chứng lỗi quyền. Đã sửa cấp UPPER/LEAF; negative-before-second exit1 đúng vì KNContext được suy và đi đến errNoPool. Sau gỡ legacy, pure-first exit0: TestFormsRequiresRegisteredSessionProjection nhận Forbidden trước DB; scope/error/realm và KN profile chính sách vẫn PASS. Không đổi test để bỏ điều kiện.

Không còn authz.KN*/RequireKN hoặc đọc .KN trong toàn bộ forms (cả fixture Go); không import KN vào forms. Bộ lọc Scope trước target và tenant/actor của Principal giữ nguyên. Baseline390 không đổi vì scanner không có entry Go forms trước B; không thay rules/aliases/baseline để tạo giảm giả. Cần xét ngữ nghĩa và ca âm runtime bên cạnh số lượng. Đang kiểm PostgreSQL thật, chưa tích hợp B.


Bằng chứng cuối FORMS-B: db-full-first40826 exit0,6/6 package PASS, không FAIL/SKIP. Forms/app117,186s (gồm vai rỗng/lạ không kế thừa realm), domain1,101s, handler107,744s, extension root2,388s, assembly107,875s (ca không suy quyền ngoài registry), KN77272,255s. Go production/consumer đối chiếu SHA không đổi. Static-first43350 exit0: build/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary390/lint0issues. Ba cấu hình và callback KN từ A giữ nguyên; không đổi API/SQL/Frontend nên giữ proof acceptance/e2e A671,369s/242,864s. Chuẩn bị helper trên commit cuối, chỉ FFdev sau migration gates thật.


## W11.66 — M4 bổ sung, nền dev8af9ad17 (16/10/2026)

Downstream consumers: closedfig → khtcsource domain/app/store/handler → khtcform/app và khtcreport domain/app; extension assembly và KN77 HTTP. Truy vấn KHTC thêm PriceAt/PriceAsOf chỉ nội bộ; không thay hợp đồng API/SQL/UI. Miễn chạy lại web/mobile vì không đổi schema công khai, handler DTO, fixture, TypeScript hoặc Dart.

Dependency evidence: `rg -l 'internal/kn/extension/(closedfig|khtcsource)' internal cmd tests -g '*.go'`; `go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}'` trên closedfig, khtcsource/..., khtcform/..., khtcperiod/..., khtcreport/..., extension root, assembly, KN77, golden/khtc cho11 package có test. Các port consumer khác chỉ nhận field cộng thêm, được build/vet toàn repo; không đổi constructor/registry/HTX/forms, acceptance/E2E ngoài KHTC không bị đổi hành vi.

Regression acceptance command: PostgreSQL riêng, `go test -v -count=1 -p 1 -timeout 55m ./internal/kn/extension/closedfig ./internal/kn/extension/khtcsource/... ./internal/kn/extension/khtcform/... ./internal/kn/extension/khtcperiod/... ./internal/kn/extension/khtcreport/... ./internal/kn/extension ./cmd/api-platform/assembly ./tests/kn77 ./tests/golden/khtc`; cổng CODEX-G1 và migration helper trước tích hợp. Scope được user cho phép trong CODEX-G1; không mạng trừ fetch/push dev qua helper, không dữ liệu thật, không sửa main/xóa worktree.

Related test points: M4, KH-FN-TI-02.05, KH-UR-TI-12/13. Ca mới `TestKH_M4_PriceCutoffPublishedReader`, `TestKH_M4_ExportKeepsPriceSnapshot`, `TestKH_M4_SparsePriceItems`, `TestKH_M4_PriceDateDoesNotExtendQuantityPeriod`, `TestKH_M4_LegacyPriceProjectionDoesNotInventSourceChange`.

Red-before: `red.exit1` đúng lỗi nghiệp vụ trên production trước sửa: muốn13/14-10 nhưng lấy12/13-10; chỉ có giá năm2025 thì không phát hiện khóa giá. Bốn package miền thuần/closedfig/report domain/app sau sửa PASS, `pure-first.exit0`; golden hiện có giữ nguyên. PG first/second sau refactor fixture exit1 do kiểu tham số fixture SQL (không tính đạt). PG third đang chạy sau ép kiểu SQL rõ ràng. Static-first build/vet/modgen/migrate-base/archlint/contract đạt rồi docscheck báo tên tệp sai; đã nhập ghi chú vào đúng W11.66.md, static-second docs/MFA/boundary390/lint0issues exit0. Còn chờ PG/gates cuối, chưa tích hợp.

PG-third75575.exit0:130.448s, TestKH_M4_PriceCutoffPublishedReader (cả2case), TestKH_M4_ExportKeepsPriceSnapshot, TestKH_M4_SparsePriceItems PASS, không SKIP. Đã kiểm2cột/ngày/min trong Excel thật, byte cũ bất biến sau duyệt giá mới và refresh; snapshotrow giữactualD8, nguồnđổi ngoài kỳđược phát hiện; tenantkhác khônglọtgiá. Bắt đầu11package hồi quy đầy đủ trên mã cố định.


Kết quả cuối W11.66 M4 trên commit6552808b: `db-full59187.exit0`,11/11 package có kiểm thử PASS,0FAIL/SKIP; assembly118.555s, KN77294.702s, closedfig0.642s, source domain0.899s, form domain1.125s, period app0.495s/domain0.591s, report app1.943s/domain1.115s, extension2.465s, golden0.616s. Go không đổi suốt lượt chạy; proofJSON hostTEMP agrigreen-codex-w1166cutoff-proof.json. `static-final27382.exit0`: build/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary390/lint0issues, diff-check0. gofmt chỉ2fixture cũ được w-common cho phép. Không thay API/SQL/web/mobile nên miễn các bộ không bị tác động như đã nêu. Helper còn phải chạy cổng migration và kiểm tra nền dev trước FF; không tuyên bố helper đạt trước kết quả thực.


## W11.66 — Mẫu5 ngày chốt trước cuối kỳ (nền59acadd7)

Downstream consumers: period/domain.QuantityEnd → period/app (danh sách nguồn), source/app (query/evaluate/capture/status), report/domain.Compose và report/app.submitted; closedfig.KHTCCrosswalk.Sources cho custom FormsKind=adhoc. Gốc extension/assembly và KN77 kiểm wiring thật.

Dependency evidence: `rg -l 'khtcperiod/domain|khtcsource|closedfig' internal/kn/extension cmd tests -g '*.go'`; cùng11package của gói M4. Hợp đồng API/SQL/frontend không đổi; miễn web/mobile và các consumer ngoài KHTC vì chỉ số Mẫu5/crosswalk custom thay hành vi. Build/vet toàn repo vẫn bắt buộc.

Regression acceptance command: PG riêng với `go test -v -count=1 -p 1 -timeout 55m` cho closedfig, khtcsource/..., khtcform/..., khtcperiod/..., khtcreport/..., extension root, assembly, tests/kn77, tests/golden/khtc. Cổng CODEX-G1 và migration helper trước FFdev. Phạm vi này nằm trong yêu cầu G1 đã được user cho phép, không dữ liệu thật, không sửa main/xóa worktree.

Related test points: KH-FN-TI-02.09, KH-UR-TI-12/13; `TestKH_M5_ProgramUsesCutoffForWeeklyFigures`, `TestKH_M5_ProgramCutoffHTTP`, `TestKH_M5_LiveProgramSourceCrosswalk`.

- Red-pure.exit1: tuần sau cutoff vẫn vào phụ lục/captured submissions; red-pg.exit1/127.329s: phiếu100 thay vì10, module120 thay vì100.
- Sửa QuantityEnd trên5cbfd91a; pure-first.exit0. PG-first.exit1/145.056s: HTTPcutoff PASS11.14s, live crosswalk đỏ đúng lỗi PeriodOf(adhoc,nil). Không coi whole command đạt.
- 79d9acc3: custom adhoc forms lấy nguồn theo tuần D8 thật trong khoảng From..To, không so custom FormsKey với native week. Full PG65906 bị gián đoạn khi hết hạn mức, không có mã thoát cuối;8package đã hoàn tất PASS,0FAIL/SKIP: closedfig0.831s, source domain0.712s, form domain1.168s, period app0.721s/domain0.778s, report app1.732s/domain1.249s, extension1.790s. Giữ phần bằng chứng này trên production Go không đổi.
- Sau reset chỉ bổ sung test DTO/Excel/hash. Resume PG15895 chạy3nhóm còn thiếu assembly, KN77, golden; đang chờ kết quả thực. Ca HTTP kiểm phụ lục Excel10/100; đổi phiếu/nguồn của tuần sau cutoff không đổi input hash/cảnh báo và không sửa byte Excel cũ, kể cả refresh. Live crosswalk kiểm cutoff giữa tuần8/10 chỉ nhận board6/10, không lấy7/10.
- Static-after-crosswalk52241 đang chạy trên production79d9acc3 và test bổ sung. Chưa tích hợp khi chưa đủ kết quả.


Kết quả cuối Mẫu5: production79d9acc3 giữ nguyên. `db-resume15895.exit0`: assembly138.677s, KN77353.770s, golden1.204s; đủ11/11package khi ghép8package đã hoàn tất trong full trước gián đoạn, không đổi whole full thành exit0. Không FAIL/SKIP. TestKH_M5_ProgramCutoffHTTP PASS19.31s gồm Excel phụ lục10/100, giữ To/cutoff riêng, hash/cảnh báo và byte cũ bất biến sau sửa phiếu/nguồn tuần sau cutoff rồi refresh; TestKH_M5_LiveProgramSourceCrosswalk PASS0.68s, cutoff giữa tuần chỉ nhận nguồn của tuần đã hết. `static-after-crosswalk52241.exit0`: build/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary390/lint0issues. ProofJSON hostTEMP agrigreen-codex-w1166programcutoff-proof.json. Chỉ thay test DTO sau production79d9acc3; chưa có API/SQL/frontend đổi. Helper còn kiểm migration trước FFdev.


## KNSPLIT-2-REPORTS-A — Session registry (nền0237b812)

Downstream consumers: reports/app formReader/dispatch actor; reports module Deps; extension policy; assembly và worker factory; KN77 đăng ký policy thật. Legacy realm role groups/schedulers/HTX export không đổi.
Dependency evidence: `rg -n 'reports.New|reports.Deps|formReader|knDeptAllows' internal cmd tests -g '*.go'`; scope callback tác động3đường đọc form và8use case dispatch. Dependency mới chỉ port/context callback, không actor/tenant projection. Existing native realm tests của handler bảo vệ compatibility; KN77/assembly kiểm đường session thật.
Regression acceptance command: PG riêng, `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77`; đủ static/helper migration trước FFdev. Miễn frontend/mobile và acceptance/e2e ngoài phạm vi vì không đổi contract/DTO hoặc core consumer các module khác. Phạm vi user đã cho phép qua CODEX-G1; không dùng dữ liệu hay SMTP thật, không sửa main/xóa worktree.
Related test points: TestRegisteredReportSessionIdentityAndFilters, TestRegisteredReportSessionDenialCannotFallBack, TestRegisteredReportSessionLegacyAndRealmCompatibility, TestReportSessionPolicyRolesAndDepartments; các test KN77 form/dispatch/PII/tenant hiện hành.
Pure-first chỉ lỗi compile thiếu field/kiểu fixture, không tính proof. Pure-second58639.exit0: reports/app0.675s, extension1.867s,4test mới PASS không SKIP. Chưa tích hợp, chờ PG/static thực.


KNSPLIT-2-REPORTS-A cuối: production540e7249 cố định; `db-full93018.exit0`,13/13package có test PASS,0FAIL/SKIP. Reports handler147.814s, assembly109.611s, worker2.306s, KN77285.926s; fixture KN77 đăng ký policy thật nên quyền/phòng/tỉnh/mask/on-behalf và RLS chạy qua callback mới. `static-first61666.exit0`: đủ build/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary390/lint0issues. ProofJSON hostTEMP agrigreen-codex-knsplit2reportsa-proof.json. Không đổi API/SQL/frontend; helper còn chạy migration thật trước FFdev. Bước A không tuyên bố gỡ xong legacy session/realm roles.


## KNSPLIT-2-REPORTS-B — Bỏ adapter phiên (nền18dd40f9)

Downstream consumers: reports/app formReader/dispatch, reports handler, assembly, worker, KN77 với policy thật của extension. Dependency evidence: `rg -n 'reports.New|reports.Deps|ReportSessionResolver|formReader|resolveSessionActor' internal cmd tests -g '*.go'`; các factory user-session đã đăng ký bước A. Worker/main chỉ xử lý background, không gọi use case phiên. Handler fixture kiểm native realm. Role groups và source ports không đổi.

Regression acceptance command: PostgreSQL riêng, `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77`; các cổng CODEX-G1 và migration helper. Miễn web/mobile và acceptance ngoài scope vì không đổi contract/UI/SQL; build/vet toàn repo bắt buộc. Đã được user cho phép trong G1, không dữ liệu/SMTP thật, giữ main và worktree.

Related test points: TestReportSessionRequiresRegistration, TestReportSessionAnonymousDenied, TestRegisteredReportSessionIdentityAndFilters, TestRegisteredReportSessionDenialCannotFallBack, TestRegisteredReportSessionNativeRealmCompatibility; KN77 form/dispatch/PII/tenant.

Red.exit1: không resolver và callback unhandled đều cấp quyền từ KNContext ở cả đọc/ghi dispatch và đọc form. Sau gỡ adapter, pure.exit0/reportsapp1.924s,0SKIP. Chờ PG/static/helper, chưa tích hợp.

Kết quả thực: PGfull53656.exit0 đủ13/13package,0FAIL/SKIP: handler90.414s, assembly66.971s, worker2.088s, KN77196.436s. Production65054726 không đổi. Staticfirst87115.exit1 chỉ docscheck sai định dạng hai tài liệu; build/vet/modgen-check/migrate-base/archlint/contractcheck đã đạt. Sau sửa docs, staticresume44281.exit0: docscheck/MFA/boundary390/lint0issues/diffcheck. Gofmt toàn repo chỉ2fixture cũ được cho phép. ProofJSON hostTEMP agrigreen-codex-knsplit2reportsb-proof.json. Helper còn kiểm8migrationgate trước tích hợp.


## KNSPLIT-2-REPORTS-C — Registry role/source/config (nềna85cfabb)

Downstream consumers: reports/app templates/exports/schedules/coop-schedules/forms/dispatch, constructor reports; KN policy owner; API assembly, hai worker factory, KN77, handler fixture. Dependency evidence: `rg -n 'reports.New|reports.Deps|rolesFor|coopOnly|jobScope|jobVisible|scheduleVisible|ReportPolicy' internal cmd tests -g '*.go'`. Registry logic trong app, port chỉ Policy DTO; không thêm import KN vào shared code.

Regression acceptance command: PG riêng với `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77`, static CODEX-G1 và migration helper. Miễn frontend/mobile và acceptance ngoài scope vì DTO/SQL/UI không đổi. Build/vet toàn repo bắt buộc. User đã cho phép G1, không dùng dữ liệu hay SMTP thật, giữ main/worktree.

Related test points: TestPolicyRegistryIsolationAndComposition, TestPolicyRegistryConflictingParameterRejected, TestRegisteredReportPolicyLegacyParity, TestRegisteredReportPolicyDoesNotLeakSources, TestReportPolicyRegistration; KN77 form/dispatch/PII và handler HTX/mixed-role/export/schedules.

Pure-first.exit1 lỗi compile receiver; pure-second app1.467s đạt trước điều chỉnh tầng registry. Boundary precheck39750.exit1 chặn import root KN mới trong shared fixture, đã gỡ (fixture vẫn dùng port dependency có sẵn). Pure-third93503.exit0 app0.994s/extension1.130s,0FAIL/SKIP, gồm immutable/composition/parity/scope/source/owner. Chờ fullPG/static thực; không coi adapter cũ đã gỡ.

Kết quả thực: PG84909.exit0 đủ13/13package,0FAIL/SKIP (handler:89.326s, assembly:68.985s, worker:1.863s, kn77:202.519s); production9bc7bb6b không đổi. Static40357.exit0 đủbuild/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary390/lint0issues. Gofmt toàn repo chỉ2fixture cũ được phép. ProofJSON hostTEMP agrigreen-codex-knsplit2reportsc-proof.json. Helper còn kiểm8migrationgate trước tích hợp. Sau D, danh mục nguồn KN trong domain.Sources/BuiltinTemplates và các bộ dựng dataset vẫn cần registry; không coi chỉ hết import là hoàn tất toàn bộ reports.


## KNSPLIT-2-REPORTS-D — Bỏ realm/config adapter (nền08121510)

Downstream consumers: reports templates/exports/schedules/coop-schedules/forms/dispatch; API, cả hai worker, KN77, handler fixture đã đăng ký policy. Dependency evidence: `rg -n 'reports.New|reports.Deps|readerRoles|schedulerRoles|sourceRoles|coopOnly|attachmentLimit' internal cmd tests -g '*.go'`. Không đổi source/catalog/data contract, UI hay SQL.

Regression acceptance command: PG riêng với `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77`, cổng CODEX-G1/helper8migrationgate. Miễn web/mobile/acceptance ngoài scope, build/vet toàn repo bắt buộc. User đã cho phép trong G1, không dữ liệu/SMTP thật, main/worktree giữ nguyên.

Related test points: TestReportPolicyRequiresRegistration, TestRegisteredReportPolicyRoleMatrix, TestRegisteredReportPolicyDoesNotLeakSources, TestPolicyRegistryIsolationAndComposition, TestPolicyRegistryConflictingParameterRejected; các ca KN77 session/phòng/tỉnh/PII và handler HTX/export/lịch.

Red2392.exit1 bắt đúng nil policy suy quyền/scope/param ngầm, empty registry PASS. Pure-first thiếu import domain (compile), đã khôi phục. Pure-second.exit0 app1.630s,0FAIL/SKIP, ma trận10vai và mixed HTX giữ phạm vi HTX, mẫu, nguồn và tác vụ. Chờ PG/static/helper thật; catalog/dataset/seed vẫn có việc tiếp theo.

Kết quả thực: PG65853.exit0 đủ13/13package,0FAIL/SKIP (handler:124.084s, assembly:107.913s, worker:2.382s, kn77:254.394s); production53a19e4d không đổi. Static44159.exit0 đủbuild/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary388/lint0issues. Gofmt toàn repo chỉ2fixture cũ được phép. Baseline chỉ xóa2entry import đã gỡ. ProofJSON hostTEMP agrigreen-codex-knsplit2reportsd-proof.json. Helper còn kiểm8migrationgate trước tích hợp; source/catalog/seed vẫn có gói tiếp theo.


## KNSPLIT-2-REPORTS-E — Catalog registry và consumer suite (nềndf2ba484)

Downstream consumers: reports/domain validate/build columns; reports/app template resolution/exports/schedules/policy/buildDataset; module Deps; KN ReportCatalog; API/hai worker/KN77; integration suite tests/reports. Dependency evidence: `rg -n 'domain.Sources|sourceSpecs|sourceSpec|Builtin|resolveTemplate|effectiveTemplates|CatalogRegistration|ReportCatalog|reports.New' internal cmd tests -g '*.go'`. Bảy test files chuyển từ reports/handler sang tests/reports, assertions giữ nguyên; fixture giờ dùng provider KN thật qua root lắp ráp. Thay command hiện hành để tránh chạy thư mục handler không còn test rồi báo đạt.

Regression acceptance command: PostgreSQL riêng, `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./cmd/api-platform/assembly ./cmd/worker ./tests/reports ./tests/kn77`, đủ static CODEX-G1/helper8migrationgate. Miễn frontend/mobile/acceptance ngoài scope vì contract/DTO wire/UI/SQL không đổi; build/vet toàn repo vẫn bắt buộc. User đã cho phép G1, không dữ liệu/SMTP thật, giữ main/worktrees.

Related test points: TestLegacyReportCatalogContract, TestRegisteredCatalogMatchesLegacy, TestCatalogRegistrationIsolation, TestCatalogRegistrationRejectsAmbiguity, TestReportCatalogMatchesLegacyContract; toàn bộ suite reports HTTP đã chuyển, KN77 quyền/phòng/tỉnh/PII/tenant.

Golden-capture.exit0/app1.810s từ legacy metadata, không từ provider mới. Pure-first chỉ compile unused import. Pure-second90371.exit0 app2.209s/domain0.836s/extension1.712s,0FAIL/SKIP:5source/5layout owner khớp byte và9source/9layout sau merge cùng validate/build. Không coi bộ dựng hoặc seed đã chuyển. Chờ PG/static/helper thực.

Kết quả thực: PG70172.exit0 đủ13/13package,0FAIL/SKIP (assembly:116.892s, worker:2.050s, reports:115.207s, kn77:289.793s); production67c70932 không đổi. Static60148.exit0 đủbuild/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary383/lint0issues. Gofmt toàn repo chỉ2fixture cũ được phép. Đối chiếu gitdf2ba484 xác nhận7file/19testfunction và assertion counts giữ nguyên sau chuyển tests/reports. Golden cũ và provider KN khớpbyte. ProofJSON hostTEMP agrigreen-codex-knsplit2reportse-proof.json. Helper còn kiểm8migrationgate trước tích hợp; F mới gỡ legacy catalog, data-builder/14gridseed còn gói sau.


## KNSPLIT-2-REPORTS-F — Catalog registration bắt buộc (nền2a877d33)

Downstream consumers: reports/domain defaults/validation/column build; app registry/template/export/schedule/policy/buildDataset; API/hai worker/KN77/tests-reports đã đăng ký owner KN thật. Dependency evidence: rg -n 'Sources|NativeSources|Builtins|sourceSpecs|builtinTemplates|CatalogRegistration|ReportCatalog' internal/platform/reports internal/kn/extension cmd tests -g '*.go'. Test suite HTTP hiện ở tests/reports, không tính handler cũ không có test.

Regression acceptance command: PostgreSQL riêng với go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./cmd/api-platform/assembly ./cmd/worker ./tests/reports ./tests/kn77, đủ static CODEX-G1/helper8migrationgate. Miễn UI/mobile/acceptance ngoài scope vì không đổi wire contract/SQL/UI; build/vet toàn repo bắt buộc. User đã cho phép G1, không dữ liệu/SMTP thật, giữ main/worktree.

Related test points: TestReportCatalogRequiresRegistration, TestRegisteredCatalogMergesContract, TestRegisteredCatalogColumnLevels, TestCatalogRegistrationIsolation/RejectsAmbiguity, TestReportCatalogMatchesLegacyContract; role matrix10vai/mixed HTX và KN77/session/PII.

Red65265.exit1/app1.630s bắt đúng missingcatalog suy5source/5builtin; emptycase PASS. Pure-first77518.exit0 app1.903s/domain0.606s/extension1.463s,0FAIL/SKIP. Golden giữ byte; legacy generator đã xóa, fixture đọc artifact E. Role tests vẫn loop instance catalog9source, không giảm xuốngnative4. Cột theo cấp được bảo vệ bằng registered golden, domain test generic giữ normalize/format/grouping. Chờ PG/static/helper.

Kết quả thực: PG76145.exit0 trênproduction224c98f9 đủ13/13package,0FAIL/SKIP (assembly:108.861s, worker:2.156s, reports:129.151s, kn77:405.491s). Static12762.exit1 chỉ2unused helpers unitCols/cols; cácgate build/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary383 trước lint đãqua. SauPGdừng,304da4aa chỉxóa11dòng là2privatefunction khôngcaller; khôngđổi runtime/import/schema. Staticcleanup99059.exit0:domain/app pure PASSnoSKIP/lint0issues/modgen/diff. GiữPGconsumerproof vìcleanupunreachable nội bộ và đãkiểmmodule; helpervẫnrebuild/vet/arch/gates/migrationtrênheadcuối. Đây làexemptionhẹpdựatheodiff+unusedproof, khônggọifullPGđãchạytrên304. ProofJSONhostTEMP agrigreen-codex-knsplit2reportsf-proof.json. Data-builder/14gridseed còn gói sau.


## KNSPLIT-2-REPORTS-G — Source builder registration (nềndefb45d8)

Downstream consumers: reports/app buildDataset/query/row conversion, constructor reports; KN ReportBuilders; API và2worker factory; KN77 vàtests/reports dùng provider thật. Dependency evidence: rg -n 'buildDataset|buildExtension|ReportBuilders|SourceBuilderRegistration|reports.New' internal cmd tests -g '*.go'. Native CGH/HTX, cap render/DLP và catalog không thay hành vi.

Regression acceptance command: PG riêng, go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./cmd/api-platform/assembly ./cmd/worker ./tests/reports ./tests/kn77, đủCODEX-G1/static/helper8migrationgates. Miễn frontend/mobile/acceptance ngoài scope vì wire contract/UI/SQL không đổi; build/vet toàn repo bắt buộc. User cho phépG1, không dữ liệu/SMTP thật, giữmain/worktree.

Related test points: TestRegisteredSourceBuilderBindsJobAndCopies, TestRegisteredSourceBuilderErrorIsFinal, TestSourceBuilderRegistryRejectsInvalid, TestReportSourceBuildersFiguresAndMissingTargets; suite HTTP export/lịch/HTX và KN77 quyền/snapshot/PII.

Pure-first2490.exit0 app1.746s/extension1.376s; pure-second51584.exit0 app1.105s/extension1.496s,0FAIL/SKIP. Figures xác nhận3.3ha/5báo cáo,30/300=10%/2HTX, missingtargetnull,5nguồn/groupbyvariety/completeness; corejob/scope/period/alias/errorfinal. ChờPG/static/helper, adapter nil-legacy còn trongG.

Kết quả thực: PG29811.exit0 đủ13/13package,0FAIL/SKIP (assembly:128.531s, worker:2.342s, reports:87.318s, kn77:201.760s); productionc9f07902 không đổi. Static25052.exit0 đủbuild/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary383/lint0issues. Gofmt toànrepo chỉ2fixtureoldallowed. Suite HTTP dùngbuilderKNthật vàgiữmọiassertion; pure5nguồn/missingtarget/alias/errorfinal đạt. ProofJSONhostTEMP agrigreen-codex-knsplit2reportsg-proof.json. Helper còn8migrationgate trướcdev; H mới gỡ adapter legacy,14seed còn gói sau.


## KNSPLIT-2-REPORTS-H — Builder bắt buộc (nền51d9ebf5)

Downstream consumers: reports/app buildDataset vànativeCGH/HTX helper; KNprovider5source; API/2worker/KN77/tests-reports. Dependency evidence: rg -n 'Extension|buildExtension|buildProgram|SourceBuilder|ReportBuilders|seasonLabel|limit' internal/platform/reports internal/kn/extension cmd tests -g '*.go'. GiữConsolidations/DispatchHistory vàhelper native, không đổi wire/SQL/UI.

Regression acceptance command: PG riêng, go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./cmd/api-platform/assembly ./cmd/worker ./tests/reports ./tests/kn77; staticCODEX-G1 vàhelper8migrationgates. Miễnweb/mobile/acceptance ngoài scope vìkhôngđổicontract/UI/SQL; build/vettoànrepo. Usercho phépG1, khôngSMTP/data thật, giữmain/worktree.

Related test points: TestSourceBuilderRequiresRegistration, TestRegisteredSourceBuilderBindsJobAndCopies/ErrorIsFinal, TestReportSourceBuildersFiguresAndMissingTargets, suiteHTTP vàKN77. Redconfirm.exit1 bắt missingregistry gọilegacy,emptyPASS. Pure74843.exit0 app2.052s/extension1.181s,0FAIL/SKIP. ChờPG/static/helper.


Kết quả thực: PG24203.exit0 đủ13/13package,0FAIL/SKIP, productionb43180db không đổi: assembly100.916s/worker2.196s/tests-reports106.907s/KN77252.802s. Static86292.exit0 đủbuild/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary383/lint0issues. Gofmt chỉ2fixturecũ được phép. ProofhostTEMP agrigreen-codex-knsplit2reportsh-proof.json. Không đổi API/SQL/UI nên miễn frontend/mobile; helper còn8migrationgate trước dev.14gridseed còn gói sau.

## KNSPLIT-2-REPORTS-I — Form seed registry (nền59621835)

Downstream consumers: reports/app seed transaction; constructor/handler; KN ReportFormSeeds; API/haiworker/KN77/tests-reports vàCLIformimport. Dependency evidence: rg -n 'TrongTrotSeeds|SeedTrongTrot|SeedForms|ReportFormSeeds|FormSeedRegistration|reports.New' internal cmd tests -g '*.go'. Giữ API/SQL/UI; frontend/mobile miễn vì không đổi contract/fixtures, ownergoldenbyte kiểm layout/formula cũ.

Regression acceptance command: PG riêng với go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./cmd/api-platform/assembly ./cmd/worker ./cmd/formimport ./tests/reports ./tests/kn77; đủstaticCODEX-G1/helper8migrationgates. User đã cho phép G1, không SMTP/data thật, giữmain/worktrees. Không dùng DB skip làm bằng chứng.

Related test points: TestLegacyFormSeedContract, TestReportFormSeedsMatchLegacyContract, TestRegisteredFormSeedsIsolationAndCopies, TestFormSeedRegistryRejectsInvalid; TestSeedExportUsesOwnerLayout; HTTPFormLifecycleSoD/ImportEndpoint và KN77. I còn nillegacy, explicit empty không suy seed.


Kết quả thực: PG16172.exit0 đủ14/14package,0FAIL/SKIP trênproduction0e709333: assembly110.482s/worker2.298s/CLI2.054s/tests-reports95.188s/KN77249.011s. Static20646.exit0 đủbuild/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary383/lint0issues. Owner14seed/goldenbyte/registryboundary/CLI226cộtHR đạt. ProofhostTEMP agrigreen-codex-knsplit2reportsi-proof.json; helper còn8migrationgate trướcdev. J mới gỡlegacy vàchuyểnca seed-engine/import/webfixture ra compositionroot, không đổiassertion/fixture.

## KNSPLIT-2-REPORTS-J — Seed registration bắt buộc (nền69410282)

Downstream consumers: reports seed service/constructor/handler; KN14seed; API/haiworker/KN77/tests-reports; CLIformimport; tests/reportforms gồm15testfunction chuyển nguyên assertion từ3tệp cũ. Dependency evidence: rg -n 'FormSeed|SeedForms|ReportFormSeeds|TrongTrotSeedDefinitions|TrongTrotSeeds|DefaultDepartment' internal cmd tests -g '*.go'. Engine/import/webfixture test chuyển sang compositionroot thuần; không kéo KNroot vào module dùng chung.

Regression acceptance command: PGriêng, go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./cmd/api-platform/assembly ./cmd/worker ./cmd/formimport ./tests/reportforms ./tests/reports ./tests/kn77; đủstaticCODEX-G1/helper8migrationgate. Dự kiến13packagecótest, không tính form/formimport nay không có test. Frontend/mobile miễn do contract/UI/fixtures không đổi byte; build/vet toànrepo. User cho phép G1, khôngSMTP/data thật, giữmain/worktrees.

Related test points: TestFormSeedsRequireRegistration; TestRegisteredFormSeedsIsolationAndCopies/RejectsInvalid; TestReportFormSeedsMatchLegacyContract;15ca engine/formula/Excel/webfixture; CLISeedExportUsesOwnerLayout và HTTP/KN77. Red99079.exit1 nil suy14,emptyPASS; giữ frozenartifactI, không còn capturegenerator.


Static-first15919.exit1 bắt4importimplementationtạicompositionroot,R02;khôngnớicổng. ĐợifullPG43670.exit0/13package/0FAIL/SKIP trên1e900978 vàCIMhếttiếntrình mới thêm module_form_engine.go:7wrapperpure/typeerror-operation/layout/constants ởgốcreports, dùngengineimplementationhiệnhành. Tests/reportforms chỉimportKNroot/reportsroot/port,15Testnames/assertioncounts giữ (TEMPtestpoints.json),golden/webfixturesbytekhôngđổi. Pure-facade68454.exit0 trênmãcuối, gồmreportforms2.195s/CLI2.196s. ChạylạifullPG-final vàstatic-final trênproductioncuốitrướchelper, khôngdùngPGtrên1e900978thayproofcuối.


Kết quả cuối: PG-final18936.exit0 đủ13/13packagecótest,0FAIL/SKIP trênproduction3289316b: assembly:97.229s, worker:2.515s, formimport:2.417s, reportforms:2.198s, reports:108.082s, kn77:245.471s. Static-final75388.exit0 đủbuild/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary383/lint0issues; lỗiR02đãsửatrongmã,khôngnớicổng. ProofhostTEMP agrigreen-codex-knsplit2reportsj-proof.json. 15testpoints/assertioncounts/goldenI/webfixturesgiữ. Helpercòn8migrationgate trướcdev; sourceDTO/dispatchcontent vàKNSPLITkhácchưa xong.

## KNSPLIT-2-REPORTS-K — Dispatch profile registration (nền5e116e26)

Downstream consumers: reports PolicyRegistry/dispatch creation/send/worker/fileURL/publiclink/DLP; dispatch BodyFor generic; KNReportPolicy provider đã nối5compositionroots; API/worker/KN77/tests-reports vàSDK/CLI. Dependency evidence: rg -n 'DispatchProfile|dispatchProfile|BodyFor|dispatchFileKey|TemplatePrefix|SheetTemplateKey|ReportPolicy' internal cmd tests -g '*.go'. Không đổi identity/scope, sổ hoặc wirecontract.

Regression acceptance command: PGriêng, go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./cmd/api-platform/assembly ./cmd/worker ./cmd/formimport ./tests/reportforms ./tests/reports ./tests/kn77; staticCODEX-G1/helper8migrationgates.13packagecótest, frontend/mobile miễn vì API/UI/fixtureskhôngđổi; build/vet toànrepo bắt buộc. Usercho phépG1, khôngSMTP/data thật, giữmain/worktree.

Related test points: TestLegacyDispatchProfileContract, TestRegisteredDispatchMessagesMatchLegacyContract, TestRegisteredDispatchProfileCopiesAndIsolation, TestDispatchProfileRegistrationRejectsInvalid, TestReportDispatchProfileMatchesLegacyContract; HTTP/email/linkdedup/PII vàKN77. K cònnillegacy vàBodycũ, Lmớigỡ.

Pure-first27369.exit0:app1.805s/dispatch0.638s/extension2.088s/reportforms1.818s/CLI2.065s,0FAIL/SKIP; capturelegacyexit0 vàmodgen/docs/boundary383/diffPASS. Đối chiếu thực4egresssite:3trongdispatch.go(tệp/gsheet/tải) và1publiclink;5là sốscalarprofile, không phải sốsite. Chờ13PG/static/helper.


Kết quả thực: PG21606.exit0 đủ13/13packagecótest,0FAIL/SKIP trênproduction065c1058: assembly:102.166s, worker:2.250s, formimport:2.020s, reportforms:2.157s, reports:104.462s, kn77:248.402s. Static18495.exit0 đủbuild/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary383/lint0issues.4egresssite/defaultsubject/body qua profile owner; golden4messagecases/provisional/links/keys vàregistryboundaries đạt. ProofTEMP agrigreen-codex-knsplit2reportsk-proof.json. Helpercòn8migrationgate trướcdev; L mới gỡlegacyprofile/Body/capture, sourceDTO vàregistrykhác còn.

## KNSPLIT-2-REPORTS-L — Dispatch profile bắt buộc (nềnbb8dffb2)

Downstream consumers: reports app dispatch/profile/worker/file/link/DLP vàgenericBodyFor, KNReportPolicy,API/haiworker/tests-reports/KN77. Dependency evidence: rg -n 'dispatchProfile|DispatchProfile|legacyDispatchProfile|BodyFor|dispatch.Body|dispatchFileKey' internal cmd tests -g '*.go'. Khôngđổiwire/SQL/UI/identity/scope/token/recipientdedup.

Regression acceptance command: PGriêng, go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./cmd/api-platform/assembly ./cmd/worker ./cmd/formimport ./tests/reportforms ./tests/reports ./tests/kn77; đủstaticCODEX-G1/helper8migrationgate. Frontend/mobile miễn vì API/UI/fixturesbytekhôngđổi; build/vettoànrepo. User cho phépG1,khôngSMTP/data thật,giữmain/worktree.

Related test points: TestDispatchProfileRequiresRegistration; TestRegisteredDispatchMessagesMatchLegacyContract; TestRegisteredDispatchProfileCopiesAndIsolation; TestDispatchProfileRegistrationRejectsInvalid; TestReportDispatchProfileMatchesLegacyContract; nativeMessageHelpers/GSheetValve vàHTTP/KN77/DLP/PII/dedup. Red1325.exit1 nilsuykn.,emptyPASS;giữfrozenartifactK,khôngcòngenerator.

Pure-first4346.exit0 app1.048s/dispatch0.578s/extension1.347s/reportforms2.008s/CLI1.982s,0FAIL/SKIP; nil/empty failclosed, owner/frozenmessage/provisional/link/keys/registryboundary giữ. Hai mô tả Go cũ đồng bộ việc seed thuộc ứng dụng saupure, không đổi executable. Chờ13PG/static/helper trênheadcuối.


Kết quả thực: PG31787.exit0 đủ13/13packagecótest,0FAIL/SKIP trênproduction2d72d02e: assembly:104.953s, worker:2.184s, formimport:2.182s, reportforms:2.043s, reports:93.995s, kn77:247.438s. Static68012.exit0 đủbuild/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary383/lint0issues. Missing/emptyprofiledenied, owner/genericfrozenmessages và4DLPsite/copy/invalid/duplicate đạt;khôngđổiartifactK/I/webfixtures. ProofTEMP agrigreen-codex-knsplit2reportsl-proof.json. Helpercòn8migrationgate trướcdev. SourceDTO/data/registrykhác/tenant/client/KHTCremindercòn.

## KNSPLIT-2-REPORTS-M — Consumer nguồn dữ liệu qua KNport (nền5cb2d5c8)

Downstream consumers: KNextension app/program+reportdata/store/reportdata/module/builder/catalog+test/handler source integration; tests/reports fakeData; generic builder/query/export; API/worker/KN77/SDK/CLI. Dependency evidence: rg -l 'extensionsrc\.(ReportQuery|ReportData|Completeness|SowingRow|SowingData|PestRow|PestData|ProgramRow|ProgramData|TaskRow|TaskData|SummaryRow|SummaryData)' -g '*.go' .; rg -l 'reportsport.Source(Sowing|GrowthPest|ProgramProgress|TasksSLA|PeriodSummary)' -g '*.go' .; inventoryTEMP agrigreen-codex-knsplit2reportsm-callers.json.9changedcallers,13DTO+5IDs; khôngplatformconsumer phảiimportKN.

Regression acceptance command: PGriêng, go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./internal/kn/extension/app ./internal/kn/extension/store ./internal/kn/extension/handler ./cmd/api-platform/assembly ./cmd/worker ./cmd/formimport ./tests/reportforms ./tests/reports ./tests/kn77.14testpackagesdựkiến,app/storekhôngtestnhưngcompile;fullKNhandler bảo vệSQLproducer/scopes. ĐủstaticCODEX-G1/helper8migrationgate; miễnfrontend/mobile vì wire/UI/fixtureskhôngđổi; build/vet toànrepo. Usercho phépG1,khôngSMTP/data thật,giữmain/worktree.

Related test points: TestProgramProgressFR03, TestReportDataSowingAndSummary, TestReportSourceBuildersFiguresAndMissingTargets, genericSourceBuilderBindsJobAndCopies, ownerCatalog/seed/profilegoldens, HTTP/export/KN77. Mchỉaliasvàqualifierkhôngbehavior;N mới gỡcapdefinitions.

Pure-first1203.exit0,0FAIL/SKIP gồmowner/registry/golden/SDK/CLI;KNapp/storecompilekhôngcótest. Script bảo vệ import đơn/nhóm import cuối, không chạyGo khi chưa đủ9callers; đã hoàn tất vàgofmt, không cóCodecompilefailure. Cần14PGproducer/fullhandler vàconsumer trướcdev.


Kết quả thực: PG48903.exit0 đủ14/14packagecótest,0FAIL/SKIP trênproduction21d2518f: handler:124.852s, assembly:102.724s, worker:2.211s, formimport:1.994s, reportforms:2.116s, reports:94.348s, kn77:253.871s. KNhandlerfull124.852s bảo vệSQLproducer/FR03/scopes;KNapp/storecompilekhôngtest. Static38201.exit0 đủbuild/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary383/lint0issues. AliasMgiữtypeidentity/5ID/goldenbyte, chưa chuyểndefinitions. ProofTEMP agrigreen-codex-knsplit2reportsm-proof.json; helpercòn8migrationgate trướcdev, N mới gỡcap13DTO/5ID.

## KNSPLIT-2-REPORTS-N — Ownership DTO và generic snapshot contract (nền2c26b5a8)

Downstream consumers: KNData producer/builder/catalog vàhandler, reportcycle/app+module glue; capreports app/module dispatch renderer; API/worker/KN77/HTTP/SDK/CLI. Dependency evidence: rg -n 'reports/port/extensionsrc' -g '*.go' . trướcđổi=7files(6genericcallers+KNaliasfile); TEMP agrigreen-codex-knsplit2reportsn-callers.json. Source13definitions/5IDs thànhKNowner; generic6types/error vềcapport; sođốiGo tokens/IDs vớiHEADbase trongTEMPstructure.json, khôngthaySQL/math/JSON.

Regression acceptance command: PGriêng, go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./internal/kn/extension ./internal/kn/extension/app ./internal/kn/extension/store ./internal/kn/extension/handler ./internal/kn/extension/reportcycle/app ./cmd/api-platform/assembly ./cmd/worker ./cmd/formimport ./tests/reportforms ./tests/reports ./tests/kn77;15testpackagesdựkiến. CổngCODEX-G1/helper8migrationgate. Miễnweb/mobile vìwire/UI/fixturesbyteskhôngđổi;build/vet toànrepo. Usercho phépG1,khôngSMTP/data thật,giữmain/worktree.

Related test points: ProgramProgressFR03/ReportDataSowingAndSummary/fullKNhandler SQLscope; owner5sourcefigures/missingtargets/metadata golden; reportcycle calendar/provincialimport; capHTTP/render/dispatch/link/DLP/dedup vàKN77. Không thêmtestmirror definitions; thựcchạybehaviorproducer/consumer.


Kết quả thực: PG95125.exit0 đủ15/15packagecótest,0FAIL/SKIP trênproduction6e1b094c. Static29556.exit0 đủbuild/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary382/lint0issues. Source13definitions/5IDownKN,generic6contractscapport,0oldSrcimports; tokens/IDs/errorstring/goldens/webfixturesgiữ. Baseline383→382 chỉoldinventorycouplingđãxóa,canonicalpathsuntouched. ProofTEMP agrigreen-codex-knsplit2reportsn-proof.json. Helpercòn8migrationgate trướcdev;registry/data/tenant/client/KHTCremindercòn.

## KNSPLIT-2-MDPARAMS-A — Registry tham số (nền 05b77fab)

Downstream consumers: masterdata Seed/ListParams/RestoreDefault/Display; KN reportcycle/cntsalert/cntscal/outbreak đọc tham số đã duyệt; API assembly, worker (reports/notify/feeds/kref và công việc extension/accounts), KN77. Evidence: rg -n 'masterdata.New\(' --glob '*.go' internal tests cmd (17 call sites trước đổi); rg -n 'SpecOf\(|ParamRegistry|Defaults\(|ValidateProposal\(' internal/platform/masterdata. KN chỉ đăng ký tại 6 composition files/10 constructor calls; các command khác không lắp KN vẫn dùng native.

Acceptance: private PG, go test -v -count=1 -p 1 -timeout 55m ./internal/platform/masterdata/... ./internal/kn/extension ./internal/kn/extension/reportcycle/... ./internal/kn/extension/cntsalert/... ./internal/kn/extension/cntscal/... ./internal/kn/extension/outbreak/... ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77. Native core metadata/display/GIS and owner calendars/alerts/GPS/outbreak covered; tests without files compile only. Full build/vet/static CODEX-G1 + helper migration8gate. Web/mobile exempt: API wire, fixtures and UI unchanged.

Test points: registry deep copy, multi-instance isolation, conflict/duplicate/cross-owner seed, bad date/decimal/color; frozen old metadata/default/date/source golden; new HTTP TestRegisteredParamsSeedRestoreAndTenantIsolation proves RLS, idempotent seed, permissions and restore through four-eye approval. Existing SQL seed/calendar/alerts/KN77 regression retained. First pure test compilation failed on a map-value assignment in test code, corrected before full regression; no DB test claimed from that attempt.


Kết quả thực trên production27248e07: PG90646.exit0 đủ16/16 package có test, 0FAIL/SKIP (handler:282.126s, handler:114.069s, handler:106.552s, handler:117.097s, assembly:111.522s, worker:2.764s, kn77:301.189s). TestRegisteredParamsSeedRestoreAndTenantIsolation PASS6.75s; frozen16spec/13seed, registry invalid/crossowner/deepcopy/isolation/color đạt. Static81712.exit0 đủ build/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary382/lint0issues; gofmt chỉ2fixturecũ. ProofTEMP agrigreen-codex-knsplit2mdparamsa-proof.json. Helper còn8migrationgate trước dev. B mới xóa legacy và chuyển11keys/helper về KN.

## KNSPLIT-2-MDPARAMS-B — Gỡ nguồn cũ, KN sở hữu khóa (nền c098c8bd)

Downstream consumers: masterdata metadata/default/seed/restore/display; KN cntsalert config/evaluate, extension app schedule_dept GPS; API/worker/KN77. Evidence: rg -l 'ParamCNTS|IndicatorKey' internal cmd tests -g '*.go' = 4 KN callers + old cap port/domain registry before deletion; TEMP structure.json proves exact old 11-key/helper source block and native JSON row data unchanged, including MRV. Shared handler fixture needs no KN seed assertions; actual owner registration stays at API/worker roots.

Acceptance: private PG, go test -v -count=1 -p 1 -timeout 55m ./internal/platform/masterdata/... ./internal/kn/extension ./internal/kn/extension/app ./internal/kn/extension/store ./internal/kn/extension/handler ./internal/kn/extension/reportcycle/... ./internal/kn/extension/cntsalert/... ./internal/kn/extension/cntscal/... ./internal/kn/extension/outbreak/... ./cmd/api-platform/assembly ./cmd/worker ./tests/kn77. 17 test packages expected (A16 + KNhandler), app/store compile-only. Full static CODEX-G1/helper8migration; web/mobile exempt because wire/UI/fixtures unchanged.

Test points: TestParametersRequireRegistration nil/empty RED exit1 on A (1.855s), then GREEN; owner frozen metadata/default/date/source; registry invalid/deepcopy/crossowner/color; TestRegisteredParamsSeedRestoreAndTenantIsolation; schedule GPS, alert thresholds, CNTS calendar/outbreak, KHTC snapshots and native GIS/default regression. No test assertions weakened. Dead global default cache and capture removed; frozen artifact retained.


Kết quả thực trên production8f5d7a93: PG24588.exit0 đủ17/17 package có test, 0FAIL/SKIP (masterdata/handler:159.806s, extension/handler:67.702s, cntsalert/handler:78.176s, cntscal/handler:72.681s, outbreak/handler:76.644s, api-platform/assembly:65.727s, cmd/worker:2.088s, tests/kn77:195.757s). Static40483.exit0 đủ build/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary371/lint0issues; gofmt chỉ2fixturecũ. Red→green nil/empty, owner frozen16spec/13seed, native JSON/MRV nguyên dữ liệu, 11-key/helper nguyên source block. ProofTEMP agrigreen-codex-knsplit2mdparamsb-proof.json. Helper còn8migrationgate trướcdev; phần catalog/registry khác/data/tenant/client/KHTCreminder còn.

## KNSPLIT-2-MDCATALOG-A — Schema/seed registry (nền c9a9674d)

Downstream: masterdata create/update/import/seed/metadata/at/sync, KN typed catalog consumers (reportcycle/cntsalert/cntscal/outbreak), API/worker/KN77, new pure composition tests/masterdatacatalogs. Evidence: rg knownCatalog/itemAt/EffectiveDated/Normalize/SyncCatalogs within masterdata/app;12 schema/60 seed actualinventory;10 KN constructor calls+sharedfixture. Core FieldError alias preserves error identity; validators get no principal/tenant/SQL.

Acceptance: private PG, full B17-package scope + ./tests/masterdatacatalogs (18 test packages), then go test -v -count=1 -p 1 -timeout 55m -run '^TestCatalogRegistryConstraintUpgrade$' ./migrations; full static CODEX-G1 + helper migration8mandatory and newupgrade test. Web/mobile exempt because wire/UI/fixtures unchanged. New generic datecatalog is tested without KN provider; upgrade on synthetic1000 preexisting ledger rows, checksum and verify_chain. No production-size claim.

Related test points: TestCaptureLegacyCatalogContract (temporarycapture, remove inB), TestOwnedCatalogsMatchLegacyContract (12schemas/60seeds/170cases), CatalogRegistryCopiesAndIsolation/RejectsInvalid, RegisteredCatalogEffectiveDatesAndIsolation, CatalogRegistryConstraintUpgrade. Prior pure-contract exit0; compile errors importspace and missingcheckRefs receiver corrected before fullPG. Existing assertions retained.


Cổng tĩnh đầu trên98de1e3a dừng ở vet (alias public khiến literal hai trường phải ghi tên trường). PG đầu được dừng đúng driver/child và cluster đã cleanup, db-full.exit=-1; không coi các PASS lẻ là hồi quy đủ. Sửa bằng AST chỉ thêm Field/Err hoặc Required/Normalize, giữ nguyên biểu thức/thứ tự ở5file (proofTEMP keyed.json). Script thử đầu lỗi đường dẫn trước khi viết mã, hai tệp tạm của nó đã chuyển về hostTEMP debug .txt; không còn trong repo. Pure-keyed73636.exit0, frozen12/60/170 vẫnkhớp; vet-keyed.exit0. Sắp chạy lại fullcode18+upgrade và static-final trên head cuối, không dùng proofPG bịdừng.


Lượt FINAL đầu (e6b50ace) không đạt: static-final lỗi ST1012 tên2biến lỗi riêng; generic HTTP test phát hiện2nhánh list/item vẫn gọi domain.EffectiveDated nên ?at bỏqua registry và trảv2 thayv1. Dừng đúng driver41135/child, cluster56892 cleanup, khôngtínhPASS. Đã nối2nhánh qua Service.CatalogEffectiveDated và bổsungassert listAt; đổi tên2biến errCatalog*. Chạy targeted PG date/RLS/sync +newupgrade trước full18 trênhead đãsửa. Giữ log cũ làm proofFAIL, khôngoverwrite.


Verified-code51844.exit0 đủ18/18packagecótest,0FAIL/SKIP trênproductionb0f4bb2f; KN77249.112s/composition2.159s, actualGo/testprocess0. Đã sửa CHỈ snapshot trong test nâng cấp sang tenant.WithTenantTx, giữ SQL/prodGo nguyên; không cần lặp18package vìtestmigrationskhôngnằmtrong18packageđãchạy. Chạy lại newupgrade riêng. Bước chuẩn bị script test từng bị auto-review chặn khiGođangchạy; đãchờkếtthúc vàxácnhậnprocess0 trước sửa trực tiếp, khôngbypass.


Kết quả cuối: code18/18 verified-code51844.exit0 trên productionb0f4bb2f, 0FAIL/SKIP. Chỉtest snapshot sauđó sửa scope (05681eca), SQL/prodGo khôngđổi: upgrade-repaired47221.exit0/PASS178.001s (test74.24s),1.000 dòng checksumnguyênqua3phase và1.001dòngchuỗibămsạchsauthêmtênmới. Static-repaired85467.exit0 fullgates/boundary371/lint0; test-followup vet/lint0; gofmtchỉ2fixtureold. Frozen12schema/60seed/170casegiữ. ProofTEMP agrigreen-codex-knsplit2mdcataloga-proof.json. Auto-review từng chặn prep/vet khi Gođangchạy; đã chờactualexit/process0 và thực hiện tuần tự, khôngbypass. Cầnhelper9migration (8mandatory+newupgrade) trướcdev. CatalogB/dependencies/tenant/client/KHTCreminder/G1 còn.
