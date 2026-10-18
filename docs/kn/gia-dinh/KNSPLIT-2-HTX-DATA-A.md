## KNSPLIT-2-HTX-DATA-A — Bốn hợp đồng dữ liệu HTX chỉ đọc

Gói thêm bốn view có phiên bản do pf/cooperative sở hữu trong schema mới pf_cooperative_pub. Schema nội bộ pf_coop, dữ liệu nguồn, các port hiện hành và cách nối KN giữ nguyên. Quyền đọc mới chỉ dành cho hai vai phân tích r_metrics/r_agg theo chính sách công bố sẵn có. KN chưa nhận quyền mới trong bước này.

- cooperatives_v1: cờ có hồ sơ và số hộ đang hoạt động có tư cách thành viên còn mở. Tên, trạng thái hoạt động và đơn vị hành chính của tổ chức phải lấy từ sản phẩm org_tree của nền tảng. HTX hoạt động chưa có hồ sơ vẫn được bên đọc tính với số hộ bằng 0.
- plots_v1: mã thửa, HTX, xã, trạng thái và diện tích; không công bố tên, hình học hoặc chủ hộ. Bên đọc giữ quy tắc đếm cả thửa nháp, bỏ thửa ngừng dùng và cộng diện tích numeric(14,4).
- seasons_v1: giữ độc lập dữ kiện season_current và yield_summary theo (tenant_id, season_ref). FULL JOIN và hai cờ present bảo toàn trường hợp thiếu một phía; các trường production_* giữ nguyên chiều và trạng thái của bảng sản lượng. Không hòa dữ kiện khác nhau giữa hai bảng chiếu.
- confirmations_v1: bí danh đã nhận quyền cùng thời điểm claimed_at. Không có dòng nghĩa là chưa xác nhận. Bên đọc lịch sử phải so claimed_at với cuối ngày chốt theo giờ Việt Nam; không thay bằng trạng thái hôm nay.

Migration 20261018030844 chỉ tạo schema/view, thu hồi quyền PUBLIC và cấp SELECT cho r_cooperative_pub. Vai công bố NOLOGIN/NOBYPASSRLS; view chạy bằng chủ migration không vượt RLS, các bảng gốc FORCE RLS. Không DML, đổi cột/sổ, sửa RLS hay cấp quyền bảng nội bộ. Ứng dụng cũ vẫn dùng đường cũ. Nếu cần gỡ công bố, dùng migration tiến mới sau khi gỡ bên đọc.

Schema công bố dùng tên chuẩn <cụm>_<module>_pub để modgen tự xác định chủ sở hữu; không thêm alias. Native HTTP routes và các port cũ không đổi mã. Hai sản phẩm dữ liệu nền tảng và bước chuyển KN sang đọc view thuộc gói kế tiếp.

Đã chụp 38 kết quả native trên PostgreSQL thật trước thay đổi: phiên 41966, exit 0, 117.421 giây. Hai tenant dùng cùng mã tham chiếu và tenant thứ ba không có dữ liệu. Tệp tests/golden/htx-read-native.json được khóa từ kết quả này; mã kiểm thử không còn cờ ghi lại.

Mốc đối chiếu: xã 89001 có 2 HTX/3 hộ và 3 thửa/6.5679 ha; xã 92001 có 1 HTX/0 hộ và 1 thửa/0.0001 ha. Tổng sản lượng gồm 3 vụ, 1 hộ phân biệt, 6.2345 ha, 5 ha đã có khai báo, dự kiến 3703.500 kg và thực tế 1000.125 kg. Hai bí danh 101/102 của cùng neo đã nhận quyền trước ngày chốt cùng được xác nhận; 103 nhận quyền ngày sau nên chưa xác nhận tại ngày chốt.

Ảnh chụp quyền cũ tests/golden/pub-reader-access.json giữ nguyên 26 vai, 41 cặp SELECT và 15 quan hệ thành viên. Tệp htx-published-access-additions.json khai riêng đúng 8 cặp SELECT và 2 quan hệ thành viên bổ sung cho r_metrics/r_agg. Cổng đối chiếu đòi tập OLD cộng đúng phần bổ sung đã rà soát; không cho đổi cờ vai.

Lượt PostgreSQL xác minh 30117 đạt cả 5 ca, exit 0, không SKIP/FAIL (111.362 giây): 38 kết quả cũ giữ nguyên; view khớp nguồn native; hai bảng chiếu thiếu nhau hoặc khác dữ kiện vẫn độc lập; quyền không được cấp bị từ chối; áp DDL trên 1.000 dòng thửa cũ giữ nguyên số dòng/checksum của 8 bảng nguồn. Cụm tạm 52534/93fd447d19ea đã dọn. Lượt đầu 80174 đỏ do tham số mảng UUID trong test; đã sửa riêng test sang []string và text[]::uuid[], giữ log lỗi và giữ nguyên DDL/assertion/golden.

Kiểm tra thuần 78301 đã đạt: 7 gói thuần cùng các unit DBadmin được chọn, không SKIP. Chưa hoàn tất static, hồi quy các bên tiêu thụ, helper và tích hợp dev ở mốc ghi này. M1–M7, tenant theo Trung tâm tỉnh, MRV HTX/MKG và phạm vi D2 đã chốt giữ nguyên.
