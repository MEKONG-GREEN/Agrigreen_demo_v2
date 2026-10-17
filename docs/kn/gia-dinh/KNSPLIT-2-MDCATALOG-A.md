## KNSPLIT-2-MDCATALOG-A — Registry định nghĩa danh mục

Nền c9a9674d. Thêm CatalogRegistration/CatalogDefinition/AttributeSpec/FieldError generic ở port; registry immutable theo instance. KN sở hữu 12 schema (nhãn, thuộc tính, validator thuần, cờ hiệu lực/đồng bộ, ngày seed) và 60 dòng nạp sẵn. Callback chỉ nhận dữ liệu danh mục/thuộc tính; tenant, principal, quyền, giao dịch và ghi sổ giữ ở core. Không đưa KN import vào module dùng chung.

Engine NormalizeWith dùng chung kiểm mã/tên/thuộc tính; FieldError cũ alias port để giữ errors.As và thông điệp. Root constructor dùng Deps{Params,Catalogs}; native hai đối số giữ được, 10 constructor calls của API/worker và fixture nối rõ. Metadata/Seed/List/At/Sync/Import/chuẩn hóa dùng registry trước legacy. A còn fallback và định nghĩa cũ tới B; typed CNTSCatalogs, tham chiếu cơ sở/mộc dấu còn phase tiếp, chưa xong cả masterdata.

Chụp từ đường cũ tests/golden/kn-masterdata-catalogs.json: 12 schema,60 seed,170 ca chuẩn hóa/lỗi. Test composition tests/masterdatacatalogs dùng module KN thật + facade chung; pure-contract exit0. Registry copy/isolation/invalid/native-protection đạt; lượt compile đầu lỗi import có dấu cách và free checkRefs thiếu receiver, đã sửa trước hồi quy. Không suy PASS từ lượt lỗi.

Storage catalog_effective cũ chỉ nhận cnts_*. Registry danh mục có ngày cần hoạt động độc lập KN. Ba migration forward riêng (migratecheck-new): 20261017022108 expand CHECKgeneric NOT VALID; 20261017022111 validate; 20261017022114 contract chỉ bỏ CHECKcũ sau khi mới valid. Mỗi tệp một transaction; lock_timeout5s; không ADD COLUMN/UPDATE/DELETE/ghi lại dòng hoặc thay hash. Runbook docs/runbooks/masterdata-catalog-registry.md. Không sửa migration đã áp dụng.

Thêm ca generic datecatalog không có provider KN: CRUD/ngày hiệu lực/RLS/quyền/đồng bộ; ca upgrade 1.000 dòng fixture kiểm checksum nguyên qua cả3phase và chuỗi băm trước/sau. Đây không phải chứng minh tải production. Cần PG thật (18 package code, gồm fixture composition mới, + migration test riêng), toàn bộ static/helper8gate+newupgrade trướcdev. Không đổi wire/OpenAPI/UI/schema tên vật lý/PO D2/MRV/tenant/M1–M7. Không đóng toànG1.


Cổng tĩnh đầu trên98de1e3a dừng ở vet (alias public khiến literal hai trường phải ghi tên trường). PG đầu được dừng đúng driver/child và cluster đã cleanup, db-full.exit=-1; không coi các PASS lẻ là hồi quy đủ. Sửa bằng AST chỉ thêm Field/Err hoặc Required/Normalize, giữ nguyên biểu thức/thứ tự ở5file (proofTEMP keyed.json). Script thử đầu lỗi đường dẫn trước khi viết mã, hai tệp tạm của nó đã chuyển về hostTEMP debug .txt; không còn trong repo. Pure-keyed73636.exit0, frozen12/60/170 vẫnkhớp; vet-keyed.exit0. Sắp chạy lại fullcode18+upgrade và static-final trên head cuối, không dùng proofPG bịdừng.


Lượt FINAL đầu (e6b50ace) không đạt: static-final lỗi ST1012 tên2biến lỗi riêng; generic HTTP test phát hiện2nhánh list/item vẫn gọi domain.EffectiveDated nên ?at bỏqua registry và trảv2 thayv1. Dừng đúng driver41135/child, cluster56892 cleanup, khôngtínhPASS. Đã nối2nhánh qua Service.CatalogEffectiveDated và bổsungassert listAt; đổi tên2biến errCatalog*. Chạy targeted PG date/RLS/sync +newupgrade trước full18 trênhead đãsửa. Giữ log cũ làm proofFAIL, khôngoverwrite.


Verified-code51844.exit0 đủ18/18packagecótest,0FAIL/SKIP trênproductionb0f4bb2f; KN77249.112s/composition2.159s, actualGo/testprocess0. Đã sửa CHỈ snapshot trong test nâng cấp sang tenant.WithTenantTx, giữ SQL/prodGo nguyên; không cần lặp18package vìtestmigrationskhôngnằmtrong18packageđãchạy. Chạy lại newupgrade riêng. Bước chuẩn bị script test từng bị auto-review chặn khiGođangchạy; đãchờkếtthúc vàxácnhậnprocess0 trước sửa trực tiếp, khôngbypass.


Kết quả cuối: code18/18 verified-code51844.exit0 trên productionb0f4bb2f, 0FAIL/SKIP. Chỉtest snapshot sauđó sửa scope (05681eca), SQL/prodGo khôngđổi: upgrade-repaired47221.exit0/PASS178.001s (test74.24s),1.000 dòng checksumnguyênqua3phase và1.001dòngchuỗibămsạchsauthêmtênmới. Static-repaired85467.exit0 fullgates/boundary371/lint0; test-followup vet/lint0; gofmtchỉ2fixtureold. Frozen12schema/60seed/170casegiữ. ProofTEMP agrigreen-codex-knsplit2mdcataloga-proof.json. Auto-review từng chặn prep/vet khi Gođangchạy; đã chờactualexit/process0 và thực hiện tuần tự, khôngbypass. Cầnhelper9migration (8mandatory+newupgrade) trướcdev. CatalogB/dependencies/tenant/client/KHTCreminder/G1 còn.
