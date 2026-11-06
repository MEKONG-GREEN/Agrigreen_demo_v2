## PN-PROGRESS-SNAPSHOT-215 — Tiến độ phòng từ bản Mẫu 5 đã chuẩn bị

Lát triển khai đầu tiên của **KHTC-PN-PROGRESS-ARCHIVE**, đối chiếu BRD KHTC v2.3 FN-PN-03.03. Chưa hoàn tất FN, UAT người dùng hoặc G1. Phần còn lại được tách tại [#247](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/247).

### Nguồn và quyền

Phòng chuyên môn đọc **snapshot Mẫu 5 đã được KHTC lưu**, không phải nguồn live tại thời điểm mở màn hình. Mốc dữ liệu, ID và phiên bản lấy từ bản lưu thật; bộ lọc và Excel dùng cùng `snapshot_id`. Không có bản đã chuẩn bị thì trả `not_prepared`, bảng rỗng và không có Excel. GET không tự tạo/cập nhật bản soạn, không phát số và không sửa dữ liệu nguồn.

Session hiện hành phải là Tỉnh đúng phòng hoặc Admin chọn một trong bốn phòng chuyên môn. Kỳ chương trình phải cùng tenant và có giao phòng đó. Xã, TW, KHTC hoặc phòng khác không được đọc qua đường này. Lọc ở server trước khi trả DTO; chỉ lá hạng mục của phòng và nhãn tổ tiên cần thiết được trả, không có tổng liên phòng, nội dung phiếu, tên người, thuyết minh, nguồn chi tiết hay bản Mẫu 5 nguyên khối. Không suy quyền từ lĩnh vực; không mở reader báo cáo/kho hiện có.

Chỉ đọc bảng của `khtcreport` trong module transaction; không gọi Forms, Plans, source port hoặc FilePort. Excel dựng sau khi đóng transaction và chỉ chứa projection đã lọc; đây là bảng tiến độ phái sinh, không phải bản xuất chính thức đã cấp số. Ghi nhận egress theo quy ước hiện hữu với actor/clock/trace thật. Không có migration hoặc baseline mới; không ghi dữ liệu HTX.

### Giao diện và hợp đồng

- `/kn/bao-cao-khtc/tien-do` và alias Admin `/kn/phong-ban/[dept]/bao-cao-khtc/tien-do`. Menu tiến độ được bật qua fragment và sinh lại; menu kho phòng vẫn Sắp có.
- Ba GET dưới `/v1/extension/khtc/departments/{department}/program-progress`: `periods?year=`, `/{periodID}` và `/{periodID}/excel`. Lựa chọn kỳ phân trang 50, lọc năm 2000–2100; lọc chương trình/mức đạt giữ ID bản đang xem. Người dùng bấm đọc bản mới để lấy version mới.
- `DetailPage`, `SelectField`, `DataTable` có STT/phân trang; số là chuỗi thập phân của Mẫu 5, không đổi qua float. Thiếu nguồn/phiếu hiển thị phần trăm “—”. Đổi principal, tenant, API client, ngữ cảnh, phòng, năm, kỳ hoặc bộ lọc hủy lifetime dữ liệu cũ; Excel trả muộn không được tải xuống.
- Reader giới hạn JSON theo ngân sách 32 MiB đã có, tối đa 10.000 dòng; chuỗi số tối đa 96 ký tự, kiểm cú pháp trước xử lý, không phân tích decimal của plan cũ. Dữ liệu snapshot sai/hỏng trả lỗi phụ thuộc, không fallback snapshot khác.

### Kiểm chứng và giới hạn

Native HTTP dùng PostgreSQL thật: chủ KHTC chuẩn bị Mẫu 5 từ phiếu đã gửi, số PN bằng Mẫu 5 cùng snapshot; Excel đọc lại được; chặn phòng/role/tenant khác; chưa chuẩn bị không sinh bản soạn; snapshot cố định còn đúng sau khi chủ lưu version mới. Domain kiểm nhãn tổ tiên, không lộ số ngoài phòng, `1e100000`, `1e-100000`, chuỗi 100.000 chữ số và công thức trong ba cột số. Web kiểm quyền, phản hồi sai resource, paging, context đổi và phản hồi/tệp muộn. Bằng chứng cổng, CI đúng SHA và phép làm sai có kiểm soát nằm trong bình luận bàn giao của issue; không suy PASS từ `ok` hoặc SKIP.

#247 tiếp tục tiến độ live/đồng nhất nguồn chưa chuẩn bị, kho báo cáo phòng, quyền snapshot/leadership và các trường hợp biên đầy đủ của FN-PN-03.03. Sửa nguồn PN dùng chung chỉ sau actual merge #215; kho/policy sau actual merge #227 và phần quyền #246. Q027/Q055 và quyền cấp trung tâm chưa chốt vẫn cần PO quyết định. Không dùng metadata để tự mở quyền.
