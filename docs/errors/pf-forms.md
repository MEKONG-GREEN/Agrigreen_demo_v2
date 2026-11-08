### pf/forms — receipt phát hành batch

| HTTP | code | Khi nào |
|---|---|---|
| 503 | forms.publish_receipt_unavailable | Capability/pool/owner policy chưa nối hoặc caller đang giữ joined transaction; không fallback sang legacy publication. |

### #288 — receipt rà soát native

| HTTP | code | Khi nào |
|---|---|---|
| 503 | forms.review_receipt_unavailable | Pool/owner policy chưa nối hoặc caller đang giữ joined TX; không fallback Entry.Review. |

Receipt trả outcome `unknown` khi không có bằng chứng xác minh đúng operation; không đồng nghĩa rollback. Authority sai/thu hồi trả lỗi native hiện hành trước lookup; không lộ receipt metadata.
