## KNSPLIT-2-REPORTS-J — Bỏ bộ mẫu KN implicit khỏi capability

Nền69410282 đã đăng ký KNReportFormSeeds tại5compositionroots. J gỡ legacyFormSeedSet, form/seed.go, port.DefaultDepartment và module.TrongTrotSeedDefinitions; nil/empty/no-default registry không suy mẫu KN. SeedForms giữ actor/tenant/quyền/giao dịch/nháp/SoD và API cũ; default department/version lấy từ đăng ký owner. Không đổi schema hoặc HTTP/UI.

Red99079.exit1/app1.635s bắt nil registry vẫn suy14mẫu; empty PASS. Xóa generator chụp legacy, giữ nguyên goldenI. Chuyển3tệp kiểm thử form-engine/webfixture/importer sang tests/reportforms (composition root thuần dùng provider KN thật), giữ15testfunction cùng assertion counts và dữ liệu đầu vào; chỉ hợp helper seed trùng. HTTP importer payload cũng lấy owner. Các đường dẫn contract/webfixtures đổi theo vị trí mới, không thay byte artifact. Lượt pure đầu đã qua app/owner/15ca nhưng CLI build bắt importform dư sau xóa wrapper; xóa import rồi chạy lại toàn bộ pure trước PG.

Cần13package cótest sau chuyển (form vàformimport không còn test riêng, thêmtests/reportforms), đủstatic/helper8migrationgate trướcdev. Các source DTO/dispatch ports còn cần kiểm kê riêng; không coi toàn bộ KNSPLIT-2 hoặc G1 đã xong. Giữ tenantKNtheotỉnh,MRVHTX/MKG,D2KNhồsơ,seedKHTC,M1–M7.


Static-first15919.exit1 bắt4importimplementationtạicompositionroot,R02;khôngnớicổng. ĐợifullPG43670.exit0/13package/0FAIL/SKIP trên1e900978 vàCIMhếttiếntrình mới thêm module_form_engine.go:7wrapperpure/typeerror-operation/layout/constants ởgốcreports, dùngengineimplementationhiệnhành. Tests/reportforms chỉimportKNroot/reportsroot/port,15Testnames/assertioncounts giữ (TEMPtestpoints.json),golden/webfixturesbytekhôngđổi. Pure-facade68454.exit0 trênmãcuối, gồmreportforms2.195s/CLI2.196s. ChạylạifullPG-final vàstatic-final trênproductioncuốitrướchelper, khôngdùngPGtrên1e900978thayproofcuối.
