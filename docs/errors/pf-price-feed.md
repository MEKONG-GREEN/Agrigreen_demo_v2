### pf/price (`cap/price` — bảng giá ngày qua cổng tích hợp, W11.81)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `price.unknown_public_dataset` | Job dựng feed của cổng tích hợp hỏi pf/price một tập dữ liệu công khai không có (chỉ `price_board_daily`), hoặc mã tỉnh của đơn vị công bố (`AG_INTEGRATION_PRICE_PUBLISHERS`) sai dạng — nội bộ worker, không ra HTTP của đối tác |
