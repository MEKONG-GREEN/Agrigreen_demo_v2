## KHTC-DASHBOARD-LIVE-POLICY — cửa sổ Mẫu 5 trực tiếp (#202)

Mã issue gốc: `W11.69-DASHBOARD-LIVE-POLICY`.

Phạm vi đầu tiên của #202 là tiến độ Mẫu 5 theo năm, quý và ngày yêu cầu, kể cả chưa mở kỳ Mẫu 5. Không mở kỳ, lưu bản soạn, cập nhật ảnh chụp hoặc cấp quyền phòng chuyên môn khi đọc. Phần quyền lãnh đạo, badge, nhắc phòng và các quyết định trường dữ liệu khác thuộc #230.

Nguồn nghiệp vụ: BRD KHTC v2.3 ngày 08/10/2026, SHA-256 `15859351F90574BF667190B1A12160465BA03F8CA9E408B94C90D285A3502E25`, FN-TI-02.01 (P752–765), BR-DL-01/02/05/06/13 (P300/302/308/310/324). Chỉ phiếu đã gửi và nguồn đã chốt được đưa vào mức đạt/bình quân. Bản nháp và lần gửi mới nhất đã trả bổ sung không được thay thế bằng lần gửi cũ.

### Ngày yêu cầu và ngày đọc thực tế

- `through_date` là ngày ISO chính tắc trong năm/quý đã chọn; kiểm độ dài và ASCII trước phân tích ngày. Không truyền thì mặc định ngày cuối quý hoặc ngày hiện tại Việt Nam, lấy ngày sớm hơn.
- Không có quy tắc BRD cấm chọn ngày tương lai. Ngày yêu cầu tương lai vẫn được nhận; `effective_cutoff` là min(cuối ngày yêu cầu UTC+7, thời điểm đọc thực tế). Không dự báo số hoặc kế hoạch tương lai.
- `quantity_to` là Thứ Ba cuối cùng không sau ngày hiệu lực thực tế; `from_date` là đầu tuần 1 của năm theo lịch KHTC v2, phiên bản lịch 2, Thứ Tư–Thứ Ba. Lũy kế tính các tuần của năm kế hoạch, kể cả tuần 1 bắt đầu cuối năm trước.
- `Plans.PlanAt(year, effectiveDate)` dùng hợp đồng BR-DL-13: phiên bản có ngày hiệu lực gần nhất không sau ngày chốt thực tế. `PriceAt` là ngày đó; `ConfirmationAt` không vượt thời điểm đọc thực tế, ngày quá khứ được đọc hết ngày UTC+7.
- Nếu quý chưa bắt đầu hoặc chưa có tuần đủ kết thúc của năm, `progress_window=null`; không gọi nguồn cho cửa sổ ngược. GET vẫn 200 với các thẻ/trạng thái hiện hữu. Thiếu dữ liệu không được giả thành số đạt 0 đã biết.
- `as_of` luôn là thời điểm đọc; không phải ngày người dùng chọn. `progress_period` chỉ được điền khi có kỳ Mẫu 5 thật; khi không có kỳ thì null, không tạo ID hoặc liên kết báo cáo giả.
- Cổng nguồn công bố trả số đã chốt hiện được công bố cho khoảng tuần, không cam kết khôi phục ảnh chụp sự kiện chốt tại một thời điểm lịch sử tùy ý. `effective_cutoff` chọn kế hoạch/giá/xác nhận và khoảng số liệu; `as_of` nói rõ lúc đọc nguồn hiện hữu. Không mô tả cửa sổ này là bản báo cáo lịch sử đã đóng băng.

### Tính toán và ranh giới

Cửa sổ đọc là kiểu tính toán riêng, không phải `ps.Period`. Core số Mẫu 5 được dùng chung với Compose; adapter bản báo cáo thật giữ nguyên đầu vào/đầu ra/băm. Bridge tổng hợp chỉ đặt năm tính toán trong envelope nội bộ để gọi hàm tổng hợp cũ, không tuần hoàn envelope đó qua port, DB hay JSON báo cáo.

Phiếu được đọc qua cổng native và xác minh bảng niêm phong, phòng/kỳ, hash nội dung; chỉ các kỳ tuần thật chưa hủy đủ ngày được chọn trước giới hạn truy vấn. Nguồn đọc qua `KHTCSources`, tái dùng `EvaluateAt` và số tuần đã đóng; I/O port ở ngoài transaction. Không gọi Refresh và không tạo snapshot trên GET. Quyền đọc hiện tại giữ nguyên KHTC/Admin; không mở quyền PN hay archive.

Đọc metadata/trạng thái KHTC hiện hữu của Trung ương vẫn trả 200 với ngữ cảnh thực. Profile phiếu native hiện không cấp vai này đọc bài nộp, nên lát đầu chưa tính số trực tiếp cho Trung ương: cửa sổ có thật mang `unavailable_reason=native_forms_access_unavailable`, không đọc kế hoạch/phiếu/nguồn mới, không suy ra thiếu kế hoạch hay đạt 0. Giao diện giữ metadata/kỳ thật và hiện thông báo chưa khả dụng; phần mở rộng quyền/dữ liệu lãnh đạo thuộc #230. Nếu quý tương lai thực sự chưa có tuần đủ kết thúc, cửa sổ vẫn null với thông báo chưa có tuần cho mọi vai, không dựng cửa sổ giả để mang lý do.

Khóa nguồn sai dạng còn tồn tại trong Kế hoạch năm đã được chấp nhận trước đây giữ ngữ nghĩa thiếu dữ liệu như đường snapshot cũ: parser không nới; hạng mục vẫn có mặt và được đánh dấu thiếu, không có số module giả. Không gọi nguồn ngoài khi không có khóa hợp lệ cần đọc; lỗi phụ thuộc hoặc dữ liệu toàn vẹn của nguồn hợp lệ vẫn được trả lỗi.

Giao diện tạo tài nguyên đọc mới khi đổi route, bộ lọc hoặc quyền hiện hành (actor, tenant, assignment/context, role/phạm vi, MFA, trạng thái phiên, mục tiêu hỗ trợ, API object). Dữ liệu và danh mục cũ bị gỡ ngay; kết quả muộn thuộc tài nguyên đã hủy không được đưa vào lần A mới sau A→B→A. Lý do hỗ trợ chỉ là văn bản audit, không làm đổi quyền hay tạo hàng loạt GET. Quyết định cho phép đọc vẫn ở API; không suy ra quyền mới từ tên phòng hay dữ liệu cũ.

### Kiểm chứng bắt buộc

Golden cố định cho Compose cũ bảo vệ JSON byte/hash; test live so cùng dòng Mẫu 5 thật; native no-period RED→GREEN, sealed/sent vs draft/returned, canceled/future/cross-tenant/cutoff, nguồn công bố/chốt thật, hơn 10.000 metadata không liên quan, và counters no-write. Các cổng web/Go/native, controlled negative, E2E giờ giả lập và CI đúng SHA phải xanh trước gửi Claude review. Tài liệu này không tự xác nhận các cổng chưa chạy.
