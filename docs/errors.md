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
| 422 | `masterdata.param_threshold_order` | pf/masterdata: mốc ngưỡng CGH (QT-02) lệch thứ tự so với hai mốc đã duyệt tại ngày hiệu lực — phải Cần chú ý < Đủ < Thừa (kiểm khi đề xuất và khi duyệt) |
| 422 | `masterdata.param_correction_required` | pf/masterdata: đã có bản duyệt cùng `effective_from` — phải nêu `correction_reason` (bản ghi bù) |
| 422 | `masterdata.param_percent_thresholds` | pf/masterdata: bộ ngưỡng % của quy chuẩn hiển thị GIS (`geo.hub_capacity_warn_pct` < `geo.hub_capacity_full_pct`) lệch thứ tự hoặc ngoài 0–100 % — "Ngưỡng % phải tăng dần và nằm trong khoảng 0–100%." (GIS US-CFG-01 AC-2; kiểm khi đề xuất và khi duyệt). Mã màu sai định dạng trả `validation_failed` trường `text`: "Mã màu không đúng định dạng HEX (ví dụ: #1E90FF)." |
| 404 | `masterdata.param_no_default` | pf/masterdata: khóa không có giá trị mặc định nhúng để "Khôi phục mặc định" (GIS US-CFG-02 AC-2) |
| 422 | `masterdata.catalog_import_invalid` | pf/masterdata: tệp nhập danh mục (HTX US-CAT-02; CSV UTF-8 hoặc .xlsx) sai mẫu — thiếu cột `catalog`/`code`/`name`, ô công thức, quá 500 dòng hoặc 2 MB — từ chối cả tệp (dòng lỗi riêng lẻ nằm trong `rows[].reason`, không phải lỗi HTTP) |
| 404 | `masterdata.catalog_import_not_found` | pf/masterdata: không có lần nhập danh mục với mã đó trong tenant |

Tự phê duyệt hoặc tự từ chối phiên bản tham số của chính mình trả mã chung `self_approval_forbidden` (R-56).
| 404 | `party.org_not_found` | pf/party: không có tổ chức/HTX, hoặc nằm ngoài phạm vi đọc (không lộ sự tồn tại) |
| 409 | `party.org_code_taken` | pf/party: mã tổ chức đã dùng trong tenant |
| 409 | `party.tax_code_taken` | pf/party: mã số thuế đã gắn với HTX khác trong tenant; `existing_ref` là HTX đang giữ |
| 422 | `party.invalid_hierarchy` | pf/party: cấp trên không hợp lệ cho loại tổ chức, tạo vòng khi di chuyển, hoặc đơn vị hành chính không thuộc địa bàn cấp trên |
| 409 | `party.org_state_conflict` | pf/party: trạng thái không cho phép (đã vô hiệu hóa, đang hoạt động, HTX đã ngừng, không có thay đổi) |
| 409 | `party.org_has_active_children` | pf/party: vô hiệu hóa tổ chức còn tổ chức con đang hoạt động; `active_children` là số con |
| 403 | `party.out_of_scope` | pf/party: ghi ngoài phạm vi TW → Tỉnh → Xã của người gọi (ADR-0009) |
| 503 | `party.dependency_unavailable` | pf/party: chưa nối hoặc không gọi được masterdata / kho định danh (kể cả `GET /v1/me` của cán bộ TTKN tỉnh khi chưa nối danh mục đơn vị hành chính); `GET /v1/cooperatives/{ref}/deactivation-preview` không đọc được vụ/nhật ký của pf/cropcycle (HTX US-HTXSTATUS-01 AC-1) |
| 422 | `party.import_invalid` | pf/party: tệp nhập (nông hộ, cây tổ chức; CSV UTF-8 hoặc .xlsx) sai mẫu — thiếu cột bắt buộc, sai định dạng, không phải UTF-8, tệp .xls cũ, ô công thức (không tính công thức; `detail` nêu ô), quá số dòng/dung lượng — từ chối cả tệp |
| 409 | `party.cooperative_name_taken` | pf/party: "Tên HTX này đã tồn tại, vui lòng chọn tên khác." — sao chép hồ sơ HTX (`POST /v1/cooperatives/{ref}/copy`, HTX US-HTX-03 AC-3) hoặc dòng nhập HTX (`POST /v1/cooperatives/import`, lý do theo dòng) trùng tên HTX đã có (không phân biệt hoa thường); `existing_ref` là HTX đang giữ tên |
| 409 | `party.household_phone_taken` | pf/party: sao chép nông hộ với SĐT/CCCD đã có trong kho định danh — trong `POST /v1/households/copy` thành lý do dòng "SĐT đã tồn tại" (HTX US-HH-02 AC-4) |
| 404 | `party.household_contact_unavailable` | pf/party: `GET /v1/households/{ref}/contact` — chủ hộ chưa có SĐT hợp lệ hoặc đã xóa định danh (HTX US-HH-03) |
| 404 | `party.household_not_found` | pf/party: `POST /v1/households/{ref}/membership` — nông hộ không có hoặc ngoài phạm vi (không lộ sự tồn tại) |
| 409 | `party.membership_unchanged` | pf/party: nông hộ đã ở đúng trạng thái thành viên HTX yêu cầu |
| 422 | `party.membership_reason_required` | pf/party: đổi thành viên HTX phải nêu lý do 10–500 ký tự |
| 404 | `party.share_grant_not_found` | pf/party (ADR-0020): không có cấp quyền chia sẻ trong tenant bên cấp của người gọi (bên nhận không xem được sổ của bên cấp) |
| 409 | `party.share_grant_state` | pf/party (ADR-0020): chỉ duyệt được bản `proposed` còn hạn; bản đã thu hồi không thao tác được nữa |
| 403 | `party.share_grant_missing` | pf/party (ADR-0020): bề mặt đối tác `/v1/partner/*` — tenant người gọi không có cấp quyền chia sẻ ĐANG HIỆU LỰC (đã duyệt, trong hạn, chưa thu hồi) có loại dữ liệu này, hoặc `grantor` không cấp cho tenant người gọi. Cũng trả cho `GET /v1/cropcycle/htx-collection/responses` (bí danh) |
| 422 | `party.share_grantor_ambiguous` | pf/party (ADR-0020): nhiều tenant bên cấp cùng cấp loại dữ liệu này cho tenant người gọi — truyền tham số `grantor` |
| 404 | `party.partner_not_found` | pf/party (ADR-0020): `GET /v1/partner/cooperatives/{ref}` — bên cấp không có HTX này (lần đọc vẫn được ghi sổ) |
| 503 | `party.partner_audit_unavailable` | pf/party (ADR-0020): không ghi được sổ đọc `pf_party.partner_read_log` — bề mặt đối tác không trả dữ liệu chưa ghi sổ |
| — | `party.valve_share_grant_missing` | pf/party (ADR-0020), lỗi consumer (không phải HTTP): phong bì van MKG (`hub_facility`, `hub_capacity`, `collection_schedule`, `htx_collection_statement`, `mrv_collection_evidence`) mang tenant chưa được bên cấp nào cấp loại `valve_*` tương ứng → consumer `pf.geo.hub_valve` / `pf.cropcycle.htx_collection_valve` / `pf.mrv.collection_evidence` trả lỗi, giao lại rồi DLQ (R-62). Duyệt cấp quyền rồi chuyển lại từ DLQ (xử lý idempotent) |
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
| 422 | `sync.conflict_resolution_invalid` | pf/sync (Admin, US-SYNC-03): `resolution = keep_version` mà `resolved_record_id` không phải bản ghi của xung đột, hoặc `correction` trỏ lại bản ghi của xung đột |
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
| 404 | `mrv.season_not_found` | pf/mrv G3: vụ chưa được liên kết (chưa nhận `pf.cropcycle.season.*`) hoặc nằm ngoài phạm vi đọc (không lộ sự tồn tại) |
| 404 | `mrv.farm_log_not_found` | pf/mrv G3: nhật ký HTX (`log_ref`) chưa được liên kết vào vụ này |
| 409 | `mrv.comparison_exists` | pf/mrv G3 (KN US-MRV-02): cặp nhật ký ↔ minh chứng đã có kết quả đối chiếu — sửa bằng bản ghi bù (`supersedes_id` + `correction_reason`) |
| 409 | `mrv.comparison_superseded` | pf/mrv G3: `supersedes_id` trỏ tới kết quả đối chiếu đã có bản ghi bù mới hơn |
| 404 | `mrv.comparison_not_found` | pf/mrv G3: `supersedes_id` không tồn tại hoặc không thuộc cặp nhật ký ↔ minh chứng này |
| 404 | `mrv.method_not_found` | pf/mrv G3 (Admin API): không có phiên bản phương pháp ước tính |
| 409 | `mrv.method_invalid_transition` | pf/mrv G3: phiên bản phương pháp đã duyệt/từ chối (bất biến) |

Tự phê duyệt phiên bản danh mục trường MRV của chính mình trả mã chung `self_approval_forbidden` (R-56).
| 404 | `content.document_not_found` | pf/content: không có tài liệu, chưa xuất bản/đã gỡ (bề mặt người đọc), hoặc ngoài phạm vi (không lộ sự tồn tại) |
| 404 | `content.library_item_not_found` | pf/content (US-LIB-05): bỏ lưu tài liệu chưa có trong danh mục cá nhân |
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
| 422 | `content.extract_unsupported` | pf/content: không trích được chữ — PDF ảnh scan (không có lớp chữ, không OCR), PDF mã hóa, phông PDF không giải mã được, tệp .doc cũ; lý do ở `detail` — soạn thủ công |
| 422 | `content.extract_failed` | pf/content: tệp Word/PDF hỏng, không đọc được, hoặc Word không có chữ — soạn thủ công (HTX US-CMS-02 AC-2) |
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
| 404 | `extension.contact_unavailable` | pf/extension: cán bộ trong danh bạ chưa khai số điện thoại ở tài khoản (hoặc tài khoản đã khóa) — không gọi/nhắn 1 chạm được (US-DIR-02) |
| 403 | `extension.farmer_household_required` | pf/extension: tài khoản nông dân chưa liên kết nông hộ nào (HTX US-SUPPORT-01) |
| 422 | `extension.household_invalid` | pf/extension: `household_ref` của yêu cầu hỗ trợ không thuộc tài khoản, hoặc tài khoản có nhiều nông hộ mà không chọn |
| 404 | `extension.support_request_not_found` | pf/extension: không có yêu cầu hỗ trợ |
| 409 | `extension.id_reused` | pf/extension: `id` do máy sinh đã dùng cho bản ghi khác |
| 503 | `extension.dependency_unavailable` | pf/extension: chưa nối pf/party (phạm vi) hoặc pf/masterdata (đơn vị, danh mục); consumer `pf.extension.gps_log` chưa nối log GPS pf/geo (sự kiện được giao lại) |
| 404 | `extension.program_report_not_found` | pf/extension: không có báo cáo tiến độ Đề án (FR-03), hoặc ngoài phạm vi đọc |
| 409 | `extension.program_report_approved` | pf/extension: phiên bản tiến độ Đề án đã được duyệt |

Đồng bộ ngoại tuyến (`task_update`, `field_report`): lỗi nghiệp vụ trả trong `results[].code` với `status = rejected` theo cùng các mã trên; bản ghi gửi lặp (`idem_key` đã ghi) trả `duplicate`.

| 404 | `mechanization.owner_not_found` | pf/mechanization: không có chủ sở hữu máy, hoặc ngoài phạm vi đọc |
| 404 | `mechanization.machine_not_found` | pf/mechanization: không có máy, hoặc HTX của máy ngoài phạm vi đọc |
| 404 | `mechanization.area_not_found` | pf/mechanization: không có bản ghi diện tích vụ |
| 404 | `mechanization.run_not_found` | pf/mechanization: chưa có kết quả cân đối đã lưu cho vụ/`run_id` (báo cáo không tính lại — chạy `POST /balance-runs`) |
| 404 | `mechanization.forecast_not_found` | pf/mechanization (FN-09): không có lần dự báo đã lưu với `id` này (chạy `POST /v1/mechanization/forecasts` trên Admin API) |
| 409 | `mechanization.serial_taken` | pf/mechanization: Số máy/SN đã có trong tenant (kể cả máy đã vô hiệu hóa); `existing_code` là mã máy đang giữ, lý do nêu HTX (FN-05 BR-01) |
| 409 | `mechanization.chassis_taken` | pf/mechanization: Số khung đã có trong tenant |
| 409 | `mechanization.owner_duplicate` | pf/mechanization: chủ sở hữu trùng định danh (SĐT qua chỉ mục mù của kho định danh), hoặc HTX đã có hồ sơ chủ loại HTX; `existing_code` là Mã CSH đang giữ |
| 409 | `mechanization.owner_has_machines` | pf/mechanization: vô hiệu hóa chủ sở hữu còn máy đang hoạt động mà chưa xác nhận `with_machines`; `active_machines` là số máy (FN-04 BR-05) |
| 409 | `mechanization.state_conflict` | pf/mechanization: trạng thái không cho phép (đã vô hiệu hóa, đang hoạt động, không có thay đổi, đã khóa/chưa khóa, chỉ khóa được bản ghi nhập tay) |
| 422 | `mechanization.cooperative_invalid` | pf/mechanization: HTX không tồn tại, không phải HTX, hoặc đã ngừng hoạt động |
| 403 | `mechanization.out_of_scope` | pf/mechanization: ghi dữ liệu của HTX ngoài phạm vi TW → Tỉnh → Xã của người gọi (ADR-0009) |
| 503 | `mechanization.dependency_unavailable` | pf/mechanization: chưa nối hoặc không gọi được pf/party, pf/masterdata hoặc kho định danh (kể cả consumer `pf.mechanization.area_sync`, CGH FN-07 — sự kiện được giao lại rồi vào DLQ) |
| 422 | `mechanization.import_invalid` | pf/mechanization: tệp nhập máy sai mẫu (thiếu cột bắt buộc, có cột "Mã máy"/"Khâu", sai CSV/.xlsx, ô công thức, quá 500 dòng) — từ chối cả tệp |
Phản hồi phát lại theo Idempotency-Key có header `Idempotent-Replayed: true`.

### pf/reports

| HTTP | code | Ý nghĩa |
|---|---|---|
| 404 | `reports.template_not_found` | pf/reports: không có mẫu báo cáo với mã này (cả mẫu hệ thống lẫn phiên bản của tenant) |
| 409 | `reports.template_retired` | pf/reports: mẫu đã ngừng dùng — không xuất, không lập lịch, không ngừng lại lần nữa |
| 422 | `reports.format_unsupported` | pf/reports: mẫu không hỗ trợ định dạng đã chọn (`xlsx`/`pdf`) |
| 404 | `reports.export_not_found` | pf/reports: không có việc xuất, hoặc người gọi không phải người yêu cầu và phạm vi không bao trọn |
| 409 | `reports.export_not_ready` | pf/reports: việc xuất chưa xong hoặc đã hỏng — chưa có tệp để tải |
| 429 | `reports.export_quota_exceeded` | pf/reports: vượt hạn mức xuất trong ngày (giờ Việt Nam) của người gọi — tham số `reports.daily_export_quota`, mặc định 50 (mục 18 DLP) |
| 403 | `reports.out_of_scope` | pf/reports: đơn vị ngoài phạm vi TW → Tỉnh → Xã của người gọi, hoặc tài khoản chưa gắn đơn vị (ADR-0009); mẫu `htx.production`: quản lý HTX lọc/lập lịch HTX khác hoặc tài khoản chưa gắn HTX (HTX US-DASH-02/03) |
| 422 | `reports.recipient_not_in_cooperative` | pf/reports: lịch của quản lý HTX (`POST /v1/reports/schedules` Application API) có người nhận không thuộc `GET /v1/reports/recipients` (tài khoản đang bật, email đã xác minh của HTX); title "Email đăng ký nhận báo cáo không hợp lệ hoặc gửi thất bại, vui lòng kiểm tra lại." (HTX US-DASH-03 AC-2) |
| 404 | `reports.schedule_not_found` | pf/reports: không có lịch gửi, hoặc ngoài phạm vi xem |
| 422 | `reports.unit_invalid` | pf/reports: mã đơn vị hành chính không còn hiệu lực hoặc không phải tỉnh/xã |
| 503 | `reports.dependency_unavailable` | pf/reports: chưa nối pf/party, pf/masterdata, kho đối tượng hoặc cổng dữ liệu của module nguồn |

Việc xuất hỏng vĩnh viễn ghi `error_code` trên việc xuất (vd `mechanization.run_not_found` khi vụ chưa có kết quả cân đối đã lưu, `reports.object_conflict` khi đã có tệp khác nội dung dưới cùng khóa).

### pf/simulation

| HTTP | code | Ý nghĩa |
|---|---|---|
| 404 | `simulation.hub_not_found` | pf/simulation: không có Hub ứng viên (trong tenant) |
| 409 | `simulation.hub_retired` | pf/simulation: Hub ứng viên đã ngừng dùng — không gán vào kịch bản được |
| 404 | `simulation.scenario_not_found` | pf/simulation: không có kịch bản (trong tenant) |
| 404 | `simulation.hub_not_attached` | pf/simulation: Hub chưa gán (hoặc đã gỡ) khỏi kịch bản |
| 422 | `simulation.scenario_without_hubs` | pf/simulation: kịch bản chưa có Hub nào — không chạy mô phỏng |
| 422 | `simulation.baseline_scenario_invalid` | pf/simulation: kịch bản baseline (baseline_mode = scenario) không tồn tại |
| 404 | `simulation.simulation_not_found` | pf/simulation: không có việc mô phỏng / ảnh chụp (cũng cho /v1/jobs/{id}) |
| 409 | `simulation.result_not_ready` | pf/simulation: việc chưa có kết quả (queued/running/failed) |
| 409 | `simulation.params_unapproved` | pf/simulation: CGH FN-01 BR-04 — còn tham số chưa có phiên bản phê duyệt trong ảnh chụp; trường mở rộng `unapproved_parameters` |
| 409 | `simulation.not_accepting_result` | pf/simulation (Internal API): việc không ở trạng thái chờ kết quả (chưa có ảnh chụp) |
| 409 | `simulation.snapshot_mismatch` | pf/simulation (Internal API): phong bì kết quả mang snapshot_sha256 khác ảnh chụp đã ghim |
| 422 | `simulation.result_invalid` | pf/simulation (Internal API): phong bì kết quả không đúng contracts/optimizer/result.v1 |
| 500 | `simulation.snapshot_integrity` | pf/simulation: ảnh chụp đọc lại không khớp băm đã lưu |
| 503 | `simulation.dependency_unavailable` | pf/simulation: chưa nối nguồn cung HTX × vụ (SupplySource; G2 đợt 2 nối ở `assembly.SupplySource` từ pf/cropcycle + pf/party + pf/geo + pf/masterdata) — việc mô phỏng hỏng với mã này |
| 422 | `simulation.supply_invalid` | pf/simulation: nguồn cung trả dữ liệu không hợp lệ (âm, tháng vụ sai) |
| 422 | `simulation.no_supply` | pf/simulation: không có HTX có toạ độ nằm trong bán kính phục vụ của các Hub |
| 403 | `simulation.internal_forbidden` | pf/simulation: Internal API chỉ nhận client mTLS có CN/SAN thuộc AG_INTERNAL_CLIENT_NAMES |

Việc mô phỏng hỏng ghi `error_code` trên việc: mã `simulation.*` (lỗi dựng ảnh chụp) hoặc `optimizer.*` do optimizer báo (`optimizer.snapshot_invalid`, `optimizer.snapshot_incomplete`, `optimizer.solver_unavailable`).

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
| 422 | `geo.crs_unsupported` | pf/geo: hệ toạ độ khác WGS84 (EPSG:4326) — thành phần `crs` khác; `.prj` của Shapefile là VN-2000, hệ chiếu phẳng, hệ khác hoặc thiếu `.prj`; hoặc toạ độ ngoài miền kinh/vĩ độ. Hệ thống không tự chuyển VN-2000 — chuyển sang EPSG:4326 trước khi tải lên |
| 422 | `geo.area_out_of_range` | pf/geo: diện tích trắc địa ngoài 0,001–1.000 ha |
| 422 | `geo.outside_commune` | pf/geo: phần thửa nằm ngoài ranh giới xã (đã nhập) vượt ngưỡng; có `outside_pct` |
| 422 | `geo.household_invalid` | pf/geo: nông hộ gán cho thửa không tồn tại, đã ngừng hoạt động hoặc không là thành viên của HTX được gán (khi đã nối pf/party) |
| 422 | `geo.cooperative_invalid` | pf/geo: HTX không tồn tại, đã ngừng hoạt động hoặc ngoài phạm vi (khi đã nối pf/party) |
| 422 | `geo.format_invalid` | pf/geo: tệp nhập không đọc được, không có đối tượng, hoặc quá 200 đối tượng; Shapefile: không phải gói .zip, thiếu .shp/.dbf, nhiều lớp, số bản ghi .dbf khác .shp |
| 422 | `geo.format_unsupported` | pf/geo: không nhận ra định dạng tệp — hỗ trợ GeoJSON, KML, Shapefile (.zip) |
| 503 | `geo.dependency_unavailable` | pf/geo: chưa nối hoặc không gọi được pf/party (phạm vi), pf/masterdata (đơn vị hành chính, tham số); vai `farmer` đọc `GET /v1/geo/gps-fixes` khi chưa nối liên kết tài khoản ↔ nông hộ (pf/accounts) |
| 404 | `geo.cooperative_boundary_not_found` | pf/geo G2: HTX chưa có ranh giới, hoặc ranh giới nằm ngoài phạm vi đọc (xã; quản lý HTX: HTX khác) — không lộ sự tồn tại |
| 409 | `geo.cooperative_boundary_exists` | pf/geo G2: HTX đã có ranh giới — sửa bằng `POST /v1/geo/cooperative-boundaries/{ref}/versions` (kèm `supersedes_id`, `correction_reason`); problem có `current_boundary_id`, `current_version`, `status` (GIS US-BND-02/03) |
| 404 | `geo.facility_not_found` | pf/geo G2: không có kho/Hub/nhà máy của tenant với ref/mã này |
| 409 | `geo.facility_code_exists` | pf/geo G2: mã kho/Hub (hoặc `ref` do máy sinh) đã được dùng — mã giữ mãi, kể cả khi đã ngừng dùng |
| 404 | `geo.hub_not_found` | pf/geo G2: chưa nhận hub MKG này qua van công bố (`hub_facility`) nên chưa nhập tay sức chứa được |
| 404 | `geo.road_not_found` | pf/geo G2: không có tuyến đường bộ |
| 409 | `geo.feature_state_conflict` | pf/geo G2: trạng thái ranh giới HTX / kho-Hub / tuyến không cho phép thao tác (sửa khi đã ngừng dùng, ngừng dùng hai lần, khôi phục khi chưa ngừng dùng) |
| 409 | `geo.capacity_nothing_to_release` | pf/geo G2: không có số sức chứa Admin nhập (manual/import) đang áp dụng để nhả (BR-52) |
| 405 | `geo.gps_log_read_only` | pf/geo G2: "Dữ liệu log chỉ đọc, không thể chỉnh sửa hoặc xóa." — mọi PUT/PATCH/DELETE vào log GPS, kể cả Admin (HTX US-GPS-03 AC-1) |

Thông báo nghiệp vụ G2 nằm ở `detail` của `validation_failed` (422), đúng câu chữ backlog GIS: kho/Hub thiếu tên hoặc địa chỉ (= `province_code` + `commune_code`; pf không lưu địa chỉ chữ, R-04) → "Vui lòng nhập đầy đủ Tên Hub và Địa chỉ."; sức chứa ≤ 0 hoặc sai định dạng số → "Sức chứa phải là số lớn hơn 0." (US-BND-05/06 AC-2/AC-3); tuyến thiếu tên hoặc phân loại → "Vui lòng nhập đầy đủ Tên tuyến và Phân loại."; tải trọng tối đa ≤ 0 hoặc sai định dạng → "Tải trọng tối đa phải là số lớn hơn 0." (US-ROAD-02/03 AC-2); ranh giới HTX thiếu HTX → "Vui lòng nhập đầy đủ Tên HTX và Địa chỉ." (US-BND-02 AC-2 — tên, địa chỉ HTX nằm ở sổ pf/party). Ranh giới HTX, khuôn viên kho và tuyến dùng chung `geo.invalid_geometry`, `geo.crs_unsupported`, `geo.area_out_of_range` (ranh giới HTX: 0,01–100.000 ha), `geo.version_conflict` (problem có `current_boundary_id` / `current_event_id` / `current_segment_id`).

Cổng `geo.GpsLog.RecordFix` (module khác gọi, không qua HTTP) trả lỗi bọc `port.ErrGpsFixInvalid` khi dữ liệu sai (thiếu actor, mục đích/nguồn lạ, không có toạ độ mà không phải chọn lô thủ công kèm `plot_ref`, `captured_at` ở tương lai) — module gọi không nên thử lại; lỗi khác (DB) thì đưa vào hàng đợi của mình và gọi lại cùng ID.

Đồng bộ ngoại tuyến `plot_boundary` (docs/dev/sync-contract.md): không chặn được máy — chồng lấn HTX khác, sửa trên phiên bản cũ, hoặc chồng lấn cùng HTX chưa xác nhận **không** bị từ chối; bản ghi được giữ, trả `status = conflict` kèm `conflict_id`. Chồng lấn cùng HTX có `confirm_overlap: true` → `accepted` (xung đột vẫn mở để xem xét). Lệch diện tích khai báo không chặn. Bản ghi sai trả `rejected` với các mã ở trên.

### pf/cropcycle

| HTTP | code | Ý nghĩa |
|---|---|---|
| 404 | `cropcycle.sop_not_found` | pf/cropcycle: không có mẫu/phiên bản SOP, hoặc (Application API) mẫu chưa ban hành / mẫu riêng của HTX khác |
| 409 | `cropcycle.sop_name_taken` | pf/cropcycle: "Tên SOP này đã tồn tại, vui lòng chọn tên khác." — so tên đã chuẩn hóa (chữ thường, gộp khoảng trắng), kể cả khi nhân bản (HTX US-CAT-01 AC-6, US-CAT-03 AC-3) |
| 409 | `cropcycle.sop_code_taken` | pf/cropcycle: mã mẫu SOP đã dùng |
| 422 | `cropcycle.sop_step_order` | pf/cropcycle: "Thứ tự bước không hợp lệ, vui lòng kiểm tra lại." — `seq` phải liên tục 1..n theo thứ tự gửi (HTX US-CAT-01 AC-6) |
| 422 | `cropcycle.sop_harvest_step_missing` | pf/cropcycle: phát hành quy trình không có bước `thu_hoach` |
| 409 | `cropcycle.sop_state` | pf/cropcycle: sửa phiên bản đã phát hành (tạo phiên bản mới), hai bản nháp cùng lúc, thao tác trên mẫu đã ngừng dùng |
| 422 | `cropcycle.sop_not_published` | pf/cropcycle: ghim vào vụ một phiên bản chưa phát hành hoặc của mẫu đã ngừng dùng |
| 422 | `cropcycle.no_sop_for_variety` | pf/cropcycle: giống chưa gắn mẫu SOP đã ban hành — chọn `sop_version_id` khi mở vụ (HTX US-SEASON-01 AC-1) |
| 422 | `cropcycle.binding_invalid` | pf/cropcycle: chỉ gắn giống với mẫu SOP **hệ thống** đã ban hành |
| 404 | `cropcycle.season_not_found` | pf/cropcycle: không có vụ trong phạm vi đọc |
| 404 | `cropcycle.plot_not_found` | pf/cropcycle: pf/geo không có thửa |
| 422 | `cropcycle.plot_invalid` | pf/cropcycle: thửa chưa gán HTX (nháp), đã ngừng dùng hoặc không có diện tích |
| 403 | `cropcycle.out_of_scope` | pf/cropcycle: "Nông hộ hoặc lô ruộng này không thuộc phạm vi quản lý của bạn." — quản lý HTX ghi cho HTX khác, nông dân ghi cho hộ khác (HTX US-SEASON-02 AC-2) |
| 403 | `cropcycle.read_only` | pf/cropcycle: cán bộ KN chỉ được đọc dữ liệu mùa vụ |
| 422 | `cropcycle.sowing_date_out_of_window` | pf/cropcycle: "Ngày gieo sạ phải trong khoảng 15 ngày trước đến 30 ngày sau ngày hiện tại." (ngày nghiệp vụ giờ Việt Nam, HTX US-SEASON-01 AC-2) |
| 409 | `cropcycle.season_already_open` | pf/cropcycle: "Lô ruộng đang canh tác vụ khác, không thể mở vụ mới." (HTX US-SEASON-04); có `open_season_ref` |
| 409 | `cropcycle.previous_season_not_completed` | pf/cropcycle: sao chép cấu hình vụ trước khi vụ gần nhất chưa hoàn thành (HTX US-SEASON-03 AC-2) |
| 409 | `cropcycle.no_previous_season` | pf/cropcycle: thửa chưa có vụ để sao chép cấu hình |
| 422 | `cropcycle.cooperative_inactive` | pf/cropcycle: HTX không tồn tại hoặc đã ngừng hoạt động |
| 422 | `cropcycle.catalog_invalid` | pf/cropcycle: mã giống / vụ / máy không có hoặc đã ngừng dùng trong danh mục masterdata |
| 409 | `cropcycle.season_not_open` | pf/cropcycle: "Lô ruộng này chưa được mở vụ. Vui lòng mở vụ trước khi ghi nhật ký." (HTX US-LOG-02 AC-2); cũng khi vụ đã đóng |
| 409 | `cropcycle.season_state` | pf/cropcycle: sửa/hủy vụ không còn mở; khai sản lượng lần hai (dùng bản ghi bù) |
| 409 | `cropcycle.concurrent_edit` | pf/cropcycle: `supersedes_id` không phải phiên bản hiện hành (vụ, nhật ký, sản lượng); có `current_*` |
| 422 | `cropcycle.locked_fields` | pf/cropcycle: "Tọa độ GPS và thời gian không thể chỉnh sửa sau khi lưu." (HTX US-LOG-01 AC-3) |
| 404 | `cropcycle.log_not_found` | pf/cropcycle: không có nhật ký trong phạm vi |
| 409 | `cropcycle.log_state` | pf/cropcycle: duyệt nhật ký không ở trạng thái chờ duyệt (bị thay, đã duyệt, chờ bổ sung) |
| 403 | `cropcycle.self_review` | pf/cropcycle: người ghi phiên bản nhật ký không được tự duyệt |
| 422 | `cropcycle.batch_empty` | pf/cropcycle: lượt ghi hàng loạt không còn lô hợp lệ; có `skipped` (HTX US-LOG-03 AC-2) |
| 409 | `cropcycle.harvest_too_early` | pf/cropcycle: "Lô ruộng chưa đến giai đoạn thu hoạch, chưa thể khai báo sản lượng." — trước ngày sạ + offset − cửa sổ của bước thu hoạch SOP đã ghim; có `harvest_open_date` (HTX US-YIELD-01 AC-1) |
| 409 | `cropcycle.phi_violation` | pf/cropcycle: ngày thu hoạch trước khi hết thời gian cách ly của lần phun thuốc gắn bước SOP có `phi_days`; có `earliest_harvest_date` |
| 422 | `cropcycle.yield_negative` | pf/cropcycle: sản lượng âm |
| 409 | `cropcycle.yield_anomaly` | pf/cropcycle: sản lượng 0 hoặc năng suất vượt ngưỡng theo giống (`cropcycle.yield_anomaly_max_t_per_ha[:giống]`, mặc định 12 t/ha); `needs_confirm`, `anomaly_kind`, `yield_t_per_ha`, `threshold_t_per_ha`. Gửi lại với `confirm_anomaly: true` (HTX US-YIELD-03) |
| 404 | `cropcycle.harvest_not_found` | pf/cropcycle: không có khai báo sản lượng trong phạm vi |
| 422 | `cropcycle.import_invalid` | pf/cropcycle: tệp nhập sản lượng không đọc được, có công thức, quá 200 dòng / 2 MB, thiếu cột mã lô/mã vụ hoặc sản lượng |
| 409 | `cropcycle.id_reused` | pf/cropcycle: `id`/`season_ref` do máy sinh đã dùng cho bản ghi khác |
| 404 | `cropcycle.collection_statement_not_found` | pf/cropcycle (G3-2): không có bảng kê thu gom MKG trong phạm vi (quản lý HTX chỉ thấy bảng kê của HTX mình) |
| 409 | `cropcycle.collection_statement_stale` | pf/cropcycle (G3-2): phản hồi theo `revision` cũ — MKG đã gửi bản sửa; `current_revision` là bản hiện hành |
| 403 | `cropcycle.collection_manager_only` | pf/cropcycle (G3-2): chỉ `coop_manager` của đúng HTX (`org_ref`) được xác nhận/tranh chấp bảng kê |
| 503 | `cropcycle.dependency_unavailable` | pf/cropcycle: chưa nối hoặc không gọi được pf/geo, pf/party, pf/masterdata, liên kết tài khoản ↔ nông hộ (vai `farmer`); consumer `pf.cropcycle.gps_log` chưa nối log GPS pf/geo (sự kiện được giao lại) |

Nhãn vị trí (HTX US-GPS-01/02): `location_status` = `match` (≤ `cropcycle.gps_match_tolerance_m`, mặc định 50 m) | `mismatch` | `missing`; nguồn `gps_source` luôn có. Thiếu GPS hoặc không đo được **không** chặn lưu.

### pf/notify

| HTTP | code | Ý nghĩa |
|---|---|---|
| 404 | `notify.notification_not_found` | pf/notify: không có thông báo, của người khác, ngoài phạm vi hoặc thông báo chủ đề cũ hơn 90 ngày — không lộ sự tồn tại |
| 403 | `notify.device_unregistered` | pf/notify: `POST /v1/devices/{id}/push-token` với thiết bị chưa đăng ký ở pf/sync (`POST /v1/devices`) hoặc đăng ký bởi tài khoản khác |
| 403 | `notify.device_revoked` | pf/notify: gắn token cho thiết bị đã bị thu hồi — app xóa dữ liệu cục bộ, sinh mã thiết bị mới |
| 403 | `notify.out_of_scope` | pf/notify: `POST /v1/sync/nudge` với `unit` ngoài phạm vi người gọi |
| 404 | `notify.nudge_target_not_found` | pf/notify: `device_id` không có token đẩy đang dùng hoặc ngoài phạm vi (trừ vai `device_admin`, `platform_admin`) |
| 429 | `notify.nudge_rate_limited` | pf/notify: đích đã được yêu cầu đồng bộ trong 5 phút qua, hoặc người gọi vượt 30 lần/giờ; có `retry_after_s` và header `Retry-After` |
| 422 | `notify.nudge_entity_unknown` | pf/notify: `entities[]` của `POST /v1/sync/nudge` có tên không phải entity chiều xuống của pf/sync (KN US-SYNC-02) |
| 503 | `notify.dependency_unavailable` | pf/notify: chưa nối sổ thiết bị của pf/sync |
### pf/accounts

Tài khoản cán bộ quản lý trong Keycloak qua Admin REST (KN US-ADM-01..03, HTX US-ACC-01/02, US-HTXSTATUS-01..03, CGH FN-01). Lỗi không bao giờ chứa họ tên, email, SĐT hay thân phản hồi của Keycloak (R-14).

| HTTP | code | Ý nghĩa |
|---|---|---|
| 403 | `accounts.out_of_scope` | pf/accounts: tổ chức (`org_ref`) hoặc tài khoản đích ngoài phạm vi TW → Tỉnh → Xã của người gọi; tổ chức cấp quốc gia chỉ người có phạm vi toàn quốc thao tác |
| 403 | `accounts.role_not_grantable` | pf/accounts: vai cần cấp, hoặc một vai tài khoản đích đang có, nằm ngoài ma trận cấp vai của người gọi (quản trị tỉnh chỉ `kn_officer`, `coop_manager`); tài khoản `platform_admin` chỉ `platform_admin` thao tác; vai không có trong realm |
| 403 | `accounts.self_action` | pf/accounts: tự khóa, mở khóa hoặc đổi đơn vị/vai của chính mình |
| 404 | `accounts.user_not_found` | pf/accounts: không có người dùng Keycloak, hoặc người dùng thuộc tenant khác (không lộ sự tồn tại) |
| 409 | `accounts.user_exists` | pf/accounts: tên đăng nhập hoặc email đã có trong realm |
| 409 | `accounts.phone_taken` | pf/accounts: "Số điện thoại này đã được đăng ký cho một tài khoản khác." (trong tenant) |
| 409 | `accounts.state_conflict` | pf/accounts: khóa tài khoản đã khóa, mở tài khoản đang hoạt động, gửi liên kết cho tài khoản đang khóa |
| 409 | `accounts.org_inactive` | pf/accounts: gán tài khoản vào tổ chức đã vô hiệu hóa, hoặc mở khóa khi tổ chức của tài khoản đang vô hiệu hóa (kích hoạt lại tổ chức sẽ tự mở) |
| 422 | `accounts.org_invalid` | pf/accounts: không có tổ chức với `org_ref` trong tenant |
| 422 | `accounts.import_invalid` | pf/accounts: tệp nhập không phải CSV/XLSX đọc được, thiếu cột bắt buộc (`email`, `org_ref`, `roles`, `full_name` hoặc `first_name`/`last_name`), không có dòng dữ liệu hoặc quá 200 dòng — từ chối cả tệp |
| 422 | `accounts.household_invalid` | pf/accounts: tài khoản nông dân — nông hộ không có, đã ngừng, chưa là thành viên HTX đang hoạt động, hoặc SĐT không khớp nông hộ nào |
| 422 | `accounts.phone_household_mismatch` | pf/accounts: SĐT không phải SĐT chủ hộ của `household_ref` đã chọn (tra chỉ mục mù kho định danh) |
| 409 | `accounts.farmer_exists` | pf/accounts: nông hộ đã có tài khoản nông dân đang dùng |
| 404 | `accounts.farmer_not_found` | pf/accounts: không có tài khoản nông dân hoặc ngoài HTX/phạm vi của người gọi |
| 503 | `accounts.activation_link_unavailable` | pf/accounts: Keycloak chưa có phần mở rộng phát liên kết hành động (`AG_KC_ACTION_LINK_URL`) — tài khoản vẫn được tạo, phát lại lượt kích hoạt sau |
| 404 | `accounts.staff_not_found` | pf/accounts: danh bạ cán bộ HTX — tài khoản không thuộc HTX của nông dân (HTX US-SUPPORT-02) |
| 404 | `accounts.contact_unavailable` | pf/accounts: cán bộ HTX chưa có SĐT hợp lệ — app ẩn nút gọi nhanh (HTX US-SUPPORT-02 AC-2) |
| 422 | `validation_failed` | pf/accounts: lý do khóa/mở khóa dưới 20 ký tự; email/SĐT/tên đăng nhập sai dạng; thiếu vai; vai sai loại tổ chức (`coop_manager` → "Vui lòng chọn HTX liên kết.") |
| 502 | `accounts.email_failed` | pf/accounts: Keycloak không gửi được email liên kết (SMTP lỗi) khi gửi lại; khi tạo tài khoản thì trả 201 với `activation_email_sent = false` |
| 503 | `accounts.dependency_unavailable` | pf/accounts: chưa cấu hình `AG_KC_*`, Keycloak không phản hồi hoặc từ chối tài khoản dịch vụ, chưa nối pf/party |

Nhập hàng loạt: lý do theo dòng dùng nhãn "Thiếu Email", "Sai định dạng Email", "Sai định dạng vai trò", "SĐT đã tồn tại", "Tên đăng nhập hoặc Email đã tồn tại", "Trùng với dòng N trong tệp", "Tổ chức ngoài phạm vi phụ trách", "Vai trò vượt quyền cấp của bạn". Thao tác loạt trả `code` của từng tài khoản trong `items[]`.

### mkg/hub (API nội bộ MKG — `cmd/api-internal`, `contracts/openapi/mkg-internal.yaml`)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `hub.facility_not_found` | Cơ sở không có (hoặc chưa được duyệt lần nào) trong tenant của token |
| 404 | `hub.version_not_found` | Không có phiên bản dữ liệu chủ số `{version}` của cơ sở |
| 404 | `hub.lot_not_found` | Không có lô trong tenant; lô vào của mẻ không tồn tại |
| 404 | `hub.movement_not_found` | Không có dịch chuyển tồn cần bù |
| 403 | `self_approval_forbidden` | Người duyệt phiên bản dữ liệu chủ là người đề xuất (R-56; DB chặn lần hai bằng CHECK) |
| 409 | `hub.code_taken` | Mã cơ sở đã dùng bởi cơ sở khác (hiện hành hoặc đang chờ duyệt) |
| 409 | `hub.pending_version_exists` | Cơ sở đang có một phiên bản chờ duyệt — duyệt/từ chối trước khi đề xuất tiếp |
| 409 | `hub.version_not_pending` | Phiên bản đã được duyệt hoặc từ chối |
| 409 | `hub.facility_inactive` | Nhận lô hoặc chạy mẻ ở cơ sở đã ngừng dùng |
| 409 | `hub.insufficient_stock` | Không đủ tồn THEO LÔ (xuất, lấy vào mẻ, hoặc bù làm tồn âm) — không có lô thì không có hàng |
| 409 | `hub.already_corrected` | Dịch chuyển đã có dòng bù; sai tiếp thì ghi nghiệp vụ mới |
| 409 | `hub.id_reused` | `id` lô do thiết bị sinh đã dùng cho bản ghi khác |
| 422 | `validation_failed` | Dữ liệu vào sai (khối lượng không phải chuỗi thập phân ≤ 3 chữ số, độ ẩm ngoài 0..100, tọa độ ngoài WGS84, thiếu lý do thay đổi ≥ 10 ký tự, từ chối thiếu lý do, `event_time` ở tương lai), xem `errors[]` |
| 422 | `hub.ratio_invalid` | Tỷ lệ nguồn của lô không hợp lệ: Σ khác 1 chính xác, > 6 chữ số, ≤ 0, trộn `mass_kg` với `ratio`, phần đóng góp < 0.000001 |
| 422 | `hub.negative_loss` | Mẻ có khối lượng ra lớn hơn khối lượng vào |
| 422 | `hub.lot_other_facility` | Lô vào mẻ / lô chỉ định khi xuất không thuộc cơ sở của thao tác |
| 422 | `hub.duplicate_lot_input` | Một lô xuất hiện hai lần trong cùng mẻ / phiếu xuất |
| 422 | `hub.allocation_mismatch` | Phân bổ tay theo lô có tổng khác lượng xuất |
| 422 | `hub.correction_unsupported` | Bù trực tiếp chỉ cho `receive` và `dispatch`; dòng của mẻ biến đổi sửa bằng mẻ bù (câu hỏi mở Q-MKG-05) |
| 422 | `hub.parent_invalid` | Cơ sở cha không phải hub hiện hành khác chính nó |
| 422 | `hub.boundary_invalid` | Ranh giới không phải Polygon/MultiPolygon WGS84 hợp lệ |
| 500 | `hub.valve_denied` | Van công bố từ chối payload (hợp đồng thiếu/chưa duyệt, trường ngoài hợp đồng) — lỗi lập trình, không có gì được ghi |
| 404 | `hub.param_set_not_found` | G3: bộ tham số không phải `risk` / `weigh` |
| 404 | `hub.param_version_not_found` | G3: không có phiên bản tham số số `{version}` |
| 409 | `hub.param_pending_exists` | G3: bộ tham số đang có phiên bản chờ duyệt |
| 409 | `hub.weigh_ticket_unknown` | G3 (Q-MKG-04): nhận lô với `weigh_ticket_ref` mà hub chưa nhận sự kiện phiếu cân HUB_IN — thử lại sau |
| 409 | `hub.weigh_ticket_used` | G3: phiếu cân đã dùng cho lần nhận khác (hoặc đã áp vào lô nhập tay) |
| 422 | `hub.weigh_ticket_lot_mismatch` | G3: `id` lô khác `lot_ref` của phiếu cân |
| 422 | `hub.weigh_mass_mismatch` | G3: `mass_kg` khác khối lượng phiếu cân (để trống để lấy theo phiếu); nhận chuyển kho: khác Σ phiếu |
| 422 | `hub.weigh_ticket_required` | G3: tham số `hub.weigh.v1` bật `ticket_required` — cần `weigh_ticket_ref`, hoặc `manual_reason` (10..500 ký tự) khi cân hỏng |
| 404 | `hub.stocktake_not_found` | G3 (Q-MKG-06): không có phiên kiểm kê trong tenant |
| 409 | `hub.stocktake_pending_exists` | G3: cơ sở đang có phiên kiểm kê chờ duyệt — duyệt/từ chối, hoặc đếm lại với `supersedes_id` + `reason` |
| 409 | `hub.stocktake_not_pending` | G3: phiên kiểm kê đã duyệt / từ chối / bị thay |
| 409 | `hub.stocktake_stale` | G3: tồn theo sổ đã đổi kể từ lúc đếm — đếm lại |
| 422 | `hub.stocktake_lines_invalid` | G3: dòng kiểm kê lặp dạng hàng hoặc thiếu một dạng hàng đang có tồn |
| 403 | `self_approval_forbidden` | G3: người duyệt phiên kiểm kê là người đếm; người duyệt tham số là người đặt (R-56; DB chặn lần hai bằng CHECK) |
| 404 | `hub.transfer_not_found` | G3 (Q-MKG-10): không có phiếu chuyển kho |
| 409 | `hub.transfer_not_in_transit` | G3: phiếu chuyển đã nhận |
| 422 | `hub.transfer_same_facility` | G3: cơ sở đích trùng cơ sở đi |
| 422 | `hub.transfer_wrong_facility` | G3: nhận ở cơ sở không phải đích của phiếu chuyển |
| 422 | `hub.transfer_ticket_invalid` | G3: phiếu cân không phải phiếu HUB_IN (đến) của một lô nguồn thuộc phiếu chuyển, hoặc lặp |

### mkg/measurement (API nội bộ MKG — `cmd/api-internal`, `contracts/openapi/mkg-internal.yaml`)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `measurement.lot_not_found` | Lô chưa có phiếu cân (hoặc chưa được đối soát) trong tenant của token |
| 404 | `measurement.ticket_not_found` | Không có phiếu cân |
| 404 | `measurement.device_not_found` | Không có thiết bị cân / kỳ đăng ký |
| 404 | `measurement.session_not_found` | Không có phiên cân |
| 404 | `measurement.version_not_found` | Không có phiên bản dung sai / định mức hao hụt số `{seq}` |
| 404 | `measurement.dispute_not_found` | Không có hồ sơ tranh chấp |
| 404 | `measurement.import_not_found` | Không có lần nhập số cân của khách |
| 403 | `self_approval_forbidden` | Người duyệt thiết bị / dung sai / định mức là người đề xuất (R-56; DB chặn lần hai bằng CHECK) |
| 409 | `measurement.device_taken` | `device_id` đã đăng ký ở tài khoản khác (một thiết bị gắn một tenant mãi mãi) |
| 409 | `measurement.pending_exists` | Thiết bị đang có đề xuất đăng ký chờ duyệt |
| 409 | `measurement.not_pending` | Đề xuất không còn chờ duyệt |
| 409 | `measurement.device_not_active` | Thiết bị / kỳ đăng ký không hiệu lực (chưa duyệt, đã thu hồi, đã có kỳ mới) |
| 409 | `measurement.session_conflict` | Thiết bị (kỳ) đang có phiên mở, hoặc `counter_from` không lớn hơn `counter_to` của phiên đã đóng trước |
| 409 | `measurement.session_closed` | Phiên cân đã đóng |
| 409 | `measurement.reconciliation_incomplete` | Lô chưa đủ phiếu để đối soát (HUB: đủ ba điểm; DIRECT: CUSTOMER_IN) |
| 409 | `measurement.dispute_transition` | Thao tác không hợp lệ ở trạng thái hồ sơ (vd kết luận khi chưa xem xét; thêm bằng chứng khi đã đóng) |
| 409 | `measurement.no_hub_in` | Lập bằng chứng MRV khi lô chưa có phiếu HUB_IN |
| 409 | `measurement.no_plot_sources` | Lô không có nguồn thửa (hoặc chưa nối nguồn gốc lô mkg/hub) |
| 409 | `measurement.no_customer_in` | G3-2: lập bằng chứng MRV cho lô DIRECT khi chưa có phiếu CUSTOMER_IN |
| 409 | `measurement.collection_link_conflict` | G3-2: lệnh gom đã gắn với lô cân khác, hoặc lô đã gắn lệnh gom khác mà không có lý do đổi (mở phiên cân có `collection_order_ref`, `POST …/lots/{id}/collection`) |
| 422 | `measurement.invalid_ticket` | Phiếu vi phạm hợp đồng TicketWriter: đã có phiếu hiệu lực ở điểm cân (cần `supersedes_id`), phiếu bù trỏ phiếu khác lô/điểm hoặc đã bị bù, HUB_IN/HUB_OUT trên lô DIRECT, tuyến khác tuyến của lô, nhập tay thiếu bằng chứng/lý do, `id` đã dùng |
| 422 | `measurement.window_incomplete` | Cửa sổ số đo có bộ đếm thiếu (không bao giờ nội suy) |
| 422 | `validation_failed` | Dữ liệu vào sai (khối lượng không phải chuỗi ≤ 3 chữ số, phần trăm ngoài 0..100 hoặc > 2 chữ số, khóa Ed25519 không 32 byte, `cert_sha256` không phải hex 64, lý do < 10 ký tự, ngưỡng nghiêm trọng < ngưỡng cờ, khoảng ẩm không liền nhau) |
| 500 | `measurement.valve_denied` | Van công bố từ chối payload `mrv_collection_evidence` (hợp đồng thiếu, trường ngoài hợp đồng) — không có gì được ghi |

### mkg/measurement — ingest (`cmd/ingest`, `POST /v1/ingest/batches` qua mTLS)

Agent biên nhận `200` + `{device_id, epoch, high_water_counter}` khi lô đã commit (gửi lặp cùng lô trả đúng xác nhận cũ) và chỉ xóa đệm tới `high_water_counter`. `429` và `5xx` → gửi lại sau; mọi `4xx` khác → **không** gửi lại, cách ly lô và báo động (ADR-0019).

| HTTP | code | Khi nào |
|---|---|---|
| 400 | `edge.decode` | Thân không phải lô `agrigreen.edge.batch.v1`: JSON hỏng, khóa lạ, thiếu khóa (kể cả `ntp_offset_ms`), dữ liệu thừa |
| 422 | `edge.format` · `edge.device_id` · `edge.epoch` · `edge.counter_range` · `edge.ntp_offset` · `edge.reading` | Lô sai ràng buộc hợp đồng (`edgewire.Validate`; mã như `contracts/edge/batch.v1.vectors.json`) |
| 422 | `edge.key_id` | `key_id` của lô không khớp khóa nguồn đã duyệt của kỳ `(device_id, epoch)` |
| 422 | `edge.signature` | Chữ ký Ed25519 sai dạng hoặc không hợp lệ với khóa đã duyệt |
| 401 | `ingest.client_cert_required` | Không có chứng thư mTLS client (listener đã chặn ở bắt tay; mã này là lớp phòng thủ thứ hai) |
| 403 | `ingest.device_mismatch` | `device_id` của lô không trùng CN/SAN của chứng thư client (R-16) |
| 403 | `ingest.unknown_device` | `(device_id, epoch)` chưa đăng ký hoặc chưa được duyệt trong sổ thiết bị |
| 403 | `ingest.cert_mismatch` | Chứng thư client hợp lệ với CA nhưng không phải chứng thư đã duyệt của kỳ (SHA-256 DER khác) |
| 403 | `ingest.device_revoked` | Thiết bị đã bị thu hồi — lô đã nhận trước đó giữ nguyên (RB-1) |
| 409 | `ingest.epoch_superseded` | Kỳ đăng ký đã được thay; bộ đếm của kỳ cũ không bao giờ nhận thêm — đăng ký kỳ mới |
| 409 | `ingest.batch_conflict` | Đã có lô cùng `(device_id, epoch, counter_from)` nhưng khác `payload_sha256`/`counter_to` — báo động (thiết bị hỏng hoặc bị giả), không ghi gì |
| 409 | `ingest.counter_regression` | Lô chồng lấn hoặc lùi bộ đếm so với lô cuối đã nhận của `(device_id, epoch)`. Khoảng trống thì được nhận và giám sát báo mất lô |
| 413 | `payload_too_large` | Thân vượt `edgewire.MaxBodyBytes` (1 MiB) |
| 429 | `ingest.rate_limited` | Vượt tốc độ theo thiết bị (`AG_INGEST_RATE_PER_MIN`, `AG_INGEST_RATE_BURST`); có `Retry-After` |

Mã của vòng tích phân (không trả qua HTTP; nhãn `code` của metric `ag.ingest.integrations` và log `event=ingest.integrate`): `ingest.window_incomplete` (cửa sổ thiếu bộ đếm — thử lại, báo động sau `AG_INGEST_STUCK_AFTER`), `ingest.no_stable_readings`, `ingest.non_positive_net` (tổng ≤ bì), `ingest.window_too_large`, `ingest.window_corrupt`, `measurement.invalid_ticket` (TicketWriter từ chối).

### mkg/fieldops (API nội bộ MKG — `cmd/api-internal`, `contracts/openapi/mkg-internal.yaml`)

| HTTP | code | Khi nào |
|---|---|---|
| 400 | `fieldops.cursor_invalid` | Con trỏ pull bị sửa, cắt, hoặc của thiết bị/tài khoản khác — pull lại không có `cursor` |
| 403 | `self_approval_forbidden` | Người duyệt lệnh gom là người lập (hoặc người sửa kế hoạch gần nhất) — R-56; DB chặn lần hai bằng CHECK |
| 403 | `fieldops.device_mismatch` | Thiếu header `X-Device-Id`, hoặc `device_id` của lô đồng bộ khác header |
| 404 | `fieldops.order_not_found` | Không có lệnh gom trong tenant của token |
| 404 | `fieldops.roll_count_not_found` | Không có bản ghi đếm cuộn cần bù |
| 409 | `fieldops.order_transition` | Chuyển trạng thái không hợp lệ (sửa kế hoạch sau khi duyệt, duyệt lệnh không còn `planned`, đóng lệnh chưa giao tổ, hủy lệnh đã đóng/hủy) |
| 409 | `fieldops.order_not_open` | Ghi số / mở ca trên lệnh chưa duyệt, đã đóng hoặc đã hủy |
| 409 | `fieldops.stage_already_counted` | Công đoạn này của thửa đã có số — sửa bằng bản ghi bù |
| 409 | `fieldops.already_corrected` | Bản ghi đã bị bù; bù bản ghi hiệu lực mới nhất |
| 409 | `fieldops.id_reused` | `id` do máy sinh đã dùng cho bản ghi khác (khác `idem_key`) |
| 409 | `fieldops.nothing_to_confirm` | Thửa chưa có số xếp ghe/xe nên chưa có bảng kê để HTX xác nhận |
| 409 | `fieldops.statement_stale` | Xác nhận theo `statement_revision` cũ — MKG đã sửa số sau đó |
| 422 | `validation_failed` | Dữ liệu vào sai (thiếu `rolls`, `loose_kg` khi có cuộn, lý do < 10 ký tự, ngày không phải YYYY-MM-DD, khung > 90 ngày, `event_time` ở tương lai…), xem `errors[]` |
| 422 | `fieldops.plot_not_in_order` | Thửa không thuộc lệnh gom |
| 422 | `fieldops.previous_stage_missing` | Công đoạn trước chưa có số (proto FM-03) |
| 422 | `fieldops.stage_exceeds_previous` | Số cuộn nhiều hơn công đoạn trước (proto FM-04) — đếm lại hoặc bù công đoạn trước |
| 422 | `fieldops.discrepancy_reason_required` | Số cuộn ít hơn công đoạn trước mà thiếu `discrepancy_reason` (proto FM-04) |
| 422 | `fieldops.unit_mismatch` | Công đoạn trước đếm cuộn mà công đoạn sau ghi rơm rời (hoặc ngược lại) |
| 422 | `fieldops.next_stage_conflict` | Bản bù làm công đoạn sau vi phạm FM-04 — bù công đoạn sau trước |
| 422 | `fieldops.htx_not_found` | Application API không có (hoặc chưa chia sẻ — câu hỏi RB-2) HTX, hoặc HTX đã vô hiệu hóa |
| 422 | `fieldops.plot_not_found` | Application API không có (hoặc chưa chia sẻ) thửa |
| 422 | `fieldops.plot_other_htx` | Thửa không thuộc HTX của lệnh gom, là thửa nháp hoặc đã ngừng dùng |
| 500 | `fieldops.valve_denied` | Van công bố từ chối payload — lỗi lập trình, không có gì được ghi |
| 503 | `fieldops.directory_unavailable` | Không gọi được Application API (chưa cấu hình tài khoản dịch vụ MKG, lỗi mạng, token) |

Kết quả theo bản ghi của `POST /v1/fieldops/sync/push` dùng thêm `fieldops.unknown_entity` (entity khác `roll_count`) và `fieldops.sink_failed` (lỗi hệ thống — máy giữ và tự gửi lại).

### pf/waterway

Mạng đường thủy có phiên bản (mục 16; GIS FN-09, US-WATER-01/02/03). Vẽ/sửa, dựng, phát hành ở Admin API (MFA); tra lớp và định tuyến ở Application API.

| HTTP | code | Ý nghĩa |
|---|---|---|
| 422 | `validation_failed` | pf/waterway: thiếu tên tuyến hoặc phân loại → "Vui lòng nhập đầy đủ Tên tuyến và Phân loại."; chiều rộng, độ sâu, tải trọng tối đa (hoặc tĩnh không công trình) ≤ 0 hay sai định dạng số → "Chiều rộng, độ sâu và tải trọng tối đa phải là số lớn hơn 0." (US-WATER-02/03 AC-2); hình học không phải LineString ≥ 2 đỉnh WGS84; bản sửa thiếu `reason` (5–500 ký tự) hoặc `expected_revision`. `length_m` do hệ thống tính, giá trị client gửi bị bỏ qua |
| 404 | `waterway.segment_not_found` | pf/waterway: không có đoạn với `segment_ref` trong tenant |
| 404 | `waterway.structure_not_found` | pf/waterway: không có công trình với `structure_ref` trong tenant |
| 409 | `waterway.revision_conflict` | pf/waterway: `expected_revision` không phải bản hiện hành (đã có người sửa) — tải lại rồi sửa |
| 422 | `waterway.segment_retired` | pf/waterway: gắn công trình đang dùng vào đoạn đã ngừng dùng |
| 422 | `waterway.structure_off_segment` | pf/waterway: công trình cách tuyến quá `waterway.structure_max_offset_m` (mặc định 500 m) |
| 404 | `waterway.build_not_found` | pf/waterway: không có job dựng với id này trong tenant |
| 404 | `waterway.network_version_not_found` | pf/waterway: không có phiên bản với số này; định tuyến/lớp ghim `version` chưa từng phát hành; `GET /v1/waterway/networks/current` khi chưa phát hành phiên bản nào |
| 409 | `waterway.version_not_publishable` | pf/waterway: phát hành phiên bản không ở trạng thái `built` (đã phát hành hoặc đã bị thay) |
| 403 | `self_approval_forbidden` | pf/waterway: người phát hành là người đã yêu cầu dựng phiên bản (R-56) |
| 422 | `waterway.params_invalid` | pf/waterway: tham số dựng từ pf/masterdata không nhất quán (cần snap ≤ nối tự động ≤ báo cáo; lớp phương tiện thiếu kích thước). Job dựng gặp lỗi này chuyển `failed` với `error_code` tương ứng |
| 503 | `waterway.dependency_unavailable` | pf/waterway: chưa nối CSDL hoặc không đọc được tham số/danh mục pf/masterdata |

Định tuyến không tìm được đường thủy (chưa phát hành mạng, điểm ngoài bán kính tiếp cận, không liên thông hoặc không đủ tải trọng) **không** là lỗi: trả `200` với `found = false`, `source = haversine` (Haversine × `waterway.haversine_factor`) và `reason`.

### pf/metrics

Chỉ số có lịch sử và nhật ký lớp GIS (G3-A5; `GET /v1/metrics/*`, `GET /v1/gis/*`).

| HTTP | Mã | Khi nào |
|---|---|---|
| 404 | `metrics.unknown_metric` | pf/metrics: không có định nghĩa chỉ số với `key` (hoặc chưa có phiên bản hiệu lực vào ngày D) |
| 422 | `metrics.not_closed` | pf/metrics: ngày nghiệp vụ chưa kết thúc (trước `Cutoff(D)` = 00:00 giờ VN ngày D+1) — job/backfill không ghi, không chụp |
| 403 | `metrics.out_of_scope` | pf/metrics: `unit` ngoài phạm vi người gọi, hoặc xin phạm vi toàn tenant (`unit` rỗng) khi không có phạm vi TW; tài khoản chưa gắn đơn vị (nhật ký lớp) |
| 422 | `metrics.range_too_large` | pf/metrics: khoảng `from`..`to` quá 400 ngày hoặc `from` > `to` |
| 404 | `metrics.unknown_layer` | pf/metrics: lớp GIS không có (`plot`, `coop_boundary`, `facility`, `season_stage`, `cgh_balance`, `waterway`) |
| 422 | `metrics.future_date` | pf/metrics: phát lại một ngày ở tương lai |
| 422 | `metrics.invalid_layer_change` | pf/metrics: dòng nhật ký lớp thiếu `feature_ref`/`source_event_id`/`source_type`/`event_time`, `op` lạ, hoặc `create` có `before` (chỉ `LayerLog.Append`) |
| 409 | `metrics.definition_edited` | pf/metrics (job/backfill): tệp `defs/<key>.v<n>.yaml` của phiên bản đã dùng bị sửa tại chỗ (băm khác `definition_pin`) — thêm phiên bản mới thay vì sửa |
| 503 | `metrics.dependency_unavailable` | pf/metrics: chưa nối CSDL, nguồn chỉ số của module chủ dữ liệu, pf/party (phạm vi) hoặc pf/geo (hình học) |

Nguồn không có dữ liệu cho một phạm vi/ngày (vd vụ chưa có lần cân đối trước cutoff) **không** là lỗi API: job không ghi dòng (`port.ErrNoData`), API trả danh sách rỗng.
