### pf/apiclient — policy feed của cấu hình triển khai

| HTTP | code | Khi nào |
|---|---|---|
| 403 | `apiclient.feed_disabled` | Dataset nằm trong DisabledFeeds do server sở hữu; build/read/snapshot/checksum không được lấy dữ liệu hoặc ký URL. Map nil giữ gateway dùng chung |
