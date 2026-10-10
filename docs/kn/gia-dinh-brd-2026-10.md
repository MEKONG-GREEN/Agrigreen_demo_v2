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
