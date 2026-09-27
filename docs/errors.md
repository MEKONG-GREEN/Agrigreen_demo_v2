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
| 403 | `sync.device_unregistered` | pf/sync: thiếu/sai header `X-Device-Id`, thiết bị chưa đăng ký trong tenant, hoặc đăng ký bởi tài khoản khác |
| 403 | `sync.device_revoked` | pf/sync: thiết bị đã bị thu hồi — app **xóa dữ liệu cục bộ** và khóa token; đăng ký lại cùng mã cũng trả mã này |
| 409 | `sync.device_taken` | pf/sync: `device_id` đã được tài khoản khác đăng ký |
| 404 | `sync.device_not_found` | pf/sync (Admin): không có thiết bị trong tenant |
| 409 | `sync.device_already_revoked` | pf/sync (Admin): thiết bị đã bị thu hồi trước đó |
| 400 | `sync.cursor_invalid` | pf/sync: con trỏ pull bị sửa (HMAC sai), sai định dạng, hoặc của thiết bị/tenant khác — pull lại không có `cursor` |
| — | `sync.unknown_entity` | pf/sync push: mã trong kết quả `rejected` của một bản ghi — entity không có module nhận |
| — | `sync.sink_failed` | pf/sync push: mã trong kết quả `rejected` — module nhận lỗi hệ thống; máy **giữ và tự gửi lại**, không bắt người dùng tạo bản ghi bù |
| 404 | `sync.conflict_not_found` | pf/sync (Admin): không có xung đột |
| 409 | `sync.conflict_state` | pf/sync (Admin): xung đột đã được đóng |
| 422 | `sync.file_checksum_mismatch` | pf/sync: SHA-256 của `blob` khác `meta.sha256_ciphertext` |
| 409 | `sync.file_ref_taken` | pf/sync: `file_ref` đã dùng cho tệp có bản mã khác |
| 404 | `sync.file_not_found` | pf/sync: không có tệp minh chứng trong tenant |
| 400 | `sync.purpose_required` | pf/sync: đọc tệp minh chứng thiếu header `X-Purpose` (R-42) |
| 403 | `sync.purpose_not_allowed` | pf/sync: mục đích không phải `mrv.evidence` hoặc `extension.field_support` |
| 403 | `sync.consent_missing` | pf/sync: chủ thể chưa đồng ý (hoặc đã rút) cho mục đích đọc tệp |
| 503 | `sync.dependency_unavailable` | pf/sync: chưa cấu hình kho đối tượng hoặc kho định danh |
| 422 | `mrv.consent_missing` | pf/mrv: minh chứng gắn `household_ref` nhưng nông hộ chưa đồng ý mục đích `mrv.evidence` (mục 10.5). Đồng bộ ngoại tuyến trả `rejected` với cùng mã |
| 403 | `mrv.out_of_scope` | pf/mrv: thu minh chứng hoặc xuất gói cho đơn vị ngoài phạm vi TW → Tỉnh → Xã của người gọi (ADR-0009) |
| 422 | `mrv.catalog_version_unavailable` | pf/mrv: loại minh chứng chưa có phiên bản danh mục ĐÃ DUYỆT, hoặc `catalog_version` ghim tới phiên bản không tồn tại/chưa duyệt |
| 404 | `mrv.catalog_version_not_found` | pf/mrv: không có phiên bản danh mục (Admin API), hoặc chưa có bản đã duyệt (Application API) |
| 409 | `mrv.catalog_invalid_transition` | pf/mrv: trạng thái phiên bản danh mục không cho phép (gửi duyệt khi không phải nháp; duyệt/từ chối khi chưa gửi hoặc đã quyết định) |
| 403 | `mrv.catalog_not_setter` | pf/mrv: gửi duyệt phiên bản danh mục do người khác đặt |
| 404 | `mrv.evidence_not_found` | pf/mrv: không có minh chứng, hoặc nằm ngoài phạm vi đọc (không lộ sự tồn tại); `supersedes_id` không tồn tại |
| 409 | `mrv.evidence_id_reused` | pf/mrv: `id` do máy sinh đã dùng cho bản ghi khác |
| 409 | `mrv.evidence_superseded` | pf/mrv: sửa sai hoặc duyệt một phiên bản đã có bản ghi bù mới hơn — thao tác trên bản hiện hành |
| 409 | `mrv.review_exists` | pf/mrv: phiên bản minh chứng đã có quyết định duyệt; sửa sai tạo phiên bản mới chờ duyệt lại |
| 403 | `mrv.reviewer_is_capturer` | pf/mrv: người duyệt đã thu (hoặc sửa) một phiên bản của chính chuỗi minh chứng này |
| 404 | `mrv.export_not_found` | pf/mrv: không có yêu cầu xuất, hoặc bộ lọc của job vượt phạm vi người gọi |
| 409 | `mrv.export_not_ready` | pf/mrv: gói chưa dựng xong (`queued`) hoặc job hỏng (`failed`) |
| 503 | `mrv.dependency_unavailable` | pf/mrv: chưa nối phạm vi, kiểm đồng ý hoặc kho đối tượng |

Tự phê duyệt phiên bản danh mục trường MRV của chính mình trả mã chung `self_approval_forbidden` (R-56).

Phản hồi phát lại theo Idempotency-Key có header `Idempotent-Replayed: true`.
