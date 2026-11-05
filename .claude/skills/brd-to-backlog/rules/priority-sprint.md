# Priority, story point và xếp sprint

## 1. Priority (theo trường Priority mặc định của Jira)

Xét theo thứ tự; dừng ở luật đầu tiên khớp. Mỗi Story/Epic ghi **một dòng lý do** ở cột *Lý do priority*.

| Priority | Khi nào |
|---|---|
| **Highest** | Việc nền mà **≥ 2 Epic** hoặc **≥ 3 Story** phụ thuộc (đăng nhập, phân quyền, danh mục gốc, khung giao diện); hoặc yêu cầu bảo mật/pháp lý bắt buộc |
| **High** | BRD ghi ưu tiên Cao / Must; hoặc nằm trên luồng chính của quy trình To-Be |
| **Medium** | BRD ghi TB / Should; hoặc luồng phụ, ngoại lệ của luồng chính |
| **Low** | BRD ghi Thấp / Could; tiện ích, báo cáo phụ |
| **Lowest** | BRD ghi Won't / để sau / "giai đoạn 2" — để ở backlog, **không xếp sprint** |

Điều chỉnh:
- **Rủi ro cao làm sớm:** Story có tích hợp hệ ngoài, nghiệp vụ còn câu hỏi mở quan trọng, hoặc công nghệ chưa dùng trong repo → nâng một bậc (tối đa High) và ghi "rủi ro" trong lý do.
- Story không được có priority cao hơn Story nó phụ thuộc; gặp trường hợp này → nâng priority của Story tiền đề.
- Priority của Epic = priority cao nhất trong các Story của nó.
- Sub-task kế thừa priority của Story.

## 2. Story point (Fibonacci)

Ước theo độ phức tạp + khối lượng + rủi ro, gồm cả DB, API, BE, giao diện, test của Story.

| Điểm | Mốc so sánh |
|---|---|
| 1 | Sửa nhỏ trên màn/API có sẵn, không thêm bảng |
| 2 | Một màn xem đơn giản hoặc một API đọc trên dữ liệu có sẵn |
| 3 | Một màn danh sách/chi tiết + API + bảng mới, ít quy tắc |
| 5 | Màn thêm/sửa có nhiều quy tắc kiểm, phân quyền theo vai trò, nhật ký |
| 8 | Luồng nhiều bước có trạng thái (gửi → duyệt → yêu cầu sửa), hoặc báo cáo tổng hợp, hoặc offline |
| 13 | Tích hợp ngoài hoặc tổng hợp phức tạp — **bắt buộc tách** trước khi xếp sprint |

Không ước được (thiếu thông tin) → ghi điểm tạm, đánh dấu "ước tạm" và thêm *Câu hỏi mở*.

## 3. Xếp sprint

Tham số: độ dài **2 tuần**; velocity `V` (mặc định **20 điểm, ghi là giả định**); dự phòng `R` = **15%** cho lỗi phát sinh và review. Năng lực kế hoạch mỗi sprint = `floor(V × (1 − R))` (mặc định 17 điểm).

Thuật toán:
1. Bỏ Story `Lowest` ra backlog.
2. Sắp Story theo thứ tự phụ thuộc (topo). Trong các Story đã sẵn sàng (tiền đề đã xếp), chọn theo: priority → rủi ro cao trước → Story mở khóa nhiều Story khác trước → thứ tự trong quy trình To-Be.
3. Đổ vào sprint hiện tại tới khi đầy năng lực kế hoạch; không vượt. Story còn lại chuyển sprint sau.
4. Story phụ thuộc phải ở **sprint sau** tiền đề. Ngoại lệ: cùng sprint nếu tiền đề ≤ 3 điểm và ghi rõ thứ tự làm trong cột *Phụ thuộc*.
5. Không xếp Story có câu hỏi mở chặn (BRD chưa đủ để làm) vào sprint 1–2; đặt sau và ghi "chờ trả lời Qn".
6. Sprint 1 ưu tiên nền tảng + **ít nhất một lát dọc demo được**. Mỗi sprint có *Sprint goal* một câu mô tả luồng demo được cuối sprint.
7. Ngày sprint: sprint n bắt đầu `ngày bắt đầu + (n−1) × 14 ngày`, kết thúc `bắt đầu + 11 ngày` (thứ Sáu tuần thứ hai nếu bắt đầu thứ Hai). Sprint trùng nghỉ lễ dài (Tết Nguyên đán, 30/4–1/5, 2/9) → ghi ở cột *Rủi ro*, đề xuất giảm tải.

Đầu ra: tab *Sprint plan* (mỗi sprint: ngày, goal, Story, tổng điểm, năng lực, còn trống, rủi ro) và cột *Sprint* trên tab Story/Epic (Epic ghi khoảng sprint, vd `S1–S3`).

Velocity thay đổi khi duyệt → chỉ chạy lại bước 3, giữ nguyên priority và điểm.
