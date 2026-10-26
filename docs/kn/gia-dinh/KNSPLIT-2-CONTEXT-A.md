## KNSPLIT-2-CONTEXT-A — Cố định hợp đồng chiếu ngữ cảnh trước khi tách KN

Nền 5d5cc1d3 đã kiểm hồi quy trên Go1.26.9. Gói này ghi lại đầu ra native ContextVarsFrom/RequiresNode thành golden mới và kiểm các giá trị ấy trên PostgreSQL thật; không đổi production Go/SQL, role/ACL, API hay quyết định PO. Không dùng fixture này để giữ các đường ghi HTX của KN: việc loại đường ghi và tách tenant vẫn bắt buộc ở các gói sau.

TEST-CTX-001: ma trận 14 tổ hợp realm roles × có/không tokenOrg × không chọn/các vai KN × có/không selectedNode. Ngữ cảnh Admin không fallback tokenOrg; KNXa vẫn cần node dù có realmAdmin; không bổ sung kn_officer cho kn_user. Thêm ca job/purpose, nil/empty/escaped communes, support/onbehalf/actor, tenant token rỗng và view-only. Hợp đồng cố định sáu biến và quyết định RequiresNode trước khi thêm generic projection, không sao chép thuật toán policy vào expected.

TEST-CTX-002: đối chiếu toàn ma trận với set_config LOCAL qua ForModule/WithContextTx trên pool một kết nối, hai chế độ exec và statement cache nối trực tiếp. Tenant trusted khác Principal.TenantID; đủ allow/deny với 403 node_scope_required và cause, callback không chạy khi thiếu node. Sau commit/rollback/denied BEGIN mọi biến và role trả rỗng/login. Không claim đã chạy PgBouncer nếu không có server đó.

TEST-CTX-003: savepoint xóa đủ biến khi job không Principal; rollback khôi phục outer, commit giữ LOCAL mới đến outer commit. Giữ ActorRef token riêng OnBehalfOf; không giả Principal trong production worker. Native W24/FD và published HTX real-RLS consumers chạy trước dev. Golden cũ34 và exact ACL26flags59SELECT24membership không thay đổi; boundary314.

Kiểm trong ủy quyền G1: privatePG/fake dữ liệu/khôngSMTP, Go1.26.9 riêng process; source test mới và shared/db W24/FD/roles + tests/htxdata, staticfull, helper migration11 trước dev. Không cần full5/web/mobile vì không đổi mã nghiệp vụ hay wire. Gói kế mới thêm generic session/projection contract/owner registration rồi thay đường cũ với golden này; chưa coi phần tách context/tenant/routes đạt. Main/worktrees/lịch giữ.


Source3 thực tế5404exit0, không FAIL/SKIP: context4test197.598s (268vectors×hai chế độ×commit/rollback =1072giao dịch,948accepted/124denied; không rò pool, savepoint và identity đạt), native shared/db W24/FD/context/functions/grants/T4 selective130.091s, HTX14-132.530s gồm 288 đối chiếu real-node RLS và432 đối chiếu production; không chạy lại platformdata trong gói test-only này. Private52812/dacd3fbe1bf5 đã dọn/Go0 trước sửa lint. Core capture7f624ee9 giữ nguyên268fixture, sửa hai dòng test tại d842f3f5: promote Begin và fmt.Errorf %w. Không sửa production hay expected; static đầu exit1 giữ log, static-verified19729exit0/lint0/boundary314/đúng2fixture gofmt cũ.

Lượt PG kiểm lại đầu dừng trước Go test vì PowerShell flatten array một phần tử, không phải lỗi contract. Giữ log/exit2 và dọn private58265/3bb1d89d94ed; script final dùng lệnh đơn trực tiếp. Context PG final24018 đang chạy; chưa kết luận đã đạt trên hai dòng test cuối hay helper/dev. Những suite shared/db và HTX đã đạt vẫn áp dụng vì source native và caller của chúng không thay đổi.34oldgoldens nền5d5 và frozen268 tại7f624 giữ theo Git diff. Main không đổi, chưa helperqueued.


Context PG final24018 actualexit0/noFAIL-SKIP trên d842f3f5: cả4testPASS124.309s, 268vectors/1072transactions và savepoint/identity đều đạt, private64436/49817b171d5e đã dọn/Go0. Static-verified0; sharedDB/HTX source3 đã0 trên production nguyên. Source/golden bắt đầu7f624 giữ; chỉ teststyle hai dòng đã kiểm lại. Chờ helper11migration+contextdata trước khi claimdev; main/worktrees/lịch giữ. Genericprojection/registry và chuyển owner KN chưa thực hiện ở gói capture này.
