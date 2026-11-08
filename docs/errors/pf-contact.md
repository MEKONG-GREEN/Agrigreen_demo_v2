### pf/contact (`cap/directory` — Contact FN-13, W11.23)

| HTTP | code | Khi nào |
|---|---|---|
| 403 | `contact.detail_forbidden` | Cấp TW, Cấp Xã (bộ trường "basic") mở chi tiết liên hệ — chỉ xem họ tên, năm sinh, SĐT trong danh sách (BR-03) |
| 403 | `contact.manage_forbidden` | Thêm/sửa/ngừng hiển thị liên hệ ngoài tỉnh hoặc thẻ Đơn vị khi không phải Admin hay Cấp Tỉnh ở phòng HCTH (hoặc ngữ cảnh chỉ xem) |
| 403 | `contact.staff_disabled` | (nội bộ, không trả ra API) nguồn cán bộ thuộc tỉnh tắt theo cổng `AG_HRM_RECORDS` — danh bạ chỉ có liên hệ ngoài tỉnh, `staff_source = disabled` |
| 404 | `contact.not_found` | Không có liên hệ/thẻ Đơn vị với mã này trong tenant (hoặc đã ngừng hiển thị và người gọi không cập nhật được) |
| 409 | `contact.duplicate_person` | SĐT đã thuộc một chủ thể của tenant (cán bộ thuộc tỉnh hoặc liên hệ khác) — mỗi người một bản ghi (BR-01) |
| 422 | `contact.staff_read_only` | Sửa liên hệ của cán bộ thuộc tỉnh ở Contact — sửa tại hồ sơ nhân sự (FN-07) |
| 503 | `contact.vault_unavailable` | Kho định danh được nối không hỗ trợ thẻ danh bạ / ngăn hồ sơ (lỗi cấu hình) |

### vault/identity (thẻ danh bạ, W11.23)

| HTTP | code | Khi nào |
|---|---|---|
| 503 | `vault.directory_unsupported` | Hiện thực kho (client local) không có phần mở rộng `client.Directory` — lỗi cấu hình |

### Hỗ trợ tài khoản KN — Q10/FN-CH-00.01 (#274)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `contact.support_not_found` | Yêu cầu không có trong tenant hiện tại |
| 409 | `contact.support_stale` | Phiên bản thay đổi hoặc yêu cầu đã xử lý; tải lại trước thao tác mới |
| 413 | `contact.support_too_large` | Thân vượt 12 KiB, kể cả không khai Content-Length |
| 415 | `contact.support_json_required` | Sai Content-Type hoặc gửi tệp |
| 503 | `contact.support_unavailable` | Chưa cấu hình tenant/chủ thể/khóa công khai hoặc chưa nối vault khi đọc nội dung |

Lỗi vault/audit/commit không trả plaintext hoặc success giả. Mọi response intake/queue/detail/resolution `no-store`.

Các lỗi quyền/MFA, thiếu context, validation/idempotency và giới hạn IP dùng [mã chung](../errors.md); không khai lại mã.
