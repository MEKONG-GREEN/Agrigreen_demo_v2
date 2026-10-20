### pf/price (`cap/price` — điều tra giá thành sản xuất theo vụ, W11.78)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `price.survey_round_not_found` | Không có đợt điều tra (hoặc đợt thuộc bên dùng khác) |
| 409 | `price.survey_round_state` | Thao tác không hợp trạng thái đợt: mở đợt không ở Nháp, nhập tệp / rà soát khi đợt Nháp hoặc Đã chốt, mở lại đợt chưa chốt |
| 422 | `price.survey_round_incomplete` | Mở đợt khi chưa chọn nhóm cây hoặc xã điều tra |
| 409 | `price.survey_round_duplicate` | Đã có đợt cùng vụ, năm, nhóm cây — gửi `confirm = true` để vẫn lưu |
| 409 | `price.survey_round_pending` | Chốt đợt khi còn phiếu Đã nhập / Cần xác minh / Đã xác minh |
| 409 | `price.survey_stale` | `seq` gửi lên khác phiên bản hiện hành của đợt / phiếu (người khác vừa cập nhật) |
| 404 | `price.survey_form_not_found` | Không có phiếu điều tra trong đợt |
| 409 | `price.survey_form_state` | Thao tác rà soát không hợp trạng thái phiếu (vd duyệt phiếu Cần xác minh chưa ghi nhận kết quả) |
| 422 | `price.survey_import_invalid` | Tệp không đọc được (không phải CSV UTF-8 / .xlsx, có công thức, quá lớn) hoặc ghi nhận khi còn cột bắt buộc chưa ánh xạ |
| 409 | `price.survey_import_changed` | Ghi nhận với tệp / mã băm khác lần xem trước |
| 404 | `price.survey_import_not_found` | Không có lần nhập trong đợt |
| 503 | `price.survey_pii_disabled` | Cổng dữ liệu thật `AG_PRICE_SURVEY_PII` tắt (chờ DPIA) hoặc chưa nối kho định danh — nhập tệp, khai báo cán bộ điều tra |
