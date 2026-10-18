## KNSPLIT-2-REPORTS-M — Chuyển consumer hợp đồng dữ liệu KN

Nền5cb2d5c8. KNport thêm alias tạm13DTO ReportQuery/Completeness/5cặpRow+Data/ReportData và5SourceID đến định nghĩa cap cũ;9consumerfiles(KNmodule/app/store/builder/catalog/handler test +tests-reportsfake) đọcquaKNport. AliasKN→capport, không tạo platform→KN. Typeidentity/stringvalues/signatures thực giữ nguyên; không đổi thuật toán, SQL, JSON hoặcRLS.

Đây chỉ bước chuẩn bị; định nghĩa13DTO/5ID vẫn ởcap đếnN. GenericConsolidation/PeriodInfo/DispatchHistory là cổng cap còn dùng, không chuyển bừa thành nghiệp vụ KN. Script bảo vệ banđầu dừng ở import đơn catalog sau6file, không chạyGo; đã hoàn tất3filecòn lại, giữdiffđúng9callers, khôngresetworktree.

Dự kiến14packagecótest choPG:base13reports+KNhandler fullsuite bảo vệ producerSQL/TestProgramProgressFR03/TestReportDataSowingAndSummary;KNapp/store không cótest nhưng phảicompile. Không thêmtest chỉ phản chiếualias; dùngscoping/figures/HTTP/worker/exportvàgolden thực đã có. Cầnstatic/helper8migrationgate trướcdev. KhôngđổiAPI/UI/tenant/D2/MRV/M1–M7/GoogleSheet.


Kết quả thực: PG48903.exit0 đủ14/14packagecótest,0FAIL/SKIP trênproduction21d2518f: handler:124.852s, assembly:102.724s, worker:2.211s, formimport:1.994s, reportforms:2.116s, reports:94.348s, kn77:253.871s. KNhandlerfull124.852s bảo vệSQLproducer/FR03/scopes;KNapp/storecompilekhôngtest. Static38201.exit0 đủbuild/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary383/lint0issues. AliasMgiữtypeidentity/5ID/goldenbyte, chưa chuyểndefinitions. ProofTEMP agrigreen-codex-knsplit2reportsm-proof.json; helpercòn8migrationgate trướcdev, N mới gỡcap13DTO/5ID.
