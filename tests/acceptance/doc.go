// Package acceptance là kiểm thử chấp nhận theo ca kiểm thử trích từ prototype
// TypeScript mekong-green/tests/*.ts (UAT dòng S5 của docs/uat/g1-acceptance.md).
//
// Mỗi kiểm thử dựng nền tảng G1 trong tiến trình (tests/e2e/harness) trên PostgreSQL
// thật của pgtest, dùng MỘT tenant riêng (newWorld) và chỉ đi qua HTTP — trừ job leo
// thang SLA (dựng module extension như cmd/worker với đồng hồ giả). Chỉ port các ca
// thuộc module G1: sổ HTX/tổ chức/nông hộ, thửa và chồng lấn, CGH (định mức, ngưỡng,
// phân loại, cân đối), nhiệm vụ KN có SLA, báo cáo hiện trường, dữ liệu chủ, chống gửi
// trùng. Bỏ qua kho, rơm, cuộn, đơn thuê, đường thủy, mô phỏng, MFA/phiên của prototype,
// cổng sysadmin.
//
// Mỗi kiểm thử ghi nguồn trong chú thích "Nguồn:" và trong log (nguon). Chỗ nền tảng mới
// cố ý khác prototype có chú thích "Khác prototype:" và kiểm theo quy tắc MỚI.
//
// Bảng ánh xạ (prototype → Go; G = giống, K = cố ý khác):
//
//	htx-operations.test.ts  MST-01 / "Khuyến nông lập hồ sơ HTX kèm MST…"          → TestPrototypeMaSoThueHTX              K (MST theo tenant, chưa có claim)
//	brd-2026.test.ts        "Vô hiệu hoá HTX cần lý do ≥ 20 ký tự…"                 → TestPrototypeVoHieuHoaHTXCanLyDo      K (mất phạm vi thay khóa tài khoản, sổ chỉ ghi thêm)
//	db-constraints.test.ts  "CCCD: mã hoá khi lưu…"                                 → TestPrototypeNguoiDaiDienHTXOKhoDinhDanh K (PII chỉ ở kho định danh)
//	brd-2026.test.ts        DEF-ADM-02 trùng SĐT; DEF-CGH-08 che SĐT                → TestPrototypeNongHoTrungSoDienThoai   K (trả duplicate + ref cũ; mẫu che của vault)
//	brd-2026.test.ts        DEF-KN-02; scopes.test.ts SA-08                         → TestPrototypeCanBoXaChiThayDiaBanXa   K (phạm vi từ org_ref trong token)
//	brd-2026.test.ts        DEF-SYS-01 lỗi ràng buộc → thông điệp nghiệp vụ         → TestPrototypeMaToChucTrung            K (mã lỗi problem+json ổn định)
//	brd-2026.test.ts        "Thửa cần tối thiểu 4 điểm…"; htx-operations "<3 đỉnh"  → TestPrototypeThuaToiThieuBonDiem      K (mốc 4 điểm theo BRD)
//	htx-operations.test.ts  "Diện tích thửa ruộng do hệ thống TÍNH…"                → TestPrototypeDienTichDoHeThongTinh    K (declared_area_ha chỉ để đối chiếu)
//	brd-2026.test.ts        DEF-KN-PLOT-01 nơ bướm                                  → TestPrototypeThuaTuCatBiTuChoi        G
//	brd-2026.test.ts        "Chồng lấn cùng HTX → cần xác nhận…; HTX khác → chặn"   → TestPrototypeChongLanCungHTXVaKhacHTX K (mở xung đột chờ xem xét)
//	brd-2026.test.ts        DEF-KN-PLOT-02 giao cạnh                                → TestPrototypeChongLanQuaGiaoCanh      K (ngưỡng 1 %, liền kề không cờ)
//	brd-2026.test.ts        "Xoá mềm ẩn thửa… khôi phục được"                       → TestPrototypeNgungDungThuaCoLyDo      K (retire là sự kiện; chưa có khôi phục)
//	htx-operations.test.ts  "Vẽ lại đường bao thì diện tích tính lại…"              → TestPrototypeVeLaiRanhGioiTinhLaiDienTich K (phiên bản mới + lý do)
//	htx-operations.test.ts  "Khuyến nông vẽ thửa rồi gán cho HTX và thành viên"     → TestPrototypeGanThuaChoHTXVaNongHo    K (sự kiện assigned)
//	brd-2026.test.ts        DEF-HTX-01/02 nạp tệp; "Đọc GeoJSON và KML…"            → TestPrototypeNapThuaTuTep             K (Admin API, kết quả từng đối tượng)
//	brd-2026.test.ts        "Chủ sở hữu: mã CSH-<tỉnh>-xxxxx…"                      → TestPrototypeChuSoHuuMaCSH            K (định danh ở vault)
//	brd-2026.test.ts        "Máy: mã MAY-<tỉnh>-xxxxx…"                             → TestPrototypeMayMaMAY                 G
//	brd-2026.test.ts        "BR-09: số máy của HTX tại ngày T…"                     → TestPrototypeSoMayTaiNgayT            K (vô hiệu hóa là sự kiện)
//	brd-2026.test.ts        "Nhập máy hàng loạt: Cập nhật / Thay thế"               → TestPrototypeNhapMayCapNhatVaThayThe  K (vô hiệu hóa, không xóa)
//	brd-2026.test.ts        "Định mức: phải có văn bản, không chồng…"               → TestPrototypeDinhMucPhaiCoVanBan      K (effective_from, bản ghi bù, R-56)
//	brd-2026.test.ts        "Ngưỡng cảnh báo theo phiên bản…"                       → TestPrototypeNguongTheoPhienBan       G (chỉ phần áp theo ngày vụ)
//	brd-2026.test.ts        classifyCoverage; DEF-CGH-01 dashboard                  → TestPrototypePhanLoaiMucDapUng        K (không cắt 100 %, Thừa ≥ 120 %)
//	brd-2026.test.ts        "Chủng loại máy còn máy tham chiếu… chỉ ngừng"          → TestPrototypeChungLoaiMayChiNgungDung K (không có xóa)
//	brd-2026.test.ts        DEF-HTX-12/10 yêu cầu hỗ trợ thiếu mô tả                → TestPrototypeYeuCauHoTroThieuMoTa     K (cấm SĐT/CCCD trong mô tả)
//	brd-2026.test.ts        "Yêu cầu hỗ trợ quá 24 giờ… leo thang một lần"          → TestPrototypeLeoThangMotLanDuyNhat    K (job worker + outbox)
//	brd-2026.test.ts        DEF-KN-TASK-01 phân công                                → TestPrototypePhanCongCanBoKhongHopLe  K (danh bạ trực, lô nguyên tử)
//	htx-operations.test.ts  SV-02 nhập lại cùng kỳ là cập nhật                      → TestPrototypeBaoCaoGuiLaiLaPhienBanMoi K (phiên bản mới)
//	htx-operations.test.ts  SV-01/03/05 kỳ đúng định dạng                           → TestPrototypeKyBaoCaoVaChotKy         K (chốt kỳ + bản ghi bù)
//	brd-2026.test.ts        "Báo cáo tổng hợp KN gắn cờ tạm tính…"                  → TestPrototypeDashboardTamTinh         K (tạm tính = còn xã chưa báo cáo)
//	htx-operations.test.ts  "Dropdown hành chính lọc dần…"; "Cây hành chính…"       → TestPrototypeDonViHanhChinhLocDan     K (2 cấp, không có ấp)
//	import.test.ts          "Quy đổi địa giới 2025 — 12 tỉnh cũ gộp về 6"           → TestPrototypeQuyDoiDiaGioi2025        K (14 tỉnh cũ, lịch sử hiệu lực)
//	brd-2026.test.ts        DEF-HTX-13 mùa vụ không trùng mã                        → TestPrototypeMuaVuKhongTrungMa        K (không tự viết hoa mã)
//	brd-2026.test.ts        DEF-CGH-05/06/10 sửa chủng loại máy                     → TestPrototypeSuaChungLoaiMay          K (mã bất biến, định mức là tham số)
//	brd-2026.test.ts        "Giống đang được vụ tham chiếu thì chỉ ẩn…"             → TestPrototypeGiongNgungDungChiAn      K (không xóa hẳn)
//	idempotency.test.ts     "Cùng khoá, gửi hai lần…"                               → TestPrototypeCungKhoaGuiHaiLan        G
//	idempotency.test.ts     "Không có khoá…"; "Khoá quá ngắn…"                      → TestPrototypeKhongCoKhoaBiTuChoi      K (khóa bắt buộc, sai dạng → 400)
//	idempotency.test.ts     "Yêu cầu lỗi không được lưu…"                           → TestPrototypeYeuCauLoiVaKhoaCu        K (4xx cũng phát lại)
//	idempotency.test.ts     "Cùng khoá nhưng đường dẫn khác…"                       → TestPrototypeCungKhoaKhacDuongDan     G
//	idempotency.test.ts     "Khoá thuộc về người dùng…"                             → TestPrototypeKhoaTheoTenant           K (khóa theo tenant)
//
// Ca prototype KHÔNG port vì nền tảng chưa có/không đúng BRD (xem báo cáo UAT S5):
// "Không gán được thửa cho thành viên của HTX khác" (geo chưa kiểm nông hộ thuộc HTX),
// "mốc ngưỡng phải tăng dần" (chưa kiểm liên khóa), khôi phục thửa đã ngừng dùng,
// "không vô hiệu hoá được HTX còn vụ đang canh tác" (G1 chưa có vụ/nhật ký).
package acceptance
