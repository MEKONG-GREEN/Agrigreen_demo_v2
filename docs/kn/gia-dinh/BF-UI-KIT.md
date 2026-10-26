## BF-UI-KIT — Bộ UI dùng chung cho lỗi chung (#97)

- Nguồn: PRD `kn-bugfix-brd-alignment` nhóm A (R-A1..A6), System Design `Design_Sample_Screens.pdf`, menu trong các file "Update prototype" và hình menu CNTS tỉnh khách gửi ngày 23/10/2026.
- **Font (R-A3):** dev đã đúng System Design: Noto Serif cho h1–h3, Roboto cho phần còn lại. Gói này không đổi font. Lỗi khách thấy trên Demo cần đối chiếu lại sau khi promote. Tiêu đề thẻ/khối nào không phải h1–h3 sẽ được xử lý trong các gói sweep.
- **Sidebar:** đã có nhóm, mục cha mở/đóng, mục cha tự mở khi mục con đang chọn, thu gọn kèm flyout, footer cuối menu. Gói này chỉ thêm hỗ trợ **menu nhiều cấp**, vì menu TT tỉnh trong Update prototype v2.0 có 3 cấp. Thứ tự và nội dung nhóm thuộc gói BF-MENU (#98).
- **Ô số:** ô số giữ dạng chuỗi và chuẩn hóa kiểu vi-VN qua `normalizeNumberInput`. Mặc định cấm số âm. Gói này chỉ chặn ở web; kiểm phía máy chủ thuộc BF-SRV-GUARD (#113) và từng gói sweep.
- **Không đổi màn nào.** Áp vào màn là việc của #100, #101, #117, để diff nhỏ và review chéo dễ.
