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

Phản hồi phát lại theo Idempotency-Key có header `Idempotent-Replayed: true`.

### pf/geo

| HTTP | code | Ý nghĩa |
|---|---|---|
| 404 | `geo.plot_not_found` | pf/geo: không có thửa, hoặc nằm ngoài phạm vi đọc (xã/HTX) — không lộ sự tồn tại |
| 404 | `geo.conflict_not_found` | pf/geo: không có xung đột ranh giới trong phạm vi |
| 404 | `geo.admin_boundary_not_found` | pf/geo: chưa nhập ranh giới hành chính cho mã đơn vị |
| 409 | `geo.overlap` | pf/geo: ranh giới chồng lấn thửa **cùng HTX** (hoặc cùng là thửa nháp) vượt ngưỡng `geo.overlap_tolerance_pct` (mặc định 1 %, tính theo thửa nhỏ hơn); `overlaps` liệt kê `plot_ref`, `code`, `overlap_pct`. Chồng lấn khác HTX không lỗi — thửa được nhận, mở xung đột chờ xem xét |
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

Đồng bộ ngoại tuyến `plot_boundary` (docs/dev/sync-contract.md): chồng lấn cùng HTX hoặc sửa trên phiên bản cũ **không** bị từ chối — bản ghi được giữ, trả `status = conflict` kèm `conflict_id`. Bản ghi sai trả `rejected` với các mã ở trên.