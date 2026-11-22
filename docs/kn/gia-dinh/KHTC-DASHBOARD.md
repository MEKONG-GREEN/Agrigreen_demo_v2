## KHTC-DASHBOARD — Trang chủ KHTC chỉ đọc (phần đầu #166)

Đặc tả: BRD KHTC v2.3 FN-TI-02.01, prototype KhuyenNong_KHTC.html. Phần tiếp theo ở #189 W11.69-DASHBOARD-FULL; gói này không hoàn tất toàn bộ FN.

- API `GET /v1/extension/khtc/dashboard` và trang `/kn/khtc/trang-chu`: năm/quý mặc định theo giờ Việt Nam, năm 2000–2100, quý 1–4; lọc phòng và lĩnh vực.
- 5 thẻ: phiên bản báo cáo đã xuất trong năm; kỳ đang mở trong quý; phiếu đã gửi/tổng phiếu trong các kỳ đang mở; hạng mục định lượng đạt ít nhất 100%; hạng mục chậm dưới 30% sau 01/10 năm kế hoạch.
- Tiến độ đọc lại chính `capture`/`Compose` Mẫu 5 cho kỳ Mẫu 5 có ngày chốt gần nhất không vượt cuối quý hoặc hôm nay. Trả rõ `progress_period` và ngày chốt. Không có kỳ phù hợp → không tạo kỳ/bản soạn, hiện chưa có dữ liệu. Không dùng snapshot bản soạn đã cũ.
- Trung bình chương trình tính từ tỷ lệ của các hạng mục định lượng có số liệu, không cộng đơn vị khác nhau; chỉ làm tròn kết quả cuối đến hai chữ số thập phân. Dòng thiếu nguồn/phiếu không được coi là số 0 hay đạt KH. Hiện số dòng chưa đủ dữ liệu.
- Thẻ xuất và bảng quý/năm theo năm/phòng; tiến độ chương trình và phiếu/nguồn theo quý; lĩnh vực chỉ lọc chương trình. Các semantics lọc toàn dashboard cần hoàn thiện ở #202.
- Phiếu/nguồn đọc live từ dịch vụ kỳ mỗi request; không cache trong dashboard. Đọc lỗi không giả thành đã chốt/đã gửi. Sau #189: quét toàn năm bằng cursor/batch100, không cắt ngầm ở100 kỳ; nguồn chia lô. Xem KHTC-DASHBOARD-PAGING.md.
- Chỉ context KHTC Cấp Tỉnh/Cấp Quốc gia và Admin. Lãnh đạo/BGĐ dùng context KHTC chỉ xem có sẵn; không đổi context hay giả quyền từ header. Quyền trực tiếp từ phòng khác thuộc #202. Không nhận tenant từ query, dùng RLS/tenant phiên.
- Màn hình chỉ đọc; liên kết mở kỳ dùng màn hiện có, không tự ghi khi tải dashboard. Nút mở kỳ ẩn với lãnh đạo/BGĐ/chỉ xem. Chức năng nhắc và badge động thuộc #202.
- Mock dùng cùng kho phiếu/nguồn/capture báo cáo; bộ dữ liệu giả lập cũ không có cây chương trình đầy đủ nên mỗi dòng minh họa được trình bày như một chương trình. Không dùng mock làm bằng chứng phép tính cây thật; kiểm Go/DB bảo vệ phần đó.

Không migration mới, không ghi HTX, không tăng baseline. Các file menu/mock/OpenAPI sinh lại từ registry. Không lưu hay thay đổi bản xuất khi đọc dashboard.
