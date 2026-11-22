## BF-FORMS-PERIOD-OP-RECEIPT — receipt nguyên tử cho bốn CAS native

Issue #212, Codex viết, Claude review chéo. Năng lực supplier `PeriodOperations` bổ sung cho `ConditionalPeriodControl`; không thay hợp đồng CAS hiện hữu và chưa nối caller KN/HRM/CNTS.

### Phạm vi và quyền

Bốn thao tác `change_due`, `close`, `reopen`, `cancel` dùng cùng kiểm quyền, khóa kỳ và giao dịch native hiện hữu. Owner phải đăng ký `UsePeriodOperationPolicy` trước phục vụ; mặc định nil trả 503 trước ghi. Policy và resolve actor chạy ngoài TX, đọc receipt cũng tái kiểm MFA/quyền hiện tại. Binding do owner xác minh từ server; request không có authority hoặc digest tự khai. Fixture policy không phải chứng cứ đã rollout auth production. Không có route HTTP hoặc wiring owner mới trong gói này.

### Bằng chứng và retry

Operation UUID + canonical toàn request (kể cả expected revision và lý do) được băm tại supplier; identity lưu tenant/profile/kỳ/actor/authority hash. Lookup sau kiểm quyền nhưng trước trạng thái/revision/no-op của successor. Applied giữ đúng revision và outcome/snapshot metadata của chính giao dịch gốc; replay không lấy head mới. CAS/status rejection đã chứng minh trước native effects có receipt rejected bền. SQL error hoặc COMMIT không biết ACK không được biến thành rejected/applied; caller nhận unknown và đọc lại đúng operation với quyền hiện tại.

Anchor identity chỉ INSERT, không hết hạn. Receipt là ledger append-only có retention theo partition; không có ngày purge riêng trong gói. Anchor còn nhưng receipt thiếu/purge/không xác minh được trả unknown và chặn tái thực thi; anchor không tự chứng minh applied. Đổi actor/profile/body/authority không lộ receipt metadata. Quyền thu hồi trả 403 trước lookup. Không cam kết quyền bị thu hồi giữa một lời gọi đã resolve sẽ được truy vấn registry lại trong TX.

### Giới hạn và phần còn lại

[BF-FORMS-PUBLISH-OP-RECEIPT #224](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/224) xử lý publish/creation receipt: `PublishedPeriod` chưa có revision creation đã commit, không suy head+1 hoặc giả receipt. Consumer/reconciliation còn lại ở #211; gói này không tự làm caller #201/#209 tích hợp, không tuyên bố toàn CAS, production/UAT/G1 hoàn tất.

Migration forward-only `20261103205309_pf_forms_period_operation_receipt.up.sql`; không sửa lịch sử, baseline hoặc privilege ngoại lệ. Kiểm thử native phải dùng PostgreSQL tạm, gồm lost ACK thực commit/rollback, replay sau successor, ABA, hai actor cùng key, missing receipt, policy pool1 và lỗi insert receipt rollback native. Race local Windows chưa có CGO compiler; CI Go race là cổng riêng bắt buộc.
