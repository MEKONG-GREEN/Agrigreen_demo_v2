## ARCHIVE-KHTC — Xem bản xuất và tải ZIP Excel (#167, phần đầu)

- Xem trước chỉ đọc đúng `khtc_report_export.payload`, mẫu đã ghim và bộ lọc của export đã chọn. Không gọi Get, capture, bản soạn, source module hoặc mẫu live. Nội dung xem trước không có thao tác sửa/lưu.
- ZIP chọn 1–10 export khác nhau; tổng byte gốc tối đa 20 MiB. Mỗi export dùng cùng reader tenant/session, kiểm hash và egress checkpoint như tải Excel riêng. Tên trong ZIP dùng export UUID + version, không dùng đường dẫn người dùng nhập. ZIP có nguyên byte Excel đã lưu, không sinh lại nội dung.
- Không migration, không thay M1–M7, không mở quyền mới: giữ admin hoặc KHTC tỉnh/Quốc gia và RLS tenant như #34. Không mở tenant chéo (Q-027/Q-055), không coi phòng chuyên môn đã đủ quyền.
- Mẫu legacy không có compiled document thì xem lời văn và hàng số từ payload của export; mẫu đã ghim dùng cùng renderer với bản xuất và đúng bộ lọc snapshot.
- Phần còn lại #190: Word đã lưu gắn đúng snapshot và ZIP hỗn hợp; lọc kỳ/lĩnh vực; quyền phòng/lãnh đạo theo dữ liệu của phòng; phạm vi TW theo quyết định PO. #167 phần đầu chưa hoàn thành toàn bộ FN-TW-01.01/FN-PN-03.03.
- BRD nguồn v2.3 theo issue #167 và hồ sơ #34; bản trích `analysis/src/brd_taichinh.txt` còn v2.2 nên chỉ đối chiếu các yêu cầu không đổi: xem trước bản đã xuất, file tải trùng bản gốc, không bản soạn, tải nhiều file ZIP, audit.
