## CI-KN-P6S-CLOCK — Đồng hồ kiểm thử P6s

### Phạm vi và nguyên nhân

Gói [#223](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/223) sửa riêng test trình duyệt P6s đang phụ thuộc ngày thật.
Từ ngày 04/11/2026, lịch Trồng trọt chuyển sang phiên bản 2, tuần theo năm; kỳ 04/11–10/11 là Tuần 45/2026,
trong khi locator cũ luôn đòi Tuần 1. Đây là lỗi oracle của kiểm thử, không thay đổi lịch nghiệp vụ.
Ngoại lệ làn đã ghi trên issue: Codex chỉ sửa `tests/e2e/kn-p6s.spec.ts` và tài liệu gói, Claude review chéo.

### Hợp đồng kiểm thử

| Giờ Việt Nam cố định | Khoảng ngày đợt, đủ 28 ngày | Nhãn kỳ đầu / lịch |
|---|---|---|
| 02/11/2026 09:00, thứ Hai đầu tháng | 02/11–29/11/2026 | Tuần 1 / v1 |
| 04/11/2026 00:00, mốc đổi lịch | 04/11–01/12/2026 | Tuần 45/2026 / v2 |
| 30/11/2026 23:59, cuối tháng | 25/11–22/12/2026 | Tuần 48/2026 / v2 |
| 01/12/2026 00:00, tuần giao tháng | 25/11–22/12/2026 | Tuần 48/2026 / v2 |

Mỗi ca cố định `page.clock.setFixedTime` trước navigation đầu, giữ timer chạy và nhập ngày rõ ràng tại bước 3.
Sau phản hồi xếp gửi 202, tiến riêng Date thêm 2 giây: mock giao thư tại `deliver_at = at + 1500 ms` rồi chuyển trạng thái khi UI làm mới.
Mốc cuối tháng vẫn là 23:59:02 cùng ngày; giữ nguyên kiểm phản hồi 202, poll gửi thành công và mọi timeout, không đổi worker giả lập.
Oracle tĩnh độc lập kiểm đủ bốn khoảng kỳ ở bước 5, tám công việc cho hai xã và toàn bộ vòng đời Tỉnh/Xã/Chi cục/TW.
Giữ kiểm lý do, nhật ký ô, chốt kỳ, xuất/gửi báo cáo, TW chỉ xem cùng test Admin/menu; không đổi helper đăng nhập,
quyền, timeout, retry, mock/runtime, workflow, dependency, SQL hay baseline. Không thêm giả định nghiệp vụ hoặc tuyên bố UAT.
Bàn giao cần kiểm sai oracle v2 45→1 thật sự FAIL tại assertion nghiệp vụ, phục hồi byte chính xác và toàn bộ E2E PASS; cổng Go/web/PG và CI đúng SHA xanh không được thay bằng proposal.
