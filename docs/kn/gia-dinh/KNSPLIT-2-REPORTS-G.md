## KNSPLIT-2-REPORTS-G — Registry source builder và owner KN

Nền devdefb45d8 đã bỏ catalog KN implicit. G thêm port.SourceBuilderRegistration/Query/Data (DTO decimal), registry immutable trong reports/app, callback lấy tenant/scope/At/bộ lọc/column keys từ job đã được cấp quyền và đóng băng. Scope/columns được copy trước callback; output row/decimal/meta/notes được copy để không thoát alias. Callback lỗi là kết quả cuối, không chạy legacy; registry explicit rỗng không suy builder KN. Nil registry còn adapter cũ trong G, H mới gỡ.

KN sở hữu ReportBuilders(ReportBuilderDeps) và thuật toán5nguồn: tổng gieo sạ/sâu bệnh/nhiệm vụ/tổng hợp, nhóm chiều theo cột đang hiện, distinct HTX/tỉnh và tỷ lệ Đề án. Dùng decimal, giữ null khi thiếu hoặc mục tiêu không dương; lookup tên đơn vị/danh mục/HTX giữ thời điểm Clock hiện hành như trước, query dữ liệu dùng RequestedAt. Không kéo reports/domain/app vào KN. Core giữ metadata chung và render/DLP/native CGH/HTX.

API/worker reports factory/KN77/tests-reports nối provider KN thật; worker background chỉ cần metadata/lịch đăng ký nilData builder fail503 nếu bị gọi. Service.Extension và thuật toán cũ vẫn giữ trong G để xóa ở H sau dev xanh. Không coi toàn bộ reports đã tách xong:14gridseed và các module registry khác còn.

Pure-first2490.exit0 app1.746s/extension1.376s. Pure-second51584.exit0 app1.105s/extension1.496s,0FAIL/SKIP: đủ5source,3.3ha/5báo cáo,30ha đăng ký/300mục tiêu=10%,2HTX, thiếu mục tiêu trảnull; nhóm theo giống, cảnh báo completeness. Core kiểm tenant/scope/job/period binding, alias2chiều, errorfinal và duplicate/invalidregistration. Cần13PGconsumer/cổng/helper trước dev. Không đổi SQL/API/UI/tenant/D2/MRV/M1–M7/GoogleSheet.
