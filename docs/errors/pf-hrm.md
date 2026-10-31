### pf/hrm (`dom/hrm` — kho tài liệu nhân sự, W11.24)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `hrm.folder_not_found` | Thư mục không có, đã ẩn (khi ghi vào), hoặc người gọi không thấy (không lộ sự tồn tại) |
| 409 | `hrm.folder_exists` | Tạo/đổi tên/hiện lại thư mục trùng tên (không phân biệt hoa thường) với thư mục đang dùng cùng cấp |
| 409 | `hrm.folder_not_empty` | Ẩn thư mục còn thư mục con hoặc tài liệu đang hiện |
| 404 | `hrm.doc_not_found` | Tài liệu không có hoặc ngoài phạm vi xem (tài liệu đã ẩn chỉ người quản lý thấy) |
| 422 | `hrm.doc_format` | Đuôi tệp ngoài pdf, jpg, jpeg, png, docx, xlsx, doc, xls hoặc chữ ký đầu tệp không khớp đuôi |
| 413 | `hrm.doc_too_large` | Tệp vượt 15 MB (trừ 1 KB phần phong bì) |
| 503 | `hrm.docs_unavailable` | Chưa nối kho tệp `pf/evidence` hoặc kho định danh |

Mục đích tệp (HRM-DOC-PURPOSE-001): `hr_records` được HCTH đăng ký vào registry
của evidence tại gốc lắp ráp, chỉ cho chủ thể `pf/hrm` / `hr_folder` đã phân giải
và được phép xem. Tệp khác module, khác loại hoặc tệp cũ không có bộ ba trả
`403 sync.purpose_not_allowed` và ghi sổ từ chối. Không thể dùng mục đích hồ sơ
để bỏ qua đồng ý của minh chứng MRV/hiện trường.

HRM-DOC-FILE-001: resolver thư mục cung cấp FileAuthorizer. Đường mở tệp chung kiểm lại phiên bản tài liệu hiện hành, đúng thư mục, quyền theo đơn vị/bản thân, trạng thái ẩn và cờ hồ sơ. Thấy thư mục không cấp quyền tất cả tệp. Từ chối trả 403 evidence.subject_forbidden và ghi sổ denied_scope; không trả URL/bản rõ.

### pf/hrm — đánh giá HCTH (W11.26)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `hrm.appraisal_not_found` | Không có mẫu, đợt, phiếu, phiên bản hoặc lần xuất trong phạm vi tenant/phân công |
| 409 | `hrm.appraisal_conflict` | Phiên bản đổi; khóa idempotency đã thuộc actor/body/thao tác khác; đã mở kỳ |
| 422 | `hrm.appraisal_blocked` | Phiếu/đợt chưa đủ đề xuất/căn cứ/bản ký hoặc bước không cho phép |
| 503 | `hrm.appraisal_guest_unavailable` | Guest OTP W10.18 chưa nối; link khách luôn đóng, tracking #184 |
| 503 | `hrm.appraisal_unavailable` | Chưa nối kho định danh, năng lực xuất hoặc phụ thuộc đánh giá |

Các lỗi trường nhập dùng `validation_failed` với đường dẫn `errors[].field`. Không phản chiếu JSON thô hay PII vào lỗi.
`assessment` giao quyền chủ thể/HS37 đóng băng và quyền hiện tại; xuất còn giao quyền `export` và quyền kho định danh.
MS05/MS06 chặn người chưa gửi hoặc thiếu căn cứ; PHIEU_DIEM là Excel tổng hợp tiến độ riêng và không phải biểu ký.
