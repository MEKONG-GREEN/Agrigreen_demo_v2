## KNSPLIT-2-PUB-READERS-A — Module tự khai hợp đồng dữ liệu đọc

Nền9df8d64d. PostgreSQL OLD capture99143actualexit0/noskip146.790s: 26 role flags, 41 cặp quyền SELECT view hiệu lực và 15 quan hệ thành viên _pub. Private51402/0e063b7e6574 đã dọn trước thay đổi. tests/golden/pub-reader-access.json chỉ lấy ảnh chụp OLD; test đọc lại phải khớp, không có cờ sinh lại golden. RLS/giới hạn tenant kiểm bằng các cổng hiện có, ảnh chụp quyền không thay thế kiểm dữ liệu theo hàng.

Thêm consumes.publications và chỉ thị readpubview do module đọc sở hữu. Registry kiểm đúng nhà công bố, phiên bản, schema sở hữu, cùng cụm, không khai trùng và có vai DB. Dữ liệu qua view không thêm depends vào đồ thị gọi. FormatCatalog sinh PubReadOwners đã gộp nhà công bố/sắp xếp; getter sao chép slice, thiếu khai báo không sinh quyền đọc. Chỉ ghi field YAML/catalog khi có để tránh sửa metadata module không liên quan.

KN extension tự khai bốn view v1 đang đọc thực trong closedfig: extension.closed_figures, forms.closed_figures, price.board_daily, price.ticket_week; suy ra đúng ba chủ cũ extension/forms/price. A giữ nguyên DBadmin/bootstrap PubReaders cũ; đây là bước thêm adapter. B mới chuyển quyền bootstrap sang metadata và xóa hardcode, sau bằng chứng tương đương. Quyền hiện tại là vai schema _pub, không phải quyền SQL chi tiết đến từng view; khai hợp đồng phiên bản giúp kiểm nguồn và lập hồ sơ, không tuyên bố thu hẹp quyền DB hơn hiện trạng.

Unit kiểm thiếu khai báo, sai nguồn/phiên bản/schema/cụm, duplicate, self read, clone và sinh YAML/catalog. Test ngoài module đối chiếu metadata mới với cấu hình bootstrap cũ. Hồi quy PostgreSQL gồm metrics/handler, tests/kn77, APIassembly, worker; helper11 gồm 10 cổng cũ + actual ACL contract. Không thay DDL, SQL đọc nghiệp vụ, OpenAPI/UI/mobile, quyền, alias hoặc quy tắc baseline318. Giữ các golden cũ và M1–M7/D2/tenant perCenter/MRV HTX-MKG. G1 còn native policies/dữ liệu/rights/routes/tenant/client/KHTC; lịch/main/worktrees giữ.

Khi gói sau bổ sung data product hợp lệ, phải giữ ảnh chụp OLD và ghi rõ tập quyền mới cần thêm; không ghi đè golden OLD hoặc bỏ kiểm quyền.


Pure2292exit0 nhưng có1SKIP TestBootstrapClustersOnlySelected_G4 do thiếuPGenv; chỉ7purepackages đủPASS,nativeDBadminunitPASS nhưng CHƯAfullDBadmin. ToànDBadmin chuyểnthêmvào full5PG đểrequiredca chạy thật,nofail/skip; khôngsửamã/assertion. 27existinggoldenGitblobs/baseline318/rules/SQLmigrations/DBadminbootstrap giữ9df8d64d. Chỉextension.module.yaml thêmconsumes.publications; dependsnguyên. Productionc10fed41;OLD26/41/15capture99143actual0. Chưa static/full5PG/helper11/dev.


Phát hiện và sửa lỗi nền khi kiểm DBadmin thật: baseline 9df8d64d trên PostgreSQL mới (48780exit1) cũng lỗi SQLSTATE42704 own_acctbridge khi chỉ dựng TS. RolesFor trước đây lọc danh sách vai nhưng giữ MemberOf của các cụm chưa dựng. Nay chỉ bỏ parent do Roles quản lý mà không thuộc tập vai đã chọn; giữ parent PostgreSQL/bên ngoài, thứ tự và clone MemberOf. Không tạo vai cụm khác, không thu hồi quyền của lượt bootstrap trước. Unit kiểm closure PF/MKG/TS/Vault, toàn cụm DeepEqual, clone không sửa global, giữ external parent. Legacy PubReaders và chính sách _pub chưa đổi.
Cold-fixed17510actualexit0/khôngFAIL-SKIP, toàn packageDBadminPASS1.376s, coldTS0.36s; private54262/611f9373a98e đãdọn. Log thấtbại full75296 và baseline48780 giữ nguyên; static/full5PG dùng hậu tố verified sau bản sửa, cần helper11 trước tích hợp. Golden/DDL/rules/baseline318 giữ.
