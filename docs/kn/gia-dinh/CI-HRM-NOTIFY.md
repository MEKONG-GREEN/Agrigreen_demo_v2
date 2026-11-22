## CI-HRM-NOTIFY — Kiểm hợp đồng thông báo HRM sau #19

### Nguồn và nguyên nhân

Gói #186 khôi phục kiểm hợp đồng lắp ráp sau #19 trên base `033f356c542e0db152df4e36523a3ef9fc9f1293`.
[CI 41189437477, attempt 1](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/actions/runs/41189437477/job/124503031451)
thất bại ở `TestAssemblyAddsHRMConsumerKindsBuilders_NotifyHCTH` khi chạy `go test -race`:
đăng ký HRM có thêm `pf.hrm.appraisal.changed`, nhưng assertion vẫn chỉ kỳ vọng hai sự kiện cũ.
Đăng ký production và test riêng của HRM đã chủ ý có ba sự kiện; lỗi được đối chiếu là assertion lỗi thời,
không phải bằng chứng phát hiện data race.

### Hợp đồng được kiểm

- Giữ nguyên prefix 16 consumer cũ, consumer HRM thứ tự 17 và consumer KNQG sau HRM.
- Kiểm đúng ba sự kiện HRM theo thứ tự: promotion due, plan reminder, appraisal changed.
- Qua dependency lắp ráp, yêu cầu đúng ba builder singleton theo cùng thứ tự; mỗi builder có Build khác nil và Plan nil.
- Giữ kiểm loại thông báo không trùng và danh mục sự kiện hàng đợi khớp consumer HRM.

### Phạm vi và kiểm chứng

Chỉ đổi một test tại `tests/notifyconsumers/hrm_test.go` cùng hai tài liệu riêng của #186.
Không đổi production, SQL, migration, API, menu, golden, dependency hay baseline; menu kế thừa v11 của base.
Không thêm hành vi nghiệp vụ hay giả định PO. Test không chứng nhận việc gửi thông báo toàn hệ thống.

Bằng chứng bàn giao phải giữ lượt baseline RED, focused GREEN, affected Go `-race` trên PostgreSQL tạm,
mutation thiếu appraisal registration bị test từ chối và lượt phục hồi byte chính xác thành GREEN;
cùng các cổng AUTO-REVIEW §4 và toàn bộ Vitest của đúng worktree.
Danh sách affected tests là một chuỗi CSV:
`./tests/notifyconsumers,./internal/kn/hrm,./internal/platform/notify/...,./cmd/api-platform/assembly/...`.

Gói do Codex viết, Claude review và gộp qua `ticket.ps1 merge`. Sau gộp cần kiểm CI của actual dev mới;
kết quả local hoặc CI cũ của #185 không thay bằng chứng CI của đầu dev mới.
