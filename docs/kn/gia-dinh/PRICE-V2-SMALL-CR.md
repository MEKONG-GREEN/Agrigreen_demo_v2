## PRICE-V2-SMALL-CR — Báo giá v2.0 (#283)

Nguồn duy nhất: Function chi tiết FN-TI-02.06 (luồng phụ, BR-02/04) và FN-XA-03.04 (BR-01/02/03, AC-01/02) trong BRD Báo giá v2.0. Theo Function chi tiết, cả lần nhắc 07:30 và 10:30 đều loại xã đã gửi; câu mô tả chung “mọi xã” không thay thế ngoại lệ Đã gửi trong AC-01.

### Thứ tự thực hiện

1. Dependency/cấu trúc: giữ dependency; sửa dịch vụ bảng giá, chọn người nhận, UI và mock tương ứng; thêm regression native/UI.
2. Data: dùng sổ phiếu/dòng giá/cảnh báo hiện hữu và `DayChangedSince`; không cần migration. Giữ chuỗi băm và phiên bản đã duyệt.
3. Service: `Can.CancelReopen` và lệnh hủy dùng cùng kiểm tra thay đổi kể từ lần mở duyệt gần nhất trong giao dịch. `PointReminders.Targets` kiểm Submitted cho cả hai lần, theo tenant/ngày, trước khi ghi outbox.
4. API: giữ DTO/OpenAPI; `can.cancel_reopen` là quyết định server. Giữ idempotency và hai kênh chuông/email của sự kiện nhắc.
5. UI: Tỉnh có quyền thấy Hủy mở duyệt disabled khi đã sửa, kèm yêu cầu duyệt lại; phiên chỉ xem và Admin vận hành chính thức không có thao tác. Mock dùng phiên bản nguồn, không suy thay đổi từ đồng hồ trình duyệt.

### Kiểm chứng

- Native `TestPrice283CancelReopenChanges`: hiệu chỉnh, sửa ngày đi/tới, xóa, nhập thay, mở lại cảnh báo; DTO và POST đều chặn hủy, bản công bố và băm sổ không đổi sau yêu cầu bị chặn.
- Native `TestPrice283CancelUnchangedPermissionsAndReplay`: hủy chưa sửa, quyền chỉ xem/tenant và replay cùng khóa.
- Native `TestPrice283Reminder0730SubmittedExcluded`: đồng hồ giả UTC+07 (Asia/Bangkok và múi giờ nghiệp vụ hiện hành), 07:29:59/07:30:20/10:30:15; Bản nháp/Chưa nhập vẫn nhận, Đã gửi không vào outbox; không lẫn ngày/tenant, không trùng khi job lặp. Fixture cấu hình 07:30/10:30 theo AC; không đổi mặc định cấu hình hiện hữu.
- Vitest `board-cancel.test.tsx`: nút disabled/không POST sau sửa; hủy thành công khi chưa sửa; phân quyền; đang gửi không bấm trùng. Mock/browser kiểm tương tác; native PG kiểm dịch vụ/sổ.
- Không thêm ô số, menu, module, kết nối HTX hay thao tác website thuộc #268; không thêm PII hoặc I/O ngoài trong giao dịch.

### Cách chạy

Tại gốc repo, dùng Go toolchain trong `go.mod`: `scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/price/... ./tests/notify ./tests/notifyconsumers ./tests/kn77 ./migrations`. PG tạm riêng phải PASS, không SKIP. Trong `apps/web-ops`: `npm ci`, `npm run check:api`, `npm run lint`, `npx tsc --noEmit`, `npm test`, `npm run build:mock`.

Gói chuẩn bị cục bộ; CI đúng SHA, tự review và gộp chờ các gói phụ thuộc/quản trị thực sự vào dev và điều phối nhả cổng. Kết quả mock không thay UAT khách hàng.
