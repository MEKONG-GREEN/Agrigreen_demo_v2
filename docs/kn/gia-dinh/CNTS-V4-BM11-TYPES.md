## CNTS-V4-BM11-TYPES — BM-11 theo loại chăn nuôi, số hộ/trang trại trang chủ (BRD CNTS v4.0 FN-16, FN-17, #253)

Căn cứ: BRD CNTS v4.0 FN-16 (BR-07, AC-05; UR-TINH-16 "Báo cáo định kỳ thêm bảng theo loại chăn nuôi"; Hình 48), FN-10
(BR-06, BR-08; bảng trường HCN-05, HCN-06, HCN-08), Mục I.6 định nghĩa "Trang trại", FN-17 (UR-TINH-06, Hình 14);
[ADR-0056](../../adr/0056-ho-chan-nuoi-cnts-v4.md) mục 9.

- Lệch BRD một dòng: FN-17/UR-TINH-06 không liệt kê số hộ/trang trại trong "chỉ số nóng" và BR-01 ghi "số liệu chỉ từ biểu đã
  nộp"; phạm vi gói (#253) yêu cầu số hộ/trang trại ở trang chủ — làm theo phạm vi gói, đếm từ danh sách hộ chăn nuôi (master
  data, không phải biểu) với cùng quy tắc FN-16 BR-07 (hộ Đang dùng).
- Đếm theo `household_ref`, bản hiện hành của sổ `cnts_livestock_household`, chỉ hộ `active`; hộ Chờ bổ sung vị trí, Vô hiệu
  hóa không tính (FN-10 BR-08, ADR-0056 mục 6, 9). Xã của hộ là xã ở bản hiện hành (hộ đã chuyển xã chỉ tính ở xã mới).
- Trang trại = loại `trang_trai_nho`, `trang_trai_vua`, `trang_trai_cong_nghiep` (Mục I.6). Số con theo vật nuôi tách hai cột
  "Nông hộ", "Trang trại" như Hình 48; tên vật nuôi theo danh mục FN-06 đang dùng, mã không còn trong danh mục hiện mã.
- Bảng lưu trong nội dung phiên bản (jsonb đóng băng của `cnts_bm11_event`, không thêm migration): tính lúc lập và lúc "Làm mới
  số liệu"; hộ đổi loại, vô hiệu hóa sau đó không làm đổi báo cáo đã lập (FN-10 BR-06). Báo cáo lập trước gói này không có bảng
  (`herds = null`, màn ghi "lập trước CNTS v4.0").
- Phạm vi xã: các xã của tỉnh theo pf/party (như "Tổng hợp từ n/N xã"); chưa nối pf/party thì mọi xã của tenant. Dòng toàn tỉnh
  ghi "Toàn tỉnh (n/N xã có dữ liệu)" như Hình 48.
- Word BM-11: thêm mẫu v2 (bảng theo loại chăn nuôi dưới bảng Chăn nuôi); giữ mẫu v1 không đổi để băm mẫu đã ghi sổ ở pf/export
  không lệch.
- Trang chủ: hai thẻ `livestock_households` "Hộ chăn nuôi đang dùng", `livestock_farms` "Trang trại chăn nuôi" (chỉ phòng Chăn
  nuôi – Thủy sản), bấm mở "Danh sách hộ chăn nuôi"; số toàn tenant như thẻ ổ dịch hiện có.

Cách chạy, kiểm:
- Backend: `.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/extension/reportcycle/... ./internal/kn/extension/depthome/... ./internal/kn/extension/cntslivestock/... ./tests/kn77/`
- Web: trong `apps/web-ops` (đường dẫn thật của worktree): `npm run gen:api`, `npx vitest run src/kn/cnts/bm11 src/kn/depthome`,
  `npm run dev:mock` rồi mở `/kn/cnts/bao-cao-dinh-ky` (lập báo cáo → mục "Hộ, trang trại theo loại chăn nuôi") và trang chủ phòng.
