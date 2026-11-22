### pf/guestlink — capability OTP dormant

| Code | HTTP | Ý nghĩa |
|---|---|---|
| `guestlink.denied` | 403 | Token, phạm vi, OTP, quyền cấp, khóa/thời hạn hoặc quota không cho phép; không trả lý do phân biệt chủ thể ra public. |
| `guestlink.unavailable` | 503 | Capability tắt, thiếu owner/transport, transport Disabled/Dropped hoặc DB lỗi; không giả Sent/thành công. |
| `guestlink.invalid` | 422 | Cấu hình khởi tạo ngoài biên an toàn hoặc đăng ký owner không hợp lệ; không chứa key/contact. |

Chưa có HTTP adapter mới trong #184. Contract public/timing/cookie và owner HRM thuộc #200; endpoint HCTH hiện hành giữ lỗi fail-closed riêng.
