### pf/extension (`dom/extension` — Báo cáo phòng CNTS BM-11, W11.45)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.cnts_bm11_not_found` | Không có báo cáo với mã này, hoặc người gọi không được xem (tỉnh khác; phòng nhận với báo cáo chưa gửi hoặc không gửi tới phòng mình) |
| 409 | `extension.cnts_bm11_exists` | Tạo báo cáo cho tỉnh × loại × kỳ đã có báo cáo (một báo cáo mỗi kỳ; DB: `cnts_bm11_report_period`); `detail` = mã báo cáo đã có |
| 409 | `extension.cnts_bm11_not_ready` | Quy tắc 13 / BR-06: chưa tới hạn nộp của tuần cuối của kỳ, hoặc tỷ lệ xã đã nộp của biểu nguồn (BM-04, BM-17) dưới mức `extension.cnts_bm11_min_submit_pct` và kỳ chưa chốt, hoặc kỳ chưa có kỳ nguồn nào; `detail` liệt kê lý do |
| 409 | `extension.cnts_bm11_state` | Sửa, làm mới, trình khi báo cáo không Đang soạn/Trả lại; duyệt, trả lại khi không Chờ duyệt; xác nhận Đã nhận khi chưa gửi hoặc nơi nhận không thuộc báo cáo (DB: `cnts_bm11_state`, `cnts_bm11_received`) |
| 409 | `extension.cnts_bm11_version_conflict` | `expected_seq` khác bước hiện hành của báo cáo (khóa lạc quan — người khác vừa cập nhật) |

Ngoài ra dùng mã chung: `forbidden` (không phải Phòng CNTS/Admin/phòng nhận; cán bộ duyệt/trả lại; sửa đoạn chương trình không phải của mình) và `validation_failed` (loại/kỳ sai dạng, nơi nhận lạ, mục không có trong mẫu, đoạn viết quá 8.000 ký tự, trả lại thiếu ý kiến — AC-04, thiếu `expected_seq`).
