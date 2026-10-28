### pf/extension (`dom/extension` — chiến dịch điều tra mở rộng CNTS, W11.50)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.cnts_campaign_not_found` | Không có đợt với mã này, hoặc đợt do Nút Cấp Tỉnh khác tạo |
| 409 | `extension.cnts_campaign_state` | Thao tác không hợp lệ ở trạng thái của đợt: sửa nháp/phát hành đợt đã phát hành; gia hạn, nhắc, đóng đợt không ở Đang thu thập; mở lại đợt chưa đóng (DB: `cnts_campaign_not_draft`, `cnts_campaign_event_state`) |
| 409 | `extension.cnts_campaign_busy` | Đợt đang có thao tác khác chưa xong (ý định thao tác còn hạn — BF-CNTS-CAMPAIGN-POOL #156): phát hành/gia hạn/nhắc/đóng/mở lại cùng lúc, hoặc sửa nháp trong lúc đang phát hành (DB: `cnts_campaign_busy`). Hết hạn ý định (2 phút) thì thao tác chạy được |
| 409 | `extension.cnts_campaign_interrupted` | Thao tác đã gọi cap/forms nhưng ý định thao tác hết hạn trước khi ghi sự kiện đợt và thao tác khác đã giữ đợt — không ghi sự kiện; kỳ native được người đang giữ đợt đối soát lại theo sổ đợt (#156); tải lại đợt rồi thử lại |
| 422 | `extension.cnts_campaign_template` | Mã biểu điều tra chưa có ở cấu hình biểu `kn_cnts` (thiết kế biểu trước — FN-07) |
