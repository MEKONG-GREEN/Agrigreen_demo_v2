## CNTS-V4-OUTBREAK-MULTI — Báo cáo dịch bệnh BM-10 nhiều hộ (BRD CNTS v4.0 FN-11, #252)

Căn cứ: BRD CNTS v4.0 FN-11 (BR-03, AC-04, AC-05, luồng phụ "xuất Word theo mẫu"), UR-XA-10 (quy tắc kiểm tra, bảng trường
DB-04, DB-14…DB-16, Hình 10), FN-10 BR-06…BR-08, AC-07, AC-09; thiết kế theo [ADR-0056](../../adr/0056-ho-chan-nuoi-cnts-v4.md)
mục 7–8.

- DB-04 là bảng con chỉ ghi thêm `pf_extension.cnts_outbreak_household`: mỗi phiên bản ổ dịch v2 ghi đủ tập hộ của phiên bản
  (chụp `household_seq`, loại chăn nuôi DB-14) trong cùng giao dịch với dòng sổ. DB-15 (`affected_households`) = số dòng bảng
  con, máy chủ đếm; giá trị gửi lên bị bỏ qua (AC-04). Tối đa 200 hộ mỗi phiên bản.
- Hộ chọn được: có trong danh sách hộ chăn nuôi, cùng xã với ổ dịch, Đang dùng hoặc Chờ bổ sung vị trí (BR-08 "dùng được trong
  báo cáo dịch bệnh"); hộ vô hiệu hóa → 422 (BR-07). Báo cáo cũ đọc kèm trạng thái hiện hành của hộ.
- Hộ nhập mới ở DB-04: họ tên (bắt buộc), SĐT (tùy chọn), ấp, loại chăn nuôi, một hoặc nhiều vật nuôi (HCN-05 bắt buộc ở danh
  sách hộ) → ghi họ tên/SĐT vào kho định danh trước, rồi một giao dịch tạo hộ `created_pending` (mã HCN từ bộ đếm,
  `origin_outbreak_ref`) cùng phiên bản ổ dịch. Không chạy cảnh báo trùng họ tên của FN-10 ở luồng này (BRD chỉ đặt cảnh báo
  trùng ở màn Danh sách hộ; báo dịch không chờ xác nhận) — cán bộ xã gộp/vô hiệu hóa hộ trùng ở danh sách.
- "Bệnh nhóm thủy sản" (UR-XA-10 "danh mục bệnh, FN-06"): một loài trong `attrs.species` của bệnh có `attrs.group = thuy_san`
  ở danh mục vật nuôi; bệnh chưa khai loài thì xét nhóm của vật nuôi ổ dịch. Danh mục đã có thuộc tính nhóm, không thêm cột.
  DB-16 bắt buộc cả nuôi trồng và khai thác (0 được) với bệnh thủy sản; bệnh khác gửi DB-16 → 422 (không âm thầm bỏ).
- DB-16 nhận chuỗi thập phân thường, ≤ 10 chữ số nguyên, ≤ 2 số lẻ; kiểm cú pháp trước khi parse (sai → `validation_failed`).
- Địa bàn (`locality`) thành ghi chú tùy chọn; `household_ref` (một hộ, dữ liệu cũ v1) không ghi được nữa.
- Kết thúc/mở lại (Cấp Tỉnh) chụp lại nội dung và tập hộ hiện hành, giữ phiên bản quy tắc của bản hiện hành: ổ dịch cũ v1 kết
  thúc vẫn là dữ liệu cũ; cập nhật diễn biến luôn tạo phiên bản v2 (phải chọn hộ).
- Ổ dịch cũ: không chuyển đổi (không có hộ, loại chăn nuôi để tạo hộ đúng HCN-02…06). API trả `legacy = true`,
  `affected_households = 1`, `households = []`. Quay lui: gỡ route/màn v2; bảng con và cột mới giữ (không DROP).
- Ràng buộc DB-04 trên sổ được thay: `cnts_outbreak_household` (cần `household_ref` hoặc `locality`) → `cnts_outbreak_db04`
  (v1 như cũ; v2 cần `affected_households`, `household_ref` NULL). Dòng cũ đều thỏa (VALIDATE).
- Word BM-10 lên mẫu phiên bản 2: thêm số hộ (DB-15), bảng hộ (mã, chủ hộ che bớt, ấp, loại chăn nuôi), DB-16 khi có. Lần xuất
  cũ giữ phiên bản mẫu 1 trong lịch sử pf/export.
- Tin báo nghi dịch (`cnts_suspect`) giữ quy tắc v1; xác nhận tin báo mở BM-10 v2 (chọn hộ).
- Lớp bản đồ/vùng dịch (FN-18 BR-02: số hộ, diện tích thủy sản bị ảnh hưởng; lớp trang trại) để CN-6 #254 đọc
  `affected_households`, `aqua_*` của phiên bản mới nhất.

Cách chạy, kiểm:
- Backend: `.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/extension/outbreak/... ./internal/kn/extension/cntslivestock/... ./migrations/`
  (kịch bản đếm trước/sau migration: `-run TestOutbreakMultiUpgradeKeepsLegacy`).
- Web: trong `apps/web-ops` (đường dẫn thật của worktree): `npm run gen:api`, `npx vitest run src/kn/cnts/outbreak`, `npm run dev:mock`
  rồi mở `/kn/cnts/dich-benh/tao-moi`.
