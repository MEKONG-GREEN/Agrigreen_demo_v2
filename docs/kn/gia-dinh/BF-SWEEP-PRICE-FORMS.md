## BF-SWEEP-PRICE-FORMS — Lát Xã báo giá (#182)

Nguồn: BRD Báo giá v1.3 ngày07/10/2026 (analysis/src/brd_baogia.txt, FN-AD-04.01/02), UI-KIT, ma trận bugfix và ma trận Báo giá v1.3. Nhánh bắt đầu từ dev878a40f0 sau #183 đã đóng/vào dev; không xếp chồng hoặc sửa các client multipart của #183.

### Đã triển khai trong lát này

- Xã báo giá và nhóm vùng: danh sách DataTable/STT không pager giả; ngày/nhóm vùng/tab nằm trong URL và đi theo link/quay lại.
- Route strict thêm/sửa/chi tiết/phân công; thao tác ghi trên DetailPage, Hủy/Quay lại rõ ràng. Dialog chỉ xác nhận ngưng và lý do/ngày hiệu lực ngắn.
- validateForm và lỗi từng ô, dấu bắt buộc, kiểm ĐVHC còn hiệu lực/chưa gắn xã khác và nhóm vùng hoạt động trước gửi. Máy chủ giữ quyền kiểm cuối cùng và ngày hiệu lực kế tiếp.
- can_manage lấy từ status hiện hành, direct URL của người chỉ xem không hiện form ghi. Key theo tenant/actor/context/route/bộ lọc; request đang chờ của phiên cũ không điều hướng hay mang form sang phiên mới. Tải ĐVHC/danh bạ được gắn với đúng ngày/vai, không dùng kết quả cũ để gửi.
- Giữ useAction/IdempotentOperation và API có sẵn; mất response sau commit rồi Lưu lại cùng dữ liệu giữ key, không nhân đôi xã. Không đổi schema/API/role/PII, không tạo migration, không đổi mẫu giấy/M1–M7/FormGrid. Không có ô nhập số mới; các số đếm/phiên bản chỉ đọc.
- Log xã và phân công từ versions/officers thật: event_time/actor_ref/thay đổi trước-sau/lý do, không dùng đồng hồ máy hoặc tên người giả. Actor chỉ có ref trong hợp đồng thì hiển thị ref; tên cán bộ theo quyền API hiện có.

### Giới hạn và phần tiếp theo

#198 BF-SWEEP-PRICE-REST giữ items/tickets/reports/surveys và hợp đồng Log nhóm vùng. PriceRegion chỉ có version/valid_from, chưa có history/actor/event_time; màn nhóm vùng nói rõ chưa có nhật ký, không dựng log giả. Nhập Excel xã là gap nghiệp vụ có trước, chưa được thực hiện chỉ nhờ refactor giao diện. Các AC nghiệp vụ/API có sẵn không được coi là đã UAT lại toàn bộ bằng browser mock. Không chốt thêm PO hoặc thay quyền để đạt UI.

Bằng chứng kỹ thuật local/PG/mutation/CI đúng SHA được ghi ở issue182 khi đầy đủ; HTTP/PostgreSQL thật và browser API giả lập được phân biệt. Chỉ ready Reviewer Claude sau cổng và CI đúng SHA xanh, không tự gộp.
