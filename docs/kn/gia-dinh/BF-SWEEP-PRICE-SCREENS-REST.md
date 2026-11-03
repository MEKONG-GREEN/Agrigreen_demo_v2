## BF-SWEEP-PRICE-SCREENS-REST — lát phiếu Cấp Tỉnh (#204)

Lát độc lập từ dev, không stack #198: FN-TI-02.02/02.04 của BRD Báo giá v1.3 ngày07/10/2026 (`kn-handoff/analysis/src/brd_baogia.txt:348–359,378–391`). Danh sách kiểm duyệt mở chi tiết theo ID thật; Hiệu chỉnh/Sửa ngày/Xóa mềm dùng trang riêng, can.* và version của API. Nhật ký dùng GET ticket/{id}/log: header, dòng, cảnh báo có version/actor/at/reason thật. Khi hiệu chỉnh nhiều lần, giá trước là giá hiệu lực ở phiên bản liền trước của cùng mặt hàng, không gọi giá Xã gốc là giá trước mọi lần chỉnh.

### Giới hạn và giả định có chủ ý

- parseDec dùng chung entry/proxy/correct/respond chỉ thêm kiểm plain ASCII Decimal và tối đa512byte trước constructor.512 là policy tài nguyên mới được điều phối cho phép, không phải cap tiền hoặc giới hạn precision của BRD. Nil/exactempty vẫn missing; giữ -1/1.234/bounded value để flow nghiệp vụ cũ quyết định, đặc biệt respond=keep còn parse rồi bỏ giá.00001.2300 và9000.00 hợp lệ trong resource limit. Native ValidPair/cap999999999999.99/SQLnumeric(14,2) không đổi; không clamp/round hợp thức hóa.
- UI NumberField/validateForm gửi chuỗi chuẩn hóa, cặp giá được chọn bắt buộc đầy đủ/dương/đúng thứ tự/cap/2dp. Không gửi lại dòng chưa chọn. Lý do bắt buộc ở cả ba thao tác; Hủy không gọi API ghi.
- Ngữ cảnh Cấp Tỉnh phòng TTTT hoặc Admin mới mở workspace này; readonly/can.* chặn UI ghi. Thay principal/context/MFA/API identity phá local state và lượt idempotency, ACK của component cũ không điều hướng workspace mới. API hiện có vẫn quyết định quyền/tenant/khóa nhập/bảng giá/version. Không tuyên bố UI thay được kiểm quyền server.
- Log chỉ là lịch sử thực từ endpoint, không có GET PriceSheet lịch sử toàn phiếu theo version. Không dựng actor/time từ ngày phiếu/snapshot hoặc đồng hồ trình duyệt. Ref actor chưa có tên được hiển thị đúng mã tham chiếu do endpoint trả.
- Không hiện “đang sửa bởi…” giả: backend hiện dùng khóa giao dịch và optimistic version409, chưa có editor-presence/lease và tên người giữ form theo FN-TI-02.04 AC-02. Không coi AC-02 exclusive editor/presence đã hoàn tất; phần hợp đồng này còn ở remainder#210.
- Nhập Xã/nhập thay/xác minh/duyệt công bố/print-export giữ workflow hiện hữu; không sửa EntryScreen/proposal37/multipart183, API adapters/sharedhooks/sharedform, items đang review198, schema/generated/migration hoặc UAT toàn phòng. Remainder chính thức#210 giữ các mảng khác; không nâng toàn BRD/UAT thành PASS.

### Bằng chứng

Native RED trước guard:1e3 trong correct hợp lệ trả201, adj1000, bộ sổ ticket/line/alert[1,1,0]→[2,2,0]. Đây là repro HTTP/PG thật ở fixture tổng hợp, không claim khai thác tenant/PII. Pure mutation dùng cùng exponent nhỏ, tránh chạy giant-rescale khi cố ý tháo guard. Hostile1e±100000/100000digits chạy trên source có guard. Log local và kết quả cuối được ghi trong bình luận READY đúng SHA; chưa coi tài liệu này là PASS.
