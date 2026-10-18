## KNSPLIT-2-HTX-READERS-B — Gắn bộ đọc riêng, gỡ import HTX trực tiếp

Nền dev945ec7b1. Deps/service của KN dùng CooperativeStatistics và PlotStatistics do KN sở hữu. Cổng xác nhận và hai hằng trạng thái trở thành khai báo KN giữ nguyên chuỗi wire. API assembly nối NewPublishedHTXData thay ba cổng native HTX. Extension.New vẫn giữ ý nghĩa tùy chọn nil của Deps cho worker/fixture không dùng dashboard; không tự đổi sources hay thực hiện truy vấn mới tại mọi chỗ tạo module. Không đổi native HTX/MKG, hợp đồng HTTP/menu/mobile hay DDL/quyền.

Kiểm giữ38native/111platform và33golden nền945. Reader dùng nguyên SQL/role/ACL đã đạt ở A; không thêm view hay quyền. Test HTTP đi qua reader thật như DI production: HTX đang dùng/không hồ sơ/ngừng, hộ active/đóng, thửa nháp/ngừng, quốc gia/tỉnh/xã/ngoài phạm vi/tenant trống và dữ liệu tenant khác. Đối chiếu native↔pub với k_ref node closure thật, realm-role và phiên KN (xã/tỉnh/admin, nút chưa có bản chiếu), không dựa nhánh tenant chưa dựng bản chiếu. Chưa chạy tại mốc viết mã.

Chỉ giảm baseline theo import đã gỡ bằng -prune, không sửa quy tắc/alias. Dự kiến kn-import-htx=0 sau bốn import bị bỏ; tổng phải giảm thực tế. Điều này chưa hoàn tất KN độc lập: reports còn đường sản lượng gián tiếp native, Units/Orgs còn cổng nền tảng, authz/accounts/party/routes/tenant/client/KHTC reminder còn. Giữ các quyết định PO về mỗi Trung tâm tỉnh/tenant, MRV HTX-MKG, D2 chỉ hồ sơ KN trong G1, mẫu nạp sẵn và M1–M7. Lịch tự tiếp tục còn hoạt động.


Lượt prune ban đầu đã giảm đúng318→314 do gỡ4import, nhưng từ chối5SQL foreign mới trong fixture HTTP đặt dưới internal/kn/extension/handler. Không thêm baseline hay ngoại lệ: chuyển test HTTP sang tests/htxdata, nơi dựng dữ liệu đối chứng/lắp ráp; gọi gốc module và port, giữ ranh giới mã KN. Reader và wiring không đổi. Test ngoài dùng dữ liệu HTX native đã khóa38kết quả, danh mục đơn vị thật và HTTP có token xác thực; không ghi bảng ngoại từ mã KN.
