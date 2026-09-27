# ADR-0011: Bổ sung module nghiệp vụ cho các app

**Trạng thái:** Đề xuất · **Ngày:** 27/09/2026

## Bối cảnh (X7)
Danh sách module ở mục 6 chưa có chỗ cho nghiệp vụ của App Khuyến nông, Bản đồ CGH và App HTX.

## Đề xuất thêm vào `internal/platform/`
| Module | Nội dung | Ghi chú |
|---|---|---|
| `extension` | nhiệm vụ hỗ trợ + SLA, chỉ đạo khẩn, lớp tập huấn, danh bạ trực | App KN |
| `mechanization` | máy, chủ sở hữu, định mức có hiệu lực, cân đối cung–cầu | CGH: QT-01/02/03 |
| `cropcycle` | mùa vụ theo SOP, **nhật ký canh tác dạng sổ** (bằng chứng MRV), sản lượng dạng decimal | App HTX: sửa nhật ký = bản ghi bù |
| `content` | thư viện quy trình, tin tức, CMS | KN + HTX |

Mọi yêu cầu "xóa cứng" hoặc "cập nhật ghi đè" trong BRD (GIS FN-21, CGH US-CFG-01 AC-4, HTX US-LOG-02/05, US-YIELD-02) đổi thành phiên bản mới hoặc bản ghi bù (RB-1).
