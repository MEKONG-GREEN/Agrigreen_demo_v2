# ADR-0007: Danh mục nhà cung cấp trong lãnh thổ

**Trạng thái:** Đề xuất · **Ngày:** 27/09/2026

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
