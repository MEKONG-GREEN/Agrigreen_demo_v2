# Báo giá v2.0 — nghiệm thu lát Xã báo giá #210

Lát Xã báo giá #210 theo FN-AD-04.02 trong BRD2.0. Các Function khác và nhập Excel xã tiếp tục ở #257;
không dùng tài liệu v1.3 hoặc prototype làm nguồn yêu cầu cho ca mới.

| Mã ca | Vai/phạm vi | Thao tác | Kết quả cần kiểm | FN/BR/AC |
|---|---|---|---|---|
| BG210-01 | Admin có quyền quản lý | Mở detail xã chưa ngưng; bấm Ngưng | Cùng dialog lý do/ngày như danh sách; chưa xác nhận không phát lệnh | FN-AD-04.02 B3 |
| BG210-02 | Admin | Lý do trống, ngày không hợp lệ hoặc đã qua | Lỗi tại ô; ngày đã qua do server từ chối; dữ liệu/lý do còn để sửa | Luồng ngoại lệ |
| BG210-03 | Admin | Ngưng có ngày hiệu lực | Một lệnh; lịch sử trước ngày đó còn hoạt động, từ ngày đó ngưng; Log có lý do/actor/time native | B3, BR-02 |
| BG210-04 | Cấp Tỉnh; bản ghi đã ngưng | Mở detail/URL sửa | Chỉ đọc; không có Ngưng hoặc lệnh ghi | Người dùng, BR-02 |
| BG210-05 | Admin, mọi ngày xem | Xem cán bộ và lịch sử; thử URL phân công cũ | Chỉ hiển thị; không link/form phân công; URL cũ HTTP404 | BR-03 |
| BG210-06 | Admin | Lưu thêm/sửa xã | Lưu và điều hướng đúng; không success toast | B2, hợp đồng giao diện |
| BG210-07 | Admin | Mất response sau commit rồi thử lại cùng dữ liệu | Cùng khóa idempotency; chỉ một phiên bản Ngưng | BR-02, hợp đồng kỹ thuật |
| BG210-08 | Admin | Đóng dialog đang chờ và mở xã khác | Response cũ không đóng dialog mới; không phát lệnh cho xã khác | Hợp đồng phiên thao tác |
| BG210-09 | Admin | List→detail→Ngưng→quay lại | Giữ ngày/nhóm vùng/tab; chọn ngày tương lai thấy trạng thái đã ngưng, xem lịch sử còn đủ | B1/B3, BR-02/04 |

Test native Price trên PG thật kiểm quyền, lịch sử và hiệu lực API; Vitest kiểm tương tác bằng API mock;
Playwright kiểm luồng browser trên build mock. Đây là ba loại bằng chứng khác nhau, không thay cho khách nghiệm thu dữ liệu thật.

Lệnh chạy: từ gốc worktree dùng `scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/price/... ./migrations`;
trong `apps/web-ops` dùng `npm ci`, `npx vitest run src/kn/price/points`, `npm run build:mock` và
`npx playwright test tests/e2e/price-points182.spec.ts`. Ghi kết quả trên issue đúng SHA sau đủ CI/cổng; chỉ ticket merge vào dev.

## Lịch sử báo giá của Xã #219 — FN-XA-03.03 v2.0

| Mã ca | Thao tác | Kết quả cần kiểm | FN/BR/AC |
|---|---|---|---|
| BG219-01 | Chọn khoảng qua 2 tháng, trên 31 ngày; chuyển Danh sách/Lịch | Cùng khoảng và trạng thái, lịch không mất dữ liệu sau trang đầu; lịch hiển thị ngày, tổng mặt hàng đã cập nhật và trạng thái thật | B1–B2, AC-01 |
| BG219-02 | Xem tháng 2 năm nhuận, trước/sau tháng, qua tháng 12/1 | Ngày và thứ đúng; tháng giới hạn trong khoảng đã chọn; ô ngoài khoảng được ghi rõ | B1, AC-01 |
| BG219-03 | Ngày nghỉ chính thức, ngày báo bổ sung, tương lai | Theo lịch/trạng thái API; không tự gán Không nộp cho ô vắng phiếu; tương lai chưa khóa không Không nộp | AC-01 |
| BG219-04 | Lọc từng trạng thái rồi đổi cách xem | Bản nháp/Đã gửi/Đã khóa/Nộp muộn/Đã duyệt/Không nộp/Đã xóa giữ đúng trạng thái API; tổng mặt hàng = Có giá + Không có hàng | B2, luồng phụ, AC-01 |
| BG219-05 | Chọn phiếu ở lịch, đổi danh sách, In phiếu | Selection giữ qua cách xem/tháng; phiếu Không nộp, Đã xóa không chọn/in; in nhiều đi route in hiện hữu | Luồng ngoại lệ, AC-02 |
| BG219-06 | Bấm phiếu ở cả hai view; dùng Escape đóng | Chi tiết hiện hiệu chỉnh Tỉnh và trao đổi xác minh; không có Lưu/Sửa/Gửi tại lịch sử; focus trở lại nút mở | B3, BR-02 |
| BG219-07 | URL sâu, back/forward, view/tháng/status sai, khoảng đảo/không hợp lệ/quá 12 tháng | Khôi phục đúng bộ lọc; chuẩn hóa view/tháng/status; khoảng sai có lỗi và không gọi API | B1, dữ liệu vào |
| BG219-08 | Sửa URL/API point sang xã khác, đổi phân công và tenant | Không đọc được dữ liệu xã/tenant khác; phạm vi theo phân công tại ngày báo giá; giữ quyền đọc Tỉnh/Admin hiện hữu | BR-01, AC-03 |
| BG219-09 | Giữ response cũ, đổi ngữ cảnh/khoảng rồi cho response cũ về | Gỡ dữ liệu/selection/detail cũ ngay; response cũ không xuất hiện trong ngữ cảnh mới | BR-01, AC-03 |
| BG219-10 | Mạng chậm/lỗi, thử lại, rỗng; keyboard và màn 390px | Loading/lỗi/thử lại/rỗng rõ; nút/select/checkbox dùng bàn phím; không tràn ngang cả trang | B1–B3 |

Native regression `TestPriceHistoryCalendar219` chạy qua HTTP trên PostgreSQL tạm thật; kiểm khoảng qua năm/năm nhuận, lịch nghỉ/bổ sung, quyền và phân công. Vitest `history219.test.tsx` kiểm tương tác/response đến muộn; `history-mock219.test.ts` kiểm parity scope mock. Các loại bằng chứng độc lập, không thay cho nghiệm thu dữ liệu vận hành.
