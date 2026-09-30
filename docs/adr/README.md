# Architecture Decision Records

| # | Quyết định | Trạng thái |
|---|---|---|
| 0001 | [Go là ngôn ngữ dịch vụ](0001-go-ngon-ngu-dich-vu.md) | Chấp nhận |
| 0002 | [RabbitMQ thay vì hàng đợi trong PostgreSQL](0002-rabbitmq.md) | Chấp nhận |
| 0003 | [Event sourcing mức B](0003-event-sourcing-muc-b.md) | Chấp nhận |
| 0004 | [Mã hóa phong bì X25519 theo hộ](0004-ma-hoa-phong-bi-theo-ho.md) | Chấp nhận — hiện thực ở G1 |
| 0005 | [Row-level security, một policy mỗi bảng](0005-rls-mot-policy-moi-bang.md) | Chấp nhận |
| 0006 | [Engine đồng bộ ngoại tuyến và phương án xử lý slot](0006-engine-dong-bo-ngoai-tuyen.md) | Chấp nhận — 30/09/2026 ([triển khai](trien-khai-g1.md)) |
| 0007 | [Danh mục nhà cung cấp trong lãnh thổ](0007-nha-cung-cap-trong-lanh-tho.md) | Chấp nhận — 30/09/2026 ([triển khai](trien-khai-g1.md)) |
| 0008 | [Phân vùng mạng và bảng luồng](0008-phan-vung-mang.md) | Chấp nhận — hiện thực từ G4 |
| 0009 | [Mô hình tenant theo tổ chức sở hữu dữ liệu](0009-mo-hinh-tenant.md) | Chấp nhận — 30/09/2026 ([triển khai](trien-khai-g1.md)) |
| 0010 | [Kênh nông hộ: Flutter cho hiện trường, Zalo Mini App chỉ online](0010-kenh-nong-ho.md) | Chấp nhận — 30/09/2026 ([triển khai](trien-khai-g1.md)) |
| 0011 | [Bổ sung module nghiệp vụ cho các app](0011-module-nghiep-vu-cac-app.md) | Chấp nhận — 30/09/2026 ([triển khai](trien-khai-g1.md)) |
| 0012 | [Tính duy nhất trên bảng sổ phân vùng](0012-duy-nhat-tren-bang-so-phan-vung.md) | Chấp nhận |
| 0013 | [Quan sát: trace_id từ G0, OTLP từ G1](0013-quan-sat-otlp.md) | Chấp nhận — đã hiện thực OTLP ở G1 |
| 0014 | [Thư viện xuất Excel/PDF và giấy phép phông chữ nhúng](0014-thu-vien-xuat-bao-cao-va-phong-chu.md) | Đề xuất |
| 0015 | [Kênh đẩy thông báo và RB-7](0015-kenh-day-thong-bao-rb7.md) | Đề xuất — đã hiện thực ở pf/notify |
| 0016 | [Giấy phép phụ thuộc Python của optimizer](0016-giay-phep-python.md) | Đề xuất |
| 0017 | [Kho chuỗi thời gian — PostgreSQL phân vùng tháng ở cụm `ts`](0017-kho-chuoi-thoi-gian.md) | Đề xuất — hiện thực ở G3-0 |
| 0018 | [Sự kiện nội bộ MKG — vhost RabbitMQ `mkg.internal` (Q-MKG-01)](0018-su-kien-noi-bo-mkg.md) | Đề xuất — hiện thực ở G3-0 |
| 0019 | [Định danh thiết bị cân, ký tại nguồn và đồng hồ](0019-dinh-danh-thiet-bi-va-dong-ho.md) | Đề xuất — hợp đồng dây đóng băng ở G3-0 |

ADR 0006, 0007, 0009, 0010, 0011 được chủ sản phẩm chấp nhận ngày 30/09/2026 trong phiên làm việc. Bảng đối chiếu cam kết → mã → kiểm thử ở [trien-khai-g1.md](trien-khai-g1.md).

ADR 0001–0008 là danh sách bắt buộc ở Phụ lục B của tài liệu kiến trúc. ADR 0009–0016 phát sinh khi tổng hợp BRD và khi dựng G0. ADR 0017–0019 là nền của cổng G3 ([g3-conventions](../dev/g3-conventions.md)).
