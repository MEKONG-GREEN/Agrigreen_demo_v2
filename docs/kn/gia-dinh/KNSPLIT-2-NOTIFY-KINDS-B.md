## KNSPLIT-2-NOTIFY-KINDS-B — Ràng buộc lưu trữ thông báo

Nền aff0f755, sau khi A đạt pure48851/static57288/full9PG42658/helper77313 (9 migration PASS, dev xác minh). A đã chụp danh sách thực tế 43 loại cũ và 7 giá trị bị từ chối từ CHECK PostgreSQL; giữ nguyên cả 16 golden, bao gồm hợp đồng này. Danh sách cũ chỉ là ảnh chụp trước chuyển đổi, không phải danh mục SQL mới.

Ba migration mới thuộc duy nhất pf_notify: 20261017220437 thêm notification_kind_key_shape NOT VALID; 20261017220439 xác thực; 20261017220441 chỉ bỏ notification_kind_check sau khi xác thực. Mỗi tệp có lock_timeout 5s. Không sửa migration cũ, DML, cột, tenant, chỉ mục, RLS hoặc quyền. Pattern ASCII a-z/underscore, bắt đầu bằng chữ, dài 1–40 khớp registry và là tập con hợp đồng email_delivery hiện hành. Nền tảng không còn cần sửa DDL theo danh sách loại của KN; registry đã khóa ở A vẫn từ chối mọi loại chưa được chủ nghiệp vụ khai báo trước SQL.

TestNotificationKindConstraintUpgrade dựng ảnh chụp tổng hợp 1.000 dòng từ đủ 43 loại cũ, chạy ba giao dịch nâng cấp, đối chiếu count và md5 của toàn bộ JSON mỗi dòng ở từng bước. Ca này kiểm tra trạng thái NOT VALID/VALIDATE, từ chối bỏ CHECK trước xác thực, giữ CHECK cũ đến bước cuối; sau đó kiểm các biên khóa hợp lệ và CHECK từ chối khóa sai dạng. Đây là dữ liệu tổng hợp để diễn tập, không phải tuyên bố quy mô dữ liệu sản xuất.

TestRegisteredFutureKindEmailAndUnknownAtomicRetry phát hai draft: draft đầu hợp lệ tạo inbox/email/outbox, draft sau chưa đăng ký làm rollback toàn sự kiện; sửa khai báo của draft trong fixture rồi giao lại cùng event phải lưu đúng hai thông báo/email/outbox và chống trùng. Loại mới đã đăng ký đi qua hộp thư, email giả gửi đúng một lần, provenance giữ và tenant khác không thấy. Hợp đồng 43 loại cũ vẫn được kiểm thực bằng test A còn lại. Chỉ bỏ generator capture cũ vì CHECK hữu hạn không còn là cấu trúc hiện hành.

Rollback ứng dụng giữ ba migration tiến; không khôi phục CHECK cũ vì có thể đã có loại mới đã đăng ký. Không rollback dữ liệu hay sửa tenant. Nếu cần quay mã cũ, phải dừng producer loại mới trước; quy trình vận hành này không cho phép xóa dòng hoặc hạ schema.

Impact: DDL pf_notify và hai nhóm test, không đổi code production Go/API/UI/mobile. Bắt buộc pure5, static đầy đủ/boundary318 không tăng, full9PG không FAIL/SKIP và helper có 10 migration PASS (giữ 9 cũ + NotificationKindConstraintUpgrade), rồi xác minh remote dev. Chưa kiểm thử thì không đánh dấu tích hợp. G1 còn native KN policies, dữ liệu/rights/routes/tenant/client/KHTC; main, worktrees và lịch tự tiếp tục giữ.

Pure61588actual0/5pkg.16goldenGitblobs/baseline318/rules/oldmigrations/runtimeGo nguyên aff0f755. Trước static, khóa SQL collation C để ASCII không phụ thuộc locale; bổ sung ca newline/Unicode trong test upgrade. Các pure package không đổi, chưa chạy PG/helper10/dev.


Kết quả thật: pure61588actual0/5pkg, static87585actual0/toàn cổng/boundary318/lint0/gofmt2old. Full9PG24163actualexit0/không FAIL-SKIP: assembly152.348s/testsnotify156.797s. FutureKindEmailAndUnknownAtomicRetry PASS0.14s, 43 loại cũ vẫn lưu được. Private58456/6f98d006511b đã dọn,Go0; không sửa Go/SQL sauPG.16goldens/rules/baseline/runtimeGo/oldmigrations nguyên aff0f755. Còn kiểm thử nâng cấp 1.000 dòng và 9 cổng migration trong helper10 trước remote dev.
