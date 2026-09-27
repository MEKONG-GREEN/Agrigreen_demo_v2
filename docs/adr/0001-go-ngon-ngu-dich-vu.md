# ADR-0001: Go là ngôn ngữ dịch vụ

**Trạng thái:** Chấp nhận · **Ngày:** 27/09/2026

Kiến trúc v3.0 mục 4 chốt Go 1.23+ với Gin theo khung Ecombox; ma trận ICS cũng cho Go khi tính đúng trọng số trần hiệu năng 35%.

## Quyết định
- Dịch vụ viết bằng Go (go.mod đặt Go 1.26 do golang.org/x/tools yêu cầu, thỏa ràng buộc 1.23+).
- Hai ngoại lệ bắt buộc: `optimizer/` bằng Python 3.12 (OR-Tools, HiGHS/Pyomo) và `edge-agent` là nhị phân Go tĩnh.
- Prototype `mekong-green` (Node/TypeScript + SQLite) **không được chuyển mã**. Nó chỉ là đặc tả chạy được: quy tắc nghiệp vụ, ca kiểm thử, dữ liệu mẫu địa danh.

## Hệ quả
Toàn bộ tầng hạ tầng (tenant, sổ, kho định danh, outbox, OIDC) dựng mới trong `internal/shared/`.
