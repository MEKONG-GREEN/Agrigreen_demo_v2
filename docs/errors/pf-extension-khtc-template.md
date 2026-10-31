### pf/extension — Mục lục năm mẫu KHTC (W11.65)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.khtc_template_not_found` | Không có phiên bản/mẫu trong tenant, hoặc không có kỳ phù hợp cho preview. |
| 422 | `extension.khtc_template_invalid` | Cây, khóa mẫu, biến, phạm vi số/độ dài hoặc ngày áp dụng không hợp lệ; có lỗi theo trường. |
| 409 | `extension.khtc_template_version_conflict` | Bản nháp thay đổi sau khi mở màn hoặc phát hành phiên bản không còn mới nhất. |
| 409 | `extension.khtc_template_unpublished` | Không có phiên bản đã phát hành áp dụng theo ngày bắt đầu kỳ; không tự fallback cho thao tác mới. |
| 409 | `extension.khtc_template_text_in_use` | Bỏ vùng lời văn đã có nội dung trên bản soạn, cần xác nhận chỉ đổi mẫu mới. |

`forbidden`, `idempotency_key_required`, `idempotency_key_reused` và `extension.dependency_unavailable` dùng quy tắc chung. Thiếu ngữ cảnh KN không được fallback sang vai realm/legacy. Bản lịch sử không bị đổi khi xuất hiện bản phát hành mới.
