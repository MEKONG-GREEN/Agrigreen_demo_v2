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
| 400 | `vault.purpose_required` | vault/identity: thao tác thiếu mục đích (header `X-Purpose` hoặc trường `purpose`), R-42 |
| 404 | `vault.not_found` | vault/identity: không có chủ thể trong tenant của token, hoặc tra cứu không thấy |
| 409 | `vault.duplicate` | vault/identity: trùng SĐT hoặc CCCD trong tenant; problem có thêm `ref` và `public_key` của chủ thể cũ |
| 410 | `vault.erased` | vault/identity: chủ thể đã bị xóa định danh (RB-3); khóa bí mật đã hủy |
| 422 | `vault.unwrap_failed` | vault/identity: `wrapped_dek` không mở được bằng khóa của chủ thể |
| 502/503 | `vault.unavailable` | client của kho: kho định danh không phản hồi hoặc lỗi hệ thống |
| 404 | `masterdata.admin_unit_not_found` | pf/masterdata: không có đơn vị hành chính với mã đó tại thời điểm tra (hoặc đã bãi bỏ không có kế thừa) |
| 409 | `masterdata.admin_unit_conflict` | pf/masterdata: nhập danh mục có phiên bản `(code, valid_from)` đã có với nội dung khác — chỉ được đóng hiệu lực phiên bản đang mở |
| 409 | `masterdata.admin_unit_overlap` | pf/masterdata: phiên bản mới chồng lấp hiệu lực với phiên bản khác của cùng mã |
| 422 | `masterdata.admin_unit_invalid_reference` | pf/masterdata: đơn vị cha không bao trọn khoảng hiệu lực, hoặc đơn vị kế thừa không có hiệu lực tại `valid_to` |
| 404 | `masterdata.catalog_not_found` | pf/masterdata: tên danh mục không tồn tại |
| 404 | `masterdata.catalog_item_not_found` | pf/masterdata: không có mục danh mục |
| 409 | `masterdata.catalog_item_exists` | pf/masterdata: mã đã có trong danh mục (kể cả mục đã ngừng dùng) |
| 409 | `masterdata.catalog_item_state` | pf/masterdata: ngừng dùng mục đã ngừng, hoặc dùng lại mục đang dùng |
| 422 | `masterdata.farm_stage_invalid` | pf/masterdata: loại máy gắn khâu sản xuất không có hoặc đã ngừng dùng |
| 404 | `masterdata.param_not_found` | pf/masterdata: không có phiên bản tham số ĐÃ PHÊ DUYỆT có hiệu lực tại thời điểm tra |
| 404 | `masterdata.param_version_not_found` | pf/masterdata: không có phiên bản tham số |
| 409 | `masterdata.param_invalid_transition` | pf/masterdata: trạng thái không cho phép (gửi duyệt khi không phải nháp; duyệt/từ chối khi chưa gửi hoặc đã quyết định) |
| 403 | `masterdata.param_not_setter` | pf/masterdata: gửi duyệt phiên bản do người khác đặt |
| 422 | `masterdata.param_correction_required` | pf/masterdata: đã có bản duyệt cùng `effective_from` — phải nêu `correction_reason` (bản ghi bù) |

Tự phê duyệt hoặc tự từ chối phiên bản tham số của chính mình trả mã chung `self_approval_forbidden` (R-56).
| 404 | `party.org_not_found` | pf/party: không có tổ chức/HTX, hoặc nằm ngoài phạm vi đọc (không lộ sự tồn tại) |
| 409 | `party.org_code_taken` | pf/party: mã tổ chức đã dùng trong tenant |
| 409 | `party.tax_code_taken` | pf/party: mã số thuế đã gắn với HTX khác trong tenant; `existing_ref` là HTX đang giữ |
| 422 | `party.invalid_hierarchy` | pf/party: cấp trên không hợp lệ cho loại tổ chức, tạo vòng khi di chuyển, hoặc đơn vị hành chính không thuộc địa bàn cấp trên |
| 409 | `party.org_state_conflict` | pf/party: trạng thái không cho phép (đã vô hiệu hóa, đang hoạt động, HTX đã ngừng, không có thay đổi) |
| 409 | `party.org_has_active_children` | pf/party: vô hiệu hóa tổ chức còn tổ chức con đang hoạt động; `active_children` là số con |
| 403 | `party.out_of_scope` | pf/party: ghi ngoài phạm vi TW → Tỉnh → Xã của người gọi (ADR-0009) |
| 503 | `party.dependency_unavailable` | pf/party: chưa nối hoặc không gọi được masterdata / kho định danh |
| 422 | `party.import_invalid` | pf/party: tệp nhập sai mẫu (thiếu cột bắt buộc, sai định dạng, quá số dòng) — từ chối cả tệp |
| 404 | `extension.task_not_found` | pf/extension: không có nhiệm vụ hỗ trợ, hoặc nằm ngoài phạm vi xã/tỉnh (không lộ sự tồn tại) |
| 409 | `extension.task_state_conflict` | pf/extension: trạng thái nhiệm vụ không cho phép sự việc này (vd tiếp nhận khi chưa được giao, thao tác trên nhiệm vụ đã hoàn thành) |
| 403 | `extension.not_assignee` | pf/extension: tiếp nhận/cập nhật/hoàn thành nhiệm vụ không được giao cho mình |
| 422 | `extension.assignee_invalid` | pf/extension: người được giao chưa có trong danh bạ, đã ngừng, hoặc không phụ trách xã (hay tỉnh) của nhiệm vụ |
| 403 | `extension.out_of_scope` | pf/extension: ghi hoặc xem ngoài phạm vi TW → Tỉnh → Xã của người gọi (ADR-0009); quản lý HTX tạo yêu cầu cho HTX khác |
| 404 | `extension.directive_not_found` | pf/extension: không có chỉ đạo, hoặc người gọi không phải người nhận/giám sát vùng nhận |
| 403 | `extension.not_directive_target` | pf/extension: xác nhận đã đọc chỉ đạo mà mình không thuộc danh sách nhận |
| 422 | `extension.no_directive_targets` | pf/extension: vùng đã chọn chưa có cán bộ nào trong danh bạ — không ban hành chỉ đạo rỗng |
| 404 | `extension.report_not_found` | pf/extension: không có báo cáo hiện trường (hoặc ngoài phạm vi) |
| 409 | `extension.period_closed` | pf/extension: kỳ báo cáo đã chốt ở tỉnh — không nhận báo cáo mới, chỉ bản ghi bù có lý do (KN US-DASH-02 AC-2); kèm `period`, `province_code` |
| 422 | `extension.correction_reason_required` | pf/extension: bản ghi bù (`supersedes_id` / op `correct`) thiếu lý do hoặc lý do dưới 10 ký tự |
| 409 | `extension.report_superseded` | pf/extension: sửa trên phiên bản báo cáo không còn hiện hành |
| 422 | `extension.catalog_code_invalid` | pf/extension: mã giống/vụ/sâu bệnh không có hoặc đã ngừng dùng trong danh mục dùng chung (FR-01 BR-01) |
| 422 | `extension.cooperative_invalid` | pf/extension: HTX không tồn tại, đã ngừng hoạt động hoặc không thuộc xã đã chọn |
| 422 | `extension.unit_invalid` | pf/extension: mã đơn vị hành chính không còn hiệu lực hoặc sai cấp (nhiệm vụ/báo cáo gắn xã; chốt kỳ theo tỉnh) |
| 404 | `extension.staff_not_found` | pf/extension: cán bộ chưa có (hoặc đã ngừng) trong danh bạ trực |
| 409 | `extension.id_reused` | pf/extension: `id` do máy sinh đã dùng cho bản ghi khác |
| 503 | `extension.dependency_unavailable` | pf/extension: chưa nối pf/party (phạm vi) hoặc pf/masterdata (đơn vị, danh mục) |

Đồng bộ ngoại tuyến (`task_update`, `field_report`): lỗi nghiệp vụ trả trong `results[].code` với `status = rejected` theo cùng các mã trên; bản ghi gửi lặp (`idem_key` đã ghi) trả `duplicate`.

Phản hồi phát lại theo Idempotency-Key có header `Idempotent-Replayed: true`.
