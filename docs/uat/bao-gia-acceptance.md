# Báo giá v2.0 — nghiệm thu lát Xã báo giá #210

Phần này chỉ theo FN-AD-04.02 trong BRD2.0. Các Function khác và nhập Excel xã tiếp tục ở #257;
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


## FN-TI-02.13 — Hai báo cáo tuần chương trình khuyến nông (#284)

Chỉ Function/data Báo giá v2.0 final. Catalog/quy mô kế hoạch lấy nguồn chính thức; các số 10, 2, 1.934, 803.458.000 dưới đây là fixture nghiệm thu theo ví dụ BRD, không phải số thực hiện nạp vào tenant runtime.

| Mã ca | Thao tác | Kết quả cần kiểm | BR/AC |
|---|---|---|---|
| BG284-01 | Tỉnh TTTT mở hai menu chương trình | Tuần nháp đầu theo lịch hệ thống; 19 dòng thông tin, 6 đào tạo; số thực hiện đầu kỳ tenant mới bằng 0 | B1, BR-01/06 |
| BG284-02 | Fixture nông dân chủ chốt: quy mô 12, đầu kỳ 10, nhập tuần 2 | Ngay trên màn hình lũy kế 12/12, đạt 100,00% KH; không tải lại để tính | AC-01 |
| BG284-03 | Thu thập giá: quy mô gốc 6.552, điều chỉnh 3.822 có lý do, lũy kế 1.934 | Tỷ lệ 50,60% theo quy mô điều chỉnh; giữ quy mô gốc và lý do | BR-03, AC-02 |
| BG284-04 | Tổng đầu kỳ đào tạo 803.458.000, nhập tuần 10.000.000 | Tổng 813.458.000; nhóm không cộng hai lần. Nhập tay lũy kế 900.000.000 rồi chốt/tạo tuần/nhập thêm 10.000.000 cho tổng 910.000.000 | BR-02/05, AC-03 |
| BG284-05 | Lưu nháp nhiều lần, chốt xác nhận; gọi thẳng save/add/delete/adjust/import vào kỳ chốt | Tuần cũ chỉ xem, không có nút ghi; server từ chối; tuần mới kế thừa đúng đầu kỳ | BR-01, AC-04 |
| BG284-06 | Thêm mục chính, nhóm có con, thêm con vào mục chính rỗng; thử parent có dữ liệu | TT liên tục, con dưới nhóm, nhóm không nhập; parent có dữ liệu không chuyển nhóm. Xóa nhóm kèm con | Luồng phụ, BR-05 |
| BG284-07 | Xóa từ nháp một mục đã có dữ liệu, hủy rồi xác nhận | Cảnh báo ảnh hưởng lũy kế/tổng cả tuần cũ; hủy không ghi; xác nhận lọc mọi phép chiếu nhưng payload/băm ledger tuần chốt không đổi | Luồng ngoại lệ, BR-07 |
| BG284-08 | Excel gồm dòng hợp lệ, âm, chữ, tên lạ, nhóm, trùng; ô trống và lũy kế bất kỳ | Chỉ dòng hợp lệ ghi; ô trống giữ dữ liệu; cột lũy kế bỏ qua; cảnh báo ghi đè cần xác nhận; tải lỗi theo dòng | AC-05 |
| BG284-09 | Tải mẫu, điền rồi nhập; xuất một kỳ cũ và một kỳ mới | Mẫu đủ hạng mục và trống số liệu; Excel mở được, hai tầng tiêu đề/ô gộp/viền, đúng nhóm-con và tuần/ngày kỳ đang xem; chỉ thông tin có Ghi chú | AC-06 |
| BG284-10 | Xã/TW, Tỉnh phòng khác, ngữ cảnh chỉ xem, Admin chưa hỗ trợ | Xã/TW không menu, đọc dữ liệu rỗng và ghi 403; các ngữ cảnh khác không cấp quyền; hỗ trợ cần MFA | BR-06, AC-07 |
| BG284-11 | Hai người cùng lưu/chốt/tạo tuần/import/xóa; retry sau mất ACK | Mỗi version chỉ một người thắng; người sau 409 tải lại; retry cùng thao tác không thêm event; không lộ replay trước auth/phạm vi | Luồng ngoại lệ, BR-07 |
| BG284-12 | Nhập số mũ lớn/nhỏ, chuỗi rất dài, NaN/Infinity, âm hoặc tiền lẻ | Lỗi đúng ô/dòng; không parse thành 0 hoặc dùng float; quy mô 0 không chia 0 | BR-03/04 |

Native `TestWeekly*284` chạy trên pg-ephemeral chứng minh arithmetic/quyền/CAS/audit/immutable/Excel thực; Vitest và browser production mock chứng minh tương tác, không thay bằng chứng native hoặc UAT dữ liệu thật. Chạy lại theo `docs/roadmap.d/kn-2026-10/PRICE-V2-WEEKLY-PROGRAM.md`.
