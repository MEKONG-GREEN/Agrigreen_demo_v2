# Guard server nền TT-10 / TT-11

Gói #113 BF-SRV-GUARD; PRD kn-bugfix-brd-alignment ngày 23/10/2026.

- Dòng khai báo máy phải có số lượng nguyên trong khoảng 1–1.000.000. Handler JSON và use case đều kiểm; số 0 không còn là khai báo hợp lệ. Để bỏ một loại, bỏ dòng đó khỏi mảng thay thế; sự kiện removed vẫn giữ Quantity=0 và số cũ để dựng lịch sử. Không sửa dữ liệu lịch sử cũ.
- Chủ hộ khi tạo thửa hoặc khi gửi thay đổi chủ hộ phải có tên không rỗng. Trả 422 validation_failed, trường farmer_name, thông báo tiếng Việt trước khi gọi kho định danh. Nhãn trả về vẫn được che.
- Diện tích trả về lấy từ geometry; declared_area_ha là số khai báo phụ đã có, không thay diện tích hình. Guard chồng lấn KN online và cách xác nhận/xung đột riêng của API HTX giữ nguyên.
- Không đổi schema, route, tenant, node, vai hay quyền; không triển khai luồng KN ghi dữ liệu HTX.

## Phần chưa đạt của toàn bộ TT-10 / TT-11

Năm máy trên màn khai báo loại × số lượng và cây trồng thửa chưa có hợp đồng dữ liệu KN tương ứng. Hồ sơ máy CGH từng chiếc đã kiểm YearMade khi có, nhưng không thể coi đó là kiểm cho khai báo KN. KNAttrs hiện chưa chứa cây trồng. Không nhận thêm trường rồi bỏ đi để giả đạt tiêu chí.

Ticket owner KN [#119](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/119) cung cấp hợp đồng và nơi lưu hồ sơ KN riêng, phối hợp web #103/#104. #113 còn chờ phần này; guard nền không đủ để đóng toàn gói hoặc nghiệm thu P1. Mọi gộp phải có Claude Review OK và chạy ticket.ps1 merge.
