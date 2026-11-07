## PRICE-HISTORY-CALENDAR — FN-XA-03.03 (Báo giá v2.0)

- Nguồn nghiệp vụ: Function chi tiết FN-XA-03.03 trong BRD Báo giá v2.0; phần này thay lựa chọn chỉ có Danh sách của W11.75.
- Dùng nguyên API `GET /v1/price/tickets/history`: trả đầy đủ `rows` trong khoảng tối đa 366 ngày, không có phân trang API. Phân trang 31 dòng chỉ áp dụng khi trình bày Danh sách; Lịch dùng toàn bộ `rows`.
- API dựng các ngày từ lịch chính thức `planRange` (ngày trong tuần có hiệu lực và ngoại lệ nghỉ/bổ sung), trạng thái `TicketView` theo giờ khóa và bảng giá. UI không tạo trạng thái Không nộp từ ngày không có bản ghi. Ô ngày nghỉ/ngoài lịch không tự có phiếu; tương lai chỉ có Chưa nhập nếu API trả về.
- Cấp Xã đọc đúng xã được phân công tại ngày báo giá; không lấy phạm vi từ query `point`. Cấp Tỉnh/Admin tiếp tục quyền đọc hiện hữu. Không mở thêm quyền hoặc màn chỉnh sửa; chi tiết/hiệu chỉnh/trao đổi và in dùng luồng hiện hữu.
- URL chứa `from`, `to`, `view`, `month`, `status`, và `point` khi có; ngày/khoảng sai hiện lỗi trước khi gọi API. View/trạng thái/tháng không hợp lệ được chuẩn hóa an toàn. Đổi ngữ cảnh hoặc khoảng ngày remount phần dữ liệu để gỡ cả selection/detail và response cũ.
- Ngày nghiệp vụ theo UTC+7 qua `vnToday`, ngày lịch dùng phép tính UTC để không lệch theo múi giờ trình duyệt. Lịch hiển thị tuần bắt đầu Thứ Hai; trên điện thoại thành danh sách ngày có nhãn đầy đủ.
- Không thay dependency, schema, migration, port, menu hay OpenAPI. Không kết nối nguồn ngoài và không ghi dữ liệu HTX. Bề mặt hiện hữu `/kn/bao-gia/lich-su` và API history sẽ được đối chiếu `brd-surface.yaml` sau khi gói #262 đã gộp vào dev thật.

Chạy kiểm từ gốc worktree với toolchain trong `go.mod` và môi trường của CONTEXT:
`./scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/price/...`.
Trong `apps/web-ops`: `npm ci`, `npm run check:api`, `npm run lint`, `npx tsc --noEmit`, `npx vitest run`, `npm run build:mock`.
