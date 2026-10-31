---
paths:
  - "internal/**/*.go"
  - "cmd/**/*.go"
---
# Go: quy tắc dữ liệu

Nguồn: [g1-conventions §3](../../docs/dev/g1-conventions.md), [package-rules §3](../../docs/process/package-rules.md). Khi lệch, nguồn thắng.

- **Tiền, khối lượng:** `internal/shared/money` (`money.ParseAmount`, `ParsePrice`, `ParseRate`), `internal/shared/mass` (`mass.ParseKg`); cột `numeric`. **Cấm float** ở mọi module có tiền/khối lượng (máy chặn ở một số module qua `agnofloat`, phần còn lại do review). Test so sánh bằng chuỗi.
- **Diện tích (ha), tỷ lệ %:** `github.com/shopspring/decimal`, cột `numeric(14,4)`.
- **Tọa độ:** PostGIS `geometry(…, 4326)`.
- **Định danh:** UUIDv7 — tạo bằng `ids.New()`, kiểm dữ liệu vào bằng `ids.Parse` / `ids.ParseRef`. Client ngoại tuyến được gửi `id` tự sinh.
- **Thời gian:** mọi sự việc có `event_time` (lúc xảy ra) và `recorded_at` (lúc ghi nhận) (R-13). Ngày nghiệp vụ theo giờ Việt Nam: `clock.BusinessDate`, `clock.BusinessDay`; test dùng `clock.NewFixed`.
- **Không xóa cứng** dữ liệu nghiệp vụ (ADR-0011).
- **Dữ liệu cá nhân** (họ tên, SĐT, CCCD, địa chỉ nông hộ, người đại diện, chủ máy) chỉ nằm trong vault; module khác giữ `*_ref`. Trường Go chứa PII phải có tag `pii:"true"` và không đi vào log, `fmt`, `errors` (`agpiilog`).
- **Tenant:** chỉ lấy từ token/ctx. `tenant.WithID` chỉ ở middleware xác thực, `tenant.MintGrant` chỉ ở mã kiểm cấp quyền chia sẻ (`agtenantgate`).
- **Kiểm số ở server** (không âm, bắt buộc, phạm vi) — UI kiểm thêm, không thay thế (AGENTS.md §9 mục 7).
