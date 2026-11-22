## BF-CN-FORM-DESIGN — phần đầu FN-07 (#174)

BRD CNTS v3.7 (07/10/2026), UR-TINH-07/FN-07; prototype A phần danh sách/cấu trúc biểu. Đây là phần đầu có thể kiểm độc lập, **chưa hoàn tất FN-07/AC-03**.

- Màn `/kn/cnts/thiet-ke-bieu-mau` mở thật từ menu, xem năm biểu kỳ native hiện có (BM-01, BM-04, BM-17, BM-05 I/II): mã/tên/tần suất/hạn, phiên bản hiện hành, cấu trúc và bản xem trước. Phiên bản đã cài đọc từ `formsport.Publisher.LatestTemplate`, kể cả khác cấu trúc builtin; chưa cài ghi rõ cấu trúc chuẩn chỉ tham khảo, version/template_id null. Không suy từ đó đã có đủ 19 biểu chuẩn.
- GET `/v1/extension/cnts/form-designs` và POST `.../import-preview` chỉ Admin hoặc Cấp Tỉnh phòng CNTS; Xã/TW/phòng khác trả 403. Chế độ chỉ xem có thể xem/kiểm vì không ghi. Quyền sửa/owner chương trình chưa mở.
- Nạp XLSX (và CSV UTF-8) chỉ **kiểm và xem trước**, không lưu, không gọi DefineTemplate; tối đa 2 MB, 500 dòng chỉ tiêu, 8 cột. Dùng shared/tabular để giới hạn giải nén/số dòng/cột và từ chối công thức. Header theo thứ tự: `key,label,type,required,unit,group,non_negative,options`. Kiểu: text/integer/decimal/date/choice; cờ true/false hoặc rỗng=false. choice có 1–100 lựa chọn ngăn bởi `|`, không trùng/rỗng, mỗi lựa chọn tối đa 100 ký tự. Có lỗi thì trả lỗi theo dòng và `fields=[]`, không có cấu trúc nhập một phần có thể lưu nhầm. Không tính/cấp phát theo giá trị số của ô.
- Xem trước chỉ mô tả bố cục/kiểu, không phải màn nhập/nộp. Liên kết sang màn Phát hành hiện có; chưa thêm chọn phiên bản lịch sử và không tuyên bố đã chứng minh kỳ cũ giữ số theo AC-03.
- Không migration, không sửa core/shared/schema; KN không ghi dữ liệu HTX hay thông tin chủ hộ. Các file sinh menu/OpenAPI/mock sinh qua công cụ, tăng phiên bản menu cache.
- Mock dùng chung dữ liệu biểu kỳ hiện có. CSV hỗ trợ cú pháp ô trích dẫn của helper hiện hành; XLSX giả lập chỉ đọc ZIP stored do mock sinh, máy chủ thật đọc XLSX Excel nén. Test PG dùng workbook thật qua excelize/tabular; không coi mock là bằng chứng parser production.

Phần còn lại đã liên kết child:

- #191 `BF-CN-FORM-DESIGN-SHARED`: cổng list/version + policy định nghĩa theo mã/owner/khóa KNQG, base_version/idempotency, kiểm thống nhất REST/Publisher. Không nới DefineBy Tỉnh trước khi có policy vì sẽ lách được khóa KNQG qua POST /v1/forms/templates.
- #192 `BF-CN-FORM-DESIGN-EDIT`: sửa/lưu version, sao chép, mẫu nhật ký động/GPS/ảnh, biểu điều tra và chọn trong chiến dịch/phát hành, lưu kết quả Excel hợp lệ. Sau hai child mới nghiệm thu đủ FN-07.

Câu hỏi PO: 19 biểu chuẩn cố định theo mẫu gốc có cho đổi cấu trúc hay chỉ sao chép? KNQG luôn giữ BR-02. Không suy diễn quyền sửa nhật ký khi runtime vẫn trường cố định.
