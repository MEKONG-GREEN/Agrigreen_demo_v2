### pf/extension — Báo cáo KHTC (W11.66)

| Mã | HTTP | Xử lý |
|---|---|---|
| extension.khtc_report_not_found | 404 | Không có kỳ/bản xuất trong tenant; tải lại danh sách. |
| extension.khtc_report_final | 409 | Bản cuối khóa ghi, xuất mới và cập nhật nguồn; vẫn tải bản cũ. |
| extension.khtc_report_version_conflict | 409 | Phiên bản soạn khác base_version, hoặc bản xuất không còn tham chiếu bản soạn hiện tại. |
| extension.khtc_report_provisional | 409 | Xuất cần xác nhận Tạm tính; Tạm tính không được thành Bản cuối. |
| extension.khtc_report_source_changed | 409 | Cập nhật số liệu trước khi đánh dấu Bản cuối; kỳ đóng giữ nguồn đã chốt. |
| extension.khtc_report_invalid | 422 | Chỉ nhập ô hành chính và mục lời văn trong khung Mẫu 1–5. |

Dùng lại `forbidden`, `bad_request`, `idempotency_key_required`, `extension.khtc_refresh_not_owner`, `extension.khtc_period_terminal`, `extension.dependency_unavailable`. Lỗi gốc cơ sở dữ liệu không trả cho client. JSON có trường hệ thống không biết bị từ chối 400.
