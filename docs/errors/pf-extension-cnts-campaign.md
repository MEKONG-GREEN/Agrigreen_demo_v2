### pf/extension (`dom/extension` — chiến dịch điều tra mở rộng CNTS, W11.50)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.cnts_campaign_not_found` | Không có đợt với mã này, hoặc đợt do Nút Cấp Tỉnh khác tạo |
| 409 | `extension.cnts_campaign_state` | Thao tác không hợp lệ ở trạng thái của đợt: sửa nháp/phát hành đợt đã phát hành; gia hạn, nhắc, đóng đợt không ở Đang thu thập; mở lại đợt chưa đóng (DB: `cnts_campaign_not_draft`, `cnts_campaign_event_state`) |
| 409 | `extension.cnts_campaign_busy` | Đợt đang có thao tác khác chưa xong (ý định thao tác còn hạn — BF-CNTS-CAMPAIGN-POOL #156): phát hành/gia hạn/nhắc/đóng/mở lại cùng lúc, hoặc sửa nháp trong lúc đang phát hành (DB: `cnts_campaign_busy`). Hết hạn ý định (2 phút) thì thao tác chạy được |
| 409 | `extension.cnts_campaign_interrupted` | Thao tác đã gọi cap/forms nhưng ý định thao tác hết hạn trước khi ghi sự kiện đợt và thao tác khác đã giữ đợt — không ghi sự kiện; kỳ native được người đang giữ đợt đối soát lại theo sổ đợt (#156); tải lại đợt rồi thử lại. Từ #201 (BF-CNTS-CAMPAIGN-CAS-CONSUMER) cũng trả khi: mất ý định trước khi ghi revision kỳ native vào ý định (không gọi cap/forms), mất ý định giữa chuỗi Mở lại → Đổi hạn (không gọi bước sau), hoặc cap/forms từ chối lời ghi có điều kiện (`forms.period_conflict` — kỳ native đã đổi sau khi đợt chụp revision). Không tự thử lại với revision mới; gọi lại với Idempotency-Key mới |
| 422 | `extension.cnts_campaign_template` | Mã biểu điều tra chưa có ở cấu hình biểu `kn_cnts` (thiết kế biểu trước — FN-07) |
| 422 | `extension.cnts_campaign_owner` | Người phụ trách đợt (`owner_ref`) không phải cán bộ Cấp Tỉnh Phòng CNTS còn hiệu lực của tỉnh (danh bạ pf/accounts) — #236, BRD CNTS v4.0 FN-21 B1 |

Đợt điều tra dùng lại `extension.dependency_unavailable` (503) khi: chưa nối cap/forms, kể cả cổng điều khiển kỳ có điều kiện CAS #159 (thiếu thì mọi thao tác ghi kỳ native trả 503, không quay về lời ghi vô điều kiện — #201); cap/forms trả revision không khớp kỳ của đợt; cap/forms quá hạn hoặc lỗi không rõ kết quả (kỳ native đối soát theo sổ đợt khi còn giữ ý định; gọi lại cùng Idempotency-Key được).

Từ #236 (CNTS-CAMPAIGN-SCOPE, BRD CNTS v4.0 FN-21): `403 forbidden` khi người gọi không phải cán bộ tổng hợp và không là người phụ trách đợt (viên chức chuyên môn, chủ nhiệm CT/DA) mà tạo, sửa, phát hành, nhắc, gia hạn, đóng hoặc sao chép đợt giữ người phụ trách khác; Lãnh đạo phòng chỉ mở lại. Thiếu `owner_ref` → `422 validation_failed` trường `owner_ref`.
