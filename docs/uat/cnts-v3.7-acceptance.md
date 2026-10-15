# Nghiệm thu CNTS v3.7 — W11.43

Phạm vi G1: cảnh báo liên biểu, vòng đời xử lý và nhắc nộp biểu CNTS.
Nguồn và các giới hạn: [giả định W11.43](../kn/gia-dinh/W11.43.md).
Các kiểm thử tích hợp dùng PostgreSQL thật; nguồn bài nộp/danh bạ trong kiểm thử
cảnh báo là cổng giả. `TestFormsSubmissionFeed_W1143` kiểm chứng cổng nguồn thật
riêng, không thay thế diễn tập worker và gửi email trên môi trường triển khai.

| Tiêu chí | Bằng chứng tự động | Phạm vi |
|---|---|---|
| Biến động tổng đàn/khai thác vượt ngưỡng, KSGM bất thường hoặc bằng 0, lệch CN-06 và BM-01 | `TestCNTSAlerts_W1143`, `TestChangeFindingsHerd`, `TestSlaughterSpike`, `TestSlaughterZeroAndMismatch` | G1 |
| Ổ dịch mới sinh cảnh báo Khẩn; mỗi đối tượng × loại × kỳ chỉ một cảnh báo | `TestCNTSAlerts_W1143` | G1 |
| CNTS-SCOPE-001: cảnh báo không gắn xã vẫn chỉ hiện trong tỉnh phụ trách, phạm vi rỗng bị chặn | `TestProvinceAlertScope_W1143` | G1 |
| Giao, nhận, đóng có kết quả; kiểm tra phiên bản và phạm vi xã/phòng/vai | `TestCNTSAlerts_W1143`, `TestTransition` | G1 |
| Hết vi phạm tự đóng; quá hạn chỉ báo một lần theo hạn | `TestCNTSAlerts_W1143` | G1 |
| Nhắc trước hạn, khi trễ, nhắc lặp; cấu hình theo biểu và cảnh báo trễ liên tiếp | `TestCNTSAlerts_W1143`, `TestFormReminderConfig` | G1 |
| Sự kiện nộp biểu và đọc nguồn theo tenant | `TestFormsSubmissionFeed_W1143` | G1 |
| Người nhận không trùng; thông báo chủ đề khi thiếu người nhận; kênh email cho cảnh báo Khẩn/nhắc được cấu hình | `TestCNTSNotifications_W1143` | G1 |
| Danh sách và thao tác cảnh báo, cấu hình nhắc; menu tỉnh/xã | Vitest `src/kn/cnts/alert/alert.test.ts`; `TestWebMenuFixture`; `kn-menu.contract.test.ts` | G1 |
| Cảnh báo chất lượng nước, tăng trưởng, tỷ lệ chết, vị trí, nhật ký trễ, lịch tiêm trễ (dòng 6–11) | `TestLogRules` chỉ kiểm hàm đánh giá | Chờ nguồn W11.48/CN6 sau G1 |

Chưa nghiệm thu: widget “Việc cần làm” cấp Xã, người phụ trách riêng theo đợt
phát hành, bản đồ/biểu đồ cảnh báo và nút báo nghi dịch từ tỷ lệ chết.
Nhịp worker/sự kiện cần được kiểm trên môi trường triển khai để xác nhận SLA
một phút; kiểm thử hàm xử lý không tự chứng minh thời gian giao nhận thực tế.

Kết quả tiếp nhận ngày 15/10/2026 được ghi ở [sổ hồi quy](../REGRESSION.md).
