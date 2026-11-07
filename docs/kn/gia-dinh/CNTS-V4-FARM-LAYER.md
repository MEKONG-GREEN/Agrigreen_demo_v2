## CNTS-V4-FARM-LAYER — Lớp trang trại, số hộ/diện tích bị ảnh hưởng của vùng dịch (BRD CNTS v4.0 FN-18, #254)

Căn cứ: BRD CNTS v4.0 FN-18 (mô tả "Lớp trang trại; số hộ, diện tích bị ảnh hưởng của vùng dịch", BR-02, AC-02, AC-03), FN-11
BR-03 ("tổng hợp vùng dịch lấy số của lần cập nhật diễn biến mới nhất, không cộng dồn"), FN-10 BR-07, BR-08, bảng trường HCN-01…
HCN-08, UR-XA-10 DB-15, DB-16; thiết kế theo [ADR-0056](../../adr/0056-ho-chan-nuoi-cnts-v4.md) mục 8–9.

- Không có migration: chỉ đọc sổ danh sách hộ chăn nuôi (#251) và cột `affected_households`, `aqua_*` của sổ ổ dịch (#252),
  cùng module `pf/extension` (vai `extension_app` đã có `SELECT`).
- Lớp trang trại = hộ có phiên bản hiện hành trạng thái Đang dùng và loại `trang_trai_nho`, `trang_trai_vua`,
  `trang_trai_cong_nghiep` (HCN-06); vị trí = lần check-in mà `checkin_ref` của phiên bản hiện hành trỏ tới (hộ đổi vị trí theo
  H4 hiện ở chỗ mới). Hộ Chờ bổ sung vị trí (BR-08), Vô hiệu hóa (BR-07), nông hộ không vào lớp. Khóa của đối tượng trên bản đồ
  là `household_ref`.
- Lớp trang trại là danh sách hiện hành (master data), không dựng lại theo ngày xem; ngày xem (AC-03) áp cho vùng và ổ dịch như
  trước.
- Đối tượng trang trại trên bản đồ hiện mã hộ, loại, ấp, xã, vật nuôi và số con; **không** hiện họ tên, SĐT chủ hộ (ở kho định
  danh, xem ở Danh sách hộ chăn nuôi theo quyền FN-10) — bản đồ không gọi kho định danh.
- "Vùng dịch hiện số hộ, diện tích bị ảnh hưởng" (BR-02) áp cho vùng dịch, vùng bị uy hiếp, vùng đệm (hướng di chuyển là đường,
  vùng chăn nuôi/thủy sản không phải vùng dịch). Số của một vùng = tổng các ổ dịch có vị trí nằm trong hình vùng (PostGIS
  `ST_Covers`) ở ngày xem, mỗi ổ dịch lấy phiên bản diễn biến mới nhất có ngày ≤ ngày xem (không cộng các lần cập nhật). Ổ dịch đã
  kết thúc vẫn tính (số hộ đã bị ảnh hưởng trong vùng tại ngày đó).
- Ổ dịch cũ một hộ (v1, `legacy`) tính 1 hộ, DB-16 trống tính 0 (ADR-0056 mục 8). Diện tích cộng bằng decimal, trả chuỗi 2 số lẻ.
- Cấp Xã chỉ cộng ổ dịch của xã mình (AC-02) — cùng vùng, Cấp Tỉnh có thể thấy số lớn hơn.
- API thêm trường, không thêm route: `layers` có `farms`, `zone_impacts`; `zones/{ref}/contents` có `farms`, `farm_count`, `impact`;
  ổ dịch trên bản đồ có `affected_households`, `aqua_farmed_ha`, `aqua_wild_ha`, `legacy`.

Cách chạy, kiểm:
- Backend: `.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/extension/cntsmap/...`
  (`TestCNTSMap_FarmLayerAndImpact_FN18`, `TestCNTSMap_W1152`, `TestImpactOf`).
- Web: trong `apps/web-ops` (đường dẫn thật của worktree): `npm run gen:api`, `npx vitest run src/kn/cnts/map`, `npm run dev:mock`
  rồi mở `/kn/cnts/ban-do-dich` (bật lớp "Trang trại", bấm vùng dịch xem số hộ bị ảnh hưởng).
