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


## FN-CH-00.01 / Q10 — yêu cầu hỗ trợ trong KN (#274)

| Ca | Thao tác | Kết quả |
|---|---|---|
| BG274-01 | Từ Login, không đăng nhập, mở Liên hệ quản trị viên; nhập họ tên/contact/nội dung và gửi | Record KN đã commit, 202; thông báo tiếp nhận, không lộ tài khoản hoặc request khác |
| BG274-02 | Tài khoản không có assignment/phòng ban đang áp dụng | Link liên hệ dùng được, không bắt chọn KN context để gửi |
| BG274-03 | Thiếu trường, sai contact, quá dài, thêm tenant/actor/role, JSON dư hoặc file | Lỗi đúng trường/413/415/422 trước parse quá giới hạn; không thêm record; không echo PII |
| BG274-04 | Mất ACK rồi retry cùng UUID; gửi đồng thời cùng UUID | Chỉ một request + sự kiện submitted; UUID khác nội dung bị từ chối |
| BG274-05 | Admin KN + MFA mở queue/detail | Chỉ tenant hiện tại; queue không PII, detail qua vault purpose riêng và commit audit; script trong message hiện như văn bản |
| BG274-06 | User thường, realm Admin chưa chọn phiên, foreign Admin, mất MFA/role/assignment; gọi API trực tiếp | Không nhận plaintext/queue khác tenant; kiểm quyền trước retry/cache |
| BG274-07 | Hai Admin/cửa sổ xử lý cùng version | Một action commit, action stale 409; cùng UUID/action retry trả kết quả đã commit |
| BG274-08 | Làm hỏng vault/KN audit hoặc chuyển ciphertext sang request khác | Không trả plaintext hoặc success giả; rollback giữ dữ liệu/chuỗi audit hợp lệ |
| BG274-09 | Đổi account/tenant/context/MFA khi detail đang tải; xem trên mobile/bàn phím | Bỏ response cũ, không giữ PII ở storage/URL; form/loading/error/retry/focus dùng được |

Native PG + HTTP vault thật: `tests/support274`; validation/rate native: `internal/kn/contact/handler`.
Tương tác và mock contract/quyền: `apps/web-ops/src/kn/support/support274.test.tsx`; browser production dùng bản build mock, riêng với bằng chứng native.
Provision/deployment: [runbook hỗ trợ KN](../runbooks/kn-account-support.md). Không gửi email/chat cho người thật khi thử.
