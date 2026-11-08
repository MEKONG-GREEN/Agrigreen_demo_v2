### pf/forms — receipt phát hành batch

| HTTP | code | Khi nào |
|---|---|---|
| 503 | forms.publish_receipt_unavailable | Capability/pool/owner policy chưa nối hoặc caller đang giữ joined transaction; không fallback sang legacy publication. |

### pf/forms — native pending batch/count

| HTTP | code | Khi nào |
|---|---|---|
| 503 | forms.pending_unavailable | Thiếu pool/current owner policy hoặc complete scope; joined transaction, filter không chiếu đầy đủ được hoặc native evidence/coverage không hợp lệ. Không trả fake zero. |
| 409 | forms.pending_changed | Canonical query, quyền hiện hành, native visibility epoch hoặc anchor thay đổi; tải lại từ trang đầu, không nối các snapshot khác nhau. |
