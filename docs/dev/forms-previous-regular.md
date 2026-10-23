# FORMS-PREVIOUS-REGULAR (#123)

Kỳ định kỳ có loại kỳ rõ ràng bỏ qua ứng viên `adhoc` khi đọc PreviousSubmission, trước ORDER BY và LIMIT. Vẫn chọn lần nộp mới nhất của đơn vị trong kỳ trước gần nhất, cùng profile và mã biểu; không áp đặt cùng loại lịch giữa các kỳ định kỳ.

Kỳ hiện tại `adhoc` hoặc không có loại kỳ giữ hành vi cũ. Không đổi quyền người đọc, tenant/RLS, mẫu nạp sẵn, DTO, lưu trữ hay DDL. Đây là shared Previous endpoint, không thay sửa bộ đánh giá cảnh báo CNTS của owner #27.

Regression PostgreSQL gọi API app thật với một kỳ thường cũ đã nộp và một đợt đột xuất mới hơn đã nộp: day/week/ksgm_week/month/quarter/season/year đều phải chọn kỳ thường. Ca adhoc/legacy vẫn chọn đợt đột xuất; người ngoài đơn vị, tenant khác và profile khác không đọc được. Baseline tái hiện sai giá trị 99 thay 3.5 cho day/week; fixture ksgm_week ban đầu có dấu gạch dưới trong period key bị validation chặn, đã đổi key hợp lệ trước lượt cuối. Giữ log baseline lỗi, không đổi lịch sử thành PASS.

Chỉ ready khi kiểm PostgreSQL và các cổng static hoàn tất; cần Claude Review OK trước ticket.ps1 merge.
