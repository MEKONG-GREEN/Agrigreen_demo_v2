## KNSPLIT-2-HTX-READERS-A — Bộ đọc thống kê HTX của KN

Nền dev b0eeae11. Thêm bộ đọc riêng của KN và DTO/cổng hẹp cho số HTX/hộ theo xã, số thửa/diện tích, xác nhận hiện tại và tại thời điểm M6. SQL chỉ đọc view v1 trong pf_cooperative_pub và pf_party_pub; ghép theo tenant_id + ref, giao dịch read-only ở r_extension, không gọi module HTX, không bảng nội bộ, không ghi và không đổi tenant. Chưa thay wiring hay xóa đường cũ; bước B thực hiện sau khi A đạt.

HTX đang hoạt động không có hồ sơ vẫn được đếm, số hộ thiếu bằng 0; bỏ tổ chức không gắn xã/ngừng hoạt động. Thửa nháp được tính, thửa retired bỏ; số numeric14,4, nil bộ lọc = mọi xã, tập rỗng = không xã. M6 dùng claimed_at <= mốc truyền vào; không thay mốc bằng now. Lỗi nguồn trả lỗi, không giả số 0/nhãn chưa xác nhận. Không công bố tên hộ, hình học, actor.

Khai báo readpubview gồm cooperatives, plots, confirmations và org_tree. Quyền bootstrap hiện theo chủ công bố: thêm r_cooperative_pub và r_party_pub cho r_extension, do đó cho phép SELECT đúng 5 view (kể cả seasons cùng chủ). Tệp kn-htx-reader-access-additions.json ghi rõ 5 quyền đọc +2 membership; không thay các golden OLD/HTX/platform hay cờ vai. Cổng ACL phải đối chiếu đúng tổng26flags/58SELECT/23membership. Masterdata publication vẫn chưa cho extension đọc; các vai KN khác không nhận quyền này. Tất cả bảng nội bộ và thao tác ghi view tiếp tục bị từ chối.

Đối chiếu bộ đọc thực với cổng native trên PostgreSQL, ba tenant (hai tenant dùng trùng ref/xã và tenant trống), nil/rỗng/lọc/không biết/lặp xã, HTX thiếu hồ sơ, hộ đóng/ngừng, thửa nháp/ngừng, bí danh nhận quyền và ranh giới ngày VN. Giữ38native và111platform frozen. Thêm ca mất nguồn/thiếu tenant không fallback. Chưa chạy tại mốc viết mã; chỉ báo đạt theo exit thực tế.

Không đổi OpenAPI/menu/web/mobile, DDL/sổ/RLS/quyền nội bộ hay native HTX. D2 G1 chỉ hồ sơ KN, đề nghị sauG1; MRV HTX/MKG, tenant KN từng Trung tâm tỉnh, M1–M7 giữ. G1 còn đường gọi HTX, nguồn sản lượng gián tiếp qua reports, authz/accounts/party/routes, chuyển tenant, client và nhắc KHTC. Lịch tự tiếp tục giữ hoạt động.


Static ban đầu66162 exit1 đã phát hiện đúng R-02: test gốc lắp ráp import gói riêng. Sửa bằng NewPublishedHTXData ở gốc module, trả cổng KN HTXData; test dùng API này. Giữ Reader SQL và catalog runtime, không thêm ngoại lệ. Source ban đầu69177 exit0: HTX8PASS217.377s/platform4PASS154.445s, không SKIP; cụm54143/933dbcefa9c7 đã dọn. Full waiter43070 exit1 trước PG vì static1. Giữ log cũ; phải xác minh HTX qua API gốc và static-verified mới.
