### pf/extension (`dom/extension` — chiến dịch điều tra mở rộng CNTS, W11.50)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.cnts_campaign_not_found` | Không có đợt với mã này, hoặc đợt do Nút Cấp Tỉnh khác tạo |
| 409 | `extension.cnts_campaign_state` | Thao tác không hợp lệ ở trạng thái của đợt: sửa nháp/phát hành đợt đã phát hành; gia hạn, nhắc, đóng đợt không ở Đang thu thập; mở lại đợt chưa đóng (DB: `cnts_campaign_not_draft`, `cnts_campaign_event_state`) |
| 422 | `extension.cnts_campaign_template` | Mã biểu điều tra chưa có ở cấu hình biểu `kn_cnts` (thiết kế biểu trước — FN-07) |
