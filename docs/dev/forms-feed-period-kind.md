# FORMS-FEED-PERIOD-KIND (#124)

SubmissionQuery.ExcludePeriodKinds là bộ lọc tùy chọn, so khớp chính xác loại kỳ trong SQL trước ORDER/LIMIT của SubmissionFeed.Latest. Nil/rỗng giữ hành vi cũ; phần tử rỗng khớp kỳ legacy không có period kind. Tất cả giá trị là tham số SQL, không nội suy chuỗi; không hard-code consumer KN trong core.

Không đổi tenant/RLS, lọc profile/biểu/đơn vị/xã cha/khoảng ngày, last version mỗi kỳ/đơn vị và limit mặc định. Không đổi DTO HTTP, DDL, golden, guard hoặc quyền phiên. Owner #27 cần tự nối ExcludePeriodKinds với loại kỳ cần bỏ ở previous/spike/các consumer thích hợp; thêm cổng không tự sửa evaluator owner. #123 chỉ sửa endpoint Previous riêng.

Regression PG thật tạo năm adhoc mới hơn regular trong cùng cửa sổ, Limit=1 vẫn trả regular và version mới nhất. Kiểm nil/empty/literal giữ cũ; tenant/profile/date/parent giữ phạm vi; bỏ đồng thời nhiều loại và loại rỗng có semantics rõ ràng.

Chưa ready tới khi test PG và static đạt; cần Claude Review OK trước ticket.ps1 merge dev.
