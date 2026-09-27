# Mã lỗi (R-15)

Mọi lỗi trả về `application/problem+json` theo RFC 9457. Mỗi lỗi có:
- `type` = `https://errors.agrigreen.vn/<code>`;
- `code` ổn định;
- `trace_id` (R-63);
- `errors[]` liệt kê lỗi theo trường, khi kiểm dữ liệu vào.

**Mã lỗi là hợp đồng với client:** không đổi nghĩa, không tái sử dụng một mã cho việc khác.

| HTTP | code | Khi nào |
|---|---|---|
| 400 | `bad_request` | Thân không phải JSON, header sai dạng |
| 400 | `idempotency_key_required` | POST thiếu `Idempotency-Key` (R-51) |
| 401 | `unauthenticated` | Thiếu hoặc sai token, sai issuer/audience, hết hạn |
| 403 | `forbidden` | Thiếu vai |
| 403 | `mfa_required` | Bề mặt Admin khi token chưa qua yếu tố thứ hai |
| 403 | `self_approval_forbidden` | Tự phê duyệt thay đổi do chính mình đặt (R-56) |
| 404 | `not_found` | Đường dẫn không tồn tại |
| 405 | `method_not_allowed` | Phương thức HTTP không được hỗ trợ |
| 409 | `idempotency_in_progress` | Yêu cầu cùng khóa đang chạy |
| 409 | `conflict` | Xung đột dữ liệu chung |
| 413 | `payload_too_large` | Vượt giới hạn thân request |
| 422 | `validation_failed` | Dữ liệu không hợp lệ, xem `errors[]` |
| 422 | `idempotency_key_reused` | Cùng khóa, khác nội dung, khác người gọi hoặc khác route |
| 500 | `internal_error` | Lỗi hệ thống. Chi tiết chỉ nằm trong log, tra theo `trace_id` |

## Mã của module

Mã lỗi của module có dạng `<module>.<lỗi>`.

| HTTP | code | Module |
|---|---|---|
| 404 | `party.consent_not_found` | pf/party |
| 409 | `party.consent_id_reused` | pf/party: `id` do client sinh đã dùng cho bản ghi khác |
| 404 | `party.org_not_found` | pf/party: không có tổ chức/HTX, hoặc nằm ngoài phạm vi đọc (không lộ sự tồn tại) |
| 409 | `party.org_code_taken` | pf/party: mã tổ chức đã dùng trong tenant |
| 409 | `party.tax_code_taken` | pf/party: mã số thuế đã gắn với HTX khác trong tenant; `existing_ref` là HTX đang giữ |
| 422 | `party.invalid_hierarchy` | pf/party: cấp trên không hợp lệ cho loại tổ chức, tạo vòng khi di chuyển, hoặc đơn vị hành chính không thuộc địa bàn cấp trên |
| 409 | `party.org_state_conflict` | pf/party: trạng thái không cho phép (đã vô hiệu hóa, đang hoạt động, HTX đã ngừng, không có thay đổi) |
| 409 | `party.org_has_active_children` | pf/party: vô hiệu hóa tổ chức còn tổ chức con đang hoạt động; `active_children` là số con |
| 403 | `party.out_of_scope` | pf/party: ghi ngoài phạm vi TW → Tỉnh → Xã của người gọi (ADR-0009) |
| 503 | `party.dependency_unavailable` | pf/party: chưa nối hoặc không gọi được masterdata / kho định danh |
| 422 | `party.import_invalid` | pf/party: tệp nhập sai mẫu (thiếu cột bắt buộc, sai định dạng, quá số dòng) — từ chối cả tệp |

Phản hồi phát lại theo Idempotency-Key có header `Idempotent-Replayed: true`.
