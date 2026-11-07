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

## Hai biểu đồ giá thành — PRICE-COST-CHARTS #218

Function chi tiết FN-TW-01.04 v2 B1/B3 và CT-05; giá bán thuộc cùng kết quả điều tra đã chốt, không dùng bảng giá hằng ngày.

| Mã ca | Vai/phạm vi | Thao tác | Kết quả cần kiểm | FN/BR/AC |
|---|---|---|---|---|
| BG218-01 | TW/Tỉnh/Admin | Mở Giá thành sản xuất | Cột chồng sáu khoản bình quân theo mặt hàng, đồng/phiếu; cột đôi giá thành–giá bán theo mặt hàng/vùng, đồng/kg; số liệu khớp bảng | B3, BR-01/02 |
| BG218-02 | Cùng đợt | Lọc nhóm cây, mặt hàng, vùng, xã; thay đợt/so sánh | Cùng query cho bảng, thẻ và cả hai biểu đồ; mỗi đợt có bộ biểu đồ riêng; không hiển thị biểu đồ cũ trong lúc tải | B1/B3 |
| BG218-03 | Một/nhiều vùng | Xem nhóm 1–2 phiếu và chi phí bằng 0 | Mẫu nhỏ theo nhóm thực tế; số 0 không thành cột dương, không NaN/Infinity | Luồng ngoại lệ, BR-02 |
| BG218-04 | Đợt chưa chốt; phiếu Loại/Đã xóa | So với kết quả nguồn có số liệu khác nhau | Chỉ phiếu Đã duyệt/đợt Đã chốt; bình quân theo từng phiếu, không trung bình lại các giá trị vùng; trung vị vẫn có ở bảng | BR-01/02, AC-01/02 |
| BG218-05 | TW | Xem bảng, biểu đồ và gọi export trực tiếp | Không cột/tên/SĐT nông hộ; không nút xuất, API export 403; không tải raw phiếu | BR-03/04, AC-03 |
| BG218-06 | Tỉnh/Admin; Xã | Xuất hoặc gọi report trực tiếp | Giữ xuất Excel Tỉnh/Admin; Xã report 403; tệp không SĐT | BR-04 |
| BG218-07 | Không có đợt; bộ lọc không có phiếu; lỗi mạng | Mở màn/đổi lọc/Thử lại | Trạng thái chưa có dữ liệu/rỗng/đang tải rõ; retry lấy lại dữ liệu; bảng vẫn là fallback đầy đủ cho biểu đồ | Luồng ngoại lệ, B3 |
| BG218-08 | Desktop/mobile/bàn phím | Xem và cuộn biểu đồ | Nhãn, chú giải, đơn vị, số phiếu; biểu đồ cuộn trong vùng riêng; bảng giữ toàn bộ giá trị và trung vị | B3, hợp đồng UI |

Kiểm native qua PG tạm theo lệnh ở phần trên; test mới `TestBG218CostChartsAggregatesAndFilters`, giữ test `TestBG_FN_TW_01_04_CostMedianNoPII`.
Web: `npx vitest run src/kn/price/surveys`, `npm run build:mock`, production `next start -p <port-riêng>` và Playwright `tests/e2e/price-cost-charts218.spec.ts` cùng port.
Giới hạn: PDF chưa có trên màn hiện hữu, không thêm trong gói hai biểu đồ; chứng cứ browser mock không thay native DB, race Linux/CI đúng SHA hoặc UAT dữ liệu triển khai.
