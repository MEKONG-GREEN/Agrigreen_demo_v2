## BF-SWEEP-KHTC-REST — lát Kỳ báo cáo (#180)

Nguồn: BRD KHTC **v2.3 ngày 08/10/2026**, DOCX SHA-256 `15859351f90574bf667190b1a12160465ba03f8ca9e408b94c90d285a3502e25`, các đoạn P836–P879 (FN-TI-02.03). Đã đọc bản gốc từ [Product Drive](https://docs.google.com/document/d/1PwFVpnfY60et4OAXn1CGG_KLXhkfCI35/edit), không dùng bản trích v2.2 làm chuẩn. UI theo [UI-KIT](../ui-kit.md) và PRD R-A1–A5. Prototype chỉ là đặc tả.

### Phần hoàn chỉnh của lát UI

- Danh sách Kỳ báo cáo dùng DataTable tự đánh STT; giữ bộ lọc năm và trạng thái, liên kết Xem/Sửa theo quyền/trạng thái.
- Các route `/kn/khtc/ky-bao-cao/them-moi`, `/ky/[periodId]`, `/ky/[periodId]/sua-han`: trang riêng với DetailPage, liên kết quay lại/Hủy/Lưu và Log. Link tiến độ cũ `/kn/khtc/tien-do-ky/[periodId]` chuyển tới route chi tiết mới.
- Detail dùng bảng tiến độ DataTable, giữ 4 thẻ đếm và KhtcSourcesPanel hiện có. Log lấy `opened_at/opened_by` và các sự kiện API, gồm thời điểm, ref người thực hiện, hành động/hạn mới/lý do; không tự tạo sự kiện từ giao diện và không lấy PII từ UUID.
- Layout giữ nháp mở kỳ trong bộ nhớ khi chuyển giữa danh sách, tạo mới, chi tiết, sửa hạn trong cùng workspace. Không ghi nháp vào localStorage/sessionStorage; thay đổi context/vai/tenant thì khởi tạo lại workspace. Hủy hoặc mở thành công xóa nháp; nhấn liên kết Quay lại hay browser Back giữ nháp. Rời hẳn workspace/reload không phải lưu nháp bền vững.
- Kiểm form trước preview/ghi bằng validateForm, trường bắt buộc có `*`/aria-required, lỗi dưới đúng ô và lỗi server theo trường. Ngày/giờ kiểm cú pháp/độ dài/lịch trước chuyển ISO; sử dụng giờ Việt Nam. Lát này không thêm ô số tự do: năm/tuần/tháng/quý là lựa chọn từ danh sách; số lần gửi tối đa chỉ đọc. FormGrid và các mẫu giấy giữ nguyên.
- Theo v2.3 P849/P868, route mới không còn ô/nhãn Hạn xuất. API backend hiện hành vẫn giữ trường đó để tương thích; thao tác sửa hạn mới chỉ gửi `due_at`/lý do, không đổi `export_due_at`.
- Giữ quyền ghi hiện hành: Cấp Tỉnh Phòng KHTC có quyền soạn; read_only, TW/phòng khác không mở/sửa qua URL trực tiếp. Đóng/hủy vẫn theo các capability máy chủ `can_close/can_cancel`; kỳ đóng/hủy không có form sửa hạn. KN không ghi dữ liệu HTX.

### Phần còn lại, không coi đã nghiệm thu toàn chức năng

[#194 BF-SWEEP-KHTC-FINAL](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/194) giữ PlanScreen, inbox/forms/reports và các phần UI kỳ phát sinh nhúng còn lại. KhtcSourcesPanel vẫn giữ cấu trúc sẵn có, chưa sweep toàn bộ các bảng lịch sử/mốc nguồn bên trong.

Phân biệt với nghiệm thu đầy đủ FN-TI-02.03 v2.3: backend vẫn giữ lịch/hợp đồng W11.62 (Mẫu 5 đang chọn Đến ngày, hạn xuất nội bộ). Chưa sửa sang Mẫu 5 theo tuần/lịch hạn mới P848–849/P868–869; chưa có Sao chép kỳ từng dòng P862/P873/P879, điều kiện Bản cuối v2.3 mới, nhắc phòng/Soạn báo cáo mới. Các thiếu hụt này phải được hoàn thiện/kiểm riêng, không suy ra đạt từ việc route/UI đã chuẩn hóa. Nhắc phòng/dashboard thuộc #189 khi phù hợp. Không đổi M1–M7 hoặc quy tắc nghiệp vụ bằng giả định UI; không coi mock là UAT thật.

### Kiểm chứng

`periods/routes.test.tsx`: URL trực tiếp, DataTable/STT, giữ/xóa nháp đúng context, Hủy, chặn read_only/phòng khác/TW, mở kỳ qua preview, sửa hạn/Log, kỳ đã hủy, các chuỗi ngày giờ độc hại `1e100000`, `1e-100000`, 100000 chữ số và ngày không tồn tại. RED với component dev gốc thiếu bảng/route; mutation bỏ key theo context phải FAIL, nguồn gốc được khôi phục và control PASS. Test đồng hồ giả lập; không gắn với ngày chạy.

Bằng chứng Go/static/lint/web/DB thật/E2E/CI đúng SHA được ghi ở bình luận ready; chỉ gửi Claude review sau khi các cổng đạt, không tự gộp.
