## BF-SWEEP-HCTH-FORMS-REST — tham số FN19 trên trang riêng

Issue #197, Codex viết và Claude review. Lát hoàn chỉnh này thay RulePanel/RuleDialog của hai nhóm quy tắc nâng bậc và quy đổi thành tích. Sáu màn HCTH còn lại tách [#203](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/203); không xác nhận toàn FN19/HCTH hoặc UAT đã hoàn tất.

### Phạm vi và căn cứ

- BRD HCTH v3.0 FN19: quy tắc khai báo theo triển khai, Admin ghi phiên bản và HCTH chỉ xem. Giữ hợp đồng API đang có; không mở rộng nghiệp vụ Q070 hoặc coi cấu hình mẫu đã được xác nhận.
- Ngoại lệ làn đã điều phối: shell/menu.ts chuẩn hóa đúng alias HCTH catalog/scale/rule theo cấu trúc trang có thật sang full-path canonical, rồi dùng lại matcher/menu usable. Không mở phòng khác, mục tắt, quyền mới hoặc suffix lạ; không sửa menu JSON/tệp sinh.
- Liên kết danh mục mở route chi tiết/thêm phiên bản cho cả menu nhân sự và alias phòng ban. URL giữ ngày hiệu lực và phiên bản nguyên dương có giới hạn; URL sai không tải dữ liệu. Phiên bản không tồn tại không tự thay bằng phiên bản hiện hành.
- DetailPage, LabelValueBlock, bảng lịch sử có STT và Log dùng phiên bản, thời điểm ghi, payload và ghi chú thật. Hợp đồng hiện tại chưa trả actor: hiển thị thiếu thông tin, không suy từ người đang xem. Xem theo ngày hoặc phiên bản không thay đổi lịch sử; thêm mới sao chép payload và giữ bản cũ.
- NumberField/validateForm cho từng trường số, kể cả từng mốc nhắc và dòng kỷ luật/quy đổi. Giới hạn chuỗi 32 ký tự và cú pháp số thường trước chuẩn hóa/BigInt/Number. Biên, precision, thứ tự mốc nhắc, mã còn hoạt động và không trùng theo domain hiện hữu. Các tỷ lệ giữ chuỗi thập phân, chấp nhận nhập dấu phẩy Việt Nam; không làm tròn ngầm.
- Native Go duyệt từng giá trị ba tỷ lệ trước decimal.UnmarshalJSON, chặn cả tên trường case-fold và khóa trùng bị ghi đè có số mũ/chuỗi dài; giữ số JSON và chuỗi số thường, 0..100, tối đa hai chữ số lẻ. Tái sử dụng primitive giới hạn số của HRM; không đổi quyết định đã ban hành, migration hoặc baseline.
- Đổi phiên/tenant/vai/phạm vi/MFA/context hỗ trợ remount trang, xóa draft và kiểm quyền lại. Hủy chỉ điều hướng; phản hồi cũ về muộn không điều hướng phiên mới. useAction giữ khóa thao tác khi mất ACK, tạo đúng một phiên bản. Không sửa client multipart #183, shared form của Claude, 16 danh mục #163 hoặc bảng hệ số #179.

### Kiểm chứng

Regression domain và HTTP native kiểm mỗi tỷ lệ với 1e100000, 1e-100000, chuỗi 100.000 chữ số, NaN/Infinity, precision và biên; phản hồi lỗi có giới hạn, không thêm phiên bản. UI kiểm mọi ô số, ngày/URL, mã danh mục, hủy, quyền HCTH, đổi context và retry mất ACK. Browser dùng giờ giả lập, route riêng, phiên bản bất biến, Log và alias; DB thật chạy riêng qua pg-ephemeral. Kết quả thực thi các cổng, test và CI đúng SHA được ghi ở bình luận sẵn sàng, không suy PASS từ khai báo kiểm thử.

### Câu hỏi PO

Q070 và bộ cấu hình triển khai giữ nguyên; actor thiếu trong API được nêu rõ. Sáu màn theo #203, mẫu giấy/FormGrid và M1–M7 không bị thay đổi bởi lát này.
