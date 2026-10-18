# Lỗi văn bản trong các BRD Khuyến nông (gửi BA, 10/2026)

Lập ở gói W11.01 (10/10/2026) từ [đối chiếu BRD KN 10/2026](doi-chieu-brd-kn-2026-10.md): Phụ lục C mục 1.1, C8, C9 (Trồng trọt Final, HCTH v3.0) và Phụ lục D §3.5 câu 1, 2, 5, 6, 7, 21, 22 (CNTS v3.7, KHTC v2.2, Báo giá v1.3). Các câu hỏi liên quan: Q-049 (tiền tố mã), Q-052 (lỗi văn bản và mục thiếu).

Danh sách này chỉ gồm lỗi chữ, tham chiếu sai, mục thiếu và chỗ văn bản chưa cập nhật. Mâu thuẫn nghiệp vụ giữa các BRD nằm ở §5 và §4.5 của tài liệu đối chiếu, không lặp lại ở đây.

Dev vẫn làm tiếp trong lúc chờ BA sửa: cột "Cách hiểu tạm" là cách dev đang hiểu văn bản.

## 1. Trồng trọt (bản "Final" = v7.7, 01/10/2026)

Bản Final không có yêu cầu mới so với v7.7 (Phụ lục C §1.1). Các lỗi dưới đây có từ v7.7 và vẫn còn.

| # | Vị trí | Văn bản hiện tại | Đề nghị sửa | Cách hiểu tạm |
|---|---|---|---|---|
| TT-1 | FN-TI-02.01, Luồng xử lý chính, B3 | "Bấm 'Nhắc nhở' ở khung xã chưa nộp để mở FN-TI-05" | FN-TI-02.03 (Trạng thái nộp, nhắc). Không có FN-TI-05 | FN-TI-02.03 |
| TT-2 | FN-AD-04.06, dòng "Người dùng / Menu" | "Admin · Phòng ban ›  › Mẫu báo cáo" (thiếu tên phòng ban giữa hai dấu ›) | "Admin · Phòng ban › {Tên phòng ban} › Mẫu báo cáo" | Như đề nghị; web `/kn/phong-ban/{mã}/mau-bao-cao` |
| TT-3 | NFR, dòng "Bảo mật" | "phân quyền theo Mục VI" | "Mục V" (Mục V.2 là ma trận phân quyền; Mục VI là NFR) | Mục V.2 |
| TT-4 | FN-CH-00.01 | dẫn tới "Mục V.3" (menu theo vai trò) | Thêm Mục V.3, hoặc sửa tham chiếu | Bảng menu tóm tắt ở `docs/brd/kn-trong-trot-v7.7.md` §6 |
| TT-5 | Cả tài liệu | Không có Mục VII (điểm đã xác nhận, điểm còn mở) | Thêm Mục VII, hoặc bỏ mọi tham chiếu tới nó | Các điểm đã chốt ở `kn-trong-trot-v7.7.md` §9 |
| TT-6 | Trang bìa, Lịch sử thay đổi | Tệp tên "Final" nhưng bìa ghi "v7.7", lịch sử dừng ở v7.7 | Thêm dòng phiên bản cho bản Final, hoặc đặt tên tệp theo v7.7 | Final = v7.7 |
| TT-7 | Ma trận truy vết | UR-XA-21 (lịch công tác) trùng mã với UR-XA-21 của HCTH | Thêm tiền tố module: `TT-UR-XA-21` | Ma trận nghiệm thu đã dùng `TT-` |

## 2. Hành chính – Tổng hợp v3.0 (07/10/2026)

| # | Vị trí | Văn bản hiện tại | Đề nghị sửa | Cách hiểu tạm |
|---|---|---|---|---|
| HC-1 | FN-09 (và các chỗ nhắc nguồn chờ xác nhận) | "… nguồn chờ xác nhận, mục IX điểm 1" | v3.0 không có Mục IX. Thêm Mục IX hoặc sửa tham chiếu | Coi là câu hỏi mở (Q-HC-5, Q-052) |
| HC-2 | Lịch sử thay đổi v2.0 | "7 điểm cần xác nhận" | Không có danh sách 7 điểm trong v3.0. Đưa danh sách điểm mở hiện hành vào tài liệu | Như HC-1 |
| HC-3 | Mục thứ tự triển khai | "… → FN-09, FN-23" | Không có FN-23 (21 FN: FN-01…FN-21). Sửa mã | Bỏ qua FN-23 |
| HC-4 | Ma trận truy vết FN-13, FN-17, FN-21 | UR-XA-21 (phiếu đánh giá, Contact) trùng mã với UR-XA-21 của Trồng trọt | Thêm tiền tố module: `HC-UR-XA-21` | `HC-` |
| HC-5 | FN-05 Đăng nhập | Bước 1: tên đăng nhập + mật khẩu + chọn vai trò bằng dropdown; có PIN mobile, offline 72 giờ | Dùng luồng chung của v7.7: email hoặc SĐT, chọn vai trò và phòng ban sau xác thực. Bỏ PIN/offline vì Module 2 chỉ có Web (I.3) | Luồng v7.7 (Phụ lục C, C1, C10) |
| HC-6 | NFR sẵn sàng, sao lưu | "thống nhất khi triển khai" | Ghi theo v7.7 (≥ 99,5 %, RPO 24 giờ, RTO 4 giờ) vì dùng chung hạ tầng | Theo v7.7 (C11) |

## 3. Báo giá nông sản v1.3

| # | Vị trí | Văn bản hiện tại | Đề nghị sửa | Cách hiểu tạm |
|---|---|---|---|---|
| BG-1 | Ba chỗ: phần chức năng dùng chung ("chốt tại Mục VII (điểm 9)"), FN-TI-02.06 BR-06 ("giới hạn mở duyệt trong 30 ngày (cần xác nhận, Mục VII)"), FN-AD-04.04 ("mẫu T2–T6; cần xác nhận, Mục VII") | Tài liệu kết thúc ở Mục VI | Thêm Mục VII (điểm đã chốt/còn mở) | 30 ngày; lịch T2–T6 (chờ xác nhận) |
| BG-2 | Trang thông tin phê duyệt | Dòng "Người lập (BA)" để trống | Điền người lập | — |
| BG-3 | NFR, dòng "Sẵn sàng, sao lưu, phục hồi" | Ô trống | Ghi theo v7.7 (≥ 99,5 %, RPO 24 giờ, RTO 4 giờ) | Theo v7.7 |
| BG-4 | Các FN-AD-04.03 (Danh mục mặt hàng), FN-AD-04.06 (Ngưỡng cảnh báo), FN-TI-02.0x, FN-TW-01.0x | Trùng mã với Trồng trọt và KHTC, khác nghĩa | Thêm tiền tố module `BG-` | `BG-` |
| BG-5 | Tham chiếu BRD Trồng trọt | Dẫn v6.0 (FN-01/04/05) | Dẫn v7.7 (FN-CH-00.01, FN-AD-04.0x) | Phần dùng chung theo v7.7 |
| BG-6 | Tên phòng | "Phòng Thông tin Tuyên truyền" | Thống nhất một tên với Trồng trọt v7.7 ("Phòng Thông tin - Truyền thông") hoặc ngược lại | Chờ Q-02/Q-051 |
| BG-7 | Thiếu phần KHTC yêu cầu (KHTC §3.4) | Không có "bảng giá đã duyệt, số phiếu giá đã thu thập theo tuần báo cáo" và FN-PN-03.01–03 | Bổ sung theo KHTC | Làm theo KHTC (gói W11.10, W11.11) |

## 4. Chăn nuôi – Thủy sản v3.7 và Kế hoạch – Tài chính v2.2

| # | Vị trí | Văn bản hiện tại | Đề nghị sửa | Cách hiểu tạm |
|---|---|---|---|---|
| CN-1 | CNTS kỳ tuần (BM-04, BM-17) | Tuần **Thứ Ba – Thứ Hai**, hạn 12:00 Thứ Ba | KHTC GD-05/BR-DL-01 ghi đã chốt tuần **Thứ Tư – Thứ Ba** cho mọi module và "BRD CNTS cập nhật BM-04, BM-17". CNTS v3.7 chưa cập nhật | Thứ Tư – Thứ Ba (QĐ-6, đề xuất của dev) |
| CN-2 | CNTS | Không có FN-PN-03.01–03 ("Báo cáo cho KHTC") mà KHTC nói mọi phòng phải có | Bổ sung, hoặc KHTC sửa | Thêm vào menu chung (W11.11) |
| CN-3 | CNTS FN-01…21 | Dùng FN-01…21, không tiền tố, trùng số với HCTH | Thêm tiền tố `CN-` | `CN-` |
| CN-4 | CNTS tham chiếu BRD Trồng trọt | Dẫn v6.3 (FN-01…05) | Dẫn v7.7 | Theo v7.7 |
| KH-1 | KHTC FN-AD-04.03 (Mục lục mẫu báo cáo) | Trùng mã với Trồng trọt (Danh mục phòng ban) và Báo giá (Danh mục mặt hàng) | Thêm tiền tố `KH-` | `KH-` |
| KH-2 | KHTC tên phòng | "Phòng Thông tin Tuyên truyền" | Như BG-6 | Như BG-6 |

## 5. Đề nghị chung

- Mọi BRD KN dùng tiền tố module cho mã UR/FN: `TT-` (Trồng trọt), `HC-` (Hành chính – Tổng hợp), `CN-` (Chăn nuôi – Thủy sản), `KH-` (Kế hoạch – Tài chính), `BG-` (Báo giá nông sản). Ma trận nghiệm thu [kn-v7.7-acceptance.md](../uat/kn-v7.7-acceptance.md) đã dùng `TT-` từ 10/10/2026.
- Phần dùng chung (đăng nhập, phòng ban, tài khoản, menu, mẫu báo cáo) dẫn tới BRD Trồng trọt **v7.7**, không dẫn v6.x.
- Mỗi BRD có mục "Điểm đã chốt / còn mở" đánh số, và mọi tham chiếu "cần xác nhận" trỏ vào mục đó.
