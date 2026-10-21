# Phạm vi biểu do chủ ứng dụng cấp

ResolvedActor.AllowsTemplate(code) là callback máy chủ, dùng cùng resolver đã xác thực cho từng request. Nil giữ profile không giới hạn như cũ; chủ ứng dụng phải dùng closure trả false để biểu diễn danh sách quyền rỗng rõ ràng. Shared không giải mã payload grant KN hoặc cộng vai từ phiên khác.

Filter áp dụng cho danh mục/phiên bản biểu, phát hành lẻ/lô, kỳ, công việc, lịch sử/nộp/rà soát, điều khiển kỳ, figures, snapshot và sync. Callback không mở quyền role/node/tenant/đơn vị hiện có. Quyền template không thay thế quyền target; target filter vẫn phải được áp dụng cả với scope toàn bộ hoặc node chủ.

Offline ID chỉ được trả cached data sau khi kiểm profile, biểu và đơn vị hiện hành, đồng thời ID đã lưu phải cùng period/unit/submitter với request. Kiểm write-state giữ sau replay, nên retry hợp lệ trên kỳ đã đóng vẫn nhận đúng bản đã lưu. Thu hồi quyền không trả lại nội dung cũ qua replay. Held submission dùng cùng điều kiện.

Snapshot là byte bất biến có hash: không lọc JSON rồi giữ hash cũ. Với actor có filter tường minh, chỉ trả whole native snapshot khi mọi đơn vị trong snapshot đều được phép. Mirror không có metadata template/target có thể chứng minh, nên actor có filter tường minh không đọc được mirror qua cổng generic; luồng nil cũ giữ nguyên. Không thay bytes snapshot/golden hoặc actor lịch sử.

Các port job dùng tenant tường minh (Feed/SystemProgress) vẫn là cổng máy chủ tin cậy; không dựng Principal. Sync feed không tự phát sự kiện khi grant thay đổi: owner #71 phải có hợp đồng refresh/resync để successor thấy task và thiết bị bỏ cache quyền đã thu hồi. #74 chỉ cung cấp enforcement hiện hành, chưa hoàn thành FN-14/handoff.

Gói không đổi HTTP/schema/DDL/ACL. Lỗi dùng mã hiện có. KN adapter/closure thuộc #71, cần cross-review trước tích hợp qua ticket.


Review phạm vi target: Close/Cancel/ChangeDue phải được phép trên mọi đơn vị bị tác động; Reopen chỉ kiểm tập đơn vị chọn, rỗng nghĩa là toàn kỳ. Response có callback target tường minh lọc đồng bộ Period.Units, Spec.UnitParents, unit views, sự kiện mở lại và Figures.Parents. Sự kiện mở lại chỉ tác động target ẩn bị bỏ khỏi phép chiếu trả về, tránh biến danh sách rỗng thành “mở toàn kỳ”; sổ và snapshot nguyên trạng. Nil callback giữ hành vi legacy. Kiểm thử PostgreSQL bao gồm actor scope All và actor cùng node chủ, deny không tạo sự kiện, reopen subset được phép và các metadata không chứa mã target bị cấm.
