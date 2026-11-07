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

## PRICE-V2-SMALL-CR — hủy mở duyệt và nhắc nhập giá (#283)

Theo Function chi tiết BRD Báo giá v2.0; FN-XA-03.04 BR-01/AC-01 loại xã đã gửi ngay ở lần 07:30.

| Mã ca | Vai/phạm vi | Thao tác | Kết quả cần kiểm | FN/BR/AC |
|---|---|---|---|---|
| BG283-01 | Tỉnh có quyền duyệt | Mở duyệt; chưa sửa rồi Hủy mở duyệt | Nút hoạt động; về Đã duyệt phiên bản cũ, thêm một nhật ký; retry cùng khóa không thêm dòng | FN-TI-02.06 luồng phụ, BR-02 |
| BG283-02 | Tỉnh có quyền duyệt | Sau mở duyệt: hiệu chỉnh, sửa ngày đi/tới, xóa, nhập thay hoặc mở lại cảnh báo | Hủy mở duyệt hiện nhưng disabled, không POST; gọi trực tiếp trả price.board_changed, không ghi sổ; phải duyệt lại tạo phiên bản mới | FN-TI-02.06 luồng phụ, BR-02/04 |
| BG283-03 | Tỉnh chỉ xem/không QTNV; Admin chính thức; Xã/TW | Xem bảng giá đang mở duyệt | Không có thao tác Hủy mở duyệt; backend không cấp quyền. Tenant khác không thấy dữ liệu gốc | FN-TI-02.06 người dùng, phạm vi hiện hữu |
| BG283-04 | Job lúc 07:30 | Một xã Đã gửi, một Bản nháp, một Chưa nhập (đã gửi ngày khác) | Chỉ hai xã chưa gửi có sự kiện chuông/email; xã Đã gửi không có trong hàng đợi | FN-XA-03.04 BR-01, AC-01 |
| BG283-05 | Job lúc 10:30 | Xã Bản nháp gửi trước 10:30; xã còn lại chưa nhập | Chỉ xã chưa nhập nhận lần 2; chạy lặp không gửi trùng | FN-XA-03.04 AC-02 |
| BG283-06 | Job nhiều tenant/ngày | Chạy trước giờ, ngày không báo giá, tenant khác có cùng mã xã | Không gửi trước giờ/ngoài ngày báo giá; trạng thái/người nhận không lẫn tenant hoặc ngày | FN-XA-03.04 BR-01/02/03 |

Chạy `TestPrice283*` bằng pg-ephemeral và Vitest `src/kn/price/tickets/board-cancel.test.tsx`; cổng module/consumer theo [gia-dinh PRICE-V2-SMALL-CR](../kn/gia-dinh/PRICE-V2-SMALL-CR.md). Native PG kiểm bản công bố, băm sổ và outbox; build production mock kiểm tương tác giao diện.
