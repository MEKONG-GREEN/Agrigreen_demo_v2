### pf/export (`cap/export` — mẫu văn bản, xuất .docx, W10.17, ADR-0051)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `export.template_not_found` | Bên dùng xin khóa mẫu/phiên bản chưa đăng ký |
| 409 | `export.template_drift` | Mẫu đã ghi sổ trong đơn vị nhưng nội dung trong mã đổi mà không tăng phiên bản (phiên bản đã dùng là bất biến) |
| 422 | `export.invalid` | Subject, tên tệp, ký hiệu số không đúng định dạng; thiếu biến bắt buộc của mẫu; tài liệu vượt giới hạn |
| 404 | `export.run_not_found` | Lần xuất không có trong đơn vị hoặc không thuộc đối tượng được hỏi |
| 503 | `export.file_corrupt` | Byte đã lưu không khớp SHA-256 (không trả tệp) |
| 503 | `export.dependency_unavailable` | pf/export chưa nối cơ sở dữ liệu |

Bên dùng KHTC (W10.17) dùng lại `extension.khtc_report_not_found`, `extension.khtc_period_terminal`, `extension.dependency_unavailable`, `forbidden`, `idempotency_key_required`. Màn quản trị `/v1/exports/*`: `forbidden` khi không phải Admin.
