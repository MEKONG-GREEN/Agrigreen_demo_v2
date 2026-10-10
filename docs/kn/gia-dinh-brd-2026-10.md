# Giả định của đợt W11 (BRD Khuyến nông 10/2026)

Product owner **chưa trả lời** các quyết định QĐ-1…QĐ-10 và câu hỏi Q-0xx trong [đối chiếu BRD KN 10/2026](doi-chieu-brd-kn-2026-10.md) (§2, §4). Trong lúc chờ, các gói W11 làm theo cột **"Đề xuất của dev"** của tài liệu đó. Chỗ nào làm rẻ được thì lựa chọn được giữ thành tham số có phiên bản hoặc seed, để đổi khi PO trả lời mà không phải viết lại mã.

Mỗi gói ghi ở đây một mục gồm: câu hỏi (QĐ/Q), phương án đã chọn, chỗ nó nằm trong mã/tài liệu, và cần làm gì nếu PO chọn khác. Khi PO trả lời, sửa dòng tương ứng và ghi ngày.

Mặc định chung của đợt (theo hướng dẫn đợt W11):
- **QĐ-6:** tuần báo cáo Thứ Tư → Thứ Ba; KSGM 4 tuần/tháng chạy song song; số liệu nguồn sửa được tới khi chốt; KHTC đọc lũy kế của module nguồn.
- **QĐ-5:** phương án (a), G1 gồm Trồng trọt và vận hành Báo giá hằng ngày.
- **QĐ-7:** theo đề xuất của từng câu ở §4.5 và §4.7.

## W11.01 — Tài liệu KN

| Câu hỏi | Phương án đã chọn | Ở đâu | Nếu PO chọn khác |
|---|---|---|---|
| Q-049 (mã FN/UR trùng giữa các BRD) | Thêm tiền tố module (đề xuất của dev): `TT-` Trồng trọt, `HC-` HCTH, `CN-` CNTS, `KH-` KHTC, `BG-` Báo giá. Ma trận nghiệm thu Trồng trọt dùng `TT-` ở cột "Mã" và cột "FN" (mục 3–7). Lời văn, cột tham chiếu và tên kiểm thử `TestFN_…` giữ mã cũ, hiểu là `TT-` | `docs/uat/kn-v7.7-acceptance.md` (ghi chú đầu tài liệu); `docs/kn/loi-van-ban-brd.md` §5 | Nếu BA dùng tiền tố khác: thay chuỗi trong cột "Mã"/"FN" của ma trận. Tên kiểm thử không đổi |
| Q-052 (lỗi văn bản, mục thiếu) | Chưa có trả lời của BA. Dev làm theo cột "Cách hiểu tạm" trong danh sách lỗi (vd FN-TI-05 hiểu là FN-TI-02.03; "Mục VI" hiểu là Mục V; Báo giá Mục VII: mở duyệt 30 ngày, lịch T2–T6) | `docs/kn/loi-van-ban-brd.md` | Sửa dòng tương ứng và báo các gói bị ảnh hưởng (W11.02, W11.71–W11.76) |
| QĐ-6 (lịch tuần) — chỉ phần tài liệu | Danh sách lỗi ghi CNTS v3.7 (tuần Thứ Ba – Thứ Hai) là văn bản chưa cập nhật theo KHTC (Thứ Tư – Thứ Ba) | `docs/kn/loi-van-ban-brd.md` CN-1 | Nếu PO chọn Thứ Ba – Thứ Hai: chuyển CN-1 sang KHTC. Mã kỳ thuộc W11.03 |
| D3 T0 (menu mobile "đã có?") | Xác minh trong mã thay vì chờ: `kn_menu_service.dart` gọi `GET /v1/menu`, `kn_context_picker_screen.dart` chọn ngữ cảnh, có kiểm thử Flutter. UR-CH-01, UR-KT-02, FN-CH-00.01 chuyển sang **đạt** | `docs/uat/kn-v7.7-acceptance.md` | Nếu kiểm trên máy thật thấy thiếu: đưa về **một phần** và ghi phần thiếu |
| "Báo cáo đột xuất" | Thêm dòng `TT-FN-AD-04.06-DX` (**một phần**): menu và mobile Cấp Xã đã có, web thiếu trang `/kn/bao-cao/dot-xuat`. Màn hình thuộc W11.02 | `docs/uat/kn-v7.7-acceptance.md` mục 7 | — |
# Giả định khi chưa có trả lời của khách — BRD KN 10/2026

Mỗi gói W11.x ghi các câu hỏi (§2 QĐ, §4 Q-0xx của tài liệu đối chiếu v4.3/MKG v1.4/BRD KN) mà gói đã
làm theo phương án đề xuất. Khi khách trả lời khác, sửa ở chỗ được nêu.

## W11.06 — Mở phòng ban + menu kn.menu/v3

| Câu hỏi | Phương án đã chọn | Đổi ở đâu nếu khách chọn khác |
|---|---|---|
| Q-051 (QĐ-7) tên và mã phòng ban | 5 phòng ban BRD nêu tên: `trong_trot`, `chan_nuoi_thuy_san`, `ke_hoach_tai_chinh`, `thong_tin_truyen_thong`, `hanh_chinh_tong_hop`. Tên "Phòng Thông tin Tuyên truyền" theo BRD của chính phòng đó (Báo giá v1.3) và KHTC v2.2; **mã giữ** `thong_tin_truyen_thong` (đã có từ v7.7, mã là định danh, không phải tên) | Tên: Admin sửa phòng ban hoặc sửa seed JSON. Mã: chỉ thêm bí danh mới trong `port.legacyDeptAliases`, không đổi mã cũ |
| Q-051 bỏ `phong_ban_5/6` | `phong_ban_5` → `hanh_chinh_tong_hop` (bí danh); `phong_ban_6` bỏ, không kế nhiệm (không BRD nào nêu phòng thứ 6) | `port.legacyDeptAliases`; nếu khách nêu phòng thứ 6, thêm mã mới vào seed và trỏ `phong_ban_6` về mã đó |
| Q-073 HCTH cùng app hay app riêng | Cùng app KN, cùng tenant (đề xuất của dev): HCTH là một phòng ban trong `web-ops` / app di động, menu theo phòng ban | Nếu tách app: thêm `app/HC`, khuôn `hanh_chinh_tong_hop` vẫn dùng được |
| Q-050 đăng nhập | Giữ luồng v7.7 (chọn vai trò + phòng ban sau xác thực); W11.06 không đổi đăng nhập | — |
| Trạng thái seed của 4 phòng ban mới | "Chưa mở" — Admin mở khi module của phòng sẵn sàng (FN-03 AC-01). Mở sớm thì người dùng thấy khung báo cáo theo kỳ chung đang chạy + nhóm "Nghiệp vụ" toàn "Sắp có" | Seed JSON (`status`) hoặc Admin đổi trạng thái |
| Module chưa có trong menu | Hiện mờ "Sắp có" (không ẩn sau cờ) | `domain.MenuItem.ComingSoon`; gói module bỏ cờ khi màn sẵn sàng |
| Nhóm "Dùng chung" | Cuối menu mọi đối tượng (kể cả Admin), hiện có Contact (HCTH FN-13) "Sắp có" | `domain.SharedGroup` |
| Khuôn của phòng ban mới | Khung báo cáo theo kỳ chung (giữ màn đang chạy và trang mặc định của vai) + nhóm "Nghiệp vụ" theo §VI.3 từng BRD | `domain/menu_departments.go`; gói module có thể thay khung chung bằng menu riêng của phòng |
# Giả định khi hiện thực BRD Khuyến nông 10/2026 (chủ sản phẩm chưa trả lời)

Nguồn: đối chiếu v4.3 / MKG v1.4 / BRD KN (09/10/2026) — §2 quyết định QĐ-1…10, §4 câu hỏi Q-0xx. Mỗi gói ghi lựa chọn mặc định đã
dùng ("Đề xuất của dev"); đổi khi chủ sản phẩm trả lời. Mỗi gói một mục.

## W11.71 — Khung `pf/price` (ADR-0049, pf 000138)

| Câu hỏi | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| QĐ-5 / Q-044 — Báo giá có vào G1 15/12? | (a) G1 = Trồng trọt + Báo giá vận hành ngày: `pf/price` ghép vào `app/KN`, `enable: per_tenant` mặc định bật | `app/KN/app.yaml`, `internal/platform/price/module.yaml` | (b): giữ module, tắt cho tenant qua cổng bật module (`pf_party.module_enablement_event`) — không sửa mã |
| Q-082 — Chủ danh mục mặt hàng giá | Một danh mục ở `cap/price` (Phòng TTTT), KHTC đọc qua `price/port.Catalog` | ADR-0049 §3 | Nếu KHTC là chủ: `pf/price` đọc danh mục KHTC qua port, giữ sổ của mình cho lịch sử |
| Mã phòng ban Phòng TTTT (Q-02, mâu thuẫn tên "Thông tin Tuyên truyền" / "Thông tin – Truyền thông") | `thong_tin_truyen_thong` trong `KNPriceProfile.Department` (chỉ thông tin, khung chưa lọc theo phòng ban) | `cmd/api-platform/assembly/price.go` | Sửa một hằng ở gốc lắp ráp |
| Ai ghi danh mục nền ở khung | Chỉ Admin KN (`ManageBy: [admin]`); cờ "Quản trị nghiệp vụ báo giá" của Cấp Tỉnh ở W11.72 | `KNPriceProfile.ManageBy` | W11.72 mở rộng `port.Profile` |
| Đơn vị tính mặc định (BRD không liệt kê) | kg, tạ, tấn, lít, con, trái, bao, chai | `price/domain.DefaultUnits` | Chủ danh mục thêm/ngưng mã (phiên bản mới `retired`); seed chỉ ghi mã chưa có |
| 5 nhóm hàng mặc định | Theo BRD FN-AD-04.03: `lua_gao`, `rau_mau`, `chan_nuoi`, `thuy_hai_san`, `vat_tu_nong_nghiep` | `price/domain.DefaultGroups` | Như trên |
| Ngày hiệu lực danh mục | `valid_from` = ngày báo giá theo giờ VN (BR-02: áp dụng từ ngày báo giá kế tiếp); seed mặc định hiệu lực từ hôm nay | pf 000138, `price/domain.BusinessDay` | Lịch báo giá thật (ngày nghỉ, Q-086) ở W11.74 |
