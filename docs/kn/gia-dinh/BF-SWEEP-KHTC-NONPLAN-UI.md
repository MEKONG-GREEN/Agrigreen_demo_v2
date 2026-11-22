## BF-SWEEP-KHTC-NONPLAN-UI — lát hộp phiếu chỉ đọc (#226)

### Phạm vi và nguồn

BRD KHTC v2.3 (SHA-256 `15859351f90574bf667190b1a12160465ba03f8ca9e408b94c90d285a3502e25`), FN-PN-03.01/03.02: hộp phiếu của phòng, đọc trạng thái kỳ, hạn nộp, lần gửi và yêu cầu bổ sung. Lát này bổ sung Detail và History metadata thật từ GET phiếu hiện hữu; giữ nguyên đường dẫn Nhập/Tiếp tục/Sửa/Xem và màn nhập có sẵn. Không thay mẫu phiếu/lưới, không thêm mutation, không thay nghiệp vụ M1–M7.

### Dữ liệu và ranh giới

History chỉ có các lần gửi, thời điểm, kết quả/nội dung rà soát từ API. API chưa có người thực hiện, nội dung riêng từng lần gửi hay log lưu nháp đầy đủ; không tạo actor hoặc lịch sử ở client. Các phần editor, per-cell validation, lịch sử nội dung/audit, report editor và kỳ nhúng còn lại thuộc child #229; không coi #226 đã sweep toàn bộ KHTC.

Các route mới nằm dưới tiền tố danh sách có quyền hiện hữu; không mở rộng menu/role. API reference thực và principal/tenant/actor/MFA/ngữ cảnh/phòng cùng route gắn lifetime riêng. Chuyển scope xóa header/content cũ; phản hồi chậm từ lifetime đã bỏ không ghi vào lifetime mới, kể cả A→B→A. Backend vẫn kiểm quyền mỗi GET; nhãn chỉ đọc không cấp quyền. Không tái sử dụng Plan workspace của #206, không stack nhánh chưa gộp, không gọi mutation kỳ #211 hoặc SQL Forms riêng.

### Kiểm và giới hạn nghiệm thu

Không có ô số mới, migration hoặc thay đổi hợp đồng/API sinh. Có kiểm route/query/UUID, dữ liệu metadata, readonly/zero-write, API/authority replacement, deferred response/ABA và phép làm sai có kiểm soát bỏ lifetime key. Kết quả chạy cụ thể ghi ở issue review theo đúng SHA; tài liệu này không thay bằng chứng kiểm thử. Không tuyên bố production/UAT/G1/FN đầy đủ, live revocation registry hay lịch sử nghiệp vụ chưa có endpoint.
