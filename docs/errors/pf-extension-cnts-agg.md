### pf/extension (`dom/extension` — tổng hợp CNTS Cấp Tỉnh BM-02, BM-18, W11.44)

| HTTP | code | Khi nào |
|---|---|---|
| 409 | `extension.cnts_aggregate_closed` | Thêm dòng điều chỉnh BM-02 hoặc nhập nuôi trồng BM-18 cho tuần mà bảng tổng hợp đã chốt — mở lại trước (DB: `cnts_agg_input_closed`) |
| 409 | `extension.cnts_aggregate_sources_open` | Chốt bảng tổng hợp khi còn kỳ nguồn của tuần chưa chốt (BM-01 các ngày của tuần KSGM, BM-17 của tuần) hoặc tuần chưa có kỳ nguồn (D17); `detail` = số kỳ đã chốt/tổng |
| 409 | `extension.cnts_aggregate_state` | Chốt bảng đã chốt, hoặc mở lại bảng chưa chốt (DB: `cnts_agg_close_state`) |
| 404 | `extension.cnts_share_not_found` | Không có phiên bản tệp chia sẻ với mã này (hoặc thuộc tỉnh khác) |
