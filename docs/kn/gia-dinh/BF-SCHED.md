## BF-SCHED — Lịch công tác: trường bắt buộc và check-in chỉ khi đã duyệt (#99)

- **Nguồn:** log bug khách ngày 23/10/2026: TT-05/CN-04 (bắt buộc Ngày, Buổi, Xã/phường, Địa điểm, Công việc) và TT-07/CN-06 (lịch chưa duyệt thì không check-in được). PRD `kn-bugfix-brd-alignment`, nhóm B (R-B1, R-B3). R-B2 (thông báo chuông) đã có trên dev, gói này không đổi.
- **Ngày, Buổi, Công việc:** đã bắt buộc từ trước.
- **Xã/phường:** bắt buộc với **cán bộ xã** (vai có danh sách xã phụ trách). Cán bộ tỉnh đăng ký lịch phòng có thể làm việc ngoài phạm vi xã (vd họp tại Trung tâm), nên với cán bộ tỉnh trường này vẫn tùy chọn. Nếu khách muốn cán bộ tỉnh cũng bắt buộc thì đổi một điều kiện trong `NormalizeItems`.
- **Địa điểm:** bắt buộc với mọi người. Nhập **chữ** ("Trụ sở UBND xã") **hoặc chọn vị trí trên bản đồ** (tọa độ dùng để check-in). Có một trong hai là đủ.
- **Check-in:**
  - Lịch Nháp: `extension.checkin_not_registered` (như cũ).
  - Lịch đã gửi nhưng chưa có bản nào được duyệt: `extension.checkin_not_approved` (409, mã mới).
  - Lịch đã duyệt rồi gửi điều chỉnh: bản đã duyệt trước đó **vẫn hiệu lực** (như màn đăng ký đang ghi "Bản đã duyệt vẫn hiệu lực"). Check-in đối chiếu mục của bản đó, và check-in ghi `item_version` = bản đã duyệt. Mục chỉ có ở bản điều chỉnh chưa check-in được (404 `schedule_item_not_found`) cho tới khi được duyệt.
  - Hệ quả: test cũ "duyệt sau check-in" không còn đúng. Test tích hợp đổi sang luồng duyệt → check-in → điều chỉnh → duyệt lại.
- **Lịch nháp cũ** đang thiếu xã hoặc địa điểm vẫn đọc được. Khi lưu hoặc gửi lại, người dùng phải bổ sung (máy chủ trả lỗi theo từng trường `items[i].commune_code` / `items[i].location`).
- **App di động** kiểm cùng quy tắc trước khi lưu ngoại tuyến. Nút check-in chỉ hiện khi lịch đã duyệt, hoặc khi đang điều chỉnh nhưng còn bản đã duyệt.
