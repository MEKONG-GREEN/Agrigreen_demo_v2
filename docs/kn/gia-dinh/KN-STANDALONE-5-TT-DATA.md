## KN-STANDALONE-5-TT-DATA — Thửa ruộng về kho KN tự quản (#271, phần thửa)

- **Căn cứ:** BRD 2.0 Trồng trọt v7.7 FN-XA-03.05 (Vẽ ranh giới thửa ruộng), FN-XA-03.03 (HTX của KN), FN-XA-03.08 và
  FN-TI-02.07 (Lịch công tác — ô "Địa điểm: HTX"); kế hoạch `kn-handoff/PLAN-KN-STANDALONE-20261107.md` quyết định D1
  (KN tự nhập, tự quản), audit `reports/kn-standalone-audit-20261107.md` §3 điểm 1, 3, 4.
- **Dữ liệu (migration pf):**
  - `20261110013754_pf_extension_kn_plot`: sổ ảnh chụp chỉ ghi thêm `pf_extension.kn_plot_version` (`shared.apply_ledger`,
    chuỗi `kn_plot:<ref>`; sự việc `migrated | created | boundary_corrected | attributes_changed`, mỗi dòng là thửa đầy đủ
    sau sự việc) và bảng chiếu `pf_extension.kn_plot` (RLS tenant, chặn DELETE/TRUNCATE, `sync_seq`). Mã thửa mới `KT0000001…`
    (dải riêng, không trùng `TH…` của thửa đã chép).
  - `20261110013753_pf_cooperative_pub_plot_export`: view công bố `pf_cooperative_pub.v1_plot_export` (chỉ VIEW, chủ
    ag_migrator NOBYPASSRLS, chỉ cấp `r_cooperative_pub`) — nguồn đọc của lần chép, KN không nhắc bảng riêng của HTX.
  - `20261110013755_pf_extension_kn_plot_copy`: chép MỘT LẦN thửa chưa ngừng dùng theo từng tenant (đặt `app.tenant_id`;
    danh sách tenant từ đầu chuỗi băm sổ `pf_geo.plot_boundary` ở `shared.chain_head` vì vai migration không bỏ qua RLS).
    Giữ ref, mã, hình, diện tích, thuộc tính v7.7 (hồ sơ cây trồng `plot-profiles` vẫn khớp). HTX nền tảng → hồ sơ KN loại
    HTX đang liên kết (`htx_ref` của phiên bản mới nhất; nhiều hồ sơ thì mã nhỏ nhất); không có → thửa chưa gán HTX;
    HTX cũ giữ ở `legacy_cooperative_ref` (chỉ đối chiếu). Chạy lại được. Không UPDATE/DELETE, không DROP; bảng HTX giữ nguyên.
  - Kịch bản vận hành `scripts/kn271-plot-copy/count.sql` (số thửa và tổng diện tích theo tenant × xã, nguồn so với kho KN;
    rỗng = khớp) và `rollback.sql` (siêu người dùng; dừng nếu thửa đã chép có ghi mới; gỡ đúng phần đã chép, đầu chuỗi băm,
    khóa idem; chạy lại migration chép để áp lại).
- **Lịch sử:** Log của thửa đã chép bắt đầu từ ảnh chụp `migrated`; lịch sử trước khi chép giữ chỉ đọc ở module HTX
  (`pf_coop.plot_event`, `plot_boundary`) — KN không đọc bảng HTX (D1).
- **API:** `/v1/kn/plots*` (danh sách, chi tiết, vẽ, sửa thuộc tính, sửa ranh giới, Log, xuất GeoJSON/KML) do chủ KN
  (`internal/kn/extension/knplot`) đăng ký; hợp đồng chuyển sang `contracts/openapi/modules/extension/kn-plots`, DTO và mã lỗi
  `geo.*` giữ nguyên để web/field-app không đổi. `cooperative_ref` của thửa giờ là mã hồ sơ KN loại HTX (kiểm: có, đang hoạt
  động, trong phạm vi). Gỡ `registerKN` khỏi pf/cooperative (route cũ không còn).
- **Hành vi giữ nguyên:** Cấp Xã, Admin ghi trong xã phụ trách; Cấp Tỉnh xem (ghi → 403 `forbidden`), Cấp Quốc gia xem (ghi →
  403 `geo.read_only`); thửa không phòng ban đọc là Phòng Trồng trọt; diện tích trắc địa tự tính trong PostGIS; chặn mọi chồng
  lấn vượt `geo.overlap_tolerance_pct` (409 `geo.plot_overlap` kèm phần giao); ≥ 3 điểm không thẳng hàng; tự cắt → 422; ngoài
  địa giới xã (khi nối pf/geo) → 422; tên nông dân vào kho định danh, API trả nhãn che; tải tệp qua điểm kiểm soát xuất nội bộ.
  Diện tích khai báo kiểm cú pháp thập phân thường, ≤ 7 chữ số nguyên, ≤ 4 chữ số lẻ **trước** khi parse (`1e100000` → 422).
- **Đồng bộ ngoại tuyến (field-app):** entity chiều xuống `kn_plot`, chiều lên `kn_plot_boundary` (op create/correct) của chủ
  KN; cùng quy tắc như trực tuyến — chồng lấn hoặc sửa trên phiên bản cũ → bản ghi bị từ chối (không có hàng chờ xung đột).
  `BoundaryService` của gói dùng chung nhận `plotEntity`/`pushEntity` (mặc định giữ `plot`/`plot_boundary` cho App HTX).
- **HTX từ hồ sơ KN:** bộ chọn HTX của màn Thửa (web `HtxRecordPicker valueBy="record"`, field-app) đọc
  `/v1/kn/producer-records?kind=cooperative`; Lịch công tác (web `schedule/parts.tsx`, máy chủ `checkKNCooperative`) cũng vậy —
  không còn đọc `/v1/cooperatives`. Gỡ `GET /v1/kn/cooperatives`, `/{ref}`, `/{ref}/log` khỏi pf/party (route KN đọc HTX nền
  tảng); route App HTX `/v1/cooperatives…` giữ nguyên.
- **Nghiệm thu khi module HTX tắt:** `tests/knplot` gắn cổng bật module của pf/party như gốc lắp ráp, tắt `pf/cooperative` và
  `pf/mechanization` cho tenant (đối chứng: route của hai module trả 403 `module_not_enabled`) — FN-XA-03.05 trọn luồng chạy,
  bảng HTX không đổi. Lịch công tác với HTX hồ sơ KN: `internal/kn/extension/handler` `TestFN_XA_03_08_ScheduleHTXFromKNRecords`.
- **Ngoài phần này (issue con):** số máy FN-XA-03.04 (`pf/mechanization`, khóa `htx_ref`) về kho KN; ẩn ứng viên liên kết HTX
  đã công bố (`/v1/kn/producer-htx-candidates`, ô liên kết `htx_ref`) — đi cùng phần máy vì máy còn khóa theo `htx_ref`; nông hộ
  của HTX nền tảng `/v1/kn/cooperatives/{ref}/households` (pf/cooperative). Mã KN cũ trong `pf/cooperative/plot/app/kn.go`
  (không còn route gọi) giữ nguyên theo nguyên tắc không xóa mã hệ khác.
- **BRD tự mâu thuẫn / giả định:** không có.
