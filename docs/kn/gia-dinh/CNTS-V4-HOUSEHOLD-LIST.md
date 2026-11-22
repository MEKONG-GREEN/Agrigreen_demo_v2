## CNTS-V4-HOUSEHOLD-LIST — Danh sách hộ chăn nuôi (BRD CNTS v4.0 FN-10, #251)

Căn cứ: BRD CNTS v4.0 FN-10 (H1–H4, luồng phụ, luồng ngoại lệ, BR-03…BR-08, AC-05…AC-09, bảng trường HCN-01…HCN-09), ma trận
quyền "Danh sách hộ chăn nuôi (master data)", menu "Số liệu cấp xã › Danh sách hộ chăn nuôi"; thiết kế theo
[ADR-0056](../../adr/0056-ho-chan-nuoi-cnts-v4.md).

- Lệch BRD một dòng (ADR-0056 mục 4): lịch sử HCN-09 của họ tên, SĐT hiện "cũ → mới" ở dạng che bớt (`Nguyễn V. A. → Nguyễn V. B.`,
  `0912•••123 → 0913•••456`), không nguyên văn — theo HCN-02 "dữ liệu cá nhân" và Mục I.8.1.
- Kho định danh: `client.Profiles.Amend` giữ chữ ký cũ (pf/hrm, pf/contact không phải sửa); mỗi lần sửa đều ghi bản sửa. Mã bản sửa
  lấy qua phần mở rộng tùy chọn `client.Revisions` (`AmendRevision`, `Revisions`); HTTP `POST /v1/subjects/{ref}/amend` trả thêm
  `revision_ref`, `POST /v1/subjects/revisions` trả nhãn che bớt.
- Cảnh báo trùng so họ tên đã chuẩn hóa (bỏ khoảng trắng thừa, không phân biệt hoa thường) và ấp đã chuẩn hóa, với tối đa 500 hộ
  Đang dùng ghi gần nhất của xã; mỗi hộ một lần `Resolve` (mục đích `extension.contact`, kho ghi nhật ký).
- Sửa họ tên/SĐT: máy chủ `Resolve` giá trị hiện tại để biết trường nào đổi; SĐT để trống khi sửa = giữ nguyên (máy chủ chỉ trả SĐT che).
- Ngưỡng cảnh báo sai số GPS dùng tham số `cnts.gps_accuracy_m` (FN-13, mặc định 50 m); vị trí giả lập chỉ đánh dấu, vẫn lưu.
- Cấp Tỉnh: chỉ nhóm `tong_hop` (cán bộ tổng hợp) thêm, sửa, check-in, vô hiệu hóa; nhóm khác và nhóm chưa phân chỉ xem — đúng
  chữ ma trận "vai trò khác: R".
- Hộ `created_pending` (BR-08) do BM-10 v2 tạo — thuộc #252; gói này làm đường check-in chuyển Đang dùng và lọc trạng thái.
- Web dùng GPS của trình duyệt trên điện thoại (Permissions-Policy đã cho `geolocation=(self)`); BRD FN-10 không có màn Mobile riêng
  cho danh sách hộ nên field-app không đổi trong gói này.

Cách chạy, kiểm:
- Backend: `.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/extension/cntslivestock/... ./internal/vault/identity/... ./cmd/identity-vault/ ./migrations/...`
- Web: trong `apps/web-ops` (chạy từ đường dẫn thật của worktree): `npm run gen:api`, `npx vitest run src/kn/cnts/livestock`,
  `npm run dev:mock` rồi mở `/kn/cnts/so-lieu-xa/ho-chan-nuoi`.
