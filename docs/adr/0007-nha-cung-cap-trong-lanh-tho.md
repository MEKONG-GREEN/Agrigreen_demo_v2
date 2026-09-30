# ADR-0007: Danh mục nhà cung cấp trong lãnh thổ

**Trạng thái:** Chấp nhận — 30/09/2026, theo quyết định của chủ sản phẩm trong phiên làm việc · **Ngày:** 27/09/2026

## Quyết định (RB-7)
Dữ liệu cá nhân, sao lưu, ảnh, nhật ký chứng cứ, nhật ký có định danh: **chỉ nhà cung cấp có hạ tầng và pháp nhân lưu trữ trong lãnh thổ Việt Nam.**

| Nhu cầu | Local/Dev | Production (cần chốt) |
|---|---|---|
| Kho đối tượng | MinIO | Kho S3 trong lãnh thổ, có khóa đối tượng |
| Sao lưu | tệp cục bộ | Kho sao lưu trong lãnh thổ, bất biến |
| KMS, bí mật | biến môi trường | KMS trong lãnh thổ |
| Định tuyến đường bộ | — | OSRM tự vận hành |
| Thời tiết, độ mặn, tile vệ tinh | — | Qua proxy **không gửi định danh**, tọa độ làm thô theo lưới; cần ADR riêng cho từng nguồn nước ngoài |

Yêu cầu từ BRD GIS/HTX/KN về API thời tiết nước ngoài (OpenWeather, Open-Meteo) phải đi qua proxy này.

## Proxy khử định danh (`pf/extdata`)
- Client gọi `GET /v1/extdata/{source}/point` hoặc `/tiles/{z}/{x}/{y}`, không gọi thẳng nhà cung cấp.
- Proxy làm thô tọa độ về tâm ô lưới (mặc định 0,1°, tối thiểu 0,05°) và giới hạn zoom tile. Proxy không gửi token, cookie, header của người gọi, mã thiết bị, mã thửa hay mã nông hộ. Nhà cung cấp chỉ thấy ô lưới hoặc tile.
- Danh mục nguồn nằm trong cấu hình `AG_EXTDATA_*`, mặc định chặn. Bộ nhớ đệm theo ô, dùng chung giữa người gọi.
- Ở production, nguồn ngoài lãnh thổ phải thuộc `AG_NONPII_ALLOWLIST` **và** có ADR riêng (`AG_EXTDATA_<ID>_ADR`). Thiếu một trong hai thì dịch vụ không khởi động.
- Nhà cung cấp thời tiết, độ mặn và tile vệ tinh cụ thể **chờ khách chọn** (Q2-10). Khi chọn chỉ cần cấu hình mẫu URL. Local/Dev dùng nhà cung cấp giả (`fake`).

## Triển khai
Đối chiếu từng cam kết với mã và kiểm thử: [trien-khai-g1.md](trien-khai-g1.md#adr-0007-danh-mục-nhà-cung-cấp-trong-lãnh-thổ-rb-7). Biến cấu hình ở [residency.md](../runbooks/residency.md).
