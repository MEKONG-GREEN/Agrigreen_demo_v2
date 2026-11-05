# Bố cục Google Sheet bản nháp

Tên tệp: `Backlog nháp — <mã-hệ> — BRD <phiên bản> — <yyyy-mm-dd>`, đặt trong folder BRD. Hàng 1 mỗi tab là tiêu đề cột, cố định (freeze) hàng 1 và cột mã; bật lọc. Cột *Duyệt* dùng data validation `OK, Sửa, Bỏ`. Ô nhiều dòng (tiêu chí nghiệm thu) bật wrap text.

Thứ tự tab:

### 1. Tổng quan
Bảng hai cột *Mục* / *Giá trị*: Hệ · Tệp BRD (link) · Phiên bản / modifiedTime · Ngày chạy · Velocity (ghi "giả định" nếu mặc định) · Dự phòng % · Ngày bắt đầu sprint 1 · Độ dài sprint · Số Epic / Story / Sub-task · Tổng điểm · Số sprint · Số câu hỏi mở · Trạng thái (`Chờ duyệt` / `Đã duyệt` / `Đã tạo ticket`) · Cách duyệt (một dòng hướng dẫn dùng cột *Duyệt*).

### 2. Yêu cầu
`Mã` · `Mục BRD` · `Loại` · `Ưu tiên BRD` · `Vai trò` · `Tiên quyết` · `Tóm tắt` · `Story phủ` (mã tạm, cách nhau dấu phẩy) · `Trạng thái phủ` (`Đủ` / `Thiếu` / `Câu hỏi`)

Tab này cũng là **ma trận truy vết**.

### 3. Epic
`Mã tạm` · `Summary` · `Mục tiêu` · `Trong phạm vi` · `Ngoài phạm vi` · `Chỉ số thành công` · `Mã yêu cầu` · `Mục BRD` · `Phụ thuộc` · `Priority` · `Lý do priority` · `Tổng điểm` · `Sprint` · `Duyệt` · `Ghi chú duyệt` · `Jira key`

### 4. Story
`Mã tạm` · `Epic` · `Summary` · `Vai trò` · `Tôi muốn` · `Để` · `Tiêu chí nghiệm thu` · `Quy tắc nghiệp vụ` · `Kiểm nhập (server)` · `Phi chức năng` · `Story point` · `Priority` · `Lý do priority` · `Phụ thuộc` · `Sprint` · `Mã yêu cầu` · `Mục BRD` · `Giả định / câu hỏi` · `Duyệt` · `Ghi chú duyệt` · `Jira key`

### 5. Sub-task
`Mã tạm` · `Story` · `Loại` · `Summary` · `Việc cần làm` · `Hoàn thành khi` · `Ước lượng (giờ)` · `Duyệt` · `Ghi chú duyệt` · `Jira key`

### 6. Sprint plan
`Sprint` · `Từ ngày` · `Đến ngày` · `Sprint goal` · `Story` (mã tạm) · `Tổng điểm` · `Năng lực kế hoạch` · `Còn trống` · `Rủi ro / chờ trả lời`

Dòng cuối: *Backlog (chưa xếp sprint)* với Story `Lowest` và Story bị chặn.

### 7. Câu hỏi mở
`Mã` (Q1…) · `Mục BRD` · `Câu hỏi` · `Ảnh hưởng` (Story) · `Mức` (`Chặn` / `Quan trọng` / `Nhỏ`) · `Giả định tạm` · `Người trả lời` · `Trả lời`

### 8. Kiểm tra
`Mục kiểm` · `Kết quả` (`Đạt` / `Trượt` / `Bỏ qua`) · `Chi tiết` — một dòng cho mỗi mục trong [checklists/quality.md](../checklists/quality.md).

Chế độ cập nhật (BRD đổi phiên bản): thêm cột `Thay đổi` (`Mới` / `Sửa` / `Đóng`) ở đầu tab Epic, Story, Sub-task; chỉ liệt kê dòng có thay đổi.
