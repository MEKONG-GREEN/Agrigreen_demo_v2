### pf/extension — bề mặt phiên KN (KN-STANDALONE-1-ADMIN-TOOLS #264)

| HTTP | code | Khi nào |
|---|---|---|
| 403 | `extension.surface_removed` | Gọi API của công cụ nền tảng không có FN trong BRD 2.0 (tham số & phê duyệt, cây tổ chức G1, mẫu xuất & lịch gửi, xung đột/giám sát/đồng bộ thiết bị, Public API, đo lường, bảng kê, nhật ký xuất) trong ngữ cảnh phiên KN (`X-AG-Context`), ở cả Application API và Admin API. Cổng route do chủ KN cấp (`RouteGate` của `accountsport.SessionPolicies`), middleware phiên của pf/accounts gọi. Phiên nền tảng không ngữ cảnh KN giữ quyền theo vai realm |
