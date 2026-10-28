### pf/extension (`dom/extension` — Lịch trực KSGM BM-03, lịch tiêm phòng BM-07, check-in lịch công tác, W11.46)

Dùng lại mã đã có của module: `extension.read_only`, `extension.out_of_scope`, `extension.support_reason_required`,
`extension.dependency_unavailable` (chưa nối danh mục CNTS hoặc đơn vị hành chính), `extension.schedule_not_found`.

| HTTP | code | Khi nào |
|---|---|---|
| 422 | `extension.cnts_roster_invalid` | Lịch trực sai: ngày ngoài tháng, ca không phải N/Đ/NĐ, trùng ô (cơ sở, viên chức, ngày), cơ sở giết mổ không hoạt động trong tháng, ghi chú có số điện thoại/CCCD |
| 409 | `extension.cnts_roster_version_conflict` | `base_version` khác phiên bản hiện hành của lịch tháng (khóa lạc quan); `current_version` trong phần mở rộng |
| 404 | `extension.cnts_vaccination_not_found` | Lứa không có hoặc ngoài phạm vi người gọi |
| 422 | `extension.cnts_vaccination_invalid` | Lịch tiêm sai: thiếu hộ/địa bàn, tuổi lúc nhận, xã không hợp lệ, loại mô hình/giống không dùng tại ngày nhận giống, Cấp Xã sửa ngày dự kiến, ngày tiêm thực tế ở tương lai hoặc trước ngày nhận giống, không có thay đổi, thiếu lý do hủy |
| 422 | `extension.cnts_vaccination_no_procedure` | Không có quy trình tiêm cho loại mô hình/giống tại ngày nhận giống |
| 409 | `extension.cnts_vaccination_version_conflict` | `base_version` khác phiên bản hiện hành của lứa |
| 409 | `extension.cnts_vaccination_cancelled` | Ghi mũi tiêm hoặc hủy lứa đã hủy |
| 409 | `extension.checkin_disabled` | Phòng ban của lịch không dùng check-in (`extension.work_schedule_checkin` = off, mặc định ngoài CNTS) |
| 409 | `extension.checkin_exists` | Mục công tác đã check-in — check-in không sửa được (FN-12 BR-02); `checkin_id` trong phần mở rộng |
| 409 | `extension.checkin_not_registered` | Lịch còn Nháp — gửi lịch trước khi check-in |
| 409 | `extension.checkin_not_approved` | Lịch đã gửi nhưng chưa có bản nào được duyệt — chỉ check-in theo lịch đã duyệt (log bug TT-07/CN-06, 23/10/2026). Đang có điều chỉnh chờ duyệt lại thì check-in theo bản đã duyệt trước đó |
| 404 | `extension.schedule_item_not_found` | Không có mục trong phiên bản hiện hành của lịch |
