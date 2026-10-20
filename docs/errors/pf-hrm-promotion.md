### pf/hrm (`dom/hrm` — nâng bậc lương FN-09, sổ quyết định FN-10, W11.25)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `hrm.promotion_case_not_found` | Khóa kỳ nâng bậc sai dạng hoặc kỳ không còn trong danh sách (đã xử lý, dữ liệu hồ sơ đổi làm ngày đến kỳ đổi) |
| 409 | `hrm.promotion_conflict` | Thao tác không hợp trạng thái: bỏ kỳ chưa đưa vào danh sách; đưa kỳ NLTX của người đã có trong Mẫu số 2 cùng lần nâng; gỡ NLTH đã xuất; lập quyết định thứ hai từ cùng danh sách; kỳ đã có quyết định |
| 422 | `hrm.promotion_export_blocked` | Xuất Mẫu số 2 (hoặc lập NLTH) bị chặn: thành tích đã dùng/không thuộc bảng quy đổi/quá số năm, ngày nâng ngoài năm chỉ tiêu, bậc cuối, vượt chỉ tiêu — `detail` liệt kê từng người |
| 422 | `hrm.nlth_quota_exceeded` | Lập danh sách NLTH làm số người của năm (đã có quyết định + đang trong danh sách + chọn thêm) vượt chỉ tiêu |
| 422 | `hrm.promotion_rules_missing` | Tenant chưa có tham số `promotion` hoặc `achievement_conversion` có hiệu lực tại ngày xét (FN-19 — Admin cài mặc định) |
| 404 | `hrm.promotion_export_not_found` | Mã lần xuất danh sách nâng bậc không có (hoặc là dòng tải lại) |
| 404 | `hrm.decision_not_found` | Mã quyết định không có |
| 409 | `hrm.decision_warnings` | Lưu quyết định có số trùng với quyết định còn hiệu lực — gửi lại với `acknowledge_warnings = true` |
| 409 | `hrm.decision_stale` | Bậc trong hồ sơ tại ngày hưởng khác bậc hiện giữ của danh sách đã xuất (hồ sơ đã sửa sau khi xuất) |
| 409 | `hrm.decision_cancelled` | Hủy lại hoặc đính kèm tệp cho quyết định đã hủy |
| 409 | `hrm.decision_cancel_order` | Hủy quyết định khi cùng người có quyết định nâng bậc còn hiệu lực với ngày hưởng sau |
