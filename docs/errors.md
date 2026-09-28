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

**CORS (httpx.CORS, `AG_CORS_ORIGINS`):** preflight (`OPTIONS` + `Access-Control-Request-Method`) từ origin không nằm trong danh sách trả `403` **không có thân và không có mã** (trình duyệt không đọc được thân của preflight). Preflight hợp lệ trả `204`. Request thường từ origin lạ vẫn được xử lý và trả mã như trên nhưng không có header CORS.

## Mã của module

Mã lỗi của module có dạng `<module>.<lỗi>`.

| HTTP | code | Module |
|---|---|---|
| 404 | `party.consent_not_found` | pf/party |
| 409 | `party.consent_id_reused` | pf/party: `id` do client sinh đã dùng cho bản ghi khác. Qua pf/sync (entity `consent`) là mã trong kết quả `rejected`; các lỗi kiểm dữ liệu của Sink đồng ý trả `validation_failed` với `errors[]` (`op`, `payload`, `payload.subject_ref`, `purpose`…), thiếu vai `consent_writer` trả `forbidden` |
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
| 503 | `party.dependency_unavailable` | pf/party: chưa nối hoặc không gọi được masterdata / kho định danh (kể cả `GET /v1/me` của cán bộ TTKN tỉnh khi chưa nối danh mục đơn vị hành chính) |
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
| 404 | `content.document_not_found` | pf/content: không có tài liệu, chưa xuất bản/đã gỡ (bề mặt người đọc), hoặc ngoài phạm vi (không lộ sự tồn tại) |
| 404 | `content.version_not_found` | pf/content: không có phiên bản với số đó |
| 403 | `content.out_of_scope` | pf/content: soạn/duyệt ngoài phạm vi TW → Tỉnh → Xã (cán bộ tỉnh soạn phạm vi toàn quốc hoặc tỉnh khác; người duyệt tỉnh xuất bản tài liệu toàn quốc) |
| 403 | `content.kind_not_allowed` | pf/content: TTKN tỉnh soạn quy trình kỹ thuật chuẩn (KN BR-06 — tỉnh dùng loại `supplement`) |
| 409 | `content.invalid_transition` | pf/content: trạng thái không cho phép (gửi duyệt khi không phải bản nháp mới nhất, xuất bản khi chưa gửi duyệt, soạn tiếp khi đang chờ duyệt, gỡ lần hai) |
| 422 | `content.parent_invalid` | pf/content: bản bổ sung địa phương không gắn quy trình chuẩn quốc gia đã xuất bản |
| 422 | `content.tag_invalid` | pf/content: nhãn phân loại không có hoặc đã ngừng dùng trong danh mục pf/masterdata |
| 422 | `content.audience_invalid` | pf/content: mã đơn vị của phạm vi áp dụng không có, sai cấp, hoặc các xã khác tỉnh |
| 422 | `content.attachment_not_found` | pf/content: phiên bản chọn tệp không thuộc tài liệu |
| 413 | `content.file_too_large` | pf/content: tệp vượt 15 MB |
| 415 | `content.file_type_not_allowed` | pf/content: loại tệp không nhận (đính kèm: PDF, .docx, JPEG/PNG/WebP, MP4; trích: Word/PDF) — theo chữ ký nội dung, không theo tên |
| 422 | `content.checksum_mismatch` | pf/content: SHA-256 khai báo không khớp nội dung tệp |
| 422 | `content.extract_unsupported` | pf/content: chưa trích được định dạng này (PDF, .doc cũ) — soạn thủ công |
| 422 | `content.extract_failed` | pf/content: tệp Word lỗi hoặc không có chữ (ảnh scan) — soạn thủ công (HTX US-CMS-02 AC-2) |
| 503 | `content.dependency_unavailable` | pf/content: chưa nối ScopeResolver (pf/party), danh mục (pf/masterdata) hoặc kho đối tượng |

Người duyệt xuất bản phiên bản do chính mình soạn trả mã chung `self_approval_forbidden` (R-56).
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

| 404 | `mechanization.owner_not_found` | pf/mechanization: không có chủ sở hữu máy, hoặc ngoài phạm vi đọc |
| 404 | `mechanization.machine_not_found` | pf/mechanization: không có máy, hoặc HTX của máy ngoài phạm vi đọc |
| 404 | `mechanization.area_not_found` | pf/mechanization: không có bản ghi diện tích vụ |
| 404 | `mechanization.run_not_found` | pf/mechanization: chưa có kết quả cân đối đã lưu cho vụ/`run_id` (báo cáo không tính lại — chạy `POST /balance-runs`) |
| 409 | `mechanization.serial_taken` | pf/mechanization: Số máy/SN đã có trong tenant (kể cả máy đã vô hiệu hóa); `existing_code` là mã máy đang giữ, lý do nêu HTX (FN-05 BR-01) |
| 409 | `mechanization.chassis_taken` | pf/mechanization: Số khung đã có trong tenant |
| 409 | `mechanization.owner_duplicate` | pf/mechanization: chủ sở hữu trùng định danh (SĐT qua chỉ mục mù của kho định danh), hoặc HTX đã có hồ sơ chủ loại HTX; `existing_code` là Mã CSH đang giữ |
| 409 | `mechanization.owner_has_machines` | pf/mechanization: vô hiệu hóa chủ sở hữu còn máy đang hoạt động mà chưa xác nhận `with_machines`; `active_machines` là số máy (FN-04 BR-05) |
| 409 | `mechanization.state_conflict` | pf/mechanization: trạng thái không cho phép (đã vô hiệu hóa, đang hoạt động, không có thay đổi, đã khóa/chưa khóa, chỉ khóa được bản ghi nhập tay) |
| 422 | `mechanization.cooperative_invalid` | pf/mechanization: HTX không tồn tại, không phải HTX, hoặc đã ngừng hoạt động |
| 403 | `mechanization.out_of_scope` | pf/mechanization: ghi dữ liệu của HTX ngoài phạm vi TW → Tỉnh → Xã của người gọi (ADR-0009) |
| 503 | `mechanization.dependency_unavailable` | pf/mechanization: chưa nối hoặc không gọi được pf/party, pf/masterdata hoặc kho định danh |
| 422 | `mechanization.import_invalid` | pf/mechanization: tệp nhập máy sai mẫu (thiếu cột bắt buộc, có cột "Mã máy"/"Khâu", sai CSV, quá 500 dòng) — từ chối cả tệp |
Phản hồi phát lại theo Idempotency-Key có header `Idempotent-Replayed: true`.

### pf/geo

| HTTP | code | Ý nghĩa |
|---|---|---|
| 404 | `geo.plot_not_found` | pf/geo: không có thửa, hoặc nằm ngoài phạm vi đọc (xã/HTX) — không lộ sự tồn tại |
| 404 | `geo.conflict_not_found` | pf/geo: không có xung đột ranh giới trong phạm vi |
| 404 | `geo.admin_boundary_not_found` | pf/geo: chưa nhập ranh giới hành chính cho mã đơn vị |
| 409 | `geo.overlap_cross_coop` | pf/geo: ranh giới chồng lấn thửa của **HTX khác** vượt ngưỡng `geo.overlap_tolerance_pct` (mặc định 1 %, tính theo thửa nhỏ hơn) — chặn lưu, không bỏ qua được (KN US-PLOT-03); `overlaps` liệt kê `plot_ref`, `code`, `overlap_pct`. Cũng trả khi `/assign` sang HTX làm phát sinh chồng lấn khác HTX |
| 409 | `geo.overlap_needs_confirm` | pf/geo: chồng lấn thửa **cùng HTX** hoặc với thửa nháp chưa gán HTX; `needs_confirm = true`, `overlaps`. Gửi lại với `confirm_overlap: true` để lưu — xung đột `same_coop_overlap` được mở để xem xét (HTX US-PLOT-04 AC-2) |
| 409 | `geo.area_deviation_needs_confirm` | pf/geo: diện tích đo lệch so với `declared_area_ha` quá `geo.area_deviation_max_pct` (mặc định 5 %, HTX US-PLOT-02 AC-3); có `deviation_pct`, `needs_confirm`. Gửi lại với `confirm_overlap: true` |
| 409 | `geo.version_conflict` | pf/geo: `supersedes_id` không phải phiên bản ranh giới hiện hành; problem có `current_boundary_id`, `current_version` |
| 409 | `geo.plot_state_conflict` | pf/geo: thửa đã ngừng dùng, hoặc gán trùng trạng thái hiện tại |
| 409 | `geo.plot_exists` | pf/geo: `plot_ref` do máy sinh đã được dùng |
| 409 | `geo.conflict_state` | pf/geo: xung đột đã được xử lý |
| 403 | `geo.out_of_scope` | pf/geo: ghi ngoài phạm vi xã của người gọi, hoặc quản lý HTX thao tác trên thửa/HTX khác (HTX US-PLOT-01 AC-4) |
| 422 | `geo.too_few_points` | pf/geo: dưới 4 điểm phân biệt, hoặc các điểm thẳng hàng (KN US-PLOT-01 AC-1, HTX US-PLOT-02 AC-2) |
| 422 | `geo.invalid_geometry` | pf/geo: hình không hợp lệ theo PostGIS (tự cắt — hình nơ bướm, vòng sai), không phải vùng khép kín, quá 5.000 đỉnh |
| 422 | `geo.crs_unsupported` | pf/geo: hệ toạ độ khác WGS84 (EPSG:4326) — thành phần `crs` khác, hoặc toạ độ ngoài miền kinh/vĩ độ (VN-2000/UTM) |
| 422 | `geo.area_out_of_range` | pf/geo: diện tích trắc địa ngoài 0,001–1.000 ha |
| 422 | `geo.outside_commune` | pf/geo: phần thửa nằm ngoài ranh giới xã (đã nhập) vượt ngưỡng; có `outside_pct` |
| 422 | `geo.cooperative_invalid` | pf/geo: HTX không tồn tại, đã ngừng hoạt động hoặc ngoài phạm vi (khi đã nối pf/party) |
| 422 | `geo.format_invalid` | pf/geo: tệp nhập không đọc được, không có đối tượng, hoặc quá 200 đối tượng |
| 422 | `geo.format_unsupported` | pf/geo: định dạng chưa hỗ trợ (Shapefile) — chuyển sang GeoJSON hoặc KML |
| 503 | `geo.dependency_unavailable` | pf/geo: chưa nối hoặc không gọi được pf/party (phạm vi), pf/masterdata (đơn vị hành chính, tham số) |

Đồng bộ ngoại tuyến `plot_boundary` (docs/dev/sync-contract.md): không chặn được máy — chồng lấn HTX khác, sửa trên phiên bản cũ, hoặc chồng lấn cùng HTX chưa xác nhận **không** bị từ chối; bản ghi được giữ, trả `status = conflict` kèm `conflict_id`. Chồng lấn cùng HTX có `confirm_overlap: true` → `accepted` (xung đột vẫn mở để xem xét). Lệch diện tích khai báo không chặn. Bản ghi sai trả `rejected` với các mã ở trên.

### pf/notify

| HTTP | code | Ý nghĩa |
|---|---|---|
| 404 | `notify.notification_not_found` | pf/notify: không có thông báo, của người khác, ngoài phạm vi hoặc thông báo chủ đề cũ hơn 90 ngày — không lộ sự tồn tại |
| 403 | `notify.device_unregistered` | pf/notify: `POST /v1/devices/{id}/push-token` với thiết bị chưa đăng ký ở pf/sync (`POST /v1/devices`) hoặc đăng ký bởi tài khoản khác |
| 403 | `notify.device_revoked` | pf/notify: gắn token cho thiết bị đã bị thu hồi — app xóa dữ liệu cục bộ, sinh mã thiết bị mới |
| 403 | `notify.out_of_scope` | pf/notify: `POST /v1/sync/nudge` với `unit` ngoài phạm vi người gọi |
| 404 | `notify.nudge_target_not_found` | pf/notify: `device_id` không có token đẩy đang dùng hoặc ngoài phạm vi (trừ vai `device_admin`, `platform_admin`) |
| 429 | `notify.nudge_rate_limited` | pf/notify: đích đã được yêu cầu đồng bộ trong 5 phút qua, hoặc người gọi vượt 30 lần/giờ; có `retry_after_s` và header `Retry-After` |
| 503 | `notify.dependency_unavailable` | pf/notify: chưa nối sổ thiết bị của pf/sync |
