## BF-SWEEP-HCTH-FORMS — FN19 bảng hệ số trên trang riêng

Issue #179, Codex viết và Claude review. Lát hoàn chỉnh này chỉ bao gồm bảng hệ số theo ngạch FN19; phần tham số và các màn HCTH còn lại được bàn giao thành issue #197 sau khi lát này vào dev. Không coi toàn bộ HCTH đã hoàn tất.

### Phạm vi và căn cứ

- BRD HCTH v3.0 FN19: Admin quản lý, HCTH chỉ xem; bảng hiệu lực không thay đổi quyết định đã ban hành. Dữ liệu mẫu Q070 vẫn cần HCTH xác nhận.
- Danh sách có STT và ngày hiệu lực; liên kết chi tiết/thêm phiên bản trên route riêng cho cả menu nhân sự và menu phòng ban. Giữ ngày trong URL và giữ prefix phòng ban.
- DetailPage, LabelValueBlock, LogPanel lấy phiên bản/thời điểm từ API. API chưa trả người thực hiện: hiển thị thiếu thông tin, không suy từ vai đang đăng nhập. Không thêm dữ liệu audit giả.
- NumberField và validateForm: tháng nguyên 1..120; 1..20 hệ số dương <=99,99, tối đa hai chữ số thập phân và tăng dần. Chặn chuỗi quá 32 ký tự và cú pháp số mũ trước chuẩn hóa/BigInt. Chỉ đổi tháng sang Number sau kiểm giới hạn.
- Đổi context remount editor, status fail closed, Hủy chỉ điều hướng. API/server/domain hiện có tiếp tục giữ quyền, lịch sử và idempotency. Không đổi client multipart #183, 16 danh mục #163, RuleDialog, shared form của Claude, API Go hay migration.

### Kiểm chứng

- Regression kiểm mỗi ô trong 20 bậc và ô tháng với 1e100000, 1e-100000, chuỗi 100.000 chữ số, số rất lớn; kiểm biên domain, lịch thật, thứ tự hiệu lực và không sửa mảng lịch sử.
- Component regression: danh sách STT, URL menu phòng ban, Log thật, lưu phiên bản mới giữ phiên bản cũ; context/quyền; dữ liệu sai không gọi API.
- Browser E2E có giờ giả lập, tạo phiên bản trên route, lỗi số tại ô, quay về ngày cũ vẫn thấy hệ số cũ và lịch sử mới. Mock browser chỉ chứng minh UI; DB thật chạy riêng qua pg-ephemeral.
- Cổng Go/web, DB/migration, phép làm sai có kiểm soát và CI đúng SHA được ghi trong bình luận sẵn sàng sau khi thực thi; không suy PASS từ việc khai báo test.

### Câu hỏi PO

Không thay quyết định nghiệp vụ. Q070 giữ nguyên; thiếu actor trong hợp đồng hiện hữu được nêu rõ. Phạm vi còn lại theo #197.
