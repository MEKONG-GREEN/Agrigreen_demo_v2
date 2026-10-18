## KNSPLIT-2-PLATFORM-DATA-A — Hợp đồng đơn vị hành chính và cây tổ chức

Nền dev 4e16ec6f. Gói thêm hai view chỉ đọc, chưa đổi KN wiring: pf_masterdata_pub.v1_admin_units do pf/masterdata sở hữu và pf_party_pub.v1_org_tree do pf/party sở hữu. Giữ schema/bảng nguồn, lịch sử hiệu lực, trạng thái và các port hiện hành. Chỉ hai vai phân tích hiện hành nhận quyền công bố; KN chưa được cấp quyền mới.

Capture native 36163 đạt trên PostgreSQL thật: exit 0, 111 kết quả, không SKIP, 135.622 giây. Dữ liệu gồm hai tenant dùng cùng code/ref và tenant trống; 9 phiên bản đơn vị, hai ranh giới đổi ngày theo giờ Việt Nam, mã tách/kế thừa/đổi tên, Get/Current/Descendants/Children; tổ chức lọc loại/đơn vị/trạng thái và tra mã theo lô. Golden platform-read-native.json được khóa từ kết quả thật, mã kiểm thử bỏ cờ ghi lại. Cụm tạm 52444/125e8c38b24d đã dọn. Lượt đầu 82420 lỗi fixture nạp con trước cha (23503), đã sửa riêng thứ tự seed, giữ log.

Hai migration mới, mỗi tệp một schema công bố, SET LOCAL lock_timeout='5s', không DML/đổi RLS/cột/sổ hay cấp quyền bảng nội bộ. View giữ toàn bộ phiên bản đơn vị, cận valid_to loại trừ; ngày truy vấn thuộc giờ Việt Nam. Cây tổ chức cung cấp cạnh parent_ref và trạng thái hiện hành; không công bố representative_ref, actor hoặc ghi chú. Tên tổ chức là tên đơn vị, không tên cá nhân. Dùng pubview và hợp đồng JSON nội bộ; không đăng ký Public API G5.

Giữ OLD ACL và tệp bổ sung HTX bất biến. Tệp bổ sung nền tảng phải khai đúng 4 SELECT và 4 membership cho r_metrics/r_agg; cổng đối chiếu đòi tập tổng chính xác. Dự kiến tổng 9 chủ công bố, 22 view, 11 hợp đồng JSON; không giảm cổng kiểm hay sửa alias. G1 còn KN readers/HTX imports/policy/accounts/party/routes/tenant/client/KHTC; lịch tự tiếp tục giữ hoạt động.


PostgreSQL xác minh 73356 đã đạt 4 ca, exit 0, không SKIP/FAIL, 121.013s. Giữ 111 kết quả native; đối chiếu ngày hiệu lực theo giờ Việt Nam và tổ chức; kiểm từ chối quyền; nâng cấp 1.000 dòng trên mỗi bảng nguồn giữ số dòng/checksum và đọc đủ 1009 đơn vị/1007 tổ chức. Cụm65343/a256d8ec42b5 đã dọn.

Lượt 84940 đỏ được giữ riêng: test converter cần chuẩn hóa UTC đúng native wire; fixture nâng cấp phải để DB cấp sync_seq thay vì chép số cũ. Chỉ sửa test, giữ DDL/golden/assertion. Chưa pure/static/fullconsumer/helper/dev.
