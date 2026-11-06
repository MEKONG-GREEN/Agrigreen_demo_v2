## TEST-CNTS-KNQG-ALLOC — phép đo tài nguyên của các ô số

### Phạm vi kỹ thuật

Gói chỉ điều tra và sửa harness đo allocation của `cntsknqg/domain`. Ngữ nghĩa nghiệp vụ của các ô số giữ nguyên: dạng thập phân thường, giới hạn 22 ký tự, 18 chữ số phần nguyên và 4 số lẻ; không gọi decimal cho dữ liệu bị từ chối. Không đổi route, API, dữ liệu hay màn hình CNTS.

### Chẩn đoán trước khi chọn cách sửa

Helper cũ lấy chênh lệch `runtime.MemStats.TotalAlloc` của cả tiến trình quanh callback. Các lần Linux/race đã quan sát 5576 byte vượt 4096 byte trong khi syntax deny đúng; chưa quy allocation đó cho callback hoặc kết luận nguyên nhân.

Probe có build tag `allocationdiagnostics207`, tách khỏi các byte guard hiện hành. Chiến dịch có số mẫu cố định cho callback rỗng, cell number/int, Normalize và Warnings; lưu mọi mẫu rồi mới xuất báo cáo. Campaign profile riêng dùng `GODEBUG=memprofilerate=1` từ lúc khởi động. Việc flush và đọc allocation stack đều ngoài cửa sổ TotalAlloc; profile có cả setup/flush nên chỉ là bằng chứng forensic, không được lấy để trừ nhiễu hay thay budget khi chưa xác minh.

### Tiêu chí giữ nguyên

Giữ byte gate 4 KiB cho cell, 64 KiB cho Normalize và 16 KiB cho Warnings; không skip race, tăng budget, dùng min/retry-until-pass hoặc đổi byte thành số lần cấp phát. Thử cold/warm và mutant cấp phát 1 MiB trước syntax deny phải FAIL ở plain lẫn Linux/race, sau khôi phục phải PASS. Chưa có verdict tự review hoặc nghiệm thu từ commit probe.

### Chạy chẩn đoán

`go test -v -count=1 -tags allocationdiagnostics207 ./internal/kn/extension/cntsknqg/domain -run '^TestAllocationDiagnostics207$'`

`GODEBUG=memprofilerate=1 go test -v -count=1 -tags allocationdiagnostics207 ./internal/kn/extension/cntsknqg/domain -run '^TestAllocationAttribution207$'`

Chạy thêm từng lệnh với `-race` trên Linux/compiler sẵn có. Workflow `.github/workflows/cnts-allocation207.yml` thực hiện hai mode plain/race và hai cấu hình GOMAXPROCS default/2, giữ nguyên workflow CI chính.
