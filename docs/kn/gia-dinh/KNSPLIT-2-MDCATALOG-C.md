## KNSPLIT-2-MDCATALOG-C — KN sở hữu cách tra danh mục CNTS

Nền 55d424cf. Thêm port EffectiveCatalogs dùng chung: ItemAt/ListAt trả mục và ngày hiệu lực UTC DATE; CodeByAttribute tra mã hiện trạng theo thuộc tính đã khai báo. Tenant từ context, known-catalog/attribute check và giao dịch r_masterdata nằm trong capability; SQL bind cả tên thuộc tính và giá trị, không nội suy. Giữ nguyên sentinel ErrNotEffective (chuyển vị trí khai báo cùng package).

Adapter extension.NewCNTSCatalogs sở hữu phép chuyển cơ sở giết mổ, lọc xã, phạm vi cơ sở đang dùng hôm nay, tra lịch tiêm/danh sách mũi và ánh xạ vaccine–loài. Dùng parser/CodeList thuộc KN đã có ở A. Bốn consumer production API (KN outbreak/cal, forms, CNTS aggregate, accounts facility scope) nối qua adapter owner. Không gọi HTX, không đưa import KN vào module dùng chung. Interface/DTO CNTS cũ còn tạm ở masterdata/port; rules ghi refs/stableUUID/commune/stamp còn core để tách ở các bước sau. D mới gỡ implementation/facade lookup cũ; không tuyên bố masterdata/G1 hoàn tất.

Pure4 package đạt. TestW1141_CNTSCatalogs chạy đường owner thật và giữ các assertion trước; thêm 17 phép so sánh đầy đủ giá trị/thứ tự/lỗi với legacy trên cùng DB (3 ngày ×4 lookup + ItemAt, missing facility, vaccine, unknown refs, communes). Test generic không có provider KN kiểm ItemAt/ListAt/date/lookup, SQL values, cross-tenant/undeclared attr; pure negative unknown schemas chặn trước storage. Golden12/60/170 và params16/13 giữ nguyên byte.

Hồi quy dự kiến22gói: B18 + accounts app/domain/handler/port (consumer phạm vi cơ sở). Suite tests/masterdata chạy đầu để fail fast, sau đó21package cùng cụm PG riêng; không chạy song song hay chỉnh Go trong khi test. Toàn bộ cổng tĩnh, baseline không tăng, helper9migration (8mandatory+upgrade) trước dev. Không đổi HTTP/OpenAPI/UI/mobile hay migration; không cần chạy web/mobile. Giữ D2 hồ sơ KN, tenant mỗi Trung tâm tỉnh, MRV HTX/MKG và M1–M7.


Static đầu trênbf8c0f30 đạt build/vet/modgen/migrate/archlint/contract/docs/MFA/boundary369, dừng tại errorlint do3negative-test so lỗi bằng !=. Sửa test dùng errors.Is, không đổi production; chạy lại targeted pure và static-repaired. Không coi static ban đầu là đạt; giữ log/exit ban đầu.


C22 actualexit0 (session53219), 0FAIL/SKIP trên productionbf8c0f30/testfix3c7f7ea4; cụm62461 đãcleanup, Go0. tests/masterdata180.569s; cntsalert109.532s, cntscal131.305s, outbreak136.742s, accounts-handler135.770s, assembly130.649s, worker2.142s, KN77402.435s, frozen2.519s. Proof codehostTEMP agrigreen-codex-knsplit2mdcatalogc-code-proof.json. Sau khi kết thúc mới bổ sung test-only capture17legacy DTO, canonicalUUID2fixture, erroridentity3sentinels. D sẽ dùng cùnggolden thay legacy. Production không đổi; cần targeted capturePG và vet/lint followup rồi helper9, không suy PASS từ lượt chưa chạy.


Kết quả chốt C: đủ22/22PG trên productionbf8c0f30,0FAIL/SKIP; capture-only93037 actualexit0/PASS134.589s trên testhead091019bb, 17legacylookupcases đãghi tests/golden/kn-masterdata-lookups.json, canonical2fixtureUUID+zero , nạplạikhớp. DeltaGo sauproductionchỉ3testfiles (errorlintfix+capturehelper); khôngđổi thuậttoán/SQL sau22. Followup5432 actualexit0: vet/lint0issues/archlint/docs/modgen-check/knboundary369;gofmt2fixturecũ. ProofhostTEMP agrigreen-codex-knsplit2mdcatalogc-proof.json. Đủcode/PG/static; helper9migration và remoteverify còntrướckhitínhđãtíchhợp. D giữ17goldenassertions, gỡcapture vàlookup/facadelegacy; refs/stableUUID/commune/stamp vàDTO/constphasekế,G1chưahoàntất.
