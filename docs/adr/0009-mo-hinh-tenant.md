# ADR-0009: Mô hình tenant theo tổ chức sở hữu dữ liệu

**Trạng thái:** Đề xuất — cần MKG ký · **Ngày:** 27/09/2026

## Bối cảnh (xung đột X4 trong bản tổng hợp yêu cầu)
- App Khuyến nông phân phạm vi theo cây TW → Tỉnh → Xã.
- Bản đồ CGH là một khách hàng (Cục Kinh tế hợp tác), Cục xem toàn vùng.
- App HTX coi HTX là tenant.
- Ranh giới thửa do cán bộ khuyến nông vẽ **trước** khi được gán HTX.

## Đề xuất
- `tenant_id` = **tổ chức sở hữu dữ liệu** (bên tham gia ở `pf_party`). ĐTK 1 chỉ có một tenant (chương trình khuyến nông quốc gia).
- Phạm vi theo cây phân cấp và theo HTX là **phân quyền tầng ứng dụng** (R-33: RLS chỉ cách ly tenant), gắn vào `authz.Principal`.
- Dữ liệu dùng chung giữa các ứng dụng (thửa, HTX) thuộc tenant sở hữu. Ứng dụng khác đọc qua `port/` hoặc API, không chồng policy.
- Mã định danh "duy nhất toàn hệ thống" (MAY-, CSH- của CGH) là nhãn hiển thị, không mang ý nghĩa địa lý bất biến.
