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
Regression acceptance command: PG thật `go test -v -count=1 -p 1 -timeout 55m ./internal/platform/extension/... ./internal/platform/forms/... ./cmd/api-platform/assembly ./internal/platform/party/... ./tests/kn77`; migration gates; các cổng tĩnh CODEX-G1, boundary-base. Web `npm run check:api`, typecheck/lint, full Vitest, build:mock và browser editor. Golden menu sinh bằng TestWebMenuFixture trên PG thật.
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
