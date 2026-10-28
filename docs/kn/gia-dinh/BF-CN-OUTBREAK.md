## BF-CN-OUTBREAK — Báo cáo dịch bệnh BM-10: 4 thẻ thống kê, trang tạo ổ dịch 3 phần, Word, bản đồ (#111)

PRD `kn-bugfix-brd-alignment` UP-07, UP-08; BRD CNTS UR-XA-10 (BM-10, DB-01…DB-13). Phụ thuộc #17 (pf/export), #28 (bản đồ vùng dịch), BF-NOTIFY-CNTS (#115). Làn KN.

| Mục | Quyết định / giả định | Bằng chứng |
|---|---|---|
| 4 thẻ thống kê | Tổng, Chờ xác nhận, Đang theo dõi, Đã kết thúc — đếm theo `by_status` của danh sách (đã áp lọc Bệnh/Xã/ngày, chưa áp lọc trạng thái); bấm thẻ = lọc trạng thái | `outbreak-ui.test.tsx` |
| "Chờ xác nhận" | Là trạng thái `reported` (ổ dịch xã vừa gửi, Cấp Tỉnh chưa xử lý — trước gọi "Đã gửi"). Không thêm trạng thái mới, không đổi sổ | `ListOutbreaks` nhãn, `api.ts` |
| Cột "Mã" | Chưa có mã nghiệp vụ trong sổ ổ dịch: hiển thị `OD-` + 6 ký tự cuối mã ổ dịch (UUID, duy nhất), tìm kiếm được. Nếu PO cần mã đánh số tuần tự theo năm/xã thì làm gói sau (cần migration + cấp số đồng thời) | `outbreakCode` |
| Tìm kiếm, lọc, phân trang | Lọc Bệnh, Xã, Trạng thái, khoảng ngày tiếp nhận phía máy chủ (route sẵn có); tìm kiếm không dấu theo mã/bệnh/vật nuôi/xã/địa bàn và phân trang 10 dòng phía máy khách (danh sách ổ dịch của phạm vi nhỏ). Trạng thái rỗng khác nhau khi chưa có ổ dịch / không khớp lọc | `outbreak-ui.test.tsx` |
| Trang riêng 3 phần | `/kn/cnts/dich-benh/tao-moi` (R-A2): Phần 1 tình hình dịch (DB-01…DB-08, vị trí GPS bắt buộc), Phần 2 xét nghiệm, xử lý, khoanh vùng (DB-09…DB-11), Phần 3 phòng chống, kiến nghị (DB-12). Cập nhật diễn biến và xác nhận tin báo dùng cùng biểu 3 phần, vẫn mở trên màn danh sách | `OutbreakCreateScreen.tsx` |
| Ô số mặc định 0 | Quy mô đàn, số chết, tiêu hủy (con, kg), hóa chất (lít) mặc định 0, không nhận số âm (máy chủ chặn như cũ). Quy mô đàn vẫn phải > 0 khi gửi. Ngày tuổi, sai số GPS để trống (0 sai nghĩa) | `outbreak-ui.test.tsx`, `sweep-cnts.test.tsx` |
| Cảnh báo Khẩn, bản đồ | Đã có từ trước: gửi ổ dịch phát `pf.extension.outbreak.reported` → cntsalert tạo cảnh báo Khẩn (dòng 12 Mục III.5) tới Phòng CNTS; ổ dịch có tọa độ hiện trên Bản đồ vùng dịch (#28). Gói này chỉ thông báo cho người dùng và thêm liên kết "Xem trên bản đồ" | — |
| Xuất Word | `POST /v1/extension/cnts/outbreaks/{ref}/word` qua pf/export: mẫu `cnts.bm10` v1 theo báo cáo mẫu "BC tình hình bệnh dịch tả heo CP xã Hòa Thuận"; số `{n}/BC-TTKN` tự cấp (một số mỗi ổ dịch, dùng lại khi xuất lại; dùng chung dãy BC-TTKN với báo cáo KHTC vì cùng là báo cáo của Trung tâm); dòng "<tỉnh>, ngày …". Đọc ổ dịch trong giao dịch KN rồi đóng; gọi pf/export và đơn vị hành chính NGOÀI giao dịch. Cấp số nằm trong giao dịch của pf/export → không trùng khi xuất đồng thời | `tests/kn77/cnts_outbreak_word_integration_test.go` (8 xuất đồng thời → số 2..9 không trùng) |
| Ai xuất Word | Mọi người xem được ổ dịch (Cấp Xã trong xã phụ trách, Cấp Tỉnh, Admin) | như trên |
