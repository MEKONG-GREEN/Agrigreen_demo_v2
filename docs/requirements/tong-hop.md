# Tổng hợp yêu cầu (27/09/2026)

Nguồn: `G:\My Drive\Development`, gồm kiến trúc v3.0 và bốn bộ yêu cầu (BRD, Backlog, prototype).

| Bộ yêu cầu | Khách hàng / người dùng | Quy mô backlog | Điểm chính |
|---|---|---|---|
| **App Khuyến nông** (BRD v1.0, Backlog) | TTKNQG, TTKN tỉnh, khuyến nông viên xã | 52 story: 25 Cao / 21 TB / 6 Thấp | Lõi ĐTK 1. Offline trên Android giá rẻ. Backlog dẫn "BRD v5.0" nhưng bản này **không có trong thư mục** |
| **Bản đồ CGH** (BRD v1.3, Backlog v4.0) | Cục Kinh tế hợp tác (chỉ xem), Admin | 26 story: 15 / 5 / 6 | Số máy cần = diện tích ÷ định mức có hiệu lực tại vụ. Ngưỡng Đủ ≥85%, Chú ý 60–85%, Thiếu <60%. Mốc thí điểm 10/2026 |
| **GIS** (BRD ghi v1.5, Backlog v1.1) | Người dùng, Admin | 38 story: 11 / 18 / 9 | Nền bản đồ dùng chung; event log, replay; đồng bộ có hàng chờ lỗi |
| **App HTX** (Backlog v4.0, prototype v5.1) | Admin, Quản lý HTX, nông dân | 48 story: 27 / 16 / 5 | Lô ruộng GPS, mùa vụ theo SOP, nhật ký có minh chứng (bằng chứng MRV), sản lượng |

## Xung đột với kiến trúc và cách xử lý

| # | Xung đột | Nguồn | Xử lý |
|---|---|---|---|
| X1 | Xóa cứng hoặc ghi đè, trái RB-1 | GIS FN-21; CGH US-CFG-01 AC-4; HTX US-LOG-02/05, US-YIELD-02 | Chuyển thành phiên bản hoặc bản ghi bù ([ADR-0011](../adr/0011-module-nghiep-vu-cac-app.md)) |
| X2 | Gửi mật khẩu tạm qua email | GIS, CGH, HTX | Kích hoạt bằng liên kết Keycloak; MFA cho vai quản trị ([ADR-0010](../adr/0010-kenh-nong-ho.md)) |
| X3 | Zalo Mini App không làm được offline | HTX | Flutter cho luồng offline, Zalo chỉ cho phần online ([ADR-0010](../adr/0010-kenh-nong-ho.md)) |
| X4 | Mô hình tenant khác nhau giữa các app | KN, CGH, HTX | Tenant = tổ chức sở hữu dữ liệu; phạm vi theo cây phân cấp xử lý ở tầng ứng dụng ([ADR-0009](../adr/0009-mo-hinh-tenant.md)) |
| X5 | SĐT dùng làm khóa tìm kiếm, chống trùng | HTX, CGH, KN | Blind index trong vault ([ADR-0004](../adr/0004-ma-hoa-phong-bi-theo-ho.md)) |
| X6 | API thời tiết, tile ở nước ngoài | GIS, HTX, KN | Proxy không gửi định danh ([ADR-0007](../adr/0007-nha-cung-cap-trong-lanh-tho.md)) |
| X7 | Kiến trúc thiếu module nghiệp vụ cho các app | mục 6 | Thêm extension, mechanization, cropcycle, content ([ADR-0011](../adr/0011-module-nghiep-vu-cac-app.md)) |
| X8 | Mã MAY-{tỉnh} lệch khi đổi tỉnh hoặc sáp nhập | CGH | Coi là nhãn hiển thị bất biến ([ADR-0009](../adr/0009-mo-hinh-tenant.md)) |
| X9 | Chưa chốt hệ tọa độ | GIS | Lưu EPSG:4326, chuyển từ VN-2000 khi nhập (cần khách chốt) |

## Câu hỏi cần trả lời trước G1
1. Lấy BRD Khuyến nông v5.0 ở đâu? Bảng đối chiếu mã FR → FN?
2. MRV có thuộc ĐTK 1 không? Dashboard KN đang hiển thị tCO2e.
3. Chính sách lưu và xóa log của GIS và HTX (xung đột X1)?
4. SLA xử lý bản ghi xung đột offline (C-8)? Hai thời hạn báo cáo sự cố (C-9)?
5. Nhà cung cấp thời tiết, độ mặn, tile vệ tinh nào đáp ứng RB-7?
6. Cấp độ an toàn thông tin (C-2)? Danh mục trường MRV (C-1)?

## Vai trò của prototype `mekong-green`
Prototype `mekong-green` gồm khoảng 43k LOC TypeScript/SQLite, 120 bảng, khoảng 470 route. Nó là **đặc tả chạy được**:
- trích quy tắc nghiệp vụ và test thành ca kiểm thử chấp nhận;
- giữ seed địa danh ĐBSCL sau sáp nhập 2025, bộ 49 tham số, engine mô phỏng;
- dùng 21 màn hình làm đặc tả giao diện.

Không chuyển mã từ prototype sang repo này.
