### pf/extension (`dom/extension` — Báo cáo dịch bệnh BM-10, báo nghi dịch, W11.49)

Dùng lại mã đã có của module: `extension.read_only` (ngữ cảnh chỉ xem), `extension.out_of_scope` (xã ngoài phân công),
`extension.support_reason_required`, `extension.dependency_unavailable` (chưa nối danh mục CNTS hoặc đơn vị hành chính).

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.cnts_outbreak_not_found` | Ổ dịch không có, hoặc thuộc xã ngoài phạm vi người gọi |
| 422 | `extension.cnts_outbreak_invalid` | Nội dung BM-10 sai (lỗi từng trường): thiếu vị trí DB-05, số con chết/tiêu hủy > quy mô đàn, bệnh/vật nuôi không có hoặc không dùng tại ngày diễn biến, bệnh không áp dụng cho vật nuôi, có số điện thoại/CCCD trong văn bản, ngày diễn biến lùi, đổi xã, thiếu lý do kết thúc/mở lại |
| 409 | `extension.cnts_outbreak_version_conflict` | `base_version` khác phiên bản hiện hành của ổ dịch — đã có người cập nhật (khóa lạc quan) |
| 409 | `extension.cnts_outbreak_closed` | Cập nhật diễn biến hoặc kết thúc ổ dịch đã kết thúc |
| 409 | `extension.cnts_outbreak_not_closed` | Mở lại ổ dịch chưa kết thúc |
| 404 | `extension.cnts_suspect_not_found` | Tin báo nghi dịch không có, hoặc thuộc xã ngoài phạm vi |
| 422 | `extension.cnts_suspect_invalid` | Tin báo sai (thiếu mô tả, địa bàn; có số điện thoại/CCCD; thiếu lý do bác bỏ) |
| 409 | `extension.cnts_suspect_version_conflict` | `base_version` khác phiên bản hiện hành của tin báo |
| 409 | `extension.cnts_suspect_resolved` | Xác nhận/bác bỏ tin báo đã được xác nhận hoặc bác bỏ |
