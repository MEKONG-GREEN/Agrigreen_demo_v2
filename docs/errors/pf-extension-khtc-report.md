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

#246: history Word trực tiếp và direct Word GET dùng lại `extension.dependency_unavailable` (503) khi run đã lưu chưa có chứng cứ liên kết với đúng snapshot hoặc metadata/băm không khớp. Chi tiết lỗi nêu rõ liên kết chưa được chứng minh; không suy theo tên/seq/kind, không backfill khi đọc. Lỗi INSERT/COMMIT audit trả lỗi hệ thống, không trả metadata/byte và không gọi FilePort. Quyền reader và các mã 401/403 hiện hữu giữ nguyên; 404 áp dụng theo kỳ/binding owner đã kiểm chứng.

Alias direct Word thiếu owner binding, kể cả UUID ngẫu nhiên, trả đúng `503 extension.dependency_unavailable` với chi tiết `Word đã lưu chưa có chứng cứ liên kết với bản xuất; chưa thể xem hoặc tải`. Không có public metadata lookup-by-ID đầy đủ để phân biệt unknown với real-unlinked; không đọc byte chỉ để tự suy `export.run_not_found`. Kỳ không thuộc tenant hoặc binding thực thuộc kỳ khác vẫn 404 từ owner. Native negative phải kiểm cùng request có 0 exporter.Run/FilePort, không file byte, đúng code/status/detail; đây là phân loại fail-closed tường minh, không waiver lỗi chung.
