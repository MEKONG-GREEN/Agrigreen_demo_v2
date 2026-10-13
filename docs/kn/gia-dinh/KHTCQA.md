## KHTCQA — Ma trận nghiệm thu KHTC v2.2, Báo giá v1.3 và dữ liệu đối chứng quy đổi tuần (không migration)

Quyết định 13/10/2026: KHTC tổng hợp mọi phòng ở G1; kỳ nguồn tính vào tuần KHTC chứa ngày cuối; lũy kế nguồn là chuẩn; "đã chốt" = mọi kỳ nguồn quy về tuần đã chốt; giá Mẫu 1 = min/max toàn tỉnh của bảng giá đã duyệt; HTX "Chưa xác nhận" tính vào tổng, tách nhãn. Ví dụ có số: [khtc-week-mapping.md](../../uat/khtc-week-mapping.md) (chờ PO và KHTC ký).

| Câu hỏi / mục | Lựa chọn đã dùng (mặc định, chờ PO) | Ở đâu | Đổi thế nào |
|---|---|---|---|
| Xuất Mẫu 1–5 ở G1 | Chỉ Excel; Word ở G1.1 (Q-062) | khtc-v2.2-acceptance KH-QD-09 | PO chọn Word ở G1 → W11.66 + W10.17 |
| Phát hành lại báo cáo đã gửi khi xã nhận HTX / nguồn đổi | Không phát hành lại; bản đã xuất mang "Nguồn đã thay đổi" | KH-QD-07, golden G04, G06 | Sửa `reissued` trong `cases.json` và dòng KH-QD-07 |
| Ai chạy "Cập nhật số liệu" | Chỉ cán bộ KHTC phụ trách kỳ; giữ lịch sử cũ → mới | KH-QD-08, golden G03 s3 | — |
| Tuần KHTC không có kỳ nguồn (M1) | Coi là đã chốt; lũy kế giữ, trong kỳ 0; Mẫu 1 in "không phát sinh" | golden G02 | Đổi `closed` của G02 và KH-QD-03 |
| Gốc khi tuần KHTC trước đã đóng (M2) | Lũy kế đã đóng của tuần trước (phần sửa sau đóng vào tuần đang mở) | golden G04 | — |
| Chuỗi lũy kế nguồn khác năm KHTC (M3) | Hiển thị lũy kế nguồn; lũy kế năm KHTC = Σ trong kỳ | golden G05B, G05C | — |
| Hai cột ngày Mẫu 1 A.III (M4) | Hai ngày có bảng giá đã duyệt gần nhất ≤ ngày chốt | golden G02B | — |
| Thời điểm tính trạng thái HTX (M6) | Cuối ngày chốt (Thứ Ba) | golden G06 | — |
| Hạn nộp phiếu tuần KHTC (M7) | 10:00 Thứ Tư (BRD II.4); W11.62 đặt riêng, vì mặc định v2 W11.03 là 12:00 | KH-FN-TI-02.03 | — |
| W11.63 "rút gọn" ở G1 | Mẫu phiếu nạp sẵn 4 phòng, 2 loại cột mới, nhập/gửi/yêu cầu bổ sung/xác nhận, Điều chỉnh kỳ trước; "So sánh lần gửi" và thiết kế mẫu mới để sau | khtc-v2.2-acceptance mục G1 | PO chọn khác → sửa cột "Phạm vi G1" |
| Báo giá W11.77 ở G1 | Theo kế hoạch: đợt 2 (sau G1); ma trận đánh dấu rủi ro UR-TW-01, UR-TI-08 (Cao · Đợt 1) | bao-gia-v1.3-acceptance "Rủi ro G1" | Kéo FN-TW-01.01, FN-TI-02.08 về G1 |
