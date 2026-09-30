# ADR-0009: Mô hình tenant theo tổ chức sở hữu dữ liệu

**Trạng thái:** Chấp nhận — 30/09/2026, theo quyết định của chủ sản phẩm trong phiên làm việc · **Ngày:** 27/09/2026

## Bối cảnh (xung đột X4 trong bản tổng hợp yêu cầu)
- App Khuyến nông phân phạm vi theo cây TW → Tỉnh → Xã.
- Bản đồ CGH là một khách hàng (Cục Kinh tế hợp tác), Cục xem toàn vùng.
- App HTX coi HTX là tenant.
- Ranh giới thửa do cán bộ khuyến nông vẽ **trước** khi được gán HTX.

## Quyết định
- `tenant_id` = **tổ chức sở hữu dữ liệu** (bên tham gia ở `pf_party`). Giá trị lấy từ claim `tenant_id` của token, không bao giờ từ path, query hay body (R-16). ĐTK 1 chỉ có một tenant (chương trình khuyến nông quốc gia): tài khoản do pf/accounts tạo mang tenant của người tạo.
- Phạm vi theo cây phân cấp và theo HTX là **phân quyền tầng ứng dụng** (R-33: RLS chỉ cách ly tenant), gắn vào `authz.Principal`.
- Dữ liệu dùng chung giữa các ứng dụng (thửa, HTX) thuộc tenant sở hữu. Ứng dụng khác đọc qua `port/` hoặc API, không chồng policy.
- Mã định danh "duy nhất toàn hệ thống" (MAY-, CSH- của CGH) là nhãn hiển thị, không mang ý nghĩa địa lý bất biến.
- Liên thông khác tenant — xem ADR-0020. Ví dụ: tài khoản dịch vụ của tenant MKG đọc thửa và HTX thuộc tenant chương trình (RB-2). ADR này không cấp quyền xuyên tenant.

## Triển khai
Đối chiếu từng cam kết với mã và kiểm thử: [trien-khai-g1.md](trien-khai-g1.md#adr-0009-mô-hình-tenant-theo-tổ-chức-sở-hữu-dữ-liệu).
