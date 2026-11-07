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

## FN-TI-02.04 — phiên sửa phiếu #237 (BRD v2.0)

| Ca | Thao tác | Kết quả cần kiểm | Bằng chứng kỹ thuật |
|---|---|---|---|
| BG237-01 | Hai cán bộ khác nhau mở sửa cùng phiếu | Một người chiếm; người kia chỉ xem giá và thấy tên người giữ, không thấy handle | Native HTTP/PG race + UI typed/mock + production browser |
| BG237-02 | Gọi trực tiếp Correct, Redate, Delete, kể cả dùng giá Xã đề xuất | Không có handle hợp lệ của chính người gọi thì 409 và không append; quyền/phân công/trạng thái/CAS không nới | Native mọi đường ghi, quyền thay đổi, board gate, hostile numeric204 |
| BG237-03 | Cùng cán bộ mở hai tab, đóng một tab | Hai handle riêng, tab còn lại tiếp tục renew/sửa; CAS chặn phiên bản cũ | Native + typed mock |
| BG237-04 | Gia hạn, nhả, đến đúng hạn; holder cũ renew/release sau takeover | Gia hạn còn hạn mới được, nhả handle chính mình; đến đúng hạn người khác chiếm được; holder cũ không đuổi người mới | Native actual race/expiry/renew/release |
| BG237-05 | Tenant khác, TW, Xã khác phạm vi; thử đọc lease | 403/404, không tra tên/vault, RLS không trả owner/handle ngoài tenant | Native scope/RLS/name spy |
| BG237-06 | Mất ACK, retry cùng key; stale version; lỗi renew; acquire về muộn sau thoát | Replay đúng lần ghi cũ, không append trùng; stale409; form fail-closed; cleanup đúng handle | Native replay/CAS + UI flow |

BR-01/quyền Tỉnh, Admin hỗ trợ giữ nguyên; không có force-unlock UI/quyền. Tham số kỹ thuật/cấu trúc bảng tại `docs/kn/gia-dinh/PRICE-TICKET-EDIT-LEASE.md`. Test mock/browser không thay native PG hay UAT khách. Chỉ công nhận PASS khi log có phần kết, 0 SKIP và đúng source hash/SHA; Windows không có gcc phải chạy race trên Linux CI cuối.

Chạy lại: `scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/price/... ./cmd/api-platform/assembly ./tests/kn77 ./tests/notifyconsumers ./tests/notify ./migrations`; web `npm ci`, `npm run check:api`, `npm run lint`, `npx tsc --noEmit`, `npx vitest run`, `npm run build:mock`, production Playwright `tests/e2e/price-ticket-review204.spec.ts`. Thêm migration tự động khi bootstrap/khởi động API; không sửa migration đã áp dụng. Các API kỹ thuật ở `/v1/price/tickets/{id}/edit-lease` và `/acquire`, `/renew`, `/release`.

BG237-07 (đường đi vòng): Tỉnh A giữ lease Xóa trên phiếu Bản nháp; Tỉnh B gọi nhập thay cập nhật cùng phiếu phải409 `price.edit_lease_lost`, không tăng sổ. Cùng người/tab khác vẫn theo CAS cũ; sau A nhả, B ghi được. Phiếu nhập mới không bị yêu cầu lease. Đây là bảo vệ BR-06 của FN-TI-02.04 trên cùng tài nguyên, không thêm màn/quyền của FN-TI-02.09.
