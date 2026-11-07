### pf/hrm (`dom/hrm` — nâng bậc lương FN-09, sổ quyết định FN-10, W11.25)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `hrm.promotion_case_not_found` | Khóa kỳ nâng bậc sai dạng hoặc kỳ không còn trong danh sách (đã xử lý, dữ liệu hồ sơ đổi làm ngày đến kỳ đổi) |
| 409 | `hrm.promotion_conflict` | Kỳ đã có quyết định; lập quyết định thứ hai từ cùng danh sách |
| 422 | `hrm.promotion_rules_missing` | Job nhắc hiện hữu chưa có tham số promotion; FN-09 tương tác không phụ thuộc tham số này hay achievement_conversion |
| 404 | `hrm.promotion_export_not_found` | Mã lần xuất danh sách nâng bậc không có (hoặc là dòng tải lại) |
| 404 | `hrm.decision_not_found` | Mã quyết định không có |
| 409 | `hrm.decision_warnings` | Lưu quyết định có số trùng với quyết định còn hiệu lực — gửi lại với `acknowledge_warnings = true` |
| 409 | `hrm.decision_stale` | Bậc trong hồ sơ tại ngày hưởng khác bậc hiện giữ của danh sách đã xuất (hồ sơ đã sửa sau khi xuất) |
| 409 | `hrm.decision_cancelled` | Hủy lại hoặc đính kèm tệp cho quyết định đã hủy |
| 409 | `hrm.decision_cancel_order` | Hủy quyết định khi cùng người có quyết định nâng bậc còn hiệu lực với ngày hưởng sau |

FN-09 v4 chỉ tạo Mẫu1. Include/exclude, nlth-pool và proposals/withdraw đã gỡ routing, trả404.
`validation_failed` (422) cho năm/tháng/trạng thái sai hoặc NB07 ngoài1..120, số mũ/chuỗi dài,
số tháng hưởng và HS bảo lưu không đúng giới hạn; không dùng lỗi điều kiện xếp loại/quota ở FN09.
