## ARCHIVE-POLICY-FIELDS-205 — W11.69-ARCHIVE-POLICY-FIELDS: Lĩnh vực của tệp đã xuất (#205)

Nguồn: BRD KHTC v2.3 Product Drive, DOCX SHA256 `15859351f90574bf667190b1a12160465ba03f8ca9e408b94c90d285a3502e25`, P713–738/P1592–1615; issue205, review199 và hợp đồng kho hiện hữu. Đây là lát fields-only, chưa hoàn tất toàn bộ chính sách kho, FN/UAT hoặc G1.

### Nguồn metadata và bộ lọc

Mã lĩnh vực lấy từ `data.plan.Entries` của payload đã xuất, áp đúng lựa chọn Adhoc đóng băng và `export_filter` trước khi xét các hàng item thực xuất. Mã riêng của item ưu tiên; nếu thiếu thì đi lên cha gần nhất theo ngữ nghĩa hiện hữu. Hàng tiêu đề chỉ giữ ngữ cảnh, không thêm mã giả khi con ghi đè. Không suy lĩnh vực từ loại báo cáo, tên tệp, phòng, mẫu, kế hoạch hoặc danh mục hiện hành. Không giải mã số decimal không dùng trong projection; giới hạn kích thước JSON trước khi đọc metadata.

Một tệp có thể mang nhiều mã. Metadata `complete`, `partial`, `unknown` được trình bày riêng; tệp cũ chưa rõ lĩnh vực vẫn có mặt trong danh sách mặc định và không được gán mã thay thế. Mã lĩnh vực và trạng thái thiếu metadata là hai bộ lọc khác nhau. Các trang lựa chọn xét đầy đủ ứng viên khớp năm/loại/kỳ/phiên bản/tìm kiếm/lĩnh vực, không dừng sau 10.000 bản xuất. Các lựa chọn lĩnh vực giữ trang mã hữu hạn; lựa chọn kỳ giữ prefix có giới hạn theo offset/limit, không giữ toàn bộ tập kỳ trong bộ nhớ.

Bộ chọn kỳ bỏ điều kiện kỳ đang chọn; bộ chọn mã lĩnh vực bỏ điều kiện chính mã đang chọn, để có thể đổi giá trị. Các facet còn lại vẫn giao nhau. Lifetime token riêng chặn phản hồi A cũ sau khi đã chuyển A→B→A; thao tác A mới vẫn hoạt động.

GET chọn kỳ giữ `q` tìm tên kỳ tương thích API190. `document_q` mới mang bộ lọc kho (tên kỳ hoặc số văn bản); hai điều kiện giao nhau trước phân trang. Không đổi ngữ nghĩa `q` của danh sách kho và lựa chọn lĩnh vực. Bộ lọc lĩnh vực đi cùng xem trước, Excel, Word, lịch sử và body ZIP. Thay bộ lọc bỏ lựa chọn/preview cũ. Thay tenant/actor/context/vai/MFA/API làm mất dữ liệu phiên và chặn tải hoàn tất muộn. Resource xem trước được key theo bản xuất/bộ lọc, không đổi tiêu đề B mà vẫn hiển thị dữ liệu A trong lúc B chờ.

### Quyền, byte và giới hạn

Metadata không cấp thêm quyền. Mọi đường đọc vẫn kiểm reader hiện tại và tenant/kỳ; KNTinh sai phòng và KNXa bị chặn trước file port ở cả stored Word GET và mixed ZIP POST với khóa thao tác mới. Không mở quyền lãnh đạo/liên Trung tâm hoặc quyền cho phòng từ metadata lĩnh vực.

Excel tải nguyên byte đã lưu sau kiểm băm. Wrapper kho dùng projection tối thiểu để đếm toàn bộ mảng hàng đóng băng cho hợp đồng Egress hiện hữu, không thay số chưa biết bằng 0; cấu trúc hàng không xác định thì đóng lỗi. Ghi nhận Egress không phải chứng nhận mạng đã giao tệp. Số văn bản, byte/hash và liên kết Word đã xác minh không bị dựng lại. Preview vẫn dùng renderer full-payload hiện hữu: không tuyên bố đã xử lý an toàn mọi payload legacy lỗi số trong đường Preview.

Mock capture hiện hữu đóng băng `Entries=[]`, nên workflow trình duyệt hiển thị unknown đúng nguồn; không bổ sung metadata từ live plan để giả minh chứng complete. Complete/partial/nhiều mã/inheritance và hơn 10.001 bản xuất được chứng minh riêng bằng native PG và fixture đóng băng cho UI.

### Phần còn lại

[#227 W11.69-ARCHIVE-POLICY-LEGACY](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/227) giữ snapshot/attestation legacy, mở rộng leadership/department reader theo hợp đồng thật và audit toàn chuỗi. Không suy binding Word cũ hoặc quyền khi thiếu port/contract. Không thuộc #202 dashboard/live/nhắc phòng, #215 kho cho phòng, #206 Plan hay #223 clock test. Chưa chứng nhận UAT người dùng, tải đồng thời production hoặc hoàn tất toàn bộ BRD.
