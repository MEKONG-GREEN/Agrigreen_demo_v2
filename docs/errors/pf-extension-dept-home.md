### pf/extension (`dom/extension` — Trang chủ phòng + kho văn bản phòng, W11.54)

Dùng lại mã đã có của module: `extension.read_only` (ngữ cảnh chỉ xem, Ban Giám đốc), `extension.support_reason_required`,
`extension.dependency_unavailable` (chưa nối kho đối tượng — tải lên/tải xuống). Admin không chọn phòng hoặc phòng không phải
phòng KN → 422 `validation_failed` (trường `department`); vai khác chọn phòng khác phòng của ngữ cảnh, Cấp Xã/TW mở trang chủ,
Cấp TW mở kho văn bản → 403 `forbidden`.

| HTTP | code | Khi nào |
|---|---|---|
| 422 | `extension.dept_profile_invalid` | Giới thiệu và chức năng nhiệm vụ đều rỗng, quá dài, có ký tự điều khiển |
| 409 | `extension.dept_profile_version_conflict` | `base_version` khác phiên bản giới thiệu hiện hành (khóa lạc quan) |
| 404 | `extension.dept_folder_not_found` | Thư mục (hoặc thư mục cha) không có, đã ẩn, hoặc thuộc phòng khác |
| 422 | `extension.dept_folder_invalid` | Tên thư mục rỗng/quá dài/có `/` `\`, quá 5 cấp, thiếu lý do ẩn/hiện lại |
| 409 | `extension.dept_folder_exists` | Đã có thư mục cùng tên (không phân biệt hoa thường) ở cùng cấp |
| 409 | `extension.dept_folder_not_empty` | Ẩn thư mục còn thư mục con hoặc văn bản đang hiện |
| 409 | `extension.dept_folder_version_conflict` | `base_version` khác phiên bản thư mục hiện hành |
| 404 | `extension.dept_doc_not_found` | Văn bản/phiên bản không có, thuộc phòng khác, đã ẩn (người không quản lý), Cấp Xã mở văn bản không dành cho xã, tệp không còn trong kho |
| 422 | `extension.dept_doc_invalid` | Thuộc tính sai (lỗi từng trường: tên, loại, đối tượng, ngày hiệu lực), tệp rỗng, thiếu lý do ẩn/hiện lại |
| 409 | `extension.dept_doc_version_conflict` | `base_version` khác phiên bản văn bản hiện hành |
| 409 | `extension.dept_doc_state` | Ẩn văn bản/thư mục đã ẩn, hiện lại mục đang hiện, sửa/tải tệp mới cho văn bản đang ẩn |
| 422 | `extension.dept_doc_file_type` | Đuôi tệp ngoài PDF, DOCX, XLSX hoặc nội dung không đúng định dạng của đuôi |
| 413 | `extension.dept_doc_too_large` | Tệp vượt 20 MB (FN-20 AC-02) |
