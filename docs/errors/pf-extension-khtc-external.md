### pf/extension — Số liệu nguồn ngoài KHTC (W11.67)

| Mã | HTTP | Xử lý |
|---|---|---|
| extension.khtc_ext_not_found | 404 | Không có kỳ, chỉ tiêu, số liệu, tệp hoặc lần nạp trong tenant; tải lại danh sách. |
| extension.khtc_ext_period_closed | 409 | Kỳ đã đóng hoặc đã hủy: số liệu nguồn ngoài chỉ xem (BR-DL-19). |
| extension.khtc_ext_duplicate | 409 | Chỉ tiêu đã có giá trị trong kỳ: gửi lại với `overwrite: true` (hoặc `base_version`) và lý do ≥ 10 ký tự. |
| extension.khtc_ext_conflict | 409 | `base_version` khác phiên bản hiện tại hoặc người khác vừa ghi; tải lại rồi sửa. |
| extension.khtc_ext_indicator_duplicate | 409 | Mã hoặc tên chỉ tiêu (không phân biệt hoa thường, khoảng trắng) đã có. |
| extension.khtc_ext_invalid | 422 | Thiếu nguồn (cơ quan, văn bản hoặc kênh, ngày), giá trị không phải số (tối đa 4 chữ số lẻ), ngày nguồn sau hôm nay, thiếu lý do sửa; chi tiết ở `errors[]`. |
| extension.khtc_ext_import_invalid | 422 | Không đọc được tệp nạp, thiếu cột hoặc không có dòng; dùng "Tải mẫu Excel". Lỗi từng dòng nằm trong kết quả bước 1, không phải mã lỗi. |
| extension.khtc_ext_import_committed | 409 | Lần nạp đã "Hoàn tất"; nạp tệp mới nếu cần. |
| extension.khtc_ext_evidence_invalid | 422 | Tệp minh chứng rỗng, quá 15 MB hoặc nội dung không khớp phần mở rộng (PDF, ảnh JPEG/PNG, Word, Excel, .eml, .txt). |

Dùng lại `forbidden`, `bad_request`, `idempotency_key_required`, `idempotency_key_reused`, `payload_too_large`, `extension.khtc_report_final` (báo cáo đã có Bản cuối khóa nguồn của kỳ), `extension.dependency_unavailable` (chưa nối kho đối tượng cho tệp minh chứng).
